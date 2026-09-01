# Phase 1-04：评论与点赞开发记录

## 1. 完成概述

本批于 2026-09-01 在 `develop/0.2.4` 分支完成评论发布与分页查询、帖子点赞与取消点赞的 Backend 同步业务闭环，并将根 `VERSION` 从 `0.2.3` 更新为 `0.2.4`。

已完成的能力包括：

- 新增 Comment 领域模型、输入校验、游标分页、Repository、Service 和 Handler。
- `POST /api/v1/posts/:postId/comments` 只使用认证 context 中的当前用户 ID；评论内容先去除首尾 Unicode 空白，再按 1–2000 个 Unicode 字符校验。
- 评论请求继续使用严格 JSON 解码，空请求体、未知字段、多个 JSON 值、非法 JSON 和超过 64 KiB 的请求体均稳定返回校验错误。
- `GET /api/v1/posts/:postId/comments` 使用 `id DESC` 键集分页，支持默认 `limit=20`、最大 50、`limit + 1` 下一页判断和版本化的不透明 URL-safe Base64 游标。
- 评论列表以单条 JOIN 查询装配作者摘要，不按评论逐条查询用户；真实 MySQL 查询计数和执行计划均已验证。
- 新增 Like Repository、Service 和 Handler，提供 `PUT /api/v1/posts/:postId/like` 与 `DELETE /api/v1/posts/:postId/like`。
- 点赞使用普通 `INSERT` 和 MySQL 1062 重复键识别实现幂等语义；外键、连接及其他数据库错误不会被宽泛忽略。
- 取消点赞使用精确条件 `DELETE`，记录已不存在时仍返回 HTTP 204。
- 评论创建、评论列表、点赞和取消点赞都会先经过共享的帖子存在性边界，不存在的帖子稳定返回 `404 post_not_found`；数据库外键继续作为最终完整性保护。
- 点赞 Service 提供独立 `Exists(postID, userID)` 边界，为 Phase-01-05 拆分帖子公共投影和 `liked_by_me` 查询提供交接点。
- 四个新端点均注册在认证中间件保护的 `/api/v1` 路由组中，真实 Server 装配已接入 Comment 和 Like 模块。
- 评论与点赞事实写入后，既有帖子详情 MySQL 读模型可立即观察到正确的 `comment_count`、`like_count` 和 `liked_by_me`。

## 2. 实际变更文件

### 2.1 Comment 实现与测试

- `backend/internal/comment/model.go`
- `backend/internal/comment/validation.go`
- `backend/internal/comment/validation_test.go`
- `backend/internal/comment/pagination.go`
- `backend/internal/comment/pagination_test.go`
- `backend/internal/comment/repository.go`
- `backend/internal/comment/service.go`
- `backend/internal/comment/service_test.go`
- `backend/internal/comment/handler.go`
- `backend/internal/comment/integration_test.go`

### 2.2 Like 实现与测试

- `backend/internal/like/repository.go`
- `backend/internal/like/repository_test.go`
- `backend/internal/like/service.go`
- `backend/internal/like/service_test.go`
- `backend/internal/like/handler.go`
- `backend/internal/like/integration_test.go`

### 2.3 Post、HTTP、启动装配与 integration 隔离

- `backend/internal/post/repository.go`
- `backend/internal/post/service.go`
- `backend/internal/post/service_test.go`
- `backend/internal/post/integration_test.go`
- `backend/internal/http/api.go`
- `backend/internal/http/router_comment_like_test.go`
- `backend/internal/http/comment_like_integration_test.go`
- `backend/internal/http/post_integration_test.go`
- `backend/internal/integrationtest/mysql_lock.go`
- `backend/cmd/server/main.go`

### 2.4 版本与记录

- `VERSION`
- `dev/logs/Phase-01/Phase-01-04-评论与点赞.md`

## 3. 验证命令与结果

### 3.1 Backend 单元、静态和竞态检查

工作目录：`backend`

最终执行：

- `test -z "$(gofmt -l .)"`
  - 结果：通过；全部 Go 文件符合 `gofmt`。
- `go test -count=1 ./...`
  - 结果：通过；包含 Comment、Like、Post、HTTP 契约及既有 Backend 回归。
- `go vet ./...`
  - 结果：通过。
- `go test -race -count=1 ./...`
  - 结果：通过；未发现数据竞争。

新增自动化覆盖包括：

- 评论 Unicode Trim、空白、无效 UTF-8、1/2000 字符边界和超长输入。
- 评论游标往返、规范 Base64、版本、JSON 形状、正整数 ID、非法及重复分页参数。
- Comment Service 的作者归属、帖子不存在、数据库错误映射和下一页游标。
- 评论 HTTP 201/200 契约、认证、非法帖子 ID、未知字段、非法 JSON 和超限请求体。
- Like Repository 只把 MySQL 1062 重复键转为幂等冲突，外键和连接错误保持真实失败。
- Like Service 的顺序重复点赞、重复取消、帖子不存在及数据库错误映射。
- 评论和点赞四个路由的认证保护、当前用户身份传递和 204 空响应。

### 3.2 隔离真实依赖 integration 验收

使用仅供本批验收的 Compose project `gopulse-phase0104`，配置文件为 `/tmp/gopulse-phase0104-integration.env`，MySQL 与 Redis 动态宿主端口分别为 `46667` 和 `48217`。未停止、重建或写入用户日常使用的 `gopulse` Compose project。

在重建空 volume 并迁移后实际执行：

- `go run ./cmd/migrate up`
  - 结果：通过；隔离空库完成 Phase 1 schema 迁移。
- `go test -count=1 -tags=integration ./...`
  - 结果：通过；真实 MySQL/Redis integration 全量回归通过。

真实依赖场景覆盖：

- 评论按 `id DESC` 跨页查询无重复、无遗漏，作者摘要与事实正确。
- 评论列表无 N+1：无论返回记录数量，Repository 列表只执行一次 SQL。
- `EXPLAIN FORMAT=JSON` 结果包含评论分页索引 `idx_comments_post_id_id` 和用户关联使用的 `PRIMARY` 路径。
- 评论创建后帖子详情 `comment_count` 与评论事实数一致。
- 32 路并发重复点赞全部成功，最终仅保留一条 `(post_id, user_id)` 事实。
- 同一帖子可由两个用户各保留一条点赞；重复取消点赞保持成功。
- 点赞、取消点赞后，两个查看者看到的 `like_count` 和 `liked_by_me` 均正确。
- 不存在帖子返回 404；无效用户触发的真实外键错误未被误当作重复点赞成功。
- HTTP 注册、发帖、评论、评论分页、点赞、取消点赞和帖子详情组成的完整闭环通过。

验收完成后已停止并删除 `gopulse-phase0104` 的临时容器、网络和 volume；未操作用户日常 `gopulse` project。

### 3.3 Redis 停止回归与真实 Server smoke

停止本批专用 Redis 后实际执行：

```bash
go test -count=1 -tags=integration \
  ./internal/auth ./internal/comment ./internal/like \
  ./internal/post ./internal/http
```

结果：通过；本批同步业务路径及所需认证、帖子路径在 Redis 不可用时仍依赖 MySQL 正常工作。

随后构建并启动真实 Backend 进程，在同一专用 MySQL、Redis 停止的环境中完成两用户业务 smoke。覆盖两用户注册、发帖、评论与列表、重复 `PUT` 点赞、两用户点赞、重复 `DELETE` 取消及帖子详情聚合。最终观察结果为：

```json
{
  "post_id": 27,
  "comment_count": 1,
  "like_count": 1,
  "viewer_a_liked": false,
  "viewer_b_liked": true
}
```

结果：通过；真实启动装配、Cookie 认证和完整 Backend 同步闭环均不依赖 Redis。

### 3.4 Frontend、脚本与仓库治理回归

实际执行：

- `npm test -- --run`
  - 结果：通过；3 个测试文件、18 个测试全部通过。
- `npm run typecheck`
  - 结果：通过。
- `npm run build`
  - 结果：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`
  - 结果：通过；8 个治理测试全部通过。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh`
  - 结果：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
  - 结果：通过。
- `git diff --check`
  - 结果：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/0.2.4 --base-ref origin/main`
  - 结果：通过；分支名、总方案版本分配和根 `VERSION` 一致。

## 4. 实施偏差与问题处理

### 4.1 跨 Go package 的真实数据库测试隔离

Redis 停止回归首次并行执行时，不同 Go package 的 integration 测试同时向同一个全局 `posts` 表提交事实。既有 Post 列表 integration 测试要求测试期间不存在其他已提交帖子，因此受到其他 package 并发写入干扰。

这是共享持久化数据导致的测试隔离问题，不是 Comment、Like 或 Redis 降级功能失败。基于持久化数据和跨包回归串扰的明确风险，本批将验证范围扩展到相关 integration package，并新增 MySQL 会话级 `GET_LOCK` / `RELEASE_LOCK`：

- 锁只在 `integration` build tag 下编译。
- 锁连接在测试期间保持独占，并由幂等释放函数关闭。
- 所有会创建全局帖子事实的 Comment、Like、Post 和 HTTP integration 测试使用同一个命名锁串行化。

加入命名锁后，Redis 停止回归和最终全量 integration 均通过。

### 4.2 Smoke 数据与空库前提

真实 Server smoke 会在本批专用验收库提交业务事实。Smoke 后直接重跑全量 integration 时，既有 Post integration 的空库隔离前提不再成立。已仅删除并重建 `gopulse-phase0104` 的专用 volume，重新迁移后执行全量 integration，最终通过；未操作任何用户日常容器或 volume。

### 4.3 与原方案的功能差异

没有功能范围偏离。单条评论、点赞和取消点赞没有为了形式统一增加事务或冗余计数，事实仍由现有 MySQL 外键、索引和联合主键保证。额外的 MySQL 命名锁仅用于 integration 测试隔离，不进入产品运行路径。

## 5. 已知限制与后续项

- 本批不包含评论回复、层级评论、评论编辑/删除、评论点赞、点赞用户列表、通知或热度排名，符合方案边界。
- 本批不实现 Frontend 业务页面；Frontend 评论和点赞交互属于 Phase-01-06。
- 本批不读取、写入或失效 Redis 缓存。Phase-01-05 需要在评论、点赞和取消点赞成功后接入帖子公共投影缓存失效。
- 当前帖子详情仍使用 Phase-01-03 的单条 MySQL 聚合查询直接计算 `liked_by_me`。新增的 `like.Service.Exists` 可供 Phase-01-05 在公共缓存投影之外单独查询当前用户点赞状态。
- 评论与点赞写路径都已在操作前调用 `post.Service.RequireExists`；数据库外键继续处理存在性检查后的竞态并作为最终一致性保护。

## 6. Phase-01-05 交接

Phase-01-05 可直接使用以下基线：

- `post.Service.RequireExists`：不加载查看者相关完整帖子详情的共享存在性边界。
- `comment.Service.Create`：评论成功持久化后的 Service 组装位置，可在成功返回前追加公共投影失效。
- `like.Service.Like` / `like.Service.Unlike`：点赞事实成功变更或幂等完成后的 Service 组装位置。
- `like.Service.Exists`：与帖子公共投影分离的当前用户 `liked_by_me` 查询边界。
- `post.Service.Detail` / `post.Repository.FindByID`：Redis 完全停止时已经验收通过的 MySQL 详情回源路径。
- `backend/internal/integrationtest.AcquirePostFactsLock`：后续涉及全局帖子事实的跨 package integration 测试隔离工具。
- Redis 停止时注册、发帖、评论、点赞、取消点赞和详情读取的真实 Backend 闭环通过记录。
