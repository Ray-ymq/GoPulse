# Phase-17-02：Migration 与消息处理可靠性闭环实施方案

> 目标版本：`1.14.2`
> 开发分支：`develop/1.14.2`
> 运行与验收平台：真实 MySQL/RabbitMQ/Kafka/Elasticsearch/VictoriaMetrics；候选级门禁使用 Linux `amd64` Docker server

## 1. 批次目标

本批在 `1.14.1` 统一运行时合同上完成持久状态推进与异步处理的可靠性闭环：数据库只有一个可诊断、可拒绝危险状态的 Migration 入口；RabbitMQ、Kafka 和三源告警在成功、暂态失败、永久异常、重复执行、重连/重平衡和关停中保持明确提交语义。

```text
current schema/data + incoming work
                 │
                 ▼
      bounded ownership / transaction
                 │
      ┌──────────┼──────────┐
      ▼          ▼          ▼
   migrate    Rabbit/Kafka   alert lease
      │          │          │
      └──────────┼──────────┘
                 ▼
  committed once or safely retryable state
```

本批不以增加测试数量为目标；每个状态转换优先一个代表成功、一个代表失败，并只为 distinct business outcome、持久化不变量或已观察故障增加场景。

## 2. 前置条件

- Phase-17-01 已合入主线，`deploy/runtime-contracts.json`（或等价权威文件）、三类 Probe、关停预算、安全日志和错误原因码可用。
- 从最新 `upstream/main` 运行 `scripts/start-development-batch.sh Phase-17-02 --remote upstream` 创建 `develop/1.14.2`。
- Phase 16 的 `1.13.6` release manifest、Linux `amd64` 制品、当前数据配方、backup format v1 与可复核 evidence 可用；迁移输入必须绑定该版本，不能用手工拼接 SQL 冒充前序数据。
- 核对当前 MySQL schema version/dirty 处理、Rabbit business/search topology、Kafka topic/group/ownership、Marshaller target store 和 alert lease/state transaction 的真实实现。
- 准备相互隔离的 MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 和 Compose project；故障注入不得作用于用户或其他项目资源。

## 3. 实施范围

### 3.1 Migration 状态与命令合同

- 保持 `backend/migrations` 内嵌、单调递增文件为唯一 MySQL Schema 来源；不引入 ORM auto-migrate 或服务启动时的隐式 DDL。
- 扩展 Migration CLI 至少支持：
  - `status`：机器可判定输出 binary target、database version 和 `clean/behind/current/dirty/ahead`；
  - `validate`：检查内嵌 up/down 配对、编号连续性、文件唯一性和目标版本；
  - `up`：在数据库 migration lock 下只向前应用并安全重复。
- 输出和退出码稳定：`current/no change` 为成功；invalid config、connection failure、dirty、ahead、lock timeout 和 apply failure 使用不同安全原因码，不打印 DSN、SQL 参数或 Secret。
- 产品 Compose/lifecycle 只运行 `validate + status + up` 的单一 one-shot job；Backend、Worker 和 Indexer 在该 job 成功前不启动，Backend `/ready` 额外校验 Schema 为 binary target。
- 现有 `down` 若为本地开发保留，必须在帮助与产品入口中明确排除；产品恢复/回退继续使用 Phase 16 backup/restore。
- 保留 migration 12 已知专用 resume 兼容逻辑并添加边界测试，但禁止任意 dirty version 自动 force/clear。
- 不为“证明迁移”创建无业务意义的占位 migration；若本批生产改动确需 Schema，使用下一个连续编号、最小 up/down 和对应持久不变量测试。

### 3.2 Migration 数据路径

固定覆盖两条正向路径：

1. 空 MySQL → `validate/status/up` → 当前 target → 重复 `up` 无变化 → 完整产品可启动和写入。
2. 使用 Phase 16 正式入口在 `1.13.6` 候选生成当前业务、角色、插件、告警和审计数据，创建/inspect backup，在隔离 project 恢复 → 使用 `1.14.2` migration job 向前推进 → 事实对照与新增写入成功。

固定拒绝：

- 两个 migration runner 并发时只有一个写者，另一个等待后得到 current 或以 lock timeout 安全失败；
- 任意非 12 的 dirty 状态、未来数据库版本、内嵌编号缺失/重复和损坏输入阻断启动，不修改既有事实；
- migration 中断后只按该 migration 明确的事务/恢复合同继续，不使用通用 force；
- 失败 project 不报告 ready，且可通过 Phase 16 正式 restore 回到验收前快照。

本路径只声明直接前序 `1.13.6` 到当前版本的单跳兼容，不扩展为任意历史升级支持。

### 3.3 RabbitMQ ack、重试与幂等

- Business Worker 和 Search Indexer 继续使用手动 ack、有限 prefetch、persistent message 和各自隔离的 retry/dead topology。
- 业务 side effect 成功或根据稳定 event ID 确认已存在后才 ack；数据库暂态失败不得 ack。
- 暂态失败：先向 retry exchange 发布并收到 publisher confirm，确认可路由后再 ack 原 delivery；publish nack/return/timeout/连接关闭则 nack+requeue 原消息。
- 永久无效、未知 routing 或重试耗尽：先确认 dead publish，再 ack 原消息；poison message 不热循环，body/header 原文不写日志。
- consumer channel/connection 关闭后以 bounded jitter/backoff 重建 topology、QoS、confirm/return channel 和 consumer；恢复事件只记录一次状态转换。
- 关停先停止新 delivery，在共享 deadline 内完成当前处理；完成不了则 requeue。不得在 context cancel 后提交未经确认的 secondary publish。
- 通知 repository 和搜索写入以 event/post version 等既有稳定键保持幂等；重复 delivery、ack 丢失和进程重启不产生重复用户事实，搜索最终与 MySQL 当前状态一致。

### 3.4 Kafka offset、重试与异常消息

- Router 对每个请求使用有界 produce context；只有 broker ack 后返回成功。buffer full、timeout、cancel、unroutable/topic unavailable 和 shutdown 返回稳定错误/ready 状态。
- Marshaller 保持 disable auto-commit。目标写入成功后、partition lease 仍有效时提交对应 record offset；不得批量越过尚未成功的记录。
- VictoriaMetrics/Elasticsearch 暂态失败按 bounded exponential backoff 重试，offset 保持未提交；恢复后写入的时间序列/文档使用既有幂等键容忍重复。
- commit 暂态失败不得静默进入永久 halted。允许在仍持有 lease 时有界重试；超过批次锁定的终止条件后 readiness 失败、主进程非零退出，由 Compose 重建 consumer。
- assigned/revoked/lost partition 更新 ownership；旧 lease 立即取消，旧 owner 在 storage 返回后再次校验 ownership，失去 ownership 时不提交 offset。
- 可识别的无效 envelope、超限、未来时间、未知 type/source 和 transform permanent error 使用固定 reason code/计数并提交跳过，以解除 partition 阻塞；不记录 payload，也不把 storage 暂态失败误分类为永久异常。
- Kafka broker 重启、group rebalance、目标 store 故障、commit 失败、永久异常和 SIGTERM 场景必须分别验证最终 offset、目标事实、退出码和日志。

### 3.5 告警评估恢复与重复抑制

- Metrics、Logs、Events adapter 的 timeout/失败只作用于对应 rule，写入 `unknown/stale` 和固定安全 error code；其他来源继续调度。
- claim 继续使用 `lease_owner + lease_until + rule revision`；apply 在事务中锁定当前 rule/state 并确认 lease/revision/enable 状态，过期 worker 不提交。
- 同一 rule/revision 同时最多一个 active incident；重复轮次只更新 evaluation facts，不重复 trigger audit；恢复只关闭当前 incident 并写一次 recover audit。
- 重启间隙超过连续性阈值时重新开始 pending，不把停机时间当作持续满足 `for`；恢复后从持久化 `next_evaluation_at` 继续。
- Scheduler round/evaluation panic、timeout、DB/source 故障和关停都写安全原因码与有限指标；不能吞掉后留下无期限 lease 或伪 healthy 状态。
- 告警评估 degraded 不改变 Backend social readiness；管理 API/Frontend 显示来源状态并在恢复后回到正常。

### 3.6 统一状态、指标与日志

- 复用 Phase-17-01 的 Probe/日志合同，补充但不另建 Migration、Rabbit、Kafka、alert 的有限状态与指标。
- 至少可观测当前 Schema 状态、consumer session/ownership、in-flight、retrying、last commit/ack/success、permanent reject、alert last success/source error；标签只能使用有限枚举，不能包含 user/message/rule ID。
- 错误日志包含 service/module/event/safe reason、event/topic/partition/offset 等非敏感定位字段；不包含 payload、SQL、DSN、headers 或连接串。
- 状态恢复必须清除 degraded reason 并记录一次 recovery transition，避免 stale 状态永久残留。

### 3.7 集成与故障隔离

- 在同一候选上组合一个代表性社交 outbox → Rabbit → notification/search 和 Monitor → Router → Kafka → Marshaller → VM/ES → alert 闭环。
- 分别停止 RabbitMQ、Kafka、VictoriaMetrics、Elasticsearch 和单一 alert source，验证直接链路 degraded/retry，历史不被破坏，依赖恢复后自动收敛。
- 故障期间注册/登录/发帖/评论/点赞/关注/收藏代表流程仍可执行；普通用户不获得管理数据，超级管理员状态页只显示安全 reason。
- 故障注入前后保存 offset、queue depth、business/search/alert/audit 事实和无关 Docker resource 快照；cleanup 只作用于强归属 project。

## 4. 不在本批范围

- 任意历史版本升级、backup format 变更、在线 schema change 或生产零停机迁移承诺。
- 新消息 broker、Kafka DLT 产品、Rabbit topology 重设计或事件 schema 扩展。
- 新告警来源、外部通知、规则语言或管理页面重构。
- Kubernetes StatefulSet/Job/Probe、生产 HA 或容量/吞吐认证。
- 不由固定失败场景要求的一般 consumer 重写、数据库调优或全仓并发测试。

## 5. 建议实施顺序

1. 固定 Migration CLI/状态/退出码和测试数据对照，先完成 empty/current/拒绝路径。
2. 用现有 Rabbit integration 测试复现 ack/confirm/requeue/关停语义，只修直接差异。
3. 用 Marshaller 测试复现 store/commit/ownership/永久异常和 halted 假健康，完成非零退出与状态收口。
4. 补齐告警 source failure、lease/revision、重启连续性和重复抑制。
5. 通过统一状态/日志接入 Compose 故障场景，确认社交隔离和角色边界。
6. 在最终 diff 上运行固定门禁一次，记录实际结果、更新版本并停止。

## 6. 预计直接影响文件

- `backend/cmd/migrate/`、`backend/migrations/` 和 Migration/Schema readiness 直接代码与测试
- `backend/internal/worker/`、notification/search 幂等边界及 integration tests
- `marshaller/internal/consumer/`、Router Kafka producer/runtime 及直接测试
- `backend/internal/alert/` 的 scheduler/repository 状态与直接测试
- `componentmetrics/` 或运行时合同中的有限状态/指标目录
- `deploy/compose.yaml` 的 migration job、restart/health 直接配置
- `scripts/verify-phase17-state.sh`（或等价）及自测试/evidence schema
- Migration、消息处理、故障恢复和运维文档
- `dev/logs/Phase-17/Phase-17-02-Migration与消息处理可靠性闭环.md`
- `VERSION`、`.env.example` 和双 Frontend 版本元数据

若实现不需要新 Schema，不创建占位 migration；实施记录必须明确 target 未变化及验证过的原因。

## 7. 批次验收标准

### 7.1 Migration

1. `validate/status/up` 输出、退出码和安全日志稳定；空库到 current、重复 up 和 Backend Schema readiness 通过。
2. 正式 `1.13.6` 当前数据推进后，角色、业务、搜索、插件、告警和审计事实一致，且 `1.14.2` 可继续写入。
3. 并发 runner 只有一个写者；dirty/future/损坏输入不改数据、不启动长运行服务、不泄漏 DSN。
4. 失败后可通过正式 backup/restore 回到原状态；不使用 down/force 冒充产品回退。

### 7.2 RabbitMQ 与 Kafka

1. Rabbit success/retry/dead/confirm failure/reconnect/duplicate/shutdown 场景中无静默丢失，notification/search 结果幂等且最终一致。
2. Kafka store-before-commit、storage retry、commit failure、rebalance/lost ownership、permanent skip 和 shutdown 场景的 offset/事实正确。
3. 不可恢复 consumer 终态不再保留 live-but-halted 进程；ready 失败、日志可定位、进程非零退出并由 Compose 有界恢复。
4. poison payload/headers、连接串和 Secret 不进入日志、状态或 evidence。

### 7.3 告警与产品隔离

1. 三源 alert 的正常、unknown/stale、恢复、重复轮次和重启连续性通过，无重复 active incident/trigger/recover audit。
2. 单一来源、Rabbit、Kafka 或观测存储故障不阻断代表性社交闭环，也不放宽 `user/super_admin` 权限。
3. 状态/指标标签有界，恢复清除 degraded；故障注入/cleanup 不影响其他 project 或用户文件。

完成条件：以上全部通过、同名实施记录如实完成、无阻断问题、版本元数据为 `1.14.2`，本批提交已创建。

## 8. 固定验证命令与回归范围

计划新增的候选级入口由本批落地；具体环境参数可由 runner 的私有配置文件提供，凭据不得出现在命令行或日志：

```bash
(cd backend && go test -count=1 ./cmd/migrate ./migrations ./internal/worker/... ./internal/notification ./internal/search ./internal/alert/...)
(cd backend && go test -race -count=1 ./internal/worker/... ./internal/alert/...)
(cd marshaller && go test -count=1 ./internal/consumer/...)
(cd marshaller && go test -race -count=1 ./internal/consumer/...)
(cd router && go test -count=1 ./internal/kafka/... ./internal/httpserver/...)
scripts/verify-phase17-state.sh --from-manifest "$GOPULSE_PHASE16_MANIFEST" --manifest dist/release-manifest.json --work "$GOPULSE_PHASE17_WORK"
scripts/verify-business.sh
scripts/verify-marshaller.sh
scripts/verify-alerts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.14.2 --base-ref upstream/main
git diff --check
```

`verify-phase17-state.sh` 固定覆盖直接前序 Migration、Rabbit/Kafka 故障/恢复、alert restart/dedup、社交隔离、Secret scan、资源归属和结构化 receipt。它应复用已有 `verify-business`、`verify-marshaller`、`verify-alerts` 的真实能力；同一候选同一场景通过后不由子命令重复执行，实施记录列明 receipt 复用关系。

只有在本批修改 Phase-17-01 的共享 runtime/Compose/Frontend 文件时，才追加对应直接测试或 `scripts/verify-compose.sh`，并在实施记录写明共享基础设施风险；否则不重跑 Phase-17-01 完整门禁。Kubernetes、跨架构、性能和一般依赖审计不在本批门禁。

## 9. 实施记录与 Phase-17-03 交接

完成前创建 `dev/logs/Phase-17/Phase-17-02-Migration与消息处理可靠性闭环.md`，至少记录：

- Schema binary/database from/to、migration 文件集合、直接前序数据来源和事实对照；
- Rabbit queue/retry/dead/ack 与 Kafka topic/group/partition/offset/ownership 的实际结果；
- alert rule/incident/audit 在失败、恢复、重复和重启后的摘要；
- 每条命令、候选/receipt、失败轮次、最小修复、Secret/ownership/cleanup 结果；
- 是否新增 Schema、与计划偏差、已知限制和非阻断后续项。

交给 Phase-17-03 的固定输入是已合入主线的 `1.14.2` Migration、Rabbit/Kafka/告警可靠性合同及真实状态 evidence。达到条件后停止；完整产品矩阵和 Milestone 4 声明只能由 Phase-17-03 完成。
