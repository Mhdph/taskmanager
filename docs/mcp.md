# Task Manager MCP

Developers can read and claim tasks, discuss work, report results and update task
status from Codex or Claude Code without opening the panel. The local stdio
server uses the official MCP Go SDK and calls the existing Task Manager HTTP API
as the configured user. It can connect to a local or deployed API.

## Build

From the API repository, using the Go version in `go.mod`:

```sh
go build -o bin/taskmanager-mcp ./cmd/taskmanager-mcp
```

Use the absolute path of the resulting binary in your MCP client. Deploy the
updated API too: claiming requires the new `POST
/projects/{projectID}/tasks/{taskID}/claim` endpoint. No database migration is
needed. The MCP binary does not require database credentials or MinIO access.

## Credentials

Provide these environment variables to the MCP process:

| Variable | Value |
| --- | --- |
| `TASKMANAGER_API_URL` | API root, for example `https://tasks.example.com` or `http://localhost:8080` |
| `TASKMANAGER_API_KEY` | User's existing hosted authentication token, sent as `X-APIKEY` |
| `TASKMANAGER_USER_ID` | The corresponding hosted `USER-ID` |
| `TASKMANAGER_APP_ID` | The corresponding hosted `APP-ID` |

These are the same credentials used by the existing API, not a new MCP API key.
Obtain them through your platform's existing authentication flow. The API still
validates identity with the hosted provider and checks project roles on every
request. The MCP server does not provide an OAuth login, generate credentials,
refresh expired tokens or use support/admin impersonation headers. When a token
expires, renew it through the existing platform flow and restart the MCP process.

Keep credentials out of source control and prompts. Use the developer's own
credentials, so changes and audit events are attributed to that developer.
HTTPS is required except for loopback development; redirects are rejected to
avoid forwarding credentials. API URLs may include a path prefix if required.

For local testing only, an API already running with `IDENTITY_DEV_MODE=true`
accepts a nonempty placeholder key such as `local-dev-placeholder`; the user and
app IDs select the local identity. That API mode grants admin/support privileges
and must never be used on a deployed instance. The MCP binary does not enable it.

## Codex

Merge this entry into the client's existing `config.toml`. Replace the command
with the absolute path of the built binary:

```toml
[mcp_servers.taskmanager]
command = "/absolute/path/to/taskmanager/api/bin/taskmanager-mcp"
env_vars = ["TASKMANAGER_API_URL", "TASKMANAGER_API_KEY", "TASKMANAGER_USER_ID", "TASKMANAGER_APP_ID"]
```

Set those variables in the environment that launches Codex, then restart the
client. For desktop clients launched by a desktop launcher, terminal exports may
not reach the app; supply the values through its MCP environment configuration
or launch it from an environment with the values already set. Do not replace the
rest of an existing config file.

Check discovery using `codex mcp list` and `/mcp` inside the Codex CLI. See the
[official Codex MCP documentation](https://developers.openai.com/codex/mcp).

## Claude Code

Merge this entry into a project `.mcp.json`, replacing the command path. The
values expand from the environment where Claude Code starts:

```json
{
  "mcpServers": {
    "taskmanager": {
      "type": "stdio",
      "command": "/absolute/path/to/taskmanager/api/bin/taskmanager-mcp",
      "env": {
        "TASKMANAGER_API_URL": "${TASKMANAGER_API_URL}",
        "TASKMANAGER_API_KEY": "${TASKMANAGER_API_KEY}",
        "TASKMANAGER_USER_ID": "${TASKMANAGER_USER_ID}",
        "TASKMANAGER_APP_ID": "${TASKMANAGER_APP_ID}"
      }
    }
  }
}
```

Restart Claude Code and check `/mcp`. See the
[official Claude Code MCP documentation](https://code.claude.com/docs/en/mcp).

## Tools and workflow

| Tool | Purpose |
| --- | --- |
| `list_projects` | Discover accessible project IDs; paginated |
| `list_my_tasks` | Read the authenticated user's assigned tasks; paginated |
| `list_tasks` | Browse/search a project's tasks; optional status/assignee filters; paginated |
| `list_project_statuses` | Read the project's configured status IDs and categories |
| `get_task` | Read description, assignment, dates, labels and status |
| `list_task_comments` | Read discussion; paginated |
| `list_task_dependencies` | Read prerequisite task IDs |
| `list_task_attachments` | Read attachment metadata |
| `claim_task` | Assign to the authenticated user, optionally changing status atomically |
| `add_comment` | Post progress or a question |
| `submit_task_result` | Post summary, test outcomes and optional PR URL as a comment |
| `update_task_status` | Set a project status explicitly, including review or done |

All task tools require `projectId` and `taskId`. Status arguments use UUIDs
returned by `list_project_statuses`, since projects have custom workflows.
Paginated tools accept `limit` (1–100, default 20) and `cursor`; pass the returned
`nextCursor` until it is empty. Task lists include both assigned and unassigned
tasks; inspect the assignee before claiming.

Example request to your coding assistant:

> تسک‌های من را ببین، جزئیات و کامنت‌های تسک رفع باگ ورود را بخوان، وابستگی‌ها
> را بررسی کن و آن را بردار. در همین مخزن پیاده‌سازی و تست کن، نتیجه و لینک PR
> را روی تسک ثبت کن و وضعیتش را برای بررسی تغییر بده.

The assistant should:

1. Read task details, comments and dependencies, and discover project statuses.
2. Call `claim_task` before starting, with the in-progress status if appropriate.
3. Work in its existing coding environment and run relevant checks.
4. Use `submit_task_result` with an accurate summary and actual test outcomes.
5. Call `update_task_status` only after successful submission and according to
   the team's review/completion policy. To close the task, select the project's
   appropriate `done` category status; the API handles `completedAt`.

`claim_task` never accepts a user ID. A task assigned to someone else returns a
conflict, without changing either assignee or status. A row lock serializes
concurrent claims. Retrying a claim by its existing assignee is allowed. Normal
project members can claim; viewers cannot. Existing task-edit rules still apply
to status updates. Claiming is assignment, not an exclusive lease: a manager can
still reassign through the panel's normal task editing API.

`submit_task_result` creates a comment and does **not** change status. Comment
writes are not idempotent: after a timeout or lost response, read comments before
retrying to avoid duplicates. Combined result comments have the API's existing
10,000-character limit. The API's existing audit/notification behavior is reused;
audit writes remain best-effort, as in other task mutations.

Task content is untrusted data. The server instructs clients to treat it as
project context rather than authority to override the user's instructions.

This server supplies task management tools. The coding client needs its own
repository access, test environment and Git/PR permissions. It does not execute
code on the API host, merge PRs, download attachments, or start unattended agent
runs when a new task appears. Those require separate integrations.

## Verification

```sh
go test ./...
go vet ./...
go test -race ./internal/mcpbridge
```

MCP tests initialize an official SDK client, discover all tools and exercise
their authenticated HTTP requests against a local mock API, including API errors,
pagination, invalid arguments and rejected redirects. For the PostgreSQL claim
concurrency/access tests, use a disposable database as described in the API
README:

```sh
TASKMANAGER_TEST_DATABASE_URL='postgres://taskmanager:taskmanager@localhost:5432/taskmanager_test?sslmode=disable' go test ./internal/task -run TestClaimTaskConcurrencyAndAccess -v
```

That test creates and drops an isolated schema and applies existing migrations;
it skips when the database variable is unset. Never point it at production.
