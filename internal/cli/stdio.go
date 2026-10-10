package cli

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/mcpserver"
)

func runStdio(ctx context.Context, server *mcpserver.Server) error {
	return server.MCP.Run(ctx, &mcp.StdioTransport{})
}
