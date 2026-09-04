# GoPulse Phase 8 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-04 |
| 用户指定权威 Review 分支 | `develop/1.5.4` |
| Review 分支创建方式 | fetch 后远端不存在 `origin/develop/1.5.4`；从最新 `origin/main` 创建本地 `develop/1.5.4`，未推送 |
| Review 基线 | `a7b9d9404f2aeb58cc163596e40dec00dd4b1e3a`（`Merge pull request #78 from Ray-ymq/update`，Review 开始时与 `origin/main` 一致） |
| Phase 8 计划合入点 | `f737ffa21d6653737c37c7804b76d6d419d3336c`（PR #72） |
| Phase 8 已合入实现提交 | `ea3b910`（Phase-08-01 / PR #73）、`fa40b85`（Phase-08-02 / PR #75）、`058ff4d`（Phase-08-03 / PR #77） |
| Phase 8 完成记录 | `dcc40d8`，随后通过 PR #78 合入为 `a7b9d94` |
| 当前完成版本 | 根 `VERSION` 与 Frontend npm 元数据均为 `1.5.3`；本次只新增 Review 文档，不修改版本 |
| `develop/1.5.4` 治理状态 | Phase 8 总实施方案只分配到 `1.5.3`；`validate_branch.py` 对 `develop/1.5.4` 返回 1，当前分支尚不能按仓库规则推送 |
| 实际执行环境 | WSL2 Linux filesystem `/home/ray/GoPulse`，Go `1.26.7`，Node.js `24.20.0`，npm `11.19.0`，Docker `29.7.2` / Compose `v5.5.0`，Python `3.12.3` |
| Review 范围 | Phase 8 总/拆分方案与实施记录、Marshaller 配置/HTTP/Consumer/ownership/commit/Envelope/转换/VictoriaMetrics 客户端、Compose、Bash 生命周期与验收、CI、版本和分支治理 |
| Phase 8 变更规模 | 相对 Phase 7 最终基线 `60f9aa8`：46 个文件，4196 行新增、62 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. Marshaller 是否在 revoke/lost、HTTP acceptance 和 Kafka commit 竞态下保持 ownership fencing，不错误停止可恢复的 Consumer。
2. Kafka offset 是否只在 VictoriaMetrics 接受且当前 generation 仍有效后推进，永久异常、暂时失败和重放语义是否与实施方案一致。
3. 配置、HTTP listener、日常生命周期和验收脚本是否对声明支持的输入产生一致行为。
4. Phase 8 的实现记录、测试证据、版本和分支治理是否足以支持 `develop/1.5.4` 整改批次。

Review 没有读取、修改、暂存或提交工作区原有的未跟踪文件 `使用指南.md`。

## 2. 总体结论

Phase 8 已交付方向正确且经过真实隔离环境验证的指标闭环：

- 独立 Marshaller Go module 使用正式 Kafka group、关闭自动 commit，并按当前单 partition Topic 每次处理一条 record。
- Envelope value 具有 1 MiB、UTF-8、递归重复 key、未知字段、尾随 token、key/message ID、schema/type/source/timestamp、payload、family/kind/label/value 和样本集合二次校验。
- 合法 record 被确定性转换为 Prometheus text import；VictoriaMetrics 写入要求 Basic Auth、空 `204 No Content`、有界响应读取且不跟随 redirect。
- 永久异常不写入 VictoriaMetrics并在有效 lease 下提交；暂时存储失败保留 offset 并执行可取消退避；同一 Envelope 重放保持相同 series、value 和毫秒 timestamp。
- 本次重新执行 `scripts/verify-marshaller.sh`，真实 Redis success、`target_unavailable`、恢复、三类永久异常继续、VM/Kafka/进程恢复、正式 group offset、确定性重放、访问隔离和资源清理全部通过。
- Marshaller format/unit/vet/race、脚本语法与无 Docker self-test、Compose 渲染、版本元数据和 25 项脚本 CI unittest 均通过。

但是，当前实现仍不能通过 Phase 8 Review：

1. **ownership 在 commit 期间被 revoke/lost 时会被错误归类为普通 commit failure。** `Processor.commit` 在 Committer 返回错误后没有先复查 lease；由 lease cancellation 触发的 `context.Canceled` 被统一改写为 `ErrCommitFailed`。`Kafka.Run` 随后设置全局 `halted` 并退出消费循环，主进程保持 `/health=200`、`/ready=503`，不会自动加入新 generation。一次正常 rebalance 若恰好落在最长 3 秒的 commit 窗口内，可使指标消费持续停止直到人工重启。
2. **配置接受 IPv6 loopback，但 HTTP listener 和日常探测不会正确构造 IPv6 地址。** `MARSHALLER_HTTP_HOST=::1` 通过 `net.ParseIP(...).IsLoopback()`，随后被拼成非法的 `::1:9093`，而不是 `[::1]:9093`；声明合法的配置会启动失败。
3. **`develop/1.5.4` 没有权威批次分配。** Phase 8 总实施方案只有 `develop/1.5.1`～`develop/1.5.3`，当前分支校验明确失败，不能直接作为可推送整改分支。
4. **`MARSHALLER_KAFKA_POLL_TIMEOUT` 是无效配置。** 它出现在 `.env.example`、`dev.sh` 和 `Config` 中并接受范围校验，但没有传入 `NewKafka` 或用于 `PollRecords`，修改该值不会改变运行行为。

本次记录 **1 项 P1、2 项 P2、1 项 P3**。P1-01 直接破坏 Phase 8 的可靠 rebalance/ownership 主合同，并且现有定向测试与真实验收没有覆盖“commit 正在进行时 revoke/lost”的失败路径，应在后续 Phase 依赖该 Consumer 前优先关闭。

## 3. 风险分级

| 级别 | 数量 | 定义 | 本次结论 |
| --- | ---: | --- | --- |
| P0 | 0 | 数据破坏、凭据泄漏、任意代码执行或全系统不可用 | 未发现 |
| P1 | 1 | 核心数据链路可持续停止、可靠性主合同失效或需要人工恢复的重大缺陷 | 必须修复后再通过 |
| P2 | 2 | 受支持配置失效、治理阻断或重要实现/合同不一致 | 应在 `1.5.4` 整改批次关闭 |
| P3 | 1 | 无效配置、测试/文档准确性或低风险可维护性问题 | 建议与整改一并关闭 |

## 4. Review Findings

### P1-01：commit 期间的 ownership cancellation 被误判为 commit failure，正常 rebalance 可永久停止 Consumer

**位置**

- `marshaller/internal/consumer/processor.go:113-124`
- `marshaller/internal/consumer/kafka.go:107-113`
- `marshaller/cmd/marshaller/main.go:75-81`
- `marshaller/internal/consumer/processor_test.go:104-119`
- `dev/imple/Phase-08/Phase-08-总实施方案.md:203-205,218,497`

**事实**

`Processor.commit` 先验证 lease，再把根 context 和 lease context 合并后执行 Committer：

```go
if !lease.Valid() {
    return ErrOwnershipLost
}
commitCtx, cancel := mergeContext(ctx, lease.Context())
defer cancel()
if err := p.Committer.Commit(commitCtx, record); err != nil {
    return ErrCommitFailed
}
if !lease.Valid() {
    return ErrOwnershipLost
}
```

当 `OnPartitionsRevoked` 或 `OnPartitionsLost` 在 commit 进行期间取消 lease 时，Committer 通常因合并 context 取消而返回错误。当前代码不区分该错误是 ownership cancellation 还是独立 Kafka commit failure，直接返回 `ErrCommitFailed`。上层随后执行：

```go
k.halted.Store(true)
return fmt.Errorf("partition %d halted: %w", record.Partition, err)
```

主程序收到该错误后只设置 `exitCode=1` 并把 `consumerDone` channel 置为 `nil`，不会结束 HTTP server，也不会重新创建 Consumer。最终状态是进程仍存活、readiness 永久失败、正式 group 不再处理 record，直至外部重启进程。

**本次专项复现**

Review 使用 Go overlay 注入一个 Committer：在 `Commit` 内撤销当前 partition，等待传入 context 被取消并返回 `ctx.Err()`。期望结果是 `ErrOwnershipLost`，实际结果为：

```text
--- FAIL: TestReviewProbeCommitCanceledByRevoke
    review_probe_test.go:22: rebalance cancellation classified as offset commit failed, want ErrOwnershipLost
FAIL
```

该复现没有修改仓库文件。现有测试只覆盖：Writer 返回前 revoke、普通 commit error、退避期间 lost ownership；没有覆盖 Committer 正在等待时 revoke/lost。完整 `verify-marshaller.sh` 只做单 Consumer broker restart/group rejoin，也没有把 partition 在 commit 窗口内转移给第二个 group member，因此无法发现此问题。

**影响**

- 一次合法 group rebalance、成员变更或 broker/group generation 变化如果命中 commit 窗口，可让 Phase 8 指标链路持续停止。
- offset 不会被错误越过，数据源仍可从 committed offset 重放，但恢复依赖人工/外部进程重启，不符合阶段方案的可靠 rebalance 和自动恢复结论。
- `/health=200` 会继续表达进程存活；若运维只观察 liveness，消费停止可能长期存在。
- 实施记录所称“commit failure、revoke/lost 与延迟 HTTP 竞态已由注入式测试证明”覆盖不完整。

**建议修复**

- Committer 返回错误后先复查 `lease.Valid()`、`lease.Context().Err()`；由 revoke/lost cancellation 导致的错误返回 `ErrOwnershipLost`，仅在 lease 仍有效时把错误归类为 `ErrCommitFailed`。
- 为 revoke 与 lost 各增加一个代表性 commit-in-flight 测试，断言旧 generation 不提交、Consumer 不设置永久 halted，并能够在新 assignment 后从 committed offset 重取。
- 保留独立、真实的 commit failure 行为：ownership 仍有效且 commit 结果失败/不确定时不得继续后续 offset；明确是自动重新建 session，还是保持 unready 并要求受控重启，使实现、方案、README 和运维入口一致。
- 在一个隔离真实 Kafka 场景中使用第二个相同 group 成员触发 partition 转移，覆盖当前 broker restart 未覆盖的成员间 rebalance。

**关闭条件**

- commit 进行期间 revoke/lost 的定向测试稳定返回 ownership-lost 语义，不进入永久 halted 状态。
- 旧 generation 不提交，新的 owner 能从最后 committed offset 继续。
- 独立 Kafka commit failure 仍不越过当前 record，且恢复策略有实现和验收证据。

---

### P2-01：Marshaller 接受 IPv6 loopback 配置，但 listener 与日常探测生成非法地址

**位置**

- `marshaller/internal/config/config.go:63-65`
- `marshaller/internal/httpserver/server.go:23-29`
- `scripts/dev.sh:900-902`
- `marshaller/README.md:34`

**事实**

配置验证接受任意 `net.ParseIP` 可识别的 loopback IP，因此 `MARSHALLER_HTTP_HOST=::1` 是合法输入。HTTP server 之后使用字符串拼接：

```go
Addr: host + ":" + strconv.Itoa(port)
```

得到 `::1:9093`，而 Go TCP listener 的 IPv6 host/port 地址必须为 `[::1]:9093`。`scripts/dev.sh` 同样把 readiness URL 拼成 `http://::1:9093/ready`。

**本次专项复现**

Review 使用 Go overlay 检查 `httpserver.New("::1", 9093, ...)` 的最终地址，结果为：

```text
--- FAIL: TestReviewProbeIPv6LoopbackAddress
    review_probe_test.go:8: IPv6 loopback rendered as "::1:9093", want "[::1]:9093"
FAIL
```

该复现没有修改仓库文件。

**影响**

- 一个被配置层和 README 接受的 loopback IP 会在 listener 启动时失败。
- 日常 `dev.sh` 即使 listener 修复，也仍会因 readiness URL 缺少 IPv6 bracket 而报告启动失败。
- 当前 unit、self-test 和完整验收固定使用 `127.0.0.1`，不会覆盖此配置分支。

**建议修复**

- HTTP listener 使用 `net.JoinHostPort(host, strconv.Itoa(port))`。
- Bash 生命周期通过 Python `urllib.parse`、统一 helper 或明确 bracket 逻辑生成 IPv6 URL；不要继续直接拼接 host/port。
- 增加一个 IPv4 loopback 和一个 IPv6 loopback 的最小配置/listener 地址测试。
- 如果项目决定只支持 IPv4，应在配置层明确只接受 IPv4 loopback并同步 README，而不是接受后启动失败。

**关闭条件**

- `127.0.0.1` 和 `::1` 的声明行为一致：均可启动并完成 `/health`、Bearer `/ready`；或者配置、文档和验证一致拒绝 IPv6。
- 默认 IPv4 日常生命周期无回归。

---

### P2-02：`develop/1.5.4` 未被 Phase 权威方案分配，当前 Review 分支不能通过治理门禁

**位置**

- `dev/imple/Phase-08/Phase-08-总实施方案.md:49-57`
- `scripts/ci/validate_branch.py`

**事实**

Phase 8 权威分配表当前只有：

- `Phase-08-01` → `1.5.1` / `develop/1.5.1`
- `Phase-08-02` → `1.5.2` / `develop/1.5.2`
- `Phase-08-03` → `1.5.3` / `develop/1.5.3`

本次按用户指定从最新 `origin/main` 创建了本地 `develop/1.5.4`，但执行：

```bash
python3 scripts/ci/validate_branch.py --branch develop/1.5.4 --base-ref origin/main
```

实际结果：

```text
ERROR: develop/1.5.4 must map to exactly one authoritative allocation; found 0
```

**影响**

- 当前本地 Review 文档可以提交，但分支不能通过 Branch governance，也不应直接推送或创建普通开发 PR。
- 若直接在该分支实施整改，会违反每个执行批次必须有权威 target version/branch 分配的规则。

**建议修复**

- 在开始 `1.5.4` 产品整改前，先在允许的规划流程中为 Phase 8 Review 整改新增明确批次，例如 `Phase-08-04` → `1.5.4` / `develop/1.5.4`，并定义与本报告 findings 对应的验收标准。
- 若项目不希望重开 Phase 8，则应在下一阶段权威总实施方案中分配实际整改版本/分支，并重新创建符合该分配的开发分支；不要静默把已创建/已推送分支改号。
- 分配完成后重新执行 branch validator，再决定是否推送本地分支。

**关闭条件**

- `develop/1.5.4` 在一个且仅一个权威实施批次中有明确分配。
- branch validator 对最新远端 `main` 返回 0。
- 整改提交与版本更新遵循该批次分配。

---

### P3-01：`MARSHALLER_KAFKA_POLL_TIMEOUT` 被校验和传递，但完全不影响 Consumer

**位置**

- `.env.example:56`
- `scripts/dev.sh:88-92,111-114,148`
- `marshaller/internal/config/config.go:28,97`
- `marshaller/cmd/marshaller/main.go:41,47`
- `marshaller/internal/consumer/kafka.go:23-39,82-115`

**事实**

`MARSHALLER_KAFKA_POLL_TIMEOUT` 有默认值 `1s`，配置层把它解析到 `Config.KafkaPollTimeout` 并限制在 `100ms`～`10s`。日常生命周期也读取并传递该环境变量。然而：

- `main` 调用 `NewKafka` 时没有传入 `cfg.KafkaPollTimeout`。
- `Kafka` struct 不保存 poll timeout。
- `Run` 始终执行 `k.Client.PollRecords(ctx, 1)`，只使用进程根 context。

因此，把该配置设置为 `100ms`、`1s` 或 `10s` 不会产生任何运行差异。

**影响**

- `.env` 和配置校验向运维人员暴露了一个无效控制项。
- 实施记录难以准确说明 poll、rebalance callback 和 shutdown 的时间边界来自哪里。
- 后续排查 Consumer 响应时间时可能错误依赖该配置。

**建议修复**

- 如果不需要应用层 poll timeout，删除该环境变量、Config 字段、默认值和生命周期传递，并在文档中说明 PollRecords 只由根 context 取消。
- 如果需要该配置，则在每次 poll 创建有界 context，并明确 timeout 只是周期性返回/检查边界，不能被误用为 record 处理或 rebalance blocking 机制。
- 增加一个最小测试证明配置变化确实改变预期行为，或证明删除后不存在陈旧引用。

**关闭条件**

- 仓库不再存在“可配置但无效果”的 poll timeout。
- 配置、代码、README、`.env.example` 和 `dev.sh` 对 poll 行为描述一致。

## 5. 已执行验证

### 5.1 通过

```bash
# 获取权威远端并创建 Review 分支
git fetch --all --prune
git switch -c develop/1.5.4 origin/main

# Marshaller 固定 package 门禁
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)

# 脚本、Compose、版本和 CI helper
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
python3 scripts/ci/validate_versions.py
python3 -m unittest discover -s scripts/ci -p 'test_*.py'

# Phase 8 真实主验收
scripts/verify-marshaller.sh

git diff --check
```

结果摘要：

- Marshaller 8 个 package 的普通测试和 race 测试全部通过，`go vet` 与 gofmt 通过。
- `verify-marshaller.sh --self-test` 通过，拒绝 9 个不安全配置/project/query/port 场景，未访问 Docker。
- 25 项 `scripts/ci` unittest 通过；Compose 渲染和版本元数据校验通过。
- 完整 Marshaller acceptance 通过，实际记录了真实 success 10 families/11 samples、三类永久异常、Redis/VM/Kafka/进程恢复、正式 group offset、同 Envelope 重放和隔离资源清理。
- 验收完成后没有残留 `gopulse-marshaller-*` container、network、volume 或监听端口。

### 5.2 预期失败并形成 finding

```bash
# Go overlay：在 Committer 内 revoke 当前 lease，并返回被取消的 context
(cd marshaller && go test -overlay=/tmp/phase8_overlay.json \
  -run TestReviewProbeCommitCanceledByRevoke -count=1 ./internal/consumer)
```

结果：返回 `offset commit failed`，而不是 `ErrOwnershipLost`，形成 P1-01。

```bash
# Go overlay：检查被配置层接受的 IPv6 loopback listener 地址
(cd marshaller && go test -overlay=/tmp/phase8_ipv6_overlay.json \
  -run TestReviewProbeIPv6LoopbackAddress -count=1 ./internal/httpserver)
```

结果：地址为 `::1:9093`，而不是 `[::1]:9093`，形成 P2-01。

```bash
python3 scripts/ci/validate_branch.py \
  --branch develop/1.5.4 --base-ref origin/main
```

结果：`develop/1.5.4` 权威分配数量为 0，形成 P2-02。

上述两个 Go overlay probe 文件均位于 `/tmp`，执行后已删除，没有加入项目或提交。

### 5.3 未重复执行

本次没有重复执行 Frontend、Backend、Router、Monitor、Redis Exporter 和完整 business acceptance。原因是 Review 未修改这些组件，Phase-08-03 实施记录已在最终 `1.5.3` 提交上记录其固定门禁和业务隔离矩阵，本次重新执行的完整 Marshaller 主验收也没有观察到需要扩大回归范围的跨组件失败。若整改 P1-01 涉及 Consumer/ownership/commit，完成时应按新批次验收标准重跑 Marshaller package 门禁、定向 rebalance/commit 测试和完整 `verify-marshaller.sh`；只有共享生命周期或业务依赖发生变化时再扩展到对应回归。

## 6. 建议整改顺序

1. **先补权威批次分配。** 在规划流程中为 `1.5.4` / `develop/1.5.4` 建立唯一批次和验收合同，使分支可通过治理门禁。
2. **关闭 P1-01。** 修正 commit error 的 ownership 分类，增加 commit-in-flight revoke/lost 和真实双成员 rebalance 验收。
3. **关闭 P2-01。** 统一 listener 与生命周期 URL 的 IPv4/IPv6 host/port 构造；或明确收窄到 IPv4-only。
4. **关闭 P3-01。** 删除无效 poll timeout，或使其产生有定义、可验证的行为。
5. 在最终 diff 上一次性执行整改批次固定门禁，更新对应实施记录和 `VERSION` 到权威目标版本；门禁通过后提交并停止，不扩展到 Dashboard、查询 API、告警、集群或其他 Phase 9+ 能力。

## 7. 最终结论

Phase 8 的真实指标采集、Kafka 传输、Marshaller 严格转换、VictoriaMetrics 存储查询和主要故障恢复路径已经可运行，当前主验收也再次通过；未发现 offset 被提前提交、永久异常写入存储、凭据进入浏览器或业务 API 依赖可观测数据面的证据。

但 commit-in-flight ownership cancellation 的分类错误会在正常 rebalance 竞态中把可恢复的 ownership 变化升级为永久 halted Consumer，直接违背 Phase 8 的可靠消费主合同。再加上 IPv6 loopback 配置失效和 `develop/1.5.4` 治理未分配，本次结论为 **Fail**。

在 P1-01、P2-01、P2-02 关闭且固定整改门禁通过前，不应把本报告视为 Phase 8 实现 Review 通过证明。
