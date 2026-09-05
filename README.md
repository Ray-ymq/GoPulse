# GoPulse

GoPulse is currently at product version **1.9.1**. Phase 1 provides the browser-operable MySQL business system, Phase 2 adds transactional Outbox and RabbitMQ delivery, Phase 3 closes convergent Elasticsearch search, Phase 4 standardizes Schema v1 JSON logs, Phase 5 delivers the independent Redis Exporter, Phase 6 adds the authenticated Monitor Plugin Manager and metrics publishing, and Phase 7 closes the Message Router plus Kafka transport. Phase 8 closes Milestone 2 with the formal Marshaller consumer group, strict metrics Envelope v1 revalidation, deterministic Prometheus import conversion, authenticated single-node VictoriaMetrics storage/query, bounded dependency recovery, permanent-invalid continuation, deterministic replay, internal access isolation, and the full real Redis → Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics matrix. Phase 9 adds strict application-log transport, Elasticsearch storage, and administrator querying. Phase 10 includes successful and failed Redis Exporter lifecycle Events, unexpected-exit detection, deduplicated metrics collection and Redis-target failure/recovery episodes, bounded source retries, strict Elasticsearch storage, and administrator-only querying through the shared observability transport. Phase 11 closes Milestone 3 with a guarded administrator workspace, four-region overview, fixed VictoriaMetrics range queries, paged Logs and Events browsing, browser-operated Redis Exporter install/start/stop/update, runtime role-revocation handling, and dependency-isolated recovery, while ordinary users retain the social-only experience. Phase-12-01 packages the Frontend, Backend, Business Worker, Search Indexer, migrations, search initialization, and administrator CLI into non-root OCI images and closes the Docker/Compose-only social-business runtime. MySQL remains authoritative for business data, RabbitMQ remains the business-event transport, and Kafka remains limited to observability messages.

The repository currently provides:

- a Vue 3 + Vue Router Frontend for registration, login, logout, post listing/pagination, publishing, detail, comments, likes, authenticated search, notifications, authentication recovery, and an administrator-only observability overview with Exporter management;
- a diagnostic connectivity page at `/dev/status`, outside the business navigation;
- a Gin Backend with `/health`, `/ready`, and typed `/api/v1` business contracts;
- username/password authentication with bcrypt, short-lived HS256 JWTs, HttpOnly cookies, reusable authentication middleware, and database-authoritative `user|admin` roles;
- authenticated post publishing, keyset-paginated post/comment reads, comments, and idempotent likes;
- Redis cache-aside for the non-personalized post-detail projection with best-effort invalidation and MySQL fallback;
- versioned MySQL migrations for users and persistent roles, posts, comments, and post likes;
- local MySQL, Redis, RabbitMQ, fixed-version Elasticsearch, single-node Kafka 4.3.1, and authenticated single-node VictoriaMetrics 1.151.0 infrastructure;
- transactional `post.created` Outbox delivery through an isolated RabbitMQ topology and Search Indexer;
- single-line Schema v1 JSON lifecycle, HTTP, Outbox, Worker, Indexer, reindex, and Redis Exporter logs with bounded safe fields;
- an independent Redis Exporter whose `/health` reports process liveness and whose `/metrics` returns a complete current Prometheus snapshot or isolated `up 0`;
- fixed-catalog `GET /api/v1/observability/metrics` range queries backed by VictoriaMetrics, strict administrator-only Logs and Events querying, and a strict Backend trust boundary for Exporter status and actions;
- a loopback Message Router with strict Envelope v1 boundaries, Bearer service identity, explicit `metrics` routing, acknowledged Kafka production, and original-body byte preservation;
- a loopback Marshaller with strict second-pass Envelope validation, manual consumer-group offsets, generation ownership fencing, deterministic Prometheus text conversion, authenticated VictoriaMetrics writes, and isolated strict Logs and Events Elasticsearch targets;
- Docker/Compose-only daily business lifecycle scripts, read-only container verification, and a random-project real-browser business acceptance matrix;
- Frontend unit/component tests, real Chromium E2E acceptance, Backend unit/integration tests, and Linux quality gates.

Multiple Kafka topics, Schema Registry, SASL/TLS, multi-broker production topology, containerized observability workloads, Kubernetes, user profiles, follows, post update/delete indexing, automatic dead-queue replay, real-time notification push, and other later-phase capabilities are not implemented yet.

## Primary development environment

Starting with Phase-01-02, GoPulse uses WSL2 on Windows as its primary implementation and acceptance environment. Use the following baselines:

- WSL2 with a Linux distribution;
- Docker Desktop with WSL integration, or one WSL-native Docker Engine with the Docker Compose v2 plugin;
- Git and Bash for repository and Compose orchestration;
- Go 1.26, Node.js 24, npm 11, Python, and curl only when running focused source-level or historical isolated checks. They are not required by `dev.sh`, `verify.sh`, `down.sh`, or `verify-compose.sh --business`.

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

The checked-in credentials are development-only. Do not reuse them in production or commit `.env`. The root `VERSION`, Frontend package metadata, `.env.example` `GOPULSE_VERSION`, image tags, and OCI labels are kept on one version line.

`GOPULSE_RUNTIME_MODE` defaults to `host` for direct source-level commands. Host mode requires loopback listeners and loopback dependency origins. Compose sets `container` explicitly, binds the Backend to `0.0.0.0` inside its namespace, and injects only service DNS such as `mysql`, `redis`, `rabbitmq`, and `elasticsearch`. Unknown modes, container loopback/`host.docker.internal` downstreams, URL credentials where forbidden, paths, query strings, fragments, and unsafe listeners fail before application startup.

Only `PUBLISHED_HOST=127.0.0.1`, `HTTP_PORT`, and `FRONTEND_PORT` control default host publication. MySQL, Redis, RabbitMQ, and Elasticsearch have no ports in `deploy/compose.yaml`. `deploy/compose.debug.yaml` is an explicit loopback-only override for historical focused host checks and is never loaded by the daily or authoritative container acceptance paths.

## Start the development environment

Run from any directory:

```bash
/home/<user>/src/GoPulse/scripts/dev.sh
```

The script requires Docker/Compose rather than a host Go or Node toolchain. It validates the repository, branch/version, environment file, project name, and any existing Compose ownership labels; builds the versioned Frontend, Backend, Business Worker, Search Indexer, and acceptance images; starts MySQL, Redis, RabbitMQ, and Elasticsearch; waits for their health checks; requires `migrate up` and `search-reindex --if-missing` to exit successfully; then starts the independent application containers and runs the read-only container smoke.

The default project publishes only:

| Service | Address |
| --- | --- |
| Frontend production application and same-origin Backend proxy | `http://127.0.0.1:5173` |
| Backend direct API/liveness/readiness | `http://127.0.0.1:8080` |

The Frontend final image serves the compiled Vue application on container port 8080, falls back to `index.html` for Vue Router history routes, and proxies only `/api/v1`, `/health`, and `/ready` to `backend:8080`. Data services remain on the internal `business` network. Phase-12-01 deliberately leaves Monitor, Router, Marshaller, Kafka, VictoriaMetrics, and the managed Exporter outside the daily container topology; administrator observability calls therefore remain a documented local partial-unavailability state until Phase-12-02.

Use a different owned project or environment file explicitly when needed:

```bash
scripts/dev.sh --project-name gopulse-demo --env-file /path/to/development.env
```

## Verify a running environment

```bash
/home/<user>/src/GoPulse/scripts/verify.sh
```

`verify.sh` is read-only with respect to persistent application state. It validates project/service/working-directory labels, expected health and one-shot exit states, image numeric users and version labels, edge/business membership, and the rule that only Frontend and Backend publish IPv4 loopback ports. HTTP/JSON and SPA checks run in the one-shot acceptance image, so the host does not need curl, Node.js, npm, Python, or Go.

For the authoritative Phase-12-01 destructive integration acceptance, run:

```bash
scripts/verify-compose.sh --self-test
scripts/verify-compose.sh --business
```

The business mode builds a fresh random project and named volumes, cold-starts the complete social/search slice, reruns migrations and search initialization for idempotency, and drives the real browser through registration, login/logout, posts, comments, likes, notifications, deep links, Cookie persistence, and live search. It then proves Redis-to-MySQL fallback, paused Worker/Indexer recovery, Backend/Worker/Indexer replacement, clean bounded signal shutdown, and whole-project down/up with retained volumes. Every destructive operation validates project/service labels and the expected Compose working directory first; failure and signal cleanup removes only that random project and verifies that pre-existing containers, networks, and volumes still exist.

The previous `verify-business.sh` modes remain available as source-level historical regression tools. They load `deploy/compose.debug.yaml` explicitly and therefore require the host Go/Node/curl/Python toolchain; they are not evidence for the container-only runtime contract.

The no-Docker safety checks can be run independently:

```bash
scripts/verify-compose.sh --self-test
scripts/verify-business.sh --self-test
```

Lifecycle Events acceptance is likewise isolated and uses real Backend administrator plugin operations rather than direct Elasticsearch fixtures:

```bash
scripts/verify-events.sh --self-test
scripts/verify-events.sh
```

Message Router transport acceptance is isolated and destructive only inside a random owned Compose project. It proves strict authentication and Envelope rejection, original HTTP-body bytes and `message_id` record keys, real Monitor `success` and `target_unavailable` messages, Kafka stop/recovery without restarting Router or Monitor, bounded Consumer evidence, and complete process/container/network/volume cleanup:

```bash
scripts/verify-router.sh --self-test
scripts/verify-router.sh
```

Marshaller metrics acceptance is also isolated. It proves the complete real Redis → Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics path; all 10 metric families and 11 success samples; target-unavailable/recovery without application restart; three representative permanent-invalid continuations; internal Bearer/Basic and loopback boundaries; manual offset retention during VictoriaMetrics failure; Kafka/group and process recovery; replay of a captured real Envelope with one stable millisecond point; authenticated instant/range queries; invalid-row stability; and owned cleanup:

```bash
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
```

Monitor lifecycle and metrics acceptance is also isolated. It builds deterministic plugin packages, starts random-project Redis/MySQL plus a loopback HTTP capture fixture, verifies internal Bearer authentication, proves real Redis value changes in Envelope v1, exercises target-unavailable, malformed-data, Publisher-failure, stop/start/update/rollback, and restart recovery:

```bash
scripts/verify-monitor.sh --self-test
scripts/verify-monitor.sh
```

Redis Exporter acceptance is intentionally separate from the full business stack. It starts only an isolated password-protected Redis 7.2.5 and temporary Exporter, then proves current `INFO` values, stopped-target and authentication failure isolation, timeout handling, recovery without restart, SIGTERM shutdown, and ownership-safe cleanup:

```bash
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
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

`down.sh` validates the project name plus container/network/volume Compose labels and the expected `deploy/compose.yaml` working directory before calling `docker compose down`. It removes only the verified containers and networks and preserves named volumes by default. Explicit volume deletion requires both `--volumes` and an exact `--confirm-project NAME`; images are never pruned.

## Database migrations

The Backend image contains the embedded, versioned migration binary used by the Compose one-shot job:

```bash
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml run --rm migrate
```

For focused source-level development, the equivalent host-toolchain command remains `cd backend && go run ./cmd/migrate up`.

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

All successful JSON responses use the common `data` envelope. Authentication responses and `/users/me` contain only `id`, `username`, `role`, and `created_at`; `role` is always `user` or `admin`. Password hashes and JWTs are never returned in JSON. Public post/comment/notification author summaries remain limited to `id` and `username` and never expose roles.

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
  - returns HTTP `200` and the current user DTO, including the role read from MySQL for this request;
  - missing, expired, malformed, or tampered tokens return `401 authentication_required`;
  - a token whose user no longer exists also clears the cookie and returns `401`.

The cookie uses the configured name with `HttpOnly`, `SameSite=Lax`, `Path=/`, no broad `Domain`, and a lifetime coordinated with `AUTH_JWT_TTL`. Production forces the `Secure` attribute. JWT validation accepts only HS256 and requires positive decimal `sub`, `iat`, and `exp` claims. JWTs carry only the stable user ID; they do not carry or authorize from a role claim.

### Administrator identity and authorization

Every registration creates an ordinary `user`. GoPulse does not create a default administrator, promote the first account, accept an administrator role from registration JSON, or provide a browser-based role editor. After applying migrations, a server operator can explicitly promote each intended administrator with the Backend environment configured:

```bash
cd backend
go run ./cmd/admin-role promote --username alice
```

Promotion uses the same username normalization as login, fails for an unknown user, and succeeds idempotently when the user is already an administrator. It does not print credentials, tokens, database connection details, or user records. Phase 6 currently provides promotion only; demotion, disabling, deletion, role listing, and a management UI are not implemented.

The same account and HttpOnly session continue to work after promotion. `/api/v1/users/me` and administrator authorization read the current role from MySQL, so an existing valid Cookie observes the promotion without a second login protocol. Administrator authorization is a Backend boundary; future Frontend navigation checks are only presentation behavior.

| Capability | Anonymous | `user` | `admin` | Internal service identity |
| --- | --- | --- | --- | --- |
| Existing public social behavior | Existing contract | Existing contract | Existing contract | Not applicable |
| Authenticated posts, comments, likes, search, and notifications | `401 authentication_required` | Allowed | Allowed | Not applicable |
| Metrics, logs, and events queries | `401 authentication_required` | `403 permission_denied` | Allowed | Separate internal contract |
| Exporter installation, query, and lifecycle management | `401 authentication_required` | `403 permission_denied` | Allowed | Monitor token |
| Monitor, Router, Marshaller, and storage internal APIs | Denied | Denied | Browsers do not connect directly | Separate service authentication and controlled network |

The reusable Backend administrator middleware performs authentication first and then loads the current database role. A rejected request does not call the protected handler or downstream management/storage capability. The management and observability routes listed above are authorization contracts for later Phase 6 and subsequent phases; this batch does not add those public routes or a management page.

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

For every request whose server-side identifier is generated successfully, the Backend returns `X-Request-ID` as 32 lowercase hexadecimal characters. Client-provided values are ignored. The same identifier joins the response to one `http request completed` record and, for successful state changes, to the corresponding business record. Completion logs use the registered Gin route template rather than the raw path and include only method, status, integer duration/response size, authenticated numeric user ID when available, and the public error code when applicable. Gin framework debug writers are suppressed in every application environment so development startup does not mix text lines into the JSON stream. A panic before response commit returns the safe `500 internal_error` envelope. If a handler panics after HTTP bytes are committed, the irreversible wire status/body are preserved without appending a second error envelope; both panic and completion records use error severity and expose only `internal_error`, `panic_recovered`, and `response_committed` metadata. Request bodies, query values, credentials, cookies, tokens, user content, connection URLs, panic values, and stack traces are not logged.

Outbox publication and Worker/Indexer completion records carry the existing `event_id`, stable `event_type`, numeric attempt, and bounded reason only after the relevant publish/mark/ack transition succeeds. Search completion may also include the numeric `post_id`. These asynchronous records deliberately do not copy HTTP request IDs into the Envelope, and no application log includes message payloads, AMQP headers, connection URLs, search documents, index generations, or user content.

The focused isolated acceptance mode validates HTTP request correlation, cross-process event correlation, reindex lifecycle output, self-event handling, JSON parsing, and leakage boundaries without touching daily development resources:

```bash
scripts/verify-business.sh --logging-live
```

It exercises registration, login/logout, current-user, post, comment, like, search, notification, representative 400/401/404/503 responses, Redis fallback, Outbox publication, Business Worker processing/self-event ignore, Search Indexer convergence, and search-reindex start/completion. The default full acceptance additionally retains the Phase 0–3 reliability and search fault matrix and validates all four application log files. Panic recovery and request-ID entropy failure are covered by the Backend middleware tests rather than a production debug route.

## Troubleshooting

### A required port is occupied

Only Frontend and Backend are published by default. Inspect `FRONTEND_PORT` or `HTTP_PORT` with `ss -ltnp`, close the unrelated listener, or change the port in `.env`. Data-service ports are not a daily-startup conflict because they remain internal.

### A Compose service is not healthy

```bash
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml ps
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml logs mysql redis rabbitmq elasticsearch migrate search-init backend frontend
```

Initialization failures are intentionally visible and block dependent services. Startup preserves named volumes for diagnosis; rerunning is safe because upward migrations and `search-init --if-missing` are idempotent.

### A project ownership check is rejected

Do not bypass the label check or manually reuse the project name. Inspect `com.docker.compose.project`, `com.docker.compose.service`, `com.docker.compose.project.working_dir`, and `com.docker.compose.project.config_files`. Use the correct workspace/project or choose a new project name.

### `verify.sh` reports an endpoint failure

Use `docker compose logs` for the named service. `/health` is process liveness; `/ready` retains the existing business dependency contract. Redis failure permits MySQL-backed fallback, while Elasticsearch failure makes search unavailable and RabbitMQ failure delays asynchronous delivery.

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

Redis Exporter:

```bash
cd exporters/redis
test -z "$(gofmt -l .)"
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
cd ../..
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
```

Repository governance, Bash syntax, and Compose configuration:

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch "$(git branch --show-current)"
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
```

Integration tests intentionally fail rather than skip when their isolated dependencies or explicit safety marker are absent. GitHub Actions provisions the whitelisted `gopulse_integration` database and Redis DB `15`, applies upward migrations, and runs:

```bash
cd backend
go test -count=1 -tags=integration ./...
```

Do not point that command at a development or production database. Reproduce it only with `INTEGRATION_TESTS=1`, `APP_ENV=test`, the exact whitelisted database/Redis DB values, and disposable MySQL/Redis resources.

## Product version metadata

The root `VERSION` file is the sole completed-product version source. `frontend/package.json`, the root package entries in `frontend/package-lock.json`, and `.env.example` `GOPULSE_VERSION` mirror that value so npm output, Compose tags, OCI labels, and dependency reports identify the same product version. `python3 scripts/ci/validate_versions.py` and the governance quality gate reject drift.

## Phase completion and current batch

Phase 1 core business delivery completed at `0.2.6`; the Phase 1 Review closeout completed at `0.2.7`. Phase 2-01 established the message contract and transactional Outbox at `0.3.1`; Phase 2-02 connected comment/first-like transactions to confirmed RabbitMQ delivery at `0.3.2`; Phase 2-03 added the independent, reconnecting Business Worker and idempotent notification persistence at `0.3.3`; Phase 2-04 added the recipient-scoped notification API and protected Frontend notification flow at `0.3.4`; Phase 2-05 integrated the Worker into the Bash lifecycle and passed the isolated reliability matrix at `0.3.5`. PR #39 merged that milestone into `main` on September 2, 2026 as `efff938`, and its required remote quality gates passed. Phase-02-06 performs the implementation Review closeout at `0.3.6`, adding Outbox retention cleanup, full-batch lease budgeting, controlled Worker cancellation, and no-op PR prevention. RabbitMQ remains transport rather than the final fact source, and broker failure does not invalidate an already committed MySQL business operation. Phase-03-01 delivered the rebuildable historical search loop at `0.4.1`, and Phase-03-02 delivered reliable, isolated incremental indexing and lifecycle/fault acceptance at `0.4.2`. Phase-03-03 closed the Phase 0–3 integration matrix and was merged by PR #50 on September 2, 2026 as `f54f1a2`, with all configured remote gates passing. Phase-03-04 is the sole `0.4.4` implementation-Review remediation batch: it adds PIT-stable search pagination, HMAC-protected cursors, correct pagination retry semantics, and authoritative Phase 3 status allocation. PR #51 merged `develop/0.4.4` after all push quality gates passed. Repository automation now treats those push gates as the single authoritative validation: `develop/*` runs the complete product suite, while planning-only `update` runs governance checks without duplicating Backend, Frontend, Compose, or Integration jobs. The separate pull-request CI was removed because PRs created with the workflow `GITHUB_TOKEN` require manual approval before their `pull_request` workflows can start. Milestone 1 is packaged by the release-only `develop/1.0.0` change, which synchronizes the root and Frontend product metadata to `1.0.0` and adds the [1.0.0 release notes](docs/releases/1.0.0.md). Publication is authoritative only after that change passes the remote push gates and is merged into `main`, whose root `VERSION` remains the source of truth. Phase-04-01 advances the product to `1.1.1` with Schema v1 Backend JSON logging, server-generated request IDs, structured access and panic recovery records, correlated business-action logs, safe cache-degradation warnings, and isolated `--logging-live` acceptance. Phase-04-02 closes Phase 4 at `1.1.2` by migrating Backend lifecycle, Outbox, Business Worker, Search Indexer, and search-reindex output to the same schema; event publication, processing, retry/dead, self-ignore, reconnect, and rebuild records use bounded fields and are validated by both focused logging acceptance and the retained Phase 0–3 business matrix. Phase-04-03 completed the implementation-review remediation at `1.1.3`. Phase-05-01 advances the product to `1.2.1` with the independent Redis Exporter, strict Prometheus metric contract, target-failure isolation, Bash lifecycle ownership, isolated real-Redis acceptance, and a dedicated CI job. Phase-05-02 completed the stage-level integration closeout at `1.2.2`; Phase-05-03 closes the implementation Review findings at `1.2.3` by hardening host validation, isolated cleanup, port allocation, and branch governance.


Phase-08-01 advanced the product to `1.5.1` with the formal `gopulse-marshaller-metrics-v1` consumer, manual offset decisions guarded by partition-generation ownership, strict metrics Envelope v1 revalidation, deterministic Prometheus import text, authenticated VictoriaMetrics 1.151.0 storage/query, and isolated real-upstream acceptance. Phase-08-02 advanced the product to `1.5.2` by proving bounded Kafka and VictoriaMetrics dependency recovery, formal-group rejoin after broker restart, committed-offset re-fetch after an explicitly uncommitted Marshaller termination, deterministic duplicate delivery with one stable millisecond point, invalid-row stability, stronger read-only group/query verification, and strongly owned process/container/network/volume cleanup. Phase-08-03 advanced the product to `1.5.3` and closed Milestone 2 with the full 10-family/11-sample real matrix, three permanent-invalid classes, captured-real replay, internal access negatives, Kafka/VM-unavailable business isolation, and resource snapshots. PR #77 merged the batch after all 10 authoritative push jobs passed. Delivery remains at-least-once rather than exactly-once.

Phase-12-01 advances the product to `1.9.1`: the social and search runtime now builds and starts from Docker/Compose without host Go, Node.js, npm, curl, or Python; application images are fixed-version, non-root, multi-stage artifacts with shared OCI metadata; migration and search initialization are success-gated one-shot jobs; only Frontend/Backend publish loopback ports; and the random-project Chromium acceptance proves recovery, replacement, signal, and retained-volume behavior. Containerized observability is explicitly deferred to Phase-12-02.

### Backend log query pipeline

Backend, Business Worker, Search Indexer, and search-reindex Schema v1 logs remain single-line JSON on stdout and, when `LOG_MONITOR_URL` is configured, are also offered to the same bounded non-blocking in-memory shipper. The shipper uses the dedicated `LOG_MONITOR_INGEST_TOKEN`; queue full affects only the remote copy, temporary transport failures retain the ordered queue head and message ID for retry, and permanent `400`/`413`/`422` input rejection drops only that remote copy. None of these outcomes changes API, RabbitMQ acknowledgement, Outbox, indexing, or reindex exit semantics. LogMonitor derives one of the fixed `logs/backend`, `logs/business-worker`, `logs/search-indexer`, or `logs/search-reindex` envelopes from the validated service, Router transports all four through `gopulse-observability-v1`, and Marshaller revalidates the source/payload match before idempotently storing strict documents in `gopulse-logs-v1-YYYY.MM.DD` behind `gopulse-logs-v1-read`. The formal single-partition consumer group remains `gopulse-marshaller-metrics-v1`, so a temporary Elasticsearch failure intentionally backpressures later logs and metrics until the current record succeeds. Phase-09-02 delivered all background log sources at `1.6.2`. Phase-09-03 closes Phase 9 at `1.6.3` with the final real request/event/reindex matrix, exact-filter and PIT pagination checks, credential and sensitive-data isolation, strict index/mapping separation, Elasticsearch outage recovery, Metrics coexistence, business-fault isolation, and owned resource cleanup.

Administrators can query the fixed read alias through `GET /api/v1/observability/logs`. Supported filters are `from`, `to`, `service`, `module`, `level`, `message`, `request_id`, `event_id`, `error_code`, `limit`, and signed `cursor`. The default range is 15 minutes, the maximum range is 24 hours, and page size is limited to 100. Authentication and current MySQL administrator authorization run before Elasticsearch access. Use `scripts/verify-logs.sh --self-test` for safety checks and `scripts/verify-logs.sh` for the isolated real API-to-Elasticsearch acceptance.


## Administrator observability workspace

Administrators can open `/admin/observability` from the main navigation. The overview independently loads the latest fixed Redis availability metric, recent application Logs, recent Monitor Events, and the current Redis Exporter fact; one unavailable dependency does not erase successful regions. Dedicated pages support the fixed query catalogs and Redis Exporter install/start/stop/update operations. The Backend remains the authorization and trust boundary: metric expressions, labels, time steps, Elasticsearch DSL, index names, PIT values, VictoriaMetrics credentials, Monitor internals, and raw upstream responses are never accepted from or exposed to the browser. Ordinary users have no navigation entry, direct management URLs resolve to `/forbidden` before any management API request, and every Exporter route repeats real-time Backend authorization.

The Phase 11 final acceptance evidence starts with an empty isolated plugin root, completes install and upgrade through Chromium, verifies generated request-ID filters and multi-page Logs/Events, exercises VictoriaMetrics/Monitor/Elasticsearch fault windows, confirms social writes during observability degradation, checks narrow-screen and keyboard-visible controls, demotes an active administrator through isolated test SQL, scans the production bundle for internal identities, and finishes with the former host lifecycle plus owned-resource cleanup.

During Phase-12-01, `scripts/verify-observability-ui.sh --self-test` remains as a static safety regression, but its full host-lifecycle invocation and CI job are explicitly suspended on `develop/1.9.1`: `dev.sh` is now container-native while Monitor, Router, Marshaller, and the managed Exporter are intentionally deferred. Phase-12-02 must containerize that chain and reactivate the real-browser observability gate without weakening the Phase 11 authorization and trust-boundary evidence.
