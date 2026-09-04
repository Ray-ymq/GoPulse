# Phase 8-02：可靠消费、故障恢复与运维闭环实施方案

> 权威目标版本与开发分支以 `Phase-08-总实施方案.md` 第 3.2 节为准：本批对应 `1.5.2` / `develop/1.5.2`。
>
> 当前状态：未开始。

## 1. 批次目标

在 Phase-08-01 已交付真实 Kafka → Marshaller → VictoriaMetrics 查询闭环、generation fencing 和安全 commit 状态机的基础上，完成正式 Consumer 在真实 rebalance、Kafka/VictoriaMetrics 故障和进程重启条件下的恢复能力，并把 Marshaller/VictoriaMetrics 纳入安全、可恢复的日常 Bash 生命周期，形成：

```text
当前 partition ownership
→ 严格校验与确定性转换
→ VictoriaMetrics HTTP acceptance
→ ownership 再确认与有界 commit
→ revoke/lost/commit failure 时不越过未确认 offset
→ 从 committed offset 安全重取和确定性重放
```

本批是第二个实现批次，不是纯测试或阶段收口。必须交付真实故障恢复、恢复编排和运维生命周期增量；Phase-08-01 已通过的 ownership/commit 正确性不重新实现，Phase-08-03 只在最终能力上完成跨组件、业务隔离和里程碑验收。

## 2. 前置条件

- Phase-08-01 已合入主远程 `main`，远程门禁成功，实施记录与真实提交一致。
- 主远程根与 Frontend 版本均为 `1.5.1`；真实 success、target unavailable/recovery、代表性永久异常继续和 VictoriaMetrics 查询已经通过。
- Marshaller 已有正式 group `gopulse-marshaller-metrics-v1`、手动 offset、generation ownership lease、安全 commit、严格 Envelope/metrics validator、确定性 transformer、VictoriaMetrics client 和最小日常生命周期。
- 从最新主远程 `main` 创建 `develop/1.5.2`，不沿用 Phase-08-01、Phase 7 或 `update` 分支。
- 实施和真实验收在 WSL2 Linux filesystem 执行；开始前保存 Git、日常 Compose/PID/端口/volume/group 和插件根快照。

## 3. 实施范围

### 3.1 Partition ownership 的真实 rebalance 与恢复加固

- 以 Phase-08-01 已通过的 assignment generation ownership lease 为正确性基线；Writer、退避和 Committer 继续共享该 lease 的 context，不引入第二套状态机。
- `OnPartitionsRevoked`、`OnPartitionsLost` 立即取消对应 lease。旧 generation 即使随后收到 VictoriaMetrics `204` 也不得提交；lost 路径始终禁止提交。
- 不使用跨 VictoriaMetrics 写入或无限退避的 `BlockRebalanceOnPoll`，不通过丢弃客户端已缓冲 record 维持 poll。
- HTTP acceptance 后重新确认 ownership，再同步、有界提交当前 record；commit 失败、ownership 失效或结果不确定时停止推进该 partition，从最后 committed offset 安全重取。
- 同一 partition 始终只有一个业务 record 在途，不建立无界 channel、goroutine fan-out、本地 spool 或跨 record batch。

### 3.2 故障、重取与重复语义

- VictoriaMetrics 网络、timeout、认证、redirect、非 `204` 或非空响应继续属于暂时失败；当前 record 不提交，退避和日志有界。
- VictoriaMetrics 短时故障在 ownership 有效时由同一进程继续；故障跨越 assignment 时取消旧处理，由新 ownership 从 committed offset 重取。
- Kafka 停止或 broker 重启时 Marshaller health 保持存活、ready 失败并有界重连；恢复同一 Topic/group 后无需人工修改 offset。
- HTTP acceptance/commit 失败、revoke/lost 和进程终止允许相同 record 重放；相同输入必须逐 byte 生成相同 Prometheus 正文，并由 1ms dedup 保持查询稳定。
- 进程不得通过本地文件猜测 VictoriaMetrics 是否成功；未完成 commit 的 record 一律以 Kafka committed offset 为恢复事实。

### 3.3 日常生命周期与资源归属

- 在 Phase-08-01 最小接线基础上完善 `scripts/dev.sh`：等待 Kafka/Topic/VM，启动 Router、Marshaller，再启动 Monitor/Exporter；失败路径只停止本次强归属资源。
- 完善 `scripts/down.sh`：先停止 Monitor/Exporter，再停止 Marshaller、Router 和其他应用，最后停止本项目 Compose；保留日常 Kafka/VM volumes。
- 完善只读 `scripts/verify.sh`：核对 VM/Kafka container、volume、Topic、Router/Marshaller/Monitor PID、health/readiness、正式 group 和固定查询；不得 produce、消费/提交、修改 offset、删除数据或修复资源。
- 扩展 `scripts/verify-marshaller.sh` 默认模式，覆盖 Kafka/VM/Marshaller 故障恢复、明确未提交后的进程恢复、真实重复和正常/失败/中断清理。
- PID、project、container、network、volume、端口、group fixture、插件根和临时凭据都必须使用随机或固定强归属标识；unknown/mismatched 资源安全拒绝。

### 3.4 确定性状态机测试与真实验收

- 引用 Phase-08-01 通过注入 Consumer、Committer、Writer 和 ownership lease 已完成的 HTTP acceptance/commit 失败、revoke/lost、延迟 `204`、提交竞态、退避取消和 shutdown 确定性证据；相关实现变化时只重跑受影响测试，测试 seam 不得形成生产 HTTP 或普通运行配置。
- 真实 Kafka 验收强制 broker restart/rebalance，证明旧 ownership 不提交、正式 group 从 committed offset 继续。
- 真实 VictoriaMetrics 验收证明故障期间 committed offset 不推进、恢复后同一进程继续；另执行一次“明确未提交后终止 Marshaller → 恢复 VM → 重启并重取”。
- 原样重放一条真实 Envelope，证明 transformer bytes 相同、查询无同毫秒双点，并继续声明 at-least-once。
- 主矩阵前后核对 `vm_rows_invalid_total` 不增加，并查询预期时序；`204` 仍只表述为 HTTP transport acceptance。

### 3.5 CI、文档、版本与实施记录

- Marshaller CI 保留 Phase-08-01 ownership/commit 确定性测试，并增加适合 CI 的真实 Kafka/VictoriaMetrics 故障恢复场景。
- Scripts and Compose 门禁固定 rebalance/failure 自检、VM loopback/Basic/dedup/volume、Topic/group、PID 与清理归属。
- 更新根和 Marshaller README，以及必要的 Router/Monitor 交接说明，记录 at-least-once、ownership、恢复和运维边界。
- 将根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 更新为 `1.5.2`，创建本批实施记录。

## 4. 实施边界与非目标

- 不改变 Phase 7 Topic、record key/value、Envelope v1、Phase 8 正式 group、指标映射、标签或时间戳契约。
- 不新增 Backend/Frontend 指标查询入口，不接受 logs/events，不增加 Topic、DLQ、重放或 offset 管理 API。
- 不引入 exactly-once、Kafka transaction、持久本地去重、跨 record batch、多 partition 并发或 Marshaller 容器镜像。
- 不部署 VM cluster、vmagent、vmauth、多租户、高可用、TLS，不做聚合、告警、retention 或容量调优。
- 不执行完整社交业务和 Milestone 2 远程收口；这些由 Phase-08-03 完成。
- 不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 预计文件与交付物

```text
marshaller/internal/consumer/**
marshaller/internal/victoriametrics/**（仅故障/取消边界）
marshaller/internal/httpserver/**（仅 readiness/recovery 边界）
marshaller/README.md
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-marshaller.sh
deploy/compose.yaml（仅恢复/健康/归属阻断修复）
.env.example（仅本批新增或调整的可靠性配置）
.github/workflows/quality-gates.yml
.github/workflows/reusable-quality-gates.yml
scripts/ci/**（仅本批门禁）
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-08/Phase-08-02-可靠消费故障恢复与运维闭环.md
```

预计文件是允许边界，不要求制造无意义修改。若 Phase-08-01 已正确交付某项且本批无相关变化，引用其有效证据，不机械重写或重复测试。

## 6. 详细实施步骤

1. 核对 Phase-08-01 实施记录、合入提交、远程 checks、已知限制和当前正式 group offset；保存 Git 与日常资源快照。
2. 核对 Phase-08-01 的 Consumer、Committer、Writer 和 generation lease 实现/证据；未改变时直接引用，不增加生产故障开关。
3. 在真实 rebalance 与 broker restart 中验证 revoke/lost 取消、HTTP acceptance 后 ownership 再确认、commit 失败不越过和安全重取；只修复真实暴露的阻断缺口。
4. 扩展恢复编排和直接验收；若相关代码改变，只重跑受影响的 commit、延迟响应、ownership 竞态、退避取消和 shutdown 定向测试。
5. 对真实 Kafka 执行 broker restart/rebalance，对真实 VM 执行同进程恢复和明确未提交后的进程重启恢复。
6. 扩展重复重放、`vm_rows_invalid_total`、offset 和查询证据，确认 at-least-once 描述准确。
7. 完善 `dev.sh`、`verify.sh`、`down.sh` 与 `verify-marshaller.sh` 的启动、只读检查、停止和清理边界。
8. 增加直接 CI/脚本门禁并更新 README；最终 diff 稳定后执行第 8 节固定验证一次。
9. 更新根与 Frontend 版本为 `1.5.2`，创建本批实施记录，只暂存本批文件并提交 Pull Request。
10. 查询并记录真实远程 checks 与合入状态；未合入或失败时保持本批未完成。

## 7. 风险与控制

- **旧 owner 延迟提交**：所有异步结果在 commit 前复核 generation lease，revoke/lost 立即取消，竞态由确定性测试固定。
- **VM 长故障阻塞 rebalance**：不跨存储退避阻塞 rebalance；失去 ownership 后停止旧处理，从 committed offset 重取。
- **commit 失败后继续后续消息**：当前 partition 立即停止推进并重取，测试断言后续 offset 未被越过。
- **Shell 时序制造假证据**：精确状态由注入测试证明；真实 E2E 只使用可观察的 VM 停止、offset 未推进、进程终止和恢复步骤。
- **重放被误称 exactly-once**：同时记录重复 record、两次处理和查询结果，文档始终保留 at-least-once。
- **恢复测试误伤日常资源**：所有 stop/restart/delete 前确认随机 project、container ID、volume label、PID 和 group 身份。
- **本批退化为收口批次**：必须提交真实故障恢复编排或生命周期增量；纯跨组件/业务收口留给 Phase-08-03。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败修复后只重跑受影响命令或场景：

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd router && go test -count=1 ./...)
(cd monitor && go test -count=1 ./...)
(cd exporters/redis && go test -count=1 ./...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
git diff --check
```

完整社交业务回归不在本批重复执行；Phase-08-03 在最终构建与可观测故障条件下统一执行。

## 9. 验收标准

- revoke/lost 会取消旧 ownership，延迟响应和旧 generation 均不能提交 offset。
- HTTP acceptance/commit 失败不会越过当前 record；从 committed offset 重取后产生相同正文和稳定查询结果。
- Kafka、VM 和 Marshaller 故障/恢复均有界，不需人工修复 Topic、group 或 offset。
- VM 故障期间 committed offset 不推进；同进程恢复和明确未提交后的进程重启恢复均通过。
- `dev.sh → verify.sh → down.sh` 顺序、只读验证和资源清理符合强归属边界，日常 volumes 被保留。
- 精确竞态由确定性测试证明，真实 E2E 不依赖不可复现的 shell timing。
- 第 8 节固定验证和远程门禁通过，根与 Frontend 版本为 `1.5.2`，实施记录真实完整。

## 10. 明确完成条件

只有 ownership fencing、commit 失败停止推进、Kafka/VM/Marshaller 故障恢复、确定性重放、生命周期和资源安全全部通过，且 Phase-08-02 Pull Request 已合入主远程 `main`、远程门禁成功，才可标记本批完成。

## 11. 下一批交接

- 已在真实 Kafka 与注入状态机中验证的 partition ownership、revoke/lost、commit 和安全重取语义。
- 已在真实 VM/Kafka/Marshaller 故障条件下验证的同进程与进程重启恢复能力。
- 完整日常生命周期、只读验证、强归属清理和 CI 门禁。
- Phase-08-03 只执行最终跨组件矩阵、业务/访问隔离、文档版本和 Milestone 2 远程收口；除真实复现的阻断问题外不增加产品能力。
