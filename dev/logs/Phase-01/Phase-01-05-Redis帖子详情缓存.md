# Phase 1-05：Redis 帖子详情缓存实施记录

> 对应方案：`dev/imple/Phase-01/Phase-01-05-Redis帖子详情缓存.md`
> 开发分支：`develop/0.2.5`
> 完成版本：`0.2.5`

## 1. 实际完成内容

### 1.1 帖子详情公共投影与 cache-aside

- 新增独立的 `post.PublicProjection`，仅包含帖子、作者公共摘要和评论/点赞聚合计数。
- Redis 使用固定键 `gopulse:post:detail:v1:{postId}`、版本化 JSON 包装和 `REDIS_POST_DETAIL_TTL`。
- 缓存读取会校验 JSON、版本、帖子 ID、标题、正文、时间和作者必填字段；损坏值、旧版本、跨键 ID 和未知字段均按不可用缓存处理并回源 MySQL。
- 帖子详情缓存未命中或读取失败时查询 MySQL 公共投影，并使用独立的 `REDIS_OPERATION_TIMEOUT` context 最努力回填。
- `liked_by_me` 始终通过独立 MySQL 查询按当前用户装配，未进入 Redis 值；两个用户共享同一个公共缓存键时仍得到各自正确的点赞状态。
- MySQL 不存在时仍返回 `post_not_found`，且不写入空值缓存。

### 1.2 评论与点赞后的最努力失效

- 评论成功持久化后尝试删除目标帖子详情键。
- 点赞成功、重复点赞幂等成功、取消点赞成功和重复取消幂等成功后均尝试删除目标键。
- 失效发生在 MySQL 操作完成后，不参与数据库事务；Redis 删除失败只输出不含底层错误、凭据或缓存原文的诊断信息，不回滚事实操作，也不改变成功 HTTP 语义。
- MySQL 写入失败或帖子不存在时不执行缓存失效。

### 1.3 Redis 客户端与真实装配

- `platform.Redis` 新增受 context 控制的 `Get`、`Set`、`Delete` 和 `TTL` 操作，并将 Redis miss 转换为稳定哨兵错误。
- 新增 `backend/internal/platform/redis` 帖子详情 Repository，集中管理键、版本、JSON、TTL 和每次操作的短超时。
- Backend Server 使用同一个 Redis 客户端装配帖子读取、评论失效和点赞失效；readiness 继续复用既有 Redis checker。
- Redis 完全停止时 `/ready` 仍按 Phase 0 契约返回 `503`，业务详情读取、评论和点赞则降级到 MySQL；Redis 恢复后无需重启 Backend 即可恢复 readiness 和缓存读写。

### 1.4 一致性边界与文档

- 使用可控 fake 和屏障稳定复现“旧 MySQL 公共投影读取 → 事实更新与缓存删除 → 旧投影回填”的 cache-aside 竞态。
- 测试确认该竞态只会造成 TTL 内公共计数短期陈旧，不会覆盖 MySQL 事实，也不会污染逐用户 `liked_by_me`；清除缓存后重新从 MySQL 收敛。
- README 已补充帖子、评论、点赞 API、缓存字段边界、Redis 降级、失效失败和并发旧回填的 TTL 有界最终一致窗口。
- 根 `VERSION` 已从 `0.2.4` 更新为 `0.2.5`。

## 2. 实际变更文件

- `README.md`
- `VERSION`
- `backend/cmd/server/main.go`
- `backend/internal/comment/service.go`
- `backend/internal/comment/service_test.go`
- `backend/internal/http/comment_like_integration_test.go`
- `backend/internal/http/post_integration_test.go`
- `backend/internal/like/service.go`
- `backend/internal/like/service_test.go`
- `backend/internal/platform/redis.go`
- `backend/internal/platform/redis/post_detail.go`
- `backend/internal/platform/redis/post_detail_test.go`
- `backend/internal/platform/redis/post_detail_integration_test.go`
- `backend/internal/post/cache.go`
- `backend/internal/post/repository.go`
- `backend/internal/post/service.go`
- `backend/internal/post/service_test.go`
- `dev/logs/Phase-01/Phase-01-05-Redis帖子详情缓存.md`

未修改 Phase-01-02 后冻结的 `scripts/*.ps1`。

## 3. 实际验证命令与结果

### 3.1 Backend 单元、静态与竞态检查

实际执行：

```bash
cd backend
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
```

结果：全部通过。覆盖缓存命中、miss、读取失败、回填失败、无效 JSON、版本不匹配、必填字段缺失、未知字段、超时、失效失败、不同用户隔离和确定性旧回填竞态。

### 3.2 隔离 MySQL/Redis integration

在 `gopulse_integration`、用户 `gopulse_integration`、Redis DB `15` 和 `INTEGRATION_TESTS=1` 的隔离环境中先应用迁移，然后实际执行：

```bash
cd backend
go test -count=1 -tags=integration ./...
```

结果：全部通过。真实 Redis 验证包括：

- 首次 Set 后存在预期键和正 TTL。
- JSON 版本为 `1`，不包含 `liked_by_me`、密码、JWT 或 Cookie 字段。
- Get 命中、Del 失效、miss 和重新回填均正常。
- 损坏或旧版本值不会被作为业务响应。
- HTTP 评论、点赞和取消点赞在已预先填充缓存的情况下删除并重建正确计数。

### 3.3 Redis 停止业务回归

停止本批隔离 Redis 后实际执行：

```bash
cd backend
go test -count=1 -tags=integration \
  ./internal/auth ./internal/comment ./internal/like \
  ./internal/post ./internal/http
```

结果：全部通过。选择业务 package 是因为 `internal/platform` 和 `internal/platform/redis` 的真实依赖验收按契约要求 Redis 在线，不能在故障注入时作为业务降级测试运行。

随后在 Redis 保持停止时启动真实 Backend，完成两用户注册、退出与重新登录、发帖、详情读取、评论、两用户点赞和一用户取消点赞；同一 Backend 进程中恢复 Redis 后再次验证 readiness 和缓存重建。观测结果：

```json
{
  "ready_while_redis_down": 503,
  "comment_count": 1,
  "like_count": 1,
  "viewer_b_liked": true,
  "ready_after_recovery": 200,
  "cache_ttl_seconds": 300,
  "cache_contains_liked_by_me": false
}
```

结果：通过。Redis 停止时 MySQL 业务闭环可用；恢复后无需重启 Backend。

### 3.4 Redis 清空与重建

在隔离 Redis DB `15` 中预先填充真实帖子详情键，执行 `FLUSHDB` 后再次读取同一帖子详情。观测结果：

```json
{
  "key_before_flush": 1,
  "key_after_flush": 0,
  "key_after_rebuild": 1,
  "comment_count": 0,
  "like_count": 0
}
```

结果：通过。Redis 清空不影响 MySQL 事实，下一次详情读取重新建立缓存。

### 3.5 Frontend、脚本、Compose 与治理回归

实际执行：

```bash
cd frontend
npm test -- --run
npm run typecheck
npm run build

cd ..
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_branch.py \
  --branch develop/0.2.5 \
  --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh
docker compose --env-file .env.example \
  --file deploy/compose.yaml config --quiet
git diff --check
```

结果：全部通过；Frontend 3 个测试文件、18 个测试通过，分支治理确认 `develop/0.2.5` 与总方案分配及 `VERSION=0.2.5` 一致。

## 4. 与实施方案的偏差及原因

- 没有功能范围偏离。
- Redis 故障注入期间未运行要求真实 Redis 在线的 `internal/platform` 和 `internal/platform/redis` integration package，而是运行全部 MySQL 业务 package 并补充真实 Backend 进程 smoke；Redis 恢复后又运行全量 integration。这样同时验证了“业务可降级”和“依赖验收不得静默 skip”两个不同契约。
- 帖子详情的逐用户点赞事实直接由 Post MySQL Repository 的独立 `LikedByViewer` 查询装配，没有让 Post Service 反向依赖 Like Service，避免形成领域服务装配环；查询语义与方案要求一致。

## 5. 已知限制与后续项

- Phase 1 不提供分布式锁、singleflight、延迟双删、空值缓存或缓存预热。
- 失效失败或并发旧读在成功删除后回填旧投影时，公共评论/点赞计数可能在 `REDIS_POST_DETAIL_TTL` 内短期陈旧。
- 缓存陈旧不会修改 MySQL 事实，不影响评论/点赞成功语义，也不会跨用户泄漏 `liked_by_me`。
- 当前只缓存帖子详情公共投影；帖子列表、评论列表、会话和点赞事实仍不缓存。
- Phase-01-06 可直接使用已稳定的 Backend API、Redis 降级语义和同进程故障恢复基线完成 Frontend 业务闭环与 Phase 1 收口。
