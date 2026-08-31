# Phase 1-03：帖子发布与查询实施方案

> 执行序号：3 / 6
> 前置批次：Phase 1-01、Phase 1-02 已完成并通过验收
> 总方案来源：[Phase-01-总实施方案.md](Phase-01-总实施方案.md)

## 1. 批次目标

实现受认证保护的帖子发布、列表和详情查询，建立稳定的游标分页、作者摘要、评论/点赞聚合计数和当前用户点赞状态读模型。

本批所有读取都直接以 MySQL 为事实来源，不引入 Redis。完成后先证明同步读写正确，再在 Phase 1-05 增加可降级缓存。

## 2. 前置条件

- Phase 1-02 已合并到配置的主远程 `main`，根 `VERSION` 与前一批目标版本一致。
- 已从该远程最新 `main` 创建总方案为本批分配的开发分支。
- `posts`、`comments` 和 `post_likes` 表及索引已通过迁移验证。
- 用户注册、登录、认证中间件和当前用户 ID 上下文已可用。
- 统一 JSON 输入、响应和错误映射已稳定。
- Phase 1-01 和 Phase 1-02 实施记录已完成。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 Post 模块

建立 Post 领域模型、创建输入、列表/详情 DTO、MySQL Repository、Service 和 Handler。

- 发布者 ID 只来自认证 context，不接受请求体中的 `author_id`。
- 标题去除首尾空白后为 1–120 个字符。
- 正文去除首尾空白后为 1–10000 个字符。
- 创建成功后返回完整帖子 DTO，不要求客户端根据输入伪造服务器字段。

### 3.2 列表分页

- 排序固定为 `created_at DESC, id DESC`。
- `limit` 默认 20，最小 1，最大 50。
- `cursor` 对客户端不透明，使用 URL-safe Base64 封装最后一项的 UTC 时间和 ID；解析时严格校验版本、时间和正整数 ID。
- Repository 使用键集分页条件，不使用随页数增大的 offset 作为主分页方式。
- 查询 `limit + 1` 条判断是否存在下一页，只在确实还有数据时返回 `next_cursor`。
- 无效游标或 limit 返回 HTTP 400 和 `validation_failed`。

### 3.3 读模型

帖子列表和详情至少返回：

```text
id
title
content
created_at
updated_at
author.id
author.username
comment_count
like_count
liked_by_me
```

实现要求：

- 列表查询不得为每条帖子再分别执行作者、计数或点赞状态查询，避免明显 N+1。
- 聚合计数以 `comments` 和 `post_likes` 事实为准，不写入 `posts` 冗余字段。
- `liked_by_me` 根据当前认证用户和 `post_likes` 计算。
- 不存在的帖子返回 HTTP 404 和 `post_not_found`。

### 3.4 API 契约

#### `POST /api/v1/posts`

请求：

```json
{"title":"First post","content":"Hello GoPulse"}
```

返回 HTTP 201 和新帖子 DTO。

#### `GET /api/v1/posts?limit=20&cursor=...`

返回 HTTP 200，`data` 为帖子数组，`meta.next_cursor` 为字符串或 `null`。

#### `GET /api/v1/posts/:postId`

返回 HTTP 200 和帖子详情 DTO；目标不存在返回 404。

三个端点全部需要认证，未登录时统一返回 401。

## 4. 明确不做的内容

- 不实现帖子编辑、删除、标签、媒体或富文本。
- 不实现用户帖子专页、搜索、热点排名或推荐。
- 不实现评论和点赞写入端点，该工作属于 Phase 1-04。
- 不实现 Redis 读取、回填或失效，该工作属于 Phase 1-05。
- 不为了列表性能增加冗余计数列。
- 不实现 Frontend 页面。

## 5. 目标文件和目录

```text
backend/internal/post/
backend/internal/http/
backend/cmd/server/
VERSION
dev/logs/Phase-01/Phase-01-03-帖子发布与查询.md
```

## 6. 详细实施步骤

1. 检查前两批交接物、实施记录和 Git 状态。
2. 定义 Post 领域模型、创建输入和对外 DTO。
3. 实现标题与正文校验，覆盖空白、Unicode 字符和上限边界。
4. 实现游标编码/解码和分页参数校验，为损坏、缺字段和版本不匹配编写测试。
5. 实现 Post Repository 的创建、键集分页列表和详情查询。
6. 在列表和详情查询中一次性装配作者、聚合计数和 `liked_by_me`，检查 SQL 计划可使用预期索引。
7. 实现 Post Service 和 Handler，发布者只来自认证 context。
8. 在受保护 `/api/v1` 路由组注册三个帖子端点。
9. 使用真实 MySQL 创建足够数据，验证同时间并列、多页、空页和新数据插入后的分页稳定性。
10. 验证列表查询不会随返回帖子数量产生线性数量的额外 SQL。
11. 运行 Backend 全部测试、vet 和 Phase 0 回归。
12. 将根 `VERSION` 更新为总方案为本批分配的目标版本，并创建本批实施记录。

## 7. 测试与验收标准

### 7.1 单元与 Handler 测试

- 发布帖子成功返回 201，输入不合法返回 400，未登录返回 401。
- 请求体中出现 `author_id` 作为未知字段被拒绝。
- 列表默认 limit、边界 limit、无效 limit 和无效游标行为正确。
- 无下一页时 `next_cursor` 为 `null`，有下一页时游标可继续查询且不重复记录。
- 详情正确返回作者、零或非零计数与 `liked_by_me`。
- 不存在帖子返回 404 和 `post_not_found`。

### 7.2 Repository 集成测试

- 帖子作者与当前登录用户一致。
- 按 `created_at DESC, id DESC` 稳定排序。
- 多页数据无重复、无遗漏，返回数不超过 limit。
- 评论和点赞事实能够正确反映在计数与 `liked_by_me`。
- 记录并检查关键查询的实际执行计划，不把未检查的索引使用写为已通过。

### 7.3 工程检查

- `go test ./...` 通过。
- `go vet ./...` 通过。
- 用户/认证和 Phase 0 契约回归通过。
- Redis 不可用不影响本批帖子读写，因为本批尚未引入缓存依赖。

## 8. 完成定义

- 帖子发布、列表和详情 API 按契约可用。
- 列表键集分页、聚合计数和用户点赞状态有单元与真实 MySQL 测试保护。
- 帖子同步读取仅依赖 MySQL，尚未与 Redis 耦合。
- 本批实施记录已创建，根 `VERSION` 已更新为总方案分配的本批目标版本，仅提交本批文件。

## 9. 下一批次交接条件

交付给 Phase 1-04 前必须提供：

- 可验证帖子存在性的 Service 或 Repository 边界。
- 可从评论和点赞事实聚合的稳定详情读模型。
- 不透明游标编解码和分页响应工具。
- `post_not_found` 与认证错误契约。
