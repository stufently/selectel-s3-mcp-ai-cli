# Журнал работ

## IN_PROGRESS

- Проверка на живом Selectel перед первым релизом: presign max, storage class при put,
  поведение `If-None-Match`/`If-Match` и ответ на превышение лимита (сейчас — только эмулятор versitygw).
- Вынести внутреннюю кухню (спеки вех, отчёты мутаций) из публичного репо — `docs/spec-queue.md`, п. 4.

## COMPLETED

- 2026-10-10 — веха v1 «Selectel S3 MCP + AI CLI» и три захода исправлений по приёмке
  (`docs/specs/v1-selectel.md`, `v1-fix1.md`, `v1-fix2.md`, `v1-fix3.md`). Код — Codex;
  ревью — Codex и agy; независимые мутации — Grok (50 → 36 → 28 мутантов, выжившие
  закрыты тестами). Итог на `main`: ворота 77/77, conformance против versitygw,
  goreleaser 6 архивов, образ не от root.
