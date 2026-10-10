package install

import (
	"os"
	"strings"
	"testing"

	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func TestCodexManagedBlockStartsWithChildTable(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Client: "codex", Home: t.TempDir(), Binary: "/bin/storage", Args: []string{"mcp"}}
	for _, force := range []bool{false, true} {
		o.Force = force
		if _, err := MCP(p, o); err != nil {
			t.Fatal(err)
		}
		path, err := ConfigPath(o)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		begin := "# >>> " + p.Binary + " (managed)\n"
		_, body, ok := strings.Cut(string(data), begin)
		if !ok || !strings.HasPrefix(body, "[mcp_servers."+p.Binary+"]\n") {
			t.Fatalf("MANAGED_BLOCK_BODY: force=%t: child table must immediately follow marker: %q", force, body)
		}
	}
}
