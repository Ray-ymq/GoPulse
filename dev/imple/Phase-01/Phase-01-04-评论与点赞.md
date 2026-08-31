# Phase 1-04：评论与点赞实施方案

> 执行序号：4 / 6
> 前置批次：Phase 1-01 至 Phase 1-03 已完成并通过验收
> 总方案来源：[Phase-01-总实施方案.md](Phase-01-总实施方案.md)

## 1. 批次目标

实现帖子评论发布与游标分页查询，以及帖子点赞与取消点赞的幂等 API，完成 Phase 1 Backend 的同步业务闭环。

本批以 MySQL 联合主键、外键和真实事实表保证一致性，不引入 Redis 或 RabbitMQ。完成后，用户可通过 HTTP API 完成注册、登录、发帖、评论和点赞全部 Backend 流程。

## 2. 前置条件

- 认证中间件、当前用户 ID 上下文和用户摘要查询已稳定。
- 帖子创建、存在性查询、详情读模型和游标分页工具已可用。
- `comments` 和 `post_likes` 外键、索引与联合主键已在真实 MySQL 上验证。
- Phase 1-01 至 Phase 1-03 实施记录已完成。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 Comment 模块

- 评论作者 ID 只来自认证 context。
- 评论内容去除首尾空白后为 1–2000 个字符。
- 发表评论前验证目标帖子存在，数据库外键作为最终完整性保护。
- 创建成功后返回评论 `id`、`post_id`、`content`、`created_at` 和作者摘要。
- 列表按 `id DESC` 稳定排序，复用不透明游标、`limit` 默认 20/最大 50 和 `limit + 1` 分页语义。
- 评论列表查询一次性装配作者摘要，不按评论逐条查用户。

### 3.2 Like 模块

- `PUT` 语义为“确保已点赞”，相同用户重复请求只保留一条 `post_likes` 记录。
- `DELETE` 语义为“确保未点赞”，记录不存在时仍返回成功。
- 幂等性由 Service 语义和 `post_likes(post_id, user_id)` 联合主键共同保证。
- 优先使用不会吞掉非重复键错误的 SQL；不使用会将外键、连接或其他错误一律忽略的宽泛错误处理。
- 操作前验证帖子存在，帖子不存在时 `PUT` 和 `DELETE` 都返回 404。

### 3.3 API 契约

#### `POST /api/v1/posts/:postId/comments`

请求：

```json
{"content":"Nice post"}
```

返回 HTTP 201 和新评论 DTO。

#### `GET /api/v1/posts/:postId/comments?limit=20&cursor=...`

返回 HTTP 200，`data` 为评论数组，`meta.next_cursor` 为字符串或 `null`。

#### `PUT /api/v1/posts/:postId/like`

确保当前用户已点赞，返回 HTTP 204。

#### `DELETE /api/v1/posts/:postId/like`

确保当前用户未点赞，返回 HTTP 204。

四个端点全部需要认证。评论和点赞成功后，帖子详情中的 `comment_count`、`like_count` 和 `liked_by_me` 必须可从 MySQL 立即观察到正确事实。

### 3.4 一致性与事务

- 单条评论插入、点赞插入或点赞删除不为了形式统一强制开启额外事务。
- 如实际实现需要多条互相依赖的 SQL 共同成功，则由 Service 使用显式事务边界。
- 不为计数更新增加第二份事实，不需要处理事实表和冗余计数的双写一致性。

## 4. 明确不做的内容

- 不实现评论回复、层级评论、评论编辑/删除或评论点赞。
- 不实现点赞用户列表、点赞历史、热度排名或冗余计数。
- 不实现“切换点赞” API。
- 不发布 RabbitMQ 消息，不实现通知或其他异步动作。
- 不访问 Redis，缓存失效属于 Phase 1-05。
- 不实现 Frontend 页面。

## 5. 目标文件和目录

```text
backend/internal/comment/
backend/internal/like/
backend/internal/post/
backend/internal/http/
backend/cmd/server/
dev/logs/Phase-01/Phase-01-04-评论与点赞.md
```

## 6. 详细实施步骤

1. 检查 Phase 1-03 交接物、实施记录和 Git 状态。
2. 定义 Comment 领域模型、创建输入、作者摘要和对外 DTO。
3. 实现评论内容校验与游标分页。
4. 实现 Comment Repository 的插入和按帖子键集分页查询，避免作者 N+1。
5. 实现帖子存在性检查、Comment Service 和 Handler。
6. 实现 Like Repository 的幂等插入、幂等删除与存在性查查。
7. 实现 Like Service 和 Handler，将非重复键数据库错误保持为真实失败。
8. 在受保护 `/api/v1` 路由组注册四个端点。
9. 使用真实 MySQL 验证评论列表、重复点赞、重复取消和并发重复点赞。
10. 在每次事实操作后重新查询帖子详情，验证聚合计数与 `liked_by_me`。
11. 运行 Backend 全部测试、vet 和前置批次回归。
12. 创建本批实施记录。

## 7. 测试与验收标准

### 7.1 评论

- 评论成功返回 201，空白、超长、未知字段和超限请求体被拒绝。
- 作者 ID 只来自当前登录身份。
- 目标帖子不存在时创建与列表均返回 404。
- 列表按新到旧、分页无重复无遗漏，作者摘要正确。
- 评论创建后帖子 `comment_count` 增加且与事实数相等。

### 7.2 点赞

- 首次 `PUT` 创建一条点赞，重复及并发 `PUT` 后仍只有一条。
- 首次 `DELETE` 删除点赞，重复 `DELETE` 仍返回 204。
- 两个用户可对同一帖子各保留一条点赞。
- 不存在的帖子返回 404，其他数据库错误不被误当作幂等成功。
- `like_count` 与 `liked_by_me` 随事实操作正确变化。

### 7.3 工程检查

- `go test ./...` 通过。
- `go vet ./...` 通过。
- 使用真实 MySQL 的并发和外键场景验收通过。
- 停止 Redis 时全部 Backend 业务闭环仍可完成。

## 8. 完成定义

- Backend 可完成注册、登录、发帖、评论、点赞和取消点赞。
- 评论和点赞事实完全持久化到 MySQL。
- 点赞幂等性经过顺序与并发测试。
- 帖子详情的聚合计数与个性化状态正确。
- 本批实施记录已创建，仅提交本批文件。
- 不在本批更新 `VERSION`。

## 9. 下一批次交接条件

交付给 Phase 1-05 前必须提供：

- 不依赖 Redis 且已通过测试的帖子详情 MySQL 读取路径。
- 评论、点赞和取消点赞成功后可调用的 Service 组装位置。
- 与用户无关的帖子公共投影和单独的 `liked_by_me` 查询边界。
- Redis 完全停止时 Backend 业务闭环通过的基线记录。
