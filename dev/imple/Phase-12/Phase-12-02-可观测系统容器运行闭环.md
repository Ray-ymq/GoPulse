# Phase-12-02：可观测系统容器运行闭环实施方案

> 当前状态：待实施。本文档只定义第二个执行批次的范围与验收合同；目标版本、开发分支和执行顺序以 `Phase-12-总实施方案.md` 的权威分配表为准。

## 1. 批次目标

在已合入的社交业务容器闭环上，将 Monitor、Message Router、Marshaller 和 Redis Exporter 纳入标准镜像与完整 Compose，形成可从真实业务/插件操作到管理员浏览器查询的三类可观测闭环：

```text
Redis → Monitor 受管 Exporter → Monitor
Backend / Worker / Indexer logs → Monitor
Monitor lifecycle/failure events → Monitor EventMonitor
                              ↓
                         Message Router
                              ↓
                            Kafka
                              ↓
                          Marshaller
                    ┌──────┴──────┐
             VictoriaMetrics       Elasticsearch
                    └──── Backend admin APIs ────┘
                              ↓
                   Frontend 管理员工作区
```

批次完成必须同时证明：

- Monitor、Router、Marshaller 和 Redis Exporter 均有非 root、可版本化、可独立构建和运行验证的镜像。
- 显式 container mode 仅在 Compose 内允许通配监听和合法服务 DNS，host 模式的 loopback 安全默认不退化。
- 默认完整 Compose 不发布 Monitor/Router/Marshaller/Exporter/Kafka/VM/ES 宿主端口，但 Backend 通过内部身份访问它们，Frontend/浏览器仍只访问 Backend。
- 管理员从真实容器运行与真实操作查询 Metrics/Logs/Events 并管理 Exporter；普通用户仍被 Frontend/Backend 双层隔离。
- 代表性 VM、Kafka/Router/Marshaller 和 Monitor 故障是局部降级，不绑定 Backend 业务 readiness 或阻断非搜索社交闭环。

只将官方 Kafka/VM 加入 Compose、只构建可观测镜像、默认发布内部端口，或绕过 Monitor 插件契约静态启动第二个 Exporter，均不构成本批完成。

## 2. 前置条件

- Phase-12-01 已合入最新主远程 `main`，远程门禁成功，Frontend/Backend/Worker/Indexer 镜像、业务 Compose、作业、网络、卷和实施记录可核对。
- 从包含 Phase-12-01 的最新 `upstream/main` 创建总方案分配的本批分支，不沿用前批开发分支。
- 核对 Monitor Plugin Manager、package script/manifest、Exporter 进程所有权、Router/Marshaller/Monitor health/readiness、Kafka Topic/group、VM Basic identity、ES logs/events alias 与 Phase 11 Backend admin client 契约。
- 核对 Phase-12-01 实施记录的真实 Dockerfile 布局、镜像 label/tag、Compose 网络、验收入口与已知偏差，只在必要处扩展。
- 开始前保存日常 project/container/network/volume/image、端口、Kafka group/offset、VM 样本窗口、ES index/alias/PIT、Monitor plugin volume 与工作区快照。

## 3. 实施范围

### 3.1 可观测自研镜像

- 为 Router、Marshaller 和 Monitor 建立多阶段、固定 base、非 root 镜像；最终层只含二进制、必要 CA/timezone 数据与明确运行资源。
- 为 Redis Exporter 建立独立镜像，并使用同一 commit/arch/version 产生 Monitor 内置的确定性 `.tar.gz` package；manifest version、OS/arch、entrypoint digest 与镜像 label 可交叉核对。
- Monitor 镜像只对 plugin root 和必要 tmp 路径保留写权；Router/Marshaller/Exporter 优先只读根文件系统。全部进程作为 PID 1 或等价可转发信号运行。
- image inspect 检查非 root user、entrypoint/cmd、version/revision/source label、architecture、exposed internal port 和 mount；image history/layer 扫描不包含 token/password/`.env`/源码。

### 3.2 容器运行模式与配置约束

- 按总方案引入全模块一致的 `host|container` 显式模式。未设置仍为 host，未知值失败；不使用 `APP_ENV=development` 自动放开网络限制。
- Backend log shipper 在 container mode 允许 `http://monitor:9090`，但仍拒绝 userinfo、query/fragment、额外 path、非 HTTP scheme、控制字符和缺失端口；host mode 继续只允许 loopback origin。
- Monitor container mode 允许 `MONITOR_HTTP_HOST=0.0.0.0` 并将 Router URL 设为 `http://router:9091`；受管 Exporter 仍绑定 Monitor 容器内 `127.0.0.1:9121`，Redis target 使用 `redis:6379`。
- Router 绑定 `0.0.0.0:9091`、Kafka broker 使用 `kafka:19092`；仍仅接受固定 Topic 和独立 Bearer token。
- Marshaller container mode 允许 `0.0.0.0:9093`、`kafka:19092`、`http://victoriametrics:8428` 和 `http://elasticsearch:9200`；host mode 继续对监听与 VM/ES origin 执行 loopback 保护。
- Backend 使用 `http://monitor:9090` 调用 plugin API，使用 `http://victoriametrics:8428` 查询 Metrics，使用 `http://elasticsearch:9200` 查询 search/logs/events；Frontend 不获得任一该配置。
- 对每个放宽点添加代表性成功/失败单测，包括 host 拒绝 DNS/non-loopback、container 接受合法服务名，以及两种模式均拒绝凭据 URL、多余 path、控制字符和空 host。

### 3.3 Monitor 插件卷与 Exporter bootstrap

- 将 `MONITOR_PLUGIN_ROOT` 固定为容器内绝对路径并挂载专用 `monitor_plugin_data` 卷，确保非 root Monitor 用户是唯一写者。
- Monitor 镜像内包含由当前 root VERSION 和当前架构生成的 Redis Exporter package，不从运行时网络下载、不 bind mount 宿主构建产物。
- 首次启动无 registry 时安装并按 desired state 启动；同版本重启不触发无意义更新；新镜像版本可按明确策略升级；旧镜像面对高版本卷不得静默降级。
- bootstrap 复用 Plugin Manager 的 archive/manifest/digest/OS/arch/path 与 rollback 不变量，不建立第二套未校验解压/启停逻辑。失败必须保留旧可恢复事实且不报告虚假 running。
- admin 经 Backend 的 install/start/stop/update 继续以 Monitor 为唯一事实源。默认 Compose 不启动额外静态 Exporter service，不挂载 Docker socket，不改变 stop 语义为“仅停止采集”。
- 独立 Redis Exporter 镜像在隔离 project/profile 中连接真实 Redis，验证 health、完整 10 families/11 samples、target/auth failure `up 0`、恢复与 SIGTERM；不与默认 Monitor 同时抢占同一 target identity。

### 3.4 完整 Compose 可观测拓扑

- 将 Kafka、`kafka-init`、VictoriaMetrics、Router、Marshaller、Monitor 加入 Phase-12-01 拓扑，Elasticsearch 作为 search/logs/events 共享存储同时加入 business/observability 内网。
- Kafka 默认只广播容器内 listener `kafka:19092`，不广播宿主 `127.0.0.1` 或发布 `9092`；Topic 禁止自动创建，`kafka-init` 幂等创建/核对固定 Topic。
- VM 保留认证与命名卷，只对 Backend/Marshaller 所在可观测内网可达；不向浏览器或 Frontend 网络发布。
- Router/Marshaller/Monitor 只使用 Compose `expose`/网络可达，无 `ports`；Bearer token 仍必需。Monitor 为采集 Redis 加入 business 内网，不因此获得 MySQL/RabbitMQ 凭据。
- Backend/Worker/Indexer 日志配置指向 Monitor service；日志投递队列仍有界且 best-effort，Monitor 未就绪或重启不阻断这些应用容器启动与业务事实。
- 默认完整 `dev.sh`/Compose 启动全部服务；如保留 business-only/profile 入口，必须为显式可选路径，不能使完整系统需要手工连续启动多个 profile。

### 3.5 双使用态与管理员真实闭环

- 使用容器化 `admin-role` CLI 对随机验收数据库提升指定用户；不新增公开提权 API 或直接暴露 MySQL。
- 普通用户社交导航无可观测入口，直达总览/Metrics/Logs/Events/Exporter 路由在组件请求前拒绝；直接 API 为 `403 permission_denied` 且 VM/ES/Monitor client 调用为零。
- 管理员通过 Frontend 查看真实 Redis Metrics、由 Backend/Worker/Indexer 产生的 Logs、由真实插件操作/故障产生的 Events，并完成代表性 install/stop/start/update。
- 浏览器 network 只包含 Frontend origin 下 Backend `/api/v1`，不出现任何内部 service name/端口/token。页面仍只渲染安全 DTO，不显示 Compose/container/image/volume 元数据。
- 在可观测闭环运行时完成代表性社交操作，确保方案没有将管理员域变成第二套登录或破坏 admin 的社交能力。

### 3.6 可观测故障隔离与恢复

- 分别在验证 project 中停止/恢复 VM、Monitor，并对 Kafka/Router/Marshaller 选择一个代表性传输故障；每次前后校验 project/service/container ID，不影响日常栈。
- VM 故障期间 Metrics 局部 `metrics_unavailable`，恢复后 Backend/Marshaller 无需重启即可查询/写入；Logs/Events/Exporter 和社交业务保持准确状态。
- Monitor 故障期间 Exporter 管理与新采集局部 `monitor_unavailable`，Backend liveness、社交读写、已存 Metrics/Logs/Events（存储可用时）不被错误清空；恢复后 Monitor 从插件卷恢复 desired state。
- Kafka/Router/Marshaller 故障期间新可观测数据按既有 best-effort 与 consumer offset 契约降级，不扭曲为强一致；恢复后以新真实数据证明链路重新可用。
- 每个纯可观测故障窗口执行登录、帖子读取/发布与评论或点赞的代表性社交回归，核对 Backend `/ready` 未新增 VM/Monitor/Router/Marshaller/Kafka 依赖。
- Elasticsearch 故障不纳入“纯可观测 readiness 无影响”证明，因其是帖子搜索的共享依赖；实施记录必须继续准确说明 ES 对 search/既有 readiness 的影响。

### 3.7 持久化和容器替换

- 在真实 Metrics/Logs/Events 与插件 desired state 建立后，分别替换 Monitor、Marshaller 和代表性 Kafka/VM/ES 容器，核对 group offset、历史数据与插件恢复。
- Monitor 替换后旧 Exporter PID 不复用，无孤儿子进程，新进程的 manifest/digest/version 与持久事实一致。
- 保留卷的整栈 down/up 后，Kafka Topic/group、VM 样本、ES logs/events alias/data 和 Monitor plugin state 可恢复，不需手工再安装或清空。
- 该矩阵只验证直接容器运行边界，不重复 Phase 8～11 所有 replay、PIT、去重、分页和页面边界全排列。

### 3.8 端口、网络和制品安全

- 渲染默认 Compose 并检查 `ports`，只允许 Frontend/Backend 两个 loopback 用户面；默认不加载 debug override。
- 检查 Frontend 只位于 edge，不能 DNS/连接内部组件；内部验收容器未携带服务 token 时访问 Monitor/Router/Marshaller readiness/ingest 必须被拒绝。
- 扫描 image history/config/layers 与 Frontend dist，不得包含 `.env`、真实/示例 token 的意外复制、VM Basic 凭据、内部 URL、ES alias/index、Kafka Topic/group 或服务器绝对路径。
- 检查容器 user、capabilities、read-only rootfs/tmpfs、volume/bind mount、network mode 和 Docker socket；任一 privileged/host namespace/无界宿主 bind 均为阻断。
- health/readiness 失败与应用日志不输出 token/password/full connection URL、容器 env dump 或底层响应 body。

## 4. 实施边界与非目标

- 不改变 Metrics/Logs/Events Envelope/payload/index/query DTO、Kafka Topic/group、VM metric family 或 Backend 管理 API。
- 不将 Redis Exporter 改为默认独立常驻 service 而破坏 Monitor Plugin Manager 的 install/start/stop/update 契约。
- 不为 Monitor 挂载 Docker socket，不引入 Docker-in-Docker、特权容器、宿主 PID 操作或远程容器控制面。
- 不新增告警、更多 Exporter、任意查询、自定义总览、更多 Kafka Topic、多 broker 或生产 SASL/TLS。
- 不修改 ES 对帖子搜索和 Backend readiness 的现有契约；只防止容器化新增纯可观测依赖。
- 不创建 Kubernetes/Ingress 资源，不发布镜像到外部 registry，不执行供应链安全项目或全仓 Review。

## 5. 预计文件与交付物

```text
.dockerignore
.env.example
deploy/compose.yaml
deploy/compose.debug.yaml（若需）
deploy/docker/**

backend/internal/config/**
monitor/internal/config/**
monitor/cmd/monitor/**
router/internal/config/**（若需）
marshaller/internal/config/**
exporters/redis/internal/config/**（若需）

scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-compose.sh
scripts/package-redis-exporter.sh（仅容器构建需要的可重复参数）
scripts/ci/**
.github/workflows/quality-gates.yml

README.md
backend/README.md
monitor/README.md
router/README.md
marshaller/README.md
exporters/redis/README.md
dev/imple/Phase-12/Phase-12-总实施方案.md（仅状态/真实偏差）
dev/imple/Phase-12/Phase-12-02-可观测系统容器运行闭环.md（仅状态/真实偏差）
dev/logs/Phase-12/Phase-12-02-可观测系统容器运行闭环.md
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义改动。对既有业务容器文件的修改只能用于接入可观测服务或修复本批真实阻断。

## 6. 详细实施步骤

1. fetch 最新 `main`，核对 Phase-12-01 合入、远程 checks、版本、实施记录和真实 Compose 契约，创建总方案分配的批次分支并保存资源快照。
2. 实现/收紧显式 host/container 运行模式，对 Backend log shipper、Monitor 监听/Router URL、Marshaller 监听/VM/ES URL 运行直接 config 正负测试。
3. 构建 Router、Marshaller、Monitor 和独立 Exporter 非 root 镜像，构建同版本/架构的确定性 Exporter package，执行 image inspect/history/layer 与单容器 smoke。
4. 实现 Monitor 插件卷权限、幂等 bootstrap、同版本重启、升级/防降级、旧事实保留和受管 Exporter 信号关闭。
5. 扩展 Compose 为完整默认拓扑，加入 observability internal 网络、Kafka internal listener/init、VM/Monitor 卷、内部 DNS/token/Basic identity、healthcheck/restart/stop 配置，确认默认无内部宿主端口。
6. 使 Backend/Worker/Indexer 通过 Monitor service 投递日志，Backend 通过 VM/ES/Monitor service 执行管理查询/操作，保持 Frontend 只反向代理 Backend。
7. 扩展容器验收入口，使用真实浏览器完成普通用户隔离、admin Metrics/Logs/Events/Exporter 闭环、Frontend network/bundle 和内部 token 拒绝检查。
8. 执行 VM、Monitor 与一个 Kafka/Router/Marshaller 代表故障/恢复窗口，在每个窗口执行必要社交回归；替换 Monitor/Marshaller/数据容器并保留卷 down/up。
9. 运行独立 Exporter 容器矩阵与默认受管 Exporter 矩阵，确认无双运行时所有者、无 Docker socket、无孤儿进程。
10. 最终 diff 稳定后执行第 8 节固定门禁一次，更新 README、方案状态、根/Frontend 目标版本与本批实施记录。
11. 只暂存本批文件并提交；push、创建 PR，查询真实远程 checks 与合入状态。通过后立即停止并向收口批交接。

## 7. 风险与控制

- **放宽 loopback 造成宿主暴露**：放宽只在显式 container mode 生效，host 默认负测与 Compose 无 ports/internal network 联合验收。
- **Monitor/Exporter 双重所有权**：默认栈只允许 Monitor 插件运行时；独立 Exporter 镜像只在强隔离 project/profile 验收，不与 admin 管理闭环共存。
- **插件卷因容器 UID 不可写**：用空卷首启动、容器替换、同版本重启和失败 rollback 证明，不通过运行时 root/chmod 掩盖。
- **服务就绪与存活混淆**：healthcheck 保持 liveness，Bearer `/ready` 由 verify 解读；不将下游短断变成无限容器重启。
- **Kafka advertised listener 指回宿主**：默认 Compose 只验证 `kafka:19092` 内部广播，Router/Marshaller 真实连接同一值，不存在 `127.0.0.1:9092` 默认发布。
- **内部 token 进入镜像或浏览器**：只在运行时注入服务容器，扫描 image history/layers、Frontend dist/network 和应用错误；不在 CI 打印完整渲染凭据。
- **故障测试破坏日常栈**：每次 stop/restart 前验证随机 project label、service 和 container ID，结束对比快照，mismatch 安全拒绝。
- **容器化改变产品语义**：主证据使用 Phase 11 现有 Backend/Frontend 契约，不新增任意查询、双登录或静态 Exporter 替代品。

## 8. 固定验证命令与必要回归

最终 diff 上至少执行：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/config ./internal/observability/logship ./internal/exporterplugin ./internal/metricquery)
(cd monitor && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd monitor && go test -race -count=1 ./internal/config ./internal/plugin ./internal/metrics/... ./internal/events/...)
(cd router && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd marshaller && go test -race -count=1 ./internal/config ./internal/consumer ./internal/victoriametrics ./internal/elasticsearch)
(cd exporters/redis && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch "$(git branch --show-current)" --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-compose.sh --self-test
scripts/verify-compose.sh --observability
git diff --check
```

- 如最终采用单一无子命令主入口，可用覆盖本批全部矩阵的等价命令替代 `--observability`，并在实施记录写明。
- 容器主闭环必须使用真实 Redis Exporter/Monitor/Router/Kafka/Marshaller/VM/ES 和真实浏览器；mock/fake 仅用于 config/response 负面单测。
- 现有 `verify-monitor/router/marshaller/logs/events/observability-ui` 只在相关公共契约改变、容器主闭环失败或固定 CI 自然要求时运行直接范围；不重复运行全部等价真实矩阵。

## 9. 批次验收标准

- Monitor、Router、Marshaller 和 Redis Exporter 镜像可重复构建，非 root、label/entrypoint/arch 正确，无工具链、源码、`.env` 或运行凭据泄漏。
- host 模式继续拒绝非 loopback 监听/受限 origin；container 模式接受总方案的合法 service DNS 值，但不接受凭据 URL、任意 path、控制字符或未知模式。
- 完整默认 Compose 使用 internal observability 网络、Kafka internal listener、VM/ES/Monitor 持久卷和独立服务身份，默认仅 Frontend/Backend 发布 loopback 端口。
- Monitor 从镜像内确定性 package 幂等 bootstrap，在专用卷保存插件事实，容器替换后恢复 desired state，无双运行时所有者、Docker socket、特权或孤儿 Exporter。
- 独立 Exporter 镜像的真实 Redis/`up 0`/恢复/信号矩阵通过，默认完整栈的 admin install/start/stop/update 仍由 Monitor 事实源驱动。
- 管理员通过真实浏览器查询来自容器运行的 Metrics/Logs/Events 并管理 Exporter；普通用户路由/API 隔离、Frontend-only Backend 访问与安全 DTO 保持。
- VM、Monitor 和代表性 Kafka/Router/Marshaller 故障的局部降级/恢复、Backend 无新增 readiness 依赖、社交回归与准确 ES 共享依赖记录通过。
- Monitor/Marshaller 与代表性 Kafka/VM/ES 容器替换、保留卷 down/up 后，插件 desired state、offset 和必要历史 Metrics/Logs/Events 可恢复。
- 固定本地门禁、版本/分支治理、实施记录和远程 checks 通过，根/Frontend 版本等于总方案为本批分配的目标版本。

## 10. 明确完成条件

只有第 9 节全部满足、本批 Pull Request 已合入主远程 `main`、远程门禁成功，且同名实施记录与真实提交一致，本批才完成。

任一可观测镜像缺失、host 安全默认退化、内部端口默认暴露、Frontend 获得内部配置、Monitor/Exporter 所有权失真、三条链路不真实、普通用户隔离失效、纯可观测故障阻断社交闭环或持久恢复失败时，不得标记完成。

完成后立即停止，不追加 Kubernetes、Ingress、新 Exporter/新 Topic、生产身份系统、供应链工程或独立 Review。

## 11. Phase-12-03 交接

- 完整自研镜像清单、version/revision/user/entrypoint/port/arch 契约与镜像安全扫描证据。
- 完整 Compose `edge/business/observability` 网络、默认端口暴露、服务 DNS、token/Basic 身份、初始化作业、health/restart/stop 契约。
- MySQL/Redis/RabbitMQ/Kafka/VM/ES/Monitor plugin 卷及直接容器替换、保留卷 down/up 证据。
- 管理员 Metrics/Logs/Events/Exporter 真实浏览器闭环、普通用户隔离、Frontend network/bundle 安全和纯可观测故障局部降级证据。
- 容器主验收入口、self-test、强归属清理和已通过的直接测试，收口批只补跨批证据和真实阻断修复。
