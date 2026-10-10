package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitAllowFlagOverridesEnv(t *testing.T) {
	p := testProfile(t)
	t.Setenv(p.EnvPrefix+"ALLOW", "write,delete")
	c, err := Load(p, Overrides{AllowDeleteChanged: true, AllowDelete: false})
	if err != nil {
		t.Fatal(err)
	}
	if c.Allow["delete"] || !c.Allow["write"] {
		t.Fatalf("ALLOW_OVERRIDE: explicit denial lost: %v", c.Allow)
	}
	c, err = Load(p, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Allow["delete"] || !c.Allow["write"] {
		t.Fatalf("ALLOW_OVERRIDE: unset flag changed env: %v", c.Allow)
	}
}

func TestOverridePolicyAndFileRoot(t *testing.T) {
	p := testProfile(t)
	t.Setenv(p.EnvPrefix+"DESTRUCTIVE_POLICY", "allow")
	t.Setenv(p.EnvPrefix+"FILE_ROOT", "/env/root")
	c, err := Load(p, Overrides{Policy: "block", FileRoot: "/flag/root"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Policy != "block" {
		t.Fatalf("POLICY_OVERRIDE: got %s", c.Policy)
	}
	if c.FileRoot != "/flag/root" {
		t.Fatalf("ROOT_OVERRIDE: got %s", c.FileRoot)
	}
}

func TestAllowFlagsGrantLevels(t *testing.T) {
	p := testProfile(t)
	t.Setenv(p.EnvPrefix+"ALLOW", "")
	c, err := Load(p, Overrides{AllowWrite: true, AllowDelete: true, AllowAdmin: true, AllowWriteChanged: true, AllowDeleteChanged: true, AllowAdminChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []string{"write", "delete", "admin"} {
		if !c.Allow[level] {
			t.Fatalf("ALLOW_GRANT: explicit %s grant lost: %v", level, c.Allow)
		}
	}
}

func TestOpenedCredentialPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("fixture Lstat: %v %v", info, err)
	}
	// Replace the inode after Lstat, before opening it. Validation must use the
	// opened descriptor, independently of the earlier safe metadata.
	replacement := filepath.Join(dir, "replacement")
	if err = os.WriteFile(replacement, []byte("public"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(replacement, 0644); err != nil { //nolint:gosec // Negative fixture deliberately broadens permissions.
		t.Fatal(err)
	}
	if err = os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	err = validateOpenedCredentials(f)
	if err == nil || !strings.Contains(err.Error(), "credentials must be a regular file with 0600 or stricter permissions") {
		t.Fatalf("OPENED_PERMISSIONS: replacement permissions accepted or wrong refusal: %v", err)
	}
	if err = f.Chmod(0600); err != nil {
		t.Fatal(err)
	}
	if err = validateOpenedCredentials(f); err != nil {
		t.Fatalf("OPENED_PERMISSIONS: private descriptor refused: %v", err)
	}
}
