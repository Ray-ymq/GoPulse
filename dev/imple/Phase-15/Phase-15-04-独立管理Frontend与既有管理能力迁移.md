# Phase-15-04：独立管理 Frontend 与既有管理能力迁移实施方案

> 当前状态：已完成（2026-09-12）；两端固定检查、真实双镜像同源浏览器验收和运行边界检查通过，证据见同名开发记录。本文档定义 Phase 15 第四个执行批次的范围与验收合同；目标版本 `1.12.4`、开发分支 `develop/1.12.4` 和执行顺序以 `Phase-15-总实施方案.md` 为准。

## 1. 批次目标

将现有用户与管理页共用单 bundle 的形态收敛为两个独立 Frontend 应用，在同一宿主入口下保持 HttpOnly 会话、数据库权威授权和 Phase 14 全部现有管理能力：

```text
Browser http(s)://one-origin
  → frontend edge :8080
       ├─ /login, /posts, ... → user Frontend
       ├─ /admin/...          → internal admin-frontend
       └─ /api/v1/...          → Backend

admin-frontend bootstrap
  → GET /api/v1/users/me
  → super_admin 才 mount 管理 route
  → Metrics / Logs / Events / 六插件使用真实 Backend API
```

本批不开发完整大屏、告警页或用户角色页；它先为新产品能力建立可独立构建和验收的稳定载体。

## 2. 前置条件

- Phase-15-03 已合入最新 `upstream/main`，根与 Frontend 版本为 `1.12.3`，`super_admin` 与 Backend 三源告警合同已完成。
- fetch 后从最新 `upstream/main` 创建 `develop/1.12.4`。
- 现有用户 Frontend 中的 `AdminLayout`、Observability views/services/types、路由和测试清单已经核对，迁移时不顺手重写公共 API 语义。
- 按总方案 §19 有界确认 Nginx 对 `/admin`/`/admin/`、SPA deep link、Vite base asset 和同源 Cookie 的真实容器行为。

## 3. 实施范围

### 3.1 独立 `admin-frontend` 工程

- 新建 `admin-frontend/` Vue/Vite 工程，使用自己的 `package.json`、lockfile、TypeScript/Vite/Vitest 配置、entrypoint、router、styles、tests 和 `dist`。
- 管理应用 SPA base 固定为 `/admin/`，路由固定为 `/admin/`、`/admin/metrics`、`/admin/logs`、`/admin/events`、`/admin/plugins`；后续 alerts/users/audit 由 Phase-15-05 增加。
- 本批 `/admin/` 可在授权成功后显式导向 `/admin/metrics` 或显示只由真实 Backend 连接状态构成的最小首页；不使用静态大屏卡片冒充 Phase-15-05。
- 管理应用不包含注册、发帖、关注、收藏、通知等用户业务页面或路由。

### 3.2 现有管理能力迁移

- 将当前 `AdminLayout`、Metrics、Logs、Events、Exporter/Plugin views 及其专用 services/types/composables/tests 迁入 `admin-frontend`，保持 Phase 14 真实 API、strict DTO 验证、分页、错误、Secret 清理与操作确认。
- 从 `frontend` 路由树、imports、styles 和生产 bundle 中移除管理页，但保留用户端需要的 `super_admin` 会话识别和跨应用导航。
- 两应用可拥有各自的严格 HTTP/DTO 实现；若为避免协议漂移引入最小共享包，它只允许包含无 UI 的认证与响应协议，不共享业务页、布局或路由树。
- 现有 `/admin/observability`、`/admin/observability/metrics`、`/admin/observability/logs`、`/admin/observability/events`、`/admin/observability/exporters` 深链接分别重定向新路由，不在用户应用挂载一份隐藏兼容页。

### 3.3 同源 edge 与两个镜像

- 增加管理 Frontend Dockerfile 与最小 Nginx runtime，在镜像内运行管理应用 tests/typecheck/build，移除 source map，使用 numeric non-root user 和 read-only filesystem 兼容路径。
- Compose 增加 `admin-frontend` service，只连 edge network，不发布 host port，提供独立 healthcheck 与有界停止。
- 现有 `frontend` 继续是唯一发布 edge，对 `/admin/` 代理内部管理 service，对 `/api/v1/` 代理 Backend，其他 SPA 路由使用用户应用 fallback。
- `/admin` 固定重定向 `/admin/`，管理 static assets 与 deep-link fallback 不落入用户 `index.html`。插件 update 的 body/timeout 特殊代理边界继续保留。
- 验收 browser 只使用公布的 frontend origin，不直接访问 `admin-frontend` 容器或 Backend 内部 origin 来冒充同源。

### 3.4 认证恢复与路由分流

- `/login` 继续位于用户应用。登录/会话恢复返回 `user` 时默认 `/posts`，返回 `super_admin` 时默认 `/admin/`。
- 实现受限 redirect 验证：只允许同 origin 绝对 path，且 user 目标不得为 `/admin/`，super_admin 管理深链接可在登录后恢复。拒绝 scheme/host、`//`、编码绕过和控制字符。
- 管理应用在 mount 任何管理 view 或请求管理 API 前先请求 `/api/v1/users/me`。401 导向带安全 redirect 的 `/login`，非 `super_admin` 导向 `/posts`。
- 任意管理 API 返回 403 时，管理 auth state 立即降级、清理已挂载数据与候选 Secret，再导向用户端；不把用户会话误清除为未登录。
- 用户应用恢复 `super_admin` 时不挂载用户业务 shell，导向管理应用。后端仍是最终授权，客户端分流不构成权限边界。

### 3.5 两应用版本与运行契约

- 新管理应用的 package 版本从本批目标 `1.12.4` 开始，用户/管理两个 package 与两个镜像 label 都与根 `VERSION` 同步。
- 两个镜像都不包含 Node.js、npm、源码、source map、内部主机名、token 或构建机路径。
- 现有 `scripts/dev.sh`/`down.sh` 与 Compose 强归属 project 使用单一生命周期，不为管理应用创建第二套宿主启停脚本。

## 4. 不在本批范围

- 管理大屏的最终 overview API/卡片/图表与部分失败聚合。
- 告警规则、当前/历史、用户角色和审计新页面。
- 修改 Phase-15-03 的告警状态机/API，或让 Frontend 直连可观测组件。
- 统一设计令牌、完整响应式/无障碍产品化、TLS/域名、多架构或真实 macOS/Windows 验收；这些属于 Phase 16。
- 将两个应用再合并到一个 bundle，或为了“复用”共享业务布局/页面。

## 5. 建议实施顺序

1. 列出现有管理路由、view/service/type/test 及 Nginx 特殊 location，固定新老路由映射。
2. 创建最小 `admin-frontend` 工程与独立测试/构建，实现 auth bootstrap 和 `/admin/` base。
3. 逐页迁移 Metrics、Logs、Events 和 Plugins，每移一页就从用户 app 删除该路由/import，不长期复制两份。
4. 交付管理镜像/internal service 与 edge proxy/fallback/旧路由重定向，验证只有一个宿主 origin。
5. 完成 user/super_admin 登录、直达深链接、刷新、降级和 API 401/403 浏览器矩阵。
6. 运行两端制品/镜像/同源/既有管理能力门禁，然后更新版本和实施记录。

## 6. 预计直接影响文件

- 新 `admin-frontend/` 工程、页面、服务、类型、样式和测试
- `frontend/src/router/`、现有管理组件/views/services/types/tests 的迁移/删除，以及用户 auth/login 分流
- 新 `deploy/docker/admin-frontend.Dockerfile` 与管理 Nginx 配置
- `deploy/docker/frontend/nginx.conf`、`deploy/compose.yaml`、镜像构建/healthcheck 直接文件
- Frontend/acceptance e2e 的管理 base URL 与旧路由迁移
- 新 `scripts/verify-admin-frontend.sh` 与直接浏览器验收辅助文件
- `VERSION`、两个 Frontend package/lockfile 版本、受管镜像元数据和同名实施记录

## 7. 批次验收标准

### 7.1 工程、制品与路由

- `frontend` 和 `admin-frontend` 可分别安装、测试、typecheck 和 build，产生两个独立制品/镜像；版本与 revision label 正确。
- 用户应用无 AdminLayout/Observability 业务路由或页面代码，管理应用无社交业务页面。源码映射、Node/npm、Secret 和内部地址不进入两个 runtime image/bundle。
- `/admin`、`/admin/`、四个新 deep link、页面刷新和旧 `/admin/observability/*` 路由经唯一公布 origin 返回正确应用，不落入用户 SPA fallback。
- `admin-frontend` 无 host port，浏览器不能绕开 edge 直接访问；两个服务的 health、numeric user、read-only filesystem 和 SIGTERM 正常。

### 7.2 会话、权限和既有功能

- 同一 browser context 的 user 登录默认到 `/posts`，super_admin 登录默认到 `/admin/`；两者只使用同 origin HttpOnly Cookie，无 localStorage token。
- 未登录直达管理 deep link 先到登录页，登录后只在安全且角色匹配时恢复原 path；外部 redirect 和 user 的 admin redirect 被拒绝。
- 普通用户直达管理应用不发起 Metrics/Logs/Events/Plugin 数据请求，直接构造这些 API 均 403。
- super_admin 在新应用完成代表性 Metrics/Logs/Events 查询、六插件列表与一次受控生命周期操作；现有 strict DTO、分页、错误和 Secret 清理不回归。
- 将当前非 bootstrap super_admin 降级后，下一个管理请求 403，已显示 DOM/候选 Secret 清理，浏览器转到用户端且社交会话仍有效。

### 7.3 完成条件

- 用户端代表性登录/社交路由与 Backend 401 处理不回归，现有 Phase 14 管理功能在新应用完整可用。
- 本批未用静态卡片或 mock 给出“大屏完成”，也未新增告警/角色/审计业务页。
- 根、两个 Frontend 和受管版本元数据为 `1.12.4`，分支为 `develop/1.12.4`，同名实施记录与真实命令一致。

## 8. 固定验证命令与回归范围

本批最终 diff 固定运行：

```bash
(cd frontend && npm test)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)

(cd admin-frontend && npm test)
(cd admin-frontend && npm run typecheck)
(cd admin-frontend && npm run build)

bash scripts/verify-admin-frontend.sh --self-test
bash scripts/verify-admin-frontend.sh --existing-management

python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.12.4 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- `verify-admin-frontend.sh --existing-management` 必须构建两个 runtime image，经唯一 edge origin 运行真实 browser/API 角色与四个既有管理页闭环，最后强归属清理；`--self-test` 不访问 Docker。
- 如迁移改动 Backend 公共 DTO（原则上不应），补跑直接 Backend contract tests 并记录原因；不因页面迁移重跑三源告警全故障矩阵。
- 提交后补充运行 `git diff --check upstream/main...HEAD`。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-15/Phase-15-04-独立管理Frontend与既有管理能力迁移.md`。

记录必须包含迁移/删除文件映射、两个 package/image 版本、Nginx location 与 SPA fallback、宿主/内部端口、登录/redirect/deep-link/降级矩阵、既有四页真实操作、bundle/image 扫描、命令结果、偏差和后续项。交给 Phase-15-05 的固定输入至少包括：

- 已稳定的 `/admin/` 独立应用、同源 session 和角色失效处理。
- 已迁移的 Metrics/Logs/Events/六插件页与组件风格基础。
- 大屏与 alerts/users/audit 页尚未完成，不得以临时首页代替。
