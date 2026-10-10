#!/usr/bin/env bash
set -euo pipefail
bash scripts/go.sh sh -c 'go build -o /tmp/docs-binary ./cmd/selectel-s3 && go run scripts/check_docs.go /tmp/docs-binary'
