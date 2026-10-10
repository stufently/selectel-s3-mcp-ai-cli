package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"os"
	"strings"
	"testing"
)

func TestInstallPreservesResolvedLevels(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(p.EnvPrefix+"ALLOW", "write,delete")
	t.Setenv(p.EnvPrefix+"SECRET_ACCESS_KEY", "secret-test")
	var first string
	for i := range 2 {
		out := &bytes.Buffer{}
		cmd := New(p, "test", "abc")
		cmd.SetOut(out)
		cmd.SetArgs([]string{"mcp", "install", "--client", "cursor", "--dry-run"})
		if e = cmd.Execute(); e != nil {
			t.Fatal(e)
		}
		var tree map[string]any
		if e = json.Unmarshal(out.Bytes(), &tree); e != nil {
			t.Fatal(e)
		}
		server := tree["mcpServers"].(map[string]any)[p.Binary].(map[string]any)
		args := server["args"].([]any)
		if len(args) != 3 || args[1] != "--allow-write" || args[2] != "--allow-delete" {
			t.Fatalf("resolved env levels not installed: %v", args)
		}
		if strings.Contains(out.String(), "secret-test") {
			t.Fatal("installer leaked secret")
		}
		if i == 0 {
			first = out.String()
		} else if first != out.String() {
			t.Fatal("install output changed without configuration change")
		}
	}
}

func TestVersionAndCompat(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	out := &bytes.Buffer{}
	cmd := New(p, "0.1.0", "abc")
	cmd.SetOut(out)
	cmd.SetArgs([]string{"version"})
	if e = cmd.Execute(); e != nil {
		t.Fatal(e)
	}
	if out.String() != p.Binary+" 0.1.0 (abc)\n" {
		t.Fatal("version format")
	}
	out.Reset()
	cmd = New(p, "test", "abc")
	cmd.SetOut(out)
	cmd.SetArgs([]string{"compat", "--markdown"})
	if e = cmd.Execute(); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"| versioning | true |", "| object_tagging | false |", "ru-6", "2026-10"} {
		if !strings.Contains(out.String(), s) {
			t.Fatalf("compat lost %s", s)
		}
	}
}
func TestNonTTYDelete(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(p.EnvPrefix+"ACCESS_KEY_ID", "fake")
	t.Setenv(p.EnvPrefix+"SECRET_ACCESS_KEY", "fake-secret")
	cmd := New(p, "test", "abc")
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"rm", "s3://bucket/object"})
	e = cmd.Execute()
	var exit *ExitError
	if !errors.As(e, &exit) || exit.Code != 2 {
		t.Fatalf("non-TTY delete must exit 2: %v", e)
	}
}
func TestAuthSet(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	cmd := New(p, "test", "abc")
	cmd.SetIn(strings.NewReader("testkey\nsecret-material\n"))
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"auth", "set"})
	if e = cmd.Execute(); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "secret-material") {
		t.Fatal("auth set echoed secret")
	}
	path := os.Getenv("HOME") + "/.config/" + p.Binary + "/credentials"
	info, e := os.Stat(path) //nolint:gosec // HOME is a t.TempDir fixture and the profile is embedded test data.
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credentials mode")
	}
}
