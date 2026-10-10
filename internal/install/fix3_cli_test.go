package install_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/cli"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func TestInstallPermissionMatrix(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	levels := []string{"write", "delete", "admin"}
	states := []string{"unset", "true", "false"}
	var cases int
	for mask := 0; mask < 8; mask++ {
		var enabled []string
		for i, level := range levels {
			if mask&(1<<i) != 0 {
				enabled = append(enabled, level)
			}
		}
		for _, write := range states {
			for _, deleteState := range states {
				for _, admin := range states {
					cases++
					t.Run(fmt.Sprintf("env=%03b/write=%s/delete=%s/admin=%s", mask, write, deleteState, admin), func(t *testing.T) {
						home := t.TempDir()
						t.Setenv("HOME", home)
						t.Setenv(p.EnvPrefix+"ALLOW", strings.Join(enabled, ","))
						args := []string{"mcp", "install", "--client", "cursor"}
						want := []string{"mcp"}
						for i, state := range []string{write, deleteState, admin} {
							value := mask&(1<<i) != 0
							if state != "unset" {
								args = append(args, "--allow-"+levels[i]+"="+state)
								value = state == "true"
							}
							want = append(want, fmt.Sprintf("--allow-%s=%t", levels[i], value))
						}
						cmd := cli.New(p, "test", "test")
						cmd.SetOut(&bytes.Buffer{})
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
						if got := tree.Servers[p.Binary].Args; !slices.Equal(got, want) {
							t.Fatalf("INSTALL_MATRIX: args=%v got=%v want=%v", args, got, want)
						}
					})
				}
			}
		}
	}
	if cases != 216 {
		t.Fatalf("INSTALL_MATRIX: ran %d cases, want 216", cases)
	}
}
