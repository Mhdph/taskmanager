# DeepSeek AI

Configure environment variables before starting the API:

```sh
DEEPSEEK_API_KEY=<your-secret-key>
DEEPSEEK_BASE_URL=https://api.deepseek.com
DEEPSEEK_MODEL=deepseek-flash
```

The official model alias `deepseek-flash` serves DeepSeek V4.1 Flash:
https://api-docs.deepseek.com/guides/codex

The base URL and model are optional and default to the values above. Without an
API key, AI endpoints return 503; the other task manager features still work.
Do not commit API keys. This application reads process environment variables;
it does not load `.env` files automatically.

- `POST /ai/tasks/from-text` with `{"text":"..."}` returns a task draft.
- `POST /projects/{projectID}/tasks/{taskID}/ai/summary` returns a summary.
- `POST /projects/{projectID}/tasks/{taskID}/ai/suggestions` returns proposed changes.
- `POST /projects/{projectID}/ai/summary` summarizes up to 500 recently updated tasks.

Results never automatically create or modify tasks. Task context includes up to
100 comments; summaries and suggestions require permission to read those comments.
The task and authorized comments are sent to DeepSeek when those endpoints are used.

Requests use non-thinking JSON mode, a 25-second HTTP timeout and a 4096-token
output limit. Invalid or truncated output is rejected. Automatic retries are not
used, to avoid duplicate charges. Tests use a fake HTTP transport; no live API
request is made by the test suite.
