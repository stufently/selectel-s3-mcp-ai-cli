#!/usr/bin/env bash
set -euo pipefail
# Build first and exec the binary: `go run` does not relay SIGTERM to the child,
# so an interrupted gate would leave a mutant applied.
bash scripts/go.sh sh -c 'go build -o /tmp/mutation-gate scripts/mutation_gate.go && exec /tmp/mutation-gate'
