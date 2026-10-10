#!/usr/bin/env bash
set -euo pipefail
network="s3-conformance-$$"
gateway="s3-gateway-$$"
data="$PWD/.conformance-data/$$"
cleanup() {
  rc=$?
  if ! docker rm -f "$gateway" >/dev/null 2>&1; then printf 'gateway cleanup: %s\n' "$gateway" >&2; fi
  if ! docker network rm "$network" >/dev/null 2>&1; then printf 'network cleanup: %s\n' "$network" >&2; fi
  exit "$rc"
}
trap cleanup EXIT
mkdir -p "$data/data" "$data/versions"
docker network create "$network" >/dev/null
docker run -d --name "$gateway" --network "$network" --user "$(id -u):$(id -g)" -v "$data/data:/data" -v "$data/versions:/ver" -e ROOT_ACCESS_KEY=testkey -e ROOT_SECRET_KEY=testsecret12345 -e VGW_REGION=ru-1 versity/versitygw:v1.8.0 --port :7070 posix --versioning-dir /ver /data >/dev/null
docker run --rm --network "$network" -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e "SELECTEL_S3_ENDPOINT=http://$gateway:7070" -e SELECTEL_S3_ALLOW_INSECURE=1 -e SELECTEL_S3_ACCESS_KEY_ID=testkey -e SELECTEL_S3_SECRET_ACCESS_KEY=testsecret12345 -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golang:1.27.2 go test -tags conformance -count=1 ./conformance/...
