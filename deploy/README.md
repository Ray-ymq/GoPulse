# Docker Compose 部署

`deploy/` 保存容器构建和部署配置。所有命令均在仓库根目录执行。

## 目录结构

```text
deploy/
├── compose/        # 业务、可观测、整体系统的部署入口
├── docker/         # Dockerfile 和镜像构建资源
├── dev/            # 本地开发、调试和追踪覆盖配置
├── otel/           # 追踪采集器配置
├── selfmonitor/    # 自监控部署资源
└── README.md
```

环境变量示例统一维护在根目录的 [`.env.example`](../.env.example)。

## 选择部署模式

| 模式 | Compose 文件 | 当前用途 |
|---|---|---|
| 业务系统 | [compose.business.yaml](compose/compose.business.yaml) | 运行现有 PT6 业务环境，包含业务管理入口 |
| 可观测系统 | [compose.observability.yaml](compose/compose.observability.yaml) | 运行独立的采集、传输和存储链路 |
| 整体系统 | [compose.yaml](compose/compose.yaml) | 同时运行业务和可观测服务 |

**可观测系统目前仍是独立框架，尚未包含 Platform API、MySQL 和管理前端，也未向主机开放端口。** 完整的独立登录、管理和查询入口仍需补齐。

### 当前启动的服务

下表按现有文件列出默认启动内容。数字表示常驻容器数量，`—` 表示不启动。

| 服务 | 镜像 | 业务系统 | 可观测系统 | 整体系统 |
|---|---|:---:|:---:|:---:|
| 业务 API | `gopulse/backend` | 2 | — | 2 |
| Platform API | `gopulse/backend` | 1 | — | 1 |
| 业务异步任务 | `gopulse/business-worker` | 2 | — | 2 |
| 搜索索引更新 | `gopulse/search-indexer` | 2 | — | 2 |
| 用户前端 | `gopulse/frontend` | 1 | — | 1 |
| 管理前端 | `gopulse/admin-frontend` | 1 | — | 1 |
| Monitor | `gopulse/monitor` | — | 1 | 1 |
| Marshaller | `gopulse/marshaller` | — | 2 | 2 |
| MySQL | `mysql` | 1 | — | 1 |
| Redis | `redis` | 1 | — | 1 |
| RabbitMQ | `rabbitmq` | 1 | — | 1 |
| 业务 Elasticsearch | `docker.elastic.co/elasticsearch/elasticsearch` | 1 | — | 1 |
| 可观测 Elasticsearch | `docker.elastic.co/elasticsearch/elasticsearch` | — | 1 | 1 |
| Kafka | `apache/kafka` | — | 1 | 1 |
| VictoriaMetrics | `victoriametrics/victoria-metrics` | — | 1 | 1 |

镜像版本和覆盖变量以对应 Compose 文件为准。整体系统中的两个 Elasticsearch 实例使用独立数据卷。Exporter 由 Monitor 作为二进制插件启动，不单独运行容器。

### 初始化和按需任务

| 任务 | 业务系统 | 可观测系统 | 整体系统 | 执行方式 |
|---|:---:|:---:|:---:|---|
| 数据库迁移 `migrate` | 执行 | — | 执行 | 随依赖链启动，完成后退出 |
| 搜索初始化 `search-init` | 执行 | — | 执行 | 随依赖链启动，完成后退出 |
| Topic 初始化 `kafka-init` | — | 执行 | 执行 | 随依赖链启动，完成后退出 |
| 管理员初始化 `admin-role` | 可用 | — | 可用 | 按需执行，使用 `operations` profile |
| 压测容器 `k6` | 可用 | — | — | 按需执行，使用 `load` profile |

正式压测使用根目录的 `make pt`，准备步骤和执行条件见[压测操作说明](../tools/perflab/flow/README.md)。

## 准备配置和镜像

1. 安装 Docker 和 Docker Compose。使用本地构建工具时，还需满足根目录开发工具的要求，见[贡献说明](../CONTRIBUTING.md)。
2. 根据根目录 `.env.example` 准备私有环境文件 `.env`，填写实际凭据、版本和端口。不要覆盖已有配置，不要提交实际凭据。
3. 使用自研镜像前，执行 `make build-images` 构建本地镜像，或预先准备与配置匹配的镜像。构建入口以根目录 `VERSION` 为产品版本来源；部署时应核对 `.env` 中的版本和镜像配置。
4. 运行 `make check-compose`，检查仓库示例配置及覆盖组合。该命令不启动容器；实际部署前仍需校验自己的 `.env`。

## 启动、查看和停止

先选择一份 Compose 文件，在当前终端设置路径。下面三行只执行对应模式的一行：

```bash
gopulse_compose_file=deploy/compose/compose.business.yaml
gopulse_compose_file=deploy/compose/compose.observability.yaml
gopulse_compose_file=deploy/compose/compose.yaml
```

新部署可以直接使用文件中的默认项目名。接管已有部署时，必须继续使用原有项目名，例如先执行 `export COMPOSE_PROJECT_NAME=gopulse`。项目名决定命名数据卷的归属。

| Compose 文件 | 默认项目名 |
|---|---|
| `compose.business.yaml` | `gopulse-pt6-business` |
| `compose.observability.yaml` | `gopulse-observability` |
| `compose.yaml` | `deploy` |

按以下顺序校验和启动：

```bash
# 校验私有配置。
docker compose --env-file .env -f "$gopulse_compose_file" config --quiet

# 启动服务和所需初始化任务。
docker compose --env-file .env -f "$gopulse_compose_file" up -d

# 查看容器状态，包括已退出的初始化任务。
docker compose --env-file .env -f "$gopulse_compose_file" ps -a

# 查看最近的日志。
docker compose --env-file .env -f "$gopulse_compose_file" logs --tail=100
```

业务系统和整体系统通过用户前端统一提供访问入口。使用示例端口配置时，用户页面为 `http://127.0.0.1:5173/`，管理页面为 `http://127.0.0.1:5173/admin/`。实际地址由 `PUBLISHED_HOST` 和 `FRONTEND_PORT` 决定；业务系统未启动观测依赖，观测查询不可用。

停止时，使用与启动时相同的项目名、环境文件和 Compose 文件：

```bash
docker compose --env-file .env -f "$gopulse_compose_file" down
```

`down` 默认保留命名数据卷。只有确认需要删除持久化数据时，才使用 `down --volumes`。切换部署模式不会自动迁移数据；在同一主机同时启动业务系统和整体系统时，需要分别设置项目名和前端端口。

### 整体系统的 Make 命令

| 命令 | 行为 |
|---|---|
| `make stack-up` | 构建镜像，启动整体系统并验证 |
| `make stack-verify` | 验证整体系统的服务状态和访问入口 |
| `make stack-down` | 停止整体系统，保留命名数据卷 |

这些命令使用 `deploy/compose/compose.yaml`，默认项目名为 `gopulse`，可通过 `COMPOSE_PROJECT_NAME` 覆盖。若先使用上述 Docker Compose 命令启动整体系统，后续改用 Make 命令时必须保持项目名一致。

## 开发、调试和自监控

| 配置 | 用途 | 使用入口 |
|---|---|---|
| `dev/compose.local.yaml` | 本地开发依赖 | `make deps`、`make dev` |
| `dev/compose.local-linux.yaml` | Linux 观测开发覆盖配置 | `make dev-observe` 按平台选择 |
| `dev/compose.local-darwin.yaml` | macOS 观测开发覆盖配置 | `make dev-observe` 按平台选择 |
| `dev/compose.debug.yaml` | 向主机开放依赖端口 | 与整体系统配置组合使用 |
| `dev/compose.tracing.yaml` | 启用验收追踪采集 | 与整体系统配置组合使用 |
| `selfmonitor/` | 自监控服务和产品覆盖配置 | 见[自监控说明](selfmonitor/README.md) |

覆盖文件不能独立启动。以调试配置为例：

```bash
docker compose --project-name gopulse --env-file .env \
  -f deploy/compose/compose.yaml \
  -f deploy/dev/compose.debug.yaml config --quiet
```

Compose 默认以第一个配置文件所在目录解析相对路径。追踪配置和 `selfmonitor/product.override.yaml` 的挂载路径按 `deploy/compose/compose.yaml` 所在目录编写。

## 旧路径迁移

| 旧位置 | 新位置 |
|---|---|
| `deploy/compose.yaml` | `deploy/compose/compose.yaml` |
| `deploy/compose.business.yaml` | `deploy/compose/compose.business.yaml` |
| `deploy/compose.observability.yaml` | `deploy/compose/compose.observability.yaml` |
| `deploy/compose.local*.yaml` | `deploy/dev/compose.local*.yaml` |
| `deploy/compose.debug.yaml` | `deploy/dev/compose.debug.yaml` |
| `deploy/compose.tracing.yaml` | `deploy/dev/compose.tracing.yaml` |

仓库内工具已更新路径。仓库外脚本需要同步修改，并继续使用原有项目名和私有环境文件。整体系统和本地开发基础配置保留了原先默认的项目名 `deploy`。

如果私有配置中的 `MONITOR_PACKAGE_DIR` 使用相对路径，需要按新的基础文件目录调整，或改用绝对路径。未设置该变量时，默认路径仍指向仓库的 `.run/packages/`。目录迁移不需要删除数据卷。
