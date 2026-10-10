package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func resultText(out *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range out.Content {
		if c, ok := content.(*mcp.TextContent); ok {
			text.WriteString(c.Text)
		}
	}
	return text.String()
}

func TestConfirmFormRequiresApprovedTrue(t *testing.T) {
	for _, tc := range []struct {
		name     string
		content  map[string]any
		accepted bool
	}{
		{"false", map[string]any{"approved": false}, false},
		{"missing", nil, false},
		{"string", map[string]any{"approved": "true"}, false},
		{"number", map[string]any{"approved": 1}, false},
		{"true", map[string]any{"approved": true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cs, requests, params, _ := manualConfirmation(t)
			params.InputResponses["confirm"] = &mcp.ElicitResult{Action: "accept", Content: tc.content}
			out, err := cs.CallTool(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			if tc.accepted {
				if out.IsError || out.NeedsInput() || requests.Load() != 1 {
					t.Fatalf("APPROVED_TRUE: approved request failed: %+v requests=%d", out, requests.Load())
				}
			} else if !out.IsError || !strings.Contains(resultText(out), "declined:") || requests.Load() != 0 {
				t.Fatalf("APPROVED_TRUE: invalid approval accepted or wrong refusal: %+v requests=%d", out, requests.Load())
			}
		})
	}
}

func TestArgumentsRejectCaseCollidingKeys(t *testing.T) {
	var forms atomic.Int32
	opts := &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		forms.Add(1)
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approved": true}}, nil
	}}
	ctx, cs, requests := confirmationClient(t, "elicit", opts, "2026-07-28")
	for _, tc := range []struct{ name, args string }{
		{"s3_delete_object", `{"bucket":"bucket","key":"a","Key":"b"}`},
		{"s3_delete_object", `{"bucket":"bucket","Key":"b","key":"a"}`},
		{"s3_delete_object", `{"bucket":"bucket","key":"a","key":"b"}`},
		{"s3_delete_object", `{"bucket":"bucket","key":"a","\u006bey":"b"}`},
		{"s3_delete_objects", `{"bucket":"bucket","objects":[{"key":"a","Key":"b"}]}`},
		{"s3_delete_objects", `{"bucket":"bucket","objects":[{"Key":"b","key":"a"}]}`},
		{"s3_delete_objects", `{"bucket":"bucket","objects":[{"key":"a","key":"b"}]}`},
		{"s3_delete_objects", `{"bucket":"bucket","objects":[{"key":"a","nested":{"x":1,"X":2}}]}`},
	} {
		out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: json.RawMessage(tc.args)})
		if err != nil {
			t.Fatal(err)
		}
		if !out.IsError || !strings.Contains(resultText(out), "case-colliding") || requests.Load() != 0 || forms.Load() != 0 {
			t.Errorf("ARGUMENT_KEYS: %s: wrong refusal or reached gate/S3: %s requests=%d forms=%d", tc.args, resultText(out), requests.Load(), forms.Load())
		}
	}
}

func TestDownloadFileOverwriteGated(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); _, _ = fmt.Fprint(w, "replacement") }))
	defer endpoint.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	if err = os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret", Policy: "elicit", FileRoot: dir}
	opts := &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}, ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approved": true}}, nil
	}}
	ctx, cs := connectWithOptions(t, p, cfg, opts)
	params := &mcp.CallToolParams{Name: "s3_download_file", Arguments: map[string]any{"bucket": "bucket", "key": "key", "path": "target"}}
	out, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || !strings.Contains(resultText(out), "file exists") {
		t.Errorf("DOWNLOAD_OVERWRITE: existing file not refused: %s", resultText(out))
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Errorf("DOWNLOAD_OVERWRITE: existing file changed: %q err=%v", data, err)
	}
	before := requests.Load()
	params.Arguments = map[string]any{"bucket": "bucket", "key": "key", "path": "target", "overwrite": true}
	out, err = cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if out.IsError || !out.NeedsInput() || out.RequestState == "" || requests.Load() != before {
		t.Fatalf("DOWNLOAD_OVERWRITE: overwrite did not request confirmation before S3: %+v requests=%d", out, requests.Load())
	}
	params.RequestState = out.RequestState
	params.InputResponses = mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approved": true}}}
	out, err = cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := os.ReadFile(path)
	if out.IsError || out.NeedsInput() || requests.Load() != before+1 || readErr != nil || string(data) != "replacement" {
		t.Fatalf("DOWNLOAD_OVERWRITE: approved overwrite failed: %+v data=%q err=%v", out, data, readErr)
	}
}
