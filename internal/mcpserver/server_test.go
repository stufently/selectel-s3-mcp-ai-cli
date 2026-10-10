package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func connect(t *testing.T, p provider.Profile, c config.Config) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	core, e := s3core.New(c, p)
	if e != nil {
		t.Fatal(e)
	}
	server, e := New(p, c, core, "test")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = server.Close() })
	ct, st := mcp.NewInMemoryTransports()
	ss, e := server.MCP.Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "tests", Version: "test"}, nil).Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ctx, cs
}
func TestRegistration(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"read", "write", "write-delete", "all"} {
		c := config.Config{Pool: p.DefaultPool, Endpoint: p.Pools[0].Endpoint, AccessKey: "fake", SecretKey: "fake-secret", Allow: map[string]bool{}, Policy: "confirm"}
		if name != "read" {
			c.Allow["write"] = true
		}
		if name == "write-delete" || name == "all" {
			c.Allow["delete"] = true
		}
		if name == "all" {
			c.Allow["admin"] = true
		}
		ctx, cs := connect(t, p, c)
		result, e := cs.ListTools(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		names := []string{}
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		sort.Strings(names)
		expected, e := os.ReadFile("../../testdata/tools/" + name + ".golden")
		if e != nil {
			t.Fatal(e)
		}
		if strings.Join(names, "\n")+"\n" != string(expected) {
			t.Fatalf("REGISTRATION: %s mismatch: %v", name, names)
		}
	}
}
func TestCapabilities(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	c := config.Config{Pool: p.DefaultPool, Endpoint: p.Pools[0].Endpoint, Allow: map[string]bool{"write": true}, Policy: "confirm"}
	for _, enabled := range []bool{false, true} {
		p.Features["object_tagging"] = enabled
		ctx, cs := connect(t, p, c)
		out, e := cs.ListTools(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		count := 0
		for _, tool := range out.Tools {
			if strings.Contains(tool.Name, "object_tagging") {
				count++
			}
		}
		want := 0
		if enabled {
			want = 2
		}
		if count != want {
			t.Fatalf("CAPABILITY: tagging=%v count=%d", enabled, count)
		}
	}
}
func TestResourcesNoSecrets(t *testing.T) {
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	c := config.Config{Pool: p.DefaultPool, Endpoint: p.Pools[0].Endpoint, AccessKey: "fake-key", SecretKey: "fake-secret", Policy: "confirm"}
	ctx, cs := connect(t, p, c)
	for _, path := range []string{"config", "capabilities", "pools"} {
		result, e := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: p.URIScheme + "://" + path})
		if e != nil {
			t.Fatal(e)
		}
		data, e := json.Marshal(result)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(data), "fake-secret") || strings.Contains(string(data), "fake-key") {
			t.Fatal("resource leaked credentials")
		}
	}
}
