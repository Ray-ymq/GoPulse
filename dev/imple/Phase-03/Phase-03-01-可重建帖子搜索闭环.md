# Phase 3-01：可重建帖子搜索闭环实施方案

> 执行序号：1 / 3
>
> 前置阶段：Phase 2 已完成并合入主远程 `main`
>
> 总方案来源：[Phase-03-总实施方案.md](Phase-03-总实施方案.md)

## 1. 批次目标

先交付不依赖新增 RabbitMQ 事件的完整搜索读闭环：将 MySQL 历史帖子安全重建到 Elasticsearch，经认证 Backend Search API 查询，再由 Frontend 搜索页展示 MySQL 装配的当前帖子事实。

本批完成后，用户运行受支持的重建入口即可搜索既有帖子；新帖子自动增量索引留给 Phase-03-02。该边界使 Mapping、别名、重建、查询和 UI 可以在接入异步写链路前独立运行与验证。

## 2. 前置条件

- 主远程 `main` 根版本为 `0.3.5`，Phase 2 已完成能力可运行；Phase 2 Review 指出的权威状态文字滞后已在规划分支如实关闭。
- 已 fetch 主远程，并从其最新 `main` 创建本批权威分支。
- WSL2 Linux filesystem、Bash、Go、Node.js/npm、Docker 与 Compose 可用。
- 开始前工作树状态已记录，用户 `.env`、`.run`、容器和数据卷不被覆盖。

## 3. 实施范围

### 3.1 Elasticsearch 基础设施与配置

- Compose 增加固定 `9.5.2` 单节点 Elasticsearch、512 MiB heap、命名卷和 cluster healthcheck。
- 端口固定绑定回环地址，不复用可放宽的 `PUBLISHED_HOST`；`.env.example` 增加 `ELASTICSEARCH_PORT=9200` 与 `ELASTICSEARCH_URL=http://127.0.0.1:9200`。
- 引入官方 `github.com/elastic/go-elasticsearch/v9 v9.5.0`，封装 URL 校验、请求 timeout、响应体上限、错误分类、close 和 Checker。
- `/ready` 增加 `elasticsearch` 字段；ES down 时返回 `503`，`/health` 仍为纯进程存活检查。
- 扩展 `dev.sh` 的端口占用、Compose 健康等待和失败清理；迁移完成后运行 `search-reindex --if-missing`，只在别名缺失时初始化。扩展 `verify.sh` 的只读容器/readiness 检查；不修改 PowerShell。

### 3.2 索引契约与重建命令

- 新增 `internal/search`，集中定义别名、物理前缀、strict Mapping、帖子文档、Bulk item 校验和搜索仓储边界。
- 固定 `_id=post_id`，索引只保存 post ID、标题、正文和创建/更新时间，不复制作者名、计数或 viewer 状态。
- 新增 `cmd/search-reindex`，实现 advisory lock、H1 有界全量扫描、切换前计数校验、原子 alias swap、H2 并发补偿和精确旧索引清理。
- `--if-missing` 仅在别名不存在时初始化；强制重建每次创建新物理索引，不在当前索引原地清空重写。
- 任一 Bulk item、refresh、count 或 alias 操作失败均非零退出；切换前失败不得影响旧别名，删除不得使用 wildcard。

### 3.3 Search API 与 MySQL 装配

新增认证接口：

```text
GET /api/v1/search/posts?q=<keyword>&limit=<n>&cursor=<opaque>
```

- 严格解析唯一 q/limit/cursor；q trim 后 1～200 Unicode code point，limit 默认 20、最大 50。
- 使用 `multi_match best_fields` 搜索 `title^2` 和 `content`，按 `_score, created_at, post_id` 降序并使用 `search_after`。
- cursor 绑定规范化查询摘要、物理 generation 和 sort tuple；跨查询、损坏或重建后失效统一返回 `validation_failed`。
- Elasticsearch 只返回有序 post ID；帖子 Repository 增加有界批量 MySQL 装配并恢复命中顺序，返回现有 `Post` DTO。
- 增加 `search_unavailable` 并映射 `503`；内部 ES 错误、DSL、索引名与 URL 不进入公共响应。

### 3.4 Frontend 搜索页

- 新增受保护 `/search`、导航入口、Search API DTO 校验和搜索页面。
- 搜索词写入 URL query；显式提交新查询时清空旧结果/cursor，刷新和前进/后退可恢复查询。
- 复用 `PostCard`，提供加载、空结果、加载更多、参数错误、服务不可用、cursor 失效和失败重试状态。
- 浏览器只访问 Backend 相对路径，不读取 Elasticsearch 环境变量或访问 9200。

## 4. 实施边界与非目标

- 本批不修改帖子创建事务、bus Envelope、Outbox CHECK、RabbitMQ topology 或 Business Worker。
- 本批新帖子只有再次运行重建后才进入索引；不得把该限制写成已实现自动同步。
- 不实现高亮、建议、纠错、同义词、过滤、聚合、自定义中文分词或相关度调参平台。
- 不暴露重建 HTTP API，不允许 Frontend 传入 Elasticsearch DSL、index 或 sort。
- 不把 ES `_source` 直接作为公共帖子响应，不修改现有帖子列表/详情语义。
- 不修改冻结 PowerShell，不建设生产 Elasticsearch 安全/集群能力。

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
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-03/Phase-03-01-可重建帖子搜索闭环.md
```

## 6. 详细实施步骤

1. 从总方案提取本批新增行为与固定门禁，核对最新 `main` 和当前配置/响应契约。
2. 增加 ES Compose 服务、示例配置、healthcheck、端口和命名卷，并验证失败清理不删除日常卷。
3. 引入官方 client，完成最小 Config、Transport、Checker 和有界响应解码测试。
4. 定义唯一 Mapping/alias/document 生成器，以一个代表性 strict mapping 成功/失败测试固定契约。
5. 实现 MySQL 按 ID 上界与 batch 扫描、Bulk 写入及 item error 检查。
6. 实现重建锁、H1 校验、alias 原子切换、H2 补偿和切换前后失败边界；用精确索引名测试清理。
7. 实现 search query/cursor、ES `search_after` 和 MySQL hydration；覆盖标题、正文、空结果和依赖失败。
8. 注册认证 Search API 和错误映射，确认其他 API 不因 ES down 改变既有事实语义。
9. 实现 Frontend API、路由、导航和页面，以最少组件测试覆盖主成功、空结果和代表性失败。
10. 在隔离 MySQL/ES 创建历史帖子，运行重建并用 Backend/浏览器验证标题、正文、分页与跳转。
11. 精确删除验收索引后再次重建，确认全部 MySQL 帖子恢复且用户其他资源未受影响。
12. 更新 README、目标版本和实施记录，记录新帖子自动同步尚未交付的真实限制。

## 7. 风险与控制

- **重建破坏可用索引**：新建物理索引并在验证后原子切换，切换前失败不触碰现有别名。
- **误删其他索引**：禁止 wildcard，只删除本次创建或经固定前缀枚举并校验的精确索引。
- **展示事实陈旧**：ES 只返回 ID；作者、正文、计数和 viewer 状态由 MySQL 装配。
- **分页混代**：cursor 绑定 query 和 generation；重建后显式失效。
- **依赖故障扩大**：ES down 只影响 readiness/search，MySQL-backed 业务保持既有边界。
- **测试范围膨胀**：不建立搜索质量黄金集，不穷举 analyzer、输入编码或 alias 失败组合。

## 8. 固定验证命令与必要回归

最终 diff 上每项只执行一次；失败后只重跑受修复影响的命令或场景：

```bash
(cd backend && go test ./internal/config ./internal/platform ./internal/post ./internal/search ./internal/http ./cmd/search-reindex ./cmd/server)
(cd backend && go vet ./internal/platform ./internal/post ./internal/search ./internal/http ./cmd/search-reindex ./cmd/server)
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
git diff --check
```

`--search-rebuild` 是本批新增的隔离定向入口，只执行历史帖子、重建、查询、Frontend 和资源清理，不重复 Phase 2 Worker/Broker 完整矩阵。若实际修改共享认证或既有帖子公共契约，再补最小定向回归并记录风险依据。

## 9. 验收标准

- Compose ES 使用固定版本、回环端口、命名卷和健康检查，Bash 生命周期安全启动、检查和停止。
- `/ready` 包含 ES；ES down 时 search 返回 `503 search_unavailable`，其他 MySQL 业务不被错误禁用。
- Mapping、别名、文档 ID 和字段白名单与总方案一致，未来日志索引不会冲突。
- 历史帖子可从 MySQL 重建；标题与正文均有真实命中，无关词返回空结果。
- 重建使用新物理索引和原子 alias；删除精确索引后可恢复，失败不删除当前可用索引或非本次资源。
- Search API 严格验证 q/limit/cursor，支持相关度排序与分页，不泄漏 ES 内部信息。
- 搜索结果的作者、内容、评论数、点赞数和 `liked_by_me` 由 MySQL 正确装配并保持命中顺序。
- Frontend 只访问 Backend，主要搜索、空、分页、错误、重试、重置和详情跳转可用。
- 第 8 节固定门禁通过，版本元数据为 `0.4.1`，实施记录真实完整。

## 10. 明确完成条件

只有重建、Search API、Frontend 和索引删除恢复形成真实端到端闭环，固定门禁通过且无 P0/P1，才可标记本批完成。新帖子自动索引尚未交付，不得据此宣告 Phase 3 完成。

## 11. 下一批交接

- 已验证的 ES 配置、Mapping、别名和幂等文档写入接口。
- 可恢复全部 MySQL 帖子的重建命令及并发补偿边界。
- 稳定 Search API/Frontend，可直接观察新帖子最终可搜索。
- 明确保留的缺口：新帖子要到可靠增量事件接入后才自动进入索引。
