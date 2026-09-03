# GoPulse

GoPulse is currently at product version **1.1.2**. Phase 1 provides a browser-operable minimum business system backed by MySQL, Phase 2 adds reliable notification delivery through the transactional Outbox and Business Worker, Phase 3 closes the rebuildable and incrementally convergent post-search milestone, and Phase 4 standardizes safe Schema v1 JSON logs across every maintained Go application process. MySQL remains authoritative, while an isolated Search Indexer converges `post.created` events into Elasticsearch and the authenticated Frontend exposes the hydrated search results.

The repository currently provides:

- a Vue 3 + Vue Router Frontend for registration, login, logout, post listing/pagination, publishing, detail, comments, likes, authenticated search, notifications, and authentication recovery;
- a diagnostic connectivity page at `/dev/status`, outside the business navigation;
- a Gin Backend with `/health`, `/ready`, and typed `/api/v1` business contracts;
- username/password authentication with bcrypt, short-lived HS256 JWTs, HttpOnly cookies, and reusable authentication middleware;
- authenticated post publishing, keyset-paginated post/comment reads, comments, and idempotent likes;
- Redis cache-aside for the non-personalized post-detail projection with best-effort invalidation and MySQL fallback;
- versioned MySQL migrations for users, posts, comments, and post likes;
- local MySQL, Redis, RabbitMQ, and fixed-version Elasticsearch infrastructure;
- transactional `post.created` Outbox delivery through an isolated RabbitMQ topology and Search Indexer;
- single-line Schema v1 JSON lifecycle, HTTP, Outbox, Worker, Indexer, and reindex logs with request/event correlation and bounded safe fields;
- WSL/Bash lifecycle scripts, read-only runtime verification, and destructive-but-isolated business/search acceptance scripts;
- Frontend unit/component tests, real Chromium E2E acceptance, Backend unit/integration tests, and Linux quality gates.

Kafka, application containers, Kubernetes, profiles, follows, post update/delete indexing, automatic dead-queue replay, real-time notification push, and other later-phase capabilities are not implemented yet.

## Primary development environment

Starting with Phase-01-02, GoPulse uses WSL2 on Windows as its primary implementation and acceptance environment. Use the following baselines:

- WSL2 with a Linux distribution;
- Go 1.26;
- Node.js 24;
- npm 11;
- Docker Desktop with WSL integration, or one WSL-native Docker Engine with the Docker Compose v2 plugin;
- Bash with `python3`, `curl`, `flock`, `ps`, and `sha256sum`.

Keep the repository in the WSL Linux filesystem for Linux tooling and file watching:

```bash
mkdir -p ~/src
cd ~/src
git clone <repository-url> GoPulse
cd GoPulse
```

Do not use `/mnt/c/...`, `/mnt/d/...`, or another Windows-mounted checkout as the active WSL workspace. If Docker Desktop is used, enable WSL integration for the selected distribution and do not run a second Docker daemon inside that distribution.

The existing `scripts/*.ps1` files are preserved at the `0.2.1` capability baseline. They are not maintained or accepted for Phase-01-02 through Phase 16. Native Windows PowerShell compatibility will be implemented after Phase 16 against the final Bash behavior.

## Local configuration

The first `dev.sh` run creates `.env` from `.env.example` when `.env` is absent. You may also create it explicitly:

```bash
cp .env.example .env
```

Workspaces created before Phase-01-01 must manually add the Phase 1 values from `.env.example`, including `AUTH_JWT_SECRET`, `AUTH_JWT_TTL`, `AUTH_COOKIE_NAME`, `AUTH_COOKIE_SECURE`, `REDIS_POST_DETAIL_TTL`, and `REDIS_OPERATION_TIMEOUT`. Existing `.env` files must also include the Phase 2 `OUTBOX_*` and `BUSINESS_WORKER_*` values, the Phase-03-01 `ELASTICSEARCH_PORT`, `ELASTICSEARCH_URL`, `ELASTICSEARCH_REQUEST_TIMEOUT`, and `SEARCH_REINDEX_BATCH` values, and the Phase-03-02 `SEARCH_INDEXER_*` values shown in `.env.example`. The development script does not overwrite an existing `.env`. Elasticsearch URLs must use HTTP(S), include a host, and must not include credentials, query parameters, or fragments.

The checked-in values are development-only credentials. Do not reuse them in production or commit a local `.env`. `APP_ENV` must be `development`, `test`, or `production`. Production requires `AUTH_COOKIE_SECURE=true`; local development and tests may explicitly use `false` for HTTP.

By default, `PUBLISHED_HOST` and `HTTP_HOST` bind infrastructure ports and the Backend only to `127.0.0.1`. Setting either value to a non-loopback address is an explicit remote-access choice.

## Start the development environment

Run from any directory:

```bash
/home/<user>/src/GoPulse/scripts/dev.sh
```

The script performs the following sequence:

1. validates the required tools and configuration;
2. starts MySQL, Redis, RabbitMQ, and Elasticsearch with Docker Compose;
3. waits for all infrastructure health checks;
4. runs `go run ./cmd/migrate up` in `backend/`;
5. runs `go run ./cmd/search-reindex --if-missing` to initialize the search alias without replacing an existing generation;
6. builds and starts the Backend, independent Business Worker, and independent Search Indexer;
7. installs reproducible Frontend dependencies when required and starts Vite.

A failed migration or application startup stops only the Backend, Business Worker, Search Indexer, and Frontend processes started by that invocation. With the default configuration, the environment provides:

| Service | Address |
| --- | --- |
| Frontend business application | `http://localhost:5173` |
| Backend | `http://localhost:8080` |
| Backend liveness | `http://localhost:8080/health` |
| Backend readiness | `http://localhost:8080/ready` |
| Authentication API | `http://localhost:8080/api/v1` |
| RabbitMQ management | `http://localhost:15672` |
| Elasticsearch (loopback only) | `http://localhost:9200` |
| MySQL | `localhost:3306` |
| Redis | `localhost:6379` |
| RabbitMQ AMQP | `localhost:5672` |

When `HTTP_PORT` changes, the Backend, Vite proxies for `/health`, `/ready`, and `/api/v1`, and `verify.sh` all use the same resolved port. Caller environment overrides handled by `dev.sh` are passed explicitly to Vite.

Keep the foreground command running. `Ctrl+C` stops Frontend, Search Indexer, Business Worker, and Backend in that order while leaving the Compose infrastructure and named volumes available. Repository-owned identity records are stored as `.run/frontend.json`, `.run/search-indexer.json`, `.run/business-worker.json`, and `.run/backend.json`; each record binds the PID to its cwd, executable, start ticks, and command marker before cleanup is allowed.

## Verify a running environment

```bash
/home/<user>/src/GoPulse/scripts/verify.sh
```

`verify.sh` is read-only. It reads the configured `HTTP_PORT`, checks the four Compose services, verifies that the Business Worker and Search Indexer PIDs still match their repository-owned cwd, executable, start ticks, and command markers, validates `/health` and `/ready`, confirms that an unauthenticated protected API returns `401 authentication_required`, and confirms that the Frontend responds over HTTP. It never creates users, posts, comments, notifications, queue messages, or cache entries.

For complete destructive integration acceptance, run:

```bash
/home/<user>/src/GoPulse/scripts/verify-business.sh
```

`verify-business.sh` creates a random 12-character acceptance token and uses it to derive a strictly whitelisted Compose project and database. It allocates non-default loopback ports, uses a temporary environment and process directory, and never modifies `.env` or `.run`. The full Phase 0–3 matrix covers the historical rebuild and browser search journey, MySQL hydration, normal incremental indexing, Search Indexer pause/restart, duplicate delivery, RabbitMQ and Elasticsearch outages, concurrent rebuild convergence, the real Chromium notification flow, Worker and Outbox reliability, and the Redis/restart baseline. Before stopping, restarting, clearing, or deleting anything, it validates project labels, container IDs, persistent port bindings, and application PID ownership. Exit, failure, and signal traps remove only that verified acceptance project and its volumes, then compare the daily development stack snapshot.

The no-Docker negative safety checks can be run independently:

```bash
scripts/verify-business.sh --self-test
```

The targeted historical-search acceptance creates posts in an isolated stack, rebuilds and queries them through the authenticated API and browser, deletes only the active physical search index, rebuilds it again, and verifies an unrelated Elasticsearch index remains intact:

```bash
scripts/verify-business.sh --search-rebuild
```

The targeted incremental-search acceptance verifies notification/search queue isolation, atomic post Outbox creation, normal convergence, Indexer pause/restart, duplicate delivery, RabbitMQ and Elasticsearch recovery, rebuild concurrency, and final browser visibility:

```bash
scripts/verify-business.sh --search-live
```

## Stop the environment

```bash
/home/<user>/src/GoPulse/scripts/down.sh
```

`down.sh` validates and stops the recorded Frontend, Search Indexer, Business Worker, and Backend processes, removes the `gopulse` Compose containers and network, and preserves the MySQL, Redis, RabbitMQ, and Elasticsearch named volumes. It is safe to run repeatedly and refuses to signal a process whose record no longer proves repository ownership.

## Database migrations

The Backend module contains embedded, versioned SQL migrations:

```bash
cd backend
go run ./cmd/migrate up
```

Down migration is explicit and must only be used against a confirmed disposable or isolated database:

```bash
cd backend
go run ./cmd/migrate down
```

Development startup never runs `down` and never clears the development database automatically.

## Business event and Outbox foundation

The Backend defines a strict, versioned JSON envelope for `comment.created`, `post.liked`, and `post.created`. Version 1 messages are limited to 16 KiB, reject unknown fields and multiple JSON values, carry only stable numeric identifiers plus a UTC occurrence time, and use the event UUID as AMQP `message_id`. Notification routing keys remain `comment.created.v1` and `post.liked.v1`; incremental indexing uses `post.created.v1`. The search event omits `recipient_id`, title, and content so the Indexer must re-read the authoritative MySQL post.

The shared durable direct topology contract is centralized in Backend code:

| Role | Name |
| --- | --- |
| Main exchange | `gopulse.business.v1` |
| Main queue | `gopulse.business-worker.v1` |
| Retry exchange / queue | `gopulse.business.retry.v1` / `gopulse.business-worker.retry.v1` |
| Dead exchange / queue | `gopulse.business.dead.v1` / `gopulse.business-worker.dead.v1` |

Search delivery uses a separate fixed topology that binds only `post.created.v1`: `gopulse.search.v1`, `gopulse.search-indexer.v1`, `gopulse.search.retry.v1`, `gopulse.search-indexer.retry.v1`, `gopulse.search.dead.v1`, and `gopulse.search-indexer.dead.v1`. Notification queues never bind the search routing key, and search queues never bind notification routing keys.

Migration `000002_business_outbox` adds the constrained `business_outbox` table, and migration `000004_post_created_outbox` extends its event constraint. Post creation, comment creation, and a user's first non-self like write their business fact and event in the same MySQL transaction; duplicate likes, self actions, and unlike operations do not create notification events. Redis invalidation remains a best-effort operation after commit.

The Backend starts a lifecycle-bound Outbox Dispatcher that claims finite leased batches and lazily connects to RabbitMQ. It publishes persistent mandatory messages, waits for publisher confirms, and marks a row published only after a confirmed routable delivery. Broker outages, nacks, returns, timeouts, and connection loss leave the MySQL fact committed and release or preserve the event for bounded retry. `OUTBOX_LEASE_DURATION` must cover `OUTBOX_CLAIM_BATCH × OUTBOX_PUBLISH_TIMEOUT` plus a one-second state-transition margin; the checked-in default is one minute for a batch of ten and a five-second per-message timeout. `OUTBOX_POLL_INTERVAL` and `OUTBOX_RETRY_DELAY` control polling and retry availability. The same runtime deletes only expired `published` rows in bounded batches: `OUTBOX_CLEANUP_INTERVAL`, `OUTBOX_PUBLISHED_RETENTION`, and `OUTBOX_CLEANUP_BATCH` default to one hour, seven days, and 500 rows. Pending and leased rows are never eligible for retention cleanup.

Delivery is intentionally at least once: a crash after RabbitMQ confirms a publish but before MySQL records `published` can deliver the same `event_id` again. Migration `000003_notifications` adds the durable notification side-effect table, whose unique `source_event_id` absorbs sequential and concurrent duplicate deliveries.

`scripts/dev.sh` starts the independent consumer as part of the normal lifecycle. For focused Worker development after migrations, it may also be run manually from a second terminal with `cd backend && go run ./cmd/business-worker`.

The Worker loads only MySQL, RabbitMQ, and `BUSINESS_WORKER_*` settings; it does not require HTTP, Redis, JWT, or Cookie configuration. It uses manual acknowledgements and bounded prefetch. Valid `comment.created` and `post.liked` events commit a notification before ack, while self events are defensively ignored. Permanent envelope/property errors go directly to the dead queue. Temporary processing failures are republished through the TTL retry queue with a validated `x-gopulse-attempt` header and enter the dead queue after `BUSINESS_WORKER_MAX_RETRIES`. Retry/dead publications are persistent, mandatory, and confirm-gated before the original message is acked; a failed secondary publish requeues the original message.

`OUTBOX_RETRY_DELAY` is the shared retry-queue TTL used by both producer and consumer topology declarations. `BUSINESS_WORKER_PREFETCH`, `BUSINESS_WORKER_MAX_RETRIES`, `BUSINESS_WORKER_PUBLISH_TIMEOUT`, `BUSINESS_WORKER_SHUTDOWN_TIMEOUT`, `BUSINESS_WORKER_RECONNECT_MIN`, and `BUSINESS_WORKER_RECONNECT_MAX` bound consumption, retries, reconnection, and graceful shutdown. During shutdown the Worker stops new deliveries, gives the current handler the configured grace period, then cancels its processing context and waits for that handler to exit before closing AMQP and MySQL resources. Delivery remains at least once, and the database unique key—not process memory or Redis—provides idempotency. The notification HTTP API reads only durable MySQL facts; it does not expose RabbitMQ, Outbox, retry, or dead-queue state. The Frontend refreshes explicitly and does not infer a notification from a successful comment or like request.

Reliability boundaries are explicit: RabbitMQ is a single local development node rather than a production HA cluster; retry count and delay are finite; dead-queue inspection and replay remain manual operational work; and there is no exactly-once guarantee. Broker or Worker outages delay notification materialization but do not roll back committed comments or first likes. MySQL remains authoritative for both core facts and completed notifications.

## Frontend routes

| Route | Access | Purpose |
| --- | --- | --- |
| `/register` | anonymous | Create an account and establish the login Cookie |
| `/login` | anonymous | Authenticate with username and password |
| `/posts` | authenticated | Newest-first post list with cursor-based loading |
| `/notifications` | authenticated | Recipient-only asynchronous comment/like notifications with refresh, pagination, and idempotent read actions |
| `/posts/new` | authenticated | Publish a validated title and body |
| `/posts/:postId` | authenticated | Read detail, paginate comments, comment, like, and unlike |
| `/auth-recovery` | temporary recovery state | Retry current-user restoration after a network, server, or invalid-response failure |
| `/dev/status` | unrestricted diagnostic | Inspect `/health` and `/ready` without appearing in business navigation |

The first business navigation waits for `/api/v1/users/me`. Only a valid `401 authentication_required` response establishes an anonymous state. Network failures, 5xx responses, and invalid responses retain a retryable recovery state and route to `/auth-recovery`, so an existing Cookie session is not presented as a logout. Authenticated users are redirected away from anonymous pages, while unauthenticated users are redirected away from protected pages. JWT values are never read, parsed, or stored by the Frontend; all API calls use same-origin paths and Cookie credentials.

## Current HTTP contracts

### Health and readiness

`GET /health` reports only Backend process liveness:

```json
{"status":"ok","service":"backend"}
```

`GET /ready` checks MySQL, Redis, and RabbitMQ concurrently. It returns HTTP `200` when all checks are `up`, or HTTP `503` with each dependency marked `up` or `down`. A dependency checker that ignores cancellation is limited to one in-flight background execution, and checker panics are isolated as `down`. Dependency errors, panic values, connection strings, and credentials are not returned to clients.

The HTTP server enforces a 5-second read-header timeout, 10-second read timeout, 15-second write timeout, 60-second idle timeout, a 1 MiB header limit, and the existing 5-second graceful-shutdown boundary.

### User and authentication API

All successful JSON responses use the common `data` envelope. Public user data contains only `id`, `username`, and `created_at`; password hashes and JWTs are never returned in JSON.

- `POST /api/v1/auth/register`
  - accepts `{"username":"alice","password":"example-password"}`;
  - returns HTTP `201`, a public user DTO, and the authentication cookie;
  - returns `400 validation_failed` for invalid input or `409 username_conflict` for a case-insensitive username conflict.
- `POST /api/v1/auth/login`
  - returns HTTP `200`, a public user DTO, and the authentication cookie;
  - unknown users and incorrect passwords both return `401 invalid_credentials` with the same public message.
- `POST /api/v1/auth/logout`
  - is anonymous and idempotent;
  - expires the authentication cookie and returns HTTP `204` with no body.
- `GET /api/v1/users/me`
  - requires a valid authentication cookie;
  - returns HTTP `200` and the current public user DTO;
  - missing, expired, malformed, or tampered tokens return `401 authentication_required`;
  - a token whose user no longer exists also clears the cookie and returns `401`.

The cookie uses the configured name with `HttpOnly`, `SameSite=Lax`, `Path=/`, no broad `Domain`, and a lifetime coordinated with `AUTH_JWT_TTL`. Production forces the `Secure` attribute. JWT validation accepts only HS256 and requires positive decimal `sub`, `iat`, and `exp` claims.

A command-line smoke flow can retain the HttpOnly cookie in a cookie jar:

```bash
curl --show-error --fail-with-body \
  -c /tmp/gopulse-cookie.txt \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"example-password"}' \
  http://localhost:8080/api/v1/auth/register

curl --show-error --fail-with-body \
  -b /tmp/gopulse-cookie.txt \
  http://localhost:8080/api/v1/users/me
```

### Post, comment, and like API

All routes below require the authentication cookie:

- `POST /api/v1/posts` publishes a normalized title and content and returns HTTP `201`.
- `GET /api/v1/posts?limit=<n>&cursor=<token>` returns newest-first keyset pagination.
- `GET /api/v1/posts/:postId` returns the complete detail response, including `comment_count`, `like_count`, and viewer-specific `liked_by_me`.
- `POST /api/v1/posts/:postId/comments` creates a comment; `GET` on the same collection returns newest-first comment pagination.
- `PUT /api/v1/posts/:postId/like` and `DELETE /api/v1/posts/:postId/like` are idempotent and return HTTP `204`.

### Search API and rebuild command

`GET /api/v1/search/posts?q=<keyword>&limit=<n>&cursor=<opaque>` requires authentication. The query is trimmed and limited to 1–200 Unicode code points; `limit` defaults to 20 and is capped at 50. Elasticsearch searches `title^2` and `content`, orders by score plus deterministic tie-breakers, and returns only post IDs. The first page opens a two-minute Point in Time (PIT); subsequent pages reuse that snapshot with the complete `search_after` tuple. The opaque cursor is HMAC-protected and binds the query digest, physical generation, PIT, expiry, score, creation time, post ID, and `_shard_doc`, so tampered, expired, or post-rebuild cursors safely require a fresh first-page search. The Backend then hydrates the existing complete `Post` DTO from MySQL in hit order, including author, current content, comment/like counts, and viewer-specific `liked_by_me`.

The protected `/search` page stores the query in the URL, supports reload/back/forward restoration, pagination, empty/unavailable/cursor-invalid states, retry, and navigation to the existing post detail page. A temporary load-more failure retries the same cursor without discarding accumulated results; an expired or invalid snapshot cursor explicitly clears stale results and restarts from page one. The browser calls only relative Backend `/api/v1` paths and never connects to port 9200.

Run a forced zero-downtime-style rebuild from `backend/` with:

```bash
go run ./cmd/search-reindex
```

Use `--if-missing` to initialize only when the alias does not yet exist:

```bash
go run ./cmd/search-reindex --if-missing
```

Each forced rebuild creates a new physical `gopulse-post-search-v1-*` index, bulk-copies the bounded MySQL snapshot, verifies counts, atomically moves the `gopulse-post-search-v1` alias, compensates the captured tail, and deletes only validated old indices. Elasticsearch failures produce the safe public `503 search_unavailable` contract; they degrade readiness and search without becoming a dependency of existing MySQL repositories.

New posts are indexed automatically after commit. The Backend atomically records a minimal `post.created` Outbox event and publishes it to the isolated search exchange. `cmd/search-indexer` loads only MySQL, RabbitMQ, Elasticsearch, and `SEARCH_INDEXER_*` settings, re-reads the full document from MySQL, and uses `PUT /gopulse-post-search-v1/_doc/{post_id}?require_alias=true`. The stable post ID makes duplicate delivery idempotent, and `require_alias=true` prevents accidental dynamic-index creation.

Temporary MySQL/network failures, Elasticsearch `404`/`429`/`5xx`, and a missing alias use finite retry/dead handling. Missing MySQL facts and deterministic mapping `4xx` failures go directly to the search dead queue. Alias recovery and dead-queue replay remain explicit operations: restore with `search-reindex`; automatic dead-queue replay is not provided.

### Notification API

Both routes require the authentication cookie and always scope data to the current recipient:

- `GET /api/v1/notifications?limit=<n>&cursor=<token>` returns newest-first keyset pagination with notification type, timestamps, public actor summary, post ID, and nullable comment ID. Delivery identifiers, Outbox state, AMQP metadata, and internal processing errors are never returned.
- `PATCH /api/v1/notifications/:notificationId/read` returns HTTP `204`, preserves the first `read_at`, and is idempotent. Missing notifications and notifications owned by another recipient both return the same safe `404 notification_not_found` response.

The `/notifications` page exposes explicit refresh, load-more, post navigation, and per-item mark-read controls. Notifications may arrive asynchronously; there is no WebSocket, SSE, background polling, unread badge, or locally fabricated notification state.

MySQL remains the source of truth. Redis stores only the versioned public post-detail projection under `gopulse:post:detail:v1:{postId}` for `REDIS_POST_DETAIL_TTL`; the value excludes `liked_by_me`, comments, credentials, tokens, and connection data. Every detail request calculates `liked_by_me` separately from MySQL for the authenticated viewer.

A detail cache miss, timeout, connection failure, damaged JSON, unsupported cache version, or failed refill falls back to MySQL. Successful comment, like, and unlike operations attempt cache invalidation only after the MySQL write succeeds. Cache invalidation failures never roll back or change a successful business response.

Cache-aside has an explicitly bounded eventual-consistency window: a failed invalidation or an older concurrent read that refills after a successful invalidation can temporarily expose stale public counts until `REDIS_POST_DETAIL_TTL` expires or the key is cleared. Such cached data never overwrites MySQL facts and never controls viewer-specific `liked_by_me`.

If Redis is unavailable, `/ready` returns HTTP `503` with Redis marked `down`, while the authenticated MySQL business APIs continue operating through cache fallback. After Redis recovers, readiness and cache operations recover without restarting the Backend.


## Structured application logging

The Backend, Business Worker, Search Indexer, and search-reindex command write application observability records as single-line JSON to stdout. Every record carries `log_schema_version=1`, a UTC RFC3339Nano `timestamp`, lowercase `level`, a fixed `service`, a bounded `module`, and a fixed `message`; call sites cannot replace those reserved fields. Backend HTTP and lifecycle records use `service=backend`; asynchronous processing uses `service=business-worker` or `service=search-indexer`; rebuild output uses `service=search-reindex`.

For every request whose server-side identifier is generated successfully, the Backend returns `X-Request-ID` as 32 lowercase hexadecimal characters. Client-provided values are ignored. The same identifier joins the response to one `http request completed` record and, for successful state changes, to the corresponding business record. Completion logs use the registered Gin route template rather than the raw path and include only method, status, integer duration/response size, authenticated numeric user ID when available, and the public error code when applicable. Request bodies, query values, credentials, cookies, tokens, user content, connection URLs, panic values, and stack traces are not logged.

Outbox publication and Worker/Indexer completion records carry the existing `event_id`, stable `event_type`, numeric attempt, and bounded reason only after the relevant publish/mark/ack transition succeeds. Search completion may also include the numeric `post_id`. These asynchronous records deliberately do not copy HTTP request IDs into the Envelope, and no application log includes message payloads, AMQP headers, connection URLs, search documents, index generations, or user content.

The focused isolated acceptance mode validates HTTP request correlation, cross-process event correlation, reindex lifecycle output, self-event handling, JSON parsing, and leakage boundaries without touching daily development resources:

```bash
scripts/verify-business.sh --logging-live
```

It exercises registration, login/logout, current-user, post, comment, like, search, notification, representative 400/401/404/503 responses, Redis fallback, Outbox publication, Business Worker processing/self-event ignore, Search Indexer convergence, and search-reindex start/completion. The default full acceptance additionally retains the Phase 0–3 reliability and search fault matrix and validates all four application log files. Panic recovery and request-ID entropy failure are covered by the Backend middleware tests rather than a production debug route.

## Troubleshooting

### A required port is occupied

`dev.sh` checks ports `5173`, `HTTP_PORT`, `MYSQL_PORT`, `REDIS_PORT`, `RABBITMQ_PORT`, `RABBITMQ_MANAGEMENT_PORT`, and `ELASTICSEARCH_PORT` before startup. Inspect a WSL listener with:

```bash
ss -ltnp 'sport = :8080'
```

Close the reported non-GoPulse process or change the applicable port in `.env`. Vite and `verify.sh` follow a non-default `HTTP_PORT` automatically.

### A Compose service is not healthy

```bash
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml ps
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml logs mysql redis rabbitmq elasticsearch
```

Check Docker daemon availability, port ownership, `.env` values, and the named container health log before retrying.

### A stale runtime record is reported

Runtime identity records are stored under `.run/`. Stop the foreground `dev.sh` with `Ctrl+C`, then run `scripts/down.sh`. Do not delete an actively held lock to bypass process ownership checks.

### `verify.sh` reports an endpoint failure

Use the configured port, for example:

```bash
curl --fail --show-error http://localhost:8080/health
curl --fail --show-error http://localhost:8080/ready
```

If `/health` succeeds but `/ready` returns `503`, inspect the dependency reported as `down`. A Redis-only readiness failure does not make MySQL-backed business APIs unavailable; post detail reads degrade to MySQL until Redis recovers. An Elasticsearch-only readiness failure makes search return `503 search_unavailable`, while already-started MySQL-backed APIs retain their existing behavior.

## Focused development checks

Backend:

```bash
cd backend
go test ./...
go vet ./...
go test -race ./...
```

Frontend:

```bash
cd frontend
npm test
npm run typecheck
npm run build
npm run test:e2e # requires a running isolated environment and Playwright Chromium
```

Repository governance, Bash syntax, and Compose configuration:

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch "$(git branch --show-current)"
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
```

Integration tests intentionally fail rather than skip when their isolated dependencies or explicit safety marker are absent. GitHub Actions provisions the whitelisted `gopulse_integration` database and Redis DB `15`, applies upward migrations, and runs:

```bash
cd backend
go test -count=1 -tags=integration ./...
```

Do not point that command at a development or production database. Reproduce it only with `INTEGRATION_TESTS=1`, `APP_ENV=test`, the exact whitelisted database/Redis DB values, and disposable MySQL/Redis resources.

## Product version metadata

The root `VERSION` file is the sole completed-product version source. `frontend/package.json` and the root package entries in `frontend/package-lock.json` mirror that value so npm output, build metadata, and dependency reports identify the same product version. `python3 scripts/ci/validate_versions.py` and the governance quality gate reject drift.

## Phase completion and current batch

Phase 1 core business delivery completed at `0.2.6`; the Phase 1 Review closeout completed at `0.2.7`. Phase 2-01 established the message contract and transactional Outbox at `0.3.1`; Phase 2-02 connected comment/first-like transactions to confirmed RabbitMQ delivery at `0.3.2`; Phase 2-03 added the independent, reconnecting Business Worker and idempotent notification persistence at `0.3.3`; Phase 2-04 added the recipient-scoped notification API and protected Frontend notification flow at `0.3.4`; Phase 2-05 integrated the Worker into the Bash lifecycle and passed the isolated reliability matrix at `0.3.5`. PR #39 merged that milestone into `main` on September 2, 2026 as `efff938`, and its required remote quality gates passed. Phase-02-06 performs the implementation Review closeout at `0.3.6`, adding Outbox retention cleanup, full-batch lease budgeting, controlled Worker cancellation, and no-op PR prevention. RabbitMQ remains transport rather than the final fact source, and broker failure does not invalidate an already committed MySQL business operation. Phase-03-01 delivered the rebuildable historical search loop at `0.4.1`, and Phase-03-02 delivered reliable, isolated incremental indexing and lifecycle/fault acceptance at `0.4.2`. Phase-03-03 closed the Phase 0–3 integration matrix and was merged by PR #50 on September 2, 2026 as `f54f1a2`, with all configured remote gates passing. Phase-03-04 is the sole `0.4.4` implementation-Review remediation batch: it adds PIT-stable search pagination, HMAC-protected cursors, correct pagination retry semantics, and authoritative Phase 3 status allocation. PR #51 merged `develop/0.4.4` after all push quality gates passed. Repository automation now treats those push gates as the single authoritative validation: `develop/*` runs the complete product suite, while planning-only `update` runs governance checks without duplicating Backend, Frontend, Compose, or Integration jobs. The separate pull-request CI was removed because PRs created with the workflow `GITHUB_TOKEN` require manual approval before their `pull_request` workflows can start. Milestone 1 is packaged by the release-only `develop/1.0.0` change, which synchronizes the root and Frontend product metadata to `1.0.0` and adds the [1.0.0 release notes](docs/releases/1.0.0.md). Publication is authoritative only after that change passes the remote push gates and is merged into `main`, whose root `VERSION` remains the source of truth. Phase-04-01 advances the product to `1.1.1` with Schema v1 Backend JSON logging, server-generated request IDs, structured access and panic recovery records, correlated business-action logs, safe cache-degradation warnings, and isolated `--logging-live` acceptance. Phase-04-02 closes Phase 4 at `1.1.2` by migrating Backend lifecycle, Outbox, Business Worker, Search Indexer, and search-reindex output to the same schema; event publication, processing, retry/dead, self-ignore, reconnect, and rebuild records use bounded fields and are validated by both focused logging acceptance and the retained Phase 0–3 business matrix.
