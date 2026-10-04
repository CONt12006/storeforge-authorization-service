# StoreForge Authorization Service

Микросервис авторизации на Go для централизованной проверки прав пользователей в StoreForge.

Сервис является продолжением связки из Auth Service и Session Service. Auth Service отвечает за аутентификацию пользователя и выдачу токенов, Session Service на C++ хранит и управляет активными сессиями, а Authorization Service определяет, имеет ли пользователь право выполнить конкретное действие над конкретным ресурсом.

## Возможности

- ролевая модель доступа RBAC;
- политики доступа с элементами ABAC;
- проверка владельца ресурса;
- `allow` и `deny` политики;
- приоритет `deny` над разрешающими правилами;
- wildcard permissions `* / *`;
- несколько ролей у одного пользователя;
- хранение ролей, permissions и policies в PostgreSQL;
- кэширование effective access пользователя в Redis;
- инвалидация кэша при изменении ролей и правил;
- опциональная проверка активной сессии через StoreForge Session Service;
- защита внутренних endpoint'ов через `X-Internal-Api-Key`;
- `/healthz` и `/readyz`;
- graceful shutdown;
- Docker и Docker Compose;
- unit-тесты бизнес-логики и HTTP-клиента Session Service.

## Архитектура

```text
                     ┌──────────────────────┐
                     │        Client        │
                     └──────────┬───────────┘
                                │
                                ▼
                     ┌──────────────────────┐
                     │   Backend / Gateway  │
                     └───────┬──────┬───────┘
                             │      │
                        login│      │authorize
                             │      │
                             ▼      ▼
                 ┌──────────────┐  ┌──────────────────────┐
                 │ Auth Service │  │ Authorization Service│
                 │    Python    │  │          Go          │
                 │ JWT / login  │  │     RBAC / ABAC      │
                 └──────┬───────┘  └──────────┬───────────┘
                        │                     │
                        │ session lifecycle   │ validate session
                        ▼                     ▼
                 ┌─────────────────────────────────────────┐
                 │           Session Service               │
                 │                C++20                    │
                 │ sessions / rotate / revoke / logout-all │
                 └─────────────────────────────────────────┘
```

### Ответственность сервисов

**Auth Service** отвечает за регистрацию, логин, проверку credentials и выдачу access/refresh токенов.

**Session Service** отвечает за активные сессии, устройства, IP/User-Agent, ротацию refresh-токенов, отзыв отдельных сессий и logout-all.

**Authorization Service** отвечает за роли, permissions, policies и итоговое решение `allow / deny`.

## Как принимается решение

Authorization Service получает `user_id`, действие и ресурс. Если включена проверка сессий, дополнительно передаётся `session_id`.

Пример запроса:

```json
{
  "user_id": 42,
  "session_id": "e8f726f9-65ea-4a93-8bbb-bc7c79c1e064",
  "action": "update",
  "resource": {
    "type": "order",
    "id": "1005",
    "owner_id": 42
  }
}
```

Порядок проверки:

```text
request
   │
   ▼
validation
   │
   ▼
session validation
   │
   ▼
Redis cache
   │
   ├── hit ────────────────┐
   │                       │
   └── miss                │
       │                   │
       ▼                   │
   PostgreSQL              │
       │                   │
       ▼                   │
roles + permissions        │
+ policies                 │
       │                   │
       └──────► Redis ◄────┘
                    │
                    ▼
              Policy Engine
                    │
                    ▼
              allow / deny
```

Сначала проверяются подходящие policies. Если найдена подходящая `deny` policy, запрос запрещается независимо от обычных RBAC permissions. Если запрета нет, проверяется прямое permission. Если прямого permission нет, результат может быть разрешён подходящей `allow` policy.

## RBAC

Permission задаётся парой:

```text
resource + action
```

Примеры:

```text
order + read
order + create
order + update
profile + read
```

Для полного доступа поддерживается wildcard:

```text
* + *
```

Начальная миграция создаёт роли:

```text
user
moderator
admin
```

Также создаётся базовый набор permissions для `profile` и `order`.

## ABAC и ownership

Policies позволяют учитывать дополнительные условия при проверке доступа.

Поддерживаемые `condition_type`:

```text
always
owner
```

Пример policy:

```text
role = user
resource = order
action = update
effect = allow
condition_type = owner
```

Такая policy срабатывает только если:

```text
resource.owner_id == user_id
```

Для пользователя `42` этот запрос будет удовлетворять условию владельца:

```json
{
  "user_id": 42,
  "action": "update",
  "resource": {
    "type": "order",
    "id": "1005",
    "owner_id": 42
  }
}
```

А этот — нет:

```json
{
  "user_id": 42,
  "action": "update",
  "resource": {
    "type": "order",
    "id": "1005",
    "owner_id": 71
  }
}
```

## Интеграция с Session Service

По умолчанию проверка сессий отключена:

```env
SESSION_VALIDATION_ENABLED=false
```

Чтобы Authorization Service проверял активность сессии, установите:

```env
SESSION_VALIDATION_ENABLED=true
```

При проверке сервис обращается к C++ Session Service:

```http
GET /v1/users/{user_id}/sessions
X-Internal-Api-Key: <SESSION_SERVICE_API_KEY>
```

Authorization Service ищет переданный `session_id` среди активных сессий пользователя и дополнительно проверяет `expires_at`.

Если сессия отсутствует или истекла:

```json
{
  "allowed": false,
  "reason": "invalid_session"
}
```

Если проверка сессий включена, но `session_id` не передан:

```json
{
  "allowed": false,
  "reason": "session_required"
}
```

Для Session Service, запущенного отдельным Docker Compose проектом на той же машине, можно использовать:

```env
SESSION_SERVICE_URL=http://host.docker.internal:8081
```

В общей Docker-сети или Kubernetes необходимо указать DNS-имя сервиса.

## API

Все endpoint'ы, кроме `/healthz` и `/readyz`, требуют заголовок:

```http
X-Internal-Api-Key: <INTERNAL_API_KEY>
```

### Проверить доступ

```http
POST /v1/authorize
```

Пример:

```bash
curl -X POST http://localhost:8080/v1/authorize \
  -H "Content-Type: application/json" \
  -H "X-Internal-Api-Key: change-me" \
  -d '{
    "user_id": 42,
    "action": "update",
    "resource": {
      "type": "order",
      "id": "1005",
      "owner_id": 42
    }
  }'
```

Разрешение:

```json
{
  "allowed": true,
  "reason": "policy_granted"
}
```

Отказ:

```json
{
  "allowed": false,
  "reason": "insufficient_permissions"
}
```

Возможные `reason`:

```text
permission_granted
policy_granted
policy_denied
invalid_session
session_required
insufficient_permissions
```

### Получить список ролей

```http
GET /v1/roles
```

### Создать роль

```http
POST /v1/roles
```

```json
{
  "name": "support"
}
```

### Получить роли пользователя

```http
GET /v1/users/{user_id}/roles
```

### Заменить роли пользователя

```http
PUT /v1/users/{user_id}/roles
```

```json
{
  "roles": ["user", "moderator"]
}
```

Этот endpoint полностью заменяет текущий набор ролей пользователя переданным списком.

### Добавить permission роли

```http
POST /v1/roles/{role_name}/permissions
```

```json
{
  "resource": "order",
  "action": "update"
}
```

### Добавить policy роли

```http
POST /v1/roles/{role_name}/policies
```

```json
{
  "resource": "order",
  "action": "delete",
  "effect": "allow",
  "condition_type": "owner"
}
```

Допустимые значения `effect`:

```text
allow
deny
```

Допустимые значения `condition_type`:

```text
always
owner
```

## Health checks

### Liveness

```http
GET /healthz
```

Ответ:

```json
{
  "status": "ok"
}
```

`/healthz` проверяет, что процесс HTTP-сервера работает.

### Readiness

```http
GET /readyz
```

`/readyz` проверяет:

- PostgreSQL;
- Redis;
- Session Service, если `SESSION_VALIDATION_ENABLED=true`.

При готовности:

```json
{
  "status": "ready"
}
```

## PostgreSQL

Сервис использует таблицы:

```text
roles
permissions
user_roles
role_permissions
policies
```

Схема связей:

```text
user_id
  │
  ▼
user_roles
  │
  ▼
roles
  ├──────────────► role_permissions ──────► permissions
  │
  └──────────────► policies
```

Таблицы пользователей в этом сервисе нет. `user_id` приходит из Auth Service и используется как внешний идентификатор пользователя без foreign key на отдельную таблицу `users`.

Миграция находится в:

```text
migrations/001_init.sql
```

При запуске через Docker Compose она автоматически применяется PostgreSQL при создании нового volume.

## Redis

Effective access пользователя кэшируется под ключом:

```text
authorization:access:<user_id>
```

В кэш сохраняются permissions и policies пользователя.

TTL задаётся через:

```env
PERMISSIONS_CACHE_TTL=5m
```

При замене ролей пользователя удаляется только его cache key.

При добавлении permission или policy роли выполняется полная инвалидация кэша `authorization:access:*`, потому что изменение роли может затронуть нескольких пользователей.

Если Redis временно недоступен при чтении кэша, сервис пытается получить данные из PostgreSQL. Ошибка записи результата обратно в Redis не блокирует решение авторизации.

## Структура проекта

```text
storeforge-authorization-service/
├── cmd/
│   └── authorization-service/
│       └── main.go
├── internal/
│   ├── cache/
│   │   └── redis.go
│   ├── clients/
│   │   └── session/
│   │       ├── http_client.go
│   │       └── http_client_test.go
│   ├── config/
│   │   └── config.go
│   ├── domain/
│   │   └── models.go
│   ├── repository/
│   │   └── postgres/
│   │       └── repository.go
│   ├── service/
│   │   ├── authorization.go
│   │   └── authorization_test.go
│   └── transport/
│       └── httpserver/
│           └── server.go
├── migrations/
│   └── 001_init.sql
├── .dockerignore
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── Makefile
└── README.md
```

## Технологии

- Go 1.23;
- `net/http`;
- `chi`;
- `pgx`;
- PostgreSQL 17;
- `go-redis`;
- Redis 7;
- Docker;
- Docker Compose.

## Конфигурация

Пример `.env`:

```env
HTTP_ADDRESS=:8080
DATABASE_URL=postgres://storeforge:storeforge@postgres:5432/storeforge_authorization?sslmode=disable
REDIS_ADDRESS=redis:6379
REDIS_PASSWORD=
REDIS_DATABASE=0
INTERNAL_API_KEY=change-me
SESSION_VALIDATION_ENABLED=false
SESSION_SERVICE_URL=http://host.docker.internal:8081
SESSION_SERVICE_API_KEY=change-me
PERMISSIONS_CACHE_TTL=5m
HTTP_CLIENT_TIMEOUT=3s
```

| Переменная | Назначение | Значение по умолчанию |
|---|---|---|
| `HTTP_ADDRESS` | адрес HTTP-сервера | `:8080` |
| `DATABASE_URL` | DSN PostgreSQL | локальная БД `storeforge_authorization` |
| `REDIS_ADDRESS` | адрес Redis | `localhost:6379` |
| `REDIS_PASSWORD` | пароль Redis | пустой |
| `REDIS_DATABASE` | номер Redis DB | `0` |
| `INTERNAL_API_KEY` | ключ внутреннего API | `change-me` |
| `SESSION_VALIDATION_ENABLED` | включить проверку сессий | `false` |
| `SESSION_SERVICE_URL` | URL Session Service | `http://localhost:8081` |
| `SESSION_SERVICE_API_KEY` | API key Session Service | `change-me` |
| `PERMISSIONS_CACHE_TTL` | TTL effective-access cache | `5m` |
| `HTTP_CLIENT_TIMEOUT` | timeout запросов к Session Service | `3s` |

Для реального окружения обязательно замените значения `INTERNAL_API_KEY` и `SESSION_SERVICE_API_KEY`.

## Запуск через Docker Compose

Создайте `.env`:

```bash
cp .env.example .env
```

При необходимости измените ключи и настройки интеграции с Session Service.

Запустите проект:

```bash
docker compose up --build
```

Docker Compose поднимет:

```text
PostgreSQL
Redis
Authorization Service
```

Сервис будет доступен на:

```text
http://localhost:8080
```

Проверка:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Остановка:

```bash
docker compose down
```

Для удаления данных PostgreSQL и Redis:

```bash
docker compose down -v
```

## Локальный запуск

Для локального запуска должны быть доступны PostgreSQL и Redis.

Сначала установите зависимости и сформируйте `go.sum`:

```bash
go mod tidy
```

Запустите сервис:

```bash
go run ./cmd/authorization-service
```

Либо через Makefile:

```bash
make run
```

Сборка бинарника:

```bash
make build
```

Бинарник будет создан в:

```text
bin/authorization-service
```

## Тесты

Запуск всех тестов:

```bash
go test ./...
```

Или:

```bash
make test
```

Сейчас тестами покрыты базовые сценарии:

- прямое RBAC permission;
- ownership policy;
- отказ при недействительной сессии;
- HTTP-клиент интеграции с Session Service.

## Пример сценария использования

1. Пользователь логинится через Auth Service.
2. Auth Service создаёт или обновляет сессию в Session Service.
3. Backend получает `user_id` и `session_id` из доверенного auth-контекста.
4. Перед выполнением защищённой операции Backend вызывает `POST /v1/authorize`.
5. Authorization Service при необходимости проверяет сессию через C++ Session Service.
6. Права пользователя загружаются из Redis или PostgreSQL.
7. Policy Engine принимает решение.
8. Backend выполняет действие только при `allowed: true`.

Пример:

```json
{
  "user_id": 42,
  "session_id": "e8f726f9-65ea-4a93-8bbb-bc7c79c1e064",
  "action": "update",
  "resource": {
    "type": "order",
    "id": "1005",
    "owner_id": 42
  }
}
```

Ответ:

```json
{
  "allowed": true,
  "reason": "policy_granted"
}
```

## Безопасность

Authorization Service рассчитан на внутреннее межсервисное взаимодействие. Пользовательский клиент не должен напрямую обращаться к административным endpoint'ам управления ролями и policies.

Минимальные требования для нормального развёртывания:

- заменить стандартные API keys;
- не публиковать внутренние endpoint'ы напрямую в интернет;
- ограничить сетевой доступ к сервису;
- передавать `user_id` и `session_id` только из доверенного auth-контекста;
- в production использовать защищённое соединение с PostgreSQL, Redis и другими сервисами;
- хранить секреты вне репозитория.

## Связанные сервисы

```text
StoreForge Auth Service          Python / FastAPI
StoreForge Session Service       C++20
StoreForge Authorization Service Go
```

Вместе они разделяют три разные задачи IAM-контура:

```text
Authentication  -> кто пользователь
Session         -> активна ли его сессия
Authorization   -> что ему разрешено делать
```
