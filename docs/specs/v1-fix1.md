# Веха v1-fix1: исправления по приёмке v1

## 1. Шапка

- Репозиторий: `selectel-s3-mcp-ai-cli`, ветка `v1-fix1`.
- BASE_SHA: `3b5af397200bb0dd03ce9b104db50d220b7fb1c4` (код v1; спека — следующим коммитом, он защищённых путей не трогает).
- Исполнитель: Codex (`cx`). Код v1 писал он же; мутации исправлений гоняет Grok отдельно.
- Источник находок: ревью Codex и agy по диффу v1, разбор координатора, мутационный
  прогон Grok (раздел 5.11).

## 2. Где работать

Только одноразовый клон. Push, merge, deploy запрещены. Всё — в Docker под своим uid:
`bash scripts/go.sh <команда>` (образ `golang:1.27.2`). На хост ничего не ставить.

## 3. Задача и почему

Приёмка v1 нашла дефекты в гейте, подтверждении, конфигурации, перемещении,
ретраях, локальных файлах и установщике. Каждый исправить и закрыть тестом, который
на старом коде красный. Других изменений поведения не делать.

## 4. Проверено вживую / предположения

- Проверено координатором по коду `3b5af39`: каждая находка раздела 5 воспроизводится
  чтением кода (строки указаны).
- Предположение: `os.Root.Link` есть в Go 1.27 (добавлен в 1.25). Если нет —
  атомарную публикацию без замены сделать через `OpenFile(O_CREATE|O_EXCL)` финального
  имени и копирование; молча заменять на `Rename` нельзя.
- Предположение: Selectel отвечает на превышение лимита HTTP 429 с кодом, которого нет
  в списке throttle-кодов aws-sdk-go-v2 (живого ответа не видели). Поэтому ретраить
  по статусу 429, а не по коду.

## 5. Что исправить

Имена тестов обязательны: по ним идут критерии.

### 5.1 FX-1 Форма подтверждения требует `approved: true`
`internal/mcpserver/server.go:91` проверяет только `Action == "accept"`. Требовать ещё
`Content["approved"] == true` (именно bool `true`; `"true"`, `1`, отсутствие — отказ).
Тест `TestConfirmFormRequiresApprovedTrue` (`internal/mcpserver`): accept с
`approved:false`, без поля и со строкой `"true"` — отказ и ноль запросов к S3;
accept с `approved:true` — выполнение.

### 5.2 FX-2 Дайджест подтверждения не обходится регистром ключей
`internal/safety/confirmation.go:48` хэширует `map[string]any`, а аргументы декодируются
в структуры без учёта регистра и берут ПОСЛЕДНИЙ дубликат: `{"key":"a","Key":"b"}` и
`{"Key":"b","key":"a"}` дают один дайджест, но разные объекты. Аргументы инструмента,
в которых на любом уровне вложенности есть два ключа, совпадающие без учёта регистра
(включая точные дубликаты), отклонять ДО гейта и до запросов к S3.
Тест `TestArgumentsRejectCaseCollidingKeys` (`internal/mcpserver`): верхний уровень и
внутри `objects` у `s3_delete_objects` — отказ, ноль запросов к S3.

### 5.3 FX-3 Явный флаг перебивает env
`internal/config/config.go:165`: флаги только ДОБАВЛЯЮТ уровни, поэтому
`--allow-delete=false` при `SELECTEL_S3_ALLOW=delete` не снимает удаление. Порядок
по спеке v1 — флаги > env > файл. Явно заданный флаг (`Changed`) задаёт уровень в обе
стороны; не заданный — не трогает. Тест `TestExplicitAllowFlagOverridesEnv`
(`internal/config`): env `write,delete` + явный `--allow-delete=false` → delete выключен,
write включён; без флага → оба включены.

### 5.4 FX-4 Перемещение не удаляет изменённый источник
`internal/s3core/objects.go:105`: после копирования источник удаляется без проверки.
Делать так: HEAD источника → ETag; `CopyObject` с `CopySourceIfMatch: <ETag>`; перед
удалением повторный HEAD, ETag другой → источник НЕ удалять, вернуть ошибку
«source changed during move; copy kept». Тест `TestMoveKeepsChangedSource`
(`internal/s3core`, `httptest`-сервер): второй HEAD отдаёт другой ETag → DELETE не
отправлен; одинаковый → отправлен; в запросе копирования есть `x-amz-copy-source-if-match`.

### 5.5 FX-5 Ретраи 429
`internal/s3core/client.go:75`: `retry.NewAdaptiveMode()` не ретраит HTTP 429 с
неизвестным кодом. Добавить ретрай по статусу 429 (адаптивный режим сохранить).
Тест `TestRetriesHTTP429` (`internal/s3core`, `httptest`): два ответа 429 с кодом
`RateLimit`, затем 200 → операция успешна, запросов ровно 3.

### 5.6 FX-6 Рекурсивный `cp` читает только из выбранного каталога
`internal/cli/transfers.go:143`: каждый файл открывается через новый `os.OpenRoot`
от СВОЕГО каталога, поэтому подкаталог, подменённый симлинком после обхода, уводит
чтение наружу. Открыть ОДИН `os.Root` на исходный каталог и читать каждый файл через
него по относительному пути. Вынести загрузку дерева в функцию, принимающую `*os.Root`
и список относительных путей. Тест `TestUploadTreeConfinedToRoot` (`internal/cli`):
путь через подкаталог-симлинк наружу → ошибка, ноль PUT.

### 5.7 FX-7 и FX-10 Скачивание не перезаписывает локальный файл без разрешения
`internal/safety/files.go:61` всегда публикует через `Rename`, то есть заменяет
существующий файл. Это бьёт дважды: CLI `cp` проверяет цель `Lstat` до скачивания
(гонка, `transfers.go:115`), а MCP `s3_download_file` (`internal/s3core/client.go:244`)
не проверяет вовсе. Правило: `Write` без разрешения публикует БЕЗ замены (через
`Root.Link` временного файла, затем удаление временного); цель существует → ошибка,
содержимое цели не тронуто. Замена — только при `overwrite:true`, и для
`s3_download_file` она идёт под гейт так же, как `overwrite:true` у записи в S3
(добавить `overwrite` в его схему и в `Gated`). Тесты: `TestWriteNoReplace`
(`internal/safety`): цель создана до публикации → ошибка, содержимое прежнее;
`TestDownloadFileOverwriteGated` (`internal/mcpserver`): существующая цель без
`overwrite` → отказ; с `overwrite:true` → запрос подтверждения.

### 5.8 FX-8 Установщик Codex и явная таблица `[mcp_servers]`
`internal/install/install.go:167`: при явной `[mcp_servers]` с чужим сервером
добавленный блок создаёт повторную таблицу → TOML невалиден. Блок писать
dotted-заголовком `[mcp_servers.<бинарь>]` без повторного `[mcp_servers]`.
Тест `TestCodexInstallWithExplicitMcpServersTable` (`internal/install`): итог
разбирается TOML-парсером, чужой сервер на месте, остальной файл байт в байт.

### 5.9 FX-9 Мутационные ворота откатывают мутацию по сигналу
`scripts/mutation_gate.go:23`: откат только `defer`. Добавить откат по SIGINT/SIGTERM
(`signal.NotifyContext` и восстановление исходника до выхода).

### 5.10 Ничего лишнего
Документацию (`README*`, `docs/`) поправить только там, где поведение изменилось
(FX-7: `overwrite` у `s3_download_file`).

### 5.11 Выжившие мутанты Grok
Независимый прогон Grok на `3b5af39`: 50 мутантов, выжило 37. Список с точными
`original`/`mutated`, строкой и подсказкой, каким тестом убивать, лежит в
`docs/specs/v1-fix1-mutants.json`. Сделать два дела.
1. Дописать тесты, которые убивают каждого (по подсказке в `note`; тест проверяет
   ПРИЧИНУ отказа, а не только `err != nil`).
2. Добавить каждого в таблицу `scripts/mutation_gate.go` под ТЕМ ЖЕ id (`{"G01", …}`).
   Если исправления 5.1–5.9 изменили фрагмент (например SRV03 = FX-1, CFG06 = FX-3),
   переформулируй мутант под новый код так, чтобы он ломал ту же защиту, и запиши
   причину в `note` отчёта. Удалять мутанта или делать его эквивалентным нельзя.

## 6. Не трогать

`LICENSE`, `TASKS.md`, `CHANGELOG.md`, `docs/research/`, `docs/spec-queue.md`,
`docs/specs/v1-selectel.md`, `docs/specs/v1-fix1-mutants.json`.

## 7. Разрешения

Править `internal/**`, `cmd/**`, `scripts/**`, `conformance/**`, `skills/**`,
`README*`, `docs/*.md` (кроме перечисленного в разделе 6). Добавлять тесты. Коммитить в
`v1-fix1` в клоне.

## 8. Критерии приёмки

- **AC-001** Подтверждение: approved и регистр ключей — `bash -c 'out=$(bash scripts/go.sh go test -count=1 -v -run "^(TestConfirmFormRequiresApprovedTrue|TestArgumentsRejectCaseCollidingKeys|TestDownloadFileOverwriteGated)$" ./internal/mcpserver/ 2>&1); rc=$?; printf "%s\n" "$out" | tail -15; test $rc -eq 0 || exit 1; for t in TestConfirmFormRequiresApprovedTrue TestArgumentsRejectCaseCollidingKeys TestDownloadFileOverwriteGated; do printf "%s\n" "$out" | grep -q -- "--- PASS: $t " || exit 1; done'`
- **AC-002** Приоритет явного флага — `bash -c 'out=$(bash scripts/go.sh go test -count=1 -v -run "^TestExplicitAllowFlagOverridesEnv$" ./internal/config/ 2>&1); rc=$?; printf "%s\n" "$out" | tail -8; test $rc -eq 0 && printf "%s\n" "$out" | grep -q -- "--- PASS: TestExplicitAllowFlagOverridesEnv "'`
- **AC-003** Перемещение и ретраи 429 — `bash -c 'out=$(bash scripts/go.sh go test -count=1 -v -run "^(TestMoveKeepsChangedSource|TestRetriesHTTP429)$" ./internal/s3core/ 2>&1); rc=$?; printf "%s\n" "$out" | tail -10; test $rc -eq 0 || exit 1; for t in TestMoveKeepsChangedSource TestRetriesHTTP429; do printf "%s\n" "$out" | grep -q -- "--- PASS: $t " || exit 1; done'`
- **AC-004** Локальные файлы — `bash -c 'out=$(bash scripts/go.sh go test -count=1 -v -run "^(TestUploadTreeConfinedToRoot|TestWriteNoReplace)$" ./internal/cli/ ./internal/safety/ 2>&1); rc=$?; printf "%s\n" "$out" | tail -10; test $rc -eq 0 || exit 1; for t in TestUploadTreeConfinedToRoot TestWriteNoReplace; do printf "%s\n" "$out" | grep -q -- "--- PASS: $t " || exit 1; done'`
- **AC-005** Установщик Codex — `bash -c 'out=$(bash scripts/go.sh go test -count=1 -v -run "^TestCodexInstallWithExplicitMcpServersTable$" ./internal/install/ 2>&1); rc=$?; printf "%s\n" "$out" | tail -8; test $rc -eq 0 && printf "%s\n" "$out" | grep -q -- "--- PASS: TestCodexInstallWithExplicitMcpServersTable "'`
- **AC-006** Откат мутации по сигналу — `bash -c 'grep -q "signal.NotifyContext" scripts/mutation_gate.go && grep -qE "syscall.SIGTERM|unix.SIGTERM" scripts/mutation_gate.go'`
- **AC-007** Все 37 выживших мутантов внесены в ворота — `bash -c 'for id in G01 G02 C01 C02 C03 C04 SCP02 F01 F02 F03 RD01 RD02 CFG02 CFG03 CFG05 CFG06 SRV02 SRV03 SRV04 REG01 REG03 CL01 CL02 CL03 CL05 CL06 CL07 CL08 CL09 CL10 CL11 CL12 CL13 INS01 INS02 CLI01 CLI02; do grep -qF "{\"$id\"," scripts/mutation_gate.go || { echo "нет $id"; exit 1; }; done'`
- **AC-008** Сборка, vet и unit-тесты с race — `bash -c 'bash scripts/go.sh sh -c "go build ./... && go vet ./... && go vet -tags conformance ./... && go test -race -count=1 ./..."'`
- **AC-009** Линтер — `bash -c 'docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOLANGCI_LINT_CACHE=/cache/lint -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golangci/golangci-lint:v2.14.0 golangci-lint run --build-tags conformance ./...'`
- **AC-010** Conformance и MCP-смоук — `bash -c 'bash scripts/conformance.sh && bash scripts/mcp_smoke.sh'`
- **AC-011** Мутационные ворота (все мутанты убиты) и документация — `bash -c 'bash scripts/mutation_gate.sh && bash scripts/check_docs.sh'`
- **AC-012** Границы и чистое дерево — `bash -c 'test -z "$(git diff --name-only 3b5af397200bb0dd03ce9b104db50d220b7fb1c4..HEAD -- LICENSE TASKS.md CHANGELOG.md docs/research docs/spec-queue.md docs/specs/v1-selectel.md docs/specs/v1-fix1-mutants.json)" && git ls-files --error-unmatch docs/specs/v1-fix1.md >/dev/null && test -z "$(git status --porcelain -- . ":(exclude)report.json" ":(exclude)report-blocked.md")"'`

## 9. Авторевью

Не запускать: координатор делает ревью сам, по частям (лимит обёртки 100 КБ).

## 10. Контракт отчёта

`report.json` в корне клона, untracked:
`{"criteria": [{"id": "AC-001", "status": "pass|fail|blocked", "command": "…", "rc": 0, "note": "…"}]}`,
ровно AC-001…AC-012, команды посимвольно как в спеке.

## 11. Контракт на невыполнимое

Исправление требует нарушить раздел 6, тест нельзя сделать красным на старом коде,
предположение раздела 4 неверно — остановись и опиши в `report-blocked.md` с дословной
ошибкой. Обходить запрещено (`|| true`, `set +e`, `t.Skip`, ослабление проверок).

## 12. Стыки

После приёмки: Grok гоняет мутации по новым тестам, координатор сливает `v1-fix1` в `main`.
