package config

import (
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testProfile(t *testing.T) provider.Profile {
	t.Helper()
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	p.EnvPrefix = "TEST_STORAGE_"
	p.Binary = "test-storage"
	t.Setenv("HOME", t.TempDir())
	return p
}
func TestPrecedence(t *testing.T) {
	p := testProfile(t)
	if e := SaveCredentials(p, "filekey", "filesecret"); e != nil {
		t.Fatal(e)
	}
	t.Setenv(p.EnvPrefix+"ACCESS_KEY_ID", "envkey")
	t.Setenv(p.EnvPrefix+"SECRET_ACCESS_KEY", "envsecret")
	t.Setenv(p.EnvPrefix+"POOL", "ru-3")
	c, e := Load(p, Overrides{Pool: "ru-6"})
	if e != nil {
		t.Fatal(e)
	}
	if c.Pool != "ru-6" || c.AccessKey != "envkey" || c.SecretKey != "envsecret" {
		t.Fatalf("wrong precedence: pool=%s key=%s", c.Pool, c.AccessKey)
	}
	if c.Endpoint != p.Pools[2].Endpoint {
		t.Fatal("pool endpoint not derived")
	}
	if c.Policy != "confirm" || len(c.Allow) != 0 {
		t.Fatal("unsafe defaults")
	}
	if c.Fingerprint() != "envk…" {
		t.Fatal("wrong fingerprint")
	}
}
func TestCredentialPermissions(t *testing.T) {
	p := testProfile(t)
	if e := SaveCredentials(p, "key", "secret"); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(os.Getenv("HOME"), ".config", p.Binary, "credentials")
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	_, e := Load(p, Overrides{})
	if e == nil || !strings.Contains(e.Error(), "0600") {
		t.Fatal("PERMISSIONS: 0644 credential file accepted")
	}
}
func TestInsecureEndpoint(t *testing.T) {
	p := testProfile(t)
	t.Setenv(p.EnvPrefix+"ACCESS_KEY_ID", "key")
	t.Setenv(p.EnvPrefix+"SECRET_ACCESS_KEY", "secret")
	if _, e := Load(p, Overrides{Endpoint: "http://localhost:7070"}); e == nil {
		t.Fatal("http accepted without opt-in")
	}
	t.Setenv(p.EnvPrefix+"ALLOW_INSECURE", "1")
	if _, e := Load(p, Overrides{Endpoint: "http://localhost:7070"}); e != nil {
		t.Fatal(e)
	}
}
