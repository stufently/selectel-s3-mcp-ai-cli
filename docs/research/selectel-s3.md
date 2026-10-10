# Selectel S3 — проверенные факты (10.10.2026)

Источник каждого факта — документация Selectel, скачанная 10.10.2026 в формате
`.md` (`https://docs.selectel.ru/<путь>.md`). Непроверенное помечено **[предположение]**.

## Пулы и эндпоинты S3 API

Источник: `s3/manage/domains` (раздел «Домены S3 API»).

| Пул | Эндпоинт | Примечание |
|---|---|---|
| ru-1 | `s3.ru-1.storage.selcloud.ru` | Санкт-Петербург, Single-AZ |
| ru-3 | `s3.ru-3.storage.selcloud.ru` | Санкт-Петербург; единственный пул с ледяным классом |
| ru-6 | `s3.ru-6.storage.selcloud.ru` | Москва, Multi-AZ |
| ru-7 | `s3.ru-7.storage.selcloud.ru` | Москва, Single-AZ |
| gis-1 | `s3.gis-1.storage.selcloud.ru` | Москва, изолированный пул для ГИС (ФСТЭК) |
| uz-2 | `s3.uz-2.srvstorage.uz` | Узбекистан |

- Регион в подписи SigV4 = имя пула (пример boto3 в `api/object-storage-s3`: `region_name="<pool>"`).
- Адресация по умолчанию path-style; virtual-hosted включается на бакет, максимум
  100 таких бакетов на проект (`s3/about/limitations`).
- Подписи: AWS SigV4 и SigV2. «Срок жизни подписи запроса — 15 минут»
  (`api/object-storage-s3`). **[предположение]** это допуск по времени запроса, а не
  потолок срока presigned URL — проверить живым ключом.

## Совместимость с Amazon S3 API

Источник: `api/object-storage-s3`, раздел «Совместимость», дословно.

| Возможность | Selectel |
|---|---|
| Bucket CRUD | ✓ |
| Bucket ACL | ✗ |
| Bucket CORS | ✓ |
| Bucket Encryption | ✗ (только client-side) |
| Bucket Lifecycle | ✗ |
| Bucket Location | ✓ |
| Bucket Logging / Metrics / Notification / Ownership / Replication / Request Payment / Website | ✗ |
| Bucket Policy | ✓ |
| Bucket Tagging | ✗ |
| Bucket Versioning | ✓ |
| Conditional Writes | ✓ |
| Object CRUD / Copy | ✓ |
| Object ACL | ✗ (работает только Get ACL) |
| Object Lock Configuration | ✓ |
| Object Retention | ✗ |
| Object Tagging | ✗ |
| Object Versions | ✓ |
| Multipart Upload | ✓ |
| Public Access Block | ✗ (все запросы по умолчанию приватные) |

## Лимиты

Источник: `s3/about/limitations`.

- Объект до 50 ТБ; один PUT до 5 ГБ; multipart до 10 000 частей, минимальная часть 1 байт.
- Имя бакета до 128 символов UTF-8, имя объекта до 1024; бакетов в проекте до 2000.
- Политика доступа до 20 КБ.
- Лимиты запросов на бакет, в секунду: все 2000, GET+HEAD 1000, POST 200, PUT 300,
  DELETE 300. Превышение → `429 Too Many Requests`, `Rate Limit exceeded`.

## Классы хранения

Источники: `s3/about/payment`, `s3/buckets/create-bucket`.

- Стандартное, холодное, ледяное. Ледяное — только в пуле ru-3; минимальный срок
  хранения у ледяного не ограничен.
- Класс выбирается при создании бакета в панели и **после создания не меняется**;
  технически и по скорости классы одинаковые, отличается только цена.
- **[предположение]** через S3 API класс при `CreateBucket` не задаётся (бакет
  получает стандартный класс) — проверить живым ключом.
- Цены (ChatGPT-сводка владельца, октябрь 2026, **не сверены** с selectel.ru/prices):
  стандартное от 2,56 ₽/ГБ·мес, холодное от 1,25, ледяное от 0,89.

## Оплата и прочее

- Pay-as-you-go почасово; DELETE и ответы 403/5xx не тарифицируются; трафик внутри
  Selectel и входящий бесплатны (`s3/about/payment`).
- При нулевом балансе доступ блокируется, через 30 дней данные удаляются, включая
  объекты под Object Lock.
- Лимиты бакета (объём, число объектов, TTL объектов) задаются **в панели**, не через
  S3 API (`s3/buckets/limits`). Это замена lifecycle: TTL ставится только новым объектам.
- Ссылки на объект: presigned URL через SDK; одноразовые ссылки — функция панели
  (`s3/objects/obtain-link-to-object`).

## Тестовый S3-сервер для CI

Проверено 10.10.2026 в Docker на onyx:

- Образа `minio/minio` на Docker Hub больше нет (API отвечает 404).
- `versity/versitygw:v1.8.0` (posix-бэкенд, `--versioning-dir`) поддерживает:
  создание бакета, versioning (две версии объекта), теги объекта, бакет с Object Lock
  (`get-object-lock-configuration` → `Enabled`), CORS, политику бакета, multipart,
  presigned GET.
- ⚠️ Регион шлюза по умолчанию `us-east-1`; presigned URL с регионом `ru-1` дают
  `AuthorizationQueryParametersError`. Запускать шлюз с `VGW_REGION=ru-1`.
- Имена бакетов короче 3 символов отклоняются (`InvalidBucketName`).

Рабочая команда:

```bash
docker run -d --name vgw --network <сеть> --user "$(id -u):$(id -g)" \
  -v <data>:/data -v <ver>:/ver \
  -e ROOT_ACCESS_KEY=testkey -e ROOT_SECRET_KEY=testsecret12345 -e VGW_REGION=ru-1 \
  versity/versitygw:v1.8.0 --port :7070 posix --versioning-dir /ver /data
```

## Версии (проверены по реестрам 10.10.2026)

| Что | Версия | Откуда |
|---|---|---|
| Go | 1.27.2 (образ `golang:1.27.2`) | go.dev/dl, Docker Hub |
| github.com/modelcontextprotocol/go-sdk | v1.8.0 | GitHub releases |
| github.com/aws/aws-sdk-go-v2/service/s3 | v1.114.3 | proxy.golang.org |
| github.com/aws/aws-sdk-go-v2/config | v1.33.9 | proxy.golang.org |
| github.com/aws/aws-sdk-go-v2/feature/s3/manager | v1.23.14 | proxy.golang.org |
| github.com/spf13/cobra | v1.10.2 | GitHub releases |
| go.yaml.in/yaml/v3 | v3.0.5 | proxy.golang.org |
| github.com/pelletier/go-toml/v2 | v2.4.3 | proxy.golang.org |
| golangci-lint | v2.14.0 (образ `golangci/golangci-lint:v2.14.0`) | GitHub, Docker Hub |
| goreleaser | v2.18.3 (образ `goreleaser/goreleaser:v2.18.3`) | GitHub, Docker Hub |
| actionlint | 1.7.12 (образ `rhysd/actionlint:1.7.12`) | GitHub, Docker Hub |
| actions/checkout | v7.0.1 | GitHub |
| actions/setup-go | v7.0.0 | GitHub |
| golangci/golangci-lint-action | v9.3.0 | GitHub |
| goreleaser/goreleaser-action | v7.2.3 | GitHub |
| docker/setup-buildx-action | v4.4.1 | GitHub |
| versity/versitygw | v1.8.0 | GitHub, Docker Hub |
| MCP Registry server.json | схема `2025-12-11` | registry/docs/reference/server-json |

go-sdk v1.8.0: есть `ServerSession.Elicit`, `ClientCapabilities.Elicitation`,
`NewInMemoryTransports`, `AddTool[In, Out]`, `Server.AddResource` (проверено чтением
`mcp/server.go`, `mcp/protocol.go` тега v1.8.0).
