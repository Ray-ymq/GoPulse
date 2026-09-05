# GoPulse Phase 10 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-05 |
| 用户指定权威 Review 分支 | `develop/1.7.4` |
| Review 分支创建方式 | fetch 后远端不存在 `origin/develop/1.7.4`；从最新 `origin/main` 创建本地 `develop/1.7.4`，未推送 |
| Review 基线 | `2882d7edbff5d3fc176134765813fb2043b6e756`（PR #93，Review 开始时与 `origin/main` 一致） |
| Phase 10 计划合入点 | `e6557e6`（PR #88） |
| Phase 10 已合入实现提交 | `50cb497`（Phase-10-01 / PR #89）、`3c6a194`（Phase-10-02 / PR #91）、`d332630`（Phase-10-03 / PR #92） |
| Phase 10 完成记录 | `340dd16`、`8d92e95`，后者通过 PR #93 合入 Review 基线 |
| 当前完成版本 | 根 `VERSION`、Frontend `package.json` 与 lockfile 均为 `1.7.3`；本次只新增 Review 文档，不修改版本 |
| `develop/1.7.4` 治理状态 | Phase 10 总实施方案只分配到 `1.7.3`；`validate_branch.py` 对 `develop/1.7.4` 返回 1，当前分支尚不能按仓库规则推送 |
| 实际执行环境 | WSL2 Linux filesystem `/home/ray/GoPulse-1.7.4`，Go `1.26.7`，Node.js `24.20.0`，npm `11.19.0`，Docker `29.7.2` / Compose `v5.5.0`，Python `3.12.3` |
| Review 范围 | Phase 10 总/拆分方案与实施记录、Monitor EventMonitor、Plugin Manager 生命周期/故障事件、Metrics collection/target episode、Router、Marshaller Events target、Elasticsearch Events Store、Backend 管理员查询、验收脚本、版本与分支治理 |
| Phase 10 变更规模 | 相对计划基线 `e6557e6` 至阶段验收提交 `d332630`：52 个文件，3430 行新增、70 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. 插件生命周期、终态失败、非预期退出和 Metrics failure/recovery episode 是否只在真实状态提交后记录，且旧 runtime 不得破坏替换后的有效 runtime。
2. EventMonitor 的 `Record`、重试、永久拒绝、队列容量和关闭协议是否满足非阻塞、线性化与有界 shutdown 合同。
3. Router → Kafka → Marshaller → Elasticsearch 是否维持严格 Events v1 合同、幂等 `_id`、mapping/alias、ownership 与 offset 边界。
4. Backend Events API 是否维持 `401/403/admin`、固定 alias、PIT/cursor、安全 DTO 和固定可实现过滤组合。
5. Phase 10 实施记录、真实验收证据、版本及 `develop/1.7.4` 治理是否足以支持 Review 整改批次。

本次 Review 使用独立 worktree，没有暂存、删除或提交 `/home/ray/GoPulse` 原工作区内的 `使用指南.md`、Phase 7 日志目录变动或其他用户未提交改动。三个用于复现 findings 的临时测试均在执行后删除，未纳入提交。

## 2. 总体结论

Phase 10 已形成主体完整且能通过真实隔离验收的 Events 纵向闭环：

- Monitor 生成固定 Events v1 payload，通过有界内存队列向 Router 发送；确定性 `4xx` 丢弃当前坏记录，暂时失败保持队首和 message ID 重试。
- Plugin Manager 已接入 install/start/stop/update 成功事件、代表性终态失败与 unexpected exit；MetricsMonitor 已实现 collection failure/recovery 与 target unavailable/recovered 两个独立 episode。
- Router 保持原始 Envelope bytes并写入既有正式 Topic；Marshaller 使用独立 Events validator/transformer/store，并复用既有 ownership/commit 状态机。
- Elasticsearch Events template、日索引、strict root/metadata mapping、固定 read alias 和 message ID `_id` 已建立；Backend 管理员 API 通过固定 alias 和 PIT 查询安全 DTO。
- 本次重新执行完整 `scripts/verify-events.sh`，真实生命周期、失败/恢复、永久坏 record、Elasticsearch outage、offset 持有、同 ID 重放及 Metrics/Logs/Events 并存全部通过。
- 四个 Go module 的测试与 vet、直接受影响包的 race test、脚本语法/self-test、版本校验和 Python CI 测试均通过。

但是，当前实现仍不能通过 Phase 10 Review：

1. **旧 runtime watcher 在确认自己仍是当前 runtime 之前，就删除全局 process record 并禁用当前 MetricsMonitor。** 当旧进程退出与管理员重新 `Start` 交错时，旧 watcher 可以破坏已经成功替换的新 runtime，使新 exporter 仍在运行但 process record 被删除、metrics collection 被关闭。
2. **EventMonitor `Close` 在调用方 deadline 到达后仍无界等待 worker。** 只要 sender 没有及时响应 context cancellation，Monitor shutdown 就会超过配置的关闭上限并可永久卡住。
3. **Phase-10-03 声明已验证 PIT/cursor、空结果和 Elasticsearch unavailable，但固定测试与真实脚本没有覆盖合法 cursor 分页、API 级空结果和 `503 events_unavailable`。** 当前阶段完成证据与实际执行内容不完全一致。
4. **用户指定的 `develop/1.7.4` 尚未获得权威批次分配，分支治理门禁失败。**
5. **Backend 过滤器仍接受部分不可能的 event/error 组合。** 这不形成 Elasticsearch DSL 注入，但违反固定词汇查询合同。

本次记录 **1 项 P1、3 项 P2、1 项 P3**。P1-01 可在合法并发交错中破坏当前有效插件 runtime 的管理与采集事实，是 Phase 10 生命周期/ownership 主合同的阻断项，因此总体结论为 **Fail**。

## 3. Findings 汇总

| ID | 级别 | 位置 | 摘要 |
| --- | --- | --- | --- |
| P1-01 | P1 | `monitor/internal/plugin/manager.go:690-712` | stale watcher 在 runtime fencing 前删除当前 process record 并禁用当前 metrics generation |
| P2-01 | P2 | `monitor/internal/events/monitor.go:125-140,165-210` | `Close` 在 deadline 后仍等待 worker，关闭时间并非真正有界 |
| P2-02 | P2 | `scripts/verify-events.sh:185-299`、`backend/internal/eventquery/eventquery_test.go:14-89`、Phase-10-03 实施记录第 3 节 | PIT/cursor、空结果和 API `503` 的完成声明缺少实际固定证据 |
| P2-03 | P2 | `dev/imple/Phase-10/Phase-10-总实施方案.md:53-70` | `develop/1.7.4` 没有权威批次分配，分支治理校验失败 |
| P3-01 | P3 | `backend/internal/eventquery/eventquery.go:162-204` | 查询接受不可能的 event/error 组合 |

## 4. 详细 Findings

### P1-01：stale watcher 可破坏已替换的当前 runtime

**位置**

- `monitor/internal/plugin/manager.go:690-712`
- 对照 `dev/imple/Phase-10/Phase-10-02-采集故障事件与可靠性闭环.md:33-40`
- 对照 `dev/imple/Phase-10/Phase-10-03-集成验收与阶段收口.md:210-218`

**问题**

`watch` 在收到某个 `runtime.done` 后按以下顺序执行：

1. 检查该 runtime 是否标记为 intentional；
2. 调用 `m.safeRemoveProcessRecord()` 删除插件唯一的 `runtime/process.json`；
3. 调用 `m.disableMetrics(context.Background())` 禁用 Manager 当前连接的 MetricsMonitor generation；
4. 最后才在 `m.mu` 下检查 `m.runtimes[id] != runtime`。

runtime identity fencing 发生得太晚。process record 路径和 MetricsMonitor 都是插件当前 generation 的共享资源，不属于 watcher 捕获的旧 runtime 私有资源。

存在如下合法交错：

1. 原 exporter 进程已经退出，但旧 watcher 尚未运行到 fencing 检查。
2. 管理员调用 `Start`；`runtimeOwnedLocked` 发现旧进程不再受控，于是启动新 exporter、写入新的 process record、覆盖 `m.runtimes[id]` 并启用新的 metrics generation。
3. 旧 watcher 恢复执行，在检查 `m.runtimes[id]` 之前删除新的 process record并关闭新的 metrics generation。
4. 旧 watcher 随后发现 map 中已是 replacement runtime并直接返回，不提交 failed 状态，也不记录 exited event。

最终 Manager 仍可能报告 `observed_state=running`，新 exporter 进程也仍存活，但 process ownership 持久记录已丢失且采集已停止。这个结果违反 Phase 10 对 ownership fencing、旧 runtime 隔离和 Metrics/Events 主结果不被错误改变的合同。

**实际证据**

本次创建未提交临时测试 `TestReviewStaleWatcherCannotAffectReplacementRuntime`：

- Manager map 预置 replacement runtime；
- 文件系统预置 replacement process record；
- 再触发 old runtime watcher；
- 期望旧 watcher既不禁用 metrics，也不删除 replacement record。

测试稳定失败：

```text
=== RUN   TestReviewStaleWatcherCannotAffectReplacementRuntime
    review_watcher_test.go:43: stale watcher disabled replacement metrics before checking runtime ownership
--- FAIL: TestReviewStaleWatcherCannotAffectReplacementRuntime (0.00s)
```

失败发生时 watcher 已进入 replacement 的 MetricsMonitor；代码顺序同时证明 process record 删除也发生在 fencing 之前。

**建议整改**

- watcher 收到退出后，第一步必须在 Manager 锁下确认 `m.runtimes[id] == runtime`；不匹配时不允许执行任何共享副作用。
- 对 process record 删除增加 generation/record identity 条件，只能删除内容与退出 runtime record 完全匹配的文件，不能仅按全局路径删除。
- MetricsMonitor 应按 generation 关闭，或至少保证 watcher 关闭的是与该 runtime 绑定的 generation，而不是 Manager 当前 generation。
- 增加确定性交错回归测试：旧 runtime 已退出、新 `Start` 已提交 replacement 后，旧 watcher不得删除新 record、不得 Disable 新采集、不得覆盖状态或制造假 exited event。

### P2-01：EventMonitor `Close` 的调用方 deadline 不是实际返回上限

**位置**

- `monitor/internal/events/monitor.go:125-140`
- `monitor/internal/events/monitor.go:165-210`
- 对照 `dev/imple/Phase-10/Phase-10-01-插件生命周期事件端到端查询闭环.md:37-42`

**问题**

`Close` 在 `ctx.Done()` 后会：

```go
m.cancel()
closeOnce(m.stop)
<-m.done
return ctx.Err()
```

`m.done` 只有 worker 返回后才关闭。若 worker 正阻塞在 `Sender.PublishRaw` 且 sender 没有及时响应 context cancellation，`Close` 会在调用方 deadline 之后继续无界等待。`stop` channel 只能中断 retry timer，不能中断正在执行的 sender 调用。

生产 HTTP sender当前会使用 request context，但 EventMonitor 的公开 `Sender` 接口没有声明或强制“必须在 cancellation 后有限时间返回”；更重要的是，Phase 10 合同明确要求 EventMonitor 自己的关闭超时不能卡死 Monitor shutdown。当前实现把这个保证完全交给下游实现，因此并未真正建立有界返回语义。

**实际证据**

本次创建未提交临时测试 `TestReviewCloseReturnsAtCallerDeadline`，使用一个进入发送后等待显式 release、忽略 context 的 sender。调用 `Close` 的 context 为 10 ms，测试给出 100 ms 返回窗口，结果稳定失败：

```text
=== RUN   TestReviewCloseReturnsAtCallerDeadline
    review_close_test.go:42: Close remained blocked after its caller deadline
--- FAIL: TestReviewCloseReturnsAtCallerDeadline (0.10s)
```

**影响**

- `MONITOR_EVENT_SHUTDOWN_TIMEOUT` 不能作为 Monitor 退出的硬上限。
- Router/HTTP transport 的异常实现、未来 sender 替换或底层调用未及时响应 cancellation 时，Monitor 可永久停留在 shutdown。
- `monitor/cmd/monitor/main.go` 的显式 Close 与 defer Close 都依赖同一个 `done`，不能从该状态自行恢复。

**建议整改**

- `Close` 在调用方 deadline 到达后应发出 cancel/stop并立即返回 `ctx.Err()`，不能再同步无界等待 `done`。
- worker 可在后台完成退出；如需要防止 goroutine 遗留，应在 sender 合同中明确 context 响应要求，并用内部 watchdog/可控 transport 保证生产 sender 上限。
- 加入代表性测试：正常 drain 等待完成；deadline 路径按时返回；sender 延迟退出后 worker最终关闭且重复 `Close` 安全。

### P2-02：PIT/cursor、空结果和 API `503` 的阶段完成声明缺少实际固定证据

**位置**

- `dev/logs/Phase-10/Phase-10-03-集成验收与阶段收口.md:39-42`
- `dev/imple/Phase-10/Phase-10-03-集成验收与阶段收口.md:208-219`
- `scripts/verify-events.sh:185-299`
- `backend/internal/eventquery/eventquery_test.go:14-89`

**问题**

Phase-10-03 实施记录明确写明已验证：

- filter、范围；
- PIT/cursor；
- 空结果；
- Elasticsearch unavailable 契约。

但当前固定证据实际只覆盖：

- 真实脚本中的 `401`、`403` 和一次 admin `source/plugin_id/limit=100` 查询；
- 参数解析中的非法 cursor 与超范围拒绝；
- service 层 alias missing 返回空页；
- service 层 repository error 返回应用错误；
- repository 固定 alias 和单条安全 `_source` 解码。

仓库中没有实际执行的测试证明：

1. 有效第一页返回签名 cursor，第二页使用 cursor 并正确传递 PIT/search_after；
2. terminal page 关闭 PIT；
3. API route 对空 alias/空结果返回规定的空 `data`；
4. Elasticsearch 故障经 handler/response 映射为 HTTP `503` 和 `events_unavailable`；
5. 真实脚本中的 admin 查询执行分页而非一次 `limit=100` 取完。

本次 `grep` 也确认 `scripts/verify-events.sh` 中没有 cursor 或 `503/events_unavailable` 验证，`eventquery_test.go` 只有非法 cursor 输入，没有合法 cursor 流程。

**影响**

- Phase 10 总方案第 14.1 节和 Phase-10-03 第 10 节的部分验收条件没有可重复固定证据。
- PIT 更新、search_after、cursor 过期、终页关闭和错误到 HTTP 响应的回归可能在全部现有门禁通过时进入主分支。
- 实施记录把未执行的验证呈现为已完成，不符合“只记录实际执行检查”的实施记录规则。

**建议整改**

- 在 Backend 最低有效测试层补一个代表性两页成功用例，固定 filters/limit/PIT/last sort、合法 cursor 续页和 terminal PIT close。
- 增加 API 级空结果与 Elasticsearch unavailable 映射测试，验证响应 code/body，而不是只检查 service 返回了非 nil error。
- 真实 `verify-events.sh` 不必重复全部组合，但至少应使用小 limit 完成一次真实 cursor 翻页，证明 Elasticsearch PIT 路径在集成环境成立。
- 更新 Phase-10-03 实施记录，只保留真正执行并可由命令或固定测试定位的证据。

### P2-03：`develop/1.7.4` 没有权威批次分配

**位置**

- `dev/imple/Phase-10/Phase-10-总实施方案.md:53-70`

**问题**

Phase 10 权威分配表当前只包含：

- Phase-10-01 → `1.7.1` / `develop/1.7.1`
- Phase-10-02 → `1.7.2` / `develop/1.7.2`
- Phase-10-03 → `1.7.3` / `develop/1.7.3`

用户指定的 `develop/1.7.4` 在表中不存在。Review 开始时远端同样不存在该分支，因此本次从最新 `origin/main` 创建了本地分支。实际执行：

```text
python3 scripts/ci/validate_branch.py --branch develop/1.7.4 --base-ref origin/main
ERROR: develop/1.7.4 must map to exactly one authoritative allocation; found 0
```

**影响**

- 当前本地 Review 分支可以承载报告，但不满足推送前分支治理门禁。
- 后续 P1/P2 整改不能在不更新权威计划的情况下合法推送或创建普通开发 PR。

**建议整改**

在开始代码整改前，把 Review 整改作为明确批次加入 Phase 10 总实施方案，例如 Phase-10-04 → `1.7.4` / `develop/1.7.4`，并建立对应拆分实施方案、验收条件和实施记录路径。仅新增本 Review 文档不修改 `VERSION`；真正完成整改批次时再按规则把根与 Frontend 版本更新到 `1.7.4`。

### P3-01：Backend 查询接受不可能的 event/error 组合

**位置**

- `backend/internal/eventquery/eventquery.go:162-204`
- 对照 `monitor/internal/events/contract.go:116-168`
- 对照 `marshaller/internal/events/events.go:125-166`

**问题**

`validFilters` 确认 `error_code` 是全局已知词汇，也在同时提供 `operation` 时校验 operation/error 组合；但只有部分 event/error 组合被显式拒绝。

例如以下组合当前会通过解析：

- `event_name=exporter_plugin_failed&error_code=publish_failed`
- `event_name=metrics_collection_failed&error_code=start_failed`

这两类文档都不可能通过 Monitor/Marshaller Events v1 validator：

- `exporter_plugin_failed` 只允许 start/stop/update/recover 对应插件错误码；
- `metrics_collection_failed` 只允许 scrape/publish 对应采集错误码。

**实际证据**

本次创建未提交临时测试 `TestReviewRejectsImpossibleEventErrorPairs`，结果稳定失败：

```text
=== RUN   TestReviewRejectsImpossibleEventErrorPairs
    review_filters_test.go:17: accepted impossible filters: map[error_code:[publish_failed] event_name:[exporter_plugin_failed]]
    review_filters_test.go:17: accepted impossible filters: map[error_code:[start_failed] event_name:[metrics_collection_failed]]
--- FAIL: TestReviewRejectsImpossibleEventErrorPairs (0.00s)
```

**影响**

查询仍使用服务端构造的 term query，不存在原始 DSL、通配符或脚本注入；实际结果通常只是空页。因此本项定为 P3。但它违反总方案“拒绝不可能 event/operation/error 组合”的公开参数合同，也会让客户端难以区分合法空结果与无意义过滤组合。

**建议整改**

- 使用单一 event specification table 同时描述 event name、severity、operation 和允许 error codes，避免分散的特例判断。
- 当 `event_name` 与 `error_code` 同时存在时，直接按该 event 的允许错误集合校验；Metrics 与 plugin failure 不得交叉接受错误码。
- 增加一个合法组合和两个跨域非法组合的最小解析测试。

## 5. 已通过的关键检查

### 5.1 Go 代码、静态检查与 race

以下命令本次实际执行并通过：

```text
(cd backend && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd monitor && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd router && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
```

直接受影响范围 race 检查通过：

```text
(cd backend && go test -race -count=1 ./internal/eventquery ./internal/http/...)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin ./internal/metrics/collector)
(cd marshaller && go test -race -count=1 ./internal/events ./internal/elasticsearch ./internal/consumer)
```

这些成功结果不否定 P1-01 和 P2-01；两项都需要构造正常测试未覆盖的精确 generation/deadline 条件。

### 5.2 脚本、版本与治理检查

以下检查通过：

```text
python3 -m unittest discover -s scripts/ci -p 'test_*.py'   # 25 tests
python3 scripts/ci/validate_versions.py
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/verify-events.sh \
  scripts/package-redis-exporter.sh
scripts/verify-events.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-logs.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
```

版本一致性和所有 self-test 通过。分支治理检查按 P2-03 所述失败，返回状态 1。

### 5.3 真实 Events 集成验收

本次在随机强归属项目 `gopulse-events-a2df33cae504` 中重新执行：

```text
scripts/verify-events.sh
```

最终通过并输出：

```text
Failure, recovery, replay, offset, and mixed Events query closed end to end through index gopulse-events-v1-2026.09.05.
```

该验收重新证明了当前正常路径及既定故障矩阵：生命周期事件、collection/target episode、start failure、unexpected exit、永久坏消息继续、Elasticsearch outage 时 offset 不推进、恢复后推进、同 ID 重放幂等，以及 Metrics/Logs/Events 三类并存。

该成功结果不覆盖：

- old watcher 与 replacement `Start` 的精确交错；
- sender 不及时响应 cancellation 时的 Close deadline；
- 合法 PIT cursor 多页流程、API 空结果和 HTTP `503`；
- P3-01 的跨域 event/error 参数组合。

## 6. Review 通过项

除上述 findings 外，本次确认以下关键实现方向成立：

1. **Events v1 合同严格且三端基本一致**：event name、source、severity、message 和 metadata 组合在 Monitor、Marshaller 与 Backend 返回层均有白名单校验。
2. **EventMonitor 正常队列协议成立**：`Record` 与 `closed/queue` 共享 mutex，关闭前接受与关闭后拒绝已经线性化；临时失败重用同一 ID，queue full 有首次/恢复日志，退避包含可测试 jitter。
3. **Plugin 主结果与 event recorder 解耦**：recorder 返回 false 不回滚成功插件状态，也不改变 Metrics scrape 主结果。
4. **episode 主逻辑成立**：持续 collection failure 去抖，完整 publish 成功后恢复；target unavailable/recovered 只基于已成功发布的 metrics 状态。
5. **Router 与 Kafka 合同保持兼容**：Events 使用固定 `events/monitor` 组合和既有 Topic，不改变 Metrics/Logs 原始 bytes、key 与确认语义。
6. **Marshaller 隔离成立**：Events 使用独立 transformer/store/template/index/alias，并在写入后复核 strict mapping 与 alias；暂时存储失败不返回 writer 成功。
7. **Backend 授权边界成立**：未登录与普通用户在 repository 前分别得到 `401`、`403`，管理员使用固定 alias 与白名单 `_source`。
8. **真实故障恢复主链成立**：Elasticsearch outage 持有正式 group offset，恢复后重新建立合同并继续；同 ID replay 不增加文档。
9. **安全和平台边界成立**：开发与验收使用 WSL2/Bash；未修改冻结 PowerShell；随机项目、loopback 端口、PID/binary 与清理目标自检通过。

## 7. 建议整改顺序与完成条件

建议在权威计划中建立 Phase-10-04 / `1.7.4` 整改批次，并按以下顺序执行：

1. **先修复 P1-01**：把 runtime identity fencing 移到任何 process record/metrics/state/event 副作用之前，并增加 replacement generation 确定性并发测试。
2. **修复 P2-01**：让 `Close` 的返回时间真正受调用方 context 限制，同时保留正常 drain 和最终 worker cleanup。
3. **补齐 P2-02**：增加合法 PIT/cursor 两页、terminal close、API 空结果与 `503 events_unavailable` 固定证据，并校正实施记录。
4. **修复 P2-03**：更新 Phase 10 总实施方案的权威版本/分支分配，使 `develop/1.7.4` 治理通过。
5. **关闭 P3-01**：按单一 event specification 收敛 event/error 组合并添加最小解析测试。
6. 只重跑受影响 package 检查、Phase-10-04 固定完成门禁和一次最终 `scripts/verify-events.sh`；无具体风险依据时不扩展为全仓审计或额外容量活动。

Review 整改完成至少应满足：

- stale watcher 无法删除或停用 replacement generation 的任何资源；
- EventMonitor deadline 测试证明 `Close` 按时返回；
- 有效 cursor 多页、terminal PIT close、空结果与 API `503` 有固定测试证据；
- 不可能 event/error 组合返回 `400 validation_failed`；
- `develop/1.7.4` 权威映射、版本校验、分支治理、受影响 Go/race 检查和最终 Events 集成验收全部通过；
- 根与 Frontend 版本在整改批次完成时同步为 `1.7.4`，并创建对应实施记录。

## 8. 最终结论

**Review 结论：Fail。**

Phase 10 的 Events 主链、可靠性方向和真实集成矩阵总体成立，但 P1-01 表明旧 runtime watcher 仍可越过 generation 边界破坏当前有效 process record 与 MetricsMonitor；P2-01 又使声明的有界 shutdown 不具备硬返回上限。建议在继续 Phase 11 功能实现前，先通过 Phase-10-04 / `develop/1.7.4` 关闭 P1 和全部 P2，并同步修复 P3 查询合同。
