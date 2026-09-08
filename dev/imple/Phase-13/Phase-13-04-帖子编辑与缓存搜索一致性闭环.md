# Phase-13-04：帖子编辑与缓存搜索一致性闭环实施方案

> 当前状态：已完成（2026-09-08）。实现与验收记录见 `dev/logs/Phase-13/Phase-13-04-帖子编辑与缓存搜索一致性闭环.md`。本文档定义 Phase 13 第四个执行批次的范围与验收合同；目标版本 `1.10.4`、开发分支 `develop/1.10.4` 和执行顺序以 `Phase-13-总实施方案.md` 为准。实际开工前从最新 `origin/main` 创建本批分支。

## 1. 批次目标

交付作者专属帖子编辑，并把编辑从 MySQL 事实通过 outbox、缓存新鲜度和 Elasticsearch indexer 收敛到所有读取路径：

```text
作者 PATCH 帖子
  → MySQL posts + edited_at/revision + post.updated outbox（同事务）
  → Redis 旧 projection 不可被接受
  → Search Indexer 重读最新 MySQL 事实
  → 列表/详情/搜索显示新内容和已编辑时间
```

本批不实现永久删除，但必须为下一批的 delete/replay 防复活提供明确 revision、事实存在性和事件合同。

## 2. 前置条件

- Phase-13-03 已合入 `main`；所有帖子 DTO 已有 `bookmarked_by_me`，UserAppShell 和 PostRow action contract 稳定。
- 从最新 `origin/main` 创建 `develop/1.10.4`，确认根和 Frontend 版本为 `1.10.3`。
- 核对现有 posts update timestamp、Redis post detail cache、outbox event payload、Search Indexer document mapping/alias 和 MySQL hydration。
- 保存现有评论/点赞/通知/搜索和管理员回归证据。

## 3. 实施范围

### 3.1 数据模型与写事务

- migration 为 `posts` 增加 nullable `edited_at`，并推荐增加单调 `content_revision`（创建为 1，内容变更递增）。若实施不使用 revision，必须提供等价、可测试且能支撑 Phase-13-05 防乱序复活的版本合同。
- `PATCH /api/v1/posts/:postId` 只允许作者，使用创建帖子相同的 title/content trim、长度和空内容校验；不接受 client owner、created_at、edited_at 或 revision。
- 非作者统一 `403 permission_denied`，不存在统一 `404`；编辑请求以当前 MySQL row 为准。
- 标题/正文未改变时采用明确幂等行为，优先不递增 revision、不修改 edited_at、也不发布重复 event；实现与测试必须一致。
- changed content、edited_at、updated_at/revision 和 `post.updated` outbox 在同一事务；HTTP 成功不依赖 Redis/Elasticsearch 同步完成。

### 3.2 事件与搜索收敛

- `post.updated` payload 只携带 post id、revision 和必要时间/actor 元数据，不将客户端旧正文作为权威 payload。
- Search Indexer 消费时重新读取 MySQL：存在则以当前 revision upsert，缺失则执行 delete；旧 revision 不覆盖新 revision。
- mapping/alias 初始化和 reindex 流程与现有生命周期兼容；重复消息、worker 重试和 indexer restart 可重放。
- 搜索结果继续以 MySQL 水合当前帖子；只有索引收敛后，新关键词才命中，旧-only 关键词不再命中；过渡期旧命中不能泄漏过时正文。

### 3.3 Redis 缓存安全

- 共享帖子 cache 增加可验证的 revision/更新时间元数据，或引入等价 read-through freshness contract。
- 详情读路径接受 cache 前进行低成本 MySQL existence/revision 检查；不匹配时回源并覆盖/删除旧 key。
- 编辑成功即使 cache invalidation 失败，也不能让下一次详情接受旧标题/正文；禁止以“等 TTL 自然过期”作为一致性合同。
- viewer-specific following/like/bookmark/permission/author profile 继续在共享 cache 外水合。
- 不因本批编辑而缓存历史版本；返回明确 `edited_at` 和当前内容。

### 3.4 Frontend 编辑交互

- 作者在自己帖子菜单中看到“编辑”，非作者没有该动作；编辑表单复用完整验证和字符计数。
- 保存中禁用重复提交；成功刷新局部帖子状态或重新读取详情/列表，使内容、edited_at、搜索结果反馈一致。
- 失败保留用户输入并显示错误；并发/旧版本冲突若实现支持，必须使用稳定错误语义而非静默覆盖。
- 编辑不产生普通通知，不显示历史版本或撤销伪功能。

## 4. 不在本批范围

- 永久删除、依赖数据清理、通知墓碑、防删除复活（下一批）。
- 帖子历史版本、协同编辑、草稿、媒体、转发、推荐排序。
- 管理员代编辑或管理端视觉重构。

## 5. 建议实施顺序

1. 增加 edited_at/revision migration 和 post model/repository/service transaction。
2. 接入 patch route、author authorization、idempotent no-op 和 outbox event。
3. 扩展 Indexer event routing、document mapping、revision/existence convergence。
4. 修复 Redis detail cache acceptance/invalidation contract；补充 stale edit regression。
5. 接入作者编辑 UI、edited marker、失败/保存反馈。
6. 运行受影响 Go/Frontend/搜索/缓存测试与代表性 browser flow。
7. 更新版本至 `1.10.4`，建立实施记录并提交。

## 6. 预计直接影响文件

- `backend/migrations/*`
- `backend/internal/post/*`
- `backend/internal/outbox/*`
- `backend/internal/search/*`
- `backend/internal/worker/*` 或 Search Indexer 直接消费位置
- `backend/internal/platform/redis/*`
- `backend/internal/http/*`
- `frontend/src/types/api.ts`
- `frontend/src/services/api.ts`
- `frontend/src/components/*`
- `frontend/src/views/*`
- `frontend/e2e/*`
- `VERSION`、Frontend 版本元数据及 `dev/logs/Phase-13/Phase-13-04-帖子编辑与缓存搜索一致性闭环.md`

不得为了实现编辑重写无关搜索排序、缓存框架或管理员页面。

## 7. 批次验收标准

### 7.1 权限、事务与 API

- 作者可编辑；非作者获得 `403`，不存在帖子获得 `404`；标题/正文校验与创建一致。
- 成功编辑同时产生最新内容、edited_at/revision 和一条可去重 `post.updated` 事实；相同内容请求符合定义的幂等行为。
- 断开/重试 Redis 或 Search 不影响 MySQL 成功事实，恢复后投影收敛。

### 7.2 Cache/search consistency

- 详情在已有旧 Redis cache 命中条件下，编辑后不能返回旧正文；缓存失败时仍回源最新内容。
- Search Indexer 重放旧 update/create 事件不会用旧 revision 覆盖新内容；索引收敛后新关键词可命中，旧-only 关键词不命中。
- 搜索结果以当前 MySQL 水合，缓存、列表、详情、用户帖子、Following、收藏中的内容和 edited_at 一致。

### 7.3 Frontend

- 只有作者看到编辑入口；编辑成功后时间线、详情和搜索反馈最新内容并有“已编辑”标识。
- 表单具备提交中、字段错误、服务错误、重试和可访问状态；没有历史版本或删除按钮伪装在本批。
- 评论、点赞、关注、收藏、通知、管理员页面代表性回归通过。

### 7.4 完成条件

本节全部通过、无缓存/搜索阻断，实施记录真实完整、版本为 `1.10.4`、提交只包含本批文件时，Phase-13-04 才可标记完成。删除实现留给下一批，不以手工清缓存/索引作为通过条件。

## 8. 固定验证命令与回归范围

```bash
cd backend
go test ./internal/post ./internal/search ./internal/outbox ./internal/worker ./internal/http ./internal/platform/redis
go test -tags=integration ./internal/post ./internal/search ./internal/worker ./internal/http

cd ../frontend
npm test
npm run typecheck
npm run build
npm run test:e2e -- --grep "edit|搜索|cache"

cd ..
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.10.4 --base-ref origin/main
git diff --check origin/main...HEAD
```

- 若 Search Indexer 的测试按独立目录或 package 分布，使用其模块内直接受影响的等价 `go test`，并在实施记录写明。
- 集成环境必须安全地指向白名单 test DB/Redis/Elasticsearch/RabbitMQ；不使用产品数据。
- 回归至少覆盖创建、详情、帖子搜索、评论/点赞、Following、收藏、通知和一条管理员路由。
- 只有修改消息协议、共享 Redis 或 Compose 服务时，才基于具体风险扩展 full-stack verify。
- 成功检查在相关输入未变化时不重复执行。

## 9. 实施记录要求

完成前创建：

`dev/logs/Phase-13/Phase-13-04-帖子编辑与缓存搜索一致性闭环.md`

记录实际 revision/cache/indexer 策略、乱序测试、实际命令和结果，并将交给 Phase-13-05 的 `post.updated`/delete coexistence 与缓存读取合同写清楚。
