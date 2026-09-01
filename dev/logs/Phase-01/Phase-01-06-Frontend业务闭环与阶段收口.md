# Phase 1-06：Frontend 业务闭环与阶段收口实施记录

> 对应方案：`dev/imple/Phase-01/Phase-01-06-Frontend业务闭环与阶段收口.md`
> 开发分支：`develop/0.2.6`
> 完成版本：`0.2.6`

## 1. 实际完成内容

### 1.1 Frontend 路由、认证恢复与 HTTP 边界

- 引入 `vue-router@4.6.4`，建立 `/register`、`/login`、`/posts`、`/posts/new`、`/posts/:postId` 和诊断页 `/dev/status`；根路径及未知路径进入业务认证导航。
- 将 Phase 0 连通性页面迁移到不进入业务导航的 `/dev/status`，保留 `/health`、`/ready` 状态诊断能力。
- 新增单例 `useAuth` composable，共享当前用户、三态认证状态和同一个初始化 Promise；路由守卫在首次业务导航前等待 `/api/v1/users/me`，避免刷新时先误判为匿名用户。
- 匿名页和受保护页按认证状态互斥导航；全局 `401` 处理清除内存认证状态并进入登录页。
- Frontend 不读取、解析、保存或续期 JWT；所有业务请求使用同源 `/api/v1` 相对路径和 `credentials: include`。
- 新增完整 TypeScript DTO、分页包装和稳定 `ApiError`；`204` 成功不解析 JSON，未知服务端错误和网络错误只显示安全通用信息。

### 1.2 注册、登录与退出

- 注册和登录页提供用户名、密码校验、重复提交保护，以及用户名冲突、错误凭据、校验失败和网络错误提示。
- 注册或登录成功后更新共享认证状态并进入帖子列表。
- 共享导航显示当前用户和退出入口；退出使用服务端接口，随后清除本地状态并进入登录页。

### 1.3 帖子、评论与点赞业务闭环

- 帖子列表展示标题、内容摘要、作者、时间、评论数、点赞数和当前用户点赞状态，支持 `next_cursor` 加载更多以及加载、空列表、失败和无更多数据状态。
- 发布页执行标题与正文前端校验，成功后进入服务端返回的新帖子详情。
- 详情页展示帖子全文和聚合计数，处理帖子不存在、详情失败、评论加载失败和空评论状态。
- 评论列表支持游标分页；评论成功后重新读取帖子与第一页评论，以服务端事实刷新计数和内容。
- 点赞与取消点赞分别使用显式 `PUT`、`DELETE`，操作期间阻止重复提交，成功后重新读取详情，不采用本地乐观计数替代服务端事实。
- 新增响应式业务样式，覆盖桌面和窄屏下的导航、表单、列表、详情与状态提示。

### 1.4 自动化测试与真实浏览器验收

- 新增 HTTP service、认证初始化、路由守卫、认证表单、帖子列表、发布、详情、评论、点赞和诊断页测试。
- Vitest 明确只包含单元/组件测试，不会把 Playwright E2E 误收集为普通测试。
- 引入 `@playwright/test@1.62.1` 和 Chromium E2E，覆盖浏览器注册、自动登录、刷新认证恢复、发帖、评论、点赞、取消点赞、退出、受保护页重定向及重新登录。

### 1.5 Bash 验收与 CI 安全门禁

- 扩展只读 `scripts/verify.sh`：除 Compose、`/health`、`/ready` 和 Frontend 外，验证未认证帖子列表稳定返回 `401 authentication_required`；脚本不创建或修改业务事实。
- 新增可执行 `scripts/verify-business.sh`，使用随机 12 位小写十六进制 token 派生专用 Compose project、数据库、Cookie 名和临时目录，并为 MySQL、Redis、RabbitMQ、Backend、Frontend 分配非默认回环端口。
- 完整验收从空业务库迁移，执行 API 闭环和真实 Chromium E2E，验证 Backend 重启持久化、验收 Redis `FLUSHDB` 后重建、Redis 停止时 MySQL 业务降级，以及 Redis 恢复后无需重启 Backend 即恢复 readiness 和缓存。
- 启动前拒绝复用任何同名 Compose 容器、网络或 volume；清理前校验 Compose project label、service 唯一容器 ID、发布端口和资源归属。资源创建前即启用失败清理，因此 Compose 部分启动失败以及后续成功、失败、`INT`、`TERM` 均只清理本次验收资源。
- 验收前后比较日常开发 `.env`、`.run`、Compose 容器稳定状态与启动时间、MySQL/Redis 状态和具名卷，确认并行日常栈不被修改。
- 新增不访问 Docker 的安全负向测试，拒绝空 token、默认 project/数据库、非回环地址、默认端口和重复端口。
- Linux quality gate 增加 `verify-business.sh` 的 Bash 语法检查和安全自测；原生 PowerShell 文件保持 `0.2.1` 冻结基线，未修改。

### 1.6 文档、版本与阶段收口

- README 更新为 Phase 1 完成状态，记录业务路由、认证导航、只读验证、完整隔离验收、浏览器 E2E 和 Phase 2 事实边界。
- 根 `VERSION` 从 `0.2.5` 更新为本批目标版本 `0.2.6`。
- 核对 `dev/logs/Phase-01/`，Phase-01-01 至 Phase-01-06 六份实施记录均存在。

## 2. 实际变更文件

- 仓库、CI 与文档：
  - `.github/workflows/quality-gates.yml`
  - `.gitignore`
  - `README.md`
  - `VERSION`
  - `dev/logs/Phase-01/Phase-01-06-Frontend业务闭环与阶段收口.md`
- Frontend 工程与配置：
  - `frontend/package.json`
  - `frontend/package-lock.json`
  - `frontend/playwright.config.ts`
  - `frontend/vite.config.ts`
  - `frontend/src/main.ts`
  - `frontend/src/App.vue`
  - `frontend/src/styles.css`
  - 删除旧的 `frontend/src/App.test.ts`，按诊断页与业务模块拆分测试
- Frontend 业务实现：
  - `frontend/src/components/AppNav.vue`
  - `frontend/src/components/AuthForm.vue`
  - `frontend/src/components/PostCard.vue`
  - `frontend/src/composables/useAuth.ts`
  - `frontend/src/router/index.ts`
  - `frontend/src/services/api.ts`
  - `frontend/src/services/http.ts`
  - `frontend/src/types/api.ts`
  - `frontend/src/utils/format.ts`
  - `frontend/src/views/DevStatusView.vue`
  - `frontend/src/views/LoginView.vue`
  - `frontend/src/views/NewPostView.vue`
  - `frontend/src/views/PostDetailView.vue`
  - `frontend/src/views/PostsView.vue`
  - `frontend/src/views/RegisterView.vue`
- Frontend 测试：
  - `frontend/e2e/business.spec.ts`
  - `frontend/src/composables/useAuth.test.ts`
  - `frontend/src/router/index.test.ts`
  - `frontend/src/services/http.test.ts`
  - `frontend/src/views/BusinessViews.test.ts`
  - `frontend/src/views/DevStatusView.test.ts`
- Bash 验收与安全测试：
  - `scripts/verify.sh`
  - `scripts/verify-business.sh`
  - `scripts/ci/test_verify_business.py`

未修改 Phase-01-02 后冻结的 `scripts/*.ps1`。

## 3. 实际验证命令与结果

### 3.1 Frontend 单元、类型与构建

实际执行：

```bash
cd frontend
npm test
npm run typecheck
npm run build
```

结果：全部通过；Vitest 共 7 个测试文件、33 个测试通过，TypeScript/Vue 类型检查通过，Vite 生产构建通过。

### 3.2 Backend 单元、静态与竞态回归

实际执行：

```bash
cd backend
test -z "$(gofmt -l .)"
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
```

结果：全部通过；Frontend 本批未改变 Backend 业务实现，但按阶段收口要求完成全量 Backend 必要回归。

### 3.3 隔离真实依赖 integration

使用独立 Compose project `gopulse-integration-595e036296e1` 和随机回环宿主端口启动 MySQL、Redis、RabbitMQ，配置：

- `INTEGRATION_TESTS=1`
- `APP_ENV=test`
- `MYSQL_HOST=127.0.0.1`
- `MYSQL_DATABASE=gopulse_integration`
- `MYSQL_USER=gopulse_integration`
- `REDIS_HOST=127.0.0.1`
- `REDIS_DB=15`

实际执行：

```bash
cd backend
go run ./cmd/migrate up
go test -count=1 -tags=integration ./...
```

结果：迁移和全部 tagged integration package 通过；真实 MySQL、Redis、HTTP、认证、帖子、评论、点赞和缓存回归均成功。命令退出后删除该 project 的容器、网络和 volumes，未操作日常 `gopulse` project。

### 3.4 完整隔离业务验收与真实 Chromium

实际执行：

```bash
scripts/verify-business.sh
```

最终安全加固后的成功验收使用：

- Compose project：`gopulse-acceptance-7166a3d6e1d4`
- 数据库：`gopulse_acceptance_7166a3d6e1d4`
- 随机非默认回环端口和独立临时进程目录

结果：通过。覆盖：

- 从空库向上迁移和 Backend/Frontend 启动；
- API 注册、认证恢复、发帖、帖子分页、评论分页、评论、点赞和取消点赞；
- 真实 Playwright Chromium 浏览器闭环，1 个 E2E 测试通过；
- Backend 重启后用户、帖子、评论和点赞事实仍存在；
- 仅清空验收 Redis DB 后业务事实保持且详情缓存重建；
- Redis 停止时 `/ready` 报告故障而业务继续使用 MySQL；
- Redis 恢复后不重启 Backend 即恢复 readiness 和缓存；
- 日常开发栈验收前后快照一致；
- 验收容器、网络和 volumes 均已清理。

首次使用 `gopulse-acceptance-fa7ab6a2e556` 的尝试中，API 流程已通过，但本机缺少 Chromium 所需 `libnspr4`，因此浏览器步骤失败；同时最初快照包含随时间变化的 Docker `Up N minutes` 文本，造成非产品变更的比较误报。安装 Playwright Chromium 系统依赖并将快照改为稳定容器状态/启动字段后，`gopulse-acceptance-e359b965a079` 首次完整通过。随后代码复查补充“拒绝同名预存资源”和“Compose 部分启动失败也清理”的安全边界，并使用 `gopulse-acceptance-7166a3d6e1d4` 重新执行全部流程，最终再次完整通过。首次失败尝试不计为完整通过，所有三次临时资源均已清理。

### 3.5 日常栈只读验证

首次直接对预先存在的日常应用进程运行 `scripts/verify.sh` 时，日常 Backend PID `168617` 是旧二进制，对 `/api/v1/posts` 返回 404；`.run/frontend.json` 记录的 PID `168685` 已不存在。日常 MySQL、Redis、RabbitMQ 仍为 healthy，`/health` 与 `/ready` 通过。该结果是陈旧日常应用进程，不作为当前产品通过结果。

随后保持既有 `.run` 和旧 Backend 不变，使用日常基础设施启动当前代码的临时 Backend（`HTTP_PORT=45403`）和临时 Vite（端口 `5173`），实际执行：

```bash
HTTP_PORT=45403 scripts/verify.sh
```

结果：通过。三个 Compose 服务、`/health`、`/ready`、未认证受保护 API 的 `401 authentication_required` 和 Frontend HTTP 均通过；仅停止本次临时 Backend/Frontend，未终止 PID `168617`，未改写 `.run`。

### 3.6 脚本、治理与配置检查

实际执行：

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
scripts/verify-business.sh --self-test
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
```

结果：全部通过；Python 共 11 个测试通过，安全自测接受 1 个合法目标并在不访问 Docker 的情况下拒绝 6 个不安全目标，Compose 可正常渲染。另使用 Python/PyYAML 解析 `.github/workflows/quality-gates.yml`，结果通过。

完整本地验收还实际执行并通过：

```bash
python3 scripts/ci/validate_branch.py \
  --branch develop/0.2.6 \
  --base-ref origin/main
git diff --check
```

GitHub Actions workflow 已更新，但 GitHub-hosted runner 上的远程执行需在推送分支或创建 PR 后发生，本地未将其记录为已运行。

## 4. 与实施方案的偏差及原因

- 没有产品功能范围偏离。
- 完整验收首次受本机 Chromium 系统依赖和不稳定快照字段影响而未完成；修正环境依赖和验收脚本自身比较口径后，从新建空验收环境重新完整执行并通过。最终代码复查又发现 Compose 部分启动失败时应更早启用清理，补充同名资源预检和启动失败清理后再次从空环境完整执行并通过。
- 日常 `scripts/verify.sh` 的第一次运行暴露出用户已有应用进程陈旧。为避免终止或覆盖用户拥有的进程和 `.run` 记录，改用随机 Backend 端口和临时 Vite 进程验证当前代码，仅复用健康的日常基础设施；这保持了只读验证和资源归属边界。
- GitHub Actions 配置和同等本地命令已验证，但本批未推送，因此远程 CI 未实际运行。

## 5. 已知限制与后续项

- Phase 1 Frontend 是最小业务闭环，不包含个人资料、关注、搜索、通知、媒体上传、管理后台、离线模式或可访问性专项认证；这些属于后续阶段。
- 帖子列表和评论使用手动“加载更多”，未实现虚拟滚动或自动无限滚动。
- 认证仍依赖短期 Cookie 会话；Frontend 不提供 refresh token 或客户端 JWT 管理。
- 日常 `.run` 当前仍记录旧 Backend PID 和已退出 Frontend PID；本批为避免处理用户拥有的运行状态未自动清理。后续用户可在合适时机通过受支持 Bash 生命周期入口重建日常应用进程。
- Phase 2 只能在 MySQL 评论、点赞和取消点赞事实成功提交后增加异步动作；RabbitMQ 故障不得改变已提交同步业务的成功语义，也不得将 RabbitMQ 作为最终事实来源。
