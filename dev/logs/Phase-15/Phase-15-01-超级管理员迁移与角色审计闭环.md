# Phase-15-01：超级管理员迁移与角色审计闭环开发记录

## 1. 交付范围与基线

- 批次目标/交付版本：`1.12.1`；分支：`develop/1.12.1`。
- 开工已 fetch `origin` 与 `upstream`，从最新 `upstream/main`（`4b8d6cd`，根版本 `1.11.7`）新建分支。
- 已完成双角色迁移、bootstrap 运维入口、数据库权威管理授权、按 ID 角色管理、原子成功审计、签名游标查询和当前 Frontend 兼容。没有创建告警表、评估器或独立管理 Frontend。
- 原有未跟踪文件 `~` 未读取、修改、暂存或提交。

## 2. 迁移与持久不变式

实际新增 `000012_super_admin.up/down.sql`：

1. 扩宽为 `ENUM('user','admin','super_admin')`，原位转换全部 `admin`。
2. `bootstrap_super_admin` 固定 singleton=1 的 CHECK、唯一 user_id、users 外键 `ON UPDATE RESTRICT ON DELETE RESTRICT`；取最小 super_admin ID，已有 singleton 时不替换。
3. 收窄为 `ENUM('user','super_admin')`；重跑时仍可再次扩宽/收窄，角色不丢失。
4. 创建追加式 `management_audit_events`：operation_id、微秒时间、actor、固定 action/resource/phase/outcome、request_id、受限 JSON details。resource_id 使用 VARCHAR(128)，以同时表达用户/规则数字 ID 与后续固定插件 ID；用户管理 ID 本身仍为 uint64。
5. down 先扩宽，再将所有 super_admin 映射回 admin，最后删除审计/bootstrap 表并收窄；用户不删除。受控回滚前必须导出审计历史。

`cmd/migrate` 在 migration driver 锁内只恢复 **dirty version 12**：重放完整幂等 SQL，成功后标记 clean；不 force、不忽略其他 dirty version。直接扩展验证原因：此次修改持久数据迁移 runner 与 MySQL DDL 重跑边界。

真实 MySQL **8.4.0** fixture 结果（均为本次随机 Compose 项目的独立 schema）：

| fixture | 原始角色（按 ID） | up 后 | 唯一 bootstrap | down 后 |
| --- | --- | --- | --- | --- |
| empty | 无 | 无 | 无 | 无 |
| users | user,user | user,user | 无 | user,user |
| single | user,admin | user,super_admin | 2 | user,admin |
| multiple | admin,user,admin | super_admin,user,super_admin | 1 | admin,user,admin |

- 分别在第 1 条 DDL、角色转换、第 4 条 singleton 插入后停止，再重放；最后再次完整执行 up，均通过。
- bootstrap 用户直接 DELETE 被外键拒绝；第二 singleton key 被拒绝。
- 在完整历史 fixture 中引入冲突 bootstrap 表，使真实 `migrate up` 在转换角色后的 DDL 阶段失败：history 为 `12/dirty=1`；仅移除故障注入表，再执行 `migrate up` 两次，恢复为唯一 `12/dirty=0` 历史行，用户仍为 super_admin。**没有手写/force migration history**。
- HTTP fixture 在升级前签发真实会话（沿用未变化的 Phase 14 ID-only JWT 协议），停止 Backend，将 fixture 回到旧 schema 并设置旧 admin，真实 up 后重启 Backend；原 Cookie 的 `/users/me` 返回 super_admin，保护标记为 true，无重新登录或伪造 Cookie。

## 3. 引导、授权、角色与审计

### 3.1 引导账号

规范入口：`admin-role bootstrap --user-id <canonical-positive-id>`。事务锁 singleton 和 target，只有空 singleton 才更新角色、写 bootstrap 和系统成功审计；同一 ID 重跑不重复审计，不同 ID 拒绝。

保留一版本 `promote --username`，复用登录用户名规范化后调用同一声明逻辑；不再支持用 CLI 提升第二个账号。Compose 默认命令使用 `GOPULSE_BOOTSTRAP_USER_ID`；当前 Compose/Observability UI 验收入口使用精确 ID，第二管理员通过角色 API 提升，浏览器降级测试也不再直接改库。

Backend 启动校验 bootstrap 与用户角色一致；允许无 bootstrap 的新库启动注册。管理授权每请求重读 MySQL；普通用户仍为 403，无有效 bootstrap 的超级管理员为 `503 management_setup_unavailable`。社交 readiness 没有加入管理 setup 条件。

### 3.2 API 与负向矩阵

- `GET /api/v1/admin/users/:userId`：只返回 `id,username,role,created_at,is_bootstrap_super_admin`。
- `PUT /api/v1/admin/users/:userId/role`：严格解析 JSON，固定 user/super_admin；事务锁 actor、target、singleton，重新授权后变更并审计。响应 `data.user` + `data.changed`。
- `GET /api/v1/admin/audit-events`：签名且绑定 actor/query 的 keyset cursor，limit 1..100，最大 90 天，固定 action/resource_type/outcome 词表；continuation 不允许改过滤条件。
- DTO/审计详情不含密码 hash、Cookie、连接配置、原始 SQL 或上游错误。role/rule/plugin 详情 builder 不接受任意 map 或自由 reason；后续批次可以复用，未提前写入规则/插件审计事件。

真实 Backend/MySQL 验证：

| 身份/目标/输入 | 结果 |
| --- | --- |
| 未登录访问查询/变更：bootstrap、普通用户、另一管理员、不存在、非法 ID | 401 authentication_required |
| user 访问同一矩阵 | 403 permission_denied |
| super_admin 标准 ID 查询、非 bootstrap 提升/自降级 | 200 |
| super_admin 查询/修改不存在 ID | 404 user_not_found |
| 0、01、+1、负数、非数字、uint64 溢出 | 400 validation_failed |
| admin/owner/空角色、未知字段、空 body | 400 validation_failed |
| bootstrap 降级（自己或另一超级管理员发起） | 409 bootstrap_super_admin_protected |
| 无有效 bootstrap 的超级管理员访问管理 API | 503 management_setup_unavailable |
| audit 非法 cursor、被篡改签名、越界 limit/时间范围、非法过滤词 | 400 |

审计行数：bootstrap 首次声明=1；同 ID 重跑和旧命令同账号重跑仍=1；另一用户提升=2；同角色 PUT 仍=2；该用户自降级=3。全部 completed/succeeded，operation_id 唯一，details 仅 before/after。limit=1 分页得到 3 个不重复 ID，action 过滤准确。

用 MySQL trigger 注入真实审计写入失败，错误包含敏感 sentinel：

- bootstrap 声明失败时，role 与 singleton 均回滚。
- 普通用户提升失败时，role 未变、审计行数未增，API 500 不暴露 trigger 的原始错误。

自降级请求成功后，**同一 Cookie** 下一个 Metrics/Logs/Events、插件、用户管理及审计请求均为 403；`users/me` 为 user，社交发帖仍成功。另一个普通用户完成登录、当前用户读取和真实持久发帖。

## 4. 既有能力、Frontend 与指标目录

- 当前 Frontend role union、响应验证、共享 auth、导航与守卫已使用 super_admin；legacy admin DTO 明确拒绝。未搬迁 AdminLayout 或 Observability 页面。
- 真实授权回归访问了 Metrics catalog、六插件 catalog（6 项）、各插件查询（未安装的固定 404 与普通用户 403）、真实 VictoriaMetrics 空序列查询、使用已交付 mapping 的 Elasticsearch Logs/Events 空别名查询。没有重做 Phase 14 exporter 安装/摄取/故障矩阵。
- 本次 Backend 从当前源码构建；六插件 API 依赖使用强归属 **`gopulse/monitor:1.11.5` 已有镜像**，不是无可信 release catalog 的源码裸二进制。该依赖需本地可用，脚本 `pull_policy: never`；不覆盖镜像、不将旧镜像标成 1.12.1。这是角色边界回归，不是声称重跑完整当前 Monitor/六组件产品验收。
- 三条新增模板进入 `componentmetrics.BackendRoutes()`，只使用注册模板、不引入原始 ID/path 标签。47 个 method/route + 10 unmatched、5 status class、两组 HTTP family + 7 gauge，总上限 **577**。
- 直接组件测试发现旧固定断言为 547，已更新为 577，并同步当前 `docs/component-metrics.md`。旧 Phase 14 开发记录及固定旧版本验收脚本保留其历史预算，未伪改历史证据。

## 5. 实际验证命令与结果

| 命令 | 实际结果 |
| --- | --- |
| `git fetch origin` / `git fetch upstream`（命令级清空失效代理） | 成功；未修改全局 Git/代理配置 |
| `(cd backend && go test ./internal/user ./internal/http/middleware ./internal/http ./migrations ./cmd/admin-role ./cmd/migrate ./cmd/server ./internal/auth)` | 8 个 package 全部通过；包含方案 5 个 package，额外范围仅直接修改的迁移 runner/启动/auth 接线 |
| `(cd componentmetrics && go test ./...)` | 通过；扩展原因是新增公共 route tuple 改变共享指标目录预算 |
| `(cd frontend && npm test)` | 18 个 test files / 75 tests 全部通过 |
| `(cd frontend && npm run typecheck)` | 通过 |
| `(cd frontend && npm run build)` | 通过；1.12.1 构建 91 modules；包含工程自带 typecheck |
| `bash scripts/verify-role-management.sh --self-test` | 通过；不访问 Docker |
| `bash scripts/verify-role-management.sh` | 真实 MySQL/Backend 完整闭环通过；包括 fixture、DDL 恢复、旧 Cookie、角色矩阵、原子失败、审计、社交回归与自有进程/容器/volume 清理 |
| `bash -n scripts/verify-role-management.sh scripts/verify-compose-observability.sh scripts/verify-observability-ui.sh` | 通过 |
| `python3 -m py_compile scripts/ci/verify_role_management.py` | 通过 |
| `python3 scripts/ci/validate_versions.py` | 通过；VERSION、Frontend package/lock、.env.example 均为 1.12.1 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.12.1 --base-ref upstream/main` | 通过 |

实现过程中先跑直接 package/边界检查；最终成功的验证在相关源码、依赖和环境未变后未重复。Frontend 单测/typecheck 后只更新版本元数据，build 按工程脚本自带再次 typecheck。

实际修正过的失败：旧 ParseRole 测试仍接受 admin；验收环境缺少组件 metrics token；Monitor 源码二进制按设计因没有可信 release catalog 拒绝启动，改用已有六插件镜像并补齐其 Redis 配置；指标预算断言仍为 547。最终上述失败均已消除。审计故障/DDL 失败属于预期注入，不作为产品失败隐藏。

## 6. 偏差、限制与后续交接

- 真实 MySQL/HTTP 闭环集中在 `scripts/ci/verify_role_management.py`，由唯一 Bash 入口调用，不再复制同一业务矩阵到另一组 Go integration tests。脚本使用随机 project、schema、端口、凭据，清理前验证 Compose project 标签，只停止自身 Popen 子进程。
- 修改过的旧 Go `integration` fixture 角色语义已同步；本批固定 `go test` 未启用其 build tag，不将这些历史 tagged tests 记为执行过。真实证据以新的闭环脚本为准。
- 当前 Frontend 使用单元测试、typecheck 与生产构建验收。旧完整浏览器 Observability 脚本只更新直接角色调用并做语法检查；未把它或 Phase 14 全量 Compose 当成本批额外门禁。
- Phase-15-02 可复用 `RequireSuperAdmin`、每请求 DB 读取、唯一 bootstrap、多超级管理员角色事务、受限 audit builder/repository/query 及已升级 auth DTO。规则/告警审计需其实际业务变更发生后再接入；独立管理 Frontend 仍未实施。
- MySQL deadlock/数据库故障仍按安全失败返回，事务保证不提交部分角色/成功审计；不新增通用重试/后台补偿机制。
- 审计无自动归档/清理；受控 down 会删除新审计表，运维说明明确备份要求。

## 7. 实际文件

- `.env.example`
- `README.md`
- `VERSION`
- `backend/cmd/admin-role/integration_test.go`
- `backend/cmd/admin-role/main.go`
- `backend/cmd/admin-role/main_test.go`
- `backend/cmd/migrate/main.go`
- `backend/cmd/server/main.go`
- `backend/internal/apperror/error.go`
- `backend/internal/auth/service_test.go`
- `backend/internal/http/api.go`
- `backend/internal/http/auth_integration_test.go`
- `backend/internal/http/management_handler.go`
- `backend/internal/http/management_handler_test.go`
- `backend/internal/http/middleware/authorization.go`
- `backend/internal/http/middleware/authorization_test.go`
- `backend/internal/http/response/response.go`
- `backend/internal/user/audit.go`
- `backend/internal/user/management.go`
- `backend/internal/user/model.go`
- `backend/internal/user/model_test.go`
- `backend/internal/user/repository.go`
- `backend/migrations/000012_super_admin.down.sql`
- `backend/migrations/000012_super_admin.up.sql`
- `componentmetrics/registry_test.go`
- `componentmetrics/routes.go`
- `deploy/compose.yaml`
- `dev/imple/Phase-15/Phase-15-01-超级管理员迁移与角色审计闭环.md`
- `dev/imple/Phase-15/Phase-15-总实施方案.md`
- `dev/logs/Phase-15/Phase-15-01-超级管理员迁移与角色审计闭环.md`
- `docs/component-metrics.md`
- `frontend/e2e/observability.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `frontend/src/components/AppNav.vue`
- `frontend/src/components/UserAppShell.vue`
- `frontend/src/composables/useAuth.test.ts`
- `frontend/src/composables/useAuth.ts`
- `frontend/src/router/index.test.ts`
- `frontend/src/router/index.ts`
- `frontend/src/services/api.ts`
- `frontend/src/types/api.ts`
- `scripts/ci/verify_role_management.py`
- `scripts/verify-compose-observability.sh`
- `scripts/verify-observability-ui.sh`
- `scripts/verify-role-management.sh`

## 8. 提交检查

- 已对本批工作树和显式暂存文件执行 `git diff --check`、`git diff --cached --check`，均通过；仅暂存本批 46 个文件。
- 提交后的 `git diff --check upstream/main...HEAD` 与远程推送结果由最终交付回执记录，不预写未执行结果。
