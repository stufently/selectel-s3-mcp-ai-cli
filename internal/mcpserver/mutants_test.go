package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
)

func TestLegacyFormElicitation(t *testing.T) {
	var forms atomic.Int32
	opts := &mcp.ClientOptions{ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		forms.Add(1)
		if req.Params.Mode != "form" || !strings.Contains(req.Params.Message, "bucket/key") {
			t.Error("LEGACY_FORM: wrong form")
		}
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approved": true}}, nil
	}}
	ctx, cs, requests := confirmationClient(t, "elicit", opts, "2025-06-18")
	out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "s3_delete_object", Arguments: map[string]any{"bucket": "bucket", "key": "key"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.IsError || out.NeedsInput() || forms.Load() != 1 || requests.Load() != 1 {
		t.Fatalf("LEGACY_FORM: legacy form failed: %s forms=%d requests=%d", resultText(out), forms.Load(), requests.Load())
	}
}

func TestLargeResultTextSummary(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	value := strings.Repeat("x", 262145)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-Amz-Meta-Large", value) }))
	defer endpoint.Close()
	cfg := config.Config{Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret", Policy: "confirm"}
	ctx, cs := connect(t, p, cfg)
	out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "s3_head_object", Arguments: map[string]any{"bucket": "bucket", "key": "key"}})
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(out)
	if out.IsError || len(text) > 1024 || !strings.Contains(text, `"summary"`) {
		t.Fatalf("RESULT_SUMMARY: large response text not shortened: length=%d error=%v", len(text), out.IsError)
	}
	data, ok := out.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("RESULT_SUMMARY: structured result missing: %T", out.StructuredContent)
	}
	metadata, ok := data["metadata"].(map[string]any)
	if !ok || metadata["large"] != value {
		t.Fatal("RESULT_SUMMARY: full metadata lost")
	}
}

func TestFileToolsRegistration(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"", t.TempDir()} {
		for _, write := range []bool{false, true} {
			cfg := config.Config{Pool: p.DefaultPool, Endpoint: p.Pools[0].Endpoint, Policy: "confirm", FileRoot: root, Allow: map[string]bool{"write": write}}
			ctx, cs := connect(t, p, cfg)
			out, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			names := map[string]bool{}
			for _, tool := range out.Tools {
				names[tool.Name] = true
			}
			if names["s3_download_file"] != (root != "") || names["s3_upload_file"] != (root != "" && write) {
				t.Fatalf("FILE_TOOLS: root=%q write=%v download=%v upload=%v", root, write, names["s3_download_file"], names["s3_upload_file"])
			}
		}
	}
}

func TestToolRejectsAdditionalProperties(t *testing.T) {
	ctx, cs, requests := confirmationClient(t, "allow", nil, "2026-07-28")
	out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "s3_head_object", Arguments: map[string]any{"bucket": "bucket", "key": "key", "extra": "ignored?"}})
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || !strings.Contains(resultText(out), "unexpected additional properties") || requests.Load() != 0 {
		t.Fatalf("EXTRA_PROPERTY: wrong refusal or contacted S3: %s requests=%d", resultText(out), requests.Load())
	}
}
