# Phase 1-03：帖子发布与查询开发记录

## 1. 完成概述

本批在 `develop/0.2.3` 分支完成受认证保护的帖子发布、列表和详情查询闭环，并将根 `VERSION` 从 `0.2.2` 更新为 `0.2.3`。

已完成的能力包括：

- 新增 Post 领域模型、创建输入、作者摘要、完整读模型、Repository、Service 和 Handler。
- `POST /api/v1/posts` 只使用认证 context 中的当前用户 ID 作为发布者，严格 JSON 解码会拒绝请求体中的 `author_id` 等未知字段。
- 标题和正文按 Unicode 字符数校验，并在持久化前去除首尾 Unicode 空白；标题范围为 1–120 字符，正文范围为 1–10000 字符。
- `GET /api/v1/posts` 使用 `created_at DESC, id DESC` 的键集分页，支持默认 `limit=20`、范围 1–50、`limit + 1` 下一页判断和不透明 URL-safe Base64 游标。
- 游标严格校验版本、规范 UTC 时间、正整数 ID、JSON 字段和单一 JSON 值；损坏或不合法游标稳定映射为 `400 validation_failed`。
- 列表和详情通过单条 MySQL 查询装配作者摘要、评论数、点赞数和当前用户点赞状态；应用层不会按返回帖子数量发起额外 SQL。
- `GET /api/v1/posts/:postId` 支持正整数路径参数校验，并将不存在的帖子稳定映射为 `404 post_not_found`。
- 三个帖子端点统一注册在认证中间件保护的 `/api/v1` 路由下；Backend 启动装配已接入 Post Repository、Service 和 Handler。
- Post 数据路径只依赖 MySQL，没有引入 Redis 读取、回填或失效逻辑。

## 2. 实际变更文件

### 2.1 Backend 实现

- `backend/internal/post/model.go`
- `backend/internal/post/validation.go`
- `backend/internal/post/pagination.go`
- `backend/internal/post/repository.go`
- `backend/internal/post/service.go`
- `backend/internal/post/handler.go`
- `backend/internal/http/api.go`
- `backend/cmd/server/main.go`

### 2.2 测试

- `backend/internal/post/validation_test.go`
- `backend/internal/post/pagination_test.go`
- `backend/internal/post/service_test.go`
- `backend/internal/post/integration_test.go`
- `backend/internal/http/router_post_test.go`
- `backend/internal/http/post_integration_test.go`

### 2.3 版本与记录

- `VERSION`
- `dev/logs/Phase-01/Phase-01-03-帖子发布与查询.md`

## 3. 验证命令与结果

### 3.1 Backend 单元、静态和竞态检查

工作目录：`backend`

- `test -z "$(gofmt -l .)"`
  - 结果：通过；Backend Go 文件均符合 `gofmt`。
- `go test -count=1 ./...`
  - 结果：通过；包含既有健康检查、配置、用户和认证回归，以及新增 Post 校验、游标、分页、Service 和路由契约测试。
- `go vet ./...`
  - 结果：通过。
- `go test -race -count=1 ./...`
  - 结果：通过；未发现数据竞争。

### 3.2 隔离真实依赖 integration 验收

使用独立 Compose project、临时 volume 和动态宿主端口启动 MySQL 8.4 与 Redis 7.2，并配置安全白名单 integration 环境：

- `INTEGRATION_TESTS=1`
- `APP_ENV=test`
- `MYSQL_DATABASE=gopulse_integration`
- `MYSQL_USER=gopulse_integration`
- `REDIS_DB=15`
- MySQL/Redis host 均为 `127.0.0.1`

实际执行：

- `go run ./cmd/migrate up`
  - 结果：通过；隔离空库完成 Phase 1 schema 迁移。
- `go test -count=1 -tags=integration ./...`
  - 结果：通过；真实 MySQL/Redis integration 全量回归通过。
  - 新增 Post integration 覆盖发布者归属、同时间记录的 ID 次序、多页无重复遗漏、空页、翻页期间插入新数据后的稳定性、作者摘要、评论/点赞聚合和 `liked_by_me`。
  - Repository 计数包装器验证返回 2 条和 4 条记录时列表都只执行一条 SQL，没有应用层 N+1。
  - `EXPLAIN FORMAT=JSON` 实际计划检查通过，确认列表计划包含 `idx_posts_created_at_id`、评论索引 `idx_comments_post_id_id` 和关联/点赞查询使用的主键路径。
- 停止隔离 Redis 后执行 `go test -count=1 -tags=integration ./internal/post ./internal/http`
  - 结果：通过；帖子 Repository、Service 和 HTTP 数据路径在 Redis 不可用时仍然通过真实 MySQL 验收。
- 验收结束后已停止并删除本批创建的临时容器、网络和 volume；未修改现有 `.env` 或日常开发 Compose project。

### 3.3 真实 Backend 进程 smoke

在独立 MySQL 上迁移后停止隔离 Redis，构建并启动真实 `backend/cmd/server` 二进制，通过动态 HTTP 端口和 Cookie jar 执行：

1. 注册用户返回 201 并建立登录 Cookie。
2. 发布帖子返回 201，服务器完成首尾空白规范化并返回完整作者与零聚合读模型。
3. 列表返回 200、单项数据和 `meta.next_cursor=null`。
4. 详情返回 200 且 ID、标题和作者与创建结果一致。
5. 不带 Cookie 查询列表返回 `401 authentication_required`。

结果：通过；真实 Backend 装配和帖子路由在 Redis 停止时仍可完成同步 MySQL 读写。

### 3.4 Frontend、脚本和治理回归

- `npm test -- --run`（工作目录：`frontend`）
  - 结果：通过；3 个测试文件、18 个测试通过。
- `npm run typecheck`（工作目录：`frontend`）
  - 结果：通过。
- `npm run build`（工作目录：`frontend`）
  - 结果：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`
  - 结果：通过；8 个治理测试通过。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh`
  - 结果：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
  - 结果：通过。
- `git diff --check`
  - 结果：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/0.2.3 --base-ref origin/main`
  - 结果：通过；当前分支、Phase 权威分配和根 `VERSION=0.2.3` 一致。

## 4. 实施说明与方案偏差

- 无功能范围偏差。未实现帖子编辑、删除、评论/点赞写入、搜索、Frontend 或 Redis 缓存。
- 聚合读模型采用同一条帖子查询中的索引化计数和存在性子查询，避免评论与点赞多表直接连接造成的行数乘积；这仍保持每次列表请求固定为一条 SQL，并已用真实 MySQL 查询计数和 `EXPLAIN FORMAT=JSON` 验证。
- integration 数据使用事务回滚或按唯一测试用户清理；真实进程 smoke 使用独立 Compose project 和动态端口，没有操作用户现有开发数据。
- 按平台规则没有修改或验证冻结的原生 PowerShell 脚本；后续版本继续以 Bash/WSL2 为维护和验收入口。
- GitHub-hosted runner 上的远程 CI 尚未执行；本地已执行与 Workflow 对应的 Backend、Frontend、脚本、Compose 和 integration 检查。

## 5. 已知限制与后续事项

- 本批帖子读取直接以 MySQL 为事实来源；Redis 帖子详情缓存、降级读取和失效属于 Phase-01-05。
- 评论和点赞写入端点尚未实现；本批只读取现有 `comments` 和 `post_likes` 事实，相关写入属于 Phase-01-04。
- 当前只提供帖子创建、列表和详情，不提供编辑、删除、搜索、推荐、标签、媒体或用户帖子专页。
- Phase-01-04 可复用 `post.Repository.FindByID` / `post.Service.Detail` 的帖子存在性边界、Post 游标规则、`post_not_found` 错误契约和聚合读模型。
