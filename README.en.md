# Selectel S3 MCP Server & AI CLI

Selectel S3 Object Storage for Claude Code, Cursor and Codex: a stdio MCP server, streaming CLI and embedded agent skill in one Go binary. Read-only MCP is the default; large backup files travel through presigned URLs, multipart or confined file tools.

## Features

Object CRUD, server-side copy/move, versions, multipart and presigned GET/PUT. Bucket CORS, policy and Object Lock configuration. Independent permissions and profile capabilities determine tool registration. Bounded analytics find largest objects, prefix usage and abandoned uploads. Installers preserve other client entries and never store keys.

## Quick start

Use a platform binary from Releases once published, or build with Go 1.27.2:

```bash
go build -o selectel-s3 ./cmd/selectel-s3
./selectel-s3 version
./selectel-s3 auth set
./selectel-s3 auth status
./selectel-s3 ls --json
```

auth set reads the key ID and secret without terminal echo; non-TTY input supplies two lines. It atomically creates ~/.config/selectel-s3/credentials with mode 0600; broader permissions fail. Do not pass secrets in argv.

Resolution: flags > env > file. Variables: SELECTEL_S3_ACCESS_KEY_ID, SELECTEL_S3_SECRET_ACCESS_KEY, SELECTEL_S3_POOL (ru-1), SELECTEL_S3_ENDPOINT. HTTP requires SELECTEL_S3_ALLOW_INSECURE=1.

After publication: ghcr.io/stufently/selectel-s3-mcp-ai-cli:0.1.0. Build locally with docker build -t selectel-s3 .; the scratch image runs MCP as uid 65532 with CA certificates. Supply a private credentials environment/file setup.

## Connect to Claude Code, Cursor, Codex

```bash
selectel-s3 mcp install --client cursor --dry-run
selectel-s3 mcp install --client cursor
selectel-s3 mcp install --client claude-desktop
selectel-s3 mcp install --client codex
selectel-s3 mcp install --client claude-code --scope project
selectel-s3 mcp install --client claude-code --scope user
```

The final command only prints claude mcp add. Executable paths are absolute; secrets stay out of client configuration. Cursor uses ~/.cursor/mcp.json, Claude Code project scope ./.mcp.json, Desktop its OS-specific directory. JSON updates preserve foreign keys, create .bak and are idempotent.

Codex uses ~/.codex/config.toml. Existing tables require --force and must have generated # >>> selectel-s3 (managed) / # <<< selectel-s3 (managed) markers. Unmanaged tables are refused even with --force; other TOML bytes stay intact.

selectel-s3 mcp --allow-write --allow-delete --allow-admin or SELECTEL_S3_ALLOW=write,delete,admin enables independent levels. These flags also work for mcp install. File tools require SELECTEL_S3_FILE_ROOT.

## MCP tools

| Permission | Tools |
|---|---|
| read | s3_list_buckets, s3_list_objects, s3_head_object, s3_get_object, s3_preview_object, s3_presign_get, s3_list_object_versions, s3_list_multipart_uploads, s3_list_parts, s3_get_bucket_config, s3_largest_objects, s3_prefix_usage, s3_unfinished_uploads |
| read + FILE_ROOT | s3_download_file |
| write | s3_put_object, s3_create_folder, s3_copy_object, s3_presign_put, s3_multipart_create, s3_multipart_presign_part, s3_multipart_complete |
| write + FILE_ROOT | s3_upload_file |
| delete | s3_delete_object, s3_delete_objects, s3_multipart_abort |
| write + delete | s3_move_object |
| admin | s3_create_bucket, s3_put_bucket_versioning, s3_put_bucket_cors, s3_put_bucket_policy, s3_delete_bucket_policy, s3_put_object_lock_configuration |
| admin + delete | s3_delete_bucket |

Selectel tagging tools are never registered. Resources: selectel-s3://config, selectel-s3://capabilities, selectel-s3://pools.

Lists return at most 1000 items plus continuation tokens; versions/multipart have secondary markers. Inline get/put is limited to 262144 bytes; preview to 65536. Use presigned URLs or FILE_ROOT transfers for larger objects. Responses include structuredContent and JSON text; text over 256 KiB is summarized. Errors have code/message/retryable; the SDK adaptively retries 429/503.

Analytics scan max_scan entries (default 10000), marking partial results and tokens. Prefix aggregates page via next_result_offset. They are not billing estimates.

## CLI

```bash
selectel-s3 mb backups
selectel-s3 cp ./archive.tar s3://backups/daily/archive.tar --part-size 5242880
selectel-s3 ls backups/daily/ --max-keys 100 --json
selectel-s3 stat backups/daily/archive.tar
selectel-s3 cp s3://backups/daily/archive.tar ./restored.tar
selectel-s3 cp ./directory s3://backups/tree/ --recursive
selectel-s3 cp s3://backups/tree/ ./restored-directory --recursive
selectel-s3 cp s3://backups/daily/archive.tar s3://backups/copy.tar
selectel-s3 cat backups/daily/notes.txt
selectel-s3 versions backups/daily/ --json
selectel-s3 du backups/daily/ --max-scan 10000 --json
selectel-s3 presign backups/daily/archive.tar --expires 3600
selectel-s3 mv s3://backups/copy.tar s3://backups/moved.tar --yes
selectel-s3 rm backups/moved.tar --yes
selectel-s3 rb empty-bucket --yes
selectel-s3 doctor --json
selectel-s3 compat --markdown
```

cp uses ordinary local paths and s3://bucket/key or selectel-s3://bucket/key URIs. Large uploads use manager/multipart with configurable part size. --recursive copies directories/prefixes; overwrites require --overwrite and approval. Downloads are atomic. rm/rb/mv without --yes exit 2 on non-TTY input; terminals prompt. --json is global; errors use stderr and nonzero exits.

doctor checks credential source/permissions, DNS/TLS, Date-header clock skew and ListBuckets. Skew >5 minutes warns; >15 minutes fails.

## Agent skill

```bash
selectel-s3 skill install
selectel-s3 skill install --target ./agent-skills/selectel-s3
```

Default: ~/.claude/skills/selectel-s3. Idempotently installs SKILL.md and six references: backup/restore, sharing, least privilege, cost hygiene, migration and incidents. Triggers include selectel s3, presigned and Russian bucket/upload/backup requests.

## Security

Read is the MCP default. Missing capabilities/permissions remove tools. SELECTEL_S3_DESTRUCTIVE_POLICY=confirm uses client form elicitation when available, otherwise confirm:true after human approval. elicit requires elicitation and cannot be bypassed with confirm:true. block always refuses; allow proceeds.

Gates precede all S3 requests for deletion, move, signed PUT, policy/Object Lock, versioning suspension and overwrite. put/copy/move/multipart complete/upload file default to overwrite:false with target HEAD and If-None-Match: *. overwrite:true uses the same gate.

SELECTEL_S3_ALLOWED_BUCKETS and SELECTEL_S3_ALLOWED_PREFIXES take comma-separated buckets and bucket/prefix/ entries. Prefixes respect segment boundaries and reject '..'. Partial bucket scope denies bucket administration and broad listings even when the bucket is also listed in ALLOWED_BUCKETS. Copy/move checks both sides.

FILE_ROOT MCP tools use os.Root and relative paths only; absolute paths, '..' and escaping symlinks fail. Every tool call emits one JSON stderr audit record; SELECTEL_S3_AUDIT_FILE additionally appends to a 0600 file. Secrets and X-Amz-Signature/Credential/Security-Token are redacted from logs/errors. Full signed URLs are operation results only. Keep underlying S3 key rights minimal too.

## Selectel pools and storage classes

ru-1/ru-3: Saint Petersburg; ru-6 (Multi-AZ), ru-7, isolated gis-1: Moscow; uz-2: Uzbekistan. SigV4 region equals pool; addressing defaults to path-style. [Endpoints and compatibility](docs/compat.md).

Standard, cold and ice; ice only in ru-3. Select the bucket class in the panel at creation; it cannot later change, and create_bucket does not accept a class. Prices from 2.56/1.25/0.89 RUB/GB/month are the owner's unverified 2026-10 summary. The 604800-second presigned ceiling is unverified; documentation says a request signature lasts 15 minutes.

Objects: 50 TB; single PUT: 5 GB; multipart: 10000 parts. Per-bucket requests/second: total 2000, GET+HEAD 1000, POST 200, PUT/DELETE 300 each; excess yields 429. New-object TTL is configured through panel bucket limits.

## FAQ

**What is an MCP server?** A program exposes agent tools/resources through Model Context Protocol; this server uses stdio.

**How do I connect Claude Code?** Save keys with auth set and use project install or the printed user command.

**Is agent S3 access safe?** Restrict keys, scope and permissions; retain confirm/block and deliberately approve destructive effects.

**How does it differ from aws-cli/rclone?** It combines MCP, gates, audit, analytics, skill and CLI; those tools remain independently useful for transfer/automation.

**Why no lifecycle or tags?** Selectel S3 does not support them. Configure new-object TTL in the panel; tagging tools are excluded.

Planned: Yandex, VK Cloud, Cloud.ru, Timeweb, MWS, Beget.

## License

[MIT](LICENSE). [Facts and assumptions](docs/research/selectel-s3.md). Docker checks: bash scripts/go.sh go test -race ./..., bash scripts/conformance.sh, bash scripts/mcp_smoke.sh, bash scripts/mutation_gate.sh, bash scripts/check_docs.sh. Conformance uses versitygw v1.8.0 and fake keys.
