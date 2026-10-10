# GoPulse

GoPulse is a Go social application used as a real workload for an observability
and reliability experiment platform. It combines a modular business core with
metrics, logs, events, internal alerts, and controlled recovery experiments.

The completed product version is recorded in [VERSION](VERSION). Verified
capabilities, evidence boundaries, pending work, and limitations are maintained in
[capability status](dev/status/capability-status.md).

## Read first

| Purpose | Entry |
| --- | --- |
| Understand the project | [项目导读](项目导读.md), [implemented architecture diagram](dev/design/diagrams/README.md) |
| Run and use the product | [使用手册](使用手册.md), [Linux Bundle guide](deploy/release/BUNDLE-README.md) |
| Find verified capabilities and pending work | [Capability status](dev/status/capability-status.md) |
| Understand the target architecture | [Business design](<docs/GoPulse 高并发架构设计.md>), [observability design](<docs/GoPulse 可观测架构设计.md>) |
| Find contracts, operations, and validation | [Development documentation](dev/README.md) |
| Find allocated work and historical records | [Phase index](dev/phases/README.md), [implementation logs](dev/logs/README.md) |
| Browse all documentation | [文档入口](docs/README.md) |

## System overview

The business flow uses MySQL as its fact source, Redis as a cache, RabbitMQ for
asynchronous work, and Elasticsearch as a rebuildable search projection:

```text
Frontend -> Backend -> MySQL / Redis / business Elasticsearch
                  -> transactional Outbox -> RabbitMQ -> Worker / Indexer
```

The observability pipeline keeps collection, transport, transformation, and
storage responsibilities separate:

```text
Metrics / Logs / Events -> Monitor -> Router -> Kafka -> Marshaller
                                                      -> VictoriaMetrics
                                                      -> observability Elasticsearch
```

The user and administrator frontends share the same-origin edge and identity
system. Business requests are served by the two `business` Backend replicas;
observability, alert and plugin-control requests use the singleton `platform-api`
role through the same edge. Both roles use the same Backend image and shared
session/MySQL contract. Browsers use Backend APIs to query observability data
and manage plugins.
The [module diagram](dev/design/diagrams/README.md) describes the implemented
system; the two target designs define its intended direction.

## Development quick start

Use Linux `amd64`, Bash, Git, Docker Engine with Compose v2, Go, Node.js/npm, and
Chromium for browser checks. WSL2 is supported; keep the checkout in the Linux
filesystem. The documented local baseline requires at least 2 CPUs, 6 GiB RAM,
and 5 GiB available disk space.

After installing the toolchains declared by the module and package files, the
first frontend/browser setup is:

```bash
npm --prefix frontend ci
npm --prefix admin-frontend ci
npm --prefix frontend exec -- playwright install chromium
cp .env.example .env       # review and keep local credentials private
```

Daily source development runs Go/Vite on the host while Docker supplies only
owned persistent dependencies:

```bash
make deps                                # only the owned persistent dependencies
make dev
make dev-observe                         # add Router/Marshaller/Monitor/admin Vite
make test MODULE=backend
make integration SCOPE=business
make integration SCOPE=observe
make e2e SCOPE=business
make e2e SCOPE=observe
make stop
```

`business` and `observe` use isolated test projects, ports, volumes, and locks;
they never reuse the development data. `make deps`, `make dev`, `make dev-observe`,
and `make stop` run the native helper in `devtools`, which owns the workspace
identity, the private state and environment files, the source processes, and the
Compose projects. It stops a lifecycle only while the recorded process identity
still matches, and it never deletes named volumes. The observe entries prepare or
reuse the content-addressed Monitor image on demand, on Linux only. The native
entries fail on
unknown modules/scopes, unowned port conflicts, missing dependencies, and
child-process failures.

The native container, acceptance, and release paths remain explicit:

| Capability | Existing entry |
| --- | --- |
| Full container development/verification | `make dev`, `make dev-observe`, `make stack-verify`, `make stop`, `make verify-compose` |
| Plugin lifecycle and exporter contracts | `make verify-plugins`, `make verify-observe SCOPE=exporter|monitor` |
| Alert evaluation and management state | `make verify-alerts`, `make verify-roles`, `make verify-pages` |
| Recovery and persistence | `make verify-lifecycle INSTALL=clean|reuse`, `dev/operations/backup-restore.md` |
| Capacity and long-window experiments | `dev/validation/Phase-19/`, `dev/validation/Phase-20/` (the Phase 18–20 formal executors are retired; see [capability status](dev/status/capability-status.md)) |
| Bundle/release and evidence | `make package` ([gopulse-package](lifecycle/cmd/gopulse-package/main.go)), `.github/workflows/release-candidate.yml` |

The default native user edge is [http://127.0.0.1:15173](http://127.0.0.1:15173)
for test browser runs; development uses the configured `FRONTEND_PORT` (5173 by
default). See the [product manual](使用手册.md) for account setup and
troubleshooting. The [local validation map](dev/validation/local-development-tests.md)
records which native checks replace ordinary assertions and which specialized
container, plugin, recovery, capacity, artifact, and evidence checks remain.

## Product installation and recovery

Install the versioned Linux `amd64` Bundle using its immutable lifecycle tool and
[Bundle guide](deploy/release/BUNDLE-README.md). The guide covers installation,
verification, status/logs, shutdown, and restart without a source checkout.

For maintenance-window backup and same-Bundle empty-project restore, follow the
[backup and restore procedure](dev/operations/backup-restore.md).
Artifact builders use the [release documentation](deploy/release/README.md).
Supported environments and verified recovery boundaries are recorded in
[capability status](dev/status/capability-status.md).

## Development entry points

| Area | Entry |
| --- | --- |
| Social and management APIs, Worker, Indexer | [Backend](backend/README.md), [API registration](backend/internal/http/api.go) |
| User frontend | [frontend/](frontend/), [product manual](使用手册.md) |
| Administrator frontend | [admin-frontend](admin-frontend/README.md) |
| Plugin lifecycle and collection | [Monitor](monitor/README.md), [Exporters](exporters/README.md) |
| Observability transport and processing | [Router](router/README.md), [Marshaller](marshaller/README.md) |
| Runtime, message, metrics, and data contracts | [Technical contracts](dev/contracts/README.md) |
| Deployment and lifecycle | [Compose topology](deploy/compose.yaml), [lifecycle/](lifecycle/) |
| Integration and capacity methods | [Validation documentation](dev/validation/README.md) |

Focused source development uses the toolchain versions declared by the module
and package files. Typical Backend checks, from `backend/`:

```bash
go test ./...
go vet ./...
go test -race ./...
```

Frontend checks, from `frontend/` or `admin-frontend/`:

```bash
npm ci
npm test
npm run typecheck
npm run build
```

Select regression and real-system gates from the active implementation plan.
`make verify-compose` is the full-stack Compose entry; its scope is documented in the
[Compose acceptance guide](dev/validation/Phase-17/phase17-compose-matrix.md).
Integration tests require explicitly isolated dependencies and safety markers;
see the selected batch contract before running them.

## Contributing and documentation

Read [AGENTS.md](AGENTS.md) and the allocated Phase total and split implementation
plans before starting a batch. The [Phase index](dev/phases/README.md) links to
those contracts; they own batch order, target versions, branches, budgets, and
completion gates. Update the completed product version only at successful batch
completion, following the repository rules.

Navigation pages link to capability facts and implementation contracts instead
of copying current versions, execution status, or experiment results. Completed
plans, logs, reviews, and raw evidence retain their historical paths and context.
