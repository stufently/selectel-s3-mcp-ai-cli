package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func TestCodexInstallWithExplicitMcpServersTable(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Client: "codex", Home: t.TempDir(), Binary: "/bin/storage", Args: []string{"mcp"}}
	path, err := ConfigPath(o)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	foreign := "# exact user bytes\nmodel = 'custom'\n\n[mcp_servers]\nforeign = { command = 'other', args = ['arg'] }\n\n[features]\nmulti_agent = true\n"
	if err = os.WriteFile(path, []byte(foreign), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = MCP(p, o); err != nil {
		t.Fatalf("CODEX_TABLE: explicit table prevented install: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), foreign) || strings.Count(string(content), "[mcp_servers]\n") != 1 {
		t.Fatalf("CODEX_TABLE: foreign bytes changed or table repeated: %s", content)
	}
	var tree map[string]any
	if err = toml.Unmarshal(content, &tree); err != nil {
		t.Fatalf("CODEX_TABLE: invalid TOML: %v", err)
	}
	servers := tree["mcp_servers"].(map[string]any)
	if servers["foreign"].(map[string]any)["command"] != "other" || servers[p.Binary].(map[string]any)["command"] != "/bin/storage" {
		t.Fatalf("CODEX_TABLE: servers lost: %v", servers)
	}
	o.Force = true
	o.Args = append(o.Args, "--allow-delete=false")
	if _, err = MCP(p, o); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), foreign) {
		t.Fatal("CODEX_TABLE: force changed foreign bytes")
	}
	if err = toml.Unmarshal(content, &tree); err != nil {
		t.Fatal(err)
	}
}

func TestUserCommandQuotesArguments(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Client: "claude-code", Scope: "user", Binary: "/opt/my storage/cli", Args: []string{"mcp", "--file-root=/tmp/it's private"}, Env: map[string]string{p.EnvPrefix + "FILE_ROOT": "/tmp/it's private"}}
	out, err := MCP(p, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`'/opt/my storage/cli'`, `'--file-root=/tmp/it'\''s private'`, "'" + p.EnvPrefix + `FILE_ROOT=/tmp/it'\''s private'`} {
		if !strings.Contains(out, want) {
			t.Fatalf("INSTALL_QUOTE: argument not safely quoted: wanted %s, got %s", want, out)
		}
	}
}

func TestCodexDuplicateManagedMarkers(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Client: "codex", Home: t.TempDir(), Binary: "/bin/storage", Args: []string{"mcp"}, Force: true}
	path, err := ConfigPath(o)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	begin, end := "# >>> "+p.Binary+" (managed)", "# <<< "+p.Binary+" (managed)"
	original := begin + "\n" + begin + "\n[mcp_servers." + p.Binary + "]\ncommand = 'user'\n" + end + "\n"
	if err = os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = MCP(p, o)
	if err == nil || !strings.Contains(err.Error(), "invalid managed markers") {
		t.Fatalf("MANAGED_MARKERS: duplicate markers accepted or wrong refusal: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != original {
		t.Fatalf("MANAGED_MARKERS: rejected file changed: err=%v content=%s", err, content)
	}
}
