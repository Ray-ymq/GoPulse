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
system. Browsers use Backend APIs to query observability data and manage plugins.
The [module diagram](dev/design/diagrams/README.md) describes the implemented
system; the two target designs define its intended direction.

## Development quick start

Use Linux `amd64`, Bash, Git, and Docker Engine with Docker Compose v2. WSL2 is a
supported way to provide Linux; keep its checkout in the Linux filesystem.
The documented local baseline requires at least 2 CPUs, 6 GiB RAM, and 5 GiB
available disk space. See the [product manual](使用手册.md) for setup details.

From the repository root:

```bash
scripts/dev.sh
scripts/verify.sh
```

On first startup, `dev.sh` creates `.env` from `.env.example` if absent, builds the
application images, initializes dependencies, and verifies the owned Compose
project. These commands run through Docker and require no host Go or Node
installation. The example credentials are for local development; keep `.env`
private and review its credentials before use in another environment.

The default edge address is [http://127.0.0.1:5173](http://127.0.0.1:5173).
It serves the social frontend and `/admin/`, with same-origin `/api/v1` requests.
Only the edge publishes a host port; Backend, storage, and observability services
remain on private Compose networks.

To choose another owned project or environment file:

```bash
scripts/dev.sh --project-name gopulse-demo --env-file /path/to/development.env
scripts/verify.sh --project-name gopulse-demo --env-file /path/to/development.env
```

To stop the default project:

```bash
scripts/down.sh
```

Named volumes are retained by default. See each script's `--help` for ownership
checks and explicit cleanup options. Account usage, super-administrator
initialization, and routine troubleshooting are in the [product manual](使用手册.md).

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
[scripts/verify-compose.sh](scripts/verify-compose.sh) is the full-stack Compose
entry; its scope is documented in the
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
