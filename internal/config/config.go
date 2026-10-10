// Package config resolves flags > environment > private credential file.
package config

import (
	"bufio"
	"fmt"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Overrides struct {
	Pool, Endpoint, Policy, FileRoot    string
	AllowInsecure                       bool
	AllowWrite, AllowDelete, AllowAdmin bool
}
type Config struct {
	Pool          string          `json:"pool"`
	Endpoint      string          `json:"endpoint"`
	AccessKey     string          `json:"-"`
	SecretKey     string          `json:"-"`
	Source        string          `json:"credential_source"`
	Allow         map[string]bool `json:"levels"`
	Policy        string          `json:"destructive_policy"`
	Buckets       []string        `json:"allowed_buckets"`
	Prefixes      []string        `json:"allowed_prefixes"`
	FileRoot      string          `json:"file_root"`
	AuditFile     string          `json:"-"`
	AllowInsecure bool            `json:"-"`
}

func CredentialsPath(p provider.Profile) (string, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(home, ".config", p.Binary, "credentials"), nil
}
func readCredentials(p provider.Profile) (map[string]string, error) {
	path, e := CredentialsPath(p)
	if e != nil {
		return nil, e
	}
	values := map[string]string{}
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return values, nil
	}
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials must be a regular file")
	}
	if info.Mode().Perm() & ^os.FileMode(0600) != 0 {
		return nil, fmt.Errorf("credentials permissions must be 0600 or stricter: %s", path)
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.HasPrefix(key, p.EnvPrefix) {
			return nil, fmt.Errorf("invalid credentials entry (expected %sKEY=VALUE)", p.EnvPrefix)
		}
		values[key] = value
	}
	return values, scan.Err()
}
func split(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func Load(p provider.Profile, o Overrides) (Config, error) {
	c := Config{Pool: p.DefaultPool, Policy: "confirm", Allow: map[string]bool{}, Source: "none"}
	values, e := readCredentials(p)
	if e != nil {
		return c, e
	}
	get := func(suffix string) string {
		key := p.EnvPrefix + suffix
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		return values[key]
	}
	c.AccessKey, c.SecretKey = get("ACCESS_KEY_ID"), get("SECRET_ACCESS_KEY")
	if c.AccessKey != "" || c.SecretKey != "" {
		c.Source = "file"
		if _, ok := os.LookupEnv(p.EnvPrefix + "ACCESS_KEY_ID"); ok {
			c.Source = "env"
		}
		if _, ok := os.LookupEnv(p.EnvPrefix + "SECRET_ACCESS_KEY"); ok {
			c.Source = "env"
		}
	}
	if v := get("POOL"); v != "" {
		c.Pool = v
	}
	if o.Pool != "" {
		c.Pool = o.Pool
	}
	for _, pool := range p.Pools {
		if pool.ID == c.Pool {
			c.Endpoint = pool.Endpoint
		}
	}
	if c.Endpoint == "" {
		return c, fmt.Errorf("unknown pool %q", c.Pool)
	}
	if v := get("ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	if o.Endpoint != "" {
		c.Endpoint = o.Endpoint
	}
	c.AllowInsecure = o.AllowInsecure || get("ALLOW_INSECURE") == "1"
	u, e := url.Parse(c.Endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("invalid endpoint")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowInsecure) {
		return c, fmt.Errorf("endpoint requires https; set %sALLOW_INSECURE=1 to allow http", p.EnvPrefix)
	}
	for _, level := range split(get("ALLOW")) {
		if level != "write" && level != "delete" && level != "admin" {
			return c, fmt.Errorf("unknown access level %q", level)
		}
		c.Allow[level] = true
	}
	for level, enabled := range map[string]bool{"write": o.AllowWrite, "delete": o.AllowDelete, "admin": o.AllowAdmin} {
		if enabled {
			c.Allow[level] = true
		}
	}
	if v := get("DESTRUCTIVE_POLICY"); v != "" {
		c.Policy = v
	}
	if o.Policy != "" {
		c.Policy = o.Policy
	}
	switch c.Policy {
	case "confirm", "elicit", "block", "allow":
	default:
		return c, fmt.Errorf("unknown destructive policy")
	}
	c.Buckets, c.Prefixes = split(get("ALLOWED_BUCKETS")), split(get("ALLOWED_PREFIXES"))
	c.FileRoot = get("FILE_ROOT")
	if o.FileRoot != "" {
		c.FileRoot = o.FileRoot
	}
	c.AuditFile = get("AUDIT_FILE")
	return c, nil
}
func (c Config) Fingerprint() string {
	if c.AccessKey == "" {
		return "(not configured)"
	}
	r := []rune(c.AccessKey)
	if len(r) > 4 {
		r = r[:4]
	}
	return string(r) + "…"
}
func SaveCredentials(p provider.Profile, key, secret string) error {
	if key == "" || secret == "" || strings.ContainsAny(key+secret, "\r\n") {
		return fmt.Errorf("credentials must be nonempty single lines")
	}
	path, e := CredentialsPath(p)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".credentials-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		_ = f.Close()
		return e
	}
	_, e = fmt.Fprintf(f, "%sACCESS_KEY_ID=%s\n%sSECRET_ACCESS_KEY=%s\n", p.EnvPrefix, key, p.EnvPrefix, secret)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), path)
}
