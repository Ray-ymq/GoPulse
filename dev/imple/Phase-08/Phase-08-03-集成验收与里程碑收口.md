# Phase 8-03：集成验收与 Milestone 2 收口实施方案

> 权威目标版本与开发分支以 `Phase-08-总实施方案.md` 第 3.2 节为准：本批对应 `1.5.3` / `develop/1.5.3`。
>
> 当前状态：未开始。

## 1. 批次目标

基于已合入主远程 `main` 的 Phase-08-01 指标纵向闭环和 Phase-08-02 可靠消费/运维闭环，在干净、强归属的隔离资源和同一最终构建上执行 Phase 8 固定端到端矩阵、必要回归、文档、版本、实施记录和远程状态收口，证明：

```text
真实 Redis → Redis Exporter → MetricsMonitor → Message Router
          → Kafka → Marshaller → VictoriaMetrics → 指标查询
```

以及：

```text
坏消息 / 重复消息 / Kafka、Marshaller、VictoriaMetrics 故障
→ offset、重放、恢复和资源行为符合契约
→ 普通用户社交业务、管理员授权、RabbitMQ 任务保持原契约
```

本批不引入新产品能力。只有固定矩阵真实暴露、会使 Phase 8 或 Milestone 2 验收不成立的问题可以最小修复；Backend Metrics Query API、Frontend 页面和其他扩展继续留给后续阶段。

## 2. 前置条件

- Phase-08-01 和 Phase-08-02 均已合入主远程 `main`；Marshaller、Router、Monitor、脚本/Compose 等远程门禁成功，两份实施记录与真实提交一致。
- 主远程根与 Frontend 版本均为 `1.5.2`；Topic、record、Envelope、正式 group、offset、ownership、映射、VictoriaMetrics 写入/查询、故障恢复和资源语义与总方案一致。
- 从最新主远程 `main` 创建 `develop/1.5.3`，不沿用 Phase-08-01、Phase-08-02、Phase 7 或 `update` 分支。
- WSL2 Linux filesystem、Docker daemon、端口、Compose project、container、network、volume、`.run` 进程、Kafka group 和插件根可建立强归属隔离。
- 开始前保存 Git 状态与日常资源快照；存在用户资源时使用独立临时仓库/目录、随机 project、端口、group 和 volume，不触碰原资源。

## 3. 实施范围

### 3.1 最终构建与契约核对

- 对照 Phase-08-01、Phase-08-02 实施记录、合入提交和远程 checks，核对 Marshaller module、Kafka 客户端版本、VictoriaMetrics 镜像/flags、Topic/group、ownership/commit 状态机、配置、脚本和 CI 真实状态。
- 核对 Phase 7 最终 record value 仍是 Router 未改写原始 bytes，key=`message_id`，合法输入为 metrics/redis Envelope v1。
- 核对 Marshaller 的严格 validator、确定性 transformer、VictoriaMetrics client 和 Consumer 状态机与总方案一致；不存在自动提交、跨 record 无界队列或持久本地去重。
- 确认 Monitor 无 Kafka/VM、Router 无 payload 清洗/存储、Marshaller 不采集 Exporter或处理 RabbitMQ，Backend readiness 不依赖 VictoriaMetrics。
- 确认 Phase 8 未增加 Backend/Frontend Metrics Query 产品入口，VictoriaMetrics 与 Marshaller 端口保持 loopback/内部边界。
- 确认根/Frontend 版本、Compose 渲染、Bash 语法、README 和分支治理基线一致。

### 3.2 真实成功、目标故障与恢复闭环

- 指标主链路只启动随机隔离 Redis、Kafka、VictoriaMetrics、Router、Marshaller、Monitor 和真实 Redis Exporter；MySQL、RabbitMQ、Elasticsearch、Backend、Business Worker、Search Indexer 与 Frontend 不作为指标写入/查询的前置条件。
- 第 3.6 节业务隔离回归再使用独立强归属的 MySQL、RabbitMQ、Elasticsearch、Backend、Workers 和必要 Frontend 资源；该场景证明故障隔离，但不能反向成为 Phase 8 指标链路依赖。
- 显式确认 Topic 和正式测试 group 起点，清空或使用新随机 VM volume，记录本次可确认的 message ID、partition/offset 与 Envelope timestamp 时间窗。
- 对隔离 Redis 写入普通 key、带 TTL key并执行代表性命令；等待真实 success Envelope 经完整链路写入 VM。
- 使用受控即时/范围查询核对 10 个 family、11 个 success sample：`up`、`uptime_seconds`、`connected_clients`、`used_memory_bytes`、`commands_processed_total`、`keyspace_hits_total`、`keyspace_misses_total`、`cpu_seconds_total{mode="user|system"}`、`db_keys{db="..."}`、`db_expiring_keys{db="..."}`，且值与同一 Redis `INFO`/Exporter 响应在允许采集时差内对应。
- 停止 Redis但保持 Exporter、Monitor、Router、Marshaller 进程不变，查询同一 target 的新 `gopulse_redis_up=0`；恢复 Redis 后不重启任何应用即重新查询到 `up=1` 和完整 family。
- 查询使用本次 timestamp 窄窗口和固定 source/target labels，不把旧 volume 数据或直接 import 数据误认成当前结果；在主矩阵前后通过受保护 `/metrics` 核对 `vm_rows_invalid_total` 未增加，避免把 import 的 `204` 误写成逐行解析证明。

### 3.3 映射、异常过滤与继续消费

- 对真实 success/up0 时序核对 metric name、有限 value、Envelope Unix 毫秒、固定 source/target 标签和原 `mode|db` 标签；确认不存在 message ID、plugin version、scrape status、kind、partition/offset 等额外标签。
- 超限、坏 UTF-8/JSON、重复/未知/缺失字段、尾随 token、非法 schema/type/source/timestamp、null、非法 status/family/kind/label/value、重复 sample 和集合不完整的全集由 Phase-08-01 最终提交上的 Marshaller unit/fake-writer 测试证明；revoke/lost、commit 失败和延迟响应由 Phase-08-02 的确定性状态机测试证明，不在真实 Kafka 端到端层重复枚举。
- 在本次 offset 范围内，受控 fixture producer 只注入三个代表：一个结构错误、一个 key/ID 不符和一个 payload/sample 契约错误。每类都必须有“未调用/未新增 VM 时序点”和“对应 offset 已提交/随后真实合法消息已写入”两类证据；只看日志或进程存活不足。
- 注入异常后再由真实 Redis/Monitor/Router 产生合法消息，证明同一 partition 没有被毒消息永久阻塞。
- 核对永久异常日志只包含固定 reason code 和有限传输关联信息，不包含 record value、标签全集、VM 响应、凭据或内部 URL。

### 3.4 重复投递、commit 失败与进程恢复

- 保存一条合法真实 Envelope 的原始 Kafka key/value 与第一次写入的时序集合；通过独立 fixture producer 原样重放相同 key/value。
- 证明两次处理生成相同 metric names、labels、values 和 Unix 毫秒 timestamp；在 1ms dedup 设置下，窄时间窗查询每条时序只有一个有效点。
- 核对并执行 Phase-08-02 已交付的定向状态机测试：通过注入 Committer 确定性模拟 VictoriaMetrics HTTP 接受后 Kafka commit 失败，并通过注入 ownership lease 模拟 revoke/lost 与延迟响应竞态；若代码、依赖和环境未变化可引用仍有效结果，发生相关变化时只重跑受影响测试。测试接口不得暴露为生产 HTTP 或普通运行配置。
- 真实端到端只执行一个可确定观察的进程恢复场景：停止 VictoriaMetrics、产生合法 record、确认正式 group committed offset 未推进后终止 Marshaller；恢复同一 VM/volume 并重启 Marshaller，确认从 committed offset 重取、最终查询成功并提交。不得依赖 shell 时序猜测“恰好处于 commit 前”。
- 明确区分：相同 record 的确定性重放可稳定查询；不同 message ID 或不同 timestamp 的重复采集仍是不同样本；系统仍是 at-least-once，不写 exactly-once 结论。

### 3.5 Kafka、VictoriaMetrics 与 Marshaller 故障恢复

- **停止 VictoriaMetrics**：当前合法 record 不提交，Marshaller `/health=200`、`/ready=503`，内存只保留当前 record，重试/日志有界；Backend readiness 与代表性社交 API 继续工作。
- **恢复同一 VictoriaMetrics project/volume**：常规故障场景不重启 Marshaller，等待 readiness 恢复、原 record 获得 HTTP acceptance、查询可见、offset 提交和后续 record 继续处理；第 3.4 节另以一次明确未提交后的进程重启证明重取。
- **停止 Kafka**：Marshaller health 保持存活、ready 失败并有界重连；Router/Monitor 按 Phase 7 语义退化，Backend/RabbitMQ 业务能力不受 Kafka依赖。
- **恢复同一 Kafka、Topic和 group**：不重启 Router、Monitor 或 Marshaller，确认新消息继续写入/消费且 committed offset 连续。
- **停止/重启 Marshaller**：上游 Monitor/Router/Kafka 继续运行并形成积压；恢复正式 group 后从 committed offset 继续，不按“过去 24 小时”丢弃合法历史消息。
- **有界关闭**：真实链路在空闲和 VM 退避两种代表性状态发送 SIGTERM；处理、Kafka 不可用、revoke/lost 和提交竞态由最终提交上的确定性状态机测试覆盖。两层共同确认停止新 poll、取消请求/退避、只在 ownership 有效时提交已接受结果并在 shutdown timeout 内退出。

### 3.6 内部访问、用户态和业务隔离

- Marshaller `/ready` 分别使用无 token、错 token、普通用户 Cookie、admin Cookie、JWT/query token 和正确内部 Bearer；只有正确内部身份成功。
- VictoriaMetrics 分别使用无/错 Basic、普通/admin Cookie 和正确内部 Basic；只有内部身份可执行验收查询/写入，且端口只绑定 loopback。
- 验证无 Marshaller消息接收、任意查询、offset、重放或管理 HTTP API；Frontend bundle 和响应中没有 Kafka/VM/Marshaller 凭据、URL 或管理接口。
- 使用真实登录验证未登录/普通/admin 既有权限矩阵：普通用户仍可完成社交操作且插件管理 `403`，admin 仍是社交权限超集；公开作者摘要不暴露 role。
- 在 Kafka、Marshaller 和 VM 故障窗口分别执行代表性注册/登录、帖子、评论、点赞、通知与搜索必要流程；MySQL 事实、RabbitMQ 通知/索引和现有安全错误不因可观测链路降级改变。
- 日志与 HTTP 响应不得出现 token、Basic password、Cookie/JWT、broker/VM 连接详情、原始 record、用户内容、服务器绝对路径或底层错误。

### 3.7 生命周期、资源归属与清理

- 对隔离日常流程执行 `dev.sh → verify.sh → down.sh`，核对基础设施/Topic → Router → Marshaller → Monitor 启动，以及 Monitor/Exporter → Marshaller → Router → 其他进程 → Compose 关闭顺序。
- `verify.sh` 必须保持只读：不创建 Topic、消费/提交 record、produce fixture、删除时序、改变 VM 配置、修复 PID或停止资源。
- 正常、坏消息、VM/Kafka/Marshaller 故障、验收失败、脚本中断和信号退出路径都对比前后 Git、PID、端口、project、container、network、volume、Topic/group fixture、插件根和临时凭据文件快照。
- 只清理本批随机 project/目录和强归属 PID；unknown/mismatched resource 安全拒绝并保留有限诊断。不得以名称或默认端口删除日常 Kafka/VM volume。
- `down.sh` 保留日常命名 volumes；隔离验收只在确认随机 project 标签和 volume 归属后删除本次 volumes。

### 3.8 验收失败的最小修复

- 只修复总方案第 15.3 节和本文第 3 节中真实复现、会使 Phase 8/Milestone 2 验收不成立的问题；修复前保存复现命令和有限诊断。
- 修复后只重跑受影响 package、脚本或场景；最终 diff 稳定后执行第 8 节尚未通过的固定门禁。
- 新存储类型、Topic、多 partition 扩展、DLQ/重放管理、聚合、告警、Backend查询、Frontend 页面、VM cluster/vmauth/TLS、容量优化和机会性重构不属于最小修复。
- 如失败来自 Phase 7/6 已发布契约与真实实现冲突，先更新总方案与实施记录并明确兼容决策，不静默放宽 validator 或改写上游字段。

### 3.9 文档、版本、里程碑与远程状态收口

- 更新根、Marshaller、Router、Monitor README 和配置说明，使启动顺序、Topic/group、offset、Envelope、映射、VM 写入/查询、at-least-once、内部身份和限制与真实行为一致。
- 核对总方案、三份拆分方案、三份实施记录、Git 历史、版本和权威分支分配；计划、局部成功或未观察结果不得写成完成。
- 将根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 更新为 `1.5.3`。
- 本地固定门禁通过只记录本地结果；只有 Pull Request 合入主远程且远程门禁实际成功后，才将 Phase 8 与 Milestone 2 标记完成。
- 记录向 Phase 9 交付的 Marshaller扩展边界、metrics handler/VM writer 独立性和并行保持 metrics 链路的回归要求。

## 4. 实施边界与非目标

- 不新增、删除或重命名 Phase 7 Topic、record key/value、Envelope v1、Phase 8 consumer group、指标标签/时间戳或 VM 路径，除非真实验收证明是阻断级契约错误并先更新规划。
- 不实现 Backend Metrics Query API、Frontend 指标页、Dashboard、通用 MetricsQL 代理或浏览器对 VM 的任何访问。
- 不接受 logs/events，不修改 Router 路由表或增加 Topic，不实现 Elasticsearch 可观测写入；这些留给 Phase 9/10。
- 不引入 DLQ、重放/offset 管理 API、Schema Registry、持久去重、Kafka transaction、exactly-once 或跨 record batch。
- 不增加聚合、派生指标、rate/ratio、录制规则、告警、降采样、retention/容量调优、性能/长时压力测试。
- 不部署 VM cluster、vmagent、vmauth、多租户、高可用、TLS、公网入口或 Marshaller 容器镜像。
- 不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 预计文件与交付物

```text
dev/imple/Phase-08/Phase-08-总实施方案.md（仅状态/真实偏差同步）
dev/logs/Phase-08/Phase-08-03-集成验收与里程碑收口.md
README.md
marshaller/README.md
router/README.md（仅交接或阻断修复）
monitor/README.md（仅交接或阻断修复）
.env.example
scripts/verify-marshaller.sh（验收编排或阻断修复）
scripts/dev.sh（仅阻断修复）
scripts/down.sh（仅阻断修复）
scripts/verify.sh（仅阻断修复）
marshaller/**（仅阻断修复）
router/**（仅上游交接阻断修复）
monitor/**（仅上游交接阻断修复）
deploy/compose.yaml（仅阻断修复）
.github/workflows/**（仅门禁阻断修复）
scripts/ci/**（仅治理阻断修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义修改。若固定验收未暴露产品问题，本批只以验收编排/证据、文档、版本和实施记录收口。

## 6. 详细实施步骤

1. 核对 Phase-08-01、Phase-08-02 实施记录、合入提交、远程门禁、当前版本、已知限制以及 Phase 7 最终交接；保存 Git 和日常资源快照。
2. 在最终构建上完成 Marshaller 及直接受影响组件的格式、unit、vet、race、配置/脚本静态门禁；可引用仍有效结果时记录提交和环境。
3. 执行 `verify-marshaller.sh --self-test`，证明 token、URL、Topic/group、查询白名单、PID、project/container/volume、port 和清理目标负向保护有效。
4. 执行第 3.2 节真实 success、target unavailable 和恢复闭环，保存有限 message ID、offset、timestamp 窗口、查询及 Redis/Exporter 对应证据。
5. 执行第 3.3 节映射与永久异常矩阵，证明坏消息不写入、offset 被跳过且后续真实合法消息继续。
6. 执行第 3.4 节真实重复/明确未提交进程恢复，并运行注入 Committer/ownership lease 的确定性状态机测试，证明确定性重放、查询稳定、无越过提交并保留 at-least-once 描述。
7. 执行第 3.5 节 Kafka/VM/Marshaller 故障、同进程/同 group 恢复、积压和四种 shutdown 场景。
8. 执行第 3.6 节内部访问和社交/RabbitMQ/搜索代表回归，确认可观测故障不形成越权或业务依赖。
9. 执行隔离日常生命周期与第 3.7 节全部前后资源快照，确认 verify 只读、down 保留日常 volume且隔离清理无残留。
10. 只对观察到的阻断失败做有限诊断与最小修复；相关代码/配置变化后只重跑受影响项。
11. 最终 diff 稳定后完成第 8 节剩余固定门禁，更新 README、总方案状态、本批实施记录和 `1.5.3` 版本元数据。
12. 提交并创建 Pull Request，查询并记录真实远程 checks 与合入状态；未合入或失败时保持 Phase 8/Milestone 2 未完成。
13. 合入且远程门禁通过后立即停止 Phase 8，把稳定 metrics 路径和 Marshaller扩展边界交给 Phase 9。

## 7. 风险与控制

- **查询命中旧数据形成假通过**：使用随机 VM volume、记录当前 high watermark/message ID 和 Envelope timestamp，查询固定 source/target 的窄窗口。
- **直接 fixture 替代真实链路**：success/up0/recovery 主证据必须来自真实 Redis/Exporter/Monitor/Router；fixture 只注入异常和完全相同重放。
- **坏消息“继续”仅靠日志推断**：同时证明坏 record 不新增时序、其后合法 record 写入并且 committed offset 已越过坏 record。
- **VM 故障期间误提交**：停止 VM 前后记录 group committed offset，恢复前不得推进；恢复后同一 Marshaller PID 完成写入和提交。
- **commit 失败测试误制造数据丢失**：测试边界先确认隔离 group/volume和可恢复 record，禁止修改日常 group offset。
- **去重被误读为 exactly-once**：同时记录原始重复 record、两次处理和单查询点，文档明确 HTTP/commit 不构成事务。
- **Marshaller 积压被时间校验永久丢弃**：停进程形成短时积压并恢复，证明合法历史 timestamp 可写，只拒绝超前异常时间。
- **内部 Basic 变成浏览器凭据**：扫描 Frontend/Backend 配置与响应，执行 Cookie-only 负向请求，VM 只 loopback；后续产品查询必须另建 Backend 管控边界。
- **故障测试误停日常栈**：所有 stop/restart 前校验随机 Compose project label、container ID、端口、volume 和 PID，前后快照必须一致。
- **收口扩张**：只修固定矩阵复现的阻断问题，其他优化记录后停止。
- **虚构里程碑完成**：本地验收、PR、remote checks 和合入分别记录；任一未观察即保持未完成。

## 8. 固定验证命令与必要回归

最终 diff 上按影响执行；代码、配置、依赖或环境未变化且 Phase-08-01 已记录成功的 package 检查可引用，不因收口机械重复。阶段主矩阵、业务回归与治理门禁必须实际完成：

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

`scripts/verify-marshaller.sh` 必须在同一默认执行中覆盖真实 success/up0/recovery、完整指标查询、三类代表性异常继续、真实重复、VM 明确未提交后的进程恢复、Kafka/VM/Marshaller 故障恢复和资源清理。精确 commit/rebalance 竞态由同一最终提交上的注入 Committer/ownership lease 定向测试覆盖；两层证据不得互相替代。`verify-business.sh` 是可观测故障下普通用户/admin、RabbitMQ、搜索和日志必要回归。

完整验收只在 WSL2 Linux filesystem、真实 Kafka/VictoriaMetrics 和强归属隔离资源执行。环境缺失时不得标记完成，也不得用 mock consumer/writer、静态 Envelope、直接 Kafka produce 或直接 VM import 替代主链路。

## 9. 验收标准

- 真实 Redis success、target unavailable 和恢复数据均经过 Exporter → Monitor → Router → Kafka → Marshaller，并可在本次时间窗查询。
- 状态、连接、命令请求、CPU、内存和 keyspace 时序与真实输入对应；metric/labels/value/timestamp 映射固定且无高基数内部标签。
- 永久异常在写入前被拒绝、offset 被安全越过且随后合法消息正常写入；临时存储/Kafka失败、commit 失败和 lost ownership 不提交当前 record，旧 generation 不延迟提交。
- Kafka、VM 和 Marshaller 故障均有界，原组件/正式 group 恢复后可自动继续；积压合法消息不因固定过去时限被丢弃。
- 同一 Envelope 真实重放以及注入的 HTTP acceptance/commit 失败产生确定性时序，1ms dedup 下查询无同毫秒双点；主矩阵前后 `vm_rows_invalid_total` 不增加，系统仍正确标记 at-least-once。
- Marshaller `/ready` 与 VM 只接受内部身份；用户/admin Cookie 无法直连，Frontend/Backend 不泄漏地址、凭据或管理入口。
- 可观测链路故障期间，Backend readiness、普通/admin 社交业务、RabbitMQ 通知/索引和现有授权边界无回归。
- `verify.sh` 保持只读；日常与隔离生命周期顺序正确，不误杀、不误删、不遗留进程、端口、container、network、volume、group fixture、插件根或临时凭据。
- README、配置、总/拆分方案、三份实施记录、Git 历史和远程状态一致。
- 第 8 节固定完成门禁与远程 checks 通过，根和 Frontend 版本均为 `1.5.3`。

## 10. 明确完成条件

只有第 9 节全部满足、Phase-08-03 Pull Request 已合入主远程 `main`、远程固定门禁成功且三份 Phase 8 实施记录真实完整，Phase 8 与 Milestone 2 才完成。任一真实上游、完整查询、异常继续、offset、重复重放、存储/Kafka恢复、业务/访问隔离、资源安全或远程证据缺失时，不得标记完成。

达到完成条件后立即停止。Backend/Frontend 指标查询产品能力、Dashboard、告警、聚合、长期容量、VM cluster、多租户、读写分权、DLQ/重放、logs/events 均进入后续阶段，不继续占用本批。

## 11. Phase 9 交接

- 已在真实链路验证的 Marshaller lifecycle、配置、health/readiness、内部身份、结构化日志、Bash 归属和 CI 模式。
- 正式 `gopulse-marshaller-metrics-v1` group、手动 offset、earliest 初始策略，以及成功/永久无效/暂时失败/重放的处理语义。
- metrics Envelope v1 的独立严格 decoder/validator 与基于 type 的显式 handler 分派边界；Phase 9 新增 logs 时不得放宽 metrics 校验。
- metrics transformer 与 VictoriaMetrics writer 的独立接口；未来 logs transformer/Elasticsearch writer 不得把两种存储失败、offset 或标签/字段契约混合。
- 单节点 VictoriaMetrics 的 loopback、内部 Basic、持久 volume、1ms dedup、写入/查询和 at-least-once 限制。
- Phase 9 必须在新增日志链路同时回归 metrics 正常消费、VM 写入/查询、异常继续和社交业务隔离；不得依赖本批 fixture group、随机凭据或临时查询文件。
