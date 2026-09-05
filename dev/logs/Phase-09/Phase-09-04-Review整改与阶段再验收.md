# Phase-09-04：Review 整改与阶段再验收实施记录

- 实施日期：2026-09-05
- 目标版本：`1.6.4`
- 开发分支：`develop/1.6.4`
- 基线：`origin/main` at `a01db909d7efca689e3d94e35a94c739b54fb6da`，基线版本 `1.6.3`
- Review 报告提交：`8c4f36e`（`docs: add Phase 9 implementation review`）
- 完成状态：Review findings 已关闭，本地固定门禁与真实隔离再验收通过；待提交、推送、远程门禁与合入

## 1. 实际完成工作

### 1.1 P1-01：日志 shipper 关闭线性化

- 用 `stateMu` 串行化 `Enqueue` 的关闭检查、非阻塞入队与 `Close` 的关闭状态转换。
- `Close` 只有在取得同一互斥锁后才把 shipper 标记为关闭并通知 worker；因此关闭线性化点之前返回成功的队列项一定已进入队列，关闭 drain 会接管它，关闭线性化点之后的新 enqueue 一律返回 `false`。
- 增加受控交错测试：enqueue 在持锁的测试钩子处暂停，同时启动 `Close`，随后释放 enqueue，证明已接受记录被发送且关闭后记录被拒绝。

### 1.2 P2-02：queue-full 节流与 retry jitter

- queue-full 状态只在首次进入时输出一条 warning；队列再次接受记录时输出一次恢复日志并重置状态，持续满队列不再按每条业务日志放大状态日志。
- retry delay 在当前指数退避值的 ±20% 窗口内加入随机 jitter，并始终夹在 `[RetryMin, RetryMax]`；随机源通过 `io.Reader` 测试注入，熵读取失败时安全退回当前受限 delay。
- 增加持续 queue-full 代表测试，证明十次连续失败只产生一条 `queue_full`，恢复后只产生一条 `queue_available`；增加注入熵的边界测试。

### 1.3 P2-01：Elasticsearch 外部合同重验证

- 删除 Marshaller Elasticsearch client 的进程生命周期 `ready` 缓存。每次日志写入前都幂等 PUT 固定 composable index template，并严格验证 `acknowledged=true`；`Ready` 在 cluster health 通过后也重新确保 template。
- 文档写入成功后、返回 writer 成功前，分别读取目标索引 mapping 和固定 read alias，验证 `dynamic: strict`、完整 canonical 字段类型以及 `gopulse-logs-v1-read`。任一验证失败均作为暂时存储失败返回，沿用 Processor 的不提前 commit 与重试语义。
- 扩展真实 `verify-logs.sh`：保留 Marshaller PID，使用 `--force-recreate --renew-anon-volumes` 把 Elasticsearch 替换为空集群，先确认 template 与日志索引均不存在，再投递固定 message ID。最终在同一 Marshaller 进程中重建 template、严格索引和 read alias，记录可通过 alias 查询。

### 1.4 P2-03 与 P3-01：治理和查询词汇

- 在 Phase 9 总实施方案中增加唯一权威分配 Phase-09-04 → `1.6.4` → `develop/1.6.4`，创建对应拆分方案并补充批次边界、验收标准、固定门禁和实施记录路径。
- Backend 管理员查询增加与 LogMonitor/Marshaller Schema v1 同步的 service/module/message 词汇，按已知组合验证过滤条件；`error_code` 限制为现有 `apperror` 稳定代码。
- 增加合法词汇、未知 module、未知 message、未知 error code 和不可能 service/module 组合的解析测试。
- 根 `VERSION`、Frontend `package.json` 与 `package-lock.json` 同步为 `1.6.4`。

## 2. 实际变更文件

- `backend/internal/observability/logship/shipper.go`
- `backend/internal/observability/logship/shipper_test.go`
- `backend/internal/logquery/logquery.go`
- `backend/internal/logquery/logquery_test.go`
- `marshaller/internal/elasticsearch/client.go`
- `marshaller/internal/elasticsearch/client_test.go`
- `scripts/verify-logs.sh`
- `dev/imple/Phase-09/Phase-09-总实施方案.md`
- `dev/imple/Phase-09/Phase-09-04-Review整改与阶段再验收.md`
- `dev/review/2026-09-04-Phase-9实现Review报告.md`
- `dev/logs/Phase-09/Phase-09-04-Review整改与阶段再验收.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`

工作区中用户已有的 `使用指南.md` 和 Phase 7 实施记录目录移动未被读取、修改、暂存或纳入本批提交。

## 3. 实际验证

### 3.1 直接受影响测试

以下命令均通过：

```bash
(cd backend && go test ./internal/observability/logship ./internal/logquery)
(cd backend && go test -race ./internal/observability/logship ./internal/logquery)
(cd marshaller && go test ./internal/elasticsearch ./internal/consumer)
(cd marshaller && go test -race ./internal/elasticsearch ./internal/consumer)
```

直接测试覆盖关闭交错、关闭后拒绝、queue-full 节流与恢复、jitter 边界、查询已知词汇、每次写入重新确保 template、目标索引 mapping/alias 验证，以及 writer 暂时失败不提前 commit 的既有 Processor 回归。

### 3.2 必要 Backend/Marshaller 回归

以下命令均通过：

```bash
(cd backend && go test ./... && go vet ./...)
(cd marshaller && go test ./... && go vet ./...)
```

未修改 Monitor、Router、Frontend 产品行为或公共依赖，因此未扩展到全仓测试、全模块 race 或重复 Phase-09-03 已通过且未受影响的业务/Marshaller 独立矩阵。

### 3.3 脚本、版本与分支治理

以下命令均通过：

```bash
bash -n scripts/verify-logs.sh
bash scripts/verify-logs.sh --self-test
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.4 --base-ref origin/main
git diff --check
```

最终版本校验输出 `Version metadata matches root VERSION.`；分支校验输出 `Branch governance passed for develop/1.6.4.`。

### 3.4 真实日志闭环与空集群替换

```bash
bash scripts/verify-logs.sh
```

真实隔离验收通过，使用随机 Compose project、随机 loopback 端口和临时凭据。代表性非敏感证据：

- Backend 请求日志分页：8 页、15 条，无重复且固定过滤合同保持有效。
- 帖子事件：`434460f5-ee34-4f73-9781-f6deb4207fc5`。
- 评论事件：`554fc83f-2132-476d-beb6-46df46ee6808`。
- Elasticsearch 故障窗口事件：`97f5a5aa-56e0-436c-8366-b5572651e896`。
- 停止/恢复记录：`33333333333333333333333333333333`。
- 同 ID 重放记录：`abcdef0123456789abcdef0123456789`。
- 空集群替换恢复记录：`44444444444444444444444444444444`，恢复索引 `gopulse-logs-v1-2026.09.05`。
- Elasticsearch 替换前后 Marshaller PID 未变化；替换后 template、read alias、strict mapping 与 alias 查询均恢复。
- 脚本退出后随机隔离资源按既有 cleanup 清理。

## 4. 实施偏差与实际问题

- Review 建议允许“每个新日期首次写入前或受控周期”重新验证 template。本批选择每次日志写入前幂等确保 template，并在每次成功文档写入后验证实际索引 mapping/alias；这是更直接的正确性边界，代价是每条日志增加 template 和索引合同请求。当前 Phase 9 未定义吞吐 SLA，此开销记录为后续可在保持外部状态正确性的前提下优化。
- 开发中首次运行 `go test ./internal/logquery` 因删除通用 token 检查后遗留未使用的 `strings` import 编译失败；移除该 import 后直接测试及后续全量 Backend 测试通过。
- 在版本仍为 `1.6.3` 时预跑 branch governance，按预期报告 `develop/1.6.4` 应对应 `VERSION=1.6.4`；完成版本更新后最终命令通过。
- 未读取第三方依赖源码，未增加通用审计、覆盖率任务或 Review 范围外重构。

## 5. 已知限制与后续项

- stdout 仍是第一输出；关闭超时、进程崩溃、message ID 熵失败和首次 queue-full 后被拒绝的远程副本仍可丢失，符合 Phase 9 已声明的 best-effort 源端边界。
- queue-full 使用状态转换节流而非周期摘要；持续故障只输出首次状态，恢复后可重新报告。若未来需要累计丢弃计数，应作为独立可观测需求设计。
- 每次写入执行 template ensure、mapping 检查和 alias 检查，优先正确性而非吞吐；后续优化不得重新引入跨集群生命周期的永久布尔缓存。
- Elasticsearch 若恰好在 template ensure 与文档写入之间被替换，写后合同验证会阻止 offset commit；若该竞态形成了不兼容现有索引，需要运维删除或修复该索引后重试，不会静默提交不可查询日志。
- 本地整改与验收完成不等于远程门禁或合入完成；推送后的远程状态应由后续 PR/自动化结果确认。
