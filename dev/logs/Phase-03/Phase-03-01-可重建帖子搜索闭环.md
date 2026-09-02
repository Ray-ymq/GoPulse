# Phase-03-01：可重建帖子搜索闭环开发记录

## 1. 执行基线

- 执行日期：2026-09-02。
- 开始前已执行 `git fetch origin --prune`，并从 `origin/main` 的 `5fd6bd366635aa4ce7de45661e1ec76622af2cb8` 创建 `develop/0.4.1`。
- 目标版本：`0.4.1`；根 `VERSION` 与 Frontend npm 版本元数据均更新为该版本。
- 执行环境：WSL2/Linux 文件系统中的 `/home/ray/GoPulse`，Docker context 为 `default`，使用 Bash 与单一 Docker daemon。
- 开始时工作树干净；仓库根目录没有默认 Compose 文件。日常 `gopulse` Compose project、`.run/bin/gopulse-backend`、`.run/dev-wsl-validation.sh` 以及既有 `gopulse`、`gopulse-phase0203-integration` 数据卷均作为用户已有资源保留。
- 冻结的 `scripts/*.ps1`、本地 `.env` 和 `.run` 内容未修改或纳入提交。

## 2. 实际完成内容

### 2.1 Elasticsearch 基础设施与配置

- 在 `deploy/compose.yaml` 增加固定镜像 `docker.elastic.co/elasticsearch/elasticsearch:9.5.2`，采用单节点、本地关闭安全插件、512 MiB heap、回环地址 9200 映射、命名卷及等待 `yellow` 的健康检查。
- 新增 `ELASTICSEARCH_PORT`、`ELASTICSEARCH_URL`、`ELASTICSEARCH_REQUEST_TIMEOUT` 和 `SEARCH_REINDEX_BATCH` 配置；实现默认值、上下限及 HTTP(S)/host/userinfo/query/fragment 校验。
- 新增只加载 MySQL 与 Elasticsearch 的 `LoadReindex` 配置边界，重建命令不依赖 Redis、RabbitMQ、HTTP 或认证配置。
- 引入官方 `github.com/elastic/go-elasticsearch/v9 v9.5.0` 客户端，封装有界连接/响应超时、禁用重试、响应关闭取消和集群健康检查。
- Backend `/ready` 增加 `elasticsearch` 检查；Elasticsearch 不可用会降低 readiness，但未注入或替换既有 MySQL Repository。

### 2.2 搜索契约与安全重建

- 新增 `backend/internal/search`，统一定义 `gopulse-post-search-v1` alias、物理索引前缀、1 shard/0 replica、strict mapping 和文档字段白名单。
- 搜索文档仅包含 `post_id`、`title`、`content`、`created_at`、`updated_at`，`_id` 固定为十进制帖子 ID；物理索引名使用无小数点的 UTC 时间戳与随机后缀。
- 新增角色隔离命令 `backend/cmd/search-reindex`，支持强制重建和 `--if-missing` 初始化。
- 重建使用 MySQL advisory lock、新物理索引、H1 有界批量扫描、Bulk 逐项状态校验、refresh/count 核对、单次 alias 原子切换、H2 尾部补偿及精确旧索引删除。
- 删除边界只接受受控物理索引名，不使用 wildcard；切换前失败时清理本次新索引而不改变现有 alias。

### 2.3 Search API 与 MySQL hydration

- 新增认证接口 `GET /api/v1/search/posts?q=<keyword>&limit=<n>&cursor=<opaque>`。
- 严格校验唯一的 `q`、`limit`、`cursor` 和未知参数；查询词限制为 trim 后 1～200 个 Unicode code point，limit 默认 20、最大 50。
- Elasticsearch 使用 `multi_match best_fields` 搜索 `title^2` 与 `content`，按 `_score`、`created_at`、`post_id` 稳定排序并用 `search_after` 分页。
- cursor 绑定规范化 query 摘要、物理 index generation 和 sort tuple；跨查询、损坏或重建后的 cursor 返回 `validation_failed`。
- Elasticsearch 只返回有序帖子 ID；`post.MySQLRepository.FindMany` 以一次有界集合查询装配完整 `Post` DTO，并在 Go 中恢复命中顺序。
- 增加 `search_unavailable` 公共错误及 HTTP 503 映射；依赖错误、alias 缺失和无效响应不会泄漏 URL、索引名或 DSL。

### 2.4 Frontend 搜索闭环

- 新增受保护 `/search` 路由、主导航入口、Search API 严格响应校验和 `SearchView`。
- 搜索词写入 URL query；路由变化会重置旧结果和 cursor，并支持刷新、前进、后退恢复搜索意图。
- 页面复用 `PostCard`，提供初始提示、加载、空结果、服务不可用、参数/cursor 错误、重试和加载更多状态。
- 浏览器仅请求 Backend 相对 `/api/v1/search/posts`，不读取 Elasticsearch 配置，也不直接访问 9200。
- 新增组件测试和真实 Chromium 定向用例，覆盖搜索结果、空结果、代表性失败、分页及详情跳转。

### 2.5 Bash 生命周期、验收与文档

- `scripts/dev.sh` 现在校验并启动 Elasticsearch，在迁移后执行 `search-reindex --if-missing`，再启动 Backend/Worker/Frontend。
- `scripts/verify.sh` 只读验证 Elasticsearch Compose 服务及 readiness 契约；`scripts/down.sh` 继续保留包括 Elasticsearch 在内的命名卷。
- `scripts/verify-business.sh` 增加隔离随机 Elasticsearch 端口和 `--search-rebuild` 定向模式；该模式不重复 Phase 2 Worker/Broker 故障矩阵。
- 定向验收创建历史帖子、评论和点赞，验证标题/正文/无关词/分页/MySQL hydration，精确删除活动物理索引后验证安全 503，再次重建并确认恢复，同时证明无关索引仍存在。
- 修正验收脚本的 Elasticsearch HEAD 请求，使用 curl `--head`，避免 `--request HEAD` 在收到 200 后继续等待不存在的响应体。
- 扩展治理静态测试，固定 Elasticsearch 归属校验、镜像/回环端口/健康检查/命名卷以及精确索引删除和浏览器定向入口。
- README 更新版本、配置、生命周期、API、重建命令、故障语义和当前无增量索引的限制。

## 3. 变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `backend/cmd/search-reindex/main.go`
- `backend/cmd/server/main.go`
- `backend/go.mod`
- `backend/go.sum`
- `backend/internal/apperror/error.go`
- `backend/internal/config/config.go`
- `backend/internal/config/search_test.go`
- `backend/internal/http/api.go`
- `backend/internal/http/response/response.go`
- `backend/internal/http/router.go`
- `backend/internal/http/router_test.go`
- `backend/internal/platform/elasticsearch.go`
- `backend/internal/post/repository.go`
- `backend/internal/search/contract.go`
- `backend/internal/search/elasticsearch.go`
- `backend/internal/search/handler.go`
- `backend/internal/search/reindex.go`
- `backend/internal/search/service.go`
- `backend/internal/search/service_test.go`
- `deploy/compose.yaml`
- `frontend/e2e/business.spec.ts`
- `frontend/package.json`
- `frontend/package-lock.json`
- `frontend/src/components/AppNav.vue`
- `frontend/src/router/index.ts`
- `frontend/src/services/api.ts`
- `frontend/src/services/http.ts`
- `frontend/src/styles.css`
- `frontend/src/types/api.ts`
- `frontend/src/views/SearchView.vue`
- `frontend/src/views/SearchView.test.ts`
- `scripts/ci/test_verify_business.py`
- `scripts/dev.sh`
- `scripts/down.sh`
- `scripts/verify-business.sh`
- `scripts/verify.sh`
- `dev/logs/Phase-03/Phase-03-01-可重建帖子搜索闭环.md`

## 4. 实际验证

### 4.1 实现期间定向检查

- `go test ./internal/config ./internal/platform ./internal/post ./internal/search ./internal/http ./internal/apperror ./cmd/search-reindex ./cmd/server`（`backend`）：通过。
- `go vet ./internal/platform ./internal/post ./internal/search ./internal/http ./cmd/search-reindex ./cmd/server`（`backend`）：通过。
- `npm test -- --run`（`frontend`）：通过，9 个测试文件、44 项测试。
- `npm run typecheck`（`frontend`）：通过。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- `scripts/verify-business.sh --self-test`：通过；接受 1 个合法目标并拒绝 6 个不安全目标，未访问 Docker。
- `python3 -m unittest scripts.ci.test_verify_business`：通过，6 项测试。

### 4.2 隔离搜索重建验收

- `scripts/verify-business.sh --search-rebuild`：最终通过。
- 验收确认历史帖子标题和正文均可命中，无关词为空，三条结果可稳定分页；返回的作者、正文、评论数、点赞数及 `liked_by_me` 来自 MySQL hydration。
- 验收精确解析并删除当时 alias 指向的 `gopulse-post-search-v1-*` 物理索引，随后 Search API 返回不泄漏内部信息的 `503 search_unavailable`；再次强制重建后历史帖子恢复。
- 验收创建的 `gopulse-acceptance-<token>-unrelated` 索引在搜索索引删除和重建后仍存在；真实 Chromium `search-rebuild` 用例 1 项通过。
- 清理仅移除经 project/token/端口归属验证的隔离容器、网络和卷；清理后的日常开发栈快照与执行前一致。

### 4.3 最终固定完成门禁

- Backend 定向 `go test`：通过。
- Backend 定向 `go vet`：通过。
- `go test -race ./internal/post ./internal/search ./internal/http ./cmd/search-reindex`：通过，未发现数据竞争。
- `go test -count=1 -tags=integration ./internal/post ./internal/search`：在新建、迁移并最终清理的 disposable MySQL/Redis 安全目标上通过。
- `npm test -- --run`：通过，9 个测试文件、44 项测试。
- `npm run typecheck`：通过。
- `npm run build`：通过，完成 `vue-tsc --noEmit` 与 Vite production build。
- `npm run test:e2e -- --grep search-rebuild`：未提供验收 seed 环境变量时按设计跳过 1 项；同一 Chromium 场景已在 `--search-rebuild` 隔离验收中实际通过。
- Bash syntax、Compose config、`--self-test`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，19 项治理测试。
- `python3 scripts/ci/validate_versions.py`：通过，根版本及 Frontend npm 元数据一致为 `0.4.1`。
- `python3 scripts/ci/validate_branch.py --branch develop/0.4.1 --base-ref upstream/main`：通过；develop 分支校验不需要解析不存在的 base ref。
- `git diff --check`：通过。

## 5. 失败、修复与方案偏差

- 初次定向搜索验收在无关索引 HEAD 校验处发生 curl 10 秒超时。Elasticsearch 已返回 HTTP 200，但 `curl --request HEAD` 仍等待响应体；改为语义正确的 `curl --head` 后重跑同一场景并通过。
- 固定 integration 命令在没有 `INTEGRATION_TESTS=1` 时按既有安全设计失败；指向开始前已存在的 `gopulse-phase0203-integration` 数据库时，又因其中保留的帖子事实晚于测试固定时间而出现排序断言失败。未清理或改写该用户已有数据库，而是创建 whitelisted 名称的 disposable MySQL/Redis、执行迁移、运行相同测试命令并在结束时删除容器，最终通过。
- 未运行普通 `scripts/verify-business.sh` 的完整 Phase 2 Worker/Broker 十项故障矩阵。本批未修改 Outbox、RabbitMQ topology、Business Worker 或通知语义；按方案只运行新增的隔离 `--search-rebuild` 门禁。
- 除以上环境准备和 HEAD 调用修正外，没有改变实施方案的产品范围；没有修改帖子创建事务或提前接入 Phase-03-02 的增量事件链路。

## 6. 已知限制与后续项

- 本批没有自动或增量索引。每次重建只保证捕获其 H1/H2 水位范围内的 MySQL 帖子；重建完成后创建的新帖子必须再次运行重建才可搜索。
- `post.created` 可靠事件、增量 Indexer 和持续收敛属于 Phase-03-02，不在本批完成范围内。
- 本地 Elasticsearch 为单节点且关闭安全插件，仅适用于回环绑定的开发/验收环境；生产安全、集群 HA、容量与搜索质量调优尚未交付。
- 本地实现、固定门禁和自动提交完成后，远程 push、PR、合并及远程质量门禁仍需后续流程，不能预先声明完成。
