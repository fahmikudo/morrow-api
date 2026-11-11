# Morrow API

Go 1.26 API backed by PostgreSQL 16. It implements the repository contract the
frontend already uses; the browser stops being the source of truth.

Part of the `second-brain` workspace; the frontend lives in `../morrow-web`
and the MCP server in `../second-brain-mcp`.

## Layering

```
internal/module/<feature>/   model.go      wire + persisted shapes, payloads, body policy
                             repository.go every SQL statement; reads on the pool, writes on a tx
                             service.go    validation, key/id allocation, timestamps, transactions
                             handler.go    status codes and JSON
                             router.go     the feature's routes
internal/platform/           httpx (error envelope, JSON decode, middleware, router)
                             wiretime (timestamp/date JSON), pgarray, ids, keygen, htmltext, tokens
internal/bootstrap/          the only package that knows the whole graph
migrations/                  golang-migrate SQL, embedded into the binaries
cmd/server, cmd/migrate      the two executables
```

Request flow is strictly `handler → service → repository`. A service owns its
transaction, so handlers stay small and repositories stay reusable. Wire structs
live next to the feature that owns them; the snapshot and importer compose them.

Why Go: the team reads Go faster than Rust/Axum. Behaviour is unchanged — the
26 route tests and 12 fixture round-trips were ported from the previous
implementation, so the wire contract is pinned by the same assertions.

## Error envelope

Every non-2xx response — including unknown routes and body parse failures — has
the same shape:

```json
{ "error": "Title is required", "code": "validation_error" }
```

`error` is the human-readable message the frontend shows; `code` is the stable
machine value: `validation_error`, `unauthorized`, `not_found`, `conflict`,
`internal_error`. Status codes stay 400/401/404/409/500 respectively, and
internal details are logged server-side, never returned.

## Requirements

- Go 1.26+
- PostgreSQL 16 (or the bundled Docker Compose stack)

## Configuration

Copy `.env.example` to `.env` and adjust. Exported environment variables always
win over the file.

| Variable | Required | Default | Notes |
|---|---|---|---|
| `DATABASE_URL` | yes | `postgres://secondbrain:secondbrain@127.0.0.1:5433/secondbrain` | PostgreSQL connection string |
| `BIND_ADDR` | no | `127.0.0.1:8080` | Listen address |
| `CORS_ORIGIN` | no | `http://localhost:5173` | Comma-separated allowed origins |
| `TOKEN_TTL_DAYS` | no | `30` | Session lifetime |
| `DEEPSEEK_BASE_URL` | no | `https://api.deepseek.com/v1` | OpenAI-compatible DeepSeek endpoint |
| `APP_TIMEZONE` | no | `UTC` | Fallback IANA timezone for accounts without `profile.timezone`; backend timestamps stay UTC |
| `VAPID_PUBLIC_KEY` | no | empty | Web Push public key; empty disables OS notifications |
| `VAPID_PRIVATE_KEY` | no | empty | Web Push private key; empty disables OS notifications |
| `VAPID_SUBJECT` | no | `admin@example.com` | VAPID contact; sent as `mailto:` unless it is an HTTPS URL |
| `REMINDER_TICK_SECONDS` | no | `60` | Background reminder scheduler interval |
| `GOOGLE_CLIENT_ID` | no | empty | Google OAuth client id; empty disables Google Calendar |
| `GOOGLE_CLIENT_SECRET` | no | empty | Google OAuth client secret |
| `GOOGLE_REDIRECT_URL` | no | `http://127.0.0.1:8080/api/v1/integrations/google-calendar/oauth/callback` | Registered redirect URI |
| `APP_ENCRYPTION_KEY` | no | empty | Base64 key for OAuth token encryption; 32 bytes recommended, longer values are normalized |
| `CALENDAR_SYNC_SECONDS` | no | `300` | Google Calendar background sync interval |
| `PEPY_ENABLED` | no | `false` | Feature flag for the shared Pepy integration |
| `PEPY_ALLOWED_EMAILS` | no | empty | Comma-separated account emails allowed to use Pepy; `*` for all |
| `PEPY_BASE_URL` | no | `https://softwareseniapi.pepy.app/api/v1` | Pepy API base |
| `PEPY_USERNAME` / `PEPY_PASSWORD` | no | empty | Pepy login credentials |
| `PEPY_USER_ID` / `PEPY_PROJECT_ID` | no | `820` / `5674` | Hardcoded Pepy target |
| `PEPY_LOGIN_INTERVAL_SECONDS` | no | `259200` | Re-login cadence (3 days) |
| `PEPY_SYNC_SECONDS` | no | `300` | Task reconciliation/token-check interval |
| `S3_ENDPOINT` | no | empty | R2 endpoint `https://<account>.r2.cloudflarestorage.com`; empty disables attachments |
| `S3_PUBLIC_ENDPOINT` | no | `S3_ENDPOINT` | Browser-facing endpoint used only when signing URLs |
| `S3_REGION` | no | `auto` | R2 always uses `auto` |
| `S3_BUCKET` | no | `second-brain` | Private attachment bucket |
| `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY` | no | empty | Bucket-scoped R2 API token |
| `S3_FORCE_PATH_STYLE` | no | `false` | R2 requires virtual-hosted style |
| `S3_AUTO_CREATE_BUCKET` | no | `false` | Leave off; the bucket is created in the dashboard |
| `ATTACHMENT_URL_TTL_SECONDS` | no | `900` | Signed upload/download URL lifetime, capped at one hour |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FILE` | no | empty | Empty = stdout only; set = stdout + rotated file |
| `LOG_MAX_SIZE_MB` | no | `100` | Rotated file size threshold |
| `LOG_MAX_BACKUPS` | no | `3` | Rotated files kept |
| `LOG_MAX_AGE_DAYS` | no | `14` | Rotated file age limit |
| `LOG_COMPRESS` | no | `false` | Gzip rotated files |

## Run

```bash
make db                      # docker compose up -d db
make run                     # start api (configure S3_ENDPOINT for Cloudflare R2)

`make build` produces `bin/morrow-api`; `docker compose up` builds and runs
the API image instead. The database is published on `${DB_PORT:-5433}`; export
`DB_PORT` (and mirror it in `DATABASE_URL`) when 5433 is already taken.

### Migrations

| Command | Effect |
|---|---|
| `make migrate-up` | apply every pending migration |
| `make migrate-down` | roll back to zero (drops everything the migrations created) |
| `make migrate-status` | current schema version and dirty state |
| `make db-reset` | migrate down, then up — a clean database |

| `make vapid` | print a fresh `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` pair |

The schema starts from the fresh-project baseline
(`000001_initial.up.sql`). Add new numbered `.up.sql` and `.down.sql` files;
do not edit an applied migration once this is deployed.

### Troubleshooting startup

- `error: DATABASE_URL is not set. ...` — no `.env` and no exported
  `DATABASE_URL`. Run `cp .env.example .env` and start the database.
- `error: could not connect to PostgreSQL using DATABASE_URL (...)` — the URL
  points at a host/port nothing is serving. Check `docker ps`: the `db` container
  must show a published port such as `0.0.0.0:5433->5432/tcp`.
- The `db` container is `healthy` but shows **no** published port: it was created
  while that host port was occupied. Recreate it once the port is free:
  `docker compose up -d --force-recreate db`.

## Test

```bash
make test                    # or: TEST_DATABASE_URL=... go test ./... -count=1
```

The suite creates a throwaway database per test (`sb_test_<nanos>_<rand>`),
runs the migrations, registers the test user through the real route, and drives the router in-process,
so the configured role needs `CREATEDB`. `TEST_DATABASE_URL` wins over
`DATABASE_URL`; both may point at the same server.

`test/pepy_test.go` covers feature gating, login/token encryption, task link
opt-in, create/update/delete, 401 re-login, reconciliation, and timebox validation.
`test/calendar_test.go` runs the full OAuth, pull, push, last-write-wins, calendar
picker, and disconnect flows against a fake Google server. `test/contracts_test.go`
round-trips `contracts/fixtures/*.json` through the
wire structs, so a field rename fails this suite and the frontend's copy.

## Behaviour

- All responses are JSON; absent optional fields are omitted, never `null`.
- Every response carries `X-Request-Id`: the client's `X-Request-Id` value when it
  sends one (trimmed, at most 128 characters), otherwise a generated UUID v4. The
  same value is the `request_id` field on the request log line, so a client can
  quote either one when reporting a failed call.
- PATCH bodies distinguish "absent" (unchanged) from `null` (clear): only the
  declared nullable keys accept `null`, everything else returns
  `400 Field <key> cannot be null`.
- `Update*` bodies reject unknown keys; create bodies ignore them.
- Timestamps are RFC 3339 with millisecond precision and `Z`; dates are
  zero-padded `YYYY-MM-DD`.
- Entity ids are short strings (`t_1a2b3c4d`); task and project keys are
  allocated server-side and never change.
- Tasks expose `syncToPepy`; set it on create/update to opt in or out. It is
  rejected when the task has no due date and start/end time. Unsetting it stops
  future pushes and removes only the local link, not the remote Pepy task.
- Tasks carry optional `startTime` / `endTime` (`HH:mm`) for time boxing. Both
  are independent and clearable with `PATCH {"startTime":null}`; when both are
  present the end must be after the start.
- Temporal columns use native PostgreSQL types: clocks (`events`, `habits`,
  `tasks`, `reminder_settings.task_time`) are `time`, calendar dates are `date`,
  and instants are `timestamptz`. The wire contract is unchanged: `HH:mm`,
  `YYYY-MM-DD`, and RFC 3339 `Z` timestamps. `wiretime.Clock` handles the
  `time` ↔ `HH:mm` conversion.
- Every write runs in a transaction scoped to the authenticated user; reads use
  the pool.
- The assistant answers from a bounded context snapshot (tasks, projects, notes
  including their text, inbox, events, habits, areas) and returns `sources`
  entries with `{kind, id, title, href}` for the items the reply relied on: the
  model's own `Sources:` line when present, otherwise the items whose titles
  appear in the reply.
- The assistant can create tasks: `create_task` tool calls are validated
  (title required, status/priority defaulted, due date parsed, project resolved
  against the user's own projects) and executed immediately through the task
  service; the reply carries `created` with the saved tasks. Unknown projects
  drop the call, at most five are created per reply, and titles already present
  in the same project are skipped so a repeated request does not duplicate.
- `provider: "fallback"` means the reply is derived locally; `reason` says why:
  `ai_disabled`, `key_missing`, `provider_error`, or `empty_completion`.
- Each account stores its own DeepSeek API key in `ai_settings.api_key` (plain
  text at rest; protect database access accordingly). The key is write-only:
  `PATCH /ai {"apiKey":"sk-…"}` stores it (trimmed), `{"apiKey":null}` removes it,
  and responses only expose `keyConfigured`. Changing it resets the test status.
  The assistant and `POST /ai/test` use the caller's key; there is no environment
  key.
- Models come from the provider's `/models` catalogue using the caller's key
  (cached 10 minutes per key digest, curated fallback list when unreachable or
  unconfigured); `PATCH /ai` accepts any catalogued id plus the legacy
  `deepseek-chat` / `deepseek-reasoner` aliases, and `reasoningEffort` (`none`,
  `low`, `high`, `max`, or null for the provider default). Effort is only sent
  to catalogued models.
- Google Calendar is a real two-way integration. `POST /google-calendar/oauth/start`
  returns a consent URL (PKCE + one-time state); `GET
  /integrations/google-calendar/oauth/callback` stores AES-GCM-encrypted tokens,
  marks the integration connected, and imports the primary calendar. A calendar
  picker switches the synced calendar. Local event create/update/delete is
  mirrored to Google; imported Google events are editable and last-write-wins
  uses `events.updated_at` vs the Google `updated` timestamp. A background sync
  pulls incremental changes with `syncToken` (full resync on `410`) and retries
  failed pushes. Disconnect revokes the token, keeps imported events as local,
  and stops sync.
- SoftwareSeni Pepy is a feature-gated, shared-account integration. Tasks sync only
  when they have a due date + start/end time and the task's per-task opt-in link is
  enabled. Local writes mirror to Pepy create/update/delete; done tasks set
  `stop_time` to the scheduled end. Task `YYYY-MM-DD` / `HH:mm` values are
  interpreted in `profile.timezone`, then `APP_TIMEZONE`, then UTC, and sent to
  Pepy as UTC. Links live in the provider-agnostic
  `task_external_links` table (`integration_id`), not a Pepy-specific column, so
  future providers do not need a schema change. The bearer token from
  `data.token` is AES-GCM encrypted, refreshed at startup and every 3 days
  (`PEPY_LOGIN_INTERVAL_SECONDS`), and re-logged-in once on `401`. Pepy is hidden
  unless `PEPY_ENABLED` and the account email matches `PEPY_ALLOWED_EMAILS`.
- A background scheduler (`REMINDER_TICK_SECONDS`) computes due reminders from
  existing data, records them in `reminder_deliveries`, and pushes them to
  subscribed devices. Tasks fire at the user's `taskTime` when open and due today
  or earlier; events fire at their start minus `eventLeadMinutes`, with a
  15-minute catch-up window; habits fire at `startTime` on scheduled days, when
  there is no check-in yet and the cadence period target is unmet. Dates and
  `HH:mm` values are interpreted in `profile.timezone`, then `APP_TIMEZONE`, then
  UTC. `(user_id, kind, ref_id, occurrence_date)` is unique, so a repeated
  tick never sends the same reminder twice.
- Attachments live on projects, tasks, and notes. The browser uploads straight to
  S3-compatible storage through a short-lived presigned `PUT`; the API never
  streams file bytes. PDF, PNG/JPEG/WebP/GIF images, and Office/OpenDocument
  files are accepted up to 25 MiB (images 10 MiB). Completion verifies object
  size, content type, and magic bytes before flipping the row to `ready`.
  Downloads and image/PDF previews use short-lived presigned `GET`s. Deleting a
  project, task, or note removes its attachment metadata and stored objects.
  Attachments are unavailable when the S3 variables are incomplete.
- Without both VAPID keys the scheduler still records deliveries for the bell and
  Today list, but sends no OS notification; `GET /push/config` reports
  `{enabled:false, publicKey:""}`. One API process owns the loop; running several
  replicas would deliver each reminder once per replica, so keep one instance.
- Reminder settings and push subscriptions are created lazily: registration
  writes the first `reminder_settings` row, and older accounts get one on their
  first settings read, settings write, or push subscription.

## Attachment storage

Cloudflare R2 is used directly for object storage:

```bash
# Set in .env or environment:
# S3_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
# S3_REGION=auto
# S3_BUCKET=second-brain
# S3_ACCESS_KEY_ID=<r2-access-key-id>
# S3_SECRET_ACCESS_KEY=<r2-secret-access-key>
# S3_FORCE_PATH_STYLE=false
# S3_AUTO_CREATE_BUCKET=false
```

Keep the bucket private; the API signs every read. Allow browser CORS for `PUT`/`GET` on R2 directly if direct browser uploads are enabled.

## Auth

Two bearer credential kinds are accepted on the same `Authorization: Bearer`
header:

- **Registration** — `POST /auth/register` creates the account, profile, default
  integrations/AI settings, and reminder settings in one transaction, then
  returns a session token (`201`). The email is normalized to lowercase; a
  duplicate is `409 Email already in use`; the password must be at least 8
  characters; an empty name falls back to the email local part.
- **Sessions** — created by `POST /auth/register` or `POST /auth/login`, 64 hex
  characters, stored hashed with an expiry (`TOKEN_TTL_DAYS`).
  `POST /auth/logout` deletes the session.
- **Personal access tokens** — created from Settings → Agent access (or
  `POST /api-tokens`), prefixed `sb_` + 64 hex characters, for MCP servers and
  scripts that cannot run a login flow. The plaintext is returned exactly once;
  only the SHA-256 hash is stored. Tokens never expire, `last_used_at` is
  refreshed best-effort on each request, and `DELETE /api-tokens/{id}` revokes
  one immediately. Logout with a PAT is a no-op `204` (no session exists).

Passwords are Argon2id PHC strings, so hashes written by the previous
implementation still verify.

## Endpoints

Base prefix `/api/v1`; everything except `POST /auth/login` and
`POST /auth/register` needs `Authorization: Bearer <token>` (session or `sb_`
personal access token).

| Method | Path | Response |
|---|---|---|
| POST | `/auth/register` | `{token, profile}` (201) |
| POST | `/auth/login` | `{token, profile}` |
| POST | `/auth/logout` | 204 |
| GET | `/auth/session` | `{profile}` |
| GET / POST | `/api-tokens` | `ApiToken[]` / `{token, apiToken}` (201) |
| DELETE | `/api-tokens/{id}` | 204 / 404 |
| GET / PATCH | `/profile` | `Profile` |
| GET | `/inbox`, `/inbox/archived` | `InboxItem[]` |
| GET / POST | `/inbox` | `InboxItem` |
| GET / PATCH / DELETE | `/inbox/{id}` | `InboxItem` / 204 |
| POST | `/inbox/{id}/archive`, `/inbox/{id}/restore` | `InboxItem` |
| GET / POST | `/tasks` | `Task[]` / `Task` |
| GET / PATCH / DELETE | `/tasks/{id}` | `Task` / 204 |
| GET / POST | `/projects` | `Project[]` / `Project` |
| GET / PATCH / DELETE | `/projects/{id}` | `Project` / 204 |
| GET / POST | `/notes` | `Note[]` / `Note` |
| GET / PATCH / DELETE | `/notes/{id}` | `Note` / 204 |
| GET / POST | `/events` | `Event[]` / `Event` |
| GET / PATCH / DELETE | `/events/{id}` | `Event` / 204 |
| GET / POST | `/areas` | `Area[]` / `Area` |
| GET / PATCH / DELETE | `/areas/{id}` | `Area` / 204 |
| GET / POST | `/habits` | `Habit[]` / `Habit` |
| GET | `/habits/archived` | `Habit[]` |
| GET / PATCH / DELETE | `/habits/{id}` | `Habit` / 204 |
| POST | `/habits/{id}/archive`, `/habits/{id}/restore` | `Habit` |
| GET | `/habit-check-ins` | `HabitCheckIn[]` |
| PUT / DELETE | `/habits/{habitId}/check-ins/{date}` | `HabitCheckIn` / 204 |
| GET | `/snapshot` | `ServerSnapshot` |
| POST | `/import` | 204 |
| GET | `/search?q=` | `SearchResult[]` |
| POST | `/assistant` | `{reply, provider, sources, created, reason?}` |
| GET | `/settings` | `{integrations, ai, aiModels, aiEffort}` |
| PATCH | `/integrations/{id}` | `IntegrationConnection` |
| POST | `/integrations/{id}/connect`, `/disconnect`, `/sync` | `IntegrationConnection` |
| POST | `/google-calendar/oauth/start` | `{authorizationUrl}` |
| GET | `/google-calendar/calendars` | `GoogleCalendar[]` |
| PUT | `/google-calendar/calendar` | `IntegrationConnection` (body: `calendarId`) |
| POST | `/google-calendar/sync` | `IntegrationConnection` |
| POST | `/google-calendar/disconnect` | 204 |
| GET | `/attachments/config` | `{enabled, maxBytes, imageMaxBytes, extensions}` |
| GET | `/attachments?entityType=&entityId=` | `Attachment[]` |
| POST | `/attachments` | presigned upload ticket (201) |
| POST | `/attachments/{id}/complete` | `Attachment` |
| GET | `/attachments/{id}/url?inline=` | `{url, expiresAt}` |
| DELETE | `/attachments/{id}` | 204 |
| POST | `/pepy/connect` | `IntegrationConnection` |
| POST | `/pepy/sync` | `IntegrationConnection` |
| POST | `/pepy/disconnect` | 204 |
| GET | `/integrations/google-calendar/oauth/callback` | 302 to Settings (public) |
| PATCH | `/ai` | `AiSettings` (body: `enabled`, `model`, `apiKey`, `reasoningEffort`) |
| POST | `/ai/test` | `{ok, status, message, testedAt}` |
| GET / PATCH | `/reminders/settings` | `ReminderSettings` (body: `enabled`, `taskTime`, `eventLeadMinutes`) |
| GET | `/reminders` | `Reminder[]` (today's undismissed deliveries) |
| POST | `/reminders/{id}/dismiss` | 204 / 404 |
| GET | `/push/config` | `{enabled, publicKey}` |
| POST | `/push/subscriptions` | 204 (body: `{endpoint, keys:{p256dh, auth}}`) |
| DELETE | `/push/subscriptions` | 204 (body: `{endpoint}`) |
| GET | `/healthz` | `{status:"ok"}` |

### Google Calendar setup

1. Create a Google Cloud project, enable the **Google Calendar API**, and configure
   the OAuth consent screen.
2. Add the redirect URI from `GOOGLE_REDIRECT_URL` (default
   `http://127.0.0.1:8080/api/v1/integrations/google-calendar/oauth/callback`).
3. Create an OAuth client (Web application), set `GOOGLE_CLIENT_ID` and
   `GOOGLE_CLIENT_SECRET`, and generate `APP_ENCRYPTION_KEY` with
   `openssl rand -base64 32`.
4. While the app is unverified, add the Google account as a test user.

Connect from Settings → Integrations → Google Calendar. The first import covers
`now − 30 days` through `now + 180 days`; later syncs use the incremental token.

## Contracts

`contracts/fixtures/*.json` are the wire fixtures, duplicated per repo so a
standalone checkout can run its suite. `test/contracts_test.go` round-trips every
fixture through the Go wire structs and the frontend test does the same with the
TypeScript types, so a field rename must update both copies and breaks both
suites.
