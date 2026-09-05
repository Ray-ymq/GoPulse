# Phase-12-03：全栈 Compose 验收与阶段收口实施方案

> 当前状态：待实施。本文档只定义集成验收与阶段收口批的范围和验收合同；目标版本、开发分支和执行顺序以 `Phase-12-总实施方案.md` 的权威分配表为准。

## 1. 批次目标

本批不增加新的容器或产品能力。它从已合入 Phase-12-01/02 的最新主线构建当前版本全部镜像，在强归属干净 Compose project 中完成 Phase 12 唯一权威跨批矩阵：

```text
无项目运行时宿主
  → 构建全部自研镜像
  → 冷启动全部自研服务与官方基础设施
  → migration / Topic / search / plugin 幂等初始化
  → 普通用户社交闭环 + 管理员可观测闭环
  → 内部端口/身份/制品安全
  → 持久化 + 可观测故障隔离 + 恢复
  → 强归属清理 + 文档/版本/CI/Phase 13 交接
```

除非最终矩阵真实复现阻断 Phase 12 验收的问题，本批不新增 Dockerfile、service、端口、配置模式、插件能力、API 或页面范围。

## 2. 前置条件

- Phase-12-01 与 Phase-12-02 已合入最新主远程 `main`，两批远程门禁成功，根/Frontend 版本与总方案权威分配一致。
- 从包含前两批能力的最新 `upstream/main` 创建总方案分配的本批分支，不沿用前批分支或 `update`。
- 已核对两份实施记录的实际 Dockerfile 布局、镜像 digest/label、网络与端口、作业顺序、持久卷、已通过检查、真实偏差和已知限制，不因会话上下文变化重跑未变更证据。
- 验收在 WSL2 Linux filesystem 与唯一 Docker daemon 执行，日常 project 不处于本批故障操作的目标范围。
- 开始前保存 Git、project/container/network/volume/image、发布端口、数据库、RabbitMQ queue、Kafka Topic/group/offset、VM 查询窗口、ES index/alias/PIT、Monitor plugin 与浏览器制品快照。
- 如任一前批未合入、远程失败、版本不一致或关键验收缺失，先回到对应批次处理，不用收口批掩盖。

## 3. 收口范围

### 3.1 最终静态、模块与治理门禁

- 在最终提交运行 Backend/Monitor/Router/Marshaller/Exporter format、unit、vet 与直接配置/生命周期 race；运行 Frontend test/typecheck/build。
- 运行脚本 CI unittest、版本/分支治理、Bash syntax/LF、Compose 渲染、默认端口、network/volume/health/dependency 约束和强归属 self-test。
- 从最终 Git revision 构建全部自研镜像，核对 image tag、OCI version/revision/source label、非 root user、entrypoint/cmd、architecture、layers/history 和运行容器 image ID，不复用无法归属的本地 tag。
- 核对前批已通过且未变化的 config 负测、Frontend proxy、Plugin Manager、Router/Marshaller 存储契约与页面 DTO 证据；只在相关实现或环境变化时重跑额外定向检查。

### 3.2 无宿主运行时冷启动

- 在 PATH 不提供 Go、Node.js、npm、项目 Python package、MySQL/Redis/RabbitMQ/Kafka/VM/ES 客户端的条件下，仅使用 Bash、Docker 与 Compose 执行完整入口。
- 使用随机合法 project 名、临时 env、随机 Frontend/Backend loopback 端口与全新命名卷，不读写日常 `.env`、`.run`、project 或卷。
- 从无本批容器/网络/卷状态构建全部镜像，启动六个官方基础设施、Frontend、Backend、Worker、Indexer、Router、Marshaller、Monitor/受管 Exporter 和三个初始化作业。
- migration、Kafka Topic、search initialize 与 plugin bootstrap 必须在无人工进入容器的情况成功；任一作业失败会明确阻断相关服务，不返回伪健康。
- 启动完成后宿主无 Go/Node 应用 PID，容器工作目录无 bind-mounted source/node_modules/build cache，应用运行只依赖当前镜像和持久卷。

### 3.3 Compose 资源、镜像与网络拓扑

- 每个预期 service 只有一个当前 project 容器（一次性作业保留可核对退出状态），所有容器/网络/卷的 project/service label 与当前随机 token 一致，无 `container_name` 冲突。
- Frontend 只位于 edge；Backend 是明确跨区连接点；Monitor 因 Redis 采集加入 business/observability；Elasticsearch 因 search/logs/events 加入两内网；其余组件无超出总方案矩阵的网络成员。
- 默认宿主只有 Frontend/Backend 的 loopback 发布，无 MySQL/Redis/RabbitMQ management/Kafka/VM/ES/Monitor/Router/Marshaller/Exporter 端口；浏览器不能见到内部服务。
- 内部调用使用 Compose service name 与容器端口，无 `host.docker.internal`、宿主固定 IP、固定 container IP 或跨 project 旁路。
- 自研容器非 root、无 privileged/host namespace/Docker socket，只有 Monitor plugin volume 等明确持久路径可写；不存在宿主宽泛 bind mount。

### 3.4 双使用态社交闭环

- 通过 Frontend 注册一个普通用户与一个待提升管理员，两者使用同一登录/Cookie。通过容器化 `admin-role` CLI 提升后重新确认数据库权威 role。
- 普通用户完成帖子列表/分页、发布、详情、评论、点赞、通知和搜索的代表性闭环，数据来自真实 MySQL/RabbitMQ/Elasticsearch/Redis 路径。
- 管理员仍可使用全部社交能力，不建立第二套会话、管理员专用社交 API 或容器内网身份替代。
- 暂停 Worker 或 Indexer 的一个代表性场景中，MySQL 事实与 Outbox 保留，恢复后通知或搜索收敛；不重复全部 Phase 2/3 穷举矩阵。
- Redis 停止的代表性帖子读写继续以 MySQL 回退，恢复后无需重建应用镜像。

### 3.5 管理员可观测与 Exporter 闭环

- 普通用户社交导航不显示可观测入口；直达五个管理路由在组件请求前进入无权限状态；Metrics/Logs/Events 和 Exporter 代表 API 均为 `403 permission_denied` 且内部 client 零调用。
- 通过真实 Redis Exporter/Monitor/Router/Kafka/Marshaller/VM 产生指标，管理员查看一个无标签 family 与一个 `mode` 或 `db` family，核对有限值、时间窗与安全 label。
- 通过真实 Backend/Worker/Indexer HTTP 或后台行为产生唯一日志，在 Logs 页执行代表性过滤/查询；不直接写 ES。
- 通过浏览器 Exporter 操作产生生命周期 Events，在 Events 页执行代表性查询；不直接写 Kafka/ES。
- 从未安装或当前持久状态完成 install/stop/start/update 代表矩阵，核对 desired/observed state、版本、最近启动/采集/成功和安全 last error；无第二个静态 Exporter service。
- 总览四区分别呈现真实最新 Metrics、Logs、Events 和 Exporter 状态，页面不把四请求称为强一致快照，不把无数据合成为健康。

### 3.6 端口、服务身份与输出安全

- 记录真实浏览器 network，只访问 Frontend origin 下 Backend API；不访问任一内部 service name、宿主内部端口或基础设施管理面。
- 从宿主扫描当前 project published ports，只存在 Frontend/Backend loopback；从 Frontend 网络确认内部组件不可达，从授权内部验证容器确认必需服务可达。
- 未携带/错误 Bearer 身份对 Monitor/Router/Marshaller 受保护接口被拒绝；VM Basic 身份缺失/错误被拒绝；Backend admin Cookie/JWT 不被当作内部服务身份。
- 扫描 Frontend bundle、容器 env 安全输出、image layers/history/config 和 API/page error，不得泄漏 token/password、完整 AMQP URL、VM query/body、ES alias/index/PIT、Kafka Topic/group、plugin 路径/PID 或 Docker 元数据。
- 对容器日志/事件/Exporter safe error 放置 HTML/script 哨兵，Frontend 以文本显示且不执行；容器 Web server 不破坏已有内容安全边界。

### 3.7 纯可观测故障与社交回归

- 对 VM、Monitor 和一个 Kafka/Router/Marshaller 传输点各执行一个有归属、有界的 stop/restart 窗口，每次只操作当前随机 project 的预期 service。
- VM 故障只使 Metrics 查询/新写入降级；Monitor 故障只使 Exporter 管理和新采集局部降级；传输故障按现有 best-effort/offset 语义表现。
- 每个窗口内 Backend `/health` 成功，`/ready` 不因 VM/Monitor/Router/Marshaller/Kafka 新失败；执行登录、帖子读取/发布和评论或点赞的代表性社交闭环。
- 恢复原容器/服务后无需重建 Frontend/Backend，受影响页面刷新成功，以故障后新真实数据证明链路恢复。
- 如验收 ES 停止，必须准确记录 search/Backend 既有 readiness/Logs/Events 的共同降级，只要求非搜索社交 API 不新增失败，不将它写为纯可观测故障。

### 3.8 容器替换与持久化

- 在第 3.4～3.5 节事实建立后，替换 Backend、Worker、Indexer、Monitor、Marshaller 容器，确认无状态应用不依赖宿主文件，Monitor 恢复受管 Exporter 且无孤儿进程。
- 分别替换一个业务持久服务和一个可观测持久服务，确认命名卷正确重挂载且必要事实可读。
- 对整个验收 project 执行保留卷的 down/up，重跑全部幂等初始化，核对用户/admin role、帖子/通知/搜索、Kafka group、VM/ES 历史数据和 plugin desired state。
- 重启验收后再执行一个新社交操作、一个新 Metrics 样本、一条新 Log/Event 和一次插件状态读取，证明不只是静态旧数据可见。

### 3.9 生命周期、资源清理与可重复性

- 验证日常 `dev.sh → verify.sh → down.sh → dev.sh` 的启动/只读检查/保留卷恢复，不要求宿主项目运行时。
- `verify.sh` 只读：不创建用户/帖子，不提权，不操作插件，不写 MySQL/RabbitMQ/Kafka/VM/ES，不打开无法关闭的 PIT。
- 完整主验收在正常、预期负面 self-test、构建失败、初始化失败、运行失败与 signal 路径中都只清理当前随机 project 容器/网络/卷和临时 env。
- 任何破坏操作前联合校验 project name/token、Compose project/service label、container/volume ID 和预期 image label；unknown、missing、multiple 或 mismatch 安全拒绝。
- 结束后对比 Git、日常 project/container/network/volume/image、发布端口、数据库、Kafka group/offset、VM/ES/Monitor plugin 和浏览器制品快照，只允许已记录的当前批制品。

### 3.10 文档、版本、CI 与远程状态

- README 只描述已验证的 Docker/Compose 前置、启动/检查/停止命令、用户入口、默认内部端口、卷保留、日志/诊断和已知限制，不保留“自研进程运行在宿主”的过期主路径。
- 更新总方案和三份拆分方案的真实状态/偏差，不将未运行命令、未观察远程 checks、未合入 PR 或 Phase 13 工作写为完成。
- 创建同名本批实施记录，汇总前两批已引用证据、本批实际命令/结果、阻断修复、偏差、限制和 Phase 13 输入。
- 将根 VERSION 与 Frontend 元数据更新为总方案分配的本批版本，从最终提交重建镜像并核对 label，不只改文件不重建。
- 只暂存本批文件并提交，push、创建 PR，分开记录本地成功、远程 checks 和 merge。不在本批默认执行独立实现 Review。

## 4. 阻断问题修复边界

本批只允许修复下列直接阻断固定矩阵的问题：

- 任一预期镜像无法从最终提交构建/启动，不是非 root，label/entrypoint/arch 错误，或泄漏源码/凭据。
- 冷启动依赖宿主项目运行时，初始化不幂等/竞态，容器信号不排空，或健康/readiness 语义导致重启风暴。
- 服务使用宿主/固定 IP，默认内部端口暴露，Frontend 进入内部网络，或内部身份被绕过/泄漏。
- 普通用户可访问管理能力，admin 无法完成真实三类查询/Exporter 操作，或容器化改变现有 API/DTO/会话契约。
- Monitor 无法从持久卷恢复 Exporter，出现双运行时所有者/孤儿进程/静默降级，或需 Docker socket/特权才能工作。
- 代表性持久数据在容器替换或保留卷 down/up 后丢失，纯可观测故障新阻断社交业务，或故障恢复必须重建无关应用。
- verify 产生写操作，或正常/失败/signal 清理误停/误删日常 project、用户卷、无关镜像或工作区变更。
- 版本/分支/OCI label/README/实施记录/CI 与真实完成状态不一致。

修复前记录最小复现与风险依据，修复后只重跑直接受影响模块/镜像/场景，最终固定阶段矩阵在稳定 diff 上运行一次。非阻断镜像缩小、构建速度、视觉、容量和未来架构改进记入后续事项。

## 5. 实施边界与非目标

- 不新增常驻应用、基础设施、网络、持久类型、用户端口或 debug 入口。
- 不更改社交、身份、搜索、业务消息、Metrics/Logs/Events、Exporter 管理或 Frontend 产品契约。
- 不做通用镜像优化活动、依赖/CVE 审计、SBOM/签名、registry 发布、多架构矩阵或容量/压力测试。
- 不实现 Kubernetes、Ingress、高可用、外部 TLS/密钥管理或集群可观测。
- 不执行全仓代码/架构 Review，不把独立 Review 报告或严重度分类作为 Phase 12 默认门禁。
- 不修改冻结 PowerShell 或新增原生 Windows/macOS 运行验收。

## 6. 预计文件与交付物

```text
dev/imple/Phase-12/Phase-12-总实施方案.md（最终状态/真实偏差）
dev/imple/Phase-12/Phase-12-01-社交业务容器运行闭环.md（最终状态/真实偏差）
dev/imple/Phase-12/Phase-12-02-可观测系统容器运行闭环.md（最终状态/真实偏差）
dev/imple/Phase-12/Phase-12-03-全栈Compose验收与阶段收口.md（最终状态/真实偏差）
dev/logs/Phase-12/Phase-12-03-全栈Compose验收与阶段收口.md

README.md
backend/README.md
frontend/README.md（若已建立）
monitor/README.md
router/README.md
marshaller/README.md
exporters/redis/README.md

scripts/verify-compose.sh
scripts/dev.sh（仅阻断修复）
scripts/down.sh（仅阻断修复）
scripts/verify.sh（仅阻断修复）
scripts/ci/**
.github/workflows/quality-gates.yml
deploy/**（仅阻断修复）
backend/**、frontend/**、monitor/**、router/**、marshaller/**、exporters/redis/**（仅阻断修复）

VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义改动。如固定验收未暴露产品问题，本批应主要提交验收编排/证据、文档、版本和实施记录。

## 7. 详细实施步骤

1. fetch 最新 `main`，核对前两批合入提交、远程 checks、版本、实施记录、真实偏差与已知限制，创建总方案分配的批次分支并保存资源快照。
2. 在最终提交运行第 3.1 节静态、模块和治理门禁，构建全部镜像并核对 OCI/image/container 元数据。
3. 使用无项目运行时 PATH、随机 project/env/ports 与全新卷完成第 3.2～3.3 节冷启动、一次性作业、服务/网络/端口/权限核对。
4. 执行第 3.4 节普通用户/admin 社交闭环与代表性 Worker/Indexer/Redis 恢复，不重复历史全矩阵。
5. 执行第 3.5～3.6 节普通用户隔离、admin Metrics/Logs/Events/Exporter 管理、总览、浏览器 network、内部身份和制品安全。
6. 执行第 3.7 节 VM、Monitor 和一个传输点局部故障/恢复，在每个窗口完成必要社交回归并核对 readiness 语义。
7. 执行第 3.8 节应用/数据容器替换与整栈保留卷 down/up，再产生新业务/可观测证据证明持续可用。
8. 执行第 3.9 节日常生命周期、verify 只读、正常/失败/signal 清理与最终资源快照对比；只对真实阻断问题做有限修复。
9. 更新 README、方案真实状态/偏差、根/Frontend 目标版本与本批实施记录；从最终提交重建并核对镜像 label。
10. 在稳定 diff 上完成第 9 节固定命令，只暂存本批文件并提交；push、创建 PR，查询真实远程 checks 和合入状态。
11. 全部 Phase 验收、远程门禁与实施记录一致后标记 Phase 12 完成，立即停止并向 Phase 13 交接。

## 8. 风险与控制

- **宿主已安装工具使“仅 Docker”假通过**：主验收使用受限 PATH，API/浏览器客户端在 one-shot 容器中，并扫描宿主 Go/Node 应用进程。
- **复用旧镜像/卷伪造冷启动**：从最终 revision 重建标签，用全新随机 project/卷，核对容器 image ID 与 OCI revision。
- **全栈矩阵成为重复的历史回归大全**：只执行容器化直接改变的冷启动、网络、信号、持久化和代表业务/可观测路径；前批成功证据在实现未变时直接引用。
- **故障操作破坏日常资源**：每次 stop/restart/down/volume 操作前校验 project/service label、ID 和随机 token，最终对比快照。
- **把 ES 共享故障误述为纯观测故障**：总是单独记录 search/Backend readiness/Logs/Events 的既有联动，纯观测隔离只用 VM、Monitor 和 Kafka/Router/Marshaller 证明。
- **收口批演变为新容器功能**：修复只限第 4 节真实阻断，非阻断镜像优化、生产化、Kubernetes 和 Review 立即留后。
- **虚构远程完成**：本地验证、push、PR、checks、merge 和 Phase 状态分开记录，未观察即不写完成。

## 9. 固定验证命令与必要回归

最终 diff 上至少执行：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/config ./internal/http/... ./internal/observability/logship ./internal/exporterplugin ./internal/metricquery ./internal/worker ./internal/search/...)
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
scripts/verify-compose.sh
git diff --check
```

- `scripts/verify-compose.sh` 必须自己构建/启动强归属随机 project，完成第 3.2～3.9 节并清理；不依赖先启动的日常栈。
- 该命令是本批唯一完整真实阶段入口。它自然覆盖的历史业务、Exporter、Monitor、Router、Marshaller、Logs、Events 和 observability UI 健康路径不再全部重跑。
- 只有该矩阵暴露共享业务/写入/授权/清理回归，或相关实现在本批修改时，才扩展运行历史定向真实脚本，并在实施记录写明原因。

## 10. 阶段收口验收标准

- 干净 WSL 条件仅依赖 Docker/Compose 可构建、初始化、启动、只读验证和停止完整 GoPulse，宿主不启动 Go/Node 项目进程。
- 全部自研镜像的 tag、version/revision/source label、非 root user、entrypoint/cmd、architecture、运行内容和信号语义与最终提交一致，无源码/凭据泄漏。
- migration、Kafka Topic、search initialize、Monitor package bootstrap 可首次执行且幂等重跑；失败明确阻断，不伪造就绪或破坏旧持久事实。
- 默认仅 Frontend/Backend 以 loopback 发布，Frontend 只位于 edge 并只代理 Backend；内部数据/可观测组件仅在对应 internal 网络和独立服务身份下可达。
- 普通用户社交与搜索代表闭环通过，无管理导航/直路由数据/API 权限；admin 同时完成社交、真实 Metrics/Logs/Events 查询与 Exporter 管理。
- Monitor 保持受管 Exporter 唯一运行所有权，可从卷恢复 desired state，无 Docker socket/特权/双实例/孤儿进程；独立 Exporter 镜像的真实容器矩阵也通过。
- VM、Monitor 与代表性 Kafka/Router/Marshaller 故障的局部降级、必要社交回归、Backend 无新增 readiness 依赖和恢复后新数据收敛通过；ES 共享影响被准确记录。
- 应用与持久服务容器替换、整栈保留卷 down/up 后，业务事实、异步收敛、搜索、Kafka offset、Metrics/Logs/Events 与 plugin state 的必要历史/新数据均可用。
- `verify.sh` 只读，日常生命周期可重入，完整验收的正常/失败/signal 路径只清理当前强归属资源，日常 project、用户卷/镜像和工作区改动保持。
- 固定本地门禁、远程 checks、版本/分支/OCI label 治理和三份实施记录真实完整，根/Frontend 版本与本批分配目标版本一致。

## 11. 明确完成条件

只有第 10 节全部满足、本批 Pull Request 已合入主远程 `main`、远程固定门禁成功，三份 Phase 12 实施记录与真实提交一致，Phase 12 才完成。

任一干净冷启动、镜像契约、初始化幂等、网络/端口/身份边界、普通用户隔离、社交闭环、管理员三链路/Exporter 操作、持久恢复、纯可观测故障隔离、只读 verify、强归属清理或远程证据缺失时，不得标记 Phase 12 完成。

完成后立即停止，不追加独立 Review、镜像优化、供应链、Kubernetes、Ingress、高可用、容量或新产品能力。

## 12. Phase 13 交接

- 从最终提交构建且通过运行验收的 Frontend、Backend、Worker、Indexer、Monitor、Router、Marshaller 与 Exporter 镜像，以及其 version/revision/user/entrypoint/port/arch 契约。
- migration、Kafka Topic、search initialize、Monitor package bootstrap 的幂等作业/完成条件，可映射为 Kubernetes Job/init 流程。
- `edge/business/observability` 网络成员、服务 DNS、默认暴露端口、Bearer/Basic 身份、持久卷、health/readiness、restart/shutdown 和故障语义矩阵。
- Frontend/Backend 用户面、Frontend 只代理 Backend、Backend 最终 admin 授权、内部 service 无浏览器旁路的经验证安全基线。
- MySQL/Redis/RabbitMQ/Kafka/VM/ES/Monitor plugin 持久化与容器替换证据，供 Phase 13 确定 PVC 和工作负载边界。
- 完整冷启动、双使用态、三类可观测、Exporter 管理、局部故障、持久恢复与强归属清理矩阵，作为 Kubernetes 迁移行为等价验收的直接基线。

Phase 13 只在上述容器制品和契约上建立 Kubernetes 资源，不通过重新编译、宿主固定地址、NodePort 内部旁路或特权 Docker 控制来弥补本阶段缺口。
