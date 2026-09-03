# Phase 6-01：管理员身份与双用户态授权边界实施记录

- 实施日期：2026-09-03
- 开发分支：`develop/1.3.1`
- 目标版本：`1.3.1`
- 基线：已 fetch `origin`，从当时最新 `origin/main`（`2c73bbc3aa24cd47859ae4b62111c93675073d54`，产品版本 `1.2.3`）创建本批分支。

## 1. 实际完成内容

### 1.1 持久化角色与用户契约

- 新增 `000005_user_roles` 可逆迁移，为 `users` 增加 `ENUM('user', 'admin') NOT NULL DEFAULT 'user'` 字段，并提供 down migration 删除该字段。
- 新增 `user.Role`、`RoleUser`、`RoleAdmin` 与严格解析；Repository 的用户读取统一扫描并验证数据库角色，非法值不会被当作管理员。
- 注册继续依赖数据库默认值创建 `user`；注册、登录和 `/api/v1/users/me` 的当前用户 DTO 增加 `role`。
- JWT 和 Cookie 契约未改变，JWT 仍只携带用户 ID。角色提升后，持有原 Cookie 的 `/users/me` 请求会重新读取 MySQL 并返回最新 `admin`。
- 帖子、评论、通知和搜索继续使用各自仅含 `id`、`username` 的作者摘要，没有把角色加入公开业务投影、缓存或消息契约。

### 1.2 管理员提升 CLI

- 新增 `go run ./cmd/admin-role promote --username <username>`。
- CLI 复用登录的 username 规范化规则，只提供 promote；用户不存在时失败，已是管理员时幂等成功。
- Repository 使用受限的 `user -> admin` 更新条件，命令只输出通用成功信息；配置、数据库错误、密码、token、DSN 和用户记录不会进入命令输出或公开错误。
- 未增加降级、禁用、删除、列表、环境变量自动赋权、默认管理员或网页赋权能力。

### 1.3 服务端管理员授权边界

- 新增稳定错误码 `permission_denied`，HTTP 映射为 `403`。
- 新增可复用 `middleware.RequireAdmin`：先读取认证中间件写入的用户 ID，再按请求从 Repository 读取当前数据库角色。
- 未登录或用户已删除时返回 `401 authentication_required`；普通用户返回 `403 permission_denied`；管理员允许调用下游 handler；数据库读取失败返回安全的 `500 internal_error`。
- 代表性中间件测试确认所有拒绝路径都不会调用受保护 handler，且未登录路径不会查询用户 Repository。

### 1.4 Frontend 与文档

- Frontend `PublicUser` 增加 `role: 'user' | 'admin'`，已知错误码增加 `permission_denied`。
- 注册、登录和当前用户恢复改用严格响应校验，拒绝缺失角色、未知角色、额外字段或其他不符合当前用户契约的数据。
- 认证状态恢复逻辑无需改写；现有 `useAuth` 继续保存经过校验的当前用户，并增加无效角色响应回归测试。
- README 更新双用户态、显式提升命令、数据库实时授权原则、权限矩阵与本批未实现边界，没有声明管理页面或管理 API 已交付。
- 根版本及 Frontend package/lockfile 同步更新为 `1.3.1`。

## 2. 实际变更文件

- `README.md`
- `VERSION`
- `backend/cmd/admin-role/main.go`
- `backend/cmd/admin-role/main_test.go`
- `backend/cmd/admin-role/integration_test.go`
- `backend/internal/apperror/error.go`
- `backend/internal/auth/integration_test.go`
- `backend/internal/auth/service_test.go`
- `backend/internal/http/auth_integration_test.go`
- `backend/internal/http/middleware/authorization.go`
- `backend/internal/http/middleware/authorization_test.go`
- `backend/internal/http/response/response.go`
- `backend/internal/http/router_auth_test.go`
- `backend/internal/user/model.go`
- `backend/internal/user/model_test.go`
- `backend/internal/user/repository.go`
- `backend/migrations/000005_user_roles.up.sql`
- `backend/migrations/000005_user_roles.down.sql`
- `backend/migrations/embed_test.go`
- `backend/migrations/user_roles_integration_test.go`
- `frontend/package.json`
- `frontend/package-lock.json`
- `frontend/src/types/api.ts`
- `frontend/src/services/api.ts`
- `frontend/src/services/http.ts`
- `frontend/src/composables/useAuth.test.ts`
- `frontend/src/router/index.test.ts`
- `frontend/src/services/connectivity.test.ts`
- `frontend/src/views/BusinessViews.test.ts`
- `frontend/src/views/NotificationsView.test.ts`
- `frontend/src/views/SearchView.test.ts`
- `dev/logs/Phase-06/Phase-06-01-管理员身份与双用户态授权边界.md`

## 3. 验证命令与结果

### 3.1 定向实现验证

- `(cd backend && go test -count=1 ./internal/user ./internal/http/middleware ./internal/apperror ./internal/http/response ./cmd/admin-role ./internal/auth ./internal/http ./migrations)`：通过。
- `(cd frontend && npm test -- --run)`：通过，9 个测试文件、48 个测试通过。
- `(cd frontend && npm run build)`：通过，`vue-tsc --noEmit` 与 Vite production build 成功。

### 3.2 隔离 MySQL 集成验证

使用本批临时创建并在命令结束时删除的 `mysql:8.4.0` 容器，未使用或清理启动前已存在的容器、端口和进程。先执行全部迁移，再运行本批直接相关的 integration packages：

```bash
(cd backend && go run ./cmd/migrate up)
(cd backend && go test -count=1 -tags=integration ./migrations ./cmd/admin-role ./internal/auth ./internal/http)
```

结果：通过。实际验证了迁移 up/down、既有行与新行默认 `user`、非法角色写入失败、CLI 提升/重复提升/用户不存在、注册默认角色，以及同一 Cookie 在提升后读取最新 `admin`。

### 3.3 固定完成门禁

以下命令均在最终生产代码、测试、版本和 README 变更完成后执行并通过：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.3.1 --base-ref upstream/main
git diff --check
```

结果：

- Backend 全量普通测试、vet 与 race 测试通过。
- Frontend 9 个测试文件、48 个测试通过，typecheck 与 production build 通过。
- `scripts/ci` 24 个单元测试通过。
- 版本元数据和 `develop/1.3.1` 权威分配校验通过。
- Git whitespace 检查通过。

## 4. 与方案的偏差

- 实施方案前置文字预期主线版本为 `1.2.2`，但执行时最新远程 `origin/main` 已为 `1.2.3`。本批按分支生命周期规则使用实际最新主线，并保持总实施方案分配的目标版本 `1.3.1`。
- 未修改 `frontend/src/composables/useAuth.ts`：现有状态恢复实现可直接保存严格校验后的 `PublicUser`，仅类型、API 校验和回归测试需要变化。
- 未修改 `scripts/ci/**` 或 `.github/workflows/quality-gates.yml`：既有版本、分支、Backend、Frontend 和 integration 门禁已覆盖本批新增包与测试，无需制造无意义变更。

## 5. 已知限制与后续项

- 本地固定门禁和隔离 MySQL 集成验证已通过；远程 CI 需在分支推送并创建 PR 后由仓库平台执行，本记录未把尚未执行的远程结果写为已通过。
- 本批只建立身份、提升命令、当前用户契约和可复用授权中间件；插件管理、Metrics/Logs/Events 查询、管理导航与管理页面仍未实现。
- 角色降级、禁用、删除、列表和恢复/审计语义不在本批范围，后续不得通过扩展当前 CLI 隐式加入。
