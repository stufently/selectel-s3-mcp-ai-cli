package safety

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// The signing key is generated once per process, never written to disk.
var confirmationKey = sync.OnceValues(func() ([32]byte, error) {
	var key [32]byte
	_, err := rand.Read(key[:])
	return key, err
})

// Confirmations binds a human response to one tool invocation for five minutes.
// Pending nonces belong to this server and are consumed atomically before S3.
type Confirmations struct {
	key     [32]byte
	mu      sync.Mutex
	pending map[string]int64
	now     func() time.Time
}

type confirmationState struct {
	Tool      string `json:"tool"`
	Digest    string `json:"sha256"`
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"expires_at"`
}

func NewConfirmations(now func() time.Time) (*Confirmations, error) {
	key, err := confirmationKey()
	if err != nil {
		return nil, fmt.Errorf("cannot initialize confirmation signing key: %w", err)
	}
	return &Confirmations{key: key, pending: make(map[string]int64), now: now}, nil
}

func argumentDigest(args json.RawMessage) (string, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(args))
	// Preserve integer precision: a changed int64 argument must change the digest.
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("invalid confirmation arguments")
	}
	delete(value, "confirm")
	canonical, err := json.Marshal(value) // encoding/json sorts object keys recursively.
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func (c *Confirmations) Issue(tool string, args json.RawMessage) (string, error) {
	digest, err := argumentDigest(args)
	if err != nil {
		return "", err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	now := c.now()
	state := confirmationState{Tool: tool, Digest: digest, Nonce: hex.EncodeToString(nonce[:]), ExpiresAt: now.Add(5 * time.Minute).Unix()}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(payload)
	c.mu.Lock()
	defer c.mu.Unlock()
	for nonce, expiry := range c.pending {
		if expiry <= now.Unix() {
			delete(c.pending, nonce)
		}
	}
	c.pending[state.Nonce] = state.ExpiresAt
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (c *Confirmations) Consume(tool string, args json.RawMessage, token string) error {
	invalid := func() error {
		return fmt.Errorf("confirmation_invalid: missing, invalid, expired or already used confirmation state")
	}
	if len(token) > 4096 {
		return invalid()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return invalid()
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return invalid()
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return invalid()
	}
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return invalid()
	}
	var state confirmationState
	if json.Unmarshal(payload, &state) != nil || state.Tool != tool {
		return invalid()
	}
	digest, err := argumentDigest(args)
	if err != nil {
		return invalid()
	}
	if state.Digest != digest {
		return invalid()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Unix() >= state.ExpiresAt {
		return invalid()
	}
	if expiry, ok := c.pending[state.Nonce]; !ok || expiry != state.ExpiresAt {
		return invalid()
	}
	delete(c.pending, state.Nonce)
	return nil
}
