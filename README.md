# GoPulse

GoPulse is currently at product version **0.2.1**. Phase 0 is complete and Phase-01-01 has delivered the Phase 1 database migration and HTTP contract baseline.

The repository currently provides:

- a Vue 3 connectivity dashboard;
- a Gin Backend with `/health`, `/ready`, and an `/api/v1` assembly boundary;
- versioned MySQL migrations for users, posts, comments, and post likes;
- local MySQL, Redis, and RabbitMQ infrastructure;
- WSL/Bash commands for starting, verifying, and stopping the development environment.

Registration, login, authentication middleware, posts, comments, likes, the Phase 1 business UI, Elasticsearch, Kafka, application containers, and Kubernetes are not implemented yet.

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

Workspaces created before Phase-01-01 must manually add the new Phase 1 values from `.env.example`, including `AUTH_JWT_SECRET`, `AUTH_JWT_TTL`, `AUTH_COOKIE_NAME`, `AUTH_COOKIE_SECURE`, `REDIS_POST_DETAIL_TTL`, and `REDIS_OPERATION_TIMEOUT`. The development script does not overwrite an existing `.env`.

The checked-in values are development-only credentials. Do not reuse them in production or commit a local `.env`. By default, `PUBLISHED_HOST` and `HTTP_HOST` bind infrastructure ports and the Backend only to `127.0.0.1`; setting either value to a non-loopback address is an explicit remote-access choice.

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

A failed migration stops Backend and Frontend startup. A successful default start provides these loopback-only endpoints:

| Service | Address |
| --- | --- |
| Frontend connectivity dashboard | `http://localhost:5173` |
| Backend | `http://localhost:8080` |
| Backend liveness | `http://localhost:8080/health` |
| Backend readiness | `http://localhost:8080/ready` |
| RabbitMQ management | `http://localhost:15672` |
| MySQL | `localhost:3306` |
| Redis | `localhost:6379` |
| RabbitMQ AMQP | `localhost:5672` |

Keep the foreground command running. `Ctrl+C` stops Backend and Frontend while leaving the Compose infrastructure and named volumes available.

## Verify a running environment

```bash
/home/<user>/src/GoPulse/scripts/verify.sh
```

`verify.sh` is read-only. It checks the three Compose services, validates `/health` and `/ready`, and confirms that the Frontend responds over HTTP. A passing verification exits with status `0`; a failure exits nonzero with a focused diagnostic.

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

## Current HTTP contracts

`GET /health` reports only Backend process liveness:

```json
{"status":"ok","service":"backend"}
```

`GET /ready` checks MySQL, Redis, and RabbitMQ concurrently. It returns HTTP `200` when all checks are `up`, or HTTP `503` with each dependency accurately marked `up` or `down`. Dependency errors and credentials are not returned to clients.

`/api/v1` is present as the Phase 1 API assembly boundary, but no user-facing business endpoint is registered at version `0.2.1`.

## Troubleshooting

### A required port is occupied

`dev.sh` checks ports `5173`, `8080`, `3306`, `6379`, `5672`, and `15672` before startup. Inspect a WSL listener with:

```bash
ss -ltnp 'sport = :8080'
```

Close the reported non-GoPulse process or change the applicable port in `.env`. At version `0.2.1`, Vite still targets Backend port `8080`; dynamic Vite proxy configuration is assigned to Phase-01-02.

### A Compose service is not healthy

```bash
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml ps
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml logs mysql redis rabbitmq
```

Check Docker daemon availability, port ownership, `.env` values, and the named container health log before retrying.

### A stale runtime record is reported

Runtime identity records are stored under `.run/`. Stop the foreground `dev.sh` with `Ctrl+C`, then run `scripts/down.sh`. Do not delete an actively held lock to bypass process ownership checks.

### `verify.sh` reports an endpoint failure

```bash
curl --fail --show-error http://localhost:8080/health
curl --fail --show-error http://localhost:8080/ready
```

If `/health` succeeds but `/ready` returns `503`, inspect the dependency reported as `down`.

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
```

Repository governance, Bash syntax, and Compose configuration:

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_branch.py --branch "$(git branch --show-current)"
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
```

## Phase 1 handoff

Phase-01-01 is complete at `VERSION=0.2.1`. Phase-01-02 uses the authoritative `develop/0.2.2` allocation. Before implementing authentication, it must close the Phase-01-01 follow-up items recorded in the active Phase 1 plans: readiness checker isolation, complete HTTP Server resource limits, dynamic Vite proxy configuration, Gin mode mapping, and isolated integration CI.
