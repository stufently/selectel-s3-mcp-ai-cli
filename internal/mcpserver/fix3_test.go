package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
)

func TestEveryToolRejectsCaseCollidingKeys(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	for capability := range p.Features {
		p.Features[capability] = true
	}
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(endpoint.Close)
	cfg := config.Config{
		Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret",
		Policy: "allow", Allow: map[string]bool{"write": true, "delete": true, "admin": true}, FileRoot: t.TempDir(),
	}
	core, err := s3core.New(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(p, cfg, core, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.MCP.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "all-tool-keys", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	var tools []*mcp.Tool
	params := &mcp.ListToolsParams{}
	for {
		listed, err := cs.ListTools(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, listed.Tools...)
		if listed.NextCursor == "" {
			break
		}
		params.Cursor = listed.NextCursor
	}
	if len(tools) < 35 {
		t.Fatalf("ALL_TOOL_KEYS: listed %d tools, want at least 35", len(tools))
	}
	for _, tool := range tools {
		t.Run(tool.Name, func(t *testing.T) {
			encoded, err := json.Marshal(tool.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
			var schema jsonschema.Schema
			if err := json.Unmarshal(encoded, &schema); err != nil {
				t.Fatal(err)
			}
			cases := []struct{ name, args string }{{"top", `{"bucket":"bucket","Bucket":"other"}`}}
			fields := make([]string, 0, len(schema.Properties))
			for field := range schema.Properties {
				fields = append(fields, field)
			}
			sort.Strings(fields)
			for _, field := range fields {
				property := schema.Properties[field]
				value := `{"key":"a","Key":"b"}`
				switch {
				case property.Type == "object":
				case property.Type == "array" && property.Items != nil && property.Items.Type == "object":
					value = "[" + value + "]"
				default:
					continue
				}
				cases = append(cases, struct{ name, args string }{
					"nested_" + field, fmt.Sprintf(`{"bucket":"bucket",%q:%s}`, field, value),
				})
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: json.RawMessage(tc.args)})
					if err != nil {
						t.Fatal(err)
					}
					if !out.IsError || !strings.Contains(resultText(out), "case-colliding") || requests.Load() != 0 {
						t.Fatalf("ALL_TOOL_KEYS: args=%s result=%s requests=%d", tc.args, resultText(out), requests.Load())
					}
				})
			}
		})
	}
}
