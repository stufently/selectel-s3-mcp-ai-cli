package safety

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGateElicitationError(t *testing.T) {
	cause := errors.New("client disconnected")
	for _, policy := range []string{"confirm", "elicit"} {
		err := (Gate{Policy: policy}).Check(context.Background(), true, func(context.Context, string) (bool, error) { return true, cause }, "delete")
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "confirmation failed") {
			t.Fatalf("GATE_ERROR: policy=%s wrong refusal: %v", policy, err)
		}
	}
}

func TestGateInvalidPolicy(t *testing.T) {
	err := (Gate{Policy: "nope"}).Check(context.Background(), true, nil, "delete")
	if err == nil || err.Error() != "invalid gate policy" {
		t.Fatalf("GATE_POLICY: wrong refusal: %v", err)
	}
}

func TestConfirmationTokenLimit(t *testing.T) {
	c, err := NewConfirmations(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"bucket":"b","key":"k"}`)
	token, err := c.Issue("delete", args)
	if err != nil {
		t.Fatal(err)
	}
	// Base64 permits newlines. Both envelopes otherwise carry a valid live state,
	// so only the length check can refuse the oversized one.
	oversized := token + strings.Repeat("\n", 4097-len(token))
	if err = c.Consume("delete", args, oversized); err == nil || !strings.Contains(err.Error(), "confirmation_invalid") {
		t.Fatalf("TOKEN_LIMIT: oversized live state accepted or wrong refusal: %v", err)
	}
	boundary := token + strings.Repeat("\n", 4096-len(token))
	if err = c.Consume("delete", args, boundary); err != nil {
		t.Fatalf("TOKEN_LIMIT: valid boundary state refused: %v", err)
	}
}

func TestConfirmationFiveMinuteWindow(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	c, err := NewConfirmations(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"key":"k"}`)
	one, err := c.Issue("delete", args)
	if err != nil {
		t.Fatal(err)
	}
	two, err := c.Issue("delete", args)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(4*time.Minute + 59*time.Second)
	if err = c.Consume("delete", args, one); err != nil {
		t.Fatalf("FIVE_MINUTES: live approval at 4m59s refused: %v", err)
	}
	now = now.Add(time.Second)
	if err = c.Consume("delete", args, two); err == nil || !strings.Contains(err.Error(), "confirmation_invalid") {
		t.Fatalf("FIVE_MINUTES: approval at 5m accepted or wrong refusal: %v", err)
	}
}

func TestConfirmationIntegerPrecision(t *testing.T) {
	c, err := NewConfirmations(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"n":9007199254740993}`)
	token, err := c.Issue("delete", args)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Consume("delete", json.RawMessage(`{"n":9007199254740992}`), token); err == nil || !strings.Contains(err.Error(), "confirmation_invalid") {
		t.Fatalf("INTEGER_DIGEST: changed integer accepted or wrong refusal: %v", err)
	}
	if err = c.Consume("delete", args, token); err != nil {
		t.Fatalf("INTEGER_DIGEST: original integer refused: %v", err)
	}
}

func TestScopeRequiresBucket(t *testing.T) {
	s, err := NewScope(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Check("", "key", false); err == nil || err.Error() != "bucket is required" {
		t.Fatalf("EMPTY_BUCKET: wrong refusal: %v", err)
	}
}

func TestDownloadFilePrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	f, err := OpenFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err = f.Write("private", strings.NewReader("contents")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "private"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("DOWNLOAD_MODE: got %o, want 0600", info.Mode().Perm())
	}
}

func TestFilePathValidationReasons(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, `a\b`), []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, path := range []string{`a\b`, "/tmp/private", ""} {
		in, err := f.Read(path)
		if in != nil {
			_ = in.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "file path must be relative to file root") {
			t.Errorf("FILE_PATH: %q wrong refusal: %v", path, err)
		}
	}
}

func TestRedactionLowercaseAndEmptySecret(t *testing.T) {
	if got := Redact("https://example.test/?x-amz-signature=private&ok=1"); strings.Contains(got, "private") || !strings.Contains(got, "ok=1") {
		t.Fatalf("REDACT_LOWER: sensitive query survived: %s", got)
	}
	if got := Redact("ab", ""); got != "ab" {
		t.Fatalf("REDACT_EMPTY: empty secret changed text: %s", got)
	}
}
