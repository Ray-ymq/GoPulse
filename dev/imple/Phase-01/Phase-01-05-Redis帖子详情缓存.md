# Phase 1-05：Redis 帖子详情缓存实施方案

> 执行序号：5 / 6
> 前置批次：Phase 1-01 至 Phase 1-04 已完成并通过验收
> 总方案来源：[Phase-01-总实施方案.md](Phase-01-总实施方案.md)

## 1. 批次目标

在已验证正确的 MySQL 帖子详情读取路径上增加单一、明确的 Redis cache-aside 场景：缓存帖子详情公共投影，并在评论、点赞和取消点赞后尝试失效。

本批必须证明 Redis 是可降级的性能优化而不是业务事实来源：Redis 超时、数据损坏、清空或完全停止时，MySQL 业务闭环仍可用。

## 2. 前置条件

- Phase 1-04 已合并到配置的主远程 `main`，根 `VERSION` 与前一批目标版本一致。
- 已从该远程最新 `main` 创建总方案为本批分配的开发分支。
- 帖子详情公共字段与 `liked_by_me` 已可分离查询。
- 评论、点赞和取消点赞的 MySQL 事实写入已稳定。
- Redis 客户端、配置、连接关闭和 readiness checker 已由 Phase 0 提供。
- `REDIS_POST_DETAIL_TTL` 和 `REDIS_OPERATION_TIMEOUT` 配置已完成校验。
- Phase 1-01 至 Phase 1-04 实施记录已完成。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 缓存模型与键

```text
key: gopulse:post:detail:v1:{postId}
TTL: REDIS_POST_DETAIL_TTL（本地默认 5m）
```

缓存值是带明确版本的 JSON 公共投影，可包含：

```text
post id/title/content/created_at/updated_at
author id/username
comment_count
like_count
```

不得包含：

- `liked_by_me` 或其他用户个性化状态。
- 密码哈希、JWT、Cookie、连接信息或内部错误。
- 评论列表本身。

缓存 DTO 与 HTTP DTO 不必共用同一结构；应明确限定可缓存字段，避免后续 HTTP 字段自动进入 Redis。

### 3.2 读取与回填

1. 帖子详情 Service 先在独立短超时 context 内读取 Redis。
2. 命中后校验 JSON、版本和必填字段，合法时使用公共投影。
3. 未命中、超时、连接错误或损坏值一律回退 MySQL。
4. MySQL 不存在时返回 `post_not_found`，本批不建立空值缓存。
5. MySQL 返回公共投影后，使用新的短超时 context 尝试写入 Redis 并设置 TTL。
6. 回填失败仅记录不含敏感数据的诊断信息，仍返回 MySQL 结果。
7. 无论公共投影来自 Redis 还是 MySQL，`liked_by_me` 都通过 MySQL 根据当前用户单独查询并装配。

### 3.3 失效流程

在以下 MySQL 操作成功之后尝试删除目标帖子详情键：

- 发表评论。
- 点赞。
- 取消点赞。

规则：

- Redis 失效在 MySQL 成功之后执行，不参与 MySQL 事务。
- 失效失败不回滚事实操作，不把成功的评论或点赞响应改为失败。
- 幂等点赞/取消请求即使未改变 MySQL 行，也可尝试失效，保持实现简单与收敛。
- 失效失败时允许计数在 TTL 内短期陈旧，该限制必须在实施记录和开发文档中明确。

### 3.4 可观察故障语义

- Redis 故障时 `/ready` 继续按 Phase 0 契约返回 503 并标记 Redis `down`。
- 业务 API 同时按本批契约降级到 MySQL；`/ready` 表示依赖不完整，不表示所有业务必然失败。
- 缓存故障日志需限制在合理粒度，不在每次请求输出密码、完整连接串或缓存原始值。

## 4. 明确不做的内容

- 不缓存帖子列表、评论列表、用户会话或点赞事实。
- 不引入分布式锁、singleflight、空值缓存、延迟双删或缓存预热。
- 不对缓存命中率做生产级指标与告警。
- 不使用 Redis 实现认证、限流、排名或分布式事务。
- 不改变 MySQL 事实表或增加冗余计数。

## 5. 目标文件和目录

```text
backend/internal/post/
backend/internal/comment/
backend/internal/like/
backend/internal/platform/redis/
backend/cmd/server/
VERSION
dev/logs/Phase-01/Phase-01-05-Redis帖子详情缓存.md
```

## 6. 详细实施步骤

1. 检查 Phase 1-04 交接物、Redis 配置、实施记录和 Git 状态。
2. 定义带版本的帖子公共投影缓存 DTO 和集中键生成函数。
3. 定义最小缓存读、写、删接口，使用 fake 测试 Service 降级。
4. 实现 Redis Repository，为每次操作使用独立短超时。
5. 在帖子详情 Service 实现缓存优先、MySQL 回退和最努力回填。
6. 将 `liked_by_me` 保持在缓存外，为两个用户读取同一缓存键编写隔离测试。
7. 在评论和点赞 Service 的 MySQL 成功路径后增加最努力缓存失效。
8. 为命中、未命中、无效 JSON、旧版本、超时、连接失败、回填失败和失效失败编写测试。
9. 使用真实 Redis 验证 TTL、键内容、失效和清空后重建。
10. 停止 Redis 执行全部 Backend 业务闭环，再恢复 Redis 并验证无需重启 Backend。
11. 运行 Backend 全部测试、vet 和前置批次回归。
12. 将根 `VERSION` 更新为总方案为本批分配的目标版本，并创建本批实施记录。

## 7. 测试与验收标准

### 7.1 单元测试

- 缓存命中时公共投影来自 Redis，`liked_by_me` 仍按当前用户正确计算。
- 未命中时查询 MySQL 并尝试回填 TTL。
- Redis 读取、写入或删除失败不改变 MySQL 成功响应。
- 损坏 JSON、版本不匹配或必填字段缺失按未命中处理。
- MySQL 未找到时返回 404，不建立空值缓存。
- 不同用户不会通过公共缓存值相互泄漏 `liked_by_me`。

### 7.2 真实 Redis 验收

1. 首次读取帖子详情后出现预期键和正 TTL。
2. 缓存值不包含密码、JWT 或个性化点赞状态。
3. 发表评论、点赞和取消点赞后目标键被尝试删除，下次读取重建新计数。
4. 清空 Redis 不丢失任何业务事实，下次读取能重建缓存。
5. 停止 Redis 后 `/ready` 返回 503，但注册、登录、发帖、查询、评论和点赞仍成功。
6. 恢复 Redis 后无需重启 Backend，`/ready` 和缓存读写均恢复。

### 7.3 工程检查

- `go test ./...` 通过。
- `go vet ./...` 通过。
- Redis 故障路径在明确超时上限内返回。
- Phase 0 readiness 契约与 Phase 1 MySQL 业务语义均未回归。

## 8. 完成定义

- 只存在一类明确业务缓存：帖子详情公共投影。
- 缓存读取、回填、失效和降级经过单元与真实 Redis 测试。
- Redis 不可用时核心业务仍可用，MySQL 事实不丢失。
- TTL 内可能短期陈旧的最终一致限制已被明确记录。
- 本批实施记录已创建，根 `VERSION` 已更新为总方案分配的本批目标版本，仅提交本批文件。

## 9. 下一批次交接条件

交付给 Phase 1-06 前必须提供：

- 全部稳定的 Phase 1 Backend API 契约和错误码。
- 可通过 Cookie 完成的真实注册、登录和退出调用记录。
- 帖子与评论分页、点赞幂等和 Redis 降级的已验证语义。
- Backend 重启、Redis 清空/故障/恢复的验收基线。
