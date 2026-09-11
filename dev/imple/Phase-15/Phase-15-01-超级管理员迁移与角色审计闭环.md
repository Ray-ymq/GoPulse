# Phase-15-01：超级管理员迁移与角色审计闭环实施方案

> 当前状态：待实施。本文档定义 Phase 15 第一个执行批次的范围与验收合同；目标版本 `1.12.1`、开发分支 `develop/1.12.1` 和执行顺序以 `Phase-15-总实施方案.md` 为准。

## 1. 批次目标

在不改变社交业务授权与 Phase 14 管理 API DTO 的前提下，将现有 `user|admin` 升级为最终 `user|super_admin`，建立唯一引导超级管理员、按用户 ID 调整角色和可查的管理审计基础：

```text
Phase 14 users.role=user|admin
  → 可重试 schema/data migration
  → users.role=user|super_admin
  → 唯一 bootstrap user_id
  → DB-authoritative RequireSuperAdmin
  → 按 ID 查询/提升/降级 + 同事务审计
```

本批完成后，后续告警和独立管理 Frontend 可依赖稳定的 `super_admin` 信任根；不得仍以旧 `admin` 为隐式兼容值。

## 2. 前置条件

- Phase 14 完成基线已合入最新 `upstream/main`，根版本为 `1.11.7`。
- fetch 后从最新 `upstream/main` 创建 `develop/1.12.1`，不在 `update` 或 Phase 14 分支上实施。
- 保存一份带普通用户、一个旧 admin、两个旧 admin 和无 admin 的强归属 MySQL fixture；不修改用户的日常数据卷。
- 有界确认锁定 MySQL 对 ENUM 扩宽/收窄和 DDL 中断重跑的真实行为，将结果写入同名实施记录。

## 3. 实施范围

### 3.1 可重试角色迁移

- 使用开工时下一个可用 migration，按总方案 §7.2 依次扩宽 ENUM、原位将 `admin` 转为 `super_admin`、建立 bootstrap singleton，再收窄 ENUM。
- 存在旧 admin 时固定选最小 ID 为 bootstrap；所有其他旧 admin 保留为非 bootstrap `super_admin`。不删除或合并账号。
- singleton 表只允许固定 key 和一个唯一 user ID，并使用 `ON DELETE RESTRICT`。对已转换数据和已存在 singleton 的重跑不改变结果。
- down migration 先安全映射全部 `super_admin` 为 `admin`，再移除新表/约束；不删除用户或会话定位事实。

### 3.2 引导账号命令与启动不变式

- 为 operations image 提供以精确 user ID 声明 bootstrap 的规范命令；它在事务中锁定 singleton 和 target，只在 singleton 空缺时同时设置角色与引导标记。
- 保留旧 `admin-role promote --username` 的一版本安全兼容：只能初始化空 singleton 或幂等确认同一 bootstrap，不能提升第二个账号。更新 Compose 与验收脚本使用新规范入口。
- Backend 启动时读取 singleton 并确认目标是 `super_admin`。无 bootstrap 的空库保留社交注册能力，但管理路径返回固定 setup 不可用；不将告警/管理就绪加入社交 readiness。

### 3.3 Backend 角色模型与授权

- 将 Go `RoleAdmin`、parser、middleware、配置/命令、测试 fixture 和公共错误文案收敛为 `RoleSuperAdmin` 与 `RequireSuperAdmin`。不保留同时接受 admin/super_admin 的长期分支。
- 保留现有每请求从 MySQL 重读当前用户的授权逻辑，JWT 仍只定位用户 ID。
- 现有 Observability 与 exporter-plugin 路由全部切换新中间件，未登录与普通用户的 401/403 语义不变。
- 更新 `componentmetrics` 的 Backend route catalog 以包含本批新 API，不因 route template 变化产生高基数原始 path。

### 3.4 按 ID 用户查询与角色变更

- 实现 `GET /api/v1/admin/users/:userId` 与 `PUT /api/v1/admin/users/:userId/role`，严格使用总方案 §7.4 的请求/响应形状。
- 变更事务依次锁定 actor、target 和 singleton，再校验 actor 仍为 `super_admin`、target 存在、bootstrap 不被降级，最后写角色和审计。
- 同角色 PUT 幂等返回 `changed=false`，不追加伪变更审计。非 bootstrap 管理员自降级允许完成，其后管理请求立即 403。

### 3.5 审计基础

- 创建总方案 §8 的 `management_audit_events` 追加模型、repository/service 与签名 cursor 查询 API。
- 本批至少交付 bootstrap 声明和用户角色变更的原子成功审计；同时为后续规则和插件操作提供受限 action/detail builder，不预造未发生记录。
- 审计公共 DTO 不包含密码 hash、Cookie、用户资料、原始 SQL 或上游错误。

### 3.6 当前 Frontend 过渡兼容

- 将当前 `frontend` 的 role union、响应验证、auth composable、路由守卫与管理页条件从 `admin` 更为 `super_admin`，确保本批上线后管理能力不中断。
- 普通用户直达管理页继续被路由守卫阻止，但验收必须同时请求 Backend API 证明 403。
- 本批不移动 AdminLayout/Observability 文件；迁移属于 Phase-15-04。

## 4. 不在本批范围

- 告警规则、状态、评估器、当前/历史告警表。
- 独立 `admin-frontend`、管理大屏或新角色管理页。
- 用户模糊搜索、列表、删除、封禁、批量授权或新 RBAC 层次。
- 对社交 API 新增 `RequireUser`、强制超级管理员会话失效或把角色写入 JWT。
- 为了批次验收手工修改日常 MySQL volume 或绕过 migration history。

## 5. 建议实施顺序

1. 核对 migration runner、用户 repository、授权 middleware、auth DTO 和现有 operations 命令的直接调用点。
2. 完成数据迁移与新/旧 fixture 的中断重跑验证，先确立持久不变式。
3. 收敛 Go 角色模型、`RequireSuperAdmin` 和引导命令，更新 Compose/acceptance 调用。
4. 实现管理审计 repository/query，再实现按 ID 查询与角色变更事务。
5. 更新现有 Frontend 角色语义和直接回归测试。
6. 运行真实 MySQL 角色闭环、负向矩阵、审计/脱敏与固定批次门禁，最后更新版本和实施记录。

## 6. 预计直接影响文件

- `backend/migrations/` 的下一个角色/bootstrap/审计 migration 及集成测试
- `backend/internal/user/`、新的管理用户/审计边界、`backend/internal/http/` 和 `backend/internal/http/middleware/`
- `backend/cmd/admin-role/` 或实施后的等价规范 operations 命令
- `backend/cmd/server/main.go`、`backend/internal/config/` 与 `componentmetrics/routes.go`
- `frontend/src/types/`、`frontend/src/services/`、`frontend/src/composables/`、`frontend/src/router/` 及直接测试
- `deploy/compose.yaml`、后端 Dockerfile、`.env.example` 和直接运维说明
- 新 `scripts/verify-role-management.sh` 及最小直接验收辅助文件
- `VERSION`、Frontend 版本元数据和同名实施记录

实际文件以最小实现为准；不为了符合清单创建无调用者的层。

## 7. 批次验收标准

### 7.1 迁移与引导账号

- 普通用户库升级后仍全部为 `user`；单 admin 与多 admin 库的全部 admin 转为 `super_admin`，多 admin 时最小 ID 是唯一 bootstrap。
- migration 在代表性 DDL 中断后重跑成功，不出现角色丢失、第二个 singleton 或重复 migration history。
- 空库先注册、再声明 bootstrap 成功；同账号重跑幂等，不同账号重跑被拒绝。
- 旧会话升级后请求 `/users/me` 返回 `super_admin`，不需要重新登录或伪造新 Cookie。

### 7.2 角色与审计闭环

- bootstrap 查询显示保护标记，其降级和直接删除尝试被服务/外键拒绝；其他用户可在两种角色之间变更。
- 非 bootstrap 超级管理员可自降级，同一 Cookie 的下一个 Observability/插件/用户管理 API 均返回 403，社交 API 仍可使用。
- 未登录/user/super_admin 对查询与变更、bootstrap/普通/非 bootstrap/不存在/非法 ID 的负向矩阵全部符合固定 400/401/403/404/409 语义。
- 真实变更与同账号幂等重试的审计行数正确；审计 API 只对 `super_admin` 开放并不泄漏密码 hash、Cookie、SQL 或原始错误。

### 7.3 既有回归和完成条件

- `super_admin` 可使用当前管理 Frontend 和 Metrics/Logs/Events/六插件 API，`user` 构造直接请求仍 403。
- 一个普通用户完成登录、当前用户读取和一条代表性社交主路，角色迁移不把社交授权改为管理授权。
- 根、Frontend 和受管版本元数据为 `1.12.1`，分支为 `develop/1.12.1`，实施记录与真实命令一致。
- 没有告警表/评估器或独立管理应用文件被提前实现。

## 8. 固定验证命令与回归范围

实施中先运行直接 package 测试。本批最终 diff 固定运行：

```bash
(cd backend && go test ./internal/user ./internal/http/middleware ./internal/http ./migrations ./cmd/admin-role)
(cd frontend && npm test)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)

bash scripts/verify-role-management.sh --self-test
bash scripts/verify-role-management.sh

python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.12.1 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- `verify-role-management.sh` 必须使用强归属 MySQL 和 Backend，覆盖 migration 新/旧库、bootstrap 命令、角色矩阵、会话及时生效、审计和清理；`--self-test` 不访问 Docker。
- 如因实际 package 命名不同调整 Go 命令，必须在实施记录列出等价命令。固定闭环、Frontend 和治理门禁不可省略。
- 提交后补充运行 `git diff --check upstream/main...HEAD`；提交前不用它代替工作树/暂存区检查。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-15/Phase-15-01-超级管理员迁移与角色审计闭环.md`。

记录必须包含实际 migration 编号/SQL 语义、中断点与重跑结果、旧 admin 分布、bootstrap 命令与外键保护、负向矩阵、审计行与脱敏、全部命令/结果、实际文件、偏差和后续项。交给 Phase-15-02 的固定输入至少包括：

- `RequireSuperAdmin` 与每请求 MySQL 重读边界。
- bootstrap 用户 ID 保护和可用的多超级管理员模型。
- 可复用的受限审计 action/detail builder、repository 与查询 API。
- 已升级的 Frontend auth DTO，但独立管理 Frontend 仍未实施。
