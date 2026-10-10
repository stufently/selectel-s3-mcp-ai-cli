# Веха v1-fix3: табличные тесты вместо точечных

## 1. Шапка

- Репозиторий: `selectel-s3-mcp-ai-cli`, ветка `v1-fix1`.
- BASE_SHA: `b07d0ff1e25ba14a0e26eb8944981b09d16a7415` (тесты v1-fix2); спека — следующим коммитом.
- Исполнитель: Codex (`cx`). Последний заход исправлений перед слиянием в `main`.

## 2. Где работать

Только одноразовый клон. Push, merge, deploy запрещены. Всё — в Docker под своим uid:
`bash scripts/go.sh <команда>`. На хост ничего не ставить.

## 3. Задача и почему

Третий прогон Grok (по тестам v1-fix2): 28 мутантов, выжило 7 —
`docs/specs/v1-fix3-mutants.json`. Причина у всех одна: тесты проверяют ТОЧКИ, которые
назвал прошлый отчёт, а соседний вариант той же мутации проходит. Поэтому теперь тест
перебирает ВСЁ пространство, а не одну точку. Код продукта верен и не меняется.

## 4. Проверено вживую / предположения

- Проверено координатором: в `b07d0ff` ворота 70/70, ревью Codex — без находок.
- Предположение: aws-sdk-go-v2 по умолчанию ретраит 500/502/503/504 и throttle-коды;
  плюс наш 429. Если тест покажет другой дефолтный набор — зафиксируй фактический в
  `note` отчёта и проверяй его, но 429 обязан ретраиться, а 408/409/412 — нет.

## 5. Что сделать

### 5.1 Табличные тесты (имена обязательны)

- `TestEveryToolRejectsCaseCollidingKeys` (`internal/mcpserver`): сервер со ВСЕМИ
  уровнями, всеми capability и FILE_ROOT; цикл по ВСЕМ зарегистрированным инструментам
  (список из `tools/list`, не хардкодом; проверить, что их не меньше 35). Для каждого —
  аргументы с парой `bucket`/`Bucket` на верхнем уровне → отказ с `case-colliding`,
  ноль HTTP-запросов. Плюс вложенный случай для каждого инструмента с полем-объектом или
  массивом объектов (`objects`, `tags` и т. п.). Убивает J05.
- `TestInstallPermissionMatrix` (`internal/cli` или `internal/install` — там, где
  проходит путь `mcp install` с флагами): все 8 подмножеств env `SELECTEL_S3_ALLOW` ×
  для каждого из трёх уровней флаг «не задан» / `=true` / `=false` (27 сочетаний) —
  216 случаев. Ожидание по каждому уровню: флаг задан → его значение, иначе → env. В
  конфиге клиента ровно три `--allow-<level>=<bool>`. Убивает J12.
- `TestMoveETagPassthrough` (`internal/s3core`): ETag источника из таблицы — `"abc"`,
  `"AbC"`, `""` (две кавычки), `W/"x"`, `"a-3"` (multipart). `x-amz-copy-source-if-match`
  у COPY и `If-Match` у DELETE равны ETag из HEAD байт в байт. Убивает J16 и J18.
- `TestMoveDeleteStatusMatrix` (`internal/s3core`): ответ DELETE из набора 400, 403,
  404, 409, 412, 500 (500 отдавать на каждую попытку ретраера). Только 412 → «source
  changed during move; copy kept»; остальные → «copy succeeded but source deletion
  failed». Убивает J19.
- `TestRetryStatusMatrix` (`internal/s3core`): для каждого статуса из 400, 403, 404, 408,
  409, 412, 429, 500, 502, 503, 504 сервер отвечает этим статусом один раз, затем 200.
  Ретраимые (429, 500, 502, 503, 504) → успех и ровно 2 запроса; остальные → ошибка и
  ровно 1 запрос. Убивает J21.
- `TestCodexManagedBlockExactBytes` (`internal/install`): управляемый блок между
  маркерами сравнивается с эталоном ЦЕЛИКОМ, байт в байт (без пустых строк, ровно один
  перевод строки перед `# <<<`), для файла без `[mcp_servers]` и с явной `[mcp_servers]`.
  Убивает J28.

### 5.2 Ворота

Все 7 мутантов (J05 J12 J16 J18 J19 J21 J28) — в таблицу `scripts/mutation_gate.go` под
теми же id, с `original`/`mutated` из JSON; целевой тест — из 5.1. Фрагмент ровно один
раз; если нет — расширь контекстом, смысл не меняй.

### 5.3 Ничего лишнего

Код продукта (`internal/**` без `_test.go`, `cmd/**`) не менять. Если тест 5.1 краснеет
на ТЕКУЩЕМ коде — это найденный дефект продукта: остановись по разделу 10, не чини сам.

## 6. Не трогать

`LICENSE`, `TASKS.md`, `CHANGELOG.md`, `README*`, `docs/**`, `internal/**/*.go` кроме
`*_test.go`, `cmd/**`.

## 7. Разрешения

Добавлять и править `*_test.go`, `scripts/mutation_gate.go`, `scripts/mutation_gate_test.go`.
Коммитить в `v1-fix1` в клоне. Спека `docs/specs/v1-fix3.md` уже закоммичена координатором;
если она untracked — закоммить её первой.

## 8. Критерии приёмки

- **AC-001** Табличные тесты существуют и проходят — `bash -c 'out=$(bash scripts/go.sh go test -count=1 -v -run "^(TestEveryToolRejectsCaseCollidingKeys|TestInstallPermissionMatrix|TestMoveETagPassthrough|TestMoveDeleteStatusMatrix|TestRetryStatusMatrix|TestCodexManagedBlockExactBytes)$" ./internal/... 2>&1); rc=$?; printf "%s\n" "$out" | grep -E "^(--- |ok|FAIL)" | tail -12; test $rc -eq 0 || exit 1; for t in TestEveryToolRejectsCaseCollidingKeys TestInstallPermissionMatrix TestMoveETagPassthrough TestMoveDeleteStatusMatrix TestRetryStatusMatrix TestCodexManagedBlockExactBytes; do printf "%s\n" "$out" | grep -q -- "--- PASS: $t " || { echo "нет $t"; exit 1; }; done'`
- **AC-002** Все 7 мутантов внесены в ворота — `bash -c 'for id in J05 J12 J16 J18 J19 J21 J28; do grep -qF "{\"$id\"," scripts/mutation_gate.go || { echo "нет $id"; exit 1; }; done'`
- **AC-003** Ворота: все убиты, 7 новых реально прогнаны — `bash -c 'out=$(bash scripts/mutation_gate.sh 2>&1); rc=$?; printf "%s\n" "$out" | tail -3; test $rc -eq 0 || exit 1; for id in J05 J12 J16 J18 J19 J21 J28; do printf "%s\n" "$out" | grep -q "^$id killed: " || { echo "не прогнан $id"; exit 1; }; done'`
- **AC-004** Сборка, vet и unit-тесты с race — `bash -c 'bash scripts/go.sh sh -c "go build ./... && go vet ./... && go vet -tags conformance ./... && go test -race -count=1 ./..."'`
- **AC-005** Линтер — `bash -c 'docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOLANGCI_LINT_CACHE=/cache/lint -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golangci/golangci-lint:v2.14.0 golangci-lint run --build-tags conformance ./...'`
- **AC-006** Conformance и MCP-смоук — `bash -c 'bash scripts/conformance.sh && bash scripts/mcp_smoke.sh'`
- **AC-007** Код продукта не изменён, границы и чистое дерево — `bash -c 'test -z "$(git diff --name-only b07d0ff1e25ba14a0e26eb8944981b09d16a7415..HEAD -- LICENSE TASKS.md CHANGELOG.md README.md README.en.md docs cmd ":(exclude)docs/specs/v1-fix3.md" ":(exclude)docs/specs/v1-fix3-mutants.json")" && git diff --quiet a0d142585f46915a29ebb10684bd186dee015b1c HEAD -- docs/specs/v1-fix3-mutants.json && test -z "$(git diff --name-only b07d0ff1e25ba14a0e26eb8944981b09d16a7415..HEAD -- internal | grep -v "_test\.go$")" && git ls-files --error-unmatch docs/specs/v1-fix3.md >/dev/null && test -z "$(git status --porcelain -- . ":(exclude)report.json" ":(exclude)report-blocked.md")"'`

## 9. Авторевью

Не запускать: координатор делает ревью сам.

## 10. Контракт отчёта и на невыполнимое

`report.json` в корне клона, untracked:
`{"criteria": [{"id": "AC-001", "status": "pass|fail|blocked", "command": "…", "rc": 0, "note": "…"}]}`,
ровно AC-001…AC-007, команды посимвольно.

Табличный тест краснеет на текущем коде, мутант на деле эквивалентен, фрагмент нельзя
сделать уникальным — остановись и опиши в `report-blocked.md` (случай, ожидание,
факт). Обходить запрещено (`|| true`, `t.Skip`, сужение таблицы, удаление мутанта).

## 11. Стыки

После приёмки координатор сливает `v1-fix1` в `main` и пушит.
