# GoPulse

GoPulse is currently at the **Phase 0 engineering skeleton** milestone. The repository provides a Vue 3 connectivity dashboard, a Gin backend, local MySQL/Redis/RabbitMQ infrastructure, and cross-platform commands for starting, verifying, and stopping the complete development environment.

Phase 0 intentionally contains no business APIs, database schema or migrations, RabbitMQ business topology, authentication, application containers, or production observability stack.

## Prerequisites

Use these development baselines:

- Go 1.26
- Node.js 24
- npm 11
- Docker Desktop or Docker Engine with the Docker Compose v2 plugin
- PowerShell 7 on Windows, or Bash with `python3`, `curl`, `flock`, `ps`, and `sha256sum` on Unix-like systems

The development scripts validate the required tools before starting services.

## Local configuration

The first `dev` run creates `.env` from [`.env.example`](.env.example) when `.env` is absent. You may also create it explicitly:

```powershell
Copy-Item .env.example .env
```

```bash
cp .env.example .env
```

The checked-in values are development-only credentials. Do not reuse them in production or commit a local `.env`. The scripts accept a restricted dotenv format and inject only the configuration required by Compose and the Backend; infrastructure credentials are not injected into the Frontend process.

## Start the development environment

The commands resolve the repository from their own script path, so they can be invoked from any working directory.

### Windows

```powershell
E:\GoPulse\scripts\dev.ps1
```

### Unix-like systems

```bash
/path/to/GoPulse/scripts/dev.sh
```

A successful start provides:

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

Keep the foreground `dev` command running. Press `Ctrl+C` to stop the Backend and Frontend while leaving the Compose infrastructure available.

## Verify a running environment

`verify` is read-only: it does not start or stop processes or containers, manufacture failures, edit runtime records, or remove volumes. It checks that the three Compose services are healthy, validates the structured `/health` and `/ready` contracts, and confirms that the Frontend responds over HTTP.

### Windows

```powershell
E:\GoPulse\scripts\verify.ps1
```

### Unix-like systems

```bash
/path/to/GoPulse/scripts/verify.sh
```

A passing verification exits with status `0`. A failed check exits nonzero, names the failed item, and prints a focused diagnostic hint.

## Stop the environment

### Windows

```powershell
E:\GoPulse\scripts\down.ps1
```

### Unix-like systems

```bash
/path/to/GoPulse/scripts/down.sh
```

`down` stops the recorded application processes and the `gopulse` Compose project. It is safe to run repeatedly. Normal shutdown preserves the named MySQL, Redis, and RabbitMQ volumes; removing those volumes requires a separate, explicit Docker action.

## API contracts

`GET /health` reports only Backend process liveness:

```json
{"status":"ok","service":"backend"}
```

`GET /ready` checks MySQL, Redis, and RabbitMQ concurrently. It returns HTTP `200` with all checks `up`, or HTTP `503` with each dependency accurately marked `up` or `down`. Dependency errors and credentials are not returned to clients.

The Frontend renders the Backend plus the three dependency states and refreshes them without requiring an application restart after infrastructure recovery.

## Troubleshooting

### A required port is occupied

`dev` checks ports `5173`, `8080`, `3306`, `6379`, `5672`, and `15672` before startup. Close the reported non-GoPulse process or change the applicable port in `.env`. The Frontend proxy currently targets Backend port `8080`, so keep `HTTP_PORT=8080` for the standard Phase 0 flow.

On Windows, inspect a listener with:

```powershell
Get-NetTCPConnection -State Listen -LocalPort 8080 | Select-Object LocalAddress,LocalPort,OwningProcess
Get-Process -Id <PID>
```

On Unix-like systems:

```bash
ss -ltnp 'sport = :8080'
```

### A Compose service is not healthy

```powershell
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml ps
docker compose --project-name gopulse --env-file .env --file deploy/compose.yaml logs mysql redis rabbitmq
```

Check Docker daemon availability, port ownership, `.env` values, and the specific container health log before retrying.

### A stale runtime record is reported

The development commands store process identity records under `.run/`. `down` validates the PID, stable process-start identity, executable, working directory, and command marker before stopping anything. A stale or mismatched record is removed without killing the unrelated process.

First run the matching platform stop command. If it reports that the development lock is still active, return to the foreground `dev` terminal and stop it with `Ctrl+C`; do not delete an actively held lock to bypass process ownership checks.

### `verify` reports an endpoint failure

Confirm the foreground `dev` session is still active, then inspect:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/ready
```

If `/health` succeeds but `/ready` returns `503`, inspect the dependency named `down`; the Backend is designed to recover readiness after the service returns without a Backend restart.

## Focused development checks

Backend:

```bash
cd backend
go test ./...
go vet ./...
```

Frontend:

```bash
cd frontend
npm test
npm run typecheck
npm run build
```

Script syntax:

```powershell
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path scripts/verify.ps1), [ref]$null, [ref]$errors) > $null
$errors
```

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh
```

## Phase 1 handoff

Phase 1 should extend the existing Backend module and reuse the established configuration loader, MySQL/Redis clients, Compose project, readiness contract, and local lifecycle commands. It should not recreate the Phase 0 infrastructure entry points.
