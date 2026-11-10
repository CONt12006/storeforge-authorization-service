<p align="center">
  <a href="README.md"><img alt="English" src="https://img.shields.io/badge/English-1f6feb?style=for-the-badge"></a>
  <a href="README.ru.md"><img alt="Русский" src="https://img.shields.io/badge/%D0%A0%D1%83%D1%81%D1%81%D0%BA%D0%B8%D0%B9-555555?style=for-the-badge"></a>
</p>

# StoreForge Authorization Service

A Go authorization microservice for centralised user permission checks in StoreForge.

The service extends the pair of Auth Service and Session Service. Auth Service authenticates users and issues tokens, the C++ Session Service stores and manages active sessions, and Authorization Service decides whether a user may perform a specific action on a specific resource.

## Features

- RBAC role model;
- access policies with ABAC elements;
- resource ownership checks;
- `allow` and `deny` policies;
- `deny` takes precedence over allowing rules;
- wildcard permissions `* / *`;
- multiple roles per user;
- roles, permissions and policies stored in PostgreSQL;
- caching of each user's effective access in Redis;
- cache invalidation when roles and rules change;
- optional active-session check via StoreForge Session Service;
- internal endpoints protected with `X-Internal-Api-Key`;
- `/healthz` and `/readyz`;
- graceful shutdown;
- Docker and Docker Compose;
- unit tests for the business logic and the Session Service HTTP client.

## Architecture

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

### Service responsibilities

**Auth Service** handles registration, login, credential checks and issuing access/refresh tokens.

**Session Service** handles active sessions, devices, IP/User-Agent, refresh-token rotation, revoking individual sessions and logout-all.

**Authorization Service** handles roles, permissions, policies and the final `allow / deny` decision.

## How a decision is made

Authorization Service receives a `user_id`, an action and a resource. If session checks are enabled, a `session_id` is passed as well.

Example request:

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

Check order:

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

Matching policies are checked first. If a matching `deny` policy is found, the request is denied regardless of regular RBAC permissions. If there is no deny, the direct permission is checked. If there is no direct permission, the request can still be allowed by a matching `allow` policy.

## RBAC

A permission is a pair:

```text
resource + action
```

Examples:

```text
order + read
order + create
order + update
profile + read
```

Full access is supported with a wildcard:

```text
* + *
```

The initial migration creates the roles:

```text
user
moderator
admin
```

It also creates a base set of permissions for `profile` and `order`.

## ABAC and ownership

Policies let access checks take extra conditions into account.

Supported `condition_type` values:

```text
always
owner
```

Example policy:

```text
role = user
resource = order
action = update
effect = allow
condition_type = owner
```

This policy applies only if:

```text
resource.owner_id == user_id
```

For user `42`, this request satisfies the ownership condition:

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

And this one does not:

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

## Session Service integration

Session checks are disabled by default:

```env
SESSION_VALIDATION_ENABLED=false
```

To make Authorization Service check that the session is active, set:

```env
SESSION_VALIDATION_ENABLED=true
```

During the check the service calls the C++ Session Service:

```http
GET /v1/users/{user_id}/sessions
X-Internal-Api-Key: <SESSION_SERVICE_API_KEY>
```

Authorization Service looks up the given `session_id` among the user's active sessions and also checks `expires_at`.

If the session is missing or expired:

```json
{
  "allowed": false,
  "reason": "invalid_session"
}
```

If session checks are enabled but no `session_id` is passed:

```json
{
  "allowed": false,
  "reason": "session_required"
}
```

For a Session Service running as a separate Docker Compose project on the same machine, you can use:

```env
SESSION_SERVICE_URL=http://host.docker.internal:8081
```

In a shared Docker network or Kubernetes, set the service's DNS name.

## API

All endpoints except `/healthz` and `/readyz` require the header:

```http
X-Internal-Api-Key: <INTERNAL_API_KEY>
```

### Check access

```http
POST /v1/authorize
```

Example:

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

Allowed:

```json
{
  "allowed": true,
  "reason": "policy_granted"
}
```

Denied:

```json
{
  "allowed": false,
  "reason": "insufficient_permissions"
}
```

Possible `reason` values:

```text
permission_granted
policy_granted
policy_denied
invalid_session
session_required
insufficient_permissions
```

### List roles

```http
GET /v1/roles
```

### Create a role

```http
POST /v1/roles
```

```json
{
  "name": "support"
}
```

### Get a user's roles

```http
GET /v1/users/{user_id}/roles
```

### Replace a user's roles

```http
PUT /v1/users/{user_id}/roles
```

```json
{
  "roles": ["user", "moderator"]
}
```

This endpoint fully replaces the user's current set of roles with the given list.

### Add a permission to a role

```http
POST /v1/roles/{role_name}/permissions
```

```json
{
  "resource": "order",
  "action": "update"
}
```

### Add a policy to a role

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

Allowed `effect` values:

```text
allow
deny
```

Allowed `condition_type` values:

```text
always
owner
```

## Health checks

### Liveness

```http
GET /healthz
```

Response:

```json
{
  "status": "ok"
}
```

`/healthz` checks that the HTTP server process is running.

### Readiness

```http
GET /readyz
```

`/readyz` checks:

- PostgreSQL;
- Redis;
- Session Service, if `SESSION_VALIDATION_ENABLED=true`.

When ready:

```json
{
  "status": "ready"
}
```

## PostgreSQL

The service uses the tables:

```text
roles
permissions
user_roles
role_permissions
policies
```

Relationships:

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

There is no users table in this service. `user_id` comes from Auth Service and is used as an external user identifier, without a foreign key to a separate `users` table.

The migration is located in:

```text
migrations/001_init.sql
```

When running via Docker Compose, PostgreSQL applies it automatically when a new volume is created.

## Redis

A user's effective access is cached under the key:

```text
authorization:access:<user_id>
```

The cache stores the user's permissions and policies.

The TTL is set with:

```env
PERMISSIONS_CACHE_TTL=5m
```

Replacing a user's roles deletes only that user's cache key.

Adding a permission or policy to a role fully invalidates the `authorization:access:*` cache, because a role change can affect several users.

If Redis is temporarily unavailable when reading the cache, the service falls back to PostgreSQL. A failure to write the result back to Redis does not block the authorization decision.

## Project structure

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
├── README.md
└── README.ru.md
```

## Tech stack

- Go 1.23;
- `net/http`;
- `chi`;
- `pgx`;
- PostgreSQL 17;
- `go-redis`;
- Redis 7;
- Docker;
- Docker Compose.

## Configuration

Example `.env`:

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

| Variable | Purpose | Default |
|---|---|---|
| `HTTP_ADDRESS` | HTTP server address | `:8080` |
| `DATABASE_URL` | PostgreSQL DSN | local `storeforge_authorization` DB |
| `REDIS_ADDRESS` | Redis address | `localhost:6379` |
| `REDIS_PASSWORD` | Redis password | empty |
| `REDIS_DATABASE` | Redis DB number | `0` |
| `INTERNAL_API_KEY` | internal API key | `change-me` |
| `SESSION_VALIDATION_ENABLED` | enable session checks | `false` |
| `SESSION_SERVICE_URL` | Session Service URL | `http://localhost:8081` |
| `SESSION_SERVICE_API_KEY` | Session Service API key | `change-me` |
| `PERMISSIONS_CACHE_TTL` | effective-access cache TTL | `5m` |
| `HTTP_CLIENT_TIMEOUT` | timeout for Session Service requests | `3s` |

In a real environment, be sure to replace `INTERNAL_API_KEY` and `SESSION_SERVICE_API_KEY`.

## Running with Docker Compose

Create `.env`:

```bash
cp .env.example .env
```

Change the keys and the Session Service integration settings if needed.

Start the project:

```bash
docker compose up --build
```

Docker Compose starts:

```text
PostgreSQL
Redis
Authorization Service
```

The service is available at:

```text
http://localhost:8080
```

Check:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Stop:

```bash
docker compose down
```

To delete PostgreSQL and Redis data:

```bash
docker compose down -v
```

## Running locally

PostgreSQL and Redis must be available to run locally.

First install dependencies and generate `go.sum`:

```bash
go mod tidy
```

Start the service:

```bash
go run ./cmd/authorization-service
```

Or via the Makefile:

```bash
make run
```

Build the binary:

```bash
make build
```

The binary is created at:

```text
bin/authorization-service
```

## Tests

Run all tests:

```bash
go test ./...
```

Or:

```bash
make test
```

Tests currently cover the basic scenarios:

- direct RBAC permission;
- ownership policy;
- denial on an invalid session;
- the Session Service integration HTTP client.

## Example usage flow

1. The user logs in via Auth Service.
2. Auth Service creates or updates the session in Session Service.
3. The backend gets `user_id` and `session_id` from a trusted auth context.
4. Before a protected operation, the backend calls `POST /v1/authorize`.
5. Authorization Service checks the session via the C++ Session Service if needed.
6. The user's permissions are loaded from Redis or PostgreSQL.
7. The Policy Engine makes the decision.
8. The backend performs the action only if `allowed: true`.

Example:

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

Response:

```json
{
  "allowed": true,
  "reason": "policy_granted"
}
```

## Security

Authorization Service is designed for internal service-to-service communication. User-facing clients must not call the administrative endpoints for managing roles and policies directly.

Minimum requirements for a proper deployment:

- replace the default API keys;
- do not expose internal endpoints directly to the internet;
- restrict network access to the service;
- pass `user_id` and `session_id` only from a trusted auth context;
- in production, use secure connections to PostgreSQL, Redis and other services;
- keep secrets out of the repository.

## Related services

```text
StoreForge Auth Service          Python / FastAPI
StoreForge Session Service       C++20
StoreForge Authorization Service Go
```

Together they split three different IAM concerns:

```text
Authentication  -> who the user is
Session         -> whether their session is active
Authorization   -> what they are allowed to do
```
