# Phase-13-03：收藏与私有内容列表闭环开发记录

## 状态与基线

- 完成日期：2026-09-07；本批目标及完成版本：`1.10.3`；分支：`develop/1.10.3`。
- 开工执行 `git fetch origin`，确认 Phase-13-02 已通过 PR #115 合入 `origin/main`（`5a6a3b7`），从该提交创建本批分支。工作区开工时干净，根与 Frontend 基线版本均为 `1.10.2`。
- 前批证据保留在 `dev/logs/Phase-13/Phase-13-02-关注关系与Following信息流闭环.md` 对应记录及 `/tmp/gopulse-p1302-evidence/`；本批重新验证直接受影响的 Following、通知及业务浏览器路径，不重复前批全量验收。
- 本批证据目录：`/tmp/gopulse-p1303-evidence/`。临时环境文件和管理员凭据仅保存在 `/tmp`，不提交。

## 实际交付

### 数据与写入

- 新增 `000008_post_bookmarks` up/down migration，未覆盖已有 `000007_user_follows`。
- 表结构：`post_id`、`user_id`、`created_at DATETIME(6)`；主键 `(post_id, user_id)`；列表索引 `(user_id, created_at DESC, post_id DESC)`；帖子和用户外键均 `ON DELETE CASCADE`。
- 新建 `backend/internal/bookmark/{repository,service,handler}.go`，使用会话用户执行 PUT/DELETE；重复 INSERT 的唯一键冲突收敛为成功，重复 DELETE 保持成功，不刷新原收藏时间。
- 事务使用 `READ COMMITTED`，锁定目标帖子后执行写入，避免与永久删除的存在性检查发生竞态；未知帖子映射为既有 `post_not_found`，不返回其他用户关系信息。
- 收藏不写 outbox、不发通知、不增公共计数，也不进行 Redis 写入/失效，因此数据库成功不依赖 Redis。

### API、分页和缓存边界

- protected routes：`PUT /api/v1/posts/:postId/bookmark`、`DELETE /api/v1/posts/:postId/bookmark`、`GET /api/v1/bookmarks`；写入成功返回 204。
- 收藏列表的主体只取 session user id；除 `limit`、`cursor` 外的参数均拒绝，不能通过 user_id/username 查看他人收藏。
- 收藏时间与帖子 ID 组成稳定倒序 keyset；limit 沿用 1–50，内部游标最大 512 字节，外部签名 token 最大 600 字节。
- 收藏游标使用 HMAC-SHA256，绑定 viewer；生产 key 从认证 secret 做用途隔离派生。篡改、其他 viewer 的 token、非法参数均拒绝。密钥轮换后旧 token 失效，重新打开列表即可。
- 列表直接 INNER JOIN 数据库现存帖子，不会用陈旧缓存复活已删除内容；输出帖子原发布时间不替换为收藏时间，内部 `BookmarkCreatedAt` 不序列化。
- `Post.bookmarked_by_me` 为明确 boolean，创建和匿名 viewer 为 false；列表、作者帖子、Following、搜索 `FindMany` 在一次集合 SQL 中使用 viewer-specific EXISTS，不产生逐条查询。详情在共享 projection 之后通过批量 `HydrateBookmarks` 查询事实；空 viewer 不查收藏关系。
- `PublicProjection.Author` 改用不含 following 的 `PublicAuthor`，从结构上移除共享缓存中的关系状态。缓存不含 `bookmarked_by_me`、`liked_by_me`、following 或权限字段。旧 v1 JSON 如果含 following，严格解码会拒绝并按既有可观察失败路径回源填充，不把旧值当作 viewer 权威。
- 搜索仍只索引公共内容，本批无需改变 Elasticsearch schema；MySQL hydration 提供收藏状态。

### Frontend

- 新增共享 `BookmarkButton.vue`，用于现有 `PostCard` 动作行和详情动作行，首页、Following、搜索、资料帖子及收藏列表统一复用；稳定 accessible label 为“收藏帖子”，`aria-pressed` 表达当前状态。
- `useBookmarks.ts` 按会话 viewer/post 维护共享乐观状态，禁止重复在途写入，失败恢复前值并显示错误；退出清理状态，旧会话完成的请求不能覆盖新会话。
- API 读取使用读取时点标记协调已存在的状态覆盖：后续事实读取可更新按钮，早于本地变更的旧读取不覆盖新状态。
- 新增 `/bookmarks` 与 `BookmarksView.vue`，壳层原收藏占位入口转为真实私有列表；包含加载、空、错误重试、继续加载、结束和已删除自动跳过说明。
- 取消收藏后当前列表保留该行并显示未收藏，下次打开/刷新列表移除；页面明确说明此行为。未为他人资料增加收藏入口，不显示收藏数。
- 类型、严格搜索 DTO 校验和相关已有测试 fixture 同步新增 boolean 字段。

## 主要文件

- Schema：`backend/migrations/000008_post_bookmarks.{up,down}.sql`。
- 后端：新增 `backend/internal/bookmark/*`；修改 `backend/internal/post/{model,repository,service,handler,cache}.go`，新增 `bookmark_cursor.go`；路由装配为 `backend/internal/http/api.go` 与 `backend/cmd/server/main.go`。
- 后端验证：新增 `backend/internal/post/bookmark_integration_test.go`、`backend/internal/http/router_bookmark_test.go`；同步 HTTP JSON 契约和 public author/cache 测试。
- 前端：`frontend/src/components/{BookmarkButton,PostCard,UserAppShell}.vue`、`composables/{useBookmarks,useAuth}.ts`、`views/{BookmarksView,PostDetailView}.vue`、`services/api.ts`、`types/api.ts`、`router/index.ts`；新增按钮/列表单测及 `frontend/e2e/bookmark.spec.ts`，更新受影响的既有 fixtures。
- 收尾：根 `VERSION`、`.env.example`、Frontend package/lockfile、当前实施方案状态及本记录。

## 验证范围与实际结果

使用独立 Compose 项目 `gopulse-p1303`，保留原 `gopulse-p13-local`；依赖通过 loopback-only debug override 提供主机测试访问。测试配置明确为 `INTEGRATION_TESTS=1`、`APP_ENV=test`、MySQL `127.0.0.1:43308/gopulse_integration`、用户 `gopulse_integration`，Redis `127.0.0.1:46381` DB 15。浏览器访问 `http://127.0.0.1:45175`。

**范围扩展理由：** 本批移除共享 projection 的 following 字段并扩展跨页面 Post DTO，实际影响资料缓存回源与搜索/Following 页面。因此除本批 grep 外，运行既有资料发现及管理员壳层代表测试、业务登录/帖子/评论/点赞与关注通知测试；未运行 Compose 全量 E2E、依赖审计或覆盖率扩展。

| 实际命令/验证 | 结果及证据 |
| --- | --- |
| `docker compose --env-file /tmp/gopulse-p1303.env -p gopulse-p1303 -f deploy/compose.yaml -f deploy/compose.debug.yaml -f /tmp/gopulse-p1303.yaml up -d --wait mysql redis rabbitmq elasticsearch kafka victoriametrics` | PASS，`deps.log` |
| `set -a; source /tmp/gopulse-p1303-test.env; set +a; cd backend; go run ./cmd/migrate up` | PASS，`migrate2.log`；包括新收藏表 |
| `cd backend; go test ./internal/post ./internal/http ./internal/platform/redis ./internal/search ./internal/bookmark` | PASS，`go-final.log`；bookmark 包无独立单测，其真实写入由 post 外部集成测试覆盖 |
| source 白名单 test env 后 `go test -tags=integration ./internal/post ./internal/http` | PASS，`integration-final.log`；早期版本也通过，SQL 组装与匿名 viewer 分支改变后只重跑这组受影响 gates |
| 新收藏集成场景 | PASS：4 并发 PUT、唯一关系/重复不改时间、同时间二页稳定顺序、他人空列表、首页/作者/Following/search hydration、双 viewer 与匿名详情、cache hit 状态隔离、重复取消/未知帖子、帖子级联删除与孤儿用户约束、无 outbox 事件 |
| HTTP 私有收藏契约 | PASS：认证边界、PUT/DELETE 204/404、禁止主体参数、签名游标续页/篡改/跨 viewer 拒绝、空列表形状；更新旧完整 JSON 断言 |
| `cd frontend; npm test` | PASS，16 files / 63 tests，`frontend-final.log`；新增共享状态、失败回滚、退出清理、读取协调、列表重试/分页/空态 |
| `npm run typecheck` | PASS，`typecheck-final.log`，使用计划指定命令；不额外宣称显式 app tsconfig 检查通过 |
| `npm run build` | PASS，`frontend-build.log`；接受完成后同步 package 版本，重建为 1.10.3，见 `frontend-build-versioned.log` |
| Compose `build backend frontend business-worker search-indexer` 与 `up -d --wait backend frontend business-worker search-indexer monitor`（上述独立 env/override） | PASS，`build-images.log`、`up2.log`；仅复用未修改可观测服务镜像。前端读取协调修改后仅重建/启动 frontend，`build-frontend2.log`、`up-frontend2.log` |
| `GOPULSE_BASE_URL=http://127.0.0.1:45175 npm run test:e2e -- --grep 'bookmark\|收藏'` | PASS，1 test / 3.3s，`e2e-bookmark.log`，真实两用户、六种页面动作、搜索索引、错误回滚、缓存温热读取、取消与退出边界；无 skip |
| 同地址 `npm run test:e2e -- --grep 'completes the browser registration\|notifications close\|follow, Following\|profile and user search\|user shell leaves'`（提供独立管理员环境变量） | PASS，5 tests / 9.9s，`e2e-regression.log`；登录/帖子/评论/点赞、关注/Following/通知、资料搜索响应式、管理员 Metrics 页面；无 skip |
| Docker exec Redis CLI scan/get 并逐条 JSON 断言 | PASS，7 条真实缓存不含 bookmarked_by_me/liked_by_me/following；Redis stats 记录 hits=19、misses=25，`redis-projection.log` |
| `python3 scripts/ci/validate_versions.py` | PASS，根及 package/lockfile/环境元数据为 1.10.3 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.10.3 --base-ref origin/main` | PASS |
| `git diff --check`、`git diff --cached --check`、提交后 `git diff --check origin/main...HEAD` | PASS |

### 实际修复与偏差

- 初始复制写入骨架残留 clock 引用导致编译失败，已移除与本批无关的事件/cache 代码后通过；一次命令工作目录错误使若干编辑脚本未执行，重新从仓库根执行，不把失败命令计作成功验证。
- 新 boolean 字段使 HTTP 完整 JSON 契约旧断言失败，按新 DTO 合同更新后通过。
- 详情新增按钮最初位于点赞按钮之前，既有测试的首按钮选择器触发了收藏。保留原点赞位置，将收藏追加在后，原业务测试及浏览器点赞回归通过。
- 独立 env 中原已存在的 integration 用户密码与临时新凭据不一致，首次主机 migration 与 Compose migrate 失败；仅统一本批独立数据库账户和两份 `/tmp` env 后通过。未修改原运行环境和项目数据库配置。
- 列表/搜索复用现有单条集合 SQL 的 correlated EXISTS 来批量组装 viewer 状态，详情使用显式 IN 查询；没有为每条帖子增加 relation SQL。收藏列表直接查事实库而非增加私有 Redis projection。
- 现有组件实际命名为 `PostCard`，不是方案描述的 PostRow；通过共享 `BookmarkButton` 达成统一动作合同，不为命名差异重构组件体系。
- 没有阅读第三方依赖源码，没有增加范围外功能，也没有开展独立审查门禁。

## Phase-13-04 交接和限制

- `bookmarked_by_me` 只属于当前 viewer，必须保留详情、列表、作者帖子、Following 和搜索的相同语义。编辑、索引更新和缓存失效不可把该值写入公共 projection。
- 收藏排序依据收藏关系的 `created_at` 与帖子 ID，帖子编辑时间不得改变收藏位置；重复收藏也不改变时间。
- 后续永久删除仍须实现帖子原有 comments/likes 等依赖的业务清理；本批新增 bookmark FK 已实测自动级联。未实现也未宣称交付作者编辑/删除接口。
- 跨设备不做实时状态推送；后续 API 事实读取会校正当前会话覆盖。收藏列表取消后的行保留行为已在页面说明。
- 用户注销/删除的全产品流程不属本批；收藏用户 FK 防止孤儿并支持级联。
- 不新增公共收藏数、收藏通知或他人收藏入口；不宣称 Phase 13 整体完成。

## 环境与提交收口

- 验收后对 `gopulse-p1303` 执行 `down --volumes --remove-orphans`，检查 dedicated containers/volumes/networks 均为空，见 `down.log`、`cleanup.log`；保留原 `gopulse-p13-local` 的 13 个运行容器。
- 成功检查不因上下文恢复重复执行；后续重跑仅针对 SQL、前端状态逻辑或版本元数据改变的受影响输入。
- 本批按英文 Conventional Commit 提交，不自动推送或打开 PR。
