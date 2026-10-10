package install_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/cli"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func TestInstallPersistsWriteAndAdminPermissions(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, env, flag string
		want            []string
	}{
		{"deny_write", "write,delete", "--allow-write=false", []string{"mcp", "--allow-write=false", "--allow-delete=true", "--allow-admin=false"}},
		{"deny_admin", "write,admin", "--allow-admin=false", []string{"mcp", "--allow-write=true", "--allow-delete=false", "--allow-admin=false"}},
		{"grant_admin", "admin", "", []string{"mcp", "--allow-write=false", "--allow-delete=false", "--allow-admin=true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv(p.EnvPrefix+"ALLOW", tc.env)
			cmd := cli.New(p, "test", "test")
			cmd.SetOut(&bytes.Buffer{})
			args := []string{"mcp", "install", "--client", "cursor"}
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
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
			if err := json.Unmarshal(data, &tree); err != nil {
				t.Fatal(err)
			}
			if got := tree.Servers[p.Binary].Args; !slices.Equal(got, tc.want) {
				t.Fatalf("INSTALL_PERMISSIONS: %s: got %v want %v", tc.name, got, tc.want)
			}
		})
	}
}
