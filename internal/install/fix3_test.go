package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func TestCodexManagedBlockExactBytes(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	begin := "# >>> " + p.Binary + " (managed)\n"
	end := "# <<< " + p.Binary + " (managed)\n"
	want := begin + "[mcp_servers." + p.Binary + "]\n" +
		"args = ['mcp']\ncommand = '/bin/storage'\n" + end
	for _, tc := range []struct{ name, existing string }{
		{"without_parent", "model = 'test'\n"},
		{"explicit_parent", "model = 'test'\n[mcp_servers]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{Client: "codex", Home: t.TempDir(), Binary: "/bin/storage", Args: []string{"mcp"}}
			path, err := ConfigPath(o)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.existing), 0600); err != nil {
				t.Fatal(err)
			}
			for _, force := range []bool{false, true} {
				o.Force = force
				if _, err := MCP(p, o); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				text := string(data)
				start, finish := strings.Index(text, begin), strings.Index(text, end)
				if start < 0 || finish < start || strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
					t.Fatalf("MANAGED_EXACT_BYTES: invalid markers in %q", text)
				}
				if got := text[start : finish+len(end)]; got != want {
					t.Fatalf("MANAGED_EXACT_BYTES: force=%t got=%q want=%q", force, got, want)
				}
			}
		})
	}
}
