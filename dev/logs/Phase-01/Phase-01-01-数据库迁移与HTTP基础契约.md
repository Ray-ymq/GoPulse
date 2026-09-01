# Phase 1-01：数据库迁移与 HTTP 基础契约实施记录

> 对应方案：`dev/imple/Phase-01/Phase-01-01-数据库迁移与HTTP基础契约.md`
> 目标版本：`0.2.1`
> 开发分支：`develop/0.2.1`
> 记录日期：2026-09-01
> 记录修订：2026-09-01；在不改写既有完成事实的前提下，补记原方案未交付项及后续归属

## 1. 实际完成内容

- 在 Backend Go module 内新增内嵌 SQL 迁移能力与 `backend/cmd/migrate` 命令，支持 `up` 和显式 `down`。
- 新增 Phase 1 初始 Schema 迁移：按 `users → posts → comments → post_likes` 顺序建表，回滚顺序相反。
- Schema 使用 InnoDB、`utf8mb4_0900_ai_ci`、`DATETIME(6)`、显式 `RESTRICT` 外键、用户名唯一约束、帖子列表/评论列表/按用户点赞查询所需索引。
- 新增迁移专用 MySQL 连接配置：仅迁移连接启用 multi-statements，普通应用连接保持禁用；MySQL 连接统一按 UTC、`utf8mb4_0900_ai_ci` 和 `time_zone='+00:00'` 配置。
- 在 HTTP 层新增 `/api/v1` 组装边界、统一成功响应、分页响应、错误响应映射、严格 JSON 请求体解析和正整数路径 ID 解析工具。
- 在配置中新增 Phase 1 认证与 Redis 缓存参数校验：`AUTH_JWT_SECRET`、`AUTH_JWT_TTL`、`AUTH_COOKIE_NAME`、`AUTH_COOKIE_SECURE`、`REDIS_POST_DETAIL_TTL`、`REDIS_OPERATION_TIMEOUT`。
- 更新 `.env.example`、PowerShell 与 Bash 开发脚本，使开发脚本在 MySQL healthy 后、Backend/Frontend 启动前执行 `go run ./cmd/migrate up`，迁移失败时停止后续启动。
- 将根 `VERSION` 更新为 `0.2.1`。

## 2. 文件变更

- `.env.example`
- `VERSION`
- `backend/go.mod`
- `backend/go.sum`
- `backend/cmd/migrate/main.go`
- `backend/cmd/migrate/main_test.go`
- `backend/migrations/embed.go`
- `backend/migrations/embed_test.go`
- `backend/migrations/000001_phase1_schema.up.sql`
- `backend/migrations/000001_phase1_schema.down.sql`
- `backend/internal/apperror/error.go`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/internal/http/api.go`
- `backend/internal/http/router.go`
- `backend/internal/http/params/id.go`
- `backend/internal/http/params/id_test.go`
- `backend/internal/http/request/json.go`
- `backend/internal/http/request/json_test.go`
- `backend/internal/http/response/response.go`
- `backend/internal/http/response/response_test.go`
- `backend/internal/platform/mysql.go`
- `backend/internal/platform/platform_test.go`
- `scripts/dev.ps1`
- `scripts/dev.sh`
- `dev/imple/Phase-01/Phase-01-总实施方案.md`
- `dev/logs/Phase-01/Phase-01-01-数据库迁移与HTTP基础契约.md`

## 3. 实际验证命令与结果

- `git fetch origin --prune`
  - 结果：通过；确认当前工作在 `develop/0.2.1`，目标版本为 `0.2.1`。
- `gofmt -w ...`
  - 结果：通过；格式化本批新增和修改的 Go 文件。
- `go test ./...`（工作目录：`backend`）
  - 结果：通过；覆盖迁移命令参数、内嵌迁移内容、配置校验、HTTP 路由契约、严格 JSON、统一响应、路径参数和平台连接配置。
- `go vet ./...`（工作目录：`backend`）
  - 结果：通过。
- Docker Compose MySQL 临时库迁移验收：
  - 先启动 `deploy/compose.yaml` 中的 MySQL 服务并等待 healthy。
  - 交叉编译临时 Linux `migrate` 二进制，拷贝到 MySQL 容器内连接隔离库执行验证。
  - `migrate up`：通过。
  - 重复 `migrate up`：通过，返回 already up to date。
  - Schema 查询：四张业务表存在；四个关键索引存在；6 个 `DATETIME(6)` 字段存在。
  - 约束验证：用户名大小写冲突被拒绝；重复点赞被拒绝；非法外键写入被拒绝。
  - `migrate down`：通过；临时库中四张业务表剩余数量为 0。
  - 清理：临时数据库已删除；本次启动的 MySQL 容器已停止。

## 4. 偏差与原因

- 未对开发库 `gopulse` 执行 `down` 或清空数据；向下迁移仅在隔离临时库中验证，符合本批“不自动向下迁移或清空开发数据库”的边界。
- 本机 `.env` 中的宿主 MySQL 端口当前被非项目进程占用，导致宿主侧迁移 smoke 首次连接到非 MySQL 服务并超时；因此实际数据库验收改为在 Compose MySQL 容器内运行临时迁移二进制连接 `127.0.0.1:3306`。
- 实测发现单个 SQL 迁移文件包含多条 DDL 时，迁移连接必须启用 MySQL multi-statements；已改为仅迁移连接启用，普通应用连接不启用。
- 原方案要求本批关闭 readiness 无界嵌套 goroutine 与 checker panic 隔离，但本批提交只在现有 Router 中注册 `/api/v1`，未修改 readiness 执行边界或增加对应压力/panic 测试；该项未完成。
- 原方案要求建立可测试的 HTTP Server 构造函数并补齐 Read、Write、Idle 超时与 `MaxHeaderBytes`，本批未修改 `backend/cmd/server/main.go`；当前实现仍只有 `ReadHeaderTimeout`，该项未完成。
- 原方案要求让 Vite proxy、Backend、开发/验收脚本共享 `HTTP_PORT`，并完成 `APP_ENV` 到 Gin mode 的映射；本批未修改 `frontend/vite.config.ts`，Vite 仍固定代理到 `8080` 且未包含 `/api/v1`，Backend 也未设置 Gin mode，该项未完成。
- 原方案要求新增隔离 MySQL/Redis 的 GitHub Actions integration job，本批未修改 `.github/workflows/quality-gates.yml`，也未提交 `integration` build-tag 测试，该项未完成。
- 原方案要求同步更新 README 的 `.env` 升级、迁移、非默认端口和运行环境说明，本批提交未包含 README；后续在 `update` 规划/文档提交中补齐当前状态与 WSL 使用说明。
- 本记录的实际验证命令不包含 PowerShell/Bash 语法检查或两平台完整启动闭环，因此只能确认两套 `dev` 脚本已同步修改，不能把未记录的平台运行视为已通过。

## 5. 已知限制与后续事项

- 本批只建立迁移、Schema 与 HTTP 基础契约，不包含注册、登录、认证中间件、帖子/评论/点赞业务 Handler、Service 或 Repository。
- Redis 缓存参数只完成配置校验；实际帖子详情缓存读写留给 Phase 1-05。
- 已存在的本地 `.env` 不会被脚本自动覆盖；若开发者已有旧 `.env`，需要按 `.env.example` 补齐 `AUTH_JWT_SECRET` 等 Phase 1 参数后再运行开发脚本。
- readiness、HTTP Server、动态 Vite proxy、Gin mode 和隔离 integration CI 遗留项已明确转交 Phase-01-02，并要求在认证实现前关闭，不再继续延期。
- 原生 PowerShell 开发入口的共同能力冻结在本批完成版本 `0.2.1`；Phase-01-02 至 Phase 16 只维护 WSL2/Bash 入口，最终 Windows PowerShell 兼容在 Phase 16 后集中处理。
