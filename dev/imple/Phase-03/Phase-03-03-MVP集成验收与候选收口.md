# Phase 3-03：MVP 集成验收与候选收口实施方案

> 执行序号：3 / 4
>
> 前置批次：Phase-03-01 与 Phase-03-02 已完成并通过验收
>
> 后续批次：Phase-03-04（仅预留给用户主动发起的独立 Review）
>
> 总方案来源：[Phase-03-总实施方案.md](Phase-03-总实施方案.md)

## 1. 批次目标

在不新增搜索功能、不执行独立 Review 的前提下，将 Phase-03-01 的可重建搜索读闭环与 Phase-03-02 的可靠增量索引闭环放入同一最终构建，执行一次封闭的阶段级集成验收，形成可供用户后续独立 Review 的 `0.4.3` MVP 候选版。

本批只回答“已规划的 Phase 3 MVP 能力能否共同运行并满足既定验收合同”。它不预设 Review 检查项，不主动寻找计划范围外的问题，也不代替用户在 `develop/0.4.4` 上发起的独立 Review。`1.0.0` 仅在后续 Review 完成后按总方案执行 release-only 动作。

## 2. 前置条件

- Phase-03-01、Phase-03-02 已合入主远程最新 `main`，两份实施记录与远程门禁证据齐全。
- 根版本为 `0.4.2`，历史重建、Search API/Frontend、新帖 Indexer 与 Bash 生命周期均可运行。
- 已 fetch 主远程并从最新 `main` 创建 `develop/0.4.3`，未沿用前一批分支。
- 在 WSL2 Linux filesystem 中执行，具备隔离 MySQL、Redis、RabbitMQ、Elasticsearch、Backend、两个 Worker 与 Frontend 所需资源。
- 开始前记录 Git 状态与日常环境指纹；验收使用随机且可验证归属的端口、Compose project、数据库、volume、进程目录与索引前缀。

## 3. 实施范围

### 3.1 MVP 集成验收

- 在同一最终构建执行总方案第 12.3 节十项封闭矩阵一次。
- 从 Backend Search API 或真实浏览器断言最终搜索结果，不以索引计数、消息已确认或队列为空单独替代用户结果。
- 证明历史重建与实时增量可共同运行，重建期间的新帖子不因 alias 切换形成永久缺口。
- 证明 Elasticsearch、RabbitMQ 或 Search Indexer 暂停时已提交帖子仍由 MySQL 保存，依赖恢复后索引最终收敛。
- 证明搜索结果的命中与顺序来自 Elasticsearch，展示事实仍由 MySQL hydration 提供。
- 执行 Phase 0～2 的必要业务回归，确认搜索接入没有破坏注册、登录、帖子、评论、点赞和通知主链路。

### 3.2 候选版收口

- 更新 README 中与实际实现一致的架构、配置、启动、搜索 API/UI、最终一致、重建、降级、恢复和限制。
- 核对总方案、三份已执行拆分方案、三份实施记录、实际 Git 历史、版本和分支分配。
- 只在固定门禁与阶段矩阵通过后同步根 `VERSION`、Frontend npm 元数据为 `0.4.3`。
- 在实施记录中明确 `0.4.3` 是“Phase 3 MVP 候选完成、待用户主动 Review”，不得写成 Review 或 `1.0.0` 发布已完成。

### 3.3 验收失败处理

- 只修复使本批既定验收不成立的缺陷，并将修复映射到失败命令或阶段矩阵项目。
- 先在最低有效层复现并修复，再只重跑可能受影响的定向检查；最终 diff 稳定后执行固定门禁一次。
- 与 Phase 3 MVP 合同无关的观察项登记为后续信息，不在本批扩展调查、测试或生产改动。
- 若失败暴露需要改变公共契约、架构边界或批次范围，停止收口并先更新权威规划。

## 4. 实施边界与非目标

- 不执行代码或架构 Review，不生成 Review 报告，不定义 finding 等级与整改策略。
- 不新增事件类型、索引字段、Search API 参数、Frontend 搜索功能或业务实体。
- 不增加分词插件、推荐、建议、高亮、过滤、聚合、搜索历史或管理后台。
- 不执行大规模压测、集群建设、Chaos 全排列、通用依赖审计或覆盖率提升。
- 不提前实施 Phase 4 日志规范、Phase 9 日志链路、Kubernetes 或原生 PowerShell 支持。
- 不创建 `develop/0.4.4` 或 `develop/1.0.0`，不把后续 Review/发布改动混入本批。

## 5. 预计文件与交付物

```text
dev/imple/Phase-03/Phase-03-总实施方案.md
dev/logs/Phase-03/Phase-03-03-MVP集成验收与候选收口.md
README.md
scripts/verify-business.sh（仅补足已定义阶段矩阵所需能力）
backend/**（仅集成验收阻断缺陷的最小修复）
frontend/**（仅集成验收阻断缺陷的最小修复）
deploy/**（仅集成验收阻断缺陷的最小修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

实际没有变化的预计文件不得为了匹配清单而修改。

## 6. 详细实施步骤

1. 冻结 `develop/0.4.3` 基线，核对前两批实施记录、版本、远程门禁与已知限制。
2. 将总方案第 12.3 节十项矩阵逐项映射到现有脚本断言；只补足缺失的既定验收能力。
3. 在最终构建执行 Backend、Frontend、脚本治理、Compose 与版本固定门禁。
4. 从空隔离环境执行历史重建、正常增量、Indexer/ES/RabbitMQ 故障恢复、重复投递、索引删除重建和必要旧业务回归。
5. 复核成功、失败、超时和中断路径不遗留本次进程、容器、网络、volume 或错误 alias，也不触碰日常资源。
6. 对真实阻断失败做最小修复并运行受影响检查；没有阻断时不继续探索。
7. 更新 README、本批实施记录与 `0.4.3` 版本元数据，明确 MVP 候选状态和后续 Review 由用户主动发起。
8. 提交并推动 `develop/0.4.3` 的正常 PR/合并流程；远程门禁未通过时保持候选收口未完成。
9. 合入后停止 Phase 3 MVP 功能实施，不自动创建、启动或执行 Phase-03-04。

## 7. 风险与控制

- **矩阵误伤日常数据**：随机 project/database/port/path/volume 加 label、PID 与 alias 前缀校验。
- **异步断言偶发**：使用有界轮询和明确超时，输出有限 event/post/queue/index 诊断，不使用无界等待。
- **队列假通过**：最终断言 Search API 或浏览器结果，并与 MySQL 事实核对。
- **验收演变为 Review**：只处理既定矩阵的真实失败；不主动扩展检查面，不为未观察到的问题增加测试。
- **重复执行扩大工期**：最终构建的固定门禁各一次；修复后只重跑可能受影响的项目。
- **提前发布**：本批保持 `0.4.3`，后续 `0.4.4` 与 `1.0.0` 均需独立条件。

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

`scripts/verify-business.sh` 必须覆盖总方案第 12.3 节封闭矩阵，因此不再附加独立全量 Playwright、Phase 2 全量故障脚本、ES 压力测试或 Review 检查。完整验收只能在 WSL2 Linux filesystem 和可确认归属的隔离资源中执行；环境缺失时不得以 mock、源码阅读或新增长尾单测代替。

## 9. 验收标准

- 03-01 历史重建/Search API/Frontend 与 03-02 增量 Indexer 在同一最终构建共同运行。
- 标题/正文查询、MySQL hydration、排序、分页 cursor、错误降级和 Frontend 状态满足总方案契约。
- 正常增量及 Indexer、RabbitMQ、Elasticsearch 暂停/恢复后，MySQL 帖子不丢失且索引最终收敛。
- 重复投递保持一个逻辑文档；精确删除搜索索引后可从 MySQL 恢复，代表性并发发帖无永久遗漏。
- Outbox cleanup、lease budget、Worker shutdown 三项前置风险关闭，通知主链路无回归。
- Phase 0～2 必要业务能力共同运行，验收未修改用户 `.env`、日常数据卷或非本次资源。
- 第 8 节固定门禁与总方案第 12.3 节矩阵通过，无未关闭的 MVP 阻断问题。
- 实施记录只陈述实际工作与命令结果，根和 Frontend 版本均为 `0.4.3`。

## 10. 明确完成条件与交接

只有第 9 节全部满足、`develop/0.4.3` 合入主远程且远程门禁通过，才可标记“Phase 3 MVP 候选完成”。此时停止实现，将完整候选版交给用户决定何时发起 Phase-03-04。

Phase-03-04 的唯一预先约束是目标版本 `0.4.4` 与分支 `develop/0.4.4`。本方案不规定 Review 范围、检查命令、finding 处理或交付物，也不授权代理自动启动 Review；这些内容由用户届时的主动指令决定。
