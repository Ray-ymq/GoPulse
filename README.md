# GoPulse

GoPulse is currently at product version **0.3.1**. Phase 1 provides a browser-operable minimum business system backed by MySQL, with Redis used only as a degradable post-detail cache. Phase 2 now has its message-contract and transactional Outbox persistence foundation, but business requests do not publish asynchronous notifications yet.

The repository currently provides:

- a Vue 3 + Vue Router Frontend for registration, login, logout, post listing/pagination, publishing, detail, comments, likes, and authentication recovery;
- a diagnostic connectivity page at `/dev/status`, outside the business navigation;
- a Gin Backend with `/health`, `/ready`, and typed `/api/v1` business contracts;
- username/password authentication with bcrypt, short-lived HS256 JWTs, HttpOnly cookies, and reusable authentication middleware;
- authenticated post publishing, keyset-paginated post/comment reads, comments, and idempotent likes;
- Redis cache-aside for the non-personalized post-detail projection with best-effort invalidation and MySQL fallback;
- versioned MySQL migrations for users, posts, comments, and post likes;
- local MySQL, Redis, and RabbitMQ infrastructure;
- WSL/Bash lifecycle scripts, read-only runtime verification, and a destructive-but-isolated complete business acceptance script;
- Frontend unit/component tests, real Chromium E2E acceptance, Backend unit/integration tests, and Linux quality gates.

Elasticsearch, Kafka, application containers, Kubernetes, profiles, follows, search, notifications, and other later-phase capabilities are not implemented yet.

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

Workspaces created before Phase-01-01 must manually add the Phase 1 values from `.env.example`, including `AUTH_JWT_SECRET`, `AUTH_JWT_TTL`, `AUTH_COOKIE_NAME`, `AUTH_COOKIE_SECURE`, `REDIS_POST_DETAIL_TTL`, and `REDIS_OPERATION_TIMEOUT`. The development script does not overwrite an existing `.env`.

The checked-in values are development-only credentials. Do not reuse them in production or commit a local `.env`. `APP_ENV` must be `development`, `test`, or `production`. Production requires `AUTH_COOKIE_SECURE=true`; local development and tests may explicitly use `false` for HTTP.

By default, `PUBLISHED_HOST` and `HTTP_HOST` bind infrastructure ports and the Backend only to `127.0.0.1`. Setting either value to a non-loopback address is an explicit remote-access choice.

## Start the development environment

Run from any directory:

```bash
/home/<user>/src/GoPulse/scripts/dev.sh
```

The script performs the following sequence:

1. validates the required tools and configuration;
2. starts MySQL, Redis, and RabbitMQ with Docker Compose;
3. waits for all infrastructure health checks;
4. runs `go run ./cmd/migrate up` in `backend/`;
5. builds and starts the Backend;
6. installs reproducible Frontend dependencies when required and starts Vite.

A failed migration stops Backend and Frontend startup. With the default configuration, the environment provides:

| Service | Address |
| --- | --- |
| Frontend business application | `http://localhost:5173` |
| Backend | `http://localhost:8080` |
| Backend liveness | `http://localhost:8080/health` |
| Backend readiness | `http://localhost:8080/ready` |
| Authentication API | `http://localhost:8080/api/v1` |
| RabbitMQ management | `http://localhost:15672` |
| MySQL | `localhost:3306` |
| Redis | `localhost:6379` |
| RabbitMQ AMQP | `localhost:5672` |

When `HTTP_PORT` changes, the Backend, Vite proxies for `/health`, `/ready`, and `/api/v1`, and `verify.sh` all use the same resolved port. Caller environment overrides handled by `dev.sh` are passed explicitly to Vite.

Keep the foreground command running. `Ctrl+C` stops Backend and Frontend while leaving the Compose infrastructure and named volumes available.

## Verify a running environment

```bash
/home/<user>/src/GoPulse/scripts/verify.sh
```

`verify.sh` is read-only. It reads the configured `HTTP_PORT`, checks the three Compose services, validates `/health` and `/ready`, confirms that an unauthenticated protected API returns `401 authentication_required`, and confirms that the Frontend responds over HTTP. It never creates users, posts, comments, likes, or cache entries.

For complete destructive integration acceptance, run:

```bash
/home/<user>/src/GoPulse/scripts/verify-business.sh
```

`verify-business.sh` creates a random 12-character acceptance token and uses it to derive a strictly whitelisted Compose project and database. It allocates non-default loopback ports, uses a temporary environment and process directory, and never modifies `.env` or `.run`. The script exercises API and real Chromium flows, Backend restart persistence, acceptance-Redis `FLUSHDB`, Redis outage degradation, and recovery without restarting Backend. Before stopping, restarting, clearing, or deleting anything, it validates project labels, container IDs, and published ports. Exit, failure, and signal traps remove only that verified acceptance project and its volumes, then compare the daily development stack snapshot.

The no-Docker negative safety checks can be run independently:

```bash
scripts/verify-business.sh --self-test
```

## Stop the environment

```bash
/home/<user>/src/GoPulse/scripts/down.sh
```

`down.sh` validates and stops recorded application processes, removes the `gopulse` Compose containers and network, and preserves the MySQL, Redis, and RabbitMQ named volumes. It is safe to run repeatedly.

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

The Backend defines a strict, versioned JSON envelope for `comment.created` and `post.liked`. Version 1 messages are limited to 16 KiB, reject unknown fields and multiple JSON values, carry only stable numeric identifiers plus a UTC occurrence time, and use the event UUID as AMQP `message_id`. Routing keys are `comment.created.v1` and `post.liked.v1`; payloads intentionally exclude bodies, usernames, credentials, tokens, Cookies, connection URLs, and underlying errors.

The shared durable direct topology contract is centralized in Backend code:

| Role | Name |
| --- | --- |
| Main exchange | `gopulse.business.v1` |
| Main queue | `gopulse.business-worker.v1` |
| Retry exchange / queue | `gopulse.business.retry.v1` / `gopulse.business-worker.retry.v1` |
| Dead exchange / queue | `gopulse.business.dead.v1` / `gopulse.business-worker.dead.v1` |

Migration `000002_business_outbox` adds the constrained `business_outbox` table. Comment creation and a user's first non-self like now write their business fact and event in the same MySQL transaction; duplicate likes, self actions, and unlike operations do not create notification events. Redis invalidation remains a best-effort operation after commit.

The Backend starts a lifecycle-bound Outbox Dispatcher that claims finite leased batches and lazily connects to RabbitMQ. It publishes persistent mandatory messages, waits for publisher confirms, and marks a row published only after a confirmed routable delivery. Broker outages, nacks, returns, timeouts, and connection loss leave the MySQL fact committed and release or preserve the event for bounded retry. `OUTBOX_POLL_INTERVAL`, `OUTBOX_CLAIM_BATCH`, `OUTBOX_LEASE_DURATION`, `OUTBOX_PUBLISH_TIMEOUT`, and `OUTBOX_RETRY_DELAY` control the delivery loop.

Delivery is intentionally at least once: a crash after RabbitMQ confirms a publish but before MySQL records `published` can deliver the same `event_id` again. This version still does **not** run a Business Worker, create notifications, consume retry/dead queues, or expose a notification HTTP API.

## Frontend routes

| Route | Access | Purpose |
| --- | --- | --- |
| `/register` | anonymous | Create an account and establish the login Cookie |
| `/login` | anonymous | Authenticate with username and password |
| `/posts` | authenticated | Newest-first post list with cursor-based loading |
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

MySQL remains the source of truth. Redis stores only the versioned public post-detail projection under `gopulse:post:detail:v1:{postId}` for `REDIS_POST_DETAIL_TTL`; the value excludes `liked_by_me`, comments, credentials, tokens, and connection data. Every detail request calculates `liked_by_me` separately from MySQL for the authenticated viewer.

A detail cache miss, timeout, connection failure, damaged JSON, unsupported cache version, or failed refill falls back to MySQL. Successful comment, like, and unlike operations attempt cache invalidation only after the MySQL write succeeds. Cache invalidation failures never roll back or change a successful business response.

Cache-aside has an explicitly bounded eventual-consistency window: a failed invalidation or an older concurrent read that refills after a successful invalidation can temporarily expose stale public counts until `REDIS_POST_DETAIL_TTL` expires or the key is cleared. Such cached data never overwrites MySQL facts and never controls viewer-specific `liked_by_me`.

If Redis is unavailable, `/ready` returns HTTP `503` with Redis marked `down`, while the authenticated MySQL business APIs continue operating through cache fallback. After Redis recovers, readiness and cache operations recover without restarting the Backend.

## Troubleshooting

### A required port is occupied

`dev.sh` checks ports `5173`, `HTTP_PORT`, `MYSQL_PORT`, `REDIS_PORT`, `RABBITMQ_PORT`, and `RABBITMQ_MANAGEMENT_PORT` before startup. Inspect a WSL listener with:

```bash
ss -ltnp 'sport = :8080'
```

Close the reported non-GoPulse process or change the applicable port in `.env`. Vite and `verify.sh` follow a non-default `HTTP_PORT` automatically.

### A Compose service is not healthy

```bash
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml ps
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml logs mysql redis rabbitmq
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

If `/health` succeeds but `/ready` returns `503`, inspect the dependency reported as `down`. A Redis-only readiness failure does not make MySQL-backed business APIs unavailable; post detail reads degrade to MySQL until Redis recovers.

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

## Phase 1 completion and Phase 2 progress

Phase 1 core business delivery completed at `0.2.6`; the Phase 1 Review closeout completed at `0.2.7`. Phase 2-01 completes the business event, RabbitMQ topology, and transactional Outbox persistence contracts at `0.3.1`. The usable browser flow and synchronous Backend facts remain unchanged: comments and likes do not yet create Outbox rows or RabbitMQ messages. Later Phase 2 batches may connect asynchronous actions only through the documented MySQL transaction boundary; RabbitMQ must not become the final fact source, and broker failure must not invalidate an already committed MySQL operation.
