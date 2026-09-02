# Phase 3-03：集成验收与里程碑收口实施方案

> 执行序号：3 / 3
>
> 前置批次：Phase-03-01 与 Phase-03-02 已完成并通过验收
>
> 总方案来源：[Phase-03-总实施方案.md](Phase-03-总实施方案.md)

## 1. 批次目标

在同一最终构建上完成索引删除重建、增量故障恢复、Frontend 用户闭环和 Phase 0～3 必要业务回归，形成 Phase 3 与 Milestone 1 的收口证据。

本批是固定的集成验收与阶段收口批次，不新增搜索功能。Phase 3 开发版本完成后为 `0.4.3`；`1.0.0` 只在本批合入且远程门禁通过后，按总方案执行独立 release-only 动作。

## 2. 前置条件

- Phase-03-01、Phase-03-02 已合入主远程最新 `main`，各自实施记录和远程门禁证据齐全。
- 根版本为 `0.4.2`，历史重建、Search API/Frontend、新帖 Search Indexer 和 Bash 生命周期能够共同运行。
- 已 fetch 主远程并从最新 `main` 创建 `develop/0.4.3`，未沿用前一批分支。
- WSL2 Linux filesystem 具备隔离 MySQL、Redis、RabbitMQ、Elasticsearch、Backend、两个 Worker 和 Frontend 所需资源。
- 开始前保存日常栈、用户数据和进程指纹；所有验收资源使用随机且可验证的归属标识。

## 3. 实施范围

### 3.1 封闭集成验收

- 在同一最终构建执行总方案第 12.3 节十项矩阵一次，覆盖历史重建、正常增量、Search Indexer/Elasticsearch/RabbitMQ 故障、重复、并发重建和必要业务回归。
- 搜索场景最终断言 Search API/Frontend，并与 MySQL 事实核对；不以 index count、queue 清空或 Outbox `published` 单独替代用户可搜索结果。
- 检查成功、失败、Ctrl+C 和超时路径无验收进程、容器、network、volume 或错误 alias 泄漏。
- 只处理固定验收中已复现、会直接导致验收不成立的问题；修复后只重跑受影响项目。

### 3.2 文档与远程状态收口

- 更新 README 的架构、配置、命令、搜索 API/UI、最终一致、重建、降级、恢复和限制。
- 核对总方案、三份拆分方案、三份实施记录、Git 历史、版本和分支权威分配。
- 本地门禁通过时只记录本地完成状态；只有实际查询到 Pull Request 合入和远程门禁成功后，才能把总方案标记为 Phase 3 完成。

### 3.3 `1.0.0` 发布准备

- 扩展 branch governance，使其从总方案 release-only 表唯一识别 `Milestone-01-Release → 1.0.0 → develop/1.0.0`，同时保持每个 Phase 开发分支只能匹配一条权威批次分配。
- 准备发布清单：从 Phase-03-03 已验证并合入的主远程 `main` 创建 `develop/1.0.0`，只更新允许的版本/发布文档，执行版本、分支和远程门禁后合入。
- 本批不得提前把 `VERSION` 改为 `1.0.0`，不得在 Phase-03-03 尚未合入时创建发布分支，也不得虚构 tag、release 或远程结果。

## 4. 实施边界与非目标

- 不新增事件类型、索引字段、Search API 参数、Frontend 搜索功能或业务实体。
- 不增加分词插件、推荐、建议、高亮、过滤、聚合、搜索历史或管理后台。
- 不执行大规模压测、Elasticsearch/RabbitMQ 集群、Chaos 全排列或生产容量规划。
- 不提前实施 Phase 4 日志规范、Phase 9 日志链路、Docker 应用镜像或 Kubernetes。
- 不修改 PowerShell，不把 release-only 版本提交混入 `develop/0.4.3`。
- 不因剩余时间做与固定验收无关的清理、重构或测试扩充。

## 5. 预计文件与交付物

```text
dev/imple/Phase-03/Phase-03-总实施方案.md
dev/logs/Phase-03/Phase-03-03-集成验收与里程碑收口.md
README.md
scripts/verify-business.sh
scripts/ci/validate_branch.py
scripts/ci/test_validate_branch.py
backend/**（仅固定验收失败的最小修复）
frontend/**（仅固定验收失败的最小修复）
deploy/**（仅固定验收失败的最小修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义修改。如固定验收没有暴露生产代码问题，本批可以只更新验收、治理、版本和收口文档。

## 6. 详细实施步骤

1. 核对前两批实施记录、远程门禁、接受限制与日常资源指纹。
2. 最终 diff 稳定后执行 Backend、Frontend、脚本治理和 Compose 固定门禁各一次。
3. 从空隔离环境顺序执行总方案十项矩阵；每项故障恢复后先确认系统回到可验收状态，再进入下一项。
4. 复核搜索索引删除、alias swap、两个 Worker、RabbitMQ/Elasticsearch 重启和脚本中断不触碰日常资源。
5. 真实浏览器完成注册、发帖、评论、点赞、通知、历史搜索、新帖搜索、分页和详情跳转的必要旅程。
6. 对验收失败执行最小修复和定向复现，不扩展为一般性代码检查或重构。
7. 查询实际远程 Pull Request/check 状态，只记录存在的 head commit、检查名称和结论。
8. 更新 README、总方案状态、本批实施记录和 `0.4.3` 版本元数据。
9. 更新并测试 release-only 分支校验，确认 `develop/1.0.0` 唯一映射、普通 Phase 分支规则不放宽、`update` 仍禁止版本变更。
10. 提交并创建/合并 `develop/0.4.3` Pull Request；远程门禁未通过时保持阶段未完成。
11. 合入与远程门禁通过后按总方案执行独立 `develop/1.0.0` release-only 动作，不再修改 Phase 3 产品代码。

## 7. 风险与控制

- **矩阵误伤日常数据**：随机 project/database/port/path/volume 加 label、PID、alias 前缀多重校验。
- **异步断言偶发**：使用有界轮询和明确超时，输出有限 event/post/queue/index 诊断，不使用无界 sleep。
- **队列或索引假通过**：最终以 Search API/浏览器断言，并与 MySQL 事实核对。
- **重建并发误判**：在 alias 切换窗口真实创建代表性帖子，核对 H1/H2 与增量事件最终集合。
- **重复执行扩大工期**：同一最终构建固定门禁一次；修复后只重跑可能受影响项目。
- **提前发布**：本批保持 `0.4.3`；发布分支只从已验证 main 创建并限制变更范围。

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

`scripts/verify-business.sh` 是一次性阶段级真实基础设施入口，必须包含总方案十项矩阵，因此不再另跑重复的全量 Playwright、Phase 2 故障脚本或额外 Elasticsearch 压力测试。验收失败的定向复现可在固定门禁前执行，实施记录需区分定向证据和固定完成门禁。

完整验收只能在 WSL2 Linux filesystem 和可确认归属的隔离资源执行。环境缺失时不得标记完成，也不得以 mock、源码阅读或新增边界单测替代真实 MySQL/RabbitMQ/Elasticsearch/浏览器证据。

远程 Pull Request 必须通过仓库实际配置的 Branch governance、Backend、Frontend、Scripts and Compose、Integration 与自动合并相关门禁；只能记录实际观察到的名称和结果。

## 9. 验收标准

- Phase-03-01 的历史重建/Search API/Frontend 与 Phase-03-02 的增量 Search Indexer 可共同运行。
- 标题/正文查询、MySQL hydration、cursor generation、错误/降级和 Frontend 状态满足总方案契约。
- Search Indexer、RabbitMQ、Elasticsearch 暂停、重复、重启和恢复不丢 MySQL 帖子，最终索引收敛。
- 精确删除搜索索引后可从 MySQL 恢复，代表性并发发帖不因 alias 切换遗漏。
- Outbox cleanup、租约预算、Worker shutdown 与通知幂等无回归，搜索/通知拓扑没有串流。
- Phase 0～2 必要业务、缓存/消息降级和通知最终结果与 Phase 3 搜索共同运行。
- 验收未修改用户 `.env`、日常数据卷、非本次进程/容器/network 或用户其他 Elasticsearch 索引。
- PowerShell 仍为 `0.2.1`；Kafka、日志索引和后续组件未提前引入。
- 三份实施记录、总方案、Git 历史和 `VERSION=0.4.3` 一致，Pull Request 已合入且远程门禁通过。
- release-only 校验能唯一识别 `develop/1.0.0`，但本批没有提前提交 `1.0.0`。

## 10. Phase 3 完成与 `1.0.0` 发布条件

只有第 9 节满足、Phase-03-03 合入主远程 `main` 且远程门禁成功，Phase 3 才完成。此时主线开发版本为 `0.4.3`，随后执行 release-only：

1. 从已验证的最新主远程 `main` 创建 `develop/1.0.0`。
2. 只更新根/Frontend 版本、Milestone 1 状态和发布说明，以及本批已经验证为必要的治理元数据。
3. 运行版本、分支、文档和远程质量门禁并合入 `main`。
4. 主远程 `main` 根 `VERSION=1.0.0` 后，业务系统 MVP 正式发布。

任一矩阵、远程门禁、实施记录或发布证据缺失时，不得写成完成。发布后停止 Phase 3 扩展，非阻断搜索增强移交后续规划。

## 11. Phase 4 交接

- 已发布的 `1.0.0` 业务系统 MVP，以及可产生真实业务流量的搜索与异步链路。
- 独立、可重建的帖子搜索索引；未来日志索引使用不同前缀、Mapping 和查询入口。
- Backend、Business Worker、Search Indexer 和重建命令的日志点与敏感信息边界。
- 故障恢复和资源归属验收证据；Phase 4 不重新定义搜索事实来源或增量消息语义。
