# GoPulse Phase 7 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-04 |
| 用户指定权威 Review 分支 | `develop/1.4.3` |
| Review 分支创建方式 | fetch 后远端不存在 `origin/develop/1.4.3`；从最新 `origin/main` 创建本地 `develop/1.4.3`，未推送 |
| Review 基线 | `ff2fc20a16df3158751962d5d3227c69de53472d`（`ci: retry transient integration migrations (#70)`，Review 开始时与 `origin/main` 一致） |
| Phase 7 实现起点 | `b2fe9f005d389ff2855dd8a2af87c7df2058a92c`（Phase 7 计划合入提交） |
| Phase 7 已合入提交 | `66c02e3`（Phase-07-01 / PR #68）、`fd7f4bd`（Phase-07-02 / PR #69）、`ff2fc20`（收口后的 CI 重试修复 / PR #70） |
| 当前完成版本 | 根 `VERSION` 与 Frontend npm 元数据均为 `1.4.2`；本次只新增 Review 文档，不修改版本 |
| `develop/1.4.3` 治理状态 | Phase 7 总实施方案未分配 `1.4.3` / `develop/1.4.3`；当前分支校验失败，尚不能按仓库规则作为可推送开发批次 |
| 实际执行环境 | WSL2 Linux filesystem `/home/ray/GoPulse`，Go `1.26.7`，Node.js `24.20.0`，npm `11.19.0`，Docker `29.7.2` / Compose `v5.5.0` |
| Review 范围 | Phase 7 总/拆分方案与实施记录、Router 配置/Envelope/HTTP/Kafka Producer/验证 Consumer、Kafka Compose、Bash 生命周期与验收、CI、版本和分支治理 |
| Phase 7 变更规模 | 33 个文件，2095 行新增、66 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. Router 是否严格执行 Envelope v1、服务身份、原始字节、固定 Topic 和 Kafka 确认契约。
2. Kafka 不可用、请求取消和恢复时是否保持有界失败、不会形成未受控队列，且不影响业务链路。
3. `verify-router.sh` 是否真实覆盖权威方案规定的非法输入非写入、故障恢复和资源安全边界。
4. Phase 7 的版本、分支、方案和实施记录是否足以支持后续整改批次。

Review 没有读取、修改、暂存或提交工作区原有的未跟踪文件 `使用指南.md`。

## 2. 总体结论

Phase 7 已交付方向正确且可以真实运行的纵向主体能力：

- 新增独立 Router Go module，服务身份只接受独立 Bearer token；Cookie、JWT 和 query token 不能替代内部身份。
- Router 对正文大小、Content-Type、顶层 JSON 字段、重复 key、消息 ID、时间、payload、Idempotency-Key 和受支持类型执行有界校验。
- Kafka record key 固定为 `message_id`，value 保留 Router 收到的原始 HTTP body；本次真实验收再次证明 direct record 的 body/value SHA-256 相同且逐 byte 相等。
- Kafka 使用显式单 Topic、关闭自动建 Topic、`acks=all` 和客户端幂等协议写；Router 只在 delivery callback 成功后返回 `202`。
- `scripts/verify-router.sh` 的真实链路再次通过：真实 Monitor `success`、Redis 停止后的 `target_unavailable`、Kafka 停止时 health/readiness 分离、发布 `503`、原进程恢复以及隔离资源清理均成功。
- Backend、Monitor、Exporter、Frontend 和既有业务回归没有出现本次可复现的功能回归。

但是，当前实现仍不能通过 Phase 7 Review：

1. **严格 schema 边界存在实际缺口。** `schema_version` 使用 `json.Unmarshal(..., *json.Number)` 后只调用 `Int64()`，会把 JSON 字符串 `"1"` 接受为整数 1。专项复现得到 HTTP `202` 且 Producer 被调用一次，非法 Envelope 能进入 Kafka。
2. **Producer 的 256 records / 8 MiB 缓冲契约没有实际生效。** Router 在 franz-go 外增加容量为 1 的全局 slot，并让 publish、readiness 和 close 共用；健康路径永远只有一个 record 能进入客户端，其余 HTTP handler 在 slot 前等待。
3. **验收安全自检偏离权威合同。** `verify-router.sh --self-test` 直接调用 Docker Compose，且没有执行计划要求的 PID、project、container、volume、端口冲突、Topic 和清理目标负向验证。
4. **真实非法输入矩阵不完整。** 默认验收只以一个未知顶层 `topic` 字段证明非写入，没有真实覆盖计划明确列出的无/错 token、Cookie-only、Content-Type、压缩、超限、重复 key、尾随 JSON、Idempotency-Key 和 unsupported 类型等分支。
5. **随机端口和治理状态仍未收口。** 五个端口独立申请后立即释放，没有两两唯一性检查；同时 `develop/1.4.3` 没有权威批次分配，Phase-07-01 方案与实施记录仍写作“待远程门禁/合入”。

本次记录 **1 项 P1、5 项 P2**。P1-01 会直接破坏 Phase 7 对 Kafka 输入 schema 的交接保证，并可能把 Phase 8 无法按契约解析的消息写入正式 Topic，因此必须在 Phase 8 开始依赖该 Topic 前关闭。

## 3. 风险分级

| 级别 | 数量 | 定义 | 本次结论 |
| --- | ---: | --- | --- |
| P0 | 0 | 数据破坏、凭据泄漏、任意代码执行或全系统不可用 | 未发现 |
| P1 | 1 | 阻断阶段完成、破坏安全/持久数据/公共传输契约，或会向下游交付非法事实 | 必须整改后重新验收 |
| P2 | 5 | 有界性、验收可信度、资源安全或治理不一致；当前可运行但不能按权威方案关闭 | 应纳入同一整改批次 |
| P3 | 0 | 非阻断可维护性或体验问题 | 未单列 |

## 4. Review Findings

### P1-01：字符串形式的 `schema_version` 被当作整数 1 接受并写入 Kafka

**位置**

- `router/internal/envelope/envelope.go:80-87`
- `router/internal/httpserver/server.go:105-133`
- `router/internal/envelope/envelope_test.go:18-49`
- `router/internal/httpserver/server_test.go:87-113`

**事实与根因**

当前实现先把原始字段反序列化到 `json.Number`：

```go
var schema json.Number
if err := json.Unmarshal(values["schema_version"], &schema); err != nil {
    return Message{}, errors.New("schema_version must be an integer")
}
schemaValue, err := schema.Int64()
```

`encoding/json` 对 `*json.Number` 会接受 JSON number，也会接受可以解释为 number 的 JSON string。因此：

```json
{
  "schema_version": "1",
  "message_id": "0123456789abcdef0123456789abcdef",
  "type": "metrics",
  "source": "redis",
  "timestamp": "2026-09-04T00:00:00Z",
  "payload": {}
}
```

会得到 `schemaValue == 1`，随后进入 Producer。权威方案第 7.1 节要求 `schema_version` 必须是**整数** `1`；字符串不是合法 Envelope v1。

**本次专项复现**

在不保留临时测试文件的情况下，通过 Router HTTP handler + fake Producer 执行专项测试，实际结果为：

```text
=== RUN   TestReviewQuotedSchemaVersionReachesProducer
    review_probe_test.go:14: status=202 producer_calls=1
--- PASS: TestReviewQuotedSchemaVersionReachesProducer (0.00s)
```

更底层的 `envelope.Validate` 专项探针也确认：

```text
accepted quoted schema_version; message_id=0123456789abcdef0123456789abcdef
```

这不是只影响错误码的差异，而是非法 schema 已越过 Router 边界并获得 `202`。

**影响**

- Kafka Topic 不再保证只含权威 Envelope v1；Phase 8 Marshaller 若按整数 schema 解码，可能把该 record 作为异常消息处理或直接失败。
- Router 的“严格顶层验证”与实施记录、README、阶段交接不一致。
- 当前单元测试和真实验收都没有覆盖 schema 的 JSON 类型，因此固定门禁会错误通过。

**建议修复**

- 在调用数值转换前明确要求原始 token 是 JSON number，而不是 string、bool 或 null；可对 `json.RawMessage` 使用只接受数值 token 的 decoder，或先拒绝首字节为引号的表示。
- 保持只接受十进制整数 token `1`；`"1"`、`1.0`、`1e0`、`null` 和布尔值均应返回 `400 message_invalid` 或按权威映射明确处理。
- 增加最低层代表性测试：合法整数 `1` 成功，字符串 `"1"` 失败。
- 在真实 Kafka 非写入矩阵中加入字符串 schema，核对目标 partition end offset 不增加。

**关闭条件**

1. `schema_version:"1"` 不再调用 Producer，HTTP 返回非成功且符合固定错误映射。
2. 合法整数 `schema_version:1` 的原始字节仍不被改写。
3. Router unit/race 和真实 `verify-router.sh` 非写入证据通过。

---

### P2-01：容量为 1 的全局 slot 使 256 records / 8 MiB Producer 缓冲契约失效

**位置**

- `router/internal/kafka/producer.go:11-20`
- `router/internal/kafka/producer.go:22-51`
- `router/internal/kafka/producer.go:53-85`
- `router/internal/kafka/producer.go:87-98`
- `dev/imple/Phase-07/Phase-07-总实施方案.md:233-241`

**事实**

`Producer` 创建了固定容量为 1 的 slot：

```go
type Producer struct {
    client *kgo.Client
    slot   chan struct{}
}

return &Producer{
    client: client,
    slot: make(chan struct{}, 1),
}, nil
```

`Produce`、`Ready` 和 `Close` 全部必须先获得同一个 slot。结果是：

- 任意时刻最多只有一条 record 能进入 franz-go；配置的 `MaxBufferedRecords=256` 和 `MaxBufferedBytes=8 MiB` 在正常路径上不会成为实际背压边界。
- `/ready` 元数据检查与正式发布相互阻塞，健康检查流量会占用唯一生产 slot。
- Kafka 故障时，第一个请求可持有 slot 至 produce timeout；后续已完成身份与 1 MiB body 读取的 HTTP handler 在 slot 外等待，等待数量由并发连接数决定，而不是由 Kafka 的 256 records / 8 MiB 配置决定。
- `UnsafeAbortBufferedRecords()` 是全客户端操作；当前全局串行化避免了它误伤并发请求，但代价是把权威方案声明的客户端有界缓冲退化成单请求串行器。

**影响**

- README 和实施记录所称的“256-record / 8 MiB buffer”不是实际可用能力。
- 在合法内部调用突发或 Kafka 故障时，吞吐下降且 handler 内存/连接等待边界与配置不一致。
- 缺少 Kafka Producer package 的并发、buffer-full、取消和 close 定向测试，当前行为不会被现有 unit/race gate识别。

**建议修复**

- 重新设计取消路径，使单个请求超时不会通过全局 abort 影响其他 record，同时让 franz-go 的 record/byte limits 成为真实背压边界。
- 如果确实要求业务层串行，应把配置和权威方案明确改为单 in-flight，并给 HTTP handler 增加显式、可配置、立即拒绝的有界准入，而不是保留名义上的 256/8 MiB 配置。
- 增加代表性 Producer 测试：并发健康生产、buffer/准入耗尽、一个请求取消不影响其他请求、readiness 不长期饿死 publish、shutdown 有界。

**关闭条件**

- 实现、配置、README 和总方案对最大并发/缓冲的描述一致。
- 故障压力下等待请求数量受明确配置约束，且不会形成超出配置的 handler 等待队列。
- 单请求取消和恢复不会中止或污染其他已接收请求。

---

### P2-02：`verify-router.sh --self-test` 依赖 Docker，且没有执行约定的资源安全负向矩阵

**位置**

- `scripts/verify-router.sh:221-230`
- `dev/imple/Phase-07/Phase-07-总实施方案.md:271-279`
- `dev/imple/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md:64-69`

**事实**

权威方案要求 `--self-test` 执行**无 Docker**的 token、PID、project、container、volume、port、Topic 和清理目标负向验证。当前 self-test 实际只做：

1. 构建 Router 和 Consumer。
2. 验证短 token 被拒绝。
3. 验证空 offset range 被拒绝。
4. 执行 `docker compose ... config --quiet`。
5. grep 自动建 Topic 配置。

它既依赖 `docker` 命令，也没有构造伪造 PID record、危险 project、歧义 container/volume、端口冲突、被篡改 Compose 文件或清理边界。

**本次专项复现**

在保留 Go 和基础 shell 工具、但从 PATH 移除 Docker 的环境中执行：

```text
scripts/verify-router.sh: line 228: docker: command not found
exit_status=1
```

相比之下，`verify-business.sh --self-test` 和 `verify-exporter.sh --self-test` 均实际输出“不访问 Docker”的安全自检结果。

**影响**

- `Scripts and Compose` job 中的 self-test 不能证明 Router 验收清理不会误操作非归属资源。
- Docker 可用的 CI 环境掩盖了 self-test 本身的依赖违约。
- Phase 7 把 `verify-router.sh` 定义为资源安全唯一主验收入口，但最便宜、最常运行的负向门禁没有覆盖该风险。

**建议修复**

- 将 Compose 渲染检查留在已有 `Scripts and Compose` 独立步骤，不放入无 Docker self-test。
- 仿照 `verify-exporter.sh` 提取并测试 `valid_project`、端口唯一性、PID/start ticks/executable/cwd/marker 校验、Compose 文件摘要/路径、container/volume label 和 cleanup armed context。
- self-test 应在 `docker` 不存在时仍成功，并明确输出已拒绝的危险目标数量。

**关闭条件**

- 无 Docker PATH 下 `scripts/verify-router.sh --self-test` 返回 0。
- 至少覆盖计划列出的 token、PID、project、container、volume、port、Topic 和清理目标负向分支。
- 负向验证不停止无关进程、不删除资源。

---

### P2-03：默认 Router 验收的五个随机端口没有执行完整唯一性检查

**位置**

- `scripts/verify-router.sh:13-16`
- `scripts/verify-router.sh:238-242`

**事实**

当前代码连续五次调用 `free_port`：

```bash
KAFKA_PORT=$(free_port)
REDIS_PORT=$(free_port)
ROUTER_PORT=$(free_port)
MONITOR_PORT=$(free_port)
EXPORTER_PORT=$(free_port)
```

每次 Python 调用在打印端口后立即关闭 socket。脚本没有两两唯一性检查，也没有在选择整个端口组时同时保持 socket。内核可以在后续调用中重新分配同一个端口；外部进程也可以在申请与服务监听之间占用端口。

仓库已有更安全的直接先例：

- `verify-exporter.sh` 的 `allocate_ports` 对全部端口执行集合唯一性检查，并在 self-test 覆盖非相邻冲突。
- `verify-business.sh` 在同一 Python 进程中同时持有八个 socket，输出后再统一关闭，并再次验证两两唯一。

**影响**

- 随机重号会造成 Compose 绑定失败、Router/Monitor 启动失败或清理证据失真，形成偶发 CI flake。
- self-test 没有端口冲突负向用例，无法阻止同类问题回归。

**建议修复**

- 一次性申请五个端口并在生成阶段同时持有 socket；输出后再关闭。
- 对最终端口数组执行显式唯一性和非默认端口校验，失败时有限重试。
- self-test 至少覆盖相邻和非相邻两种重复组合。

**关闭条件**

- 五个端口生成逻辑保证集合大小为 5。
- 端口冲突负向测试在无 Docker self-test 中通过。

---

### P2-04：真实 Kafka 非写入验收只覆盖一个未知字段，未实现权威非法输入矩阵

**位置**

- `scripts/verify-router.sh:295-318`
- `dev/imple/Phase-07/Phase-07-总实施方案.md:307-314`
- `dev/imple/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md:64-69`
- `dev/imple/Phase-07/Phase-07-02-集成验收与阶段收口.md:42-49`

**事实**

默认真实验收先写入一个合法 direct record，然后只构造：

```python
value['topic'] = 'client-controlled'
```

并验证该未知顶层字段返回 `400`、partition end offset 不增长。权威阶段矩阵还明确要求真实覆盖：

- 无 token、错误 token、用户 Cookie、admin Cookie。
- 错误 Content-Type、压缩正文、超限正文。
- 重复 key、尾随 JSON。
- Idempotency-Key 缺失/重复/不匹配。
- unsupported schema/type/source。

现有 unit tests 覆盖其中一部分，但真实 Kafka 非写入证据只有一个 unknown-field 分支；P1-01 的字符串 schema 因此可以同时通过 unit 和 E2E 门禁。

**影响**

- 实施记录对“严格 routing / 非法请求不入 Topic”的概括强于实际证据。
- HTTP header 解析、Go server 行为和 Kafka end offset 的组合风险没有被固定门禁保护。
- 阶段完成定义要求的多个不同业务结果被压缩成一个不能代表其他分支的案例。

**建议修复**

- 在同一个隔离 Kafka project 和同一个 baseline/end-offset helper 上表驱动执行计划列出的不同拒绝类别。
- 每个请求验证固定 HTTP/code；整组执行前后核对 end offset 不变，避免为每个案例重复启动基础设施。
- 至少加入 P1-01 的字符串 schema，保证 schema 类型错误不会写入。

**关闭条件**

- 权威矩阵列出的不同拒绝结果均有真实 HTTP + Kafka 非写入证据。
- 证据输出不包含 token、Cookie 或原始 payload。

---

### P2-05：`develop/1.4.3` 未被权威方案分配，且 Phase-07-01 状态文档未同步远程完成事实

**位置**

- `dev/imple/Phase-07/Phase-07-总实施方案.md:44-49`
- `dev/imple/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md:3-5`
- `dev/logs/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md:3-10`
- `scripts/ci/validate_branch.py`

**事实**

Phase 7 权威分配表只有：

- `Phase-07-01` → `1.4.1` / `develop/1.4.1`
- `Phase-07-02` → `1.4.2` / `develop/1.4.2`

本次用户指定 `develop/1.4.3` 为权威 Review 分支，但 fetch 和 `git ls-remote` 均确认远端不存在该分支；从 `origin/main` 创建本地分支后执行治理校验，结果为：

```text
ERROR: develop/1.4.3 must map to exactly one authoritative allocation; found 0
```

此外，Phase-07-01 已通过 PR #68 合入，但其拆分方案仍写“待远程门禁和 Pull Request 合入”，实施记录仍写远程状态待补充；这与总方案的“已合入”以及 Phase-07-02 的完成声明不一致。

**影响**

- `develop/1.4.3` 当前不能通过 Branch governance，也不能按自动 PR 规则成为合法整改分支。
- Phase 7 宣称两份实施记录真实完整，但 07-01 的远程完成事实未回填。
- 若直接在当前分支实现修复并推送，会违反“先在总实施方案中权威分配批次/版本/分支”的仓库规则。

**建议修复**

- 在开始 P1/P2 代码整改前，先在 Phase 7 总实施方案中增加明确的 Review 整改批次，例如 `Phase-07-03` → `1.4.3` / `develop/1.4.3`，并补充该批验收标准。
- 同步 Phase-07-01 拆分方案与实施记录的 PR、远程 checks、合入提交和完成状态。
- 规划文档更新本身不修改 `VERSION`；整改批次完成时再把根和 Frontend 版本更新为 `1.4.3`。

**关闭条件**

- `python3 scripts/ci/validate_branch.py --branch develop/1.4.3 --base-ref origin/main` 通过且只匹配一个权威分配。
- Phase-07-01 总/拆分方案和实施记录对远程状态描述一致。

## 5. 已验证的正向实现

### 5.1 Router 身份、HTTP 与 Envelope 基础边界

- `ROUTER_API_TOKEN` 要求至少 32 bytes 且拒绝 CR/LF；比较前对提供值与期望值做 SHA-256，再使用常量时间比较。
- `/health` 不依赖 Kafka，`/ready` 要求 Bearer token 并查询 broker/Topic 元数据。
- 发布接口使用有界 reader 支持 Content-Length 和 chunked body，正文上限为配置值且最大 1 MiB。
- 顶层 unknown/duplicate/missing 字段、非法 UTF-8、尾随 token、message ID、timestamp、payload object 和 Idempotency-Key 的主体逻辑已存在。
- Router 不读取客户端 Topic，路由表固定 `metrics → gopulse-observability-v1`。

### 5.2 Kafka record、故障与恢复

- Kafka Compose 使用单节点 KRaft、internal/external/controller listener、loopback external port和显式 initializer。
- Broker 与客户端均不依赖自动建 Topic；Topic 固定为 1 partition / replication factor 1。
- Router record key 为 `message_id`，value 为原始 body；真实 direct record 验证 `byte_equal=true`。
- Kafka 停止时，本次独立验收得到 Router `/health=200`、`/ready=503`、publish=`503`。
- Kafka 与 Redis 恢复后，Router/Monitor PID 不变，真实 Monitor `success` 消息再次进入 Kafka。

### 5.3 组件职责隔离与回归

- `monitor/go.mod` 没有引入 Kafka SDK；Monitor 继续通过 HTTP Publisher 与 Router 解耦。
- Router module 没有引入 RabbitMQ、业务数据库、清洗或存储代码。
- Backend unit、Frontend unit/build 和业务验收在当前最终提交上通过，未发现 Kafka 对社交业务的直接依赖。
- `verify-router.sh` 正常完成后没有遗留本次随机 project 的 container、volume、监听端口或进程。

## 6. 本次实际执行的验证

### 6.1 通过

- `git fetch --prune origin`
- `(cd router && test -z "$(gofmt -l .)")`
- `(cd router && go test -count=1 ./...)`
- `(cd router && go vet ./...)`
- `(cd router && go test -race -count=1 ./...)`
- `(cd monitor && test -z "$(gofmt -l .)")`
- `(cd monitor && go test -count=1 ./...)`
- `(cd monitor && go vet ./...)`
- `(cd monitor && go test -race -count=1 ./...)`
- `(cd exporters/redis && test -z "$(gofmt -l .)")`
- `(cd exporters/redis && go test -count=1 ./...)`
- `(cd backend && test -z "$(gofmt -l .)")`
- `(cd backend && go test -count=1 ./...)`
- `(cd frontend && npm test -- --run)`：9 个 test files、48 项测试通过。
- `(cd frontend && npm run build)`：typecheck 与 Vite production build 通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：25 项通过。
- `python3 scripts/ci/validate_versions.py`
- `python3 scripts/ci/validate_branch.py --branch develop/1.4.2 --base-ref b2fe9f0`
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh`
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- `scripts/verify-router.sh --self-test`（Docker 可用环境）
- `scripts/verify-business.sh --self-test`
- `scripts/verify-business.sh`：完整 Chromium、RabbitMQ/Outbox、搜索、Redis/restart、日志和资源清理矩阵通过。
- `scripts/verify-monitor.sh --self-test`
- `scripts/verify-exporter.sh --self-test`
- `scripts/verify-router.sh`：真实 Kafka/Redis/Monitor/Exporter 链路、Kafka outage/recovery 和清理通过。
- `git diff --check`

### 6.2 预期失败与 Review 专项复现

1. 当前指定分支治理校验失败：

   ```text
   ERROR: develop/1.4.3 must map to exactly one authoritative allocation; found 0
   ```

2. 无 Docker PATH 下 Router self-test 失败：

   ```text
   scripts/verify-router.sh: line 228: docker: command not found
   exit_status=1
   ```

3. 字符串 schema 被接受并进入 Producer：

   ```text
   status=202 producer_calls=1
   ```

所有专项临时测试文件均已删除，没有保留为项目变更。

### 6.3 本次未重复执行

- `scripts/verify-monitor.sh` 默认完整验收。
- `scripts/verify-exporter.sh` 默认完整验收。

原因：本次直接变更范围是 Phase 7 Router/Kafka；Monitor/Exporter 的直接单元、race/self-test 已通过，且真实 `verify-router.sh` 已启动真实 Monitor 与受管 Exporter。没有观察到要求扩大到两套完整历史验收的具体回归。

## 7. 计划、实施记录与完成定义核对

| 完成项 | 状态 | 说明 |
| --- | --- | --- |
| 真实 Redis → Exporter → Monitor → Router → Kafka → Consumer | 通过 | 本次独立真实验收通过 |
| record key 与原始 bytes | 通过 | direct record 逐 byte 相等 |
| Kafka outage / recovery，无进程重启 | 通过 | health/readiness/publish/PID 证据通过 |
| 严格 Envelope v1 只允许整数 schema | **失败** | 字符串 `"1"` 获得 202 并调用 Producer |
| 全部非法输入真实 Kafka 非写入 | **未满足** | 默认验收只覆盖未知 `topic` 字段 |
| Producer 256 records / 8 MiB 有界缓冲 | **未满足** | 全局 slot 把实际 in-flight 限制为 1，其他 handler 在外部等待 |
| 无 Docker 资源安全 self-test | **失败** | self-test 调用 Docker 且缺少负向矩阵 |
| 随机端口两两唯一 | **未满足** | 五次独立申请，没有集合校验 |
| Phase 7 两份实施记录与远程事实一致 | **未满足** | 07-01 仍写待远程合入 |
| `develop/1.4.3` 权威分配 | **未满足** | 分配数为 0，branch governance 失败 |

因此，虽然 Phase-07-01/02 的正常纵向链路和大部分故障恢复能力可运行，Phase 7 仍不满足“严格合法消息才写 Kafka、固定验收可信、治理状态一致”的完整关闭条件。

## 8. 建议整改顺序

1. **先修 P1-01**：严格判定 `schema_version` JSON token 类型，并加 unit + 真实 Kafka 非写入回归。
2. **建立权威整改批次**：在总方案分配 `Phase-07-03` / `1.4.3` / `develop/1.4.3`，补齐 07-01 远程状态。
3. **修正 Producer 有界模型**：让配置的 records/bytes 边界真实生效，并覆盖并发、取消、buffer-full 和 shutdown。
4. **补齐验收矩阵**：在单次隔离环境中表驱动执行不同非法输入，不重复创建基础设施。
5. **加固验收自身安全**：无 Docker self-test、强归属清理上下文、五端口唯一性和非相邻冲突测试。
6. 在最终 diff 上运行固定 Router/Monitor/Backend/Frontend 门禁、`verify-router.sh`、必要业务回归和 branch/version validation；全部通过后更新 `VERSION` 为 `1.4.3` 并停止。

## 9. 最终判定与关闭条件

**最终判定：Fail。**

Phase 7 主链路已经可运行，但 Router 当前允许非整数 schema 进入 Kafka，直接破坏 Phase 8 的输入契约；同时 Producer 缓冲语义、非法输入真实验收、资源安全 self-test、随机端口和整改分支治理均与权威方案存在可验证偏差。

至少满足以下条件后，才建议把 Review 状态改为通过：

1. 字符串/非整数 schema 无法进入 Producer 或 Kafka，合法整数 schema 保持原始 bytes。
2. Producer 并发、缓冲和取消行为与配置及文档一致，不形成未受控 handler 等待。
3. 真实非法输入矩阵全部证明 Kafka 非写入。
4. `verify-router.sh --self-test` 在无 Docker 环境通过并覆盖资源安全负向边界。
5. 随机端口保证两两唯一，正常/失败/中断清理仍只作用于强归属资源。
6. Phase 7 总方案权威分配 `develop/1.4.3`，07-01/02 状态与远程事实一致。
7. `develop/1.4.3` 的固定本地/远程门禁通过，根与 Frontend 版本按整改批次更新为 `1.4.3`。
