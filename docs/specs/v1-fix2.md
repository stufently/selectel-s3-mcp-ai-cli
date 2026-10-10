# Веха v1-fix2: тесты против выживших мутантов v1-fix1

## 1. Шапка

- Репозиторий: `selectel-s3-mcp-ai-cli`, ветка `v1-fix1`.
- BASE_SHA: `309d53e` (код v1-fix1 + обёртка ворот через `exec`); спека — следующим коммитом.
- Исполнитель: Codex (`cx`). Мутации по новым тестам гоняет Grok отдельно.

## 2. Где работать

Только одноразовый клон. Push, merge, deploy запрещены. Всё — в Docker под своим uid:
`bash scripts/go.sh <команда>`. На хост ничего не ставить.

## 3. Задача и почему

Независимый прогон Grok по коду v1-fix1 (`d4ac8df`): 36 мутантов, выжило 18. Код при
этом верен — выжившие показывают дыры в ТЕСТАХ. Список с точными `original`/`mutated`,
строкой и подсказкой, каким тестом убивать: `docs/specs/v1-fix2-mutants.json`.
Поведение продукта не менять.

## 4. Проверено вживую / предположения

- Проверено координатором: все 18 мутантов в отчёте Grok помечены «Не эквивалентен» с
  объяснением; код исходника по каждому месту верен (сверено чтением диффа v1-fix1).
- Проверено координатором: `bash scripts/mutation_gate.sh` на `309d53e` — 55/55 убиты;
  SIGTERM контейнеру после наложения мутации → «terminated: original restored», rc 1.

## 5. Что сделать

### 5.1 Мутанты продукта (15 штук)
H04 H06 H07 H10 H11 H12 H13 H14 H18 H19 H20 H22 H28 H29 H32.
1. Дописать тесты, которые убивают каждого (подсказка — поле `note`; тест проверяет
   ПРИЧИНУ отказа, а не только `err != nil`).
2. Добавить каждого в таблицу `scripts/mutation_gate.go` под ТЕМ ЖЕ id (`{"H04", …}`),
   с `original`/`mutated` из JSON. Фрагмент обязан встречаться в файле ровно один раз;
   если не так — расширь фрагмент контекстом, смысл мутации не меняй.

### 5.2 Мутанты самих ворот (H34 H35 H36)
Ворота не могут мутировать сами себя, поэтому эти три закрываются самопроверкой:
- `MUTATION_GATE_SELFTEST_SIGNAL=TERM` и `=INT` — сигнал ПОСЛЕ наложения первого мутанта;
- `MUTATION_GATE_SELFTEST_SIGNAL=EARLY` — SIGTERM ДО записи мутанта на диск (H35).
Во всех трёх режимах: код выхода ненулевой, `git diff --quiet -- internal` чистый, в
выводе строка `original restored`, а при расхождении хэша восстановленного файла со
снимком — `restoration failed` и НЕТ `original restored` (H36). Прежнее значение `1`
оставить синонимом `TERM`.

### 5.3 Ничего лишнего
Код продукта (`internal/**` без `_test.go`, `cmd/**`) не менять. Если убить мутанта
нельзя без правки кода — это контракт на невыполнимое (раздел 10).

## 6. Не трогать

`LICENSE`, `TASKS.md`, `CHANGELOG.md`, `README*`, `docs/**`, `internal/**/*.go` кроме
`*_test.go`, `cmd/**`.

## 7. Разрешения

Добавлять и править `*_test.go`, `scripts/mutation_gate.go`, `scripts/mutation_gate.sh`.
Коммитить в `v1-fix1` в клоне.

## 8. Критерии приёмки

- **AC-001** Все 15 мутантов продукта внесены в ворота — `bash -c 'for id in H04 H06 H07 H10 H11 H12 H13 H14 H18 H19 H20 H22 H28 H29 H32; do grep -qF "{\"$id\"," scripts/mutation_gate.go || { echo "нет $id"; exit 1; }; done'`
- **AC-002** Мутационные ворота: все мутанты убиты — `bash scripts/mutation_gate.sh`
- **AC-003** Самопроверка SIGTERM — `bash -c 'git diff --quiet -- internal || exit 1; out=$(docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e MUTATION_GATE_SELFTEST_SIGNAL=TERM -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golang:1.27.2 go run scripts/mutation_gate.go 2>&1); rc=$?; printf "%s\n" "$out" | tail -3; test $rc -ne 0 && git diff --quiet -- internal && printf "%s\n" "$out" | grep -q "original restored"'`
- **AC-004** Самопроверка SIGINT — `bash -c 'git diff --quiet -- internal || exit 1; out=$(docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e MUTATION_GATE_SELFTEST_SIGNAL=INT -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golang:1.27.2 go run scripts/mutation_gate.go 2>&1); rc=$?; printf "%s\n" "$out" | tail -3; test $rc -ne 0 && git diff --quiet -- internal && printf "%s\n" "$out" | grep -q "original restored"'`
- **AC-005** Самопроверка раннего сигнала — `bash -c 'git diff --quiet -- internal || exit 1; out=$(docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e MUTATION_GATE_SELFTEST_SIGNAL=EARLY -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golang:1.27.2 go run scripts/mutation_gate.go 2>&1); rc=$?; printf "%s\n" "$out" | tail -3; test $rc -ne 0 && git diff --quiet -- internal && printf "%s\n" "$out" | grep -q "original restored"'`
- **AC-006** Сборка, vet и unit-тесты с race — `bash -c 'bash scripts/go.sh sh -c "go build ./... && go vet ./... && go vet -tags conformance ./... && go test -race -count=1 ./..."'`
- **AC-007** Линтер — `bash -c 'docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOLANGCI_LINT_CACHE=/cache/lint -v "$HOME/.cache/sel-s3-go:/cache" -v "$PWD:/src" -w /src golangci/golangci-lint:v2.14.0 golangci-lint run --build-tags conformance ./...'`
- **AC-008** Conformance и MCP-смоук — `bash -c 'bash scripts/conformance.sh && bash scripts/mcp_smoke.sh'`
- **AC-009** Код продукта не изменён, границы и чистое дерево — `bash -c 'test -z "$(git diff --name-only 309d53e0255cf56315f5ce3a4da48cc30f34345e..HEAD -- LICENSE TASKS.md CHANGELOG.md README.md README.en.md docs cmd ":(exclude)docs/specs/v1-fix2.md" ":(exclude)docs/specs/v1-fix2-mutants.json")" && test -z "$(git diff --name-only 309d53e0255cf56315f5ce3a4da48cc30f34345e..HEAD -- internal | grep -v "_test\.go$")" && test -z "$(git status --porcelain -- . ":(exclude)report.json" ":(exclude)report-blocked.md")"'`

## 9. Авторевью

Не запускать: координатор делает ревью сам.

## 10. Контракт отчёта и на невыполнимое

`report.json` в корне клона, untracked:
`{"criteria": [{"id": "AC-001", "status": "pass|fail|blocked", "command": "…", "rc": 0, "note": "…"}]}`,
ровно AC-001…AC-009, команды посимвольно.

Мутанта нельзя убить без правки кода продукта, он на деле эквивалентен, или фрагмент
нельзя сделать уникальным, не меняя смысла — остановись и опиши в `report-blocked.md`
с id и обоснованием. Обходить запрещено (`|| true`, `t.Skip`, удаление мутанта).

## 11. Стыки

После приёмки: Grok — мутации по новым тестам, координатор сливает `v1-fix1` в `main`.
