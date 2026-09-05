# Phase 12：Docker 化总实施方案

> 当前状态：待实施。本方案于 2026-09-06 以最新 `upstream/main` 提交 `26d4355` 与产品版本 `1.8.4` 为规划基线。Phase 12 使用 `1.9.x` 版本线，共拆分为 3 个执行批次。

## 1. 实施目标

在不改变 Phase 11 业务、身份、可观测数据和插件管理契约的前提下，将 GoPulse 从“基础设施运行在 Compose、自研进程运行在 WSL 宿主”收敛为完整的容器运行基线：

```text
宿主浏览器 / API 客户端
  ├─ 127.0.0.1:${FRONTEND_PORT} → Frontend 静态站点与反向代理
  └─ 127.0.0.1:${HTTP_PORT}     → Backend 公共 HTTP API

Frontend → Backend
Backend  → MySQL / Redis / RabbitMQ / Elasticsearch / Monitor / VictoriaMetrics
Business Worker → MySQL / RabbitMQ / Monitor
Search Indexer  → MySQL / RabbitMQ / Elasticsearch / Monitor
Monitor + 受管 Redis Exporter → Redis / Message Router
Message Router → Kafka → Marshaller → VictoriaMetrics / Elasticsearch

一次性作业：MySQL migration / search initialize / Kafka topic initialize
长运行服务：全部由 Docker Compose 创建、检查、停止和清理
```

阶段完成必须同时证明：

- 新的 WSL2 Linux filesystem 工作区不需要安装 Go、Node.js、npm、MySQL、Redis、RabbitMQ、Kafka、VictoriaMetrics 或 Elasticsearch，仅依赖 Docker Engine 与 Docker Compose v2 即可构建、启动、验证和停止完整 GoPulse。
- Frontend、Backend、Business Worker、Search Indexer、Monitor、Message Router、Marshaller 和 Redis Exporter 均有可版本化、可重复构建的 OCI 镜像；迁移、搜索初始化等一次性命令使用同源工具镜像执行。
- 完整 Compose 使用稳定服务名解析、明确的启动依赖和必要持久卷，不使用宿主固定 IP、`host.docker.internal`、固定容器 IP 或人工启动的本机进程。
- 普通用户社交闭环、管理员四类可观测能力、Exporter 管理、必要数据重启持久化和纯可观测故障下的社交回归均在容器环境中成立。
- 默认仅 Frontend 与 Backend 以 loopback 宿主端口作为用户访问面；数据库、消息系统和可观测内部 HTTP 面只在受控 Compose 网络中可达。

只交付 Dockerfile、只让镜像构建成功、只容器化部分进程、依赖宿主 Go/Node 启动应用、默认暴露内部端口，或使用 mock 替代完整容器主闭环，均不构成 Phase 12 完成。

## 2. 当前真实基线与规划输入

本方案编写前已 fetch 主远程。规划基线具备：

- `deploy/compose.yaml` 目前只编排 MySQL 8.4.0、Redis 7.2.5、RabbitMQ 3.13.3、Kafka 4.3.1、VictoriaMetrics 1.151.0 和 Elasticsearch 9.5.2，另有 `kafka-init` 一次性任务；七个基础设施端口以 loopback 发布到宿主。
- `scripts/dev.sh` 在宿主运行 migration 和 search reindex，再本地构建并启动 Backend、Business Worker、Search Indexer、Router、Marshaller、Monitor 与 Vite；因此日常运行仍依赖 Go、Node.js、npm、Python、curl 及宿主 PID/port 管理。
- Backend 同一 Go module 内包含 `server`、`business-worker`、`search-indexer`、`migrate`、`search-reindex` 和 `admin-role` 命令。Phase 12 早期提纲未单列 Search Indexer 和一次性命令，但它们已是完整业务、搜索和验收链路的必需运行单元，本方案将其纳入容器基线而不视为新业务范围。
- Router、Marshaller、Monitor 和 Redis Exporter 是四个独立 Go module。Monitor 通过 Plugin Manager 校验包、持久化 desired/observed state，并作为唯一运行时所有者启停 Redis Exporter 子进程。
- Frontend 是 Vue/Vite 应用，开发态依赖 Vite proxy 访问 Backend；尚无生产静态资源服务、SPA fallback、容器健康端点和容器内 Backend 反向代理配置。
- 容器服务名不能直接替换所有 loopback：Backend `LOG_MONITOR_URL`、Monitor 监听地址、Marshaller 监听地址及 VM/Elasticsearch URL 当前有 loopback 安全约束。需要新增显式容器运行模式，默认主机模式的 loopback 保护不得被宽泛削弱。
- Backend 的 `MONITOR_URL`、VictoriaMetrics 和 Elasticsearch client 已支持受限 HTTP(S) URL，MySQL/Redis/Kafka/RabbitMQ 已支持主机名或标准连接 URL；Router 可以绑定容器通配地址。
- `scripts/verify.sh` 与各隔离验收脚本假定自研组件为宿主 PID、基础设施有宿主 loopback 端口；Phase 12 需增加容器原生验收入口，并保留现有定向回归作为实施期最低有效证据，不能把宿主模式通过冒充为容器验收。
- Phase 11 已交付同一 Cookie 身份、Backend 实时 admin 授权、Metrics/Logs/Events 查询、Exporter 管理和四区总览。Frontend 只访问 Backend，VM/Monitor 不进入 Backend readiness；Elasticsearch 因同时承担帖子搜索，仍是既有 `/ready` 依赖。
- MySQL、Redis、RabbitMQ、Kafka、VictoriaMetrics 和 Elasticsearch 已有命名卷；Monitor 插件根目录目前位于宿主 `.run`，容器化后必须转为独立持久卷并验证重启恢复。

Phase-12-01 开工前必须重新 fetch 最新主远程并核对上述进程、配置、端口、卷、健康契约和 Phase 11 交接。如公共契约或前置批次已变化，先更新本总方案及尚未开始的拆分方案。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 11 最终版本 `1.8.4` 位于最新主远程 `main`，其实施记录、远程门禁与双使用态浏览器证据可核对。
- 每批实施、应用测试和集成验收在 Windows 宿主的 WSL2 Linux filesystem 与唯一 Docker daemon 中执行；Bash 是唯一维护的本地生命周期和验收入口。
- 每批开始前保存 Git、Compose project/container/network/volume/image、发布端口、数据库、Kafka group/offset、ES index/alias/PIT、VM 查询窗口、Monitor 插件卷和浏览器制品快照。
- 实施期只读取直接涉及的镜像、配置、生命周期、健康接口、验收和公共契约，不扩展为全仓 Review、依赖审计、覆盖率活动或生产镜像供应链工程。

### 3.2 权威批次、版本与开发分支

Phase 12 使用 `1.9.x` 版本线，`1.9.0` 只作为阶段基线，不创建空批次。下表是本阶段执行顺序、目标版本与开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-12-01 | `1.9.1` | `develop/1.9.1` | 待实施 |
| Phase-12-02 | `1.9.2` | `develop/1.9.2` | 待实施 |
| Phase-12-03 | `1.9.3` | `develop/1.9.3` | 待实施 |

执行规则：

- 每批从包含全部前置批次的最新 `upstream/main` 创建独立分支，不在 `update`、Phase 11 分支或已完成的 Phase 12 分支继续实施。
- 同一批次全部提交共享目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`，镜像 label/tag 使用同一版本。
- 每批完成前创建与拆分方案同名的 `dev/logs/Phase-12/Phase-12-XX-*.md`，只记录实际改动、验证、偏差、失败与限制。
- Phase-12-01 交付可由容器构建、初始化和运行的社交业务及搜索闭环，并建立后续可观测容器共用的镜像、网络和作业基线。
- Phase-12-02 在已合入的业务容器闭环上交付完整可观测容器链路、Monitor 受管 Exporter 生命周期、内部网络和管理员浏览器闭环。
- Phase-12-03 只对前两批能力执行冷启动、跨批集成、重启持久化、故障隔离、资源清理和 Phase 12 收口；除真实复现的阻断问题外不新增功能。
- 已推送分支不得静默改名或重新编号。实施前批次拆分或顺序变化时，先修改本表并重算尚未创建的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 为 Frontend、Backend、Business Worker、Search Indexer、Monitor、Message Router、Marshaller 和 Redis Exporter 建立固定版本、多阶段、非 root 的 OCI 镜像构建方式。
- 将 migration、search initialize/reindex 和必要验收 CLI 作为一次性 Compose 作业运行，保持同源代码和同版本镜像，不在镜像启动时重新编译。
- 建立完整默认 Compose 拓扑，包含全部自研长运行单元、六个官方基础设施、Kafka Topic 初始化、MySQL 迁移、搜索初始化、健康检查、启动顺序、有界停止与必要持久卷。
- 建立默认 host 和显式 container 两种运行模式：只有 container 模式才允许容器通配监听与符合规则的服务 DNS HTTP origin；默认与既有 host 模式继续要求 loopback。
- 以 Compose service name 和容器端口建立内部连接，保留独立 Bearer/Basic 服务身份，不在 Frontend 镜像、浏览器 bundle、URL、镜像 layer 或 build arg 中携带凭据。
- Frontend 镜像服务编译后静态资源，支持 SPA fallback，并将 `/api/v1`、`/health` 和 `/ready` 代理到 Backend；不使用 Vite dev server 作为阶段运行结果。
- 用 Compose 命名卷保护 MySQL、Redis、RabbitMQ、Kafka、VictoriaMetrics、Elasticsearch 和 Monitor plugin root；验证服务重启与整个 project 停止/再启动后的必要数据。
- 将日常 Bash 生命周期和完整验收收敛到 Docker/Compose 调度，宿主不再启动、记录或杀死自研 PID；定向 Go/Frontend 测试可继续在实施和 CI 环境使用对应工具链。
- 新增镜像合同、Compose 静态配置、容器网络/端口、无宿主运行时冷启动、完整浏览器闭环和资源强归属验收，并接入 Linux 质量门禁。

### 4.2 明确不做

- Kubernetes Deployment/Service/StatefulSet/PV/Secret/Probe、Helm/Kustomize、节点标签和调度约束；它们属于 Phase 13。
- Ingress、公网域名、外部 TLS、证书签发、统一集群入口或生产跨域策略；它们属于 Phase 14 及以后。
- 多副本、自动伸缩、生产高可用、备份恢复体系、长时容量/压力测试或跨主机 Compose。
- 注册表发布、镜像签名、SBOM/来源证明、CVE 政策、自动多架构发布、生产级基础镜像更新机制或供应链平台。
- Docker Swarm、Docker-in-Docker，不向 Monitor 挂载 Docker socket，不为了启停 Exporter 赋予容器宿主级控制权。
- 重构社交 API、消息 Envelope、Logs/Events/Metrics DTO、搜索语义、用户角色或 Exporter 业务状态机；容器化仅修改运行、配置和验收边界。
- 对内部数据服务默认发布宿主端口。临时调试端口只能由明确的 loopback override 启用，不是日常启动或阶段验收前提。
- 将日常容器化结果改成热重载开发环境，或绑定挂载源码、`node_modules`、Go build cache 作为应用运行条件。
- 修改冻结的 `scripts/*.ps1`、新增原生 Windows 验收或 Windows runner。

## 5. 镜像与运行制品契约

### 5.1 自研镜像矩阵

| 镜像 | 源码/构建结果 | 默认进程 | 运行状态 | 对外契约 |
| --- | --- | --- | --- | --- |
| `gopulse/frontend` | `frontend` lockfile 安装、typecheck 与 Vite build；最终层只含 dist 和 Web server 配置 | 非 root 静态 Web server | 无持久状态 | Frontend HTTP、SPA fallback、Backend 代理与 liveness |
| `gopulse/backend` | Backend module 的 `server`，并包含同版本 `migrate`、`search-reindex`、`admin-role` 工具 | Backend server + Outbox Dispatcher | 无本地持久状态 | `/health`、`/ready`、`/api/v1` |
| `gopulse/business-worker` | Backend module 的 `business-worker` | Business Worker | 状态位于 MySQL/RabbitMQ | 无公共端口，由进程存活/行为验证 |
| `gopulse/search-indexer` | Backend module 的 `search-indexer` | Search Indexer | 状态位于 RabbitMQ/Elasticsearch | 无公共端口，由进程存活/搜索收敛验证 |
| `gopulse/router` | Router module 的 `router` | Message Router | 无本地持久状态 | 内部 `/health`、Bearer `/ready` 和 ingest API |
| `gopulse/marshaller` | Marshaller module 的 `marshaller` | Marshaller | Kafka offset/VM/ES 是权威状态 | 内部 `/health` 与 Bearer `/ready` |
| `gopulse/monitor` | Monitor module + 由同一提交构建的受管 Redis Exporter package | Monitor 与由 Plugin Manager 持有的 Exporter 子进程 | `monitor_plugin_data` 保存 registry/releases/runtime 所需事实 | 内部 `/health`、Bearer `/ready`、log ingest 和 plugin API |
| `gopulse/redis-exporter` | Redis Exporter module 的 `redis-exporter` | 独立 Exporter | 无持久状态 | 独立镜像验收使用 `/health` 和 `/metrics` |

`migrate`、`search-reindex --if-missing` 和 `admin-role` 不创建与 Backend 源码重复的常驻镜像，由 `gopulse/backend` 以显式 command 作为一次性 Compose 作业运行。Business Worker 和 Search Indexer 必须拥有独立最终镜像/标签，以作为 Phase 13 独立工作负载的直接输入。

### 5.2 Redis Exporter 容器语义

- 默认完整 Compose 不能同时启动一个静态 Exporter service 和 Monitor 受管 Exporter，否则会出现双运行时所有者、端口冲突、指标重复与 admin stop/update 失真。
- 完整系统中，Monitor 容器保留 Phase 6～11 契约：从镜像内的确定性 package 安装/更新，在同一容器网络命名空间的 loopback 上启停一个 Exporter 子进程，并将插件事实持久化到独立卷。这是不赋予 Docker socket/特权的容器化运行边界。
- `gopulse/redis-exporter` 独立镜像仍必须构建并在隔离 Compose project/profile 中验证真实 Redis 目标、`up 0`、恢复与信号关闭，作为自研 Exporter 标准镜像和 Phase 13 可选运行单元。
- Monitor 的首次启动 bootstrap 必须是幂等的：无安装时安装镜像内 package，同版本已安装时不重复变更，高版本持久状态不被旧镜像静默降级，失败时保留可恢复的旧事实并使容器明确失败或降级，不伪造 running。

### 5.3 通用构建契约

- builder 和 runtime base image 使用明确版本，不使用浮动 `latest`；实施时记录最终选定的镜像和变更理由。
- Go 服务采用 multi-stage build，最终层不包含 Go toolchain、Git、源码与 build cache；Frontend 最终层不包含 Node.js、npm、`node_modules` 或 sourcemap（除非有明确运行需求并记录）。
- 使用 lockfile/`go.sum` 和带 cache mount 的确定性构建；禁止在 build arg、Dockerfile `ENV`、复制文件、构建日志或 image history 中写入运行凭据。
- 每个最终镜像设置非 root 数字 UID/GID、明确 `WORKDIR`、exec-form `ENTRYPOINT/CMD`、UTC 时间和适合进程的停止信号；只对确需的 Monitor plugin volume 保留写权限。
- 镜像至少写入 OCI `version`、`revision` 和 `source` label，且 `version` 与当前批次根 `VERSION` 一致；Compose 使用明确本地 tag，不隐式拉取同名远程镜像。
- 根 `.dockerignore` 和必要的子上下文排除 `.git`、`.env`、`.run`、本地制品、测试输出、`node_modules` 和无关文档，同时不排除构建必需的 lockfile、源码和 VERSION。
- 本阶段完成当前 WSL/Docker 架构的构建和运行验收。为 `TARGETOS/TARGETARCH` 和 Exporter manifest 架构一致性预留标准参数，但不把全部多架构发布列为门禁。

## 6. Compose 拓扑、网络与端口

### 6.1 服务与作业类型

| 类型 | Compose service | 启动/完成条件 |
| --- | --- | --- |
| 公共访问 | `frontend`、`backend` | 镜像已构建，前置初始化成功，进程 liveness 通过 |
| 业务后台 | `business-worker`、`search-indexer` | migration 成功，相关依赖可用；之后依赖短断由已有恢复逻辑处理 |
| 可观测自研 | `monitor`、`router`、`marshaller` | 内部 liveness 通过；readiness 依赖状态由验收单独核对 |
| 业务基础设施 | `mysql`、`redis`、`rabbitmq` | 官方镜像 healthcheck 通过 |
| 共享搜索/可观测设施 | `elasticsearch` | 集群 yellow/green 健康；既有 Backend search/readiness 语义保持 |
| 可观测基础设施 | `kafka`、`victoriametrics` | 官方镜像的有界 healthcheck 通过 |
| 初始化作业 | `kafka-init`、`migrate`、`search-init` | 退出码 0 才允许依赖者继续；可重跑、不执行破坏性回滚 |
| 按需运维/验收 | `admin-role`、`acceptance` 或等价 one-shot profile | 不在默认启动路径常驻，需显式命令且不发布端口 |

Compose 不声明 `container_name`，所有容器、网络和卷依靠 project label 归属，使日常环境、隔离验收与 CI 可以并存。`depends_on` 只用于冷启动顺序，不把它误作运行期恢复机制。

### 6.2 网络分区

| 网络 | 成员 | 边界 |
| --- | --- | --- |
| `edge` | Frontend、Backend | Frontend 只能经服务名代理 Backend；不直连数据库或可观测组件 |
| `business` (`internal: true`) | Backend、Business Worker、Search Indexer、migration/search 作业、MySQL、Redis、RabbitMQ、Elasticsearch，以及为采集 Redis 加入的 Monitor | 业务与搜索内网；不面向宿主浏览器 |
| `observability` (`internal: true`) | Backend、Business Worker、Search Indexer、Monitor、Router、Kafka、Marshaller、VictoriaMetrics、Elasticsearch | 日志/指标/事件运行面；只接受服务身份或内部协议 |

Backend 和 Monitor 是因业务职责需要跨区的明确连接点，不将任意容器同时加入所有网络。Elasticsearch 同时为帖子搜索与 Logs/Events 存储，所以明确加入两个内网；这不改变 index/alias 的逻辑隔离。

### 6.3 默认端口暴露矩阵

| 服务 | 容器端口 | 默认宿主发布 | 访问者 |
| --- | ---: | --- | --- |
| Frontend | `8080` 或实施时固定的非特权端口 | `127.0.0.1:${FRONTEND_PORT}` | 浏览器；通过同源路由访问 Backend |
| Backend | `8080` | `127.0.0.1:${HTTP_PORT}` | 公共 API 客户端与验收；认证/授权不改 |
| MySQL / Redis / RabbitMQ / Elasticsearch | 原生端口 | 无 | 相应内网应用 |
| Kafka / VictoriaMetrics | `19092` / `8428` | 无 | Router/Marshaller/Backend 等内部应用 |
| Monitor / Router / Marshaller | `9090` / `9091` / `9093` | 无 | 带独立服务身份的内部调用者 |
| Monitor 受管 Redis Exporter | `9121` loopback | 无，不向 Compose 网络广播 | Monitor 容器内部 |

如实施调试需要临时访问基础设施，使用单独 `deploy/compose.debug.yaml` 或等价明确 override，只发布 `127.0.0.1` 随机/配置端口，不由 `dev.sh` 或阶段主验收默认加载。验收结束必须清理该 override 产生的强归属资源。

## 7. 容器配置与服务身份

### 7.1 显式运行模式

- 引入一个全模块一致、枚举值的运行模式配置（规划名 `GOPULSE_RUNTIME_MODE=host|container`，如实施时已有等价契约可沿用）。未设置默认 `host`，未知值启动失败。
- `host` 模式保留 Backend log shipper、Monitor、Marshaller 的 loopback 监听/下游 URL 限制，现有定向测试与安全默认不退化。
- `container` 模式仅允许监听配置使用 IP 通配地址，并允许下游 HTTP origin/broker 使用经过现有长度、scheme、host、port、userinfo/query/fragment 和控制字符校验的 DNS 主机名。它不允许相对 URL、任意 path、凭据嵌入 URL 或空 host。
- 容器放宽与 Compose 默认不发布内部端口、`internal` 网络和 Bearer/Basic 身份一起验收；不把“在容器中”当作跳过身份校验的理由。

### 7.2 内部地址矩阵

| 调用者 | 配置 | 容器值 |
| --- | --- | --- |
| Backend / Worker / Indexer / migration | MySQL | `mysql:3306` |
| Backend / Monitor 受管 Exporter | Redis | `redis:6379` |
| Backend / Worker / Indexer | RabbitMQ | `amqp://<identity>@rabbitmq:5672/` |
| Backend / Indexer / Marshaller | Elasticsearch | `http://elasticsearch:9200` |
| Backend / Worker / Indexer / reindex | log Monitor | `http://monitor:9090` + 独立 ingest token |
| Backend | plugin Monitor | `http://monitor:9090` + admin-to-service token |
| Backend | VictoriaMetrics query | `http://victoriametrics:8428` + Basic identity |
| Monitor | Message Router | `http://router:9091` + Router token |
| Router / Marshaller | Kafka | `kafka:19092` + 固定 Topic/group |
| Marshaller | VictoriaMetrics write | `http://victoriametrics:8428` + Basic identity |
| Frontend Web server | Backend upstream | `http://backend:8080` |

- `.env.example` 继续只包含明确的本地开发凭据示例；`.env` 不进入构建上下文或 Git。Compose 配置对必需值使用 required interpolation，缺失时在创建容器前失败。
- 当前单节点 VictoriaMetrics 的 Backend query 和 Marshaller write 仍共用内部 Basic 身份，是已知 MVP 限制；不因容器化将该身份注入 Frontend 或发布 VM 端口。
- 运行时凭据可通过 Compose environment 注入容器，且必须从日志、错误、healthcheck 输出和浏览器响应中排除。Docker secrets/外部密钥管理属于后续生产化，不把本地 `.env` 描述为生产安全存储。

## 8. Frontend 容器与用户访问边界

- Frontend builder 必须使用 `npm ci`、当前 lockfile、typecheck 和 production build；构建错误阻止镜像产生。
- 静态 Web server 对已存在资源返回正确 MIME/cache header，未命中的前端 GET 路由 fallback 到 `index.html`；不将未知 API 路径错误地返回 HTML 200。
- `/api/v1`、`/health`、`/ready` 只反向代理到 `backend:8080`，保留 method/body/header/status，设置有界 timeout 与 body limit，不代理 Monitor/Router/Marshaller/VM/ES 的任何路径。
- 同源 Cookie、`401 authentication_required`、`403 permission_denied`、管理路由守卫和运行中降权契约保持；不为容器端口新增宽泛 CORS。
- Frontend image/bundle 扫描不得出现 Monitor/Router/Marshaller/Kafka/VM/ES 地址、Topic/group、alias/index、Basic/Bearer token、绝对服务器路径或本地 `.env` 内容。
- Frontend healthcheck 检查静态 Web server 存活，Backend healthcheck 检查 `/health` 而非把所有可观测依赖变成容器重启条件。完整 readiness 在 `verify` 中核对并准确报告依赖状态。

## 9. 启动、停止与持久化

### 9.1 冷启动顺序

1. Bash 入口验证工作区、Docker/Compose 版本、分支/版本、`.env` 必需值、project name 和宿主 loopback 端口，不要求 Go/Node/npm/Python/curl。
2. 构建当前版本全部自研镜像，对同一 commit 传入相同 version/revision label，任一镜像失败即停止。
3. 启动 MySQL、Redis、RabbitMQ、Kafka、VictoriaMetrics 与 Elasticsearch，等待官方 healthcheck；不用无限 sleep 或主机固定时间代替健康条件。
4. 执行 `kafka-init` 创建/核对固定 Topic，执行 `migrate up`，执行 `search-reindex --if-missing`；一次性作业失败必须保留可诊断日志并阻止相关长运行服务开始。
5. 启动 Router、Marshaller、Monitor/Exporter、Backend、Worker、Indexer 与 Frontend。Monitor 的 package bootstrap 只在核对持久插件事实后执行幂等安装/更新。
6. 等待全部长运行容器 liveness，再由只读验证器核对 Backend/Router/Marshaller/Monitor readiness、Topic/group、VM/ES 存储契约、Exporter 状态与 Frontend HTTP。

冷启动失败不删除持久卷。重跑时 migration、Topic 初始化、search initialize 和 plugin bootstrap 必须幂等，不要求人工清库才能恢复。

### 9.2 停止和信号

- 长运行镜像让业务进程成为 PID 1 或使用适当 init，不通过会吞信号的 shell 包装。Compose `stop_grace_period` 不小于对应进程 shutdown timeout 及必要余量。
- Monitor 收到终止信号时先停止采集、排空有界事件队列并安全停止受管 Exporter；容器再启动从插件卷恢复 desired state，不复用旧 PID。
- `scripts/down.sh` 只对经验证的 Compose project 执行 `down`，默认保留命名卷与镜像。删除卷是另一个明确、强归属且需要用户主动调用的操作，不是日常 stop 的副作用。
- 不继续生成 `.run/*.json` 宿主 PID 记录；容器身份由 project label、service label、container ID 和预期 image label 联合证明。

### 9.3 持久化矩阵

| 卷 | 保存内容 | 重启验收 |
| --- | --- | --- |
| `mysql_data` | 用户、角色、帖子、评论、点赞、Outbox、通知、migration 版本 | 业务事实与 admin 角色保留 |
| `redis_data` | 缓存/AOF | 容器重启不破坏权威 MySQL 事实，缓存可重建 |
| `rabbitmq_data` | 业务 queue/message 状态 | 未处理事件在 Worker/Indexer 恢复后收敛 |
| `kafka_data` | 可观测 Topic 与 consumer group offset | Marshaller 恢复时从已提交 offset 继续 |
| `victoriametrics_data` | Metrics 样本 | 历史样本在 project 停止/启动后可查 |
| `elasticsearch_data` | post search、Logs 和 Events indices/aliases | 三类数据的必要查询恢复 |
| `monitor_plugin_data` | 受管 package/release/registry/desired state 与必要安全错误 | Monitor 容器替换后依契约恢复 Exporter，旧 PID 不复用 |

不将 Frontend dist、Go 二进制、应用日志文件、临时上传、PIT/cursor 或容器 PID 变成命名卷。Docker logging driver 保留 stdout/stderr，日志数据链路仍以 Monitor → Router → Kafka → Marshaller → Elasticsearch 为产品闭环。

## 10. 安全、健康与故障语义

### 10.1 身份和最小暴露

- 未登录用户、普通用户和管理员的 Backend 授权结果不因容器内网可达而变化；普通用户对 Metrics/Logs/Events/Exporter API 仍为 `403 permission_denied`，且内部 client 调用为零。
- Frontend 容器只连接 Backend，不加入任一内部数据网络；浏览器 network 不出现 `mysql`、`redis`、`rabbitmq`、`kafka`、`monitor`、`router`、`marshaller`、`victoriametrics` 或 `elasticsearch` 地址。
- 自研容器默认非 root，不使用 privileged、host network/IPC/PID、Docker socket 或无界 bind mount；在不阻断直接契约时移除不必要 capabilities 并对无状态镜像使用只读根文件系统/受控 tmpfs。
- 健康检查不在 command line 或普通输出打印 token/password。需要身份的 readiness 由容器内验证器从运行时环境发起，错误仅返回安全摘要。

### 10.2 liveness、readiness 与启动依赖

- 有 HTTP 服务的自研容器使用进程 liveness `GET /health`；无 HTTP 服务的 Worker/Indexer 以进程存活、有界功能探针或 Compose 运行状态证明，不为 Phase 12 临时新增未规划网络 API。
- Backend 容器 healthcheck 不以 `/ready` 失败触发重启，避免数据依赖短断变成重启风暴。`verify` 独立解读 `/ready` 的 MySQL/Redis/RabbitMQ/Elasticsearch 既有契约。
- Router/Marshaller/Monitor 的 `/health` 只表示进程可服务；Bearer `/ready` 仍表达实际下游依赖。Compose 启动可等待必需的冷启动前置，运行期不因 readiness 短断反复杀死有恢复能力的进程。

### 10.3 故障矩阵

| 故障 | 必须受影响 | 必须保持 |
| --- | --- | --- |
| VictoriaMetrics 停止/重启 | Metrics 新写入和查询降级，Marshaller readiness 失败 | Backend liveness 与既有业务 readiness、Logs/Events/Exporter、非搜索社交操作 |
| Kafka/Router/Marshaller 中断 | 新可观测消息传输/存储延迟，相关 readiness 失败 | MySQL/RabbitMQ 业务事实与社交闭环，恢复语义按现有 best-effort/offset 契约 |
| Monitor/Exporter 停止 | Exporter 管理、新 Metrics/Logs/Events 采集局部降级 | Backend liveness、普通社交 API、已存历史 VM/ES 数据查询（对应存储可用时） |
| Elasticsearch 停止 | 帖子搜索、Backend 既有 readiness、Logs/Events 存储/查询降级 | 非搜索社交 API 不因容器化新增失败；MySQL 业务事实保留 |
| RabbitMQ 停止 | 异步通知/搜索增量交付延迟，Backend readiness 按既有契约降级 | 同步 MySQL 业务事实与 Outbox 保留，恢复后收敛 |
| Redis 停止 | 缓存与 Redis Exporter `up 0` | MySQL 权威帖子读写回退，恢复不需重建应用镜像 |
| 长运行自研容器被替换 | 该进程的短暂不可用 | 外部持久事实不丢失，旧 PID/临时状态不被误认为当前事实 |

“可观测故障不阻断社交业务”在本阶段专指不把 VM、Monitor、Router、Marshaller 或 Kafka 新增到 Backend 业务 readiness/事务路径，并验证代表性非搜索社交闭环。Elasticsearch 与 RabbitMQ 的既有业务 readiness/搜索/异步语义必须准确保留和记录，不为通过 Phase 12 而伪造全部依赖无关。

## 11. Bash 生命周期、验收入口与 CI

- `scripts/dev.sh` 收敛为容器原生启动入口：验证安全参数，创建 `.env` 示例副本（仅在缺失时），构建当前版本镜像，启动完整 project 并等待有界初始化/健康；不再构建宿主二进制、运行 Vite 或写 PID 记录。
- `scripts/verify.sh` 继续为日常只读检查，但检查对象改为已运行 Compose project；需要 curl/JSON 工具的检查在一次性验证容器内执行，宿主仅调用 Docker/Compose。
- 新增 `scripts/verify-compose.sh` 或等价的完整阶段验收入口，使用随机合法 project 名、随机 loopback 用户端口、临时 env 与全新命名卷，不修改 `.env`、`.run` 或日常 project。
- 完整验收的 API/JSON/浏览器客户端在专用 one-shot acceptance image/profile 中运行，不要求宿主 Node/Go/Python/curl，也不挂载 Docker socket。需要停止/恢复服务的编排由宿主 Bash 只通过已验证 project/service/container label 执行。
- `scripts/down.sh` 可重入，对 project 名、Compose label、容器与预期工作区进行校验，默认只删除容器和网络、保留卷；失败/signal 路径只清理当前随机验收 project。
- 现有 `verify-business.sh`、`verify-exporter.sh`、`verify-monitor.sh`、`verify-router.sh`、`verify-marshaller.sh`、`verify-logs.sh`、`verify-events.sh` 和 `verify-observability-ui.sh` 继续作为实施期直接回归证据。只在相关契约变更或阶段主验收暴露具体回归时扩展，不把所有历史真实矩阵机械重跑多次。
- CI 增加所有自研镜像构建、OCI label/user/entrypoint 检查、Compose 渲染、默认端口/网络/卷/健康契约、容器自测和完整容器闭环。对资源较重的完整闭环只运行一个权威入口，不与等价下层脚本重复。

## 12. 测试与验收策略

### 12.1 最低有效测试层

- Dockerfile/Frontend Web server/Compose 配置由静态渲染、镜像构建、image inspect 和单容器 smoke 证明；不用文本 grep 代替真实镜像运行。
- 运行模式与 URL/host 放宽在各相关 Go config package 增加代表性成功与失败测试：host 模式拒绝 non-loopback，container 模式接受合法服务 DNS，未知模式/恶意 URL 仍拒绝。
- 启动作业、强归属清理和端口保护使用 shell self-test 覆盖代表性正负路径，不用真实删除日常资源验证拒绝逻辑。
- 每批在最终 diff 上运行直接受影响 module 的 format/unit/vet/race 与 Frontend test/build，再运行本批固定容器闭环一次。未变化且已成功的历史检查不重复扩展。

### 12.2 Phase 封闭端到端矩阵

1. 在无 Go/Node/npm/项目数据服务的干净 WSL 用户路径或等价容器工具限制环境，仅使用 Docker/Compose 构建全部镜像并冷启动随机 project。
2. 核对六个基础设施、七个自研长运行 Compose 服务（Frontend、Backend、Worker、Indexer、Router、Marshaller、Monitor，其中 Monitor 内含受管 Exporter）、三个初始化作业、网络、卷、image label 和健康状态；不存在宿主 Go/Node 业务进程。
3. 从真实浏览器注册普通用户与待提升管理员，通过容器化 `admin-role` 运维命令提升后，完成帖子、评论、点赞、通知与搜索收敛的代表闭环。
4. 普通用户无可观测入口、直达管理路由无内部请求、可观测/Exporter API 全为 `403`；管理员通过 Frontend/Backend 查看真实 Metrics/Logs/Events 并执行代表性 Exporter 安装/启停/更新。
5. 证明 Redis → Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics 以及 Backend/Worker/Indexer 日志、Monitor Events 到 Elasticsearch 的真实链路；不直接写 VM/ES 伪造主证据。
6. 停止并恢复 VM、Kafka/Router/Marshaller 和 Monitor 的代表性纯可观测故障，确认准确局部降级、Backend 无新增 readiness 依赖、代表性社交读写成功与恢复后的数据收敛。
7. 替换 Backend、Worker、Indexer、Monitor、Marshaller 及代表性持久数据服务容器，再对整个 project 执行保留卷的 down/up，确认业务事实、异步收敛、搜索、Metrics/Logs/Events 和插件 desired state 的必要保留。
8. 扫描宿主 published ports、Frontend network/bundle、Compose 网络成员、image history/config/user/layers 与容器 mount/capability，确认只有 Frontend/Backend loopback 入口，无凭据/内部地址/源码泄漏和高权限旁路。
9. 在正常、构建失败、初始化失败、运行验收失败和 signal 路径前后对比 project/container/network/volume/image 与日常资源；只清理当前随机验收 project，不删日常卷或用户镜像。

## 13. 批次拆分与交付关系

### 13.1 Phase-12-01：社交业务容器运行闭环

- 交付 Frontend、Backend、Business Worker、Search Indexer 及 Backend 一次性工具镜像，以及 MySQL/Redis/RabbitMQ/Elasticsearch 业务 Compose 拓扑。
- 完成非 root multi-stage build、service DNS、Frontend 生产静态服务/代理、migration/search-init 作业、业务持久卷、有界信号关闭与容器化业务验收。
- 批次结束时，不依赖宿主 Go/Node/npm 可启动社交和搜索系统，并向下批交付稳定镜像/tag/network/job/volume 契约。

详细方案：`dev/imple/Phase-12/Phase-12-01-社交业务容器运行闭环.md`。

### 13.2 Phase-12-02：可观测系统容器运行闭环

- 交付 Monitor、Message Router、Marshaller 和独立 Redis Exporter 镜像，将 Kafka、VictoriaMetrics 及共享 Elasticsearch 接入完整默认拓扑。
- 完成显式 container mode、服务 DNS、Bearer/Basic 身份、Monitor 插件卷与幂等 package bootstrap，默认收口内部端口。
- 批次结束时，完整 Compose 可完成 Metrics/Logs/Events/Exporter 管理员浏览器闭环，并在代表性可观测故障中保持社交业务边界。

详细方案：`dev/imple/Phase-12/Phase-12-02-可观测系统容器运行闭环.md`。

### 13.3 Phase-12-03：全栈 Compose 验收与阶段收口

- 从最新合入基线构建全部镜像，在无项目运行时的干净条件完成冷启动、双使用态、三条可观测链路、插件管理、重启持久化、故障隔离和资源清理的唯一权威阶段矩阵。
- 只修复该固定矩阵真实复现的阻断问题，完成 README、方案状态、实施记录、版本、CI 和 Phase 13 镜像/拓扑交接。
- 不新增容器功能、不做独立 Review，固定完成门禁通过后立即停止。

详细方案：`dev/imple/Phase-12/Phase-12-03-全栈Compose验收与阶段收口.md`。

## 14. 预计变更边界

```text
.dockerignore
.env.example
.github/workflows/quality-gates.yml
README.md
backend/README.md
frontend/README.md（若实施时建立）
monitor/README.md
router/README.md
marshaller/README.md
exporters/redis/README.md

deploy/compose.yaml
deploy/compose.debug.yaml（若采用 override）
deploy/docker/**
frontend/静态 Web server 配置

backend/internal/config/**
monitor/internal/config/**
monitor/cmd/monitor/**（只限幂等 package bootstrap/容器启停边界）
marshaller/internal/config/**
router/internal/config/**（若容器模式直接受影响）
exporters/redis/internal/config/**（若容器模式直接受影响）

scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-compose.sh（或等价容器主验收入口）
scripts/ci/**

dev/imple/Phase-12/*.md（只限实施状态/真实偏差）
dev/logs/Phase-12/*.md
VERSION
frontend/package.json
frontend/package-lock.json
```

预计边界是允许修改的上限，不要求为填充清单制造无意义文件。Dockerfile 的实际组织可采用根多目标、各 module 独立文件或等价结构，但必须保持第 5 节的镜像边界和第 6 节的运行拓扑。

## 15. 固定完成门禁

各批根据直接受影响范围执行其拆分方案中的固定命令。Phase-12-03 的最终 diff 至少执行：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd monitor && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd router && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd exporters/redis && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch "$(git branch --show-current)" --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-compose.sh --self-test
scripts/verify-compose.sh
git diff --check
```

- `scripts/verify-compose.sh` 是 Phase 12 唯一完整容器主入口，自然覆盖的业务/可观测健康路径不再调用全部历史真实脚本重复验收。
- Go race 范围按各批拆分方案的直接配置/生命周期 package 执行；只有观察到并发、共享启停或数据收敛回归时才扩展更广 race 范围，并在实施记录说明风险依据。
- 镜像必须从最终提交重建，完整闭环在强归属随机 project 与全新卷中运行；已有日常容器、本机进程或旧数据不能作为通过条件。
- 实施机缺少 Docker/Compose 或资源不足时，不得标记阶段完成；可以如实记录未执行门禁，但不用 mock 或静态渲染代替。

## 16. Phase 级验收、完成与交接

### 16.1 Phase 级验收标准

- 仅依赖 Docker Engine 与 Docker Compose v2，可从干净卷构建并启动全部自研镜像、官方基础设施、初始化作业和长运行服务，宿主无 Go/Node/npm 业务进程。
- Frontend、Backend、Business Worker、Search Indexer、Monitor、Router、Marshaller 和 Redis Exporter 镜像可重复构建，版本/revision label、非 root 用户、entrypoint、架构和运行内容符合固定契约。
- migration、Kafka Topic 和 search initialize 幂等成功；首次启动、保留卷的重复启动以及代表性容器替换都不需要手工修库/清卷。
- 普通用户可完成注册、登录、帖子、评论、点赞、通知与搜索代表闭环；admin 在同一身份系统查看真实 Metrics/Logs/Events 并管理 Exporter。
- 普通用户无管理导航、直接管理 URL 无内部请求、可观测 API 均为 `403`；Frontend/bundle 不含内部地址或凭据。
- 默认只发布 Frontend/Backend 的 loopback 用户端口，内部数据、Monitor、Router、Marshaller、Kafka、VM、ES 无宿主发布；网络成员和服务身份符合第 6～7 节。
- 三条可观测链路都来自真实容器运行与真实操作；VM、Kafka/Router/Marshaller、Monitor 代表故障仅产生准确局部降级，社交事实与代表性非搜索闭环继续成立。
- MySQL/RabbitMQ/Kafka/VM/ES/Monitor plugin 等必要事实经服务重启和 project 保留卷再启动后可恢复；Redis 缓存丢失也不改变 MySQL 权威业务事实。
- 冷启动、只读 verify、完整验收、正常 down、失败/signal 清理、版本/分支治理与远程门禁通过，三份实施记录真实完整，根与 Frontend 版本均为 `1.9.3`。

### 16.2 完成与停止条件

只有第 16.1 节全部满足、Phase-12-03 Pull Request 已合入主远程 `main`、远程固定门禁成功，且三份 Phase 12 实施记录与真实提交一致，Phase 12 才完成。

任一自研镜像缺失、仍需宿主运行时启动业务、初始化非幂等、内部端口默认暴露、服务 DNS/身份边界缺失、普通用户隔离失效、完整可观测链路不真实、持久事实丢失、纯可观测故障阻断社交闭环或强归属清理证据缺失时，不得标记完成。

完成后立即停止，不追加 Kubernetes、Ingress、高可用、生产供应链、容量测试或独立 Review。独立实现 Review 只在用户明确请求时另行执行。

### 16.3 Phase 13 交接

- 带明确 version/revision label、非 root 用户、稳定 entrypoint、内部端口和信号语义的 Frontend、Backend、Worker、Indexer、Monitor、Router、Marshaller 与 Exporter 镜像。
- MySQL migration、Kafka Topic 初始化、search initialize 和 Monitor package bootstrap 的幂等作业契约，可直接转换为 Kubernetes Job/init 流程而不重写业务逻辑。
- `edge/business/observability` 网络成员、服务 DNS、端口、身份、持久卷、liveness/readiness 和启停顺序矩阵，作为 Phase 13 Service/ConfigMap/Secret/PVC/Probe 设计输入。
- Monitor 容器持有受管 Exporter 子进程且不需要 Docker socket 的已验证运行边界，以及独立 Exporter 镜像的可运行证据。
- 只发布 Frontend/Backend 用户面、Frontend 只代理 Backend、Backend 最终 admin 授权、内部服务不面向浏览器的安全基线。
- 完整 Compose 冷启动、持久化、故障隔离和资源清理验收矩阵，供 Phase 13 证明 Kubernetes 迁移后行为等价。

Phase 13 必须使用这些已验证镜像与运行契约，不在 Kubernetes 中临时编译源码、回退到宿主固定地址，或通过公开内部 Service 解决连通性。
