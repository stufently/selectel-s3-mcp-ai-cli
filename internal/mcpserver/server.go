// Package mcpserver exposes the guarded core through stdio MCP.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/analytics"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/safety"
	"os"
	"strings"
	"time"
)

type Server struct {
	MCP   *mcp.Server
	Audit *safety.Audit
}

func (s *Server) Close() error { return s.Audit.Close() }
func New(p provider.Profile, c config.Config, core *s3core.Client, version string) (*Server, error) {
	confirmations, e := safety.NewConfirmations()
	if e != nil {
		return nil, e
	}
	audit, e := safety.NewAudit(os.Stderr, c.AuditFile, c.SecretKey, c.AccessKey)
	if e != nil {
		return nil, e
	}
	server := mcp.NewServer(&mcp.Implementation{Name: p.Binary, Version: version}, &mcp.ServerOptions{Instructions: "Read-only by default. Get human approval for destructive operations. Transfer large bytes by presigned URLs or confined files."})
	names := []string{}
	for _, spec := range Tools(p, c) {
		names = append(names, spec.Name)
		schema := inputSchema(spec)
		resolved, e := schema.Resolve(nil)
		if e != nil {
			_ = audit.Close()
			return nil, e
		}
		server.AddTool(&mcp.Tool{Name: spec.Name, Description: spec.Description, InputSchema: schema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			args := s3core.Args{}
			outcome := "error"
			defer func() {
				key := args.Key
				if key == "" {
					key = args.Prefix
				}
				audit.Record(spec.Name, strings.TrimPrefix(spec.Name, "s3_"), args.Bucket, key, outcome, s3core.Gated(spec.Name, args), start)
			}()
			failure := func(err error) (*mcp.CallToolResult, error) {
				mapped := s3core.MapError(err, c.SecretKey, c.AccessKey)
				data := map[string]any{"error": mapped}
				encoded, _ := json.Marshal(data)
				return &mcp.CallToolResult{IsError: true, StructuredContent: data, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}, nil
			}
			var raw map[string]any
			if e := json.Unmarshal(req.Params.Arguments, &raw); e != nil {
				return failure(fmt.Errorf("invalid tool arguments"))
			}
			if e := resolved.Validate(raw); e != nil {
				return failure(fmt.Errorf("invalid tool arguments: %w", e))
			}
			if e := json.Unmarshal(req.Params.Arguments, &args); e != nil {
				return failure(fmt.Errorf("invalid tool arguments"))
			}
			supportsForm := false
			if req.Session != nil {
				ip := req.Session.InitializeParams()
				if ip != nil && ip.Capabilities != nil && ip.Capabilities.Elicitation != nil && (ip.Capabilities.Elicitation.Form != nil || ip.Capabilities.Elicitation.URL == nil) {
					supportsForm = true
				}
			}
			var ask safety.Elicitor
			var inputResult *mcp.CallToolResult
			inputRequired := errors.New("confirmation input required")
			if req.Params.InputResponses != nil || req.Params.RequestState != "" {
				if e := confirmations.Consume(spec.Name, req.Params.Arguments, req.Params.RequestState); e != nil {
					return failure(e)
				}
				response, ok := req.Params.InputResponses["confirm"].(*mcp.ElicitResult)
				if !supportsForm || !ok || response == nil || response.Action != "accept" {
					return failure(fmt.Errorf("declined: human confirmation was not accepted"))
				}
				ask = func(context.Context, string) (bool, error) { return true, nil }
			} else if supportsForm {
				ask = func(_ context.Context, effect string) (bool, error) {
					state, e := confirmations.Issue(spec.Name, req.Params.Arguments)
					if e != nil {
						return false, e
					}
					inputResult = &mcp.CallToolResult{
						InputRequests: mcp.InputRequestMap{"confirm": &mcp.ElicitParams{Mode: "form", Message: effect, RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{"approved": map[string]any{"type": "boolean", "title": "Approve destructive action"}}, "required": []string{"approved"}}}},
						RequestState:  state,
					}
					return false, inputRequired
				}
			}
			var result any
			var e error
			switch spec.Name {
			case "s3_largest_objects", "s3_prefix_usage", "s3_unfinished_uploads":
				result, e = analytics.Query(ctx, core, spec.Name, args)
			default:
				result, e = core.Execute(ctx, spec.Name, args, ask)
			}
			if e != nil {
				if errors.Is(e, inputRequired) {
					outcome = "input_required"
					return inputResult, nil
				}
				return failure(e)
			}
			data, e := json.Marshal(result)
			if e != nil {
				return failure(fmt.Errorf("cannot encode response"))
			}
			text := string(data)
			if len(data) > int(s3core.InlineLimit) {
				text = `{"summary":"Result is available in structuredContent; text omitted because it exceeds 262144 bytes."}`
			}
			outcome = "success"
			return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
		})
	}
	resources := map[string]any{"config": map[string]any{"configuration": c, "version": version}, "capabilities": map[string]any{"features": p.Features, "tools": names}, "pools": map[string]any{"pools": p.Pools, "storage_classes": p.StorageClasses}}
	for path, value := range resources {
		uri := p.URIScheme + "://" + path
		server.AddResource(&mcp.Resource{URI: uri, Name: path, MIMEType: "application/json"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			data, e := json.Marshal(value)
			if e != nil {
				return nil, e
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(data)}}}, nil
		})
	}
	return &Server{MCP: server, Audit: audit}, nil
}
