# 06. SQL 与查询优化

> 学习目标：结合 GoPulse 帖子、评论和通知列表，理解游标分页、LIMIT、JOIN、EXISTS、COUNT、慢查询定位与 SQL 优化的证据链。

## 一、八股文基础

### 1. OFFSET 分页与游标分页有什么区别？

传统分页：

```sql
SELECT id, created_at
FROM posts
ORDER BY created_at DESC, id DESC
LIMIT 20 OFFSET 100000;
```

页码越靠后，执行器通常需要访问并跳过更多排序结果。它在大偏移量下可能成本高，也可能因并发新增/删除导致用户翻页重复或遗漏。

游标（Keyset）分页：

```sql
SELECT id, created_at
FROM posts
WHERE created_at < ? OR (created_at = ? AND id < ?)
ORDER BY created_at DESC, id DESC
LIMIT 21;
```

游标保存上一页末尾的排序键，让查询从有序边界继续。它适合向前或向后逐页浏览，但不天然支持任意跳转页码，也不能在并发修改排序键时承诺绝对稳定的快照。

**为什么是 `LIMIT 21`？** 如果页面展示上限是 20，多读取 1 条用于判断是否存在下一页。

### 2. `JOIN`、`EXISTS`、`COUNT(*)` 适用什么场景？

- `JOIN`：需要另一表的字段时关联，例如展示帖子作者的用户名。
- `EXISTS`：只关心是否存在关联关系，例如当前用户是否点赞。
- `COUNT(*)`：需要精确计数，例如评论数；但对每条帖子执行相关子查询可能成为成本热点，不能假设开销永远很小。

## 二、GoPulse 中的实际应用

### 场景 A：帖子流分页

[`backend/internal/post/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/post/repository.go) 中 `listStatement` 的关键构造逻辑：

```go
if options.Cursor != nil {
    query += fmt.Sprintf(
        " AND (%s < ? OR (%s = ? AND p.id < ?))",
        timestamp, timestamp,
    )
    arguments = append(arguments,
        options.Cursor.CreatedAt,
        options.Cursor.CreatedAt,
        options.Cursor.ID,
    )
}
query += " ORDER BY " + timestamp + " DESC, p.id DESC LIMIT ?"
arguments = append(arguments, options.Limit+1)
```

普通帖子流按 `p.created_at` 排序；收藏模式改为 `b.created_at`，并关联 `post_bookmarks b`。因此游标字段的含义依赖列表模式，不应无条件解释成帖子本身的创建时间。

`post/pagination.go` 还将游标打包为版本化 Base64URL 令牌，包含时间戳和 ID，并校验令牌结构。这是**应用层游标协议**，不是 MySQL 特殊的数据类型。

### 场景 B：评论列表

[`backend/internal/comment/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/comment/repository.go)：

```sql
WHERE c.post_id = ? AND c.id < ?
ORDER BY c.id DESC
LIMIT ?;
```

评论列表只需要按 ID 排序，游标可以简化成一个 `id`；配合索引 `(post_id,id DESC)`，能够有效服务这一访问模式。

### 场景 C：通知列表

[`backend/internal/notification/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/notification/repository.go)：

```sql
WHERE n.recipient_id = ?
  AND (n.created_at < ? OR (n.created_at = ? AND n.id < ?))
ORDER BY n.created_at DESC, n.id DESC
LIMIT ?;
```

仓库建有 `idx_notifications_recipient_created_id(recipient_id,created_at DESC,id DESC)`，与“按接收人筛选 + 时间及主键排序”对应。和普通帖子列表相比，`recipient_id` 是联合索引的最左列。

### 场景 D：帖子展示中的关联查询

帖子列表投影包含：

```sql
(SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id) AS comment_count,
(SELECT COUNT(*) FROM post_likes likes WHERE likes.post_id = p.id) AS like_count,
EXISTS(SELECT 1 FROM post_likes my_like
       WHERE my_like.post_id = p.id AND my_like.user_id = ?) AS liked_by_me
```

`COUNT` 与 `EXISTS` 分别解决“数量”与“是否存在”问题。相关子查询不等于必然慢查询；执行器、数据规模、索引和单页记录数量决定实际开销。

## 三、如何在项目中验证优化

按固定的证据顺序执行，而不是直接凭经验改 SQL：

1. **提取真实 SQL**：从 `repository.go` 记录最终拼接出的查询条件和参数分布，区分普通、作者、关注和收藏列表。
2. **检查索引**：`SHOW INDEX FROM posts;` / `SHOW INDEX FROM notifications;`。
3. **执行计划**：对代表性 SQL 运行 `EXPLAIN`，重点关注实际选中的索引、估算扫描行数、排序和回表可能性。
4. **实际测量**：在可控测试数据上使用 `EXPLAIN ANALYZE`、慢查询日志和请求耗时指标；比较变更前后的 P95/P99，而不只看单次延迟。
5. **回归正确性**：验证重复时间戳、空结果、分页最后一页、并发新增/删除、收藏时间排序等边界情况。

下面的 SQL 只用于复现分析，不代表 GoPulse 已经运行过此 SQL：

```sql
EXPLAIN
SELECT id, author_id, created_at
FROM posts
WHERE created_at < '2026-01-01 00:00:00.000000'
ORDER BY created_at DESC, id DESC
LIMIT 21;
```

## 四、设计思考

- **索引不是越多越好**：查询性能与写入成本需要平衡。
- **不要轻易替换 `EXISTS` 为 `COUNT(*) > 0`**：只判断存在时 `EXISTS` 可以在找到匹配记录后停止；最终仍以计划验证。
- **避免无依据地宣称“解决 N+1”**：帖子查询中仍有相关计数和关系检查；应评估实际执行计划及批量预取路径，而不是仅凭 SQL 语法下结论。
- **`ORDER BY` 需要稳定的唯一边界**：仅按 `created_at` 排序会让同时间记录的翻页结果不确定；追加 `id` 更可靠。
- **过滤条件可能改变最优索引**：帖子流源码使用 `FORCE INDEX`，但用户筛选和关注条件会改变查询选择性，应分别评估。

## 五、关联面试题

1. **为什么深分页慢？** 较大 OFFSET 往往要求扫描/跳过大量排序结果。
2. **游标分页为什么常用 `(created_at,id)`？** 用唯一且稳定的排序元组表示严格的页边界。
3. **`EXISTS` 与 `COUNT(*)` 有什么区别？** 前者问存在性，后者求精确数量。
4. **如何定位慢 SQL？** 真实参数、慢日志、执行计划、索引统计、实测耗时和业务回归相结合。
5. **建了联合索引就能省排序吗？** 不一定，过滤条件、排序方向、表达式和优化器计划都会影响。
