# Task Manager API

Go HTTP API with PostgreSQL, MinIO attachments, hosted identity, notifications,
and optional DeepSeek AI. This repository contains the backend only.

## Run locally with Docker Compose

Requirements: Docker with Compose, and access to the hosted GraphQL identity service.

1. Copy the example settings without overwriting an existing local configuration:

   ```sh
   cp -n .env.example .env
   ```

2. Edit `.env`. Set `HOSTED_GRAPHQL_ENDPOINT` to the real service URL. For an existing
   minimal `.env`, add the settings you need; Compose also reads `.env.example` as
   defaults. Set `NOTIFICATION_SERVICE_URL` and `DEEPSEEK_API_KEY` to enable those
   integrations. URLs used by the API must be reachable from its container;
   `localhost` inside the API container refers to that container.

3. Build and start:

   ```sh
   docker compose up --build -d
   docker compose logs -f api migrate
   ```

Compose waits for PostgreSQL and MinIO, runs the migrations, then starts the API.
Migration failure prevents API startup. The services listen on the local machine:
API `http://localhost:8080`, MinIO `http://localhost:9000`, console
`http://localhost:9001`, PostgreSQL port 5432.

`GET /live` checks that the process responds. `GET /health` checks the database and
storage bucket, returning 200 when both are available or 503 otherwise. It does not
check optional providers or the hosted identity service.

```sh
curl --fail http://localhost:8080/health
```

### Local development without the hosted platform

Auth is normally delegated to the hosted platform (`X-APIKEY`/`USER-ID`/`APP-ID`
resolved over its GraphQL profile endpoint). For local client development the
API offers a dev identity resolver, disabled by default:

```sh
IDENTITY_DEV_MODE=true
```

When set, only `USER-ID` (and optionally `APP-ID`, which doubles as the
tenant id) are required — no token, no hosted call. The derived identity is an
admin + support principal, and the tenant member directory still syncs, so a
fresh database works with the SvelteKit UI out of the box. **Never enable
`IDENTITY_DEV_MODE` on a deployed instance.** `MINIO_ENDPOINT` may be left
empty in this mode; attachment endpoints then return an error instead of
failing startup. But `docker compose` sets `MINIO_ENDPOINT`; unset it there
explicitly to use this shortcut.

## Run the API with Go

Requires the Go version in `go.mod`, PostgreSQL 17 and a MinIO instance. Start only
local dependencies, apply migrations, export settings, then run the application:

```sh
docker compose up --build -d postgres minio
docker compose run --rm migrate
set -a
. ./.env.example
. ./.env
set +a
go run .
```

The application reads process environment variables; it does not load `.env`
automatically. Use trusted, shell-compatible `.env` values when sourcing the file.
Local `DATABASE_URL` and `MINIO_ENDPOINT` should refer to `localhost`, unlike the
internal service names used by Compose.

## Migrations and existing databases

SQL migrations live in `db/migrations`. Compose uses the
[golang-migrate CLI](https://github.com/golang-migrate/migrate) pinned to v4.20.1.
For an externally managed database with the CLI installed:

```sh
migrate -path db/migrations -database "$DATABASE_URL" up
migrate -path db/migrations -database "$DATABASE_URL" version
```

A database whose schema was created manually must be reconciled with migration
history before using `up`. Do not blindly force a migration version. Migration 9
intentionally refuses inconsistent historical references. Back up PostgreSQL and
MinIO data before upgrading a deployed instance; a new app image alone does not
apply production migrations.

## Tests

```sh
go test ./...
go vet ./...
go test -race ./internal/notification
```

Database tests skip unless `TASKMANAGER_TEST_DATABASE_URL` is set. Run them against
a disposable test database: they create and drop temporary schemas, and require
schema creation and extension privileges. Do not point them at production.

```sh
TASKMANAGER_TEST_DATABASE_URL='postgres://taskmanager:taskmanager@localhost:5432/taskmanager_test?sslmode=disable' go test ./...
```

Create `taskmanager_test` before running that command. Regenerate database bindings
with `sqlc generate` after modifying queries.

## Authentication and features

Protected endpoints require `X-APIKEY`, `USER-ID`, and `APP-ID`, validated against
the hosted GraphQL service. `FROM-SUPPORT-MENU` is optional. Project membership and
roles control access; user IDs in application payloads do not replace authentication.

- Projects, memberships, configurable statuses and tasks.
- Comments, watchers, attachments, dependencies, search and dashboard.
- Chat under `/conversations` and `/projects/{projectID}/chat`: one decision
  thread per project, direct messages (a single thread per user pair) and group
  conversations. `GET /conversations` includes unread counts and the last
  message; `GET /conversations/{id}/messages?before=<cursor>` pages history by
  the monotonic message sequence; `POST /conversations/{id}/read` advances the
  per-member read cursor. Project chat membership follows project membership.
- Time tracking under `/projects/{projectID}/tasks/{taskID}/time-entries`:
  `POST /start`, `POST /stop`, `GET /`, `DELETE /{entryID}`. Each user has one active
  timer per tenant. Only the owner can stop a timer or delete their completed entry.
- Archived lists use `?archived=true`. Restore with
  `POST /projects/{projectID}/restore` or
  `POST /projects/{projectID}/tasks/{taskID}/restore`.
- [DeepSeek setup and AI endpoints](docs/ai.md).
- [MCP setup for Codex and Claude Code](docs/mcp.md): read and claim tasks,
  post results and update status through a local stdio server.

## Deployment settings

Compose is a local development stack with development credentials. For deployment,
use managed secrets, real database/storage credentials and an HTTPS reverse proxy.
MinIO TLS is configurable with `MINIO_USE_SSL`. `MINIO_PUBLIC_URL` is the externally
reachable root URL used for signed downloads; it may differ from the internal
`MINIO_ENDPOINT`. The signer uses MinIO's default `us-east-1` region. Expose the
public hostname without rewriting the signed path or query.

`.env` is ignored and excluded from Docker build context. Removing it from tracking
does not remove earlier Git history; rotate any real credential that was previously
committed. Notifications use an in-memory bounded queue with limited retries; a
process crash can lose pending notifications.
