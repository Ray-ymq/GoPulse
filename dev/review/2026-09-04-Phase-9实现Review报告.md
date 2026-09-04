# GoPulse Phase 9 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-04 |
| 用户指定权威 Review 分支 | `develop/1.6.4` |
| Review 分支创建方式 | fetch 后远端不存在 `origin/develop/1.6.4`；从最新 `origin/main` 创建本地 `develop/1.6.4`，未推送 |
| Review 基线 | `a01db909d7efca689e3d94e35a94c739b54fb6da`（PR #86，Review 开始时与 `origin/main` 一致） |
| Phase 9 计划合入点 | `ef8e4b46eec61d4012dcf7b577294f0d21ce8299`（PR #80） |
| Phase 9 已合入实现提交 | `66ccb2e`（Phase-09-01 / PR #81）、`4e04b00`（Phase-09-02 / PR #83）、`cff5098`（Phase-09-03 / PR #85） |
| Phase 9 完成记录 | `95105c4`，随后通过 PR #86 合入为 `a01db90` |
| 当前完成版本 | 根 `VERSION`、Frontend `package.json` 与 lockfile 均为 `1.6.3`；本次只新增 Review 文档，不修改版本 |
| `develop/1.6.4` 治理状态 | Phase 9 总实施方案只分配到 `1.6.3`；`validate_branch.py` 对 `develop/1.6.4` 返回 1，当前分支尚不能按仓库规则推送 |
| 实际执行环境 | WSL2 Linux filesystem `/home/ray/GoPulse`，Go `1.26.7`，Node.js `24.20.0`，npm `11.19.0`，Docker `29.7.2` / Compose `v5.5.0`，Python `3.12.3` |
| Review 范围 | Phase 9 总/拆分方案与实施记录、Backend/后台进程日志 shipper、LogMonitor、Router、Marshaller logs handler、Elasticsearch 日志写入、管理员查询、生命周期与验收脚本、版本和分支治理 |
| Phase 9 变更规模 | 相对 Phase 8 整改完成基线 `ce72a8e` 至 Phase 9 验收提交 `cff5098`：67 个文件，4875 行新增、130 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. stdout 优先的日志远程副本是否在并发写入、队列满、远端故障、重试和进程关闭期间满足已声明的非阻塞与有界 drain 契约。
2. LogMonitor → Router → Kafka → Marshaller → Elasticsearch 是否维持严格字段、固定路由、幂等 `_id`、ownership fencing 和“存储成功后提交 offset”的可靠性边界。
3. Elasticsearch template、严格 mapping、日志 alias 与帖子搜索索引是否在运行期故障和恢复后仍保持隔离且不静默丢失可查询日志。
4. 管理员查询是否维持 `401/403/admin` 授权边界、固定 alias、签名 PIT 游标、白名单返回和受约束查询合同。
5. Phase 9 的实施记录、验证证据、版本及 `develop/1.6.4` 治理是否足以支持后续整改批次。

Review 没有暂存或提交工作区内与本任务无关的 `使用指南.md`、Phase 7 日志目录变动或其他用户改动。

## 2. 总体结论

Phase 9 已形成方向正确、主体可运行且经过真实隔离环境验证的日志闭环：

- Backend、Business Worker、Search Indexer 与 Search Reindex 继续首先输出 Schema v1 JSON stdout 日志，并通过有界内存队列异步复制到 LogMonitor。
- LogMonitor 使用独立 Bearer 身份，对固定 service/module/message 与安全字段做第一次清洗，再封装 logs Envelope；Router 保持原始 Envelope bytes并写入既有单 Topic。
- Marshaller 共用既有 ownership/commit 状态机，对 metrics 与四类 logs source 显式分派；日志文档以 `message_id` 作为 `_id` 幂等写入按 UTC 日期划分的独立索引。
- Backend 管理员 API 通过现有 Cookie 身份与数据库实时 admin 角色授权，只读固定 alias，使用 HMAC 签名 PIT 游标，并返回 canonical 白名单字段。
- 本次重新执行完整 `scripts/verify-logs.sh`，真实请求日志、后台日志、8 页/16 条 PIT 分页、权限、敏感信息、故障恢复、幂等重放、Metrics 共存、索引隔离及资源清理均通过。
- 四个 Go module 的 unit test 与 vet 全部通过；直接受影响包的 race test、脚本语法/self-test、版本校验和 CI Python 测试均通过。

但是，当前实现仍不能通过 Phase 9 Review：

1. **日志 shipper 的 `Enqueue` 与 `Close` 没有建立线性化边界。** 并发关闭时，`Enqueue` 可以在观察到 `closed=false` 后被暂停；`Close` 随后排空空队列并结束 worker；原 `Enqueue` 再把记录写入无人消费的缓冲队列并返回 `true`。调用方会认为该远程副本已被接受，但它不会被发送，直接破坏“已接受队列项在关闭期限内 drain”的合同。本次使用临时定向测试稳定复现了该问题。
2. **Marshaller 只在进程生命周期内第一次日志写入前确保 template。** `ready=true` 后不再检查 template，`Ready` 也只检查 cluster health。若 Elasticsearch 集群在 Marshaller 不重启时被替换、重置或 template 被删除，当前允许自动建索引的环境会在新日期/新索引第一次写入时创建没有 strict mapping 和 read alias 的索引；写入返回成功后 offset 仍会提交，但管理员查询看不到这些日志。
3. **源端故障状态日志没有按方案节流，重试也没有 jitter。** 队列满时每条新日志都会同步追加一条 stdout warning；持续故障下这会把原业务日志量放大为额外日志风暴。所有实例还使用完全相同的确定性指数退避，恢复时可能同步冲击 LogMonitor。
4. **`develop/1.6.4` 没有权威批次分配。** Phase 9 总实施方案只声明 `develop/1.6.1`～`develop/1.6.3`，分支校验明确失败，当前 Review/整改分支不能直接推送。
5. **查询参数没有完全落实“已知词汇”限制。** `module`、`message` 和 `error_code` 仅接受通用安全 token/字符串，没有按 Phase 9 受版本控制词汇表约束。当前实现不存在 DSL 注入，但公开查询合同比方案更宽，且与低基数、可预测查询边界不一致。

本次记录 **1 项 P1、3 项 P2、1 项 P3**。P1-01 会在正常进程关闭与并发日志写入交错时确认接受但实际丢失日志，是 Phase 9 可靠关闭主合同的阻断项，因此总体结论为 **Fail**。

## 3. Findings 汇总

| ID | 级别 | 位置 | 结论 |
| --- | --- | --- | --- |
| P1-01 | P1 | `backend/internal/observability/logship/shipper.go:76-111,115-133` | `Enqueue`/`Close` 竞态可让记录返回已接受但永不投递 |
| P2-01 | P2 | `marshaller/internal/elasticsearch/client.go:52-64,84-113` | template 一次性缓存导致运行期集群/template 丢失后可能写入无 alias、非 strict 索引并提交 offset |
| P2-02 | P2 | `backend/internal/observability/logship/shipper.go:86-91,138-175` | queue-full 状态无节流且重试无 jitter，故障期间会放大 stdout 日志并形成同步重试 |
| P2-03 | P2 | `dev/imple/Phase-09/Phase-09-总实施方案.md:54-63` | `develop/1.6.4` 未映射到权威批次，分支治理校验失败 |
| P3-01 | P3 | `backend/internal/logquery/logquery.go:169-195` | 查询 `module/message/error_code` 未按已知词汇表收敛 |

## 4. 详细 Findings

### P1-01：`Enqueue` 与 `Close` 竞态可确认接受但丢失日志

**位置**

- `backend/internal/observability/logship/shipper.go:76-92`
- `backend/internal/observability/logship/shipper.go:95-111`
- `backend/internal/observability/logship/shipper.go:115-133`

**问题**

`Enqueue` 先独立读取 `closed`，随后生成 ID、复制 body，最后向带缓冲 channel 非阻塞发送。`Close` 则设置 `closed=true`、关闭 `closing`，worker 收到关闭信号后只排空当时可见的队列并退出。两条路径之间没有 mutex、单 owner command channel 或其他线性化机制。

因此存在如下合法交错：

1. `Enqueue` 读取 `closed=false`。
2. `Enqueue` 在随机 ID 或 body copy 阶段被暂停。
3. `Close` 设置 `closed=true`，worker 看到空队列后退出并关闭 `done`。
4. 原 `Enqueue` 恢复，成功写入仍有容量的 `queue`，返回 `true`。
5. worker 已退出，该记录永远不会投递。

**实际证据**

本次创建了未提交的临时定向测试：使用 128 MiB body 扩大 `closed` 检查与 channel send 之间的窗口，同时调用 `Close`。测试在约 0.09 秒内失败：

```text
--- FAIL: TestReviewEnqueueCloseRaceCanAcceptUndeliveredItem
    accepted item was not delivered
```

临时测试文件随后已删除，未进入最终 diff。

**影响**

- 正常 SIGTERM、一次性 `search-reindex` 退出或后台进程结束时，最后一批并发日志可能被确认进入远程队列却实际丢失。
- `Runtime.Close` 成功返回不能证明所有返回 `Enqueue=true` 的记录已发送或已按永久拒绝处理。
- 现有 race detector 不会报告该问题，因为它是通道/原子操作构成的逻辑竞态，而不是未同步内存访问。

**建议整改**

- 为“接受新项”和“开始关闭”建立单一线性化点；可由 mutex 保护 closed + enqueue，或让单 worker 通过 command channel 同时拥有 enqueue/close 状态。
- 保证 `Enqueue=true` 的语义为：该项已经进入关闭 drain 必然可见的集合；关闭线性化之后的 enqueue 必须返回 `false`。
- 增加确定性的并发测试，覆盖 enqueue 已开始但尚未发布到队列时的关闭，以及 queue 满/空两种交错。

### P2-01：template 只确保一次，运行期状态丢失后可产生不可查询索引

**位置**

- `marshaller/internal/elasticsearch/client.go:52-64`
- `marshaller/internal/elasticsearch/client.go:84-113`

**问题**

`ensureTemplate` 在第一次成功 PUT 后把进程内 `ready` 永久设为 `true`。后续 `Write` 不再访问 template API；`Ready` 只调用 `/_cluster/health`，不会验证固定 template、strict mapping 或 read alias。

当 Elasticsearch 在 Marshaller 不重启的情况下被替换为新集群、恢复为空状态或 template 被删除时：

- `ready` 仍为 `true`；
- `/ready` 仍可能返回成功；
- 当前允许自动建索引的部署会接受 `PUT /gopulse-logs-v1-YYYY.MM.DD/_doc/<id>`，但新索引没有 `dynamic: strict` 和 `gopulse-logs-v1-read`；
- writer 验证 `_index/_id/result` 后返回成功，Processor 随即提交 Kafka offset；
- 日志已从正式消费链路移除，却不能从固定 alias 查询。

这与总实施方案“template 缺失时由 Marshaller 幂等确保；template/mapping 不兼容不得提交 offset”的合同不一致。

**建议整改**

- 不要用永久进程布尔值代表外部集群状态；至少在集群身份/连接恢复后、每个新日志日期首次写入前或受控周期内重新验证固定 template。
- `Ready` 应验证 template 存在且关键 index pattern、strict mapping、alias 与固定合同兼容，而不只是 cluster health。
- 在文档写入成功后，确保目标索引确实附带固定 alias 和兼容 mapping，再允许 commit；或在部署侧禁用非白名单自动建索引并显式创建目标索引。
- 增加“Marshaller 进程保持运行、Elasticsearch 更换为空集群”的恢复测试，证明 template 会重建且该记录最终可通过 read alias 查询。

### P2-02：queue-full 状态无节流，退避无 jitter

**位置**

- `backend/internal/observability/logship/shipper.go:86-91`
- `backend/internal/observability/logship/shipper.go:138-175`
- 对照 `dev/imple/Phase-09/Phase-09-总实施方案.md:144-155`

**问题**

- 每次 queue send 失败都会立即输出 `log remote copy dropped / queue_full`，没有状态去重、时间节流或采样。
- retry delay 只做确定性的 `delay *= 2`，没有方案要求的 jitter。

远端故障时，单 worker 被队首记录占用，队列很快填满。之后每一条业务日志除原 stdout 记录外，还会同步生成一条 queue-full warning；高日志量会被进一步放大。多个进程采用相同 retry 配置时，又会在相近时间同步重试和恢复。

**建议整改**

- 对 queue-full 使用状态转换或时间窗口节流，例如首次、周期摘要和恢复三类有限日志，而不是逐条 warning。
- 在 `[retryMin,retryMax]` 约束内加入可测试的随机 jitter，并允许测试注入随机源。
- 增加故障持续期间的定向测试，证明 N 次 queue-full 不会产生 N 条状态日志，并验证恢复后状态可重新报告。

### P2-03：`develop/1.6.4` 没有权威批次分配

**位置**

- `dev/imple/Phase-09/Phase-09-总实施方案.md:54-63`

**问题**

Phase 9 权威分配表只包含：

- Phase-09-01 → `1.6.1` / `develop/1.6.1`
- Phase-09-02 → `1.6.2` / `develop/1.6.2`
- Phase-09-03 → `1.6.3` / `develop/1.6.3`

本次用户指定的 `develop/1.6.4` 在表中不存在。实际执行：

```text
python3 scripts/ci/validate_branch.py --branch develop/1.6.4 --base-ref origin/main
ERROR: develop/1.6.4 must map to exactly one authoritative allocation; found 0
```

**影响**

- 当前本地 Review 分支可以承载报告，但不满足推送前分支治理门禁。
- 后续整改提交无法在不修改权威计划的情况下合法推送或创建普通开发 PR。

**建议整改**

在开始代码整改前，把 Review 整改作为明确批次加入 Phase 9 总实施方案，例如 Phase-09-04 → `1.6.4` / `develop/1.6.4`，并补充该批次验收条件。仅新增本 Review 文档不修改 `VERSION`；真正完成整改批次时再按规则把版本更新到 `1.6.4`。

### P3-01：管理员查询未完全限制到已知词汇

**位置**

- `backend/internal/logquery/logquery.go:169-195`
- 对照 `dev/imple/Phase-09/Phase-09-总实施方案.md:284-297`

**问题**

`ParseOptions` 对 `service` 和 `level` 使用明确枚举，但：

- `module` 只使用通用 `filterTokenPattern`；
- `message` 只限制长度和少数字符；
- `error_code` 只使用通用 token pattern。

方案要求这些字段使用精确匹配与已知词汇。当前实现仍使用服务器生成的 term query，不存在原始 DSL、通配符或脚本注入，但公开输入集合比计划更宽。

**建议整改**

- 复用或提取与 Schema v1 validator 一致的受版本控制词汇表，至少约束 `module` 与 `message`；对确实允许开放安全 token 的字段，应在总方案中明确调整合同。
- 添加一组代表性查询解析测试：一个合法词汇成功，一个未知 module/message 返回 `400 validation_failed`。

## 5. 已通过的关键检查

### 5.1 代码与静态检查

以下命令本次实际执行并通过：

```text
(cd backend && go test ./...)
(cd router && go test ./...)
(cd monitor && go test ./...)
(cd marshaller && go test ./...)

(cd backend && go vet ./...)
(cd router && go vet ./...)
(cd monitor && go vet ./...)
(cd marshaller && go vet ./...)
```

直接受影响范围的 race 检查通过：

```text
backend: logquery、logship、http、business-worker、search-indexer、search-reindex
monitor: logs、httpserver
router: envelope、httpserver、kafka
marshaller: envelope、logs、elasticsearch、consumer
```

这些成功结果不否定 P1-01；该问题是关闭协议的逻辑竞态，需要专门构造交错才能暴露。

### 5.2 脚本、版本与治理检查

以下检查通过：

```text
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-logs.sh \
  scripts/verify-router.sh scripts/verify-monitor.sh scripts/verify-marshaller.sh
bash scripts/verify-logs.sh --self-test
python3 scripts/ci/validate_versions.py
python3 scripts/ci/test_auto_pr_workflow.py
git diff --check
```

结果：

- `verify-logs --self-test` 通过。
- 版本元数据一致，均为 `1.6.3`。
- CI Python 测试 4 项通过。
- `develop/1.6.4` 分支校验单独执行并按 P2-03 所述失败。

### 5.3 真实隔离验收

本次重新执行：

```text
bash scripts/verify-logs.sh
```

结果通过，包括：

- 真实 Backend 请求成功/失败日志关联。
- Business Worker、Search Indexer、Search Reindex 日志链路。
- 管理员精确过滤与 8 页/16 条 PIT 分页。
- 未登录 `401`、普通用户 `403`、Elasticsearch 故障 `503`。
- 永久坏消息继续、Elasticsearch 停止/恢复、同 ID 重放唯一文档。
- 日志 template、strict mapping、read alias 与帖子搜索 alias 隔离。
- Metrics/Logs 共存、业务状态不受日志故障影响、隔离资源清理。

该验收覆盖正常链路和既定故障矩阵，但没有覆盖 P1-01 的 enqueue/close 精确交错，也没有模拟 P2-01 的“Marshaller 不重启而 Elasticsearch 被替换为空集群”。

## 6. Review 通过项

除 Findings 外，本次确认以下设计和实现符合 Phase 9 主方向：

1. Router 对 logs/metrics 使用固定 Topic，保持原始 Envelope bytes，不接受客户端 Topic 或任意路由。
2. Marshaller Decoder 保持顶层唯一 JSON、大小、UTF-8、key/message ID、schema/type/source/timestamp 与 future skew 校验；metrics 与 logs handler 显式隔离。
3. LogMonitor 与 Marshaller 对日志 payload 执行两次严格校验，未知字段、重复 key、嵌套/null、敏感哨兵和 source/timestamp 不一致均被拒绝。
4. Elasticsearch 日志写入固定 index prefix、template 名、read alias 与 `_id=message_id`，正常运行时重放不会产生第二份文档。
5. Processor 在 writer 成功且 ownership 有效后提交；永久无效记录安全跳过，暂时存储失败保留 offset 并重试。
6. Backend 日志路由位于 Authentication 与数据库实时 Admin Authorization 之后，拒绝请求不触达日志 repository。
7. 查询只读固定日志 alias，PIT cursor 使用独立 domain 派生 HMAC key，绑定过滤条件、limit、PIT、过期时间和 search-after。
8. 查询返回 DTO 不暴露 `_index`、`_id`、PIT、Kafka metadata、内部 URL、底层错误或凭据。
9. stdout 是所有进程日志的第一输出，远程 sink 故障不改变 HTTP、RabbitMQ 或 reindex 的业务结果。
10. Phase 9 三份实施记录与已合入提交、版本及远程 PR 状态总体一致。

## 7. 建议整改顺序与完成条件

建议建立 Phase-09-04 / `1.6.4` Review 整改批次，并按以下顺序处理：

1. **先修复 P1-01**：定义 Enqueue/Close 的线性化语义，加入确定性并发回归测试。
2. **修复 P2-01**：让 template/alias/mapping 的外部状态可重新验证，加入 Elasticsearch 空集群替换恢复测试。
3. **修复 P2-02**：为 queue-full 状态增加节流，为指数退避增加 jitter，并添加最小代表测试。
4. **修复 P2-03**：在 Phase 9 总实施方案增加 `1.6.4` 权威分配，使分支校验通过。
5. **关闭 P3-01**：收敛查询词汇或在方案中明确安全 token 例外，并更新解析测试。

整改批次的建议固定完成门禁：

```text
# 直接受影响测试
(cd backend && go test ./internal/observability/logship ./internal/logquery)
(cd backend && go test -race ./internal/observability/logship ./internal/logquery)
(cd marshaller && go test ./internal/elasticsearch ./internal/consumer)
(cd marshaller && go test -race ./internal/elasticsearch ./internal/consumer)

# 必要回归
(cd backend && go test ./... && go vet ./...)
(cd marshaller && go test ./... && go vet ./...)
bash scripts/verify-logs.sh --self-test
bash scripts/verify-logs.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.4 --base-ref origin/main
git diff --check
```

Phase 9 Review 只有在以下条件同时满足后才可改为通过：

- P1-01 与全部 P2 已关闭；P3-01 已修复或经权威方案明确接受。
- 临时构造的关闭竞态变为稳定通过的仓库回归测试。
- Elasticsearch 空集群替换场景不会创建无 alias/非 strict 日志索引，也不会提前提交 offset。
- 持续 queue-full 场景的状态日志数量有界，retry jitter 可验证。
- `develop/1.6.4` 获得唯一权威批次分配且分支/版本校验通过。
- 直接受影响检查、必要回归和真实 `verify-logs.sh` 最终 diff 门禁通过。

## 8. 最终结论

Phase 9 的正常端到端日志能力、管理员查询、安全字段边界和既定故障验收已经具备较完整的实现基础，但可靠关闭仍存在可复现的确认后丢失，运行期 Elasticsearch template 状态也可能与进程缓存脱节并造成不可查询日志被提交。加上故障日志节流、退避 jitter 和 `1.6.4` 分支治理缺口，当前提交不能作为 Phase 9 的最终 Review 通过状态。

**Review 结论：Fail。建议进入 Phase-09-04 / `develop/1.6.4` 整改，不应在 P1/P2 关闭前继续 Phase 10 实现。**
