# Phase 3-03：Review、集成验收与里程碑收口实施方案

> 执行序号：3 / 3
>
> 前置批次：Phase-03-01 与 Phase-03-02 已完成并通过验收
>
> 总方案来源：[Phase-03-总实施方案.md](Phase-03-总实施方案.md)

## 1. 批次目标

对 Phase 3 最终实现执行独立代码/架构 Review，最小整改实际阻断项，随后一次性完成搜索删除重建、增量故障恢复、Frontend 用户闭环和 Phase 0～3 必要业务回归，形成 Phase 3 与 Milestone 1 的可审计收口证据。

本批是固定 Review、集成验收与阶段收口批次，不新增搜索功能。Phase 3 产品开发版本完成后为 `0.4.3`；`1.0.0` 只在本批合入和远程门禁通过后按总方案 release-only 动作发布。

## 2. 前置条件

- Phase-03-01、Phase-03-02 已合入主远程最新 `main`，实施记录和远程门禁证据齐全。
- 根版本为 `0.4.2`，历史重建、Search API/Frontend、新帖 Indexer 和 Bash 生命周期可运行。
- 已 fetch 主远程并从最新 `main` 创建本批权威分支，未沿用前一批分支。
- WSL2 Linux filesystem 具备隔离 MySQL、Redis、RabbitMQ、ES、Backend、两个 Worker 和 Frontend 所需资源。
- 开始前保存日常栈与用户数据指纹，所有验收资源有随机且可验证的归属。

## 3. 实施范围

### 3.1 独立 Review

Review 固定覆盖：

- MySQL/ES 数据归属与 Search API hydration 是否存在反向事实依赖。
- rebuild lock、H1/H2、Bulk item error、alias 原子切换和精确删除是否会丢失或误删数据。
- posts + Outbox 原子性、事件/拓扑隔离、至少一次与 `_id` 幂等是否与文档一致。
- Outbox cleanup、lease budget 和 Worker shutdown 三项前置 finding 是否真实关闭。
- Query/cursor/timeout/body limit/错误映射、认证、Frontend 直连边界和日志脱敏是否安全。
- Bash/CI 的端口、容器、volume、PID 和中断清理是否保持归属保护。
- Phase 0～2 能力与 Phase 4 交接是否被 Phase 3 破坏。

在 `dev/review/` 生成 Phase 3 Review 报告，记录基线、范围、实际命令、证据、P0～P3 finding 和结论。不得把计划检查写成已执行，也不得用补测试代替已发现的生产缺陷。

### 3.2 Finding 处理边界

- P0/P1 必须最小修复并重跑受影响门禁；无法关闭时 Phase 3 保持未完成。
- 直接使总方案验收不成立的 P2 作为收口阻断；非阻断 P2/P3 登记责任、风险和建议，不扩大本批。
- 修复必须映射 Review finding 或失败验收；禁止机会性重构、依赖升级、视觉改版或搜索增强。
- 只为实际失败增加最低有效测试，同一事实不在 unit/integration/E2E 三层重复证明。

### 3.3 封闭集成验收与文档收口

- 在同一最终构建执行总方案第 12.3 节十项矩阵一次，包括重建、正常增量、Indexer/ES/RabbitMQ 故障、重复、并发重建和必要业务回归。
- 断言最终 Search API/Frontend，不以 index count 或 queue 清空单独替代用户可搜索。
- 检查成功、失败、Ctrl+C 和超时路径无验收进程、容器、网络、volume 或错误 alias 泄漏。
- 更新 README 的架构、配置、命令、搜索 API/UI、最终一致、重建、降级、恢复和限制。
- 核对总方案、三份拆分方案、三份实施记录、Review 报告、Git 历史、版本和分支分配。
- 仅在门槛通过后把总方案状态更新为完成，并如实写入远程合并/门禁的待办或证据。

### 3.4 `1.0.0` 发布准备

- 扩展 branch governance，使其从总方案 release-only 表唯一识别 `Milestone-01-Release → 1.0.0 → develop/1.0.0`，且不改变 Phase 批次校验。
- 准备发布清单：从 03-03 已验证 merge commit 创建 `develop/1.0.0`，只更新允许的版本/发布文档，执行版本与远程门禁后合入。
- 本批不得提前把 `VERSION` 改为 `1.0.0`，不得在 03-03 尚未合入时创建发布分支，也不得虚构 tag/release。

## 4. 实施边界与非目标

- 不新增事件类型、索引字段、Search API 参数、Frontend 搜索功能或业务实体。
- 不增加分词插件、推荐、建议、高亮、过滤、聚合、搜索历史或管理后台。
- 不执行大规模压测、ES/RabbitMQ 集群、Chaos 全排列或生产容量规划。
- 不提前实施 Phase 4 日志规范、Phase 9 日志链路或 Kubernetes。
- 不修改 PowerShell，不把 release-only 版本提交混入 `develop/0.4.3`。
- 不因剩余时间做未映射 finding 的清理或测试扩充。

## 5. 预计文件与交付物

```text
dev/review/<date>-Phase-3实现Review报告.md
dev/imple/Phase-03/Phase-03-总实施方案.md
dev/logs/Phase-03/Phase-03-03-Review集成验收与里程碑收口.md
README.md
scripts/verify-business.sh
scripts/ci/validate_branch.py
scripts/ci/test_validate_branch.py
backend/**（仅 Review/验收阻断 finding 的最小修复）
frontend/**（仅 Review/验收阻断 finding 的最小修复）
deploy/**（仅 Review/验收阻断 finding 的最小修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

## 6. 详细实施步骤

1. 冻结 Review 基线和 Phase 3 变更范围，核对两批实施记录、远程门禁与限制。
2. 按第 3.1 节审阅直接相关生产代码、测试、脚本和公共契约，形成有证据的报告。
3. 对 P0/P1 与阻断 P2 选择最小修复，先跑最低层失败复现，再跑受影响命令；非阻断项登记后停止。
4. 最终 diff 稳定后执行 Backend、Frontend、脚本治理和 Compose 固定门禁各一次。
5. 从空隔离环境顺序执行总方案十项矩阵；每项恢复后先确认系统回到可验收状态。
6. 复核索引删除、alias swap、两个 Worker/Broker/ES 重启和脚本中断不触碰日常资源。
7. 真实浏览器完成注册、发帖、评论、点赞、通知、历史搜索、新帖搜索、分页和详情跳转的必要旅程。
8. 查询实际远程 PR/check 状态，只记录存在的 head commit、检查名称和结论。
9. 更新 README、总方案状态、本批实施记录、Review 结论和 `0.4.3` 版本元数据。
10. 更新并测试 release-only 分支校验与发布清单，确认不允许重复分配或在 `update` 修改版本。
11. 提交并创建/合并 `develop/0.4.3` PR；远程门禁未通过时保持阶段未完成。
12. 合入与门禁通过后按总方案执行独立 `develop/1.0.0` release-only 动作，不再修改 Phase 3 产品代码。

## 7. 风险与控制

- **Review 演变成重做**：只处理有代码证据且映射验收的 finding，非阻断建议登记后停止。
- **矩阵误伤日常数据**：随机 project/database/port/path/volume 加 label/PID/alias 前缀多重校验。
- **异步断言偶发**：有界轮询和明确超时，输出有限 event/post/queue/index 诊断，不使用无界 sleep。
- **队列假通过**：索引场景最终由 Search API 或浏览器断言，并与 MySQL 事实核对。
- **重复执行扩大工期**：同一最终构建门禁一次；修复后只重跑可能受影响项目。
- **提前发布**：本批保持 `0.4.3`；发布分支只从已验证 main 创建且限制变更范围。

## 8. 固定完成门禁

最终 diff 上每项执行一次：

```bash
(cd backend && go test ./...)
(cd backend && go vet ./...)
(cd backend && go test -race ./...)
(cd backend && go test -count=1 -tags=integration ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/0.4.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

`scripts/verify-business.sh` 是一次性阶段级真实基础设施入口，必须包含总方案十项矩阵。因此不再另跑独立全量 Playwright、重复 Phase 2 故障脚本或额外 ES 压力测试。Review 修复的定向复现可在固定门禁前执行，实施记录需区分证据。

完整验收只能在 WSL2 Linux filesystem 和可确认归属的隔离资源执行。环境缺失时不得标记完成，也不得用 mock、源码阅读或新增边界单测代替真实 MySQL/RabbitMQ/ES/浏览器证据。

## 9. 验收标准

- Review 覆盖既定范围，结论有真实代码/命令/运行证据，P0/P1 为零。
- 03-01 历史重建/Search API/Frontend 与 03-02 增量 Indexer 可共同运行。
- 标题/正文查询、MySQL hydration、cursor generation、错误/降级和 Frontend 状态满足契约。
- Indexer/RabbitMQ/ES 暂停、重复、重启和恢复不丢 MySQL 帖子，最终索引收敛。
- 精确删除搜索索引后从 MySQL 恢复，代表性并发发帖不因 alias 切换遗漏。
- Outbox cleanup/lease 和 Worker shutdown 三项风险关闭且通知无回归。
- Phase 0～2 必要业务、缓存/消息降级和通知最终结果无回归。
- 验收未修改用户 `.env`、日常数据卷或非本次进程/容器/索引。
- PowerShell 仍为 `0.2.1`；Kafka、日志索引和后续组件未提前引入。
- 三份实施记录、总方案、Review、Git 历史和 `VERSION=0.4.3` 一致，远程门禁通过。
- release-only 校验能唯一识别 `develop/1.0.0`，但本批没有提前提交 `1.0.0`。

## 10. Phase 3 完成与 `1.0.0` 发布条件

只有第 9 节满足、Phase-03-03 合入主远程 `main` 且远程门禁成功，Phase 3 才完成。此时主线开发版本为 `0.4.3`，随后立即执行 release-only：

1. 从已验证的最新主远程 `main` 创建 `develop/1.0.0`。
2. 只更新根/Frontend 版本、Milestone 1 状态和发布说明。
3. 运行版本、分支、文档和远程门禁并合入 `main`。
4. 主远程 `main` 根 `VERSION=1.0.0` 后，业务系统 MVP 正式发布。

任一矩阵、远程门禁、实施记录或发布证据缺失时，不得写成完成。发布后停止 Phase 3 扩展，非阻塞增强移交后续规划。

## 11. Phase 4 交接

- 已发布的 `1.0.0` 业务系统 MVP 与可产生真实业务流量的搜索/异步链路。
- 独立可重建的帖子搜索索引；未来日志索引使用不同前缀、Mapping 和查询入口。
- Backend、Business Worker、Search Indexer 和重建命令的日志点及敏感信息边界。
- 完整 Review/故障证据；Phase 4 不重新定义搜索事实或增量消息语义。
