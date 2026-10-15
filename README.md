# Selectel S3 MCP Server & AI CLI

![Status](https://img.shields.io/badge/status-development-blue) ![License](https://img.shields.io/badge/license-MIT-green)

MCP-сервер и CLI для S3 Селектел (Selectel Object Storage): подключение к Claude Code, Cursor и Codex, просмотр бакетов, восстановление бэкапов и работа с файлами через Go-бинарь `selectel-s3`.

[English guide](README.en.md) · [Каталог S3 MCP-серверов](https://stufently.github.io/s3-mcp-ai-cli-kit/)

## English

A Go MCP server, CLI and agent skill for Selectel S3 Object Storage. Connect Claude Code, Cursor and Codex to scoped object storage for inspection and backup recovery. Six profile pools; tagging and lifecycle tools are disabled. See the [full English guide](README.en.md).

## Возможности

- Object CRUD, серверные copy/move, версии, multipart, presigned GET/PUT.
- CORS, политика бакета и Object Lock configuration; регистрация по возможностям профиля.
- Ограниченные сканы: крупные объекты, использование префиксов, брошенные загрузки.
- Потоковый CLI, JSON, диагностика, установка MCP-конфигураций и встроенного skill.

## Быстрый старт

Возьми бинарь своей платформы из [Releases](https://github.com/stufently/selectel-s3-mcp-ai-cli/releases/latest) (v0.1.0 опубликован; есть в MCP Registry и Smithery) либо собери с Go 1.27.2:

```bash
go build -o selectel-s3 ./cmd/selectel-s3
./selectel-s3 version
./selectel-s3 auth set
./selectel-s3 auth status
./selectel-s3 ls --json
```

auth set спрашивает key ID и secret без эха; без TTY читает две строки stdin. Не передавай секрет в argv. Файл ~/.config/selectel-s3/credentials создаётся атомарно с 0600; более широкие права отклоняются.

Приоритет: флаги > env > файл. Переменные: SELECTEL_S3_ACCESS_KEY_ID, SELECTEL_S3_SECRET_ACCESS_KEY, SELECTEL_S3_POOL (ru-1), SELECTEL_S3_ENDPOINT. HTTP требует SELECTEL_S3_ALLOW_INSECURE=1.

Образ: ghcr.io/stufently/selectel-s3-mcp-ai-cli:0.1.0; локальная сборка: docker build -t selectel-s3 . Образ запускает MCP от uid 65532, содержит CA-сертификаты. Передай креды через приватное окружение/файл.

## Подключение к Claude Code, Cursor, Codex

[![Add selectel-s3 MCP server to Cursor](https://cursor.com/deeplink/mcp-install-dark.svg)](https://cursor.com/en/install-mcp?name=selectel-s3&config=eyJjb21tYW5kIjoiZG9ja2VyIiwiYXJncyI6WyJydW4iLCItaSIsIi0tcm0iLCItZSIsIlNFTEVDVEVMX1MzX0FDQ0VTU19LRVlfSUQiLCItZSIsIlNFTEVDVEVMX1MzX1NFQ1JFVF9BQ0NFU1NfS0VZIiwiLWUiLCJTRUxFQ1RFTF9TM19QT09MIiwiZ2hjci5pby9zdHVmZW50bHkvc2VsZWN0ZWwtczMtbWNwLWFpLWNsaTowLjEuMCJdLCJlbnYiOnsiU0VMRUNURUxfUzNfQUNDRVNTX0tFWV9JRCI6IiR7ZW52OlNFTEVDVEVMX1MzX0FDQ0VTU19LRVlfSUR9IiwiU0VMRUNURUxfUzNfU0VDUkVUX0FDQ0VTU19LRVkiOiIke2VudjpTRUxFQ1RFTF9TM19TRUNSRVRfQUNDRVNTX0tFWX0iLCJTRUxFQ1RFTF9TM19QT09MIjoiJHtlbnY6U0VMRUNURUxfUzNfUE9PTH0ifX0%3D)

Кнопка ставит MCP из Docker-образа `ghcr.io/stufently/selectel-s3-mcp-ai-cli:0.1.0` в режиме только чтения. Ключи в конфиг не пишутся: Cursor подставляет `${env:…}` из окружения, поэтому задай `SELECTEL_S3_ACCESS_KEY_ID` и `SELECTEL_S3_SECRET_ACCESS_KEY` там, откуда запускается Cursor; `SELECTEL_S3_POOL` — если бакет не в пуле по умолчанию (пустое значение = пул по умолчанию). Тот же блок — для Windsurf (`mcp_config.json`) и вручную для `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "selectel-s3": {
      "command": "docker",
      "args": [
        "run",
        "-i",
        "--rm",
        "-e",
        "SELECTEL_S3_ACCESS_KEY_ID",
        "-e",
        "SELECTEL_S3_SECRET_ACCESS_KEY",
        "-e",
        "SELECTEL_S3_POOL",
        "ghcr.io/stufently/selectel-s3-mcp-ai-cli:0.1.0"
      ],
      "env": {
        "SELECTEL_S3_ACCESS_KEY_ID": "${env:SELECTEL_S3_ACCESS_KEY_ID}",
        "SELECTEL_S3_SECRET_ACCESS_KEY": "${env:SELECTEL_S3_SECRET_ACCESS_KEY}",
        "SELECTEL_S3_POOL": "${env:SELECTEL_S3_POOL}"
      }
    }
  }
}
```

```bash
selectel-s3 mcp install --client cursor --dry-run
selectel-s3 mcp install --client cursor
selectel-s3 mcp install --client claude-desktop
selectel-s3 mcp install --client codex
selectel-s3 mcp install --client claude-code --scope project
selectel-s3 mcp install --client claude-code --scope user
```

Последняя команда только печатает claude mcp add. Путь бинаря абсолютный; ключи в конфиг не пишутся. Cursor: ~/.cursor/mcp.json, Claude Code project: ./.mcp.json; Desktop — каталог конфигурации своей ОС. JSON сохраняет чужие ключи, .bak и идемпотентность.

Codex: ~/.codex/config.toml. Существующая таблица требует --force; заменяется только блок между # >>> selectel-s3 (managed) / # <<< selectel-s3 (managed). Таблица без маркеров не заменяется даже с --force; остальные байты TOML целы.

Уровни включаются независимо: selectel-s3 mcp --allow-write --allow-delete --allow-admin либо SELECTEL_S3_ALLOW=write,delete,admin. Флаги доступны и в mcp install. FILE_ROOT включается через SELECTEL_S3_FILE_ROOT.

## Инструменты MCP

| Уровень | Инструменты |
|---|---|
| read | s3_list_buckets, s3_list_objects, s3_head_object, s3_get_object, s3_preview_object, s3_presign_get, s3_list_object_versions, s3_list_multipart_uploads, s3_list_parts, s3_get_bucket_config, s3_largest_objects, s3_prefix_usage, s3_unfinished_uploads |
| read + FILE_ROOT | s3_download_file |
| write | s3_put_object, s3_create_folder, s3_copy_object, s3_presign_put, s3_multipart_create, s3_multipart_presign_part, s3_multipart_complete |
| write + FILE_ROOT | s3_upload_file |
| delete | s3_delete_object, s3_delete_objects, s3_multipart_abort |
| write + delete | s3_move_object |
| admin | s3_create_bucket, s3_put_bucket_versioning, s3_put_bucket_cors, s3_put_bucket_policy, s3_delete_bucket_policy, s3_put_object_lock_configuration |
| admin + delete | s3_delete_bucket |

Теги у Selectel не регистрируются. Ресурсы: selectel-s3://config, selectel-s3://capabilities, selectel-s3://pools.

Списки — до 1000 элементов с continuation_token; версии/multipart имеют второй маркер. Inline get/put — 262144 байт, preview — 65536; большие файлы идут через presigned URL или FILE_ROOT. Ответ содержит structuredContent и JSON-текст; текст свыше 256 КиБ заменяется краткой сводкой. Ошибки: code/message/retryable; 429/503 SDK повторяет адаптивно.

Аналитика сканирует максимум max_scan (по умолчанию 10000); partial/token отмечают неполный результат. Prefix usage выдаёт страницы агрегатов через next_result_offset. Это не биллинг.

## Сценарий: Выбор пула и восстановление резервных копий

Выберите пул существующего бакета и проверьте архив перед восстановлением. `--pool` меняет и endpoint, и регион подписи; для московского пула `ru-6` пример выглядит так:

```bash
selectel-s3 --pool ru-6 ls backups/daily/ --json
selectel-s3 --pool ru-6 stat backups/daily/archive.tar
selectel-s3 --pool ru-6 cp s3://backups/daily/archive.tar ./restored-selectel.tar
```

Примеры используют существующий бакет `backups` и объект `daily/archive.tar`; замените их своими значениями. Профиль и API облака ещё требуют [живой проверки](TASKS.md).

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

cp принимает обычные локальные пути и S3 URI s3://bucket/key либо selectel-s3://bucket/key. Большие загрузки используют manager/multipart, --recursive копирует каталоги/префиксы. Перезапись требует --overwrite и подтверждения; выгрузка атомарна. rm/rb/mv без --yes на не-TTY дают код 2; на TTY спрашивают. --json общий, ошибки — stderr и ненулевой код.

doctor проверяет источник/права кредов, DNS/TLS, часы по Date и ListBuckets: >5 минут — предупреждение, >15 — ошибка.

## Skill для AI-агентов

```bash
selectel-s3 skill install
selectel-s3 skill install --target ./agent-skills/selectel-s3
```

По умолчанию ~/.claude/skills/selectel-s3. Идемпотентно устанавливает SKILL.md и шесть references: backup/restore, share, least privilege, cost hygiene, migration, incident response. Триггеры: «selectel s3», «бакет селектел», «загрузи в s3», «presigned», «бэкап в s3».

## Безопасность

MCP по умолчанию регистрирует только read; неподдерживаемые возможности и отсутствующие уровни исключают инструмент.

SELECTEL_S3_DESTRUCTIVE_POLICY: confirm использует elicitation при поддержке клиента, иначе нужен confirm:true после согласия человека; elicit требует elicitation и не допускает обхода confirm:true; block всегда запрещает, allow выполняет без гейта. Отказ предшествует любым S3-запросам. Гейт покрывает удаления, move, presigned PUT, политику/Object Lock, suspension versioning и перезаписи.

put/copy/move/multipart complete/upload file по умолчанию имеют overwrite:false: HEAD цели и If-None-Match: *. overwrite:true требует тот же гейт.

Скоуп: SELECTEL_S3_ALLOWED_BUCKETS=backups и SELECTEL_S3_ALLOWED_PREFIXES=backups/daily/. Префикс учитывает границу сегмента; daily-evil не подходит. Сегменты '..' отклоняются. Частичный скоуп запрещает целый бакет и широкие списки даже при наличии его в ALLOWED_BUCKETS. Copy/move проверяет источник и цель.

Файловые MCP-инструменты используют os.Root: только относительные пути внутри FILE_ROOT; '..', абсолютные пути и симлинки наружу отклоняются. На каждый вызов одна JSON-строка в stderr; SELECTEL_S3_AUDIT_FILE дополнительно пишет в 0600. Секреты и X-Amz-Signature/Credential/Security-Token редактируются в логах/ошибках. Полная подписанная ссылка — только результат операции. Ограничивай также права самого S3-ключа.

s3_download_file по умолчанию публикует скачанный файл без замены существующего, в том числе созданного во время скачивания. Для замены передай overwrite:true: применяется разрушительная политика с подтверждением через форму либо confirm:true после согласия человека.

## Пулы и классы хранения Selectel

ru-1 и ru-3 — Санкт-Петербург; ru-6 (Multi-AZ), ru-7, gis-1 (изолированный ГИС) — Москва; uz-2 — Узбекистан. Регион SigV4 равен пулу; path-style по умолчанию. [Эндпоинты и совместимость](docs/compat.md).

Стандартное, холодное, ледяное; ледяное только ru-3. Класс выбирается в панели при создании и потом не меняется; create_bucket не принимает класс. Цены от 2,56 / 1,25 / 0,89 ₽/ГБ·мес — непроверенная сводка владельца за 2026-10. Максимум presigned 604800 секунд тоже не подтверждён; документация говорит о 15 минутах срока подписи запроса.

Объект до 50 ТБ, один PUT до 5 ГБ, multipart до 10000 частей. На бакет запросов/с: всего 2000, GET+HEAD 1000, POST 200, PUT и DELETE по 300; превышение даёт 429. TTL задаётся в панели и применяется к новым объектам.

## FAQ

**Как выбрать регион Selectel S3 для MCP?** Укажите `SELECTEL_S3_POOL` или флаг `--pool` с идентификатором из [таблицы совместимости](docs/compat.md). Пул должен совпадать с пулом вашего бакета; `ru-1` — значение по умолчанию.

**Можно ли настроить lifecycle и теги Селектел через этот MCP-сервер?** В профиле проекта эти возможности выключены, поэтому соответствующие инструменты не регистрируются. Настройки TTL через панель описаны в [ресерче Selectel](docs/research/selectel-s3.md).

**Как восстановить бэкап из Selectel S3?** Используйте `stat`, затем `cp s3://bucket/key ./new-file`. CLI скачивает потоково и публикует локальный файл атомарно; новый путь позволяет избежать замены существующего архива.

**Что такое MCP-сервер?** Программа предоставляет агентам инструменты и ресурсы через Model Context Protocol; здесь stdio.

**Как подключить MCP-сервер к Claude Code?** Сохрани ключи auth set, затем используй project install либо напечатанную user-команду.

**Безопасно ли давать агенту доступ к S3?** Ограничь ключи, скоуп и уровни; сохрани confirm/block и одобряй разрушительные эффекты осознанно.

**Чем это отличается от aws-cli и rclone?** Бинарь объединяет MCP, гейт, аудит, аналитику, skill и CLI; aws-cli/rclone остаются самостоятельными средствами передачи/автоматизации.

**Почему нет lifecycle и тегов?** Selectel S3 API их не поддерживает. TTL новых объектов — в панели; теги исключены профилем.

## Другие провайдеры

[Общий каталог: MCP-серверы и CLI для российских S3](https://stufently.github.io/s3-mcp-ai-cli-kit/).

| Провайдер | Репозиторий | Сценарий |
|---|---|---|
| Selectel | [selectel-s3-mcp-ai-cli](https://github.com/stufently/selectel-s3-mcp-ai-cli) | Выбор пула и восстановление резервных копий |
| Yandex Cloud | [yandex-s3-mcp-ai-cli](https://github.com/stufently/yandex-s3-mcp-ai-cli) | Инвентаризация объектов и проверка версий бэкапов |
| VK Cloud | [vkcloud-s3-mcp-ai-cli](https://github.com/stufently/vkcloud-s3-mcp-ai-cli) | Проверка Hotbox/Icebox и доступ к нужному региону |
| Cloud.ru | [cloudru-s3-mcp-ai-cli](https://github.com/stufently/cloudru-s3-mcp-ai-cli) | Диагностика доступа к Evolution Object Storage |
| Timeweb Cloud | [timeweb-s3-mcp-ai-cli](https://github.com/stufently/timeweb-s3-mcp-ai-cli) | Восстановление архивов и учёт объёма префикса |
| MWS | [mws-s3-mcp-ai-cli](https://github.com/stufently/mws-s3-mcp-ai-cli) | Проверка HMAC-доступа и короткие ссылки на архивы |
| Beget | [beget-s3-mcp-ai-cli](https://github.com/stufently/beget-s3-mcp-ai-cli) | Просмотр и восстановление файлов из готового бакета |
| Shared Go kit | [s3-mcp-ai-cli-kit](https://github.com/stufently/s3-mcp-ai-cli-kit) | Общие MCP, CLI, защиты и CI |

## Лицензия

[MIT](LICENSE). [Проверенные факты и предположения](docs/research/selectel-s3.md).

Проверки в Docker: bash scripts/go.sh go test -race ./..., bash scripts/conformance.sh, bash scripts/mcp_smoke.sh, bash scripts/mutation_gate.sh, bash scripts/check_docs.sh. Conformance использует versitygw v1.8.0 и тестовые ключи.

Общие MCP/CLI, защиты и проверки поставляет [s3-mcp-ai-cli-kit v0.1.0](https://github.com/stufently/s3-mcp-ai-cli-kit/tree/v0.1.0); в этом репозитории остаются встроенный профиль, тонкий main и документы Selectel.
