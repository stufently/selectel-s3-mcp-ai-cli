# Changelog

## Unreleased

- Add provider-specific Russian/English discovery text, recovery examples, FAQ and links to all S3 MCP projects.
- Add social preview artwork and a shared bilingual provider directory.

- Remove internal working documents from the public tree while retaining Git history.
- Move shared MCP/CLI, safety and checks to s3-mcp-ai-cli-kit v0.1.0.
- Refuse unprotected default writes; gate approved overwrites and require conditional delete for move.

- 2026-10-10: v1 — один бинарь `selectel-s3`: MCP-сервер (35 инструментов, уровни
  read/write/delete/admin, гейт опасных операций с подписанным подтверждением, скоуп по
  бакетам и префиксам, корень локальных файлов, аудит), CLI (`ls`, `cp`, `mv`, `rm`,
  `presign`, `doctor`, `mcp install` для Claude Code/Cursor/Codex/Claude Desktop) и skill.
- 2026-10-10: исправления по приёмке v1:
  - подтверждение в форме требует `approved: true`;
  - аргументы с ключами, совпадающими без учёта регистра, отклоняются до гейта;
  - явный `--allow-<уровень>=false` перебивает env и сохраняется в `mcp install`;
  - `mv` не удаляет источник, изменившийся во время переноса (ETag, `If-Match`);
  - ретрай HTTP 429;
  - рекурсивный `cp` читает только из выбранного каталога;
  - `s3_download_file` и `cp` не перезаписывают локальный файл без `overwrite` и подтверждения;
  - `mcp install` для Codex не ломает конфиг с явной таблицей `[mcp_servers]`.
- 2026-10-10: каркас репо, исследование Selectel S3 (`docs/research/selectel-s3.md`).
