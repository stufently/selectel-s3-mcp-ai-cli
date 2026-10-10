package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/safety"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The matrix catches direct elicitation on the new protocol and policy bypasses.
func TestElicitationEndToEnd(t *testing.T) {
	for _, protocol := range []string{"2026-07-28", "2025-11-25"} {
		for _, policy := range []string{"confirm", "elicit", "block", "allow"} {
			for _, capable := range []bool{false, true} {
				for _, action := range []string{"accept", "decline", "cancel"} {
					for _, confirm := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/client=%v/%s/confirm=%v", protocol, policy, capable, action, confirm), func(t *testing.T) {
							options := &mcp.ClientOptions{}
							var forms atomic.Int32
							if capable {
								options.ElicitationHandler = func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
									forms.Add(1)
									if req.Params.Mode != "form" || !strings.Contains(req.Params.Message, "bucket/key") {
										t.Error("wrong elicitation effect")
									}
									return &mcp.ElicitResult{Action: action, Content: map[string]any{"approved": true}}, nil
								}
							}
							ctx, cs, requests := confirmationClient(t, policy, options, protocol)
							out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "s3_delete_object", Arguments: map[string]any{"bucket": "bucket", "key": "key", "confirm": confirm}})
							if err != nil {
								t.Fatal(err)
							}
							want := policy == "allow" || policy == "confirm" && (capable && action == "accept" || !capable && confirm) || policy == "elicit" && capable && action == "accept"
							wantRequests := int32(0)
							if want {
								wantRequests = 1
							}
							if requests.Load() != wantRequests || out.IsError == want {
								t.Fatalf("MRTR_GATE: requests=%d want=%d error=%v content=%+v", requests.Load(), wantRequests, out.IsError, out.Content)
							}
							wantForms := int32(0)
							if capable && (policy == "confirm" || policy == "elicit") {
								wantForms = 1
							}
							if forms.Load() != wantForms {
								t.Fatalf("MRTR_FORMS: forms=%d want=%d", forms.Load(), wantForms)
							}
						})
					}
				}
			}
		}
	}
}

func confirmationClient(t *testing.T, policy string, options *mcp.ClientOptions, protocol string) (context.Context, *mcp.ClientSession, *atomic.Int32) {
	t.Helper()
	return confirmationClientAt(t, policy, options, protocol, time.Now)
}

func confirmationClientAt(t *testing.T, policy string, options *mcp.ClientOptions, protocol string, now func() time.Time) (context.Context, *mcp.ClientSession, *atomic.Int32) {
	t.Helper()
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	requests := new(atomic.Int32)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(endpoint.Close)
	cfg := config.Config{Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret", Policy: policy, Allow: map[string]bool{"delete": true, "write": true}}
	core, err := s3core.New(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	confirmations, err := safety.NewConfirmations(now)
	if err != nil {
		t.Fatal(err)
	}
	server, err := newServer(p, cfg, core, "test", confirmations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.MCP.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "confirmation-tests", Version: "test"}, options).Connect(ctx, ct, &mcp.ClientSessionOptions{ProtocolVersion: protocol})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	if cs.InitializeResult().ProtocolVersion != protocol {
		t.Fatalf("negotiated wrong protocol: %s", cs.InitializeResult().ProtocolVersion)
	}
	return ctx, cs, requests
}

func manualConfirmation(t *testing.T) (context.Context, *mcp.ClientSession, *atomic.Int32, *mcp.CallToolParams, *mcp.CallToolResult) {
	t.Helper()
	return manualConfirmationAt(t, time.Now)
}

func manualConfirmationAt(t *testing.T, now func() time.Time) (context.Context, *mcp.ClientSession, *atomic.Int32, *mcp.CallToolParams, *mcp.CallToolResult) {
	t.Helper()
	opts := &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}, ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept"}, nil
	}}
	ctx, cs, requests := confirmationClientAt(t, "elicit", opts, "2026-07-28", now)
	params := &mcp.CallToolParams{Name: "s3_delete_object", Arguments: map[string]any{"bucket": "bucket", "key": "key"}}
	pending, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if pending.IsError || !pending.NeedsInput() || len(pending.InputRequests) != 1 || pending.RequestState == "" || len(pending.Content) != 0 || pending.StructuredContent != nil || requests.Load() != 0 {
		t.Fatalf("MRTR_PENDING: missing content-free signed confirmation: %+v requests=%d", pending, requests.Load())
	}
	form, ok := pending.InputRequests["confirm"].(*mcp.ElicitParams)
	if !ok || form.Mode != "form" || !strings.Contains(form.Message, "bucket/key") {
		t.Fatal("MRTR_PENDING: wrong confirmation form")
	}
	params.RequestState = pending.RequestState
	params.InputResponses = mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approved": true}}}
	return ctx, cs, requests, params, pending
}

func TestConfirmationSignature(t *testing.T) {
	ctx, cs, requests, params, _ := manualConfirmation(t)
	parts := strings.Split(params.RequestState, ".")
	if len(parts) != 2 {
		t.Fatal("state envelope must have payload and signature")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(sig) == 0 {
		t.Fatal("invalid signature encoding")
	}
	sig[0] ^= 1
	params.RequestState = parts[0] + "." + base64.RawURLEncoding.EncodeToString(sig)
	out, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || requests.Load() != 0 {
		t.Fatalf("STATE_SIGNATURE: forged signature accepted, requests=%d", requests.Load())
	}
}

func TestConfirmationArguments(t *testing.T) {
	ctx, cs, requests, params, _ := manualConfirmation(t)
	params.Arguments = map[string]any{"bucket": "bucket", "key": "different", "confirm": true}
	out, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || requests.Load() != 0 {
		t.Fatalf("STATE_ARGUMENTS: changed arguments accepted, requests=%d", requests.Load())
	}
}

func TestConfirmationNonce(t *testing.T) {
	ctx, cs, requests, params, _ := manualConfirmation(t)
	// Confirm is intentionally excluded from the digest; JSON member order is immaterial.
	params.Arguments = json.RawMessage(`{"confirm":true,"key":"key","bucket":"bucket"}`)
	out, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if out.IsError || requests.Load() != 1 {
		t.Fatalf("STATE_ACCEPT: valid confirmation failed: %+v requests=%d", out, requests.Load())
	}
	out, err = cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || requests.Load() != 1 {
		t.Fatalf("STATE_NONCE: repeated confirmation executed, requests=%d", requests.Load())
	}
}

func TestConfirmationExpired(t *testing.T) {
	var seconds atomic.Int64
	seconds.Store(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC).Unix())
	ctx, cs, requests, params, _ := manualConfirmationAt(t, func() time.Time { return time.Unix(seconds.Load(), 0) })
	seconds.Add(300)
	out, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || requests.Load() != 0 {
		t.Fatalf("STATE_EXPIRED: expired confirmation executed, requests=%d", requests.Load())
	}
}

func TestConfirmationToolChanged(t *testing.T) {
	ctx, cs, requests, params, _ := manualConfirmation(t)
	params.Name = "s3_presign_put"
	out, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError || requests.Load() != 0 {
		t.Fatalf("STATE_TOOL: confirmation accepted for another tool, requests=%d", requests.Load())
	}
}

func TestConfirmationDeclinedCannotBeReused(t *testing.T) {
	for _, action := range []string{"decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			ctx, cs, requests, params, _ := manualConfirmation(t)
			params.InputResponses["confirm"] = &mcp.ElicitResult{Action: action}
			out, err := cs.CallTool(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			if !out.IsError || requests.Load() != 0 {
				t.Fatalf("STATE_DECLINED: %s executed, requests=%d", action, requests.Load())
			}
			params.InputResponses["confirm"] = &mcp.ElicitResult{Action: "accept"}
			out, err = cs.CallTool(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			if !out.IsError || requests.Load() != 0 {
				t.Fatalf("STATE_DECLINED: %s token reused, requests=%d", action, requests.Load())
			}
		})
	}
}

func TestForgedInputResponses(t *testing.T) {
	for _, policy := range []string{"confirm", "elicit", "block", "allow"} {
		t.Run(policy, func(t *testing.T) {
			ctx, cs, requests := confirmationClient(t, policy, &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}}, "2026-07-28")
			out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "s3_delete_object", Arguments: map[string]any{"bucket": "bucket", "key": "key", "confirm": true}, InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept"}}})
			if err != nil {
				t.Fatal(err)
			}
			if !out.IsError || requests.Load() != 0 {
				t.Fatalf("STATE_MISSING: input response without state accepted, requests=%d", requests.Load())
			}
		})
	}
}

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
