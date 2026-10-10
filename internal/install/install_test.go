package install

import (
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONInstall(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	p.Binary = "test-storage"
	p.EnvPrefix = "TEST_STORAGE_"
	for _, client := range []string{"cursor", "claude-desktop", "claude-code"} {
		home := t.TempDir()
		project := t.TempDir()
		options := Options{Client: client, Scope: "project", Home: home, Project: project, Binary: "/usr/bin/test-storage", Args: []string{"mcp", "--allow-write"}}
		path, e := ConfigPath(options)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, []byte(`{"other":{"preserve":true},"mcpServers":{"foreign":{"command":"foreign"}}}`), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = MCP(p, options); e != nil {
			t.Fatal(e)
		}
		first, e := os.ReadFile(path) //nolint:gosec // ConfigPath uses fixture HOME/project directories.
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(first), "foreign") || !strings.Contains(string(first), "preserve") {
			t.Fatal("foreign keys lost")
		}
		if _, e = MCP(p, options); e != nil {
			t.Fatal(e)
		}
		second, e := os.ReadFile(path) //nolint:gosec // Same fixture config as the first installation.
		if e != nil {
			t.Fatal(e)
		}
		if string(first) != string(second) {
			t.Fatal("JSON install not idempotent")
		}
		if _, e = os.Stat(path + ".bak"); e != nil {
			t.Fatal("backup missing")
		}
		options.Env = map[string]string{p.EnvPrefix + "SECRET_ACCESS_KEY": "do-not-write"}
		if _, e = MCP(p, options); e == nil {
			t.Fatal("installer accepted secret")
		}
	}
}
func TestCodexInstall(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	p.Binary = "test-storage"
	options := Options{Client: "codex", Home: t.TempDir(), Binary: "/usr/bin/test-storage", Args: []string{"mcp"}}
	path, e := ConfigPath(options)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	foreign := "# user comment\nmodel = \"test\"\n[mcp_servers.foreign]\ncommand = \"foreign\"\n"
	if e = os.WriteFile(path, []byte(foreign), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = MCP(p, options); e != nil {
		t.Fatal(e)
	}
	if _, e = MCP(p, options); e == nil {
		t.Fatal("existing codex table replaced without --force")
	}
	options.Force = true
	options.Args = append(options.Args, "--allow-write")
	if _, e = MCP(p, options); e != nil {
		t.Fatal(e)
	}
	content, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(string(content), foreign) || strings.Count(string(content), "[mcp_servers.test-storage]") != 1 {
		t.Fatal("codex foreign bytes lost or duplicate table")
	}
	if e = os.WriteFile(path, []byte(foreign+"[mcp_servers.test-storage]\ncommand=\"user-owned\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = MCP(p, options); e == nil {
		t.Fatal("unmanaged codex table overwritten")
	}
}
func TestDryRunAndUserCommand(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	options := Options{Client: "cursor", Home: t.TempDir(), Binary: "/bin/storage", Args: []string{"mcp"}, DryRun: true}
	path, e := ConfigPath(options)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = MCP(p, options); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("dry run wrote file")
	}
	options.Client = "claude-code"
	options.Scope = "user"
	options.DryRun = false
	options.Env = map[string]string{p.EnvPrefix + "POOL": "ru-6"}
	out, e := MCP(p, options)
	if e != nil || !strings.Contains(out, "claude mcp add") || !strings.Contains(out, p.EnvPrefix+"POOL=ru-6") {
		t.Fatal("missing user installation command")
	}
}
func TestSkillInstall(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skill")
	if e := Skill(target); e != nil {
		t.Fatal(e)
	}
	if e := Skill(target); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"SKILL.md", "references/backup-restore.md", "references/share-file.md", "references/least-privilege.md", "references/cost-hygiene.md", "references/migration.md", "references/incident-response.md"} {
		data, e := os.ReadFile(filepath.Join(target, name))
		if e != nil || len(data) == 0 {
			t.Fatalf("missing skill file %s: %v", name, e)
		}
	}
}
