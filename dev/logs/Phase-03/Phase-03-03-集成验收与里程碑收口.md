# Phase 3-03：集成验收与里程碑收口实施记录

## 1. 执行摘要

- 执行分支：`develop/0.4.3`。
- 基线：2026-09-02 fetch 后的 `origin/main=3fa8230cbd7ccc0bde17b1227767c27df959d9e4`，根与 Frontend 版本均为 `0.4.2`。
- 目标版本：`0.4.3`；本批没有创建 `develop/1.0.0`，也没有提交 `1.0.0`、tag 或 release。
- 前置远程证据：GitHub API 显示 PR #49 于 2026-09-02 16:31:01 UTC 合入，最终 head 为 `a2fb57850ecabe4bdc963902f924c41a01682d29`，merge commit 为 `3fa8230cbd7ccc0bde17b1227767c27df959d9e4`。该 head 的 `Quality gates before PR / Branch governance`、`Backend`、`Frontend`、`Scripts and Compose`、`Integration` 和 `Open PR and enable auto-merge` 均为 `success`。
- 本地结论：固定 Backend、Frontend、治理、Compose、真实 integration 和完整隔离业务验收全部通过；当前分支尚未 push 或创建 Pull Request，故只记录 Phase-03-03 本地完成，Phase 3 与 Milestone 1 尚未完成。

## 2. 实际完成工作

### 2.1 封闭矩阵入口收口

- 将 `scripts/verify-business.sh` 的默认完整模式收口为同一隔离 Compose project、同一最终构建和同一应用进程集合上的 Phase 0～3 验收入口。
- 默认完整模式依次执行基础 API/浏览器旅程、历史搜索删除重建、增量搜索故障恢复、Phase 2 Worker/Outbox 可靠性矩阵、Backend/缓存重启和 Redis 降级恢复；不再要求操作者分别运行两个搜索定向模式来拼接阶段证据。
- 在历史搜索真实验收中补充空白 `q`、越界 `limit`、损坏 cursor、跨 query cursor、跨 generation cursor、响应数量上限和脱敏 `search_unavailable` 断言。
- 保留 `--search-rebuild` 与 `--search-live` 作为失败后的定向复现入口；本批没有增加新的搜索产品功能。

### 2.2 Milestone 1 release-only 分支治理

- 扩展 `scripts/ci/validate_branch.py`，除 Phase 批次分配表外，还解析总方案 release-only 表中的 `Milestone-XX-Release` 行。
- `develop/1.0.0` 现在必须且只能匹配一条权威 release-only 分配，并仍须与根 `VERSION` 一致。
- 增加治理测试，验证合法 release-only 分支可通过、release 与普通 Phase 分配重复时被拒绝；既有普通 `develop/x.x.x` 唯一映射和 `update` 禁止修改 `VERSION` 的规则保持不变。

### 2.3 文档与版本

- README 更新为 `0.4.3`，说明完整 Phase 0～3 隔离矩阵、搜索架构/命令/最终一致与故障恢复边界，以及 Phase-03-03 合入和远程门禁之前不得标记阶段完成。
- 总方案权威状态表更新为 Phase-03-01、Phase-03-02 已由 PR #49 合入；Phase-03-03 为本地完成、待 PR 与远程门禁，并记录实际查询到的 commit/check 证据。
- 根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 同步为 `0.4.3`。
- 未修改任何 PowerShell 文件、Backend/Frontend 产品功能、索引字段、事件类型或公共 API。

## 3. 变更文件

- `README.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `scripts/verify-business.sh`
- `scripts/ci/validate_branch.py`
- `scripts/ci/test_validate_branch.py`
- `scripts/ci/test_verify_business.py`
- `dev/imple/Phase-03/Phase-03-总实施方案.md`
- `dev/logs/Phase-03/Phase-03-03-集成验收与里程碑收口.md`

## 4. 实际验证

### 4.1 资源归属与前后指纹

- 执行前保存本地 `.env` SHA-256：`e01905492fd0037ecaef3a76441b44b72a5739992bcbac9bd1033fac05bf2511`。
- 执行前已存在日常 `gopulse` project，以及其他任务留下的 `gopulse-phase0203-integration` project；本批将其作为用户/既有资源，只纳入快照比较，未停止、修改或删除。
- 完整验收使用随机 project `gopulse-acceptance-939e97f52835`；独立 integration gate 使用随机 project `gopulse-integration-9517d0f00167`。
- 两次验证结束后，上述随机 project 的 container、network 和 volume 均不存在；`.env` 哈希未变化，既有 `gopulse` 与 `gopulse-phase0203-integration` 资源状态保持不变。

### 4.2 最终固定完成门禁

以下命令在最终生产代码/脚本差异上各执行一次并通过：

- `(cd backend && go test ./...)`：通过。
- `(cd backend && go vet ./...)`：通过。
- `(cd backend && go test -race ./...)`：通过，未报告数据竞争。
- `(cd backend && go test -count=1 -tags=integration ./...)`：在全新 disposable MySQL、Redis、RabbitMQ、Elasticsearch 上通过；先执行全部 upward migrations，所有 Backend package 通过，验收 project 随后删除。
- `(cd frontend && npm test -- --run)`：通过，9 个测试文件、44 项测试。
- `(cd frontend && npm run typecheck)`：通过。
- `(cd frontend && npm run build)`：通过，完成 `vue-tsc --noEmit` 与 Vite production build。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，21 项测试。
- `python3 scripts/ci/validate_versions.py`：通过，根与 Frontend 版本一致为 `0.4.3`。
- `python3 scripts/ci/validate_branch.py --branch develop/0.4.3 --base-ref upstream/main`：通过；当前开发分支唯一匹配 Phase-03-03。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- `scripts/verify-business.sh --self-test`：通过；接受 1 个合法目标并拒绝 6 个不安全目标，未访问 Docker。
- `scripts/verify-business.sh`：通过，见第 4.3 节。
- `git diff --check`：通过。

### 4.3 同一最终构建的 Phase 0～3 隔离矩阵

`scripts/verify-business.sh` 从空随机环境完成迁移并启动 MySQL、Redis、RabbitMQ、Elasticsearch、Backend、Business Worker、Search Indexer 与 Frontend，最终退出码为 0。实际证据包括：

- 基础真实 Chromium 套件中注册、发帖、评论、点赞、退出/登录和两用户通知闭环通过；未提供定向搜索 seed 的两个用例按设计跳过，随后在对应搜索流程中分别真实执行并通过。
- 历史搜索通过标题和正文命中，无关词为空；分页无重复，评论/点赞后的 MySQL hydration 返回最新计数与 `liked_by_me`。
- 精确删除活动物理索引后 Search API 返回脱敏的 `503 search_unavailable`；从 MySQL 重建后历史结果恢复，跨 generation cursor 被拒绝，验收创建的无关 Elasticsearch 索引仍存在。
- 搜索页面真实 Chromium `search-rebuild` 用例 1 项通过，完成搜索结果显示、空状态和详情跳转。
- 新帖子与最小 `post.created` Outbox 原子存在并自动可搜索；通知与搜索 queue binding 不串流。
- Search Indexer 停止期间消息保留，重启后收敛；同一事件重复投递后仍只有一个逻辑文档。
- RabbitMQ 停止期间发帖成功且 Outbox 保留，恢复后自动发布并索引；Elasticsearch 停止时发帖成功、搜索降级，恢复后 retry 收敛。
- alias 重建期间并发创建的帖子最终可搜索；搜索页面真实 Chromium `search-live` 用例 1 项通过。
- Phase 2 可靠性矩阵继续通过：正常通知、Worker 暂停恢复、broker outage + Backend restart、unacked redelivery、重复幂等、retry/dead 后续连续性和 RabbitMQ durable restart 均成立。
- Backend/缓存重启与 Redis 故障恢复通过；最终 `/health`、`/ready` 和认证业务 API 正常。
- cleanup trap 删除且仅删除经 token、Compose label、端口和进程身份验证的验收资源，并通过日常栈前后快照比较。

## 5. 失败、修复与方案偏差

- 默认完整模式原先只执行 Phase 0～2 业务可靠性矩阵，搜索重建和增量故障验收只能通过两个定向参数单独运行，不满足本批“同一最终构建、一次阶段矩阵”的合同。本批将两个既有搜索流程纳入默认完整模式，并添加脚本治理测试防止再次遗漏。
- 首次定向运行 `python3 -m unittest scripts.ci.test_validate_branch scripts.ci.test_verify_business` 时，`test_validate_branch.py` 的既有本地导入方式需要 `scripts/ci` 在 `PYTHONPATH` 中，命令因 `ModuleNotFoundError: validate_branch` 失败；使用 `PYTHONPATH=scripts/ci` 的定向命令随后 16 项通过，最终固定 discovery 命令 21 项通过。未修改生产行为来绕过该调用方式差异。
- 首个 disposable integration 包装命令在执行前被本地命令安全策略因包含 `rm -rf` 清理写法拒绝，没有创建任何资源；替换为精确 `unlink`/`rmdir` 清理后，固定 integration 命令在新随机 project 中通过并完成清理。
- Phase-03-01 与 Phase-03-02 实际由同一 PR #49 合入，而非各自独立 PR；该历史偏差已在既有 Phase-03-02 记录和本次总方案状态中如实保留。本批未改写已推送分支或历史。
- 没有固定验收失败要求修改 Backend、Frontend 或 deploy 产品实现，因此未扩大为一般代码审查、重构、覆盖率工作或额外压力测试。

## 6. 已知限制与后续项

- 当前分支尚未 push、创建 Pull Request 或取得自身远程门禁结果；Phase 3 仍不能标记完成。
- 只有 `develop/0.4.3` 合入最新远程 `main` 且实际配置的远程检查成功后，才可从该状态创建独立 `develop/1.0.0` release-only 分支。
- `1.0.0` 动作只能更新允许的版本/发布文档和必要治理元数据；本记录不宣称已创建 tag、release 或完成 Milestone 1 发布。
- 产品仍只对 `post.created` 提供增量索引；帖子更新/删除、自动 dead-queue replay、搜索建议/高亮/过滤/聚合等继续留后。
- PowerShell 保持 `0.2.1` 历史能力基线，未作为本批验收入口。
