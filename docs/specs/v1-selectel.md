# Веха v1: Selectel S3 MCP-сервер + AI CLI + skill

## 1. Шапка

- Репозиторий: `/home/deploy/github/selectel-s3-mcp-ai-cli` (публичный будущий
  `github.com/stufently/selectel-s3-mcp-ai-cli`), ветка `main`.
- Дата: 2026-10-10.
- BASE_SHA: `a3dcfc82f3fe847a056ebee561fabc1ef54a8814` — «Scaffold repo and Selectel S3 research».
- Исполнитель: **Codex (`cx`)** — прямое указание владельца для этого проекта.
- Ревьюеры по `cross-review-v1` для Codex: `agy` + `grok`.
- Размер: весь v1 одной вехой — осознанное решение владельца (обычный лимит вехи
  превышен намеренно). Работай последовательно по разделу 5, коммить по шагам.

## 2. Где работать

- Только одноразовый клон, который выдал постановщик (путь — в первом сообщении панели).
  Новая ветка: `v1-selectel` от BASE_SHA.
- Живое дерево `/home/deploy/github/selectel-s3-mcp-ai-cli` не трогать.
- **push в origin запрещён**, работу заберёт постановщик через `git fetch` из клона.
- Эта спека приезжает в клон untracked: `docs/specs/v1-selectel.md`. **Закоммить её
  вместе с работой** (первым коммитом).
- Всё выполняется в Docker под текущим uid (`-u "$(id -u):$(id -g)"`), ничего не ставить
  на хост. Кэш Go — каталог `$HOME/.cache/sel-s3-go` (постановщик создал, владелец — ты).
- Образы уже скачаны: `golang:1.27.2`, `golangci/golangci-lint:v2.14.0`,
  `goreleaser/goreleaser:v2.18.3`, `rhysd/actionlint:1.7.12`, `versity/versitygw:v1.8.0`.
  В `golang:1.27.2` **нет `curl` и `git`** (проверено) — тесты не должны их требовать.
- Сеть к proxy.golang.org есть: зависимости Go качай обычным `go mod tidy`.

## 3. Задача и почему

Публичный open-source проект: один Go-бинарь `selectel-s3`, который одновременно
- **MCP-сервер** (stdio) для AI-агентов (Claude Code, Cursor, Codex, Claude Desktop);
- **CLI** для людей и скриптов;
- **skill** (SKILL.md-плейбуки) для агентов, ставится командой бинаря.

Это первый из семи провайдерских репо (Selectel, Yandex, VK Cloud, Cloud.ru, Timeweb,
MWS, Beget). Функционал у всех будет одинаковый, поэтому **всё провайдерское живёт
только в `internal/provider/`**; остальной код провайдер-нейтрален и позже переедет
в общий модуль. Образец идей — `backblaze-labs/b2-mcp` (гейт опасных операций,
байты мимо модели, регистрация по возможностям), плюс уровни доступа как в MinIO
AIStor MCP, аудит каждого вызова как в XNS, скоуп бакетов/префиксов как в quickS3.
Факты о Selectel — `docs/research/selectel-s3.md` (прочитай целиком до начала).

## 4. Что проверено вживую и что предположение

Проверено (команды и вывод — в `docs/research/selectel-s3.md`):
- эндпоинты пулов, таблица совместимости S3 API, лимиты, классы хранения Selectel;
- versitygw v1.8.0 умеет: бакеты, versioning, теги объекта, бакет с Object Lock, CORS,
  политика, multipart, presigned GET; регион шлюза надо задавать `VGW_REGION=ru-1`,
  иначе presigned даёт `AuthorizationQueryParametersError`; бакеты короче 3 символов
  отвергаются;
- go-sdk v1.8.0: `mcp.AddTool[In, Out]`, `(*Server).AddResource`, `(*ServerSession).Elicit`,
  `ClientCapabilities.Elicitation`, `mcp.NewInMemoryTransports` (чтение исходников тега);
- версии всех зависимостей и образов — таблица в research-файле;
- схема MCP Registry: `https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json`,
  sha256 `3fba09590c99f61735d234822279f4223fab9e300c0a81e81c91ab62a4114de0`.

Предположения (не проверены, код обязан это учитывать):
- P1. Поддерживает ли versitygw условную запись `If-None-Match: *` — неизвестно.
  Если нет — заголовок проверяется unit-тестом через `httptest`, а conformance-кейс
  заносится в `conformance/SKIPS.md` с дословной ошибкой шлюза. Других пропусков нет.
- P2. Потолок срока presigned у Selectel неизвестен (в доке «срок жизни подписи 15 минут»).
  В профиле: `presign_max_seconds: 604800`, `verified: false` с этим примечанием.
- P3. Класс хранения через S3 API при создании бакета не задаётся. В профиле класс
  описан, но `s3_create_bucket` его не принимает; в описании инструмента сказано,
  что класс выбирается в панели.
- P4. Цены в профиле — из сводки владельца, `verified: false`, с датой `2026-10`.

Утверждений о существующем коде нет: в репо пока только документы.

## 5. Что сделать

### 5.1 Каркас

- `go.mod`: модуль `github.com/stufently/selectel-s3-mcp-ai-cli`, `go 1.27`.
  Зависимости — версии из research-таблицы (новее можно, старее нельзя; итоговые версии
  перечисли в `note` критерия сборки).
- Раскладка:
  ```
  cmd/selectel-s3/main.go     # тонкий: собирает provider + cli
  internal/provider/          # ЕДИНСТВЕННОЕ место с Selectel: profile.yaml (go:embed), profile.go
  internal/config/            # флаги > env > файл кредов; профиль
  internal/s3core/            # клиент aws-sdk-go-v2 и операции
  internal/safety/            # уровни доступа, гейт, скоуп, fs-guard, редактирование, аудит
  internal/mcpserver/         # регистрация инструментов и ресурсов (go-sdk)
  internal/analytics/         # largest, prefix usage, unfinished uploads
  internal/cli/               # cobra-команды
  internal/install/           # mcp install, skill install
  skills/                     # embed.go + selectel-s3/SKILL.md + references/*.md
  conformance/                # тесты против versitygw (build tag conformance)
  testdata/                   # golden-файлы, server.schema.json
  scripts/                    # conformance.sh, mcp_smoke.sh, mutation_gate.sh, check_docs.sh
  ```
- Версия `0.1.0` через `-ldflags -X`; `selectel-s3 version` печатает `selectel-s3 <версия> (<commit>)`.

### 5.2 Профиль провайдера (`internal/provider/profile.yaml`)

Поля: `id: selectel`, `display_name: Selectel`, `env_prefix: SELECTEL_S3_`,
`uri_scheme: selectel-s3`, `binary: selectel-s3`, `default_pool: ru-1`, `path_style: true`,
`pools[]` (id, endpoint, location, multi_az, storage_classes), `storage_classes[]`
(id, name_ru, price_rub_gb_month, verified, checked_at), `features` (булевы:
`versioning, object_lock_config, object_retention, object_tagging, bucket_tagging,
lifecycle, cors, bucket_policy, conditional_writes, multipart, presign, acl`) строго
по таблице совместимости research-файла, `limits` (max_put_bytes, max_parts,
rate_limits), `presign_max_seconds: 604800` + `verified: false` (значение — из P2 раздела 4), `docs_urls`. Всё остальное — из research-файла,
ничего не выдумывать. Нейтральный код читает провайдера только через тип `Profile`.

### 5.3 Конфигурация и креды

- `SELECTEL_S3_ACCESS_KEY_ID`, `SELECTEL_S3_SECRET_ACCESS_KEY`, `SELECTEL_S3_POOL`
  (по умолчанию `ru-1`), `SELECTEL_S3_ENDPOINT` (переопределение, для тестов),
  `SELECTEL_S3_ALLOW_INSECURE=1` (разрешает `http://`; без него `http` — отказ).
  Регион подписи = пул.
- Файл кредов `~/.config/selectel-s3/credentials` (формат `KEY=VALUE`, те же имена).
  Права шире `0600` → отказ с понятной ошибкой. Порядок: флаги > env > файл.
- `selectel-s3 auth set` читает ключи из stdin без эха (без TTY — построчно из stdin),
  пишет файл атомарно с `0600`; `auth status` печатает источник и отпечаток
  (первые 4 символа key id + `…`), секрет — никогда.
- Секрет нигде не попадает в argv, вывод, логи, ответы MCP, конфиги клиентов.
- ⚠️ Имена выше — для профиля Selectel. В коде вне `internal/provider` они **не пишутся
  литералами**: имя переменной = `profile.env_prefix` + суффикс (`ACCESS_KEY_ID`, `POOL`, …),
  каталог кредов = `~/.config/<profile.binary>/`, схема URI = `profile.uri_scheme`,
  имя сервера в конфигах клиентов = `profile.binary`. Иначе не пройдёт критерий изоляции.
  Это касается и env из разделов 5.4–5.5.

### 5.4 Ядро S3 и инструменты MCP

Все имена инструментов — с префиксом `s3_`. Ответ: `structuredContent` + краткий JSON в
тексте; списки до 1000 элементов с токеном продолжения; текст ответа до 256 КиБ.
Инлайн-данные объекта (get/put) — **до 256 КиБ** (`max_bytes`, по умолчанию и потолок 262144);
больше — отказ с подсказкой взять `s3_presign_get`/`s3_presign_put`.
Ошибки S3 → `{code, message, retryable}`; 429/503 — ретраи SDK с адаптивным бэкоффом.

Уровни доступа — независимые флаги (env `SELECTEL_S3_ALLOW=write,delete,admin`, флаги
`mcp --allow-write --allow-delete --allow-admin`). Без флагов — только чтение.
Инструмент без нужного уровня **не регистрируется**. Инструмент с `cap:<feature>`
регистрируется только если `features.<feature>: true`. Инструмент с `fileroot`
регистрируется только при заданном `SELECTEL_S3_FILE_ROOT`. «G» — под гейтом (5.5).

| Инструмент | Уровень | Условие | G |
|---|---|---|---|
| s3_list_buckets | read | | |
| s3_list_objects | read | | |
| s3_head_object | read | | |
| s3_get_object (≤256 КиБ, text/base64) | read | | |
| s3_preview_object (первые ≤64 КиБ, тип, заголовок CSV) | read | | |
| s3_presign_get (по умолчанию 3600 с, потолок из профиля) | read | | |
| s3_list_object_versions | read | cap:versioning | |
| s3_list_multipart_uploads | read | | |
| s3_list_parts | read | | |
| s3_get_bucket_config (location, versioning, cors, policy, object lock — каждое «нет/не поддерживается») | read | | |
| s3_largest_objects (скан до `max_scan`, по умолчанию 10000) | read | | |
| s3_prefix_usage (объём/число по префиксам первого уровня, ограниченный скан) | read | | |
| s3_unfinished_uploads (старше N часов) | read | | |
| s3_get_object_tagging | read | cap:object_tagging | |
| s3_download_file (в локальный путь внутри корня) | read | fileroot | |
| s3_put_object (≤256 КиБ; правило перезаписи ниже) | write | | частично |
| s3_create_folder | write | | |
| s3_copy_object (серверная копия; перезапись → G) | write | | частично |
| s3_presign_put | write | | G |
| s3_multipart_create / s3_multipart_presign_part / s3_multipart_complete (complete — правило перезаписи) | write | cap:multipart | частично |
| s3_put_object_tagging | write | cap:object_tagging | |
| s3_upload_file (локальный файл внутри корня, multipart через manager; правило перезаписи) | write | fileroot | частично |
| s3_delete_object (опц. version_id) | delete | | G |
| s3_delete_objects (≤1000) | delete | | G |
| s3_move_object | write+delete | | G |
| s3_multipart_abort | delete | cap:multipart | G |
| s3_create_bucket (класс не задаётся, см. P3) | admin | | |
| s3_delete_bucket (только пустой) | admin+delete | | G |
| s3_put_bucket_versioning (`Suspended` → G) | admin | cap:versioning | частично |
| s3_put_bucket_cors | admin | cap:cors | |
| s3_put_bucket_policy / s3_delete_bucket_policy | admin | cap:bucket_policy | G |
| s3_put_object_lock_configuration | admin | cap:object_lock_config | G |

Для профиля Selectel теги (`object_tagging: false`) не регистрируются никогда.

**Правило перезаписи** (общее для `s3_put_object`, `s3_copy_object`, `s3_move_object`,
`s3_multipart_complete`, `s3_upload_file`): аргумент `overwrite` (по умолчанию `false`).
- `overwrite:false` — если профиль `conditional_writes: true`, запрос идёт с
  `If-None-Match: *`; дополнительно, до записи, `HeadObject` цели: объект существует →
  отказ `already_exists` (у `s3_multipart_complete` проверка до `CompleteMultipartUpload`).
- `overwrite:true` — операция идёт под гейт с той же политикой
  `SELECTEL_S3_DESTRUCTIVE_POLICY` (при `block` — отказ, при `confirm` — подтверждение и т. д.).
Обойти гейт перезаписью через любой из пяти инструментов нельзя; это проверяется тестами.

Ресурсы: `selectel-s3://config` (несекретная конфигурация: пул, эндпоинт, уровни, политика
гейта, скоуп, корень файлов, версия), `selectel-s3://capabilities` (features профиля +
список зарегистрированных инструментов), `selectel-s3://pools` (пулы, классы, цены с
`verified`). Схема URI — из `profile.uri_scheme`.

### 5.5 Безопасность (`internal/safety`)

- **Гейт** `SELECTEL_S3_DESTRUCTIVE_POLICY=confirm|elicit|block|allow`, по умолчанию `confirm`:
  - `confirm`: клиент умеет elicitation → форма подтверждения (accept → выполнить,
    decline/cancel → отказ); не умеет → нужен аргумент `confirm: true`, иначе отказ с
    описанием эффекта, адресованным человеку;
  - `elicit`: только elicitation; клиент не умеет → отказ; `confirm: true` не помогает;
  - `block`: отказ всегда; `allow`: без гейта.
  Отказ гейта — до любого запроса к S3: unit-тест на `httptest`-сервере проверяет, что
  при отказе сервер получил **ноль** запросов (не только что объект на месте).
- **Скоуп** `SELECTEL_S3_ALLOWED_BUCKETS` (через запятую) и `SELECTEL_S3_ALLOWED_PREFIXES`
  (`bucket/prefix/`, через запятую). Проверка на КАЖДОМ инструменте до запроса;
  `s3_list_buckets` фильтрует; префикс сравнивается по границе сегмента
  (`backups` не пускает в `backups-evil/`); ключи с `..`-сегментами — отказ.
  Копирование/перемещение проверяет и источник, и цель.
  Если для бакета заданы префиксы, бакет считается **частично разрешённым**:
  операции над бакетом целиком (`s3_get_bucket_config`, все `s3_put_bucket_*`,
  `s3_delete_bucket_policy`, `s3_put_object_lock_configuration`, `s3_delete_bucket`,
  `s3_create_bucket`) — отказ; списки (`s3_list_objects`, `s3_list_object_versions`,
  `s3_list_multipart_uploads`, аналитика) без префикса или с префиксом шире разрешённого —
  отказ с перечнем разрешённых префиксов; presign — только для ключей внутри префикса.
  Бакет из `ALLOWED_BUCKETS` без префиксов — разрешён целиком. Пустые оба списка — без
  ограничений.
- **Корень файлов**: без `SELECTEL_S3_FILE_ROOT` файловые инструменты не регистрируются;
  с ним все файловые операции идут **только через `os.Root`** (`os.OpenRoot(fileRoot)`,
  затем `root.Open/OpenFile/Create/Rename`) — это закрывает гонку «проверили путь, а
  потом подменили каталог симлинком». Ручной резолв путей вне `os.Root` запрещён;
  абсолютные пути и `..` в аргументе — отказ до обращения к ФС. Запись — во временный
  файл внутри корня и `root.Rename`. Тест: симлинк внутри корня, указывающий наружу, —
  отказ и на чтение, и на запись.
- **Аудит**: на каждый вызов инструмента одна JSON-строка в stderr (`slog`):
  `ts, tool, s3_action, bucket, key|prefix, outcome, gated, duration_ms`.
  `SELECTEL_S3_AUDIT_FILE` — дополнительно дописывать в файл (`0600`, создаётся так).
- **Редактирование**: в логах и ошибках нет секрета; в presigned URL в логах вырезаны
  `X-Amz-Signature`, `X-Amz-Credential`, `X-Amz-Security-Token` (сама ссылка уходит
  только в ответ инструмента).

### 5.6 CLI (`cobra`)

`selectel-s3 ls [bucket[/prefix]] | stat | cat | cp | mv | rm | presign | du | mb | rb |
versions | doctor | auth set|status | compat --markdown | mcp | mcp install | skill install | version`.
- Общий флаг `--json`; ошибки — ненулевой код и сообщение в stderr.
- `cp`: локальный↔S3 и S3↔S3; большие файлы — multipart через manager (размер части
  флагом); `--recursive` для каталогов.
- `rm`/`rb`/`mv`: без `--yes` на не-TTY — отказ с кодом 2; на TTY — вопрос.
- `doctor`: источник кредов и права файла, DNS/TLS эндпоинта, расхождение часов по
  заголовку `Date` (предупреждение > 5 мин, ошибка > 15 мин — подпись Selectel живёт
  15 мин), `ListBuckets`; `--json`. Только чтение.
- `compat --markdown`: таблица features/пулов/классов из профиля → `docs/compat.md`.
- `mcp install --client cursor|claude-desktop|codex|claude-code [--scope user|project]
  [--dry-run] [--force] [--allow-write …]`: пишет в конфиг клиента команду
  (абсолютный путь бинаря, аргумент `mcp`, несекретные env/флаги). **Секретов в конфиг
  не пишет никогда** (креды — из файла кредов). JSON (cursor `~/.cursor/mcp.json`,
  claude-desktop — путь по ОС, claude-code `--scope project` → `./.mcp.json`):
  сохранить чужие ключи, атомарная запись, `.bak` рядом, идемпотентно.
  codex `~/.codex/config.toml`: если таблица `[mcp_servers.selectel-s3]` уже есть —
  отказ без `--force`; с `--force` — заменить только блок между маркерами
  `# >>> selectel-s3 (managed)` / `# <<< selectel-s3 (managed)`; остальной файл байт в байт.
  claude-code `--scope user` → напечатать готовую команду `claude mcp add …`
  (сама не выполняет). `--dry-run` печатает итог и ничего не пишет.
- `skill install [--target <dir>]` (по умолчанию `~/.claude/skills/selectel-s3`):
  раскладывает встроенные файлы скила, идемпотентно.

### 5.7 Skill (`skills/selectel-s3/`)

`SKILL.md` с frontmatter `name: selectel-s3` и `description` (RU+EN, ≤ 1024 символа,
триггеры: «selectel s3», «бакет селектел», «загрузи в s3», «presigned», «бэкап в s3»).
Тело: когда звать какие инструменты, байты мимо модели, гейт, особенности Selectel.
`references/`: `backup-restore.md`, `share-file.md`, `least-privilege.md`
(пользователи/роли/S3-ключи Selectel, политика бакета), `cost-hygiene.md`
(брошенные multipart, крупные объекты, TTL-лимиты бакета в панели вместо lifecycle,
классы хранения), `migration.md`, `incident-response.md`. Только факты из research-файла.

### 5.8 Документация и SEO

- `README.md` — по-русски, но СВЕРХУ английский блок. Обязательные заголовки, дословно:
  `# Selectel S3 MCP Server & AI CLI`, `## English`, `## Возможности`, `## Быстрый старт`,
  `## Подключение к Claude Code, Cursor, Codex`, `## Инструменты MCP`, `## CLI`,
  `## Skill для AI-агентов`, `## Безопасность`, `## Пулы и классы хранения Selectel`,
  `## FAQ`, `## Другие провайдеры`, `## Лицензия`.
  Первый абзац после бейджей естественно содержит: Selectel S3, Object Storage, MCP,
  Claude Code, Cursor, Codex, CLI, AI-агент, бэкап. FAQ содержит вопросы: «Что такое
  MCP-сервер?», «Как подключить MCP-сервер к Claude Code?», «Безопасно ли давать агенту
  доступ к S3?», «Чем это отличается от aws-cli и rclone?», «Почему нет lifecycle и тегов?».
  «Другие провайдеры» — список шести без ссылок, с пометкой «в планах».
- `README.en.md` — полный английский: `# Selectel S3 MCP Server & AI CLI`, `## Features`,
  `## Quick start`, `## Connect to Claude Code, Cursor, Codex`, `## MCP tools`, `## CLI`,
  `## Agent skill`, `## Security`, `## Selectel pools and storage classes`, `## FAQ`, `## License`.
- `docs/compat.md` — вывод `compat --markdown`, байт в байт.
- `server.json` по схеме `2025-12-11`: `name: io.github.stufently/selectel-s3-mcp-ai-cli`,
  пакет `registryType: oci`, `identifier: ghcr.io/stufently/selectel-s3-mcp-ai-cli`,
  `version: 0.1.0`, transport stdio, переменные окружения (секретные — `isSecret: true`).
  Копия схемы — `testdata/server.schema.json` (sha256 из раздела 4); валидация — Go-тест.

### 5.9 Сборка и CI

- `.golangci.yml` (v2): `govet, staticcheck, errcheck, ineffassign, unused, gosec`
  (без исключений `nolint` без комментария-причины).
- `.goreleaser.yaml` (v2): linux/darwin/windows × amd64/arm64, tar.gz (windows — zip),
  checksums, ldflags версии.
- `Dockerfile`: multi-stage `golang:1.27.2` → `scratch` с ca-certificates, `USER 65532:65532`,
  `ENTRYPOINT ["/selectel-s3"]`, `CMD ["mcp"]`.
- `.github/workflows/ci.yml` (push/PR): lint, unit, conformance (`scripts/conformance.sh`),
  actionlint; `release.yml` (теги `v*`): goreleaser + образ в ghcr. Действия — версии
  из research-таблицы, запинены тегом.

### 5.10 Скрипты

- `scripts/conformance.sh`: уникальные имена сети и контейнера (с `$$`), `trap` уборки
  (`docker rm -f <имя>`, `docker network rm <имя>`, без `rm -rf`), versitygw с
  `VGW_REGION=ru-1` и `--user` текущего uid, затем `golang:1.27.2` в той же сети:
  `go test -tags conformance -count=1 ./conformance/...` с `SELECTEL_S3_ENDPOINT`,
  `SELECTEL_S3_ALLOW_INSECURE=1` и тестовыми ключами. Код возврата = код тестов.
- `scripts/mcp_smoke.sh`: собирает бинарь в Docker, запускает `selectel-s3 mcp` по stdio
  с фейковыми кредами для 4 наборов (`нет`, `--allow-write`, `--allow-write --allow-delete`,
  все три) и без `SELECTEL_S3_FILE_ROOT`, шлёт `initialize`, `notifications/initialized`,
  `tools/list`, сравнивает отсортированные имена с `testdata/tools/{read,write,write-delete,all}.golden`.
  Сеть для `tools/list` не нужна (регистрация не ходит в S3).
- `scripts/mutation_gate.sh`: обвязка по правилам из спеки-шаблона (чистый прогон зелёный →
  фрагмент ровно один раз → мутация → целевой тест падает на СВОЕЙ строке ассерта →
  откат в `trap`/`finally` и сверка sha256). Мутации (минимум по одной на пункт):
  M1 регистрация игнорирует уровень доступа; M2 `confirm` пропускает без подтверждения;
  M3 `block` пропускает; M4 `elicit` принимает `confirm:true`; M5 скоуп префикса без
  границы сегмента; M6 fs-guard без разворота симлинков; M7 редактирование не вырезает
  `X-Amz-Signature`; M8 `cap:` игнорируется при регистрации; M9 `s3_put_object` без
  `If-None-Match` по умолчанию; M10 файл кредов принимается с правами `0644`;
  M11 `s3_multipart_complete` с `overwrite:false` не проверяет существование цели;
  M12 `overwrite:true` не идёт под гейт.
  Целевые тесты — unit (без S3). Печатает по каждой «killed/survived» и строку ассерта.
- `scripts/check_docs.sh`: обязательные заголовки README.md/README.en.md (дословно из 5.8);
  `docs/compat.md` совпадает с `compat --markdown` (через Docker); sha256
  `testdata/server.schema.json` совпадает с разделом 4.

### 5.11 Тесты

Unit (без S3, обычный `go test`): профиль и эндпоинты пулов; приоритет конфигурации и отказ
по правам файла кредов; матрица гейта (4 политики × клиент с/без elicitation × accept/decline);
скоуп (включая `backups` vs `backups-evil`, `..`); fs-guard (симлинк наружу, `..`);
редактирование; регистрация инструментов против таблицы 5.4 (golden) и `cap:`-флагов
(тестовый профиль с `object_tagging: true` регистрирует теги); `If-None-Match` через
`httptest`; правило перезаписи для всех пяти инструментов (при `block` перезапись
невозможна ни одним); ноль запросов к S3 при отказе гейта; маппинг ошибок (429 → retryable); `mcp install` для каждого клиента во временном
HOME (идемпотентность, чужие ключи целы, секретов нет, codex: отказ без `--force`,
замена только своего блока); `skill install`; `server.json` против схемы; генератор compat.

Conformance (`-tags conformance`, versitygw): CRUD бакета; put/get/head/list с пагинацией
(`max_keys=2` на 5 объектах); защита от перезаписи (см. P1); presign GET/PUT через
`net/http`; multipart по presigned-частям и complete; copy/move; delete/delete_objects;
versioning → 2 версии → удаление версии; бакет с Object Lock и чтение конфигурации;
CORS и политика; теги на тестовом профиле; аналитика (largest, prefix usage, unfinished);
MCP in-memory: наборы инструментов по уровням, гейт без побочного эффекта (объект на месте),
скоуп, файловые инструменты внутри/снаружи корня, аудит-строка без секрета и подписи;
CLI бинарём через `os/exec`: `mb`, `cp` 20 МиБ туда-обратно с multipart и сравнением sha256,
`ls --json`, `presign` + GET, `rm` без `--yes` → код 2, с `--yes` → удалён, `rb`.

## 6. Не трогать

`LICENSE`, `TASKS.md`, `CHANGELOG.md`, `docs/research/**`, `docs/spec-queue.md` —
это файлы постановщика. Исключение: эта спека (`docs/specs/v1-selectel.md`) коммитится
вместе с работой. `README.md` переписывается целиком (разрешено). Ничего за пределами
клона, никаких правок хоста, никакого `sudo`.

## 7. Критерии приёмки

Каждая команда запускается из корня клона.

- **AC-001** Сборка и vet — `bash -c 'mkdir -p "$HOME/.cache/sel-s3-go" && docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golang:1.27.2 sh -c "go build ./... && go vet ./... && go vet -tags conformance ./..."'`

- **AC-002** Unit-тесты с race — `bash -c 'docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golang:1.27.2 go test -race -count=1 ./...'`

- **AC-003** Линтер — `bash -c 'docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOLANGCI_LINT_CACHE=/cache/lint -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golangci/golangci-lint:v2.14.0 golangci-lint run --build-tags conformance ./...'`

- **AC-004** Conformance против versitygw (S3 + MCP + CLI) — `bash scripts/conformance.sh`

- **AC-005** MCP по stdio: состав инструментов по уровням — `bash scripts/mcp_smoke.sh`

- **AC-006** Провайдер изолирован в internal/provider — `bash -c 'test -d internal/provider || exit 1; git grep -qi selectel -- internal/provider || exit 1; git grep -qi selectel -- "internal/*.go" ":(exclude)internal/provider/**"; test $? -eq 1 || exit 1; git grep -qE "selcloud|srvstorage" -- "*.go" ":(exclude)internal/provider/**"; test $? -eq 1'`

- **AC-007** Мутационные ворота безопасности — `bash scripts/mutation_gate.sh`

- **AC-008** goreleaser: проверка и snapshot на 6 платформ — `bash -c 'docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src goreleaser/goreleaser:v2.18.3 check && docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src goreleaser/goreleaser:v2.18.3 release --snapshot --clean --skip=publish,docker && test "$(ls dist/*.tar.gz dist/*.zip | wc -l)" -eq 6'`

- **AC-009** Docker-образ собирается и не работает от root — `bash -c 'docker build -q -t selectel-s3-mcp-ai-cli:acceptance . >/dev/null && docker run --rm selectel-s3-mcp-ai-cli:acceptance version | grep -q "^selectel-s3 0.1.0" && test "$(docker inspect -f "{{.Config.User}}" selectel-s3-mcp-ai-cli:acceptance)" = "65532:65532"'`

- **AC-010** actionlint — `bash -c 'docker run --rm -v "$PWD:/repo:ro" -w /repo rhysd/actionlint:1.7.12 -color=never'`

- **AC-011** Документация, compat и схема реестра — `bash scripts/check_docs.sh`

- **AC-012** Границы и чистое дерево — `bash -c 'test -z "$(git diff --name-only a3dcfc82f3fe847a056ebee561fabc1ef54a8814..HEAD -- LICENSE TASKS.md CHANGELOG.md docs/research docs/spec-queue.md)" && git ls-files --error-unmatch docs/specs/v1-selectel.md >/dev/null && test -z "$(git status --porcelain -- . ":(exclude)report.json" ":(exclude)report-blocked.md")"'`

## 8. Авторевью `cross-review-v1`

Пара для Codex: `agy` + `grok`, только чтение. Порядок — дословно по
`/home/deploy/.claude/skills/executor-milestone/docs/spec-template.md`, раздел
«Автономное перекрёстное ревью»: реализация → своя проверка → коммит REVIEW_SHA →
оба ревью параллельно на `BASE_SHA..REVIEW_SHA` → единый список находок → ОДИН заход
исправлений → FINAL_SHA → одна проверка `REVIEW_SHA..FINAL_SHA` теми же ревьюерами.
Квитанции пишет только обёртка, путь абсолютный:

```bash
bash /home/deploy/.claude/skills/executor-milestone/scripts/review_run.sh initial agy --clone <клон> --base a3dcfc82f3fe847a056ebee561fabc1ef54a8814 --range a3dcfc82f3fe847a056ebee561fabc1ef54a8814..<REVIEW_SHA> --context "Веха v1 selectel-s3-mcp-ai-cli, спека docs/specs/v1-selectel.md; только чтение"
bash /home/deploy/.claude/skills/executor-milestone/scripts/review_run.sh initial grok --clone <клон> --base a3dcfc82f3fe847a056ebee561fabc1ef54a8814 --range a3dcfc82f3fe847a056ebee561fabc1ef54a8814..<REVIEW_SHA> --context "Веха v1 selectel-s3-mcp-ai-cli, спека docs/specs/v1-selectel.md; только чтение"
```

Фокус для ревьюеров: обход гейта/скоупа/корня файлов, утечка секрета или подписи,
соответствие таблице 5.4 и features профиля, провайдерский код вне `internal/provider`.

## 9. Контракт отчёта

`report.json` в КОРНЕ клона, untracked, схема v2. Записей в `criteria` ровно двенадцать:
от AC-001 до AC-012, команды — посимвольно из раздела 7. Поля дословно:
`schema_version` (2), `policy_id` (`cross-review-v1`), `spec_sha256` (sha256 этого файла),
`base_sha` (`a3dcfc82f3fe847a056ebee561fabc1ef54a8814`), `reviewed_sha`, `final_sha`,
`executor` (`{"backend": "codex", "model": "<точная модель>"}`), `review` с
`initial_receipts`, `verification_receipts`, `resolutions`, `handoff_status`, `criteria`.

```json
{"schema_version": 2, "policy_id": "cross-review-v1", "spec_sha256": "…",
 "base_sha": "a3dcfc82f3fe847a056ebee561fabc1ef54a8814", "reviewed_sha": "…", "final_sha": "…",
 "executor": {"backend": "codex", "model": "…"},
 "review": {"initial_receipts": [], "verification_receipts": [], "resolutions": []},
 "handoff_status": "ready",
 "criteria": [{"id": "AC-001", "status": "pass", "command": "…", "rc": 0, "note": "версии зависимостей: …"}]}
```

Перед сдачей: `python3 /home/deploy/.claude/skills/executor-milestone/scripts/accept_run.py <клон> --spec docs/specs/v1-selectel.md --dry-run` должен дать код 2.
`blocked` — только когда среда не даёт выполнить критерий: `"rc": null` и дословная
ошибка в `note`. Обходить несовместимость запрещено (`--no-deps`, `GOTOOLCHAIN=local`,
`|| true`, `set +e` в командах критериев и т. п.).

## 10. Контракт на невыполнимое

Остановись и доложи в `report-blocked.md` (и `report.json` со статусами), если:
- go-sdk v1.8.0 не даёт нужного (например, elicitation из обработчика инструмента);
- versitygw не поддерживает что-то кроме P1 — не обходить, не подменять сервер;
- версия зависимости из таблицы не собирается — не понижать молча;
- критерий невыполним по причине спеки (а не кода).
Ослаблять безопасность ради зелёного теста запрещено.

## 11. Стыки со следующими вехами

- Вторая волна (API управления Selectel, оценка стоимости, HTTP-транспорт, async-задачи,
  prompts) опирается на: тип `Profile`, реестр инструментов с условиями уровня/cap/fileroot,
  `safety`-пакет как единственную точку гейта и скоупа.
- Вынос общего кода в `s3-mcp-ai-cli-kit`: всё вне `internal/provider` и `cmd/` должно
  быть провайдер-нейтральным уже сейчас (критерий изоляции это держит).
