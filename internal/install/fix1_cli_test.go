package install_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/cli"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func TestInstallPersistsExplicitDenials(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(p.EnvPrefix+"ALLOW", "delete")
	cmd := cli.New(p, "test", "test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"mcp", "install", "--client", "cursor", "--allow-delete=false"})
	if err = cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tree struct {
		Servers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err = json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	args := tree.Servers[p.Binary].Args
	if len(args) != 4 || args[0] != "mcp" || args[1] != "--allow-write=false" || args[2] != "--allow-delete=false" || args[3] != "--allow-admin=false" {
		t.Fatalf("INSTALL_DENIALS: final permissions not explicit: %v", args)
	}
}
