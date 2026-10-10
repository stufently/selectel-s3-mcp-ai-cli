package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
)

func TestArgumentsRejectNonAdjacentCollisions(t *testing.T) {
	ctx, cs, requests := confirmationClient(t, "block", nil, "2026-07-28")
	for _, tc := range []struct{ name, args string }{
		{"s3_delete_object", `{"key":"a","bucket":"bucket","Key":"b"}`},
		{"s3_delete_objects", `{"bucket":"bucket","objects":[{"key":"a","version_id":"v","Key":"b"}]}`},
	} {
		out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: json.RawMessage(tc.args)})
		if err != nil {
			t.Fatal(err)
		}
		if !out.IsError || !strings.Contains(resultText(out), "case-colliding") || requests.Load() != 0 {
			t.Errorf("NONADJACENT_KEYS: %s: wrong reason or S3 request: %s requests=%d", tc.args, resultText(out), requests.Load())
		}
	}
}

func TestArgumentsValidateOtherTools(t *testing.T) {
	ctx, cs, requests := confirmationClient(t, "block", nil, "2026-07-28")
	for _, tc := range []struct{ name, args string }{
		{"s3_put_object", `{"bucket":"bucket","key":"a","Key":"b","data":"text","overwrite":true}`},
		{"s3_multipart_complete", `{"bucket":"bucket","key":"a","upload_id":"id","overwrite":true,"parts":[{"part_number":1,"etag":"a","ETag":"b"}]}`},
	} {
		out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: json.RawMessage(tc.args)})
		if err != nil {
			t.Fatal(err)
		}
		if !out.IsError || !strings.Contains(resultText(out), "case-colliding") || requests.Load() != 0 {
			t.Errorf("ALL_TOOL_KEYS: %s: wrong reason or S3 request: %s requests=%d", tc.name, resultText(out), requests.Load())
		}
	}
}

func TestArgumentsRejectTrailingJSONValue(t *testing.T) {
	const raw = `{"bucket":"bucket","key":"key"} {"extra":true}`
	if err := validateArgumentKeys(json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), "expected one JSON value") {
		t.Errorf("ONE_JSON_VALUE: validator lost trailing-value reason: %v", err)
	}
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(204) }))
	defer endpoint.Close()
	cfg := config.Config{Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret", Policy: "block"}
	core, err := s3core.New(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(p, cfg, core, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	// JSON transport cannot encode two values as one arguments field. Inject
	// the raw stream at the receiving boundary, then run the real tool handler.
	server.MCP.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok {
				call.Params.Arguments = json.RawMessage(raw)
			}
			return next(ctx, method, req)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.MCP.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "trailing-json-test", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "s3_head_object", Arguments: map[string]any{"bucket": "bucket", "key": "key"}})
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || !strings.Contains(resultText(out), "expected one JSON value") || requests.Load() != 0 {
		t.Fatalf("ONE_JSON_VALUE: handler lost reason or contacted S3: %s requests=%d", resultText(out), requests.Load())
	}
}
