# 部署目录

在仓库根目录执行部署命令，通过 `-f` 指定 Compose 文件。

| 目录 | 用途 |
|---|---|
| `compose/` | 业务系统、可观测系统和整体系统的部署入口 |
| `docker/` | 自研镜像的 Dockerfile 及构建资源 |
| `dev/` | 本地开发配置、平台覆盖配置及调试和追踪覆盖配置 |
| `otel/` | 追踪采集器配置 |
| `selfmonitor/` | 自监控服务、产品覆盖配置和主机控制资源 |

## 三种部署入口

| 用途 | 文件 | 当前范围 |
|---|---|---|
| 业务系统 | `compose/compose.business.yaml` | 现有 PT6 业务部署，包含业务管理入口和初始化任务 |
| 可观测系统 | `compose/compose.observability.yaml` | 独立采集、传输和存储框架，尚未包含 Platform API、MySQL 和管理前端 |
| 整体系统 | `compose/compose.yaml` | 业务和可观测服务共同运行 |

本次目录整理保留原有服务配置。三份文件尚未全部实现规划中的部署范围。

环境变量示例统一维护在仓库根目录的 [`.env.example`](../.env.example)。实际部署时准备私有环境文件，并填写凭据、版本和端口。不要提交实际凭据。以下命令中的 `.env` 指仓库根目录的私有文件。

```bash
# 先检查配置。
docker compose --env-file .env -f deploy/compose/compose.business.yaml config --quiet

# 启动业务系统。
docker compose --env-file .env -f deploy/compose/compose.business.yaml up -d

# 启动可观测框架。
docker compose --env-file .env -f deploy/compose/compose.observability.yaml up -d

# 启动整体系统。
docker compose --project-name gopulse --env-file .env -f deploy/compose/compose.yaml up -d
```

选择对应模式执行启动命令。需要本地构建镜像时，先在根目录执行 `make build-images`。使用 `make stack-up`、`make stack-down` 和 `make stack-verify` 时，工具会指定整体系统的文件路径和项目名。

## 开发与覆盖配置

| 文件 | 使用方式 |
|---|---|
| `dev/compose.local.yaml` | 由 `make deps` 和开发命令启动本地依赖 |
| `dev/compose.local-linux.yaml` | Linux 观测开发模式的覆盖配置 |
| `dev/compose.local-darwin.yaml` | macOS 观测开发模式的覆盖配置 |
| `dev/compose.debug.yaml` | 配合整体系统配置使用，向主机开放依赖端口 |
| `dev/compose.tracing.yaml` | 配合整体系统配置使用，启用验收追踪采集 |

Compose 以第一个配置文件所在目录解析覆盖文件中的相对路径。追踪配置和 `selfmonitor/product.override.yaml` 中的挂载路径按整体系统入口 `deploy/compose/compose.yaml` 解析。

```bash
docker compose --project-name gopulse --env-file .env \
  -f deploy/compose/compose.yaml -f deploy/dev/compose.debug.yaml config --quiet
```

## 从旧目录迁移

| 旧位置 | 新位置 |
|---|---|
| `deploy/compose.yaml` | `deploy/compose/compose.yaml` |
| `deploy/compose.business.yaml` | `deploy/compose/compose.business.yaml` |
| `deploy/compose.observability.yaml` | `deploy/compose/compose.observability.yaml` |
| `deploy/compose.local*.yaml` | `deploy/dev/compose.local*.yaml` |
| `deploy/compose.debug.yaml` | `deploy/dev/compose.debug.yaml` |
| `deploy/compose.tracing.yaml` | `deploy/dev/compose.tracing.yaml` |

更新仓库外脚本中的旧路径。继续使用原有项目名和私有环境文件，才能关联原有命名数据卷。整体系统和本地开发基础配置显式保留原先默认的项目名 `deploy`；工具或命令中的 `--project-name` 仍可覆盖它。

如果私有环境文件中的 `MONITOR_PACKAGE_DIR` 使用相对路径，需要按新的基础文件目录调整，或改用绝对路径。未设置该变量时，默认路径仍指向仓库的 `.run/packages/`。

停止时使用与启动时相同的项目名、环境文件和 Compose 文件。`down` 默认保留命名数据卷；只有确认需要删除持久化数据时，才使用 `down --volumes`。目录迁移不需要删除数据卷。

运行 `make check-compose` 检查部署配置和覆盖组合。该检查不启动容器。
