# Phase-13-04：帖子编辑与缓存搜索一致性闭环开发记录

## 状态与环境

- 完成日期：2026-09-08；目标版本 `1.10.4`，分支 `develop/1.10.4`。
- 开始时工作区干净，根与 Frontend 版本均为 `1.10.3`；执行 `git fetch origin` 后从 `origin/main` 创建本批分支，没有继续使用上一批 `develop/1.10.3`。
- 在 WSL2/Linux、Bash 下实现和验收。未委派子代理，未阅读第三方依赖源码。
- 使用独立 Compose 项目 `gopulse-p1304` 提供 MySQL、Redis、RabbitMQ、Elasticsearch，宿主机运行本批编译的 Backend、Business Worker、Search Indexer 和 Vite。没有修改原 `gopulse-p13-local` 的容器、配置或产品数据。
- 白名单测试库/账户为 `gopulse_integration`，Redis DB 为 `15`；依赖地址均为 loopback，端口分别为 MySQL `43316`、Redis `46389`、RabbitMQ `45682`、Elasticsearch `49210`；HTTP `48084`、Frontend `45184`。
- 本次临时配置、命令输出和故障验证脚本保存在 `/tmp/gopulse-p1304/`，不提交包含凭据、Cookie 或临时运行数据的文件。该目录不是长期证据存储，以下结果和仓库测试是开发记录的持久内容。

## 实际交付

### 写事务与公共 API

- migration `000009` 增加 nullable `edited_at` 和默认值为 1 的 `content_revision`，扩展 outbox 类型约束。
- 新增作者专属 `PATCH /api/v1/posts/:postId`。请求复用创建帖子的 trim、Unicode 字符数、非空校验；严格 JSON 解码拒绝 owner、时间和 revision 等客户端字段。
- 事务使用 `SELECT ... FOR UPDATE` 读取当前作者和正文，非作者返回 `403 permission_denied`，不存在返回 `404 post_not_found`。
- 真实内容变化时更新正文、标题、编辑时间、更新时间、revision，并在同一事务插入 `post.updated`；outbox 插入失败时全部回滚。
- trim 后内容相同则不增加 revision、不改变 edited_at、不产生重复事件；不实现客户端乐观锁，多个有效作者请求按数据库行锁顺序生效。
- `post.updated` 只携带 post id、revision、actor、事件 ID/时间等元数据，没有标题和正文，也不进入普通通知队列。

### 缓存与读取

- Post 和公共 projection 增加 `edited_at`、`content_revision`，列表、详情、作者帖子、Following、收藏和搜索 MySQL 水合使用相同字段。
- 详情接受 Redis 命中前用主键查询 MySQL 的当前 revision；缺失、查询失败或 revision 不符均拒绝该缓存并回源。没有可验证 revision 的旧缓存不能作为新事实返回。
- 更新后的缓存失效是 best effort；即使旧值仍在、Invalidate 和 Set 都失败，详情也不能返回旧标题或正文。该情形由真实 MySQL 加故障缓存的集成测试验证。
- following、like、bookmark 和作者资料继续在共享 projection 外水合，不缓存历史正文或 viewer 权限。

### 搜索版本、重放与重建

- Search Indexer 同时消费 `post.created`/`post.updated`，每次重读当前 MySQL，而非信任事件中的历史内容。事实缺失时删除搜索文档；网络等临时失败仍走现有重试路径。
- 增量和 bulk 都使用当前 `content_revision` 作为 Elasticsearch `external_gte` 版本；旧版本冲突作为已过期重放处理，重复同版本可幂等执行。
- 新 mapping 包含 edited_at/revision。`--if-missing` 遇到没有 revision 字段的旧 alias 时自动重建，而非只添加字段：旧索引使用内部版本，重复投递可能使其版本高于第一条编辑的 revision，直接原位切换版本语义会永久拒绝有效更新。
- 重建切换 alias 后再次扫描 `0..H2` 的所有事实，而非只补新增 ID，补偿第一次扫描后、alias 切换前已被旧索引消费的编辑。版本保护防止补偿扫描覆盖更晚的消息。
- 新集成测试实际制造旧索引内部版本、切换前编辑、低版本单条/bulk 写入和旧 create/update 重放，验证最终只有新关键词命中。

### Frontend

- 在既有 `PostCard` 和详情元信息区域复用 `PostEditMenu`：仅作者显示原生键盘可操作的“更多/编辑”，显示“已编辑”和时间；不添加删除、撤销、历史版本假入口。
- 复用 `NewPostView` 的完整验证和表单实现，新增 `/posts/:postId/edit`，先读当前帖子、预填内容，提供加载失败重试、字符计数、保存中禁用和 `aria-busy`。
- 保存失败保留输入和错误提示；成功跳转详情并重新读取事实；之后访问列表、Following、作者、收藏和搜索均读取最新内容。
- 严格搜索响应验证同步增加两个字段；更新受影响测试 fixture，没有放宽公共 DTO 校验。

## 验证命令与结果

命令使用上述独立环境（`source /tmp/gopulse-p1304/env`），未对产品环境运行集成测试。

| 检查 | 实际结果 |
| --- | --- |
| `cd backend; go test ./internal/post ./internal/search ./internal/bus ./internal/platform` | PASS，`affected2.log`；覆盖直接包及新增消息类型/搜索拓扑的共享协议风险 |
| `go run ./cmd/migrate up` | PASS，`migrate.log`；清理独立测试数据库后再次迁移成功，`reset.log` |
| `go test -tags=integration ./internal/post -run TestIntegrationEdit` | PASS，`edit-integration2.log`；作者写入、revision、no-op、403/404、outbox 失败回滚、失效/填充失败仍拒绝旧缓存 |
| `go test ./internal/post ./internal/search ./internal/outbox ./internal/worker ./internal/http ./internal/platform/redis` | PASS，`go-unit.log`，固定 Go 单元门禁 |
| `go test -tags=integration -p 1 ./internal/post ./internal/search ./internal/worker ./internal/http` | post/http PASS，`go-integration2.log`；RabbitMQ 尚未就绪导致 worker 失败，见下方修正 |
| `rabbitmq-diagnostics -q check_running` 后 `go test -tags=integration -p 1 ./internal/search ./internal/worker` | PASS，`rabbit-ready.log`、`go-integration3.log`；Worker 重试/死信/重投与通知回归通过 |
| 搜索升级策略最后修改后 `go test ./internal/search` 和 `go test -tags=integration ./internal/search` | PASS，`search-unit-final.log`、`search-integration-final.log`；包含真实 ES 旧内部版本升级、重建补偿、低版本写入和乱序重放 |
| `go test ./migrations ./cmd/search-reindex` | PASS，`migration-reindex-tests.log`；扩展原因是新增 migration 与调整初始化/重建行为 |
| `cd frontend; npx vitest run src/views/EditPostView.test.ts src/views/SearchView.test.ts` | PASS，2 files / 5 tests，`frontend-affected.log` |
| `npm test` | PASS，17 files / 64 tests，`frontend-final.log` |
| `npm run typecheck` | PASS，`typecheck.log`，使用计划指定命令 |
| `npm run build` | PASS，`build.log`；同步版本元数据后再次构建为 1.10.4，`build-versioned.log` |
| `GOPULSE_BASE_URL=http://127.0.0.1:45184 npm run test:e2e -- --grep 'edit\|搜索\|cache'` | PASS，1 test / 4.1s，`e2e-edit2.log`；真实两用户权限、错误保留与重试、旧缓存温热读取、no-op、两种关键词、六类读取页面，无 skip |
| 同地址 `npm run test:e2e -- --grep 'completes the browser registration\|notifications close\|follow, Following\|bookmark 收藏\|user shell leaves'`，提供独立管理员账户环境变量 | PASS，5 tests / 9.8s，`e2e-regression.log`；创建/详情/评论/点赞、通知、Following、收藏和管理员 Metrics 壳层，无 skip |
| 故障闭环：`outage.py prepare` → 停 Indexer 及独立 Redis/ES → `outage.py edit` → 恢复 Redis/ES 与 Indexer → `outage.py verify` | PASS，`outage.log`、`outage-recovery.log`、`indexer-restart.log`；停机期间 PATCH/详情成功，恢复后排队 create/update 自动消费、新关键词命中且旧关键词消失；未手工清 cache/index |
| `python3 scripts/ci/validate_versions.py` | PASS，根 VERSION、Frontend package/lock 与 `.env.example` 为 `1.10.4` |
| `python3 scripts/ci/validate_branch.py --branch develop/1.10.4 --base-ref origin/main` | PASS |
| `git diff --check` | PASS；提交前另执行 staged diff 检查，提交后执行方案规定的 `origin/main...HEAD` 检查 |

## 过程中修正的失败与范围偏差

- 首轮旧缓存单元测试 fixture 没有 revision 查询能力，新的 fail-closed 缓存策略使其失败；为 fake repository 增加相应能力，保留原 viewer 隔离和交互计数测试语义。搜索缺失事实从永久失败变为 delete，更新对应测试；拓扑预期同步加入 update 三条绑定。
- 严格搜索 DTO 使旧 Frontend fixture 缺少 edited_at/revision 而失败；更新受影响 fixture 后全套通过。HTTP 完整 JSON 断言同步更新。
- 新事务测试第一次误用不存在的 outbox `post_id` 列；改为查询实际 JSON payload 后通过。
- 第一次固定集成门禁与浏览器/常驻 Worker 共用本批临时库：浏览器帖子影响固定时间分页断言，Worker 抢消息导致测试等待和数据库锁超时。停止本批进程、重建本批白名单数据库/消息容器，改为 `-p 1` 顺序测试；未修改生产逻辑来适配环境干扰。
- RabbitMQ 新容器首次尚未启动完毕，worker 连接失败；显式通过 readiness 后只重跑未通过的 worker 和已修改的 search，不重跑已通过 post/http。
- 浏览器脚本最初误把 follow 参数当 username 且期望 204；按现有 numeric ID/200 合同修正后通过，未变更关注 API。
- 故障恢复首次在 ES 索引尚未就绪时查询得到 503；恢复轮询接受这一临时状态后通过，未改生产搜索错误语义。
- 组件实际名为 `PostCard`，不是方案示意的 PostRow。使用宿主进程 + 独立容器依赖验证，不宣称执行完整 Compose 产品 build/up/verify/down 门禁；本批没有修改 Compose 或生命周期脚本。
- 管理员回归验证页面、导航和 CSS 壳层，不宣称本批测试环境具备完整可观测后端；没有启动独立 Monitor，日志远程投递的不可用/关闭超时警告不影响业务验收。
- 成功检查仅因直接相关源文件、fixture、版本或测试环境变化而重跑，没有开展全库审查、依赖审计或覆盖率扩张。

## Phase-13-05 交接与限制

- `post.updated` 与后续 delete 必须共存，保留当前事务/outbox/revision 和缓存 existence/revision 核对合同。
- 本批缺失事实会删除 ES 文档，但尚未提供持久删除墓碑或跨删除竞态的永久版本围栏；不能据此宣称已经完成删除防复活。Phase-13-05 必须处理并发旧快照、延迟重试和删除版本合同。
- 不支持编辑历史、撤销、管理员代编辑或客户端 expected-revision 冲突协议。不同会话的已有画面不实时推送，后续 API 读取返回当前事实。
- 列表/收藏排序仍使用原 created_at/关系时间，编辑不改变排序事实。
- 旧 mapping 首次升级自动重建；后续新 mapping 初始化不重复重建。永久删除引入后需重新检查重建 count/补偿与墓碑的关系。
- 无本批阻断问题，不标记 Phase 13 整体完成。

## 环境与提交收口

- 停止本批宿主 Backend、Worker、Indexer 和 Vite；对 `gopulse-p1304` 执行 `down --volumes --remove-orphans`，检查本批容器和网络清空，见 `cleanup.log`。
- 根及受管版本同步为 `1.10.4`，实施方案状态标记本批完成；只提交本批变更，英文 Conventional Commit，不自动推送或创建 PR。

## 变更文件

- `.env.example`
- `VERSION`
- `backend/internal/bus/envelope.go`
- `backend/internal/http/api.go`
- `backend/internal/http/router_post_test.go`
- `backend/internal/platform/rabbitmq_publisher.go`
- `backend/internal/platform/rabbitmq_topology.go`
- `backend/internal/platform/rabbitmq_topology_test.go`
- `backend/internal/post/cache.go`
- `backend/internal/post/edit.go`
- `backend/internal/post/edit_integration_test.go`
- `backend/internal/post/handler.go`
- `backend/internal/post/model.go`
- `backend/internal/post/repository.go`
- `backend/internal/post/service.go`
- `backend/internal/post/service_test.go`
- `backend/internal/search/contract.go`
- `backend/internal/search/edit_integration_test.go`
- `backend/internal/search/elasticsearch.go`
- `backend/internal/search/processor.go`
- `backend/internal/search/processor_test.go`
- `backend/internal/search/reindex.go`
- `backend/migrations/000009_post_edits.down.sql`
- `backend/migrations/000009_post_edits.up.sql`
- `dev/imple/Phase-13/Phase-13-04-帖子编辑与缓存搜索一致性闭环.md`
- `dev/logs/Phase-13/Phase-13-04-帖子编辑与缓存搜索一致性闭环.md`
- `frontend/e2e/edit.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `frontend/src/components/BookmarkButton.test.ts`
- `frontend/src/components/PostCard.vue`
- `frontend/src/components/PostEditMenu.vue`
- `frontend/src/router/index.ts`
- `frontend/src/services/api.ts`
- `frontend/src/types/api.ts`
- `frontend/src/views/BookmarksView.test.ts`
- `frontend/src/views/BusinessViews.test.ts`
- `frontend/src/views/EditPostView.test.ts`
- `frontend/src/views/FollowingView.test.ts`
- `frontend/src/views/NewPostView.vue`
- `frontend/src/views/PostDetailView.vue`
- `frontend/src/views/SearchView.test.ts`
