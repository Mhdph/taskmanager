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

## NUP platform sign-in

The standalone UI uses the same bridge-token and session-header contract as the
NUP admin app. `POST /auth/bridge` accepts `{"bridgeToken":"..."}` without existing
session headers, exchanges it via `nupConfirmBridgeToken` at
`HOSTED_GRAPHQL_ENDPOINT`, and returns `token`, `appId`, `userId`, and
`fromSupportMenu`. Invalid tokens return 401; an unavailable provider returns
502. Request JSON is bounded, bridge tokens are limited to 8192 bytes, and
responses are never cached. Redirects from the identity provider are rejected
so credentials cannot be forwarded to another host.

The UI stores the platform session cookies and forwards `X-APIKEY`, `USER-ID`,
`APP-ID`, and `FROM-SUPPORT-MENU` on authenticated requests. Support-menu values
`yes`/`no` and boolean strings are accepted after hosted profile validation.
`APP_ENV=production` (or staging) refuses `IDENTITY_DEV_MODE=true`; the dev bypass
is allowed only in development and test. All API responses use `Cache-Control:
no-store` and `X-Content-Type-Options: nosniff`.

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
- [Member capacity and workload](docs/member-capacity.md): configure working
  days, daily hours and time zone under `/members/{userID}/capacity`; read
  manager-only `/workload` and personal `/members/{userID}/workload` reports.
  Reports show remaining estimated work, daily overload, unscheduled and
  unestimated tasks. Apply migration 24.
- [Task delivery and approval](docs/task-deliveries.md) under
  `/projects/{projectID}/tasks/{taskID}/deliveries`: versioned submissions,
  designated reviewer approval or changes with a required reason, withdrawal,
  paginated history and transactional activity records. Apply migration 22.
- [Configurable personal task reminders](docs/task-reminders.md) under
  `/projects/{projectID}/tasks/{taskID}/reminders`: absolute or before-due times,
  multiple reminders, snooze and cancellation. A database-backed worker retries
  provider failures with a stable idempotency key. Apply migration 23 and set
  `NOTIFICATION_SERVICE_URL` to enable delivery.
- Chat under `/conversations` and `/projects/{projectID}/chat`: one decision
  thread per project, direct messages (a single thread per user pair) and group
  conversations. `GET /conversations` includes unread counts and the last
  message; `GET /conversations/{id}/messages?before=<cursor>` pages older
  history by the monotonic message sequence; `?after=<seq>` replays messages
  newer than a known sequence, oldest first, and returns `nextAfter` while
  more remain (`before` and `after` are mutually exclusive). `POST
  /conversations/{id}/read` advances the per-member read cursor. Project chat
  membership follows project membership.
- Chat writes and real-time delivery:
  - `POST /conversations/{id}/messages` with an optional `clientMessageId`.
    Retrying the same id returns the stored message with `200` instead of
    duplicating it; a new message returns `201`.
  - `PATCH /conversations/{id}/messages/{messageID}` (author only) and
    `DELETE /conversations/{id}/messages/{messageID}` (author or conversation
    owner) edit or tombstone a message.
  - `GET /chat/stream` upgrades to a WebSocket using the same authentication
    headers, or a one-time ticket for browser clients. `POST /chat/stream-token`
    (normal auth headers) returns `{token, expiresAt}` with
    `Cache-Control: no-store`; open `GET /chat/stream?token=...` within 60
    seconds. Only the ticket hash is stored, and consuming it deletes the row
    atomically, so a ticket works once across API instances and replayed or
    expired tickets get `401`. Fetch a fresh ticket for every reconnect.
    Tickets are bearer credentials in a query string: avoid logging stream
    URLs and never cache them. Header authentication keeps working. It pushes
    `message.created`, `message.edited`, `message.deleted` and
    `conversation.read` frames after a `hello` frame. Clients that miss frames
    (offline, slow consumer, reconnect) resync through the REST history using
    `seq`. Fan-out uses Postgres `LISTEN/NOTIFY`, so multiple API instances
    stay in sync. Message inserts for one conversation are serialized with a
    transaction-scoped advisory lock, so `seq` assignment order matches commit
    order and `after` replay cannot skip a lower sequence that committed
    late. On reconnect, open the socket first and then replay with
    `after=<lastSeq>`, deduping overlapping frames and rows by `seq`; fetching
    first leaves a small window where a message commits between the query and
    the handshake. `after` covers newly created messages only: edits and
    deletes keep the original `seq`, so a client that needs those should
    reload the latest history page. `CHAT_WS_ORIGINS` lists allowed
    cross-origin hosts; the request host is always accepted.
  - Typing indicators: send `{"type":"typing","conversationId":"<id>",
    "typing":true}` over the stream. The server validates membership, throttles
    repeated start events to one every two seconds, and pushes
    `{"type":"typing","conversationId":"<id>","userId":"...","typing":true}`
    to the other conversation members (also across API instances). Send
    `typing:false` when the composer empties or loses focus, and expire the
    indicator client-side after a few seconds without a refresh in case a stop
    frame is lost.
  - Presence: `GET /conversations/{id}/presence` returns the member ids with a
    live stream connection on any API instance, and the stream pushes
    `presence.online` / `presence.offline` frames for users who share a
    conversation. Each WebSocket connection owns a Postgres presence lease,
    refreshed by the 30-second ping and expiring after 90 seconds. Per-user
    advisory locks make connect, release and expiry reconcile the global
    online state once, then publish the transition through the broker; every
    instance re-checks the lease before fanning a frame out, so a rapid
    reconnect cannot deliver an out-of-order final state. Surviving instances
    sweep expired leases left by a crashed process, and graceful shutdown
    releases leases immediately. A hard crash can look online until its leases
    expire; message history replay still covers anything missed in that
    window.
  - Notification preferences: `GET /conversations/{id}/notification-preference`
    returns the caller's mode and `PUT` stores it with
    `{"mode":"all|mentions|mute"}`. Preferences are per conversation and
    per member; `all` is the default. `mentions` notifies only when the
    member is mentioned, `mute` silences the conversation. Muting only
    affects push notifications: the WebSocket stream still delivers every
    message, and unread counts are unchanged.
  - Mentions: a message body mentions a member with `@` followed by that
    member's user id (`@alice`). Mentions are resolved once, against the
    conversation members, when the message is written, and re-resolved when
    the message is edited. The resolved list is stored on the message and
    returned as `mentionedUserIds` in REST history and WebSocket frames.
    A mention must be a standalone word: `alice@example.com` and `@alice2`
    are not mentions, and the longest matching id wins (`@alice.bob` mentions
    `alice.bob`, not `alice`).
  - Offline notifications: after a message commits, every member without a
    live lease on any API instance gets a `chat.message` notification through
    `NOTIFICATION_SERVICE_URL`, with the project, conversation and message ids,
    the actor, a title and a 200-character preview (`Sent an attachment` for
    attachment-only messages). The author is skipped, online members are
    skipped, muted members are skipped, mention-only members are skipped
    unless the message mentions them, and idempotent retries notify only
    once. Presence is one batched shared-store query per message, and if that
    query fails the notification is still sent (fail open) rather than
    silently dropped. Delivery is best-effort through the bounded dispatcher,
    so a full queue or a failing notification service never fails the message
    write. Chat notifications stay disabled when `NOTIFICATION_SERVICE_URL` is
    empty.
  - Attachments are uploaded before the message:
    1. `POST /conversations/{id}/attachments` with multipart field `file`
       returns pending attachment metadata (same size limit as task
       attachments).
    2. Send a message with `attachmentIds` (up to 10). Binding happens in the
       same transaction as the message insert, so a message and its files are
       atomic and an upload cannot be used twice. The body may be empty when
       attachments are present.
    3. `GET /conversations/{id}/attachments/{attachmentID}/download` returns a
       five-minute presigned URL; only conversation members can request it.
    4. `DELETE /conversations/{id}/attachments/{attachmentID}` cancels a
       pending upload (uploader only, before it is sent).
    Message payloads in REST history and WebSocket events include an
    `attachments` array. Deleting a message deletes its attachment metadata
    and storage objects. Pending uploads that are never sent are swept after
    24 hours. Files are served with an attachment disposition, and filenames
    are sanitized before they reach a response header.
  - Storage reconciliation: a background job lists the attachment objects the
    application owns (`tenants/{tenant}/chat/...` and
    `tenants/{tenant}/projects/.../tasks/...`) and removes the ones that no
    live attachment row references any more. It runs at startup and hourly,
    so a crash, a failed cleanup or a cascading database delete (a deleted
    task, conversation or project) cannot leak objects in the bucket.
    Soft-deleted attachment rows do not count as references, because the
    download endpoints already filter them out. Objects modified in the last
    ten minutes are left alone, which protects an upload that has not been
    written to the database yet, and objects outside the attachment layout
    are never touched.
- Sprints under `/projects/{projectID}/sprints`: a project keeps its planning
  in sprints with an explicit `planned -> active -> completed` lifecycle.
  Starting and completing are explicit actions; the planned end date never
  closes a sprint by itself. A project has at most one active sprint
  (enforced by the database) and any number of planned or completed ones.
  - `GET /sprints?state=planned|active|completed` lists the sprints,
    `POST /sprints` creates a planned one (`name`, optional `goal`,
    `plannedStartAt`, `plannedEndAt`).
  - `GET /sprints/{sprintID}` returns the sprint with its live totals,
    `PATCH /sprints/{sprintID}` edits it and `DELETE /sprints/{sprintID}`
    removes a planned sprint and returns its tasks to the backlog. A planned
    sprint is fully editable, an active sprint only accepts a new name,
    goal or planned end date, and a completed sprint is immutable.
  - `POST /sprints/{sprintID}/start` starts the sprint and snapshots the
    tasks it committed to. `POST /sprints/{sprintID}/complete` freezes the
    report and moves the unfinished work to `carryoverSprintId` (a planned
    sprint of the same project) or back to the backlog when it is null.
    Repeating either action returns the existing result and never carries
    work over twice; an impossible transition is a `409`.
  - `GET /sprints/{sprintID}/report` returns the live summary of a planned
    or active sprint and the frozen report of a completed one. The report
    separates the initial commitment, the work added and removed while the
    sprint ran, the completed work, the unfinished carryover and the
    archived work, and it counts each task's own estimate once.
  - `PUT /projects/{projectID}/tasks/{taskID}/sprint` with
    `{"sprintId": "<uuid>"}` plans a task into a sprint and
    `{"sprintId": null}` returns it to the backlog. Task assignment has its
    own endpoint so the planning rules live in one place. Only unfinished,
    non-archived tasks can be moved; finished tasks keep the sprint they
    were completed in, and archived tasks keep their assignment for
    traceability. Moving a task never moves its subtasks.
  - Task listings accept `?sprintId=<uuid>` and `?backlog=true` (the
    unfinished tasks without a sprint) on the project list, the workspace
    list and My Work; using both at once is a `400`. Saved views can
    persist a `sprint` filter with a sprint id or the value `backlog`.
  - Permissions: managers administer sprints, members change the task
    membership, viewers read them.
- Time tracking under `/projects/{projectID}/tasks/{taskID}/time-entries`:
  `POST /start`, `POST /stop`, `GET /`, `DELETE /{entryID}`. Each user has one active
  timer per tenant. Only the owner can stop a timer or delete their completed entry.
- Automation rules under `/projects/{projectID}/automation-rules`:
  `GET /` lists them, `POST /` creates one, `PATCH /{ruleID}` edits or
  enables/disables it and `DELETE /{ruleID}` removes it. A rule has one
  trigger and one to five ordered actions.
  - Triggers: `task_status_changed` with optional `fromStatusId` and
    `toStatusId`; `task_created` with optional `hasAssignee`;
    `task_assigned` with optional `toUserId`; and `task_due` with
    `daysBefore` (1–30, default 1).
  - Actions: `set_assignee` assigns a fixed project member
    (`{"assignee":"user","userId":"bob"}`) or the task creator
    (`{"assignee":"creator"}`); `notify` sends a notification to any of
    `assignee`, `creator`, `watchers` and explicit member ids, with an
    optional `title` and `body`. Rule notifications reach
    `NOTIFICATION_SERVICE_URL` with type `automation.triggered`.
  - Example: when a task enters review, assign it to a reviewer and notify
    them:

    ```json
    {
      "name": "Request review",
      "triggerType": "task_status_changed",
      "triggerConfig": {"toStatusId": "<review-status-id>"},
      "actions": [
        {"type": "set_assignee", "assignee": "user", "userId": "bob"},
        {"type": "notify", "recipients": ["assignee"]}
      ]
    }
    ```

  - Task events are evaluated asynchronously after the write and never
    fail it. An action applied by a rule is not evaluated again, so rules
    cannot cascade. The due sweep runs at startup and every
    `AUTOMATION_SWEEP_INTERVAL` (default `15m`); it reminds once per rule,
    task and due date, and again only when the deadline changes. It looks
    at upcoming deadlines, not tasks that are already overdue.
  - Permissions: managers create, edit and delete rules; members and
    viewers read them. `AUTOMATION_MAX_RULES_PER_PROJECT` (default 20)
    caps stored rules per project; creating one more returns `409`.
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
