#!/usr/bin/env bash
set -euo pipefail
bash scripts/go.sh sh -c 'go build -o /tmp/mcp-smoke-binary ./cmd/selectel-s3 && go run scripts/mcp_smoke.go /tmp/mcp-smoke-binary'
