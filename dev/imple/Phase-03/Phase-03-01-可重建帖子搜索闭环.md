# Phase 3-01：可重建帖子搜索闭环实施方案

> 执行序号：1 / 3
>
> 总方案来源：[Phase-03-总实施方案.md](Phase-03-总实施方案.md)

## 1. 批次目标

从当前仅有 MySQL 帖子事实的基线，纵向交付一条可运行、可验证的历史帖子搜索闭环：固定版本 Elasticsearch → 从 MySQL 安全重建 → Backend 认证搜索 API → Frontend 搜索页面。

本批必须证明搜索索引可以被精确删除并从 MySQL 恢复，同时明确 Elasticsearch 只负责命中和排序，返回给用户的帖子字段仍由 MySQL 装配。本批不接入 `post.created` 增量事件，因此命令完成后新建帖子需再次重建才可搜索。

## 2. 前置条件

- Phase 2 与 Review 整改已合入配置的主远程 `main`，根版本为 `0.3.6`。
- 已 fetch 主远程并从最新 `main` 创建 `develop/0.4.1`，未沿用规划分支或已完成开发分支。
- 在 WSL2 Linux filesystem 中实施，使用一个明确的 Docker daemon 和 Bash 入口。
- 开始前记录 Git 状态、日常 Compose project、volume 和 `.run` 进程状态，不覆盖用户改动。
- 当前 MySQL 中的 `posts`、现有 `post.Post` DTO、认证中间件、分页响应和脚本归属保护是兼容基线。

## 3. 实施范围

### 3.1 Elasticsearch 基础设施与配置

- 在 Compose 新增固定 `docker.elastic.co/elasticsearch/elasticsearch:9.5.2` 服务、单节点设置、512 MiB heap、`1` shard/`0` replica 目标、命名卷和至少 `yellow` 的健康检查。
- 9200 端口固定绑定 `127.0.0.1:${ELASTICSEARCH_PORT}`；本地可关闭安全插件，但不允许通过 `PUBLISHED_HOST` 暴露。
- 新增 `ELASTICSEARCH_PORT`、`ELASTICSEARCH_URL`、`ELASTICSEARCH_REQUEST_TIMEOUT` 和 `SEARCH_REINDEX_BATCH` 示例值、默认值、上下限及 URL 校验；URL 禁止 userinfo。
- 引入官方 Go 客户端 `github.com/elastic/go-elasticsearch/v9 v9.5.0`，封装有界 request、response close、状态分类和健康检查。
- Backend `/ready` 增加 `elasticsearch`；Elasticsearch down 使 readiness 降级，但不把 Elasticsearch 注入既有 MySQL Repository。
- `dev.sh` 在依赖健康和迁移后执行 `search-reindex --if-missing`；`verify.sh` 只读检查服务与 readiness；`down.sh` 保留 Elasticsearch volume。

### 3.2 索引契约与重建命令

- 新增 `backend/internal/search`，集中定义 `gopulse-post-search-v1` 别名、物理前缀、strict Mapping、文档结构、Bulk item 校验与搜索 Repository 边界。
- 文档只包含 `post_id`、`title`、`content`、`created_at` 和 `updated_at`；`_id` 固定为十进制 `post_id`，不包含作者、计数或 viewer 状态。
- 新增角色隔离的 `cmd/search-reindex` 配置与命令入口，只依赖 MySQL/Elasticsearch，不要求 Redis、RabbitMQ、HTTP 或认证配置。
- 实现专用 MySQL advisory lock、新物理索引、H1 有界扫描、Bulk 逐项校验、切换前计数核对、单次 alias 原子切换、H2 尾部补偿和精确旧索引清理。
- `--if-missing` 只在别名不存在时初始化；强制重建每次创建新物理索引，不在当前索引原地清空重写。
- 任一 Bulk item、refresh、count 或 alias 操作失败均返回非零；切换前失败不改变旧别名，删除操作禁止 wildcard。

### 3.3 Search API 与 MySQL hydration

新增认证接口：

```text
GET /api/v1/search/posts?q=<keyword>&limit=<n>&cursor=<opaque>
```

- 严格解析唯一的 q/limit/cursor；q trim 后为 1～200 Unicode code point，limit 默认 20、最大 50。
- 使用 `multi_match best_fields` 搜索 `title^2` 和 `content`，按 `_score DESC, created_at DESC, post_id DESC` 排序并使用 `search_after`。
- cursor 绑定版本、规范化 query 摘要、物理 index generation 和 sort tuple；跨查询、损坏或重建后失效统一返回 `validation_failed`。
- Elasticsearch 只返回有序 ID；帖子 Repository 增加一次有界批量 MySQL 装配，并在 Go 中恢复命中顺序，返回现有 `Post` DTO。
- Elasticsearch 不可用、超时、别名缺失、拒绝请求或响应无效统一返回 `503 search_unavailable`；公共响应不泄漏 URL、DSL、index 或底层错误。

### 3.4 Frontend 搜索页

- 新增受保护 `/search`、主导航入口、Search API 类型/响应校验和搜索页面。
- 查询词写入 URL query；显式提交新查询时清空旧结果/cursor，刷新、前进和后退恢复搜索意图。
- 复用 `PostCard`，提供初始提示、加载、空结果、加载更多、参数错误、服务不可用、cursor 失效和失败重试状态。
- 结果点击进入现有帖子详情；浏览器只访问 Backend 相对路径，不读取 Elasticsearch 环境变量或访问 9200。

## 4. 实施边界与非目标

- 本批不修改帖子创建事务、bus Envelope、Outbox CHECK、RabbitMQ topology、Business Worker 或通知语义。
- 本批新帖子只有再次运行重建后才进入索引；不得把该限制写成已实现自动同步或阶段完成。
- 不实现高亮、建议、纠错、同义词、过滤、聚合、自定义中文分词、相关度调参平台或搜索管理 API。
- 不允许 Frontend 传入 Elasticsearch DSL、index、字段或 sort，不把 Elasticsearch `_source` 直接作为公共帖子响应。
- 不修改现有帖子列表/详情契约，不把 `/ready` 的依赖状态当成既有业务 API 的授权开关。
- 不修改冻结 PowerShell，不建设生产 Elasticsearch 安全、集群、ILM、快照或容量能力。

## 5. 预计文件与交付物

```text
deploy/compose.yaml
.env.example
backend/go.mod
backend/go.sum
backend/internal/config/
backend/internal/platform/elasticsearch.go
backend/internal/search/
backend/internal/post/
backend/internal/http/
backend/internal/apperror/
backend/cmd/server/
backend/cmd/search-reindex/
frontend/src/router/
frontend/src/views/SearchView.vue
frontend/src/components/AppNav.vue
frontend/src/services/
frontend/src/types/
frontend/src/**/*.test.ts
frontend/e2e/business.spec.ts
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-business.sh
scripts/ci/
.github/workflows/quality-gates.yml
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-03/Phase-03-01-可重建帖子搜索闭环.md
```

预计文件只表示允许触达的边界；实际未修改文件不得写入实施记录，遇到跨边界需求时先确认其是否直接服务本批验收。

## 6. 详细实施步骤

1. 从总方案提取本批新增行为和固定门禁，核对最新 `main`、现有配置、readiness、帖子 DTO、分页响应与脚本安全契约。
2. 增加 Elasticsearch Compose 服务、示例配置、healthcheck、回环端口和命名卷，并同步 CI 的 Compose 端口断言。
3. 引入官方客户端，完成 Backend、重建命令各自所需的最小配置、transport、checker 和有界响应处理。
4. 定义唯一 Mapping、alias、物理前缀和文档生成器，以代表性 strict Mapping 测试固定字段契约。
5. 实现 MySQL 按 ID 上界与 batch 扫描、单 ID/多 ID hydration 和 Bulk 写入逐项检查。
6. 实现重建 lock、H1 校验、alias 原子切换、H2 补偿和切换前后失败边界；所有清理使用精确索引名。
7. 实现搜索 query/cursor、Elasticsearch `search_after` 和 MySQL hydration，覆盖标题、正文、空结果、分页与依赖失败。
8. 注册认证 Search API 和 `search_unavailable` 错误映射，确认其他 API 不因 Elasticsearch down 改变既有事实语义。
9. 实现 Frontend API、路由、导航和页面，以最少组件测试覆盖主成功、空结果和代表性失败。
10. 扩展 Bash 生命周期、只读验证和隔离定向验收；从空 MySQL/Elasticsearch 创建历史帖子并验证 API/浏览器搜索。
11. 精确删除验收索引后再次重建，确认全部已捕获 MySQL 帖子恢复且非本次索引、volume 和进程未受影响。
12. 更新 README、目标版本和实施记录，如实记录“新帖子自动索引尚未交付”的批次限制。

## 7. 风险与控制

- **重建破坏可用索引**：新建物理索引并在验证后原子切换；切换前失败不触碰旧别名。
- **误删其他索引**：禁止 wildcard，只删除本次创建或按固定前缀枚举并再次校验的精确索引。
- **展示事实陈旧**：Elasticsearch 只返回 ID；作者、正文、计数和 viewer 状态由 MySQL 装配。
- **分页跨代混用**：cursor 绑定 query 和 index generation；重建后显式失效。
- **依赖故障扩大**：Elasticsearch down 只影响 readiness/search，已启动 Backend 的 MySQL-backed API 保持原边界。
- **重建并发误承诺**：本批明确只保证捕获水位内的历史快照；持续增量收敛必须等待 Phase-03-02。
- **验收误伤用户资源**：复用并扩展既有随机 project/port/path/volume 和进程身份白名单，破坏前逐项确认归属。
- **测试范围膨胀**：不建立搜索质量黄金集，不穷举 analyzer、输入编码或 alias 失败排列。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败后只重跑受修复影响的命令或场景：

```bash
(cd backend && go test ./internal/config ./internal/platform ./internal/post ./internal/search ./internal/http ./internal/apperror ./cmd/search-reindex ./cmd/server)
(cd backend && go vet ./internal/platform ./internal/post ./internal/search ./internal/http ./cmd/search-reindex ./cmd/server)
(cd backend && go test -race ./internal/post ./internal/search ./internal/http ./cmd/search-reindex)
(cd backend && go test -count=1 -tags=integration ./internal/post ./internal/search)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
(cd frontend && npm run test:e2e -- --grep search-rebuild)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
scripts/verify-business.sh --search-rebuild
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/0.4.1 --base-ref upstream/main
git diff --check
```

`--search-rebuild` 是本批新增的隔离定向入口，只执行历史帖子、重建、查询、Frontend 和资源清理，不重复 Phase 2 Worker/Broker 完整故障矩阵。若实际修改共享认证、帖子公共 DTO 或脚本核心归属逻辑，再补一条对应最低层回归，并在实施记录中写明风险依据。

## 9. 验收标准

- Compose Elasticsearch 使用固定版本、回环端口、命名卷和健康检查；Bash 生命周期可以安全启动、检查和停止。
- `/ready` 包含 Elasticsearch；Elasticsearch down 时 Search API 返回 `503 search_unavailable`，既有 MySQL 业务不被错误改造成 Elasticsearch 依赖。
- Mapping、别名、文档 ID 和字段白名单与总方案一致，Search API、重建命令与未来 Indexer 共用同一契约。
- 历史帖子可从 MySQL 重建；标题与正文均有真实命中，无关词返回空结果。
- 重建使用新物理索引和原子 alias；精确删除搜索索引后可恢复，失败不删除当前可用索引或用户其他资源。
- Search API 严格验证 q/limit/cursor，支持稳定排序与分页，不泄漏 Elasticsearch 内部信息。
- 搜索结果的作者、内容、评论数、点赞数和 `liked_by_me` 由 MySQL 正确装配并保持命中顺序。
- Frontend 只访问 Backend，搜索、空状态、分页、错误、重试、重置和详情跳转可用。
- 第 8 节固定门禁通过，版本元数据为 `0.4.1`，实施记录真实完整且明确增量限制。

## 10. 明确完成条件

只有 Elasticsearch 基础设施、MySQL 重建、认证 Search API、Frontend 页面和索引删除恢复形成真实端到端闭环，固定门禁通过且无 P0/P1，才可标记本批完成。新帖子自动索引尚未交付，不得据此宣告 Phase 3 完成。

## 11. 下一批交接

- 已验证的 Elasticsearch 配置、Mapping、别名和幂等文档写入边界。
- 可恢复所捕获 MySQL 帖子的重建命令，以及 H1/H2 并发补偿接口。
- 稳定的 Search API/Frontend，可直接作为增量索引的用户可观察出口。
- 明确保留的唯一主缺口：新帖子要到可靠 `post.created` 事件接入后才自动进入索引。
