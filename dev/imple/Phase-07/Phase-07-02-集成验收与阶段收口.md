# Phase 7-02：集成验收与阶段收口实施方案

> 权威目标版本与开发分支以 `Phase-07-总实施方案.md` 第 3.2 节为准：本批对应 `1.4.2` / `develop/1.4.2`。
>
> 当前状态：本地实现与固定验收完成，待远程门禁和合入。

## 1. 批次目标

基于已合入主远程 `main` 的 Phase-07-01 最终实现，在干净隔离资源和同一最终构建上执行 Phase 7 固定端到端验收、必要回归、文档、版本、实施记录和远程状态收口，证明：

```text
真实 Redis → Exporter → MetricsMonitor → Message Router
          → Kafka → Consumer 完整消息
```

以及：

```text
Kafka/Router 故障 → 可观测发布有界失败
                  → 普通用户社交业务、管理员授权、RabbitMQ 任务保持原契约
```

本批不引入新产品能力；只有固定矩阵暴露的阻断问题可以最小修复。

## 2. 前置条件

- Phase-07-01 已合入主远程 `main`，远程 Router、Monitor、脚本/Compose 等门禁成功，实施记录与真实提交一致。
- 主远程根与 Frontend 版本均为 `1.4.1`；Router Endpoint、Topic、record、错误和失败语义与总方案一致。
- 从最新主远程 `main` 创建总方案分配的本批分支，不沿用 Phase-07-01 分支。
- WSL2 Linux filesystem、Docker daemon、端口、Compose project、volume、`.run` 进程和插件根可建立强归属隔离。
- 开始前保存工作区和日常资源快照；若存在用户资源，改用独立临时仓库/目录和随机端口，不触碰原资源。

## 3. 实施范围

### 3.1 最终构建与契约核对

- 对照 Phase-07-01 实施记录、合入提交和远程 checks，核对 Router module、franz-go 版本、Kafka 镜像、Topic、配置和脚本真实状态。
- 确认 Monitor Publisher 仍使用固定 HTTP/Bearer/Idempotency-Key/202 契约，且 `monitor/go.mod` 没有 Kafka SDK。
- 确认 Router 只含 `metrics → gopulse-observability-v1`，不含清洗、存储、Marshaller、logs/events 或 RabbitMQ 业务路由。
- 确认 root/Frontend 版本、Compose 渲染、Bash 语法和分支治理基线一致。

### 3.2 真实成功与消息完整性闭环

- 启动隔离 Kafka、Redis、Backend、Router、Monitor 和真实 Redis Exporter 插件，显式确认 Topic 已创建。
- 通过真实 Redis 状态变化产生新的 success Envelope，记录 Router 接收关联 ID和 Consumer 读取的 key/value。
- 逐 byte 比较 Kafka value 与 Router 收到的 HTTP body；解析后同时核对 schema、message ID、type/source、timestamp、target、scrape status 和实际 Redis sample。
- 停止 Redis，在同一 Monitor/Exporter/Router 进程下读取合法 `target_unavailable` Envelope，证明 Router 不误删或改写故障消息。
- Consumer 只在本次测试确定的 offset 范围读取并以 message ID 关联，不能把 Topic 中旧消息误当成当次结果。

### 3.3 身份、输入和非写入负向矩阵

- 分别使用无 Authorization、错误 Router token、普通用户 Cookie、管理员 Cookie 和混合 Cookie/Bearer 请求 Router，只有正确服务 token 可继续处理。
- 覆盖错误 Content-Type、Content-Encoding、空/超限 body、无效 UTF-8/JSON、重复/未知/缺失顶层字段、尾随 token、非法 schema/message ID/timestamp/payload、Idempotency-Key 缺失/重复/不匹配以及 logs/events 类型。
- 为每个代表性拒绝记录发布前后 Topic offset/消息 ID 集合，证明无目标 record 写入，而不是只检查 HTTP 状态。
- 响应与结构化日志中不得出现 token、Cookie/JWT、原始 body、broker 地址、内部 URL、文件路径或底层 Kafka 错误。

### 3.4 Kafka/Router 故障恢复与业务隔离

- 停止 Kafka broker：Router `/health` 保持 `200`，鉴权 `/ready` 为 `503`，发布在固定 timeout 内非 `202`；无无限 goroutine、buffer 或 shutdown 等待。
- Kafka 故障期间确认 MetricsMonitor 继续 scrape 并记录有限 publish failure；Exporter 状态、Backend readiness 和普通用户代表性社交 API 保持可用。
- 恢复同一 Kafka project、volume 和 Topic，不重启 Router/Monitor；等待 readiness 恢复并验证新 metrics 消息进入 Kafka。
- 有界关闭/重启 Router，确认 Monitor 只按 Phase 6 语义丢弃当次失败消息、继续后续周期，不阻断 Backend。
- 使用真实登录分别验证普通用户可执行代表性社交操作、admin 仍是权限超集、普通用户插件管理仍 `403`、公开作者摘要不暴露 role。
- 确认通知/索引等 RabbitMQ 路径仍使用 RabbitMQ，Kafka 停止不改变其配置或权限边界。

### 3.5 生命周期、资源和清理验收

- 执行隔离 `dev.sh → verify.sh → down.sh`，核对 Kafka → Router → Monitor 启动和 Monitor → Router → Kafka 关闭顺序。
- 正常、Kafka 故障、Router 故障、脚本中断和验收失败路径都对比前后进程、端口、container、network、volume、Topic、plugin root 和临时文件快照。
- `verify.sh` 保持只读，不创建 Topic、不消费 record、不修复 PID 或停止资源。
- 所有清理只作用于随机 project/目录和强归属 PID；未知资源或归属不匹配时安全拒绝并保留诊断。

### 3.6 验收失败的最小修复

- 只修复总方案阶段矩阵和本文第 3 节中已复现、会使 Phase 7 验收不成立的问题；修复前保存复现命令和有限诊断。
- 修复后只重跑受影响的 package/脚本场景；最终 diff 稳定后执行第 8 节尚未通过的固定门禁。
- 新类型、Topic、持久去重、Schema Registry、死信/重放、SASL/TLS、多 broker、Marshaller、VictoriaMetrics 或可观测页面不属于最小修复。
- 如失败来自 Phase 6 已发布契约与真实实现冲突，先更新总方案并记录兼容决策，不静默改写上游语义。

### 3.7 文档、版本与远程状态收口

- 更新根 README、Router/Monitor README、配置和必要运行说明，使启动顺序、Endpoint、Topic、record、失败/重复、内部身份和限制与真实行为一致。
- 核对总方案、两份拆分方案、两份实施记录、Git 历史、版本和权威分支分配；不把计划或未观察结果写为已完成。
- 将根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 更新为 `1.4.2`。
- 本地门禁通过只记录本地结果；只有 Pull Request 已合入且远程门禁实际成功后，才把 Phase 7 标记为完成。

## 4. 实施边界与非目标

- 不新增、删除或重命名 Endpoint、Header、Envelope 顶层字段、消息类型、Topic、record key/value 或错误码，除非已证明是阻断级契约错误。
- 不实现 Marshaller、VictoriaMetrics、logs/events、字段转换、异常过滤、存储或查询。
- 不增加多 Topic、DLQ、重放、持久队列、应用级去重、exactly-once、SASL/TLS 或生产 Kafka 拓扑。
- 不进行长时压测、消息大小/故障全排列、一般代码审查、依赖审计、覆盖率活动或机会性重构。
- 不新增 Router 容器镜像，不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 预计文件与交付物

```text
dev/imple/Phase-07/Phase-07-总实施方案.md
dev/logs/Phase-07/Phase-07-02-集成验收与阶段收口.md
README.md
router/README.md
monitor/README.md
.env.example
scripts/verify-router.sh（仅验收编排或阻断修复）
scripts/dev.sh（仅阻断修复）
scripts/down.sh（仅阻断修复）
scripts/verify.sh（仅阻断修复）
router/**（仅阻断修复）
monitor/**（仅阻断修复）
deploy/compose.yaml（仅阻断修复）
.github/workflows/**（仅门禁阻断修复）
scripts/ci/**（仅治理阻断修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义修改。如验收未暴露产品问题，本批只以验收证据、文档、版本和实施记录收口。

## 6. 详细实施步骤

1. 核对 Phase-07-01 实施记录、合入提交、远程门禁、当前版本、已知限制和日常资源快照。
2. 在最终构建上完成 Router/Monitor 直接受影响的格式、unit、vet、race 和配置/脚本静态门禁；引用仍有效的成功结果时记录其提交与环境。
3. 执行 `verify-router.sh --self-test`，证明 token、PID、project/container/volume、port、Topic 和清理目标负向保护有效。
4. 执行第 3.2 节真实 success/target unavailable 纵向闭环，保存 message ID、offset 范围、key 和 value 完整性证据。
5. 执行第 3.3 节身份/输入拒绝矩阵，证明拒绝请求未写 Kafka且错误/日志安全。
6. 执行第 3.4 节 Kafka/Router 故障、原进程恢复、Monitor 连续采集和代表性社交/RabbitMQ 隔离验证。
7. 执行隔离日常生命周期和第 3.5 节前后资源快照，确认无双重启动、误杀、误删或残留。
8. 仅对观察到的阻断失败做有限诊断和最小修复；相关代码/配置变化后只重跑受影响项。
9. 最终 diff 稳定后完成第 8 节剩余固定门禁，更新 README、总方案状态、本批实施记录和 `1.4.2` 版本元数据。
10. 提交并创建 Pull Request，查询并记录真实远程 checks 与合入状态；未合入或失败时保持 Phase 7 未完成。
11. 合入和远程门禁通过后立即停止 Phase 7，将 Topic/record/Envelope/重复语义交给 Phase 8。

## 7. 风险与控制

- **消费旧消息形成假通过**：验收先记录 high watermark/offset 起点，只在本次范围按 message ID 关联。
- **语义相等掩盖字节改写**：同时执行逐 byte 对比和 JSON 业务字段核对，不只比较反序列化对象。
- **HTTP 拒绝但 Producer 已被调用**：以 Topic 前后 offset/ID 集合证明不写，而不是只检查状态码。
- **Kafka 停止同时误停业务 Compose**：使用独立隔离 project；业务隔离验证所需依赖单独确认归属，停止前保存 ID。
- **恢复实际依赖 Router 重启**：记录 Router/Monitor PID，Kafka 恢复前后必须一致。
- **Monitor publish failure 被误当 scrape failure**：分别核对 `last_scrape_at`、安全错误和新消息恢复，不要求补发历史。
- **管理员 Cookie 被误当内部身份**：直接构造 Cookie-only 请求并确认 `401`，服务 token 与用户会话严格分离。
- **收口扩张**：只有固定矩阵真实复现的阻断问题可修复，其他改进记录后停止。
- **虚构远程完成**：本地验收、PR、checks 和合入状态分别记录，未观察结果保持未完成。

## 8. 固定验证命令与必要回归

最终 diff 上按影响执行；代码、配置、依赖或环境未变化且 Phase-07-01 已记录成功的 package 检查可引用，不因收口机械重复。阶段主矩阵和治理门禁必须实际完成：

```bash
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
python3 scripts/ci/validate_branch.py --branch develop/1.4.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-router.sh --self-test
scripts/verify-router.sh
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

`verify-router.sh` 必须在同一默认执行中覆盖真实 Monitor 输入、Consumer 原始 bytes、非法请求不写、Kafka 停止/恢复和资源清理。`verify-business.sh` 是可观测故障下普通用户/管理员、RabbitMQ、搜索和日志必要回归，不得用 Router 单元测试替代。

完整验收只在 WSL2 Linux filesystem 和强归属隔离资源执行。环境缺失时不得标记完成，也不得用 mock broker、fixture Envelope、直接 Kafka CLI produce 或源码审查替代。

## 9. 验收标准

- 真实 Redis success 和 target unavailable Envelope 均经 Monitor → Router → Kafka 被 Consumer 在本次 offset 范围读取。
- record key 等于 `message_id`，value 与 Router 接收 body 逐 byte 一致，Router 未改写任何业务字段。
- 未授权、Cookie-only、非法/超限/未支持消息全部安全拒绝并有 Kafka 非写入证据。
- Kafka 停止时 Router health、readiness、发布超时和错误符合契约；Monitor 继续 scrape，Backend 业务和授权边界无回归。
- Kafka 恢复后不重启 Router/Monitor即可传输新消息，不要求补发历史失败消息。
- Monitor 无 Kafka SDK/Topic，Router 无清洗/存储，RabbitMQ 继续只承载业务任务。
- 日常与隔离生命周期顺序正确，不误杀、不误删、不遗留进程、端口、container、network、volume、plugin root 或临时 Consumer 资源。
- README、配置、总/拆分方案、两份实施记录、Git 历史和远程状态一致。
- 第 8 节固定完成门禁与远程 checks 通过，根和 Frontend 版本均为 `1.4.2`。

## 10. 明确完成条件

只有第 9 节全部满足、Phase-07-02 Pull Request 已合入主远程 `main`、远程门禁成功且两份 Phase 7 实施记录真实完整，Phase 7 才完成。任一真实上游输入、Consumer 完整性、非写入、Kafka 恢复、业务隔离、资源安全或远程证据缺失时，不得标记完成。

达到完成条件后立即停止。任何 Topic 扩展、持久去重、重放、SASL/TLS、多 broker、Marshaller、VictoriaMetrics、日志/事件链路或可观测前端均进入后续 Phase，不继续占用本批。

## 11. Phase 8 交接

- 可稳定消费的 `gopulse-observability-v1` 及显式创建/就绪契约。
- key=`message_id`、value=原始 Envelope v1 bytes，业务时间以 Envelope `timestamp` 为准。
- 当前合法 `type=metrics`、`source=redis`，success/target unavailable 均保留 Phase 6 payload 语义。
- Router `202` 代表已确认写入；超时存在不确定结果，相同 message ID 可能重复，Phase 8 Consumer 必须据此设计幂等或重复处理。
- Kafka/Router 故障不会阻断社交业务，Consumer/Marshaller/存储继续保持内部网络和独立服务身份。
- Phase 8 建立正式 consumer group、offset 提交、异常消息处理、字段转换和 VictoriaMetrics 写入；不得把 Phase 7 验收 Consumer 当作产品 Marshaller。
