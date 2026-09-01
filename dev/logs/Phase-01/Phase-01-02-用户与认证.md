# Phase 1-02：用户与认证实施记录

> 对应方案：`dev/imple/Phase-01/Phase-01-02-用户与认证.md`
> 目标版本：`0.2.2`
> 开发分支：`develop/0.2.2`
> 记录日期：2026-09-01

## 1. 实际完成内容

### 1.1 Phase-01-01 遗留整改

- 将 readiness 检查收敛为每个依赖最多一个在途 checker；即使 checker 永久阻塞并忽略 context，连续 `/ready` 请求也不会线性增加后台 checker goroutine，释放阻塞后可恢复正常检查。
- 在独立 checker 边界恢复 panic，并稳定映射为该依赖 `down` 与 HTTP 503，不向响应暴露 panic、连接串或凭据。
- 新增可测试的 HTTP Server 构造函数，设置 `ReadHeaderTimeout=5s`、`ReadTimeout=10s`、`WriteTimeout=15s`、`IdleTimeout=60s`、`MaxHeaderBytes=1 MiB`，保留 5 秒优雅关闭。
- 对 `APP_ENV` 实施规范化和白名单校验，并在 Router 创建前映射 Gin mode：`development` 对应 debug、`test` 对应 test、`production` 对应 release；其他值启动失败。生产环境继续强制 Secure Cookie。
- Vite 通过 `loadEnv` 读取仓库根环境配置；`/health`、`/ready`、`/api/v1` 共用由 `HTTP_PORT` 生成的 Backend target。`scripts/dev.sh` 启动 Frontend 时显式传入解析后的 `HTTP_PORT`。
- 在 GitHub Actions 中增加隔离 MySQL/Redis integration job：从空 `gopulse_integration` 库执行向上迁移，再运行 `go test -count=1 -tags=integration ./...`；测试环境使用显式开关和数据库、用户、Redis DB、loopback host 白名单，依赖缺失时失败而不是 skip。
- README 更新到 `0.2.2`，同步 WSL2/Bash 主开发环境、迁移、动态端口、认证 API、CI 和当前限制。

### 1.2 用户与认证能力

- 新增 User 领域模型、公开 DTO、用户名/密码输入边界和 MySQL Repository。
- 用户名去除首尾空白后按 `[A-Za-z0-9_]{3,32}` 校验；大小写唯一性由现有 MySQL case-insensitive 唯一约束作为最终边界，MySQL 1062 映射为稳定的 `username_conflict`。
- 密码保持原始字节，不去除空白；要求至少 8 个 Unicode 字符且不超过 bcrypt 的 72 字节边界。
- 使用固定直接依赖 `golang.org/x/crypto v0.48.0` 的 bcrypt，采用库默认 cost；数据库只持久化 bcrypt 哈希。未知用户登录执行 dummy bcrypt 比较，降低用户枚举的工作量差异。
- 使用 Go 标准库实现最小 HS256 JWT：仅接受 `HS256`/`JWT` header，要求 `sub`、`iat`、`exp`，`sub` 必须是正十进制用户 ID，并验证签名、签发时间、有效期、篡改、算法替换和最大令牌长度；签发和验证时钟可注入测试。
- 新增 Cookie 管理：`HttpOnly`、`SameSite=Lax`、`Path=/`、空 Domain，`Max-Age`/`Expires` 与 JWT TTL 协调；退出写入同名同 Path 的过期 Cookie，且无 Cookie 时仍返回 204。
- 新增认证 Service、Handler 和中间件；中间件统一从指定 Cookie 验证 JWT，并通过请求 context 提供类型明确的 `uint64` 当前用户 ID。缺失、过期、篡改或非法令牌统一返回 `401 authentication_required`，不回显令牌或内部验证细节。
- 完成并注册以下端点：
  - `POST /api/v1/auth/register`
  - `POST /api/v1/auth/login`
  - `POST /api/v1/auth/logout`
  - `GET /api/v1/users/me`
- Server 组装 MySQL Repository、bcrypt、JWT、Cookie、认证 Service/Handler 和认证中间件；Redis 不参与注册、登录或鉴权数据路径。
- 根 `VERSION` 从 `0.2.1` 更新为 `0.2.2`。

## 2. 文件变更

- 工程、版本、文档与 CI：
  - `.github/workflows/quality-gates.yml`
  - `README.md`
  - `VERSION`
  - `backend/go.mod`
- Backend 启动、配置、路由和平台组装：
  - `backend/cmd/server/main.go`
  - `backend/cmd/server/main_test.go`
  - `backend/internal/config/config.go`
  - `backend/internal/config/config_test.go`
  - `backend/internal/http/api.go`
  - `backend/internal/http/router.go`
  - `backend/internal/http/router_test.go`
  - `backend/internal/http/router_auth_test.go`
  - `backend/internal/http/auth_integration_test.go`
  - `backend/internal/platform/mysql.go`
  - `backend/internal/platform/integration_test.go`
  - `backend/internal/integrationtest/environment.go`
- User 模块：
  - `backend/internal/user/model.go`
  - `backend/internal/user/repository.go`
  - `backend/internal/user/repository_test.go`
  - `backend/internal/user/validation.go`
  - `backend/internal/user/validation_test.go`
- Auth 模块：
  - `backend/internal/auth/cookie.go`
  - `backend/internal/auth/cookie_test.go`
  - `backend/internal/auth/handler.go`
  - `backend/internal/auth/integration_test.go`
  - `backend/internal/auth/password.go`
  - `backend/internal/auth/password_test.go`
  - `backend/internal/auth/service.go`
  - `backend/internal/auth/service_test.go`
  - `backend/internal/auth/token.go`
  - `backend/internal/auth/token_test.go`
- HTTP 认证中间件：
  - `backend/internal/http/middleware/authentication.go`
  - `backend/internal/http/middleware/authentication_test.go`
- Frontend 开发代理与测试：
  - `frontend/vite.config.ts`
  - `frontend/vite.config.test.ts`
- WSL/Bash 开发入口：
  - `scripts/dev.sh`
- 本实施记录：
  - `dev/logs/Phase-01/Phase-01-02-用户与认证.md`

## 3. 实际验证命令与结果

### 3.1 Backend 静态与回归检查

- `test -z "$(gofmt -l .)"`（工作目录：`backend`）
  - 结果：通过；全部 Go 文件符合 gofmt。
- `go test -count=1 ./...`（工作目录：`backend`）
  - 结果：通过；覆盖配置、readiness、HTTP Server、用户校验、Repository 错误映射、bcrypt、JWT、Cookie、认证 Service/Handler/中间件和既有 HTTP/迁移回归。
- `go vet ./...`（工作目录：`backend`）
  - 结果：通过。
- `go test -race -count=1 ./...`（工作目录：`backend`）
  - 结果：通过；readiness 并发边界及本批 Backend 单元/契约测试未发现数据竞争。

### 3.2 Frontend、治理、脚本与 Compose

- `npm test -- --run`（工作目录：`frontend`）
  - 结果：通过；3 个测试文件、18 个测试通过，包含非默认 `HTTP_PORT` 和三条代理路径回归。
- `npm run typecheck`（工作目录：`frontend`）
  - 结果：通过。
- `npm run build`（工作目录：`frontend`）
  - 结果：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`
  - 结果：通过；8 个治理测试通过。
- `python3 scripts/ci/validate_branch.py --branch develop/0.2.2 --base-ref origin/main`
  - 结果：通过；分支名、目标版本和相对 `origin/main` 的开发范围符合治理规则。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh`
  - 结果：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
  - 结果：通过。
- 使用 Python/PyYAML 对 `.github/workflows/quality-gates.yml` 执行 `yaml.safe_load`。
  - 结果：通过；Workflow YAML 可解析。

### 3.3 隔离真实依赖 integration 验收

- 使用独立 Compose project、临时 volume 和宿主端口启动 MySQL 8.4 与 Redis 7.2；配置为：
  - `INTEGRATION_TESTS=1`
  - `APP_ENV=test`
  - `MYSQL_DATABASE=gopulse_integration`
  - `MYSQL_USER=gopulse_integration`
  - `REDIS_DB=15`
  - MySQL/Redis host 均为 `127.0.0.1`
- `go run ./cmd/migrate up`（工作目录：`backend`）
  - 结果：通过；隔离空库完成向上迁移。
- `go test -count=1 -tags=integration ./...`（工作目录：`backend`）
  - 结果：通过；真实 MySQL 验证注册、bcrypt 持久化、大小写用户名冲突、正确/错误密码登录、Cookie/current-user/logout 契约，以及关闭并重建数据库池和 Router 后的持久化登录；真实 Redis 可用性检查通过。
- 停止隔离 Redis 后执行 `go test -count=1 -tags=integration ./internal/platform`。
  - 结果：按预期失败并报告 Redis connection refused，确认依赖缺失不会被 skip 或形成假绿。
- 验收结束后已清理本批创建的临时容器、网络和 volume；未修改现有 `.env`、开发 Compose project 或用户数据。

### 3.4 真实 Backend 进程端到端 smoke

- 使用动态空闲端口和独立 Compose project 启动真实 MySQL/Redis，执行迁移，构建并启动 `backend/cmd/server` 二进制。
- 通过真实 HTTP 与 Cookie jar 依次验证：注册返回 201、Cookie 访问 `/users/me` 返回 200、退出返回 204、退出后 `/users/me` 返回 `401 authentication_required`、终止 Backend、重新启动 Backend、使用不同大小写用户名和原密码登录返回 200。
  - 结果：通过；最终输出为 `PASS: real Backend process register/me/logout/401/restart/login E2E succeeded`。
- 首次固定端口 smoke 在启动应用前因宿主端口 `43306` 已占用而失败；改为动态空闲端口后消除环境冲突。随后一次测试脚本误判 curl cookie jar 的 `#HttpOnly_` 记录格式；修正测试断言后同一产品流程通过。两次均为验收环境/脚本问题，不是应用契约失败，且临时资源均已清理。

## 4. 偏差与原因

- 方案允许 JWT 使用满足同等边界的实现；本批没有引入第三方 JWT 包，而是使用 Go 标准库实现最小 HS256 编解码、HMAC 校验和声明验证，以减少依赖和算法面。所有方案要求的算法替换、缺失声明、非法 subject、篡改和过期测试均已覆盖。
- GitHub Actions integration job 已在仓库中定义并通过本地 YAML 解析；其同等迁移和 integration 命令已在本地隔离 MySQL/Redis 环境通过，但 GitHub-hosted runner 上的实际 job 需在分支推送或 PR 后由远程 CI 执行。
- 未把完整 `scripts/dev.sh` 长生命周期作为单一命令运行；实际执行了 Bash 语法、Compose 渲染、动态 Vite 配置测试、隔离依赖 integration 和真实 Backend 进程 E2E。未将未执行的完整 Frontend/Backend 开发会话写为已通过。
- 按平台规则没有修改或验证原生 PowerShell 脚本；`scripts/*.ps1` 保持 `0.2.1` 能力基线冻结。

## 5. 已知限制与后续事项

- 本批只交付 Backend 用户与认证闭环；Frontend 注册、登录和当前用户界面留给 Phase-01-06。
- 当前认证使用短期无状态 JWT，不包含 refresh token、服务端会话、主动撤销列表或密钥轮换；退出依靠客户端清除 Cookie，已签发令牌在到期前仍可被持有该令牌的客户端使用。
- 用户名显示保留首次注册时的大小写，但查找和唯一性遵循 MySQL `utf8mb4_0900_ai_ci` 的大小写不敏感语义。
- Redis 不参与认证正确性；本批只验证 integration 依赖可用性。Redis 帖子详情缓存仍属于 Phase-01-05。
- RabbitMQ 不属于本批认证数据路径，未加入认证真实进程 smoke；既有 readiness 契约继续覆盖其检查边界。
- 后续 Phase-01-03 可直接复用认证中间件，通过 `middleware.CurrentUserID` 从请求 context 获取可信用户 ID，并通过 User Repository/Service 边界查询公开用户摘要。
