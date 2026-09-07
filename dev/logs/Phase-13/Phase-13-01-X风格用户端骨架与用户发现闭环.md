# Phase-13-01 开发记录

## 执行与范围

- 2026-09-07，从 fetch 后的 `origin/main`（`300140d`）创建 `develop/1.10.1`，基线版本 `1.9.4`，目标版本 `1.10.1`。
- 本批实现用户资料、用户发现、作者显示名称和独立用户端壳层；不实现关注、收藏事实、帖子编辑或删除。
- 验证只覆盖方案门禁及直接修改的 HTTP/缓存边界；未开展通用审计。

## 实现选择与文件

- `backend/migrations/000006_user_profiles.{up,down}.sql`：新增非空 display_name/bio，迁移以 username 回填，bio 统一空字符串；注册显式写入 username 作为 display_name。migration 集成测试在独立临时表执行 up/down/up。
- `backend/internal/user/{model,repository,profile}.go`：公开 Profile DTO 不含 role/password；Unicode trim 校验，两个更新字段均必填；仅会话身份更新。MySQL 搜索限制 query 1–200 字符、limit 1–50，空 query 返回 validation_failed；用 `!` 转义 LIKE。排序为精确 username、username/display_name 前缀、包含，再按 id 升序；HMAC 游标绑定 query/rank/id。
- `backend/internal/http/{api,user_handler}.go`、`backend/cmd/server/main.go` 和 apperror/response：接入四条认证路由与 user_not_found 404。Handler 放在 HTTP package，避免 user → middleware → user 循环依赖。
- `backend/internal/post/{model,repository,service}.go`：列表/搜索在原单条 JOIN 查询水合 display_name；详情在缓存读取后通过一次批量用户查询刷新摘要，旧缓存结构无需失效迁移，Redis 非权威字段不会作为最终名称使用。用户帖子沿用时间/ID keyset 并加 author_id 过滤。
- `frontend/src/components/{UserAppShell,PostCard}.vue`、router、`user.css`：独立路由壳层及局部 tokens，三栏/图标栏/底部导航，连续时间线，字母头像，发布入口、账户区管理入口；Following/收藏为明确禁用占位。
- `frontend/src/views/{PostsView,SearchView,ProfileView,PostDetailView,NewPostView,NotificationsView}.vue`、services/api、types/api：帖子/用户搜索 tabs、资料/本人编辑、初始值/计数/失败重试/成功状态、用户帖子分页。搜索响应严格校验器同步接受 author.display_name。
- 直接验收测试：user profile 单元与集成、migration round trip、HTTP author DTO 断言、ProfileView Vitest、SearchView fixture、`frontend/e2e/profile.spec.ts`。

## 环境与过程中的失败修复

- 本机初始无运行容器。使用随机隔离之外的专用项目名 `gopulse-p13-local`（本次独占创建）和白名单库/账户 `gopulse_integration`、Redis DB 15，全部发布在 loopback 非默认端口。测试凭据与覆盖配置只保存在 `/tmp/gopulse-p13*.env/yaml`，不进入仓库。
- 先用既有 `1.9.4` 镜像保存 Desktop/Mobile 首页、搜索、通知和管理员 Metrics 基线截图；证据位于 `/tmp/gopulse-p13-evidence/before-*.png`。
- Docker internal 网络不会向宿主发布依赖端口，首次宿主 migration 连接失败；仅在临时测试 override 中改为非 internal 后成功，产品 Compose 未改变。
- 首轮编译发现上述 package 循环，迁移 handler 至 HTTP package 后解决；原严格 HTTP author JSON 断言同步扩展。
- 浏览器发现帖子搜索严格解析器未接受新增 display_name，已修正并更新对应 fixture。搜索异步投影等待改为轮询真实 API，避免在页面 reload 后立即读取未完成渲染的 count。
- 额外显式运行 `npx vue-tsc --noEmit -p tsconfig.app.json`，发现 6 条既有类型错误（exporters.ts 1 条、http.test.ts 4 条、NotificationsView.test.ts 1 条）。使用 `git archive origin/main` 的临时基线运行同一命令，诊断逐行一致；不修改无关文件。既定 `npm run typecheck` 使用 solution 配置，不能替代此显式检查。作为基线非阻断跟进记录，不能声称显式全应用类型检查通过。

## 验证结果

下列命令均实际执行并通过（输出保存在 `/tmp/gopulse-p13-evidence/`）：

```text
cd backend
go test ./internal/user ./internal/post ./internal/search ./internal/http
# PASS（最终受影响包；已有成功结果由 Go test cache 复用）
# source /tmp/gopulse-p13-test.env；APP_ENV=test、INTEGRATION_TESTS=1
# MySQL 127.0.0.1:43307 / gopulse_integration，Redis 127.0.0.1:46380 / DB15
# Elasticsearch 127.0.0.1:49201
go test -tags=integration ./internal/user ./internal/post ./internal/search ./migrations
# PASS，含资料搜索分页/转义、真实 Redis 命中改名、作者帖子分页、搜索水合与迁移 up/down/up
cd ../frontend
npm test
# PASS：12 files / 59 tests
npm run typecheck
# PASS（既有 solution 命令；显式 app 配置限制见上文）
npm run build
# PASS
GOPULSE_BASE_URL=http://127.0.0.1:45174 GOPULSE_PROFILE_ADMIN_USER=p13_baseline \
  GOPULSE_PROFILE_ADMIN_PASSWORD=<isolated-test-password> \
  npm run test:e2e -- --grep "profile|user search|user shell"
# PASS：2 tests / 3.9s，均实际执行，无 skip
```

浏览器覆盖：两用户注册、发布/详情、资料编辑校验与保存、刷新恢复、三种用户搜索、进入他人资料、缓存与首页/搜索显示名称、严格公开字段、404/400、Desktop/Tablet/Mobile 导航和无横向溢出、退出/登录重定向、管理员 Metrics。Frontend container 实际提供 SPA fallback。截图已保存并检查 Desktop/Mobile 时间线与管理员布局；最终五张截图见 `frontend/test-results/profile-*.png`，另复制到 `/tmp/gopulse-p13-evidence/`。最终测试是在新增导航收藏禁用占位和路由格式整理后重建前端容器运行，未用旧镜像充当最终结果。

两次定向 frontend build 和容器替换仅用于发生相关变化的 UI；没有重跑整个 Phase 12 生命周期或不相关服务门禁。Backend/Frontend 镜像使用本批专属 `p13-local` tag，没有覆盖已有 `1.9.4` 镜像。

版本与分支治理、diff 检查结果见提交前收口条目。

## Phase-13-02 交接

- 稳定 user id/username 与可变 display_name/bio 已分离，Profile.is_self 是唯一当前 viewer 产品状态，不伪造关注计数。
- `/users/:username` 为 Frontend 资料路由；`/users/me` 继续保留原认证 DTO，`/users/me/profile` 只更新本人。
- UserAppShell 独立于 AdminLayout；后续可替换 Following/收藏禁用占位，不需改管理布局。
- 暗色、头像媒体、更多搜索排序、全应用既有类型错误作为后续；不扩展本批。

## 提交前收口

- `VERSION`、Frontend package/lock 根元数据与 `.env.example` 的产品版本/image tag 已同步为 `1.10.1`；规划文件状态更新为本地批次完成，未声称已经合入 main。
- `python3 scripts/ci/validate_versions.py`：PASS。
- `python3 scripts/ci/validate_branch.py --branch develop/1.10.1 --base-ref origin/main`：PASS。
- `git diff --check`：PASS；提交后同样检查 `origin/main...HEAD`。
- 专用 Compose 项目执行 `down --volumes --remove-orphans` 成功；按项目 label 查询确认 containers、networks、volumes 均无残留。截图与命令输出保留在 `/tmp/gopulse-p13-evidence/`，不提交运行期数据或测试凭据。
- 本批要求的完成门禁通过；上文已验证为基线既存的显式类型诊断保留为非阻断限制。无关注/收藏等后续批次事实混入本提交。
