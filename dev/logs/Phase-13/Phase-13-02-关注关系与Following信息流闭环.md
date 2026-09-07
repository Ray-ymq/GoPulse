# Phase-13-02 开发记录

## 状态与执行基线

- 2026-09-07，本地实现与验收完成，目标版本 `1.10.2`，分支 `develop/1.10.2`；未声称已推送或合入 main。
- 开工执行 `git fetch origin`，确认 Phase-13-01 已合入 `origin/main`（`229a766`），版本 `1.10.1`。工作区最初干净；已有同名本地分支没有独立提交，位于旧 main，使用 `git merge --ff-only origin/main` 将它更新为本批最新基线，没有重命名远程分支或覆盖用户改动。
- 范围仅为关注关系、私有关系页、Following、关注通知及直接回归；没有实现收藏、编辑、删除或推荐。

## 实际实现与文件

### 数据与 API

- `backend/migrations/000007_user_follows.{up,down}.sql`：关系复合主键、两方向时间/用户 ID 分页索引、级联外键与禁止 self-follow 的 CHECK。通知 post_id 可空，并扩展通知形状及 outbox 类型约束。down 显式删除关系事件/通知后恢复旧约束，属于有损回退；本次实际执行新库向上迁移，未执行 down。
- `backend/internal/user/follow.go`：事务锁定当前 follower，唯一键处理重复写入，首次插入关系与 `user.followed` outbox 同事务提交；重复关注不新增事件，重复取消幂等，不发取消事件。目标不存在返回 user_not_found，自关注返回 validation_failed。关系查询只能以服务器会话用户为 owner。
- `backend/internal/user/profile.go`：资料及用户搜索返回 viewer 的 `following`，不返回目标的完整关系或数量；搜索仍为既有排序与签名游标。
- `backend/internal/http/{api,user_handler}.go`、`backend/cmd/server/main.go`：接入 PUT/DELETE `/users/:username/follow`、GET `/users/me/following`、`/users/me/followers`、`/posts/following`。Gin 同层通配符须复用 `:username` 名称，follow handler 将其解析为正整数 user ID；对外实际 URL 与计划一致。不暴露任意用户关系列表。
- `backend/internal/post/{model,repository,service}.go`：Following 使用当前 viewer 的数据库关系过滤，按帖子 created_at/id 倒序 keyset 分页。列表与搜索批量水合在原 SQL 中查询 author.following，保持单次列表查询；详情在共享缓存读取后重新查询 viewer 关系，不把私人状态当作共享缓存事实。

### 事件与通知

- `backend/internal/bus/envelope.go`：新增 `user.followed` 与 `user.followed.v1`；actor/recipient 表示 follower/followed，包含事件 ID、UTC 时间，无 post/comment 资源，拒绝自身事件。
- `backend/internal/platform/rabbitmq_{topology,publisher}.go`：复用 Business exchange、主队列、retry/dead 队列和可靠 publisher；Search 队列不接收关系事件。
- `backend/internal/notification/{model,repository}.go`：关注通知持久化 NULL post/comment，公开 DTO 的 post_id 可空，actor 增加 display_name。旧 comment/like 数据仍可读，source_event_id 唯一约束去重，已读更新继续 recipient-scoped。内部历史查询用 COALESCE 将无帖子映射为内部零值，公开响应仍为 null。
- 沿用现有 Worker ack/retry/self-event 处理；新增真实 RabbitMQ 测试证明关注事件一次临时失败、重放与最终单行通知。

### Frontend

- 新增 `components/FollowButton.vue`、`composables/useFollowing.ts`：按 viewer/target 共享乐观状态与 pending，失败回滚并显示错误，退出清理；本人不显示关注按钮。
- `components/PostCard.vue`、`views/{PostDetailView,ProfileView,SearchView}.vue`：作者、资料和搜索结果关注控制；仅本人资料提供 `/me/following`、`/me/followers` 入口。
- `views/PostsView.vue`：真实全部/Following tabs，加载、空态、错误重试和分页；`views/RelationsView.vue`、`router/index.ts`：受登录守卫保护的本人关系页与分页。
- `views/NotificationsView.vue`、`services/api.ts`、`types/api.ts`：关注通知文案、actor 资料链接、无帖子链接语义，兼容旧 comment/like 通知响应。
- `user.css`：用户端局部样式处理关注按钮、关系页和长作者名称换行，管理员样式不变。
- 验收测试：`user/follow_integration_test.go`、`worker/integration_test.go`、bus/topology/HTTP 既有合同测试；`FollowButton.test.ts`、`FollowingView.test.ts`、`e2e/follow.spec.ts`，并更新 Phase-13-01 浏览器断言以接受 following 字段、已启用 tab 和新增按钮后的链接定位。
- `VERSION`、Frontend package/lock 根版本和 `.env.example` 产品版本/image tag 更新为 `1.10.2`；split plan 状态同步为本地完成。

## 环境、失败与修复

- 使用 WSL2/Linux Bash。新建独立 Compose 项目 `gopulse-p1302`，不修改或清理原 `gopulse-p13-local` 项目。测试凭据和 override 只放 `/tmp/gopulse-p1302*.env/yaml`。
- 集成白名单：`INTEGRATION_TESTS=1`、`APP_ENV=test`，MySQL `127.0.0.1:43308/gopulse_integration`、同名账户，Redis `127.0.0.1:46381` DB15；RabbitMQ `127.0.0.1:45674`，Elasticsearch `127.0.0.1:49202`。Frontend `127.0.0.1:45175`，Backend `127.0.0.1:48082`。
- 首次迁移因测试 override 未解除实际 business internal 网络而无法连接；仅修正临时 override 的 business/observability 网络，重建专用网络后 migrate up 成功，项目 Compose 文件不变。
- 初版 user 引用 post 造成 user→post→middleware→user import cycle；改用 user 局部关系 options/cursor，HTTP 层转换，未读取第三方源码。
- 最小 Go 检查发现新增可空 post ID、actor/following 字段和路由绑定数使旧断言失效，按修改后的公共合同更新。集成还捕获列表多一次查询，改为原列表/搜索 SQL 的 EXISTS 水合，保留原单查询验收，不放宽性能断言。
- 因本批修改共享消息路由、通知 schema 和用户端 CSS，扩大验证到实际 Compose publisher/worker/Frontend、旧业务通知及管理员代表页面；不运行无关审计或完整 Phase 12 故障矩阵。
- 首轮浏览器通知未到达：集成 Worker 在同一专用 RabbitMQ 留下 retry TTL=1s，而 Compose 合同为30s。Broker 日志确认 PRECONDITION_FAILED；确认专用 retry 队列消息为0后删除该空队列，重启本批 Backend/Worker，由应用按30s声明，outbox 积压正常消费。没有修改产品重试参数、删业务事件或人工修复通知事实。
- 资料浏览器回归发现手机上既有长用户名链接溢出；定位到作者链接宽度，用户壳层内增加换行后重建 Frontend，Desktop/Tablet/Mobile 无横向溢出，管理员回归通过。

## 实际验证结果

成功检查未因用户多次“继续”而重复执行；仅因相关实现、测试或运行环境改变重跑受影响项。日志与截图保存在 `/tmp/gopulse-p1302-evidence/`，不提交运行数据或凭据。

| 实际命令/路径 | 结果 |
| --- | --- |
| `cd backend && go run ./cmd/migrate up`（source 独立 test env） | PASS：新库迁移完成；初次网络失败及修复见上 |
| `go test ./internal/user ./internal/post ./internal/notification ./internal/outbox ./internal/worker ./internal/http ./internal/bus ./internal/platform ./internal/search` | PASS，`/tmp/p1302-go-final.log` |
| `go test -tags=integration ./internal/user ./internal/post ./internal/notification ./internal/worker` | 首次 user/notification/worker PASS；post 单查询断言失败。修复后 `go test -tags=integration ./internal/post ./internal/user` PASS，未重跑未受影响的 notification/worker；见 integration/integration2 日志 |
| 新增关系集成测试 | PASS：4并发请求、唯一关系/事件、self/unknown/DB约束、重复通知、私有关系及时间相同分页、Following 分页、取消无事件、用户删除级联 |
| 新增 Worker 关注 retry/replay 集成 | PASS：一次临时失败、重放、恢复与唯一无资源通知；旧 Worker retry/dead/shutdown 测试同样通过 |
| `cd frontend && npm test` | PASS：14 files / 61 tests（含共享按钮回滚、本人隐藏、Following 失败恢复/分页/空态） |
| `npm run typecheck`、`npm run build` | PASS；最后 CSS 修改后 build 成功，内部再次执行规定 typecheck |
| `GOPULSE_BASE_URL=http://127.0.0.1:45175 npm run test:e2e -- --grep 'follow\|Following\|notification'` | 最终 PASS：2 tests / 5.0s，关注全链路及旧评论/点赞通知；无 skip |
| 同环境 `--grep 'profile\|user search\|user shell\|completes the browser registration\|runs Compose business scenario: business'` | 注册/登录/发布/评论/点赞与 Compose business、管理员共3项 PASS；profile 手机溢出失败后修复 |
| 修复 CSS 后 `--grep 'profile\|user search\|user shell'`，提供独立管理员凭据 | PASS：2 tests / 3.9s；用户搜索、资料、缓存作者名称、响应式和管理员 Metrics，无 skip |
| Docker Compose `build backend business-worker search-indexer frontend` 与 `up -d --wait backend frontend business-worker search-indexer monitor`（独立 env/override） | PASS；生产实现改变后仅重建受影响镜像；使用本批 p1302-local tag，其余未改可观测镜像复用1.9.4 |
| `python3 scripts/ci/validate_versions.py` | PASS |
| `python3 scripts/ci/validate_branch.py --branch develop/1.10.2 --base-ref origin/main` | PASS |
| `git diff --check` | PASS；提交后另查 `origin/main...HEAD` |

### 非阻断既有类型诊断

额外执行 `npx vue-tsc --noEmit -p tsconfig.app.json`，仍报 Phase-13-01 已记录的6条诊断：`services/exporters.ts` 的 at 类型、`services/http.test.ts` 四处类型参数、`views/NotificationsView.test.ts` 的 exists 类型。新增文件没有诊断；未修改无关文件。规定的 solution-level typecheck/build 通过，不宣称显式 app 检查通过，也未为此重新开展基线审计。

## Phase-13-03 交接与限制

- 资料/用户搜索 `following`、帖子 `author.following` 是会话 viewer 状态；收藏扩展不可把这些字段当作共享缓存的权威值。
- 关系/Following 的 cursor 沿用 UTC created_at + 正整数 ID 的规范 Base64 keyset 形状；HTTP 拒绝未知参数、非法 cursor/limit，owner/作者集合不能由客户端指定。用户搜索仍保留原 HMAC/query-bound cursor，不混用。
- 关注通知 `type=user.followed`、`post_id=null`、`comment_id=null`，actor 含 id/username/display_name；旧通知保留正整数 post ID。未来删除墓碑必须继续支持无资源通知，不生成 `/posts/null`。
- UI 共享状态按当前浏览器会话维护，不提供跨设备实时关系推送；下一次 API 读取提供数据库事实。推荐、公开关系数量/列表、收藏、编辑删除仍属后续。
- 关系写入按 follower 用户行串行化，唯一关系与同事务 outbox 为最终约束；本次未引入另一个锁服务或异步关系投影。
- 不声称 Phase 13 整体完成，不变更总方案的阶段完成状态；本批停止于验收和提交。

## 环境清理与提交收口

- 专用 Compose 项目执行 `down --volumes --remove-orphans` 成功；按 `com.docker.compose.project=gopulse-p1302` 查询 containers、volumes、networks 均为空。原 `gopulse-p13-local` 的13个运行容器保留，不删除其网络/数据。
- 最后补充开发记录后 `git diff --check` 通过。所有暂存文件均为本批从干净基线产生的变更，使用英文 Conventional Commit；不自动推送、不打开 PR。
