#!/usr/bin/env bash
set -euo pipefail
docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golang:1.27.2 "$@"
