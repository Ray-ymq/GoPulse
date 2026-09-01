# Phase 1-02：用户与认证实施方案

> 执行序号：2 / 6
> 前置批次：Phase 1-01 已合并；其实施记录列明的遗留整改由本批优先关闭
> 总方案来源：[Phase-01-总实施方案.md](Phase-01-总实施方案.md)

## 1. 批次目标

实现用户名密码注册、登录、退出和当前用户查询，建立 bcrypt 密码哈希、短期 JWT、HttpOnly Cookie 以及 Gin 认证中间件。

在认证实现前，本批必须先关闭 Phase-01-01 未完成的 readiness 生命周期与 panic 隔离、HTTP Server 完整资源边界、动态 Vite proxy、Gin mode 映射和隔离 integration CI。这些都是共享运行边界，不得继续延期或以修改 Phase-01-01 原始方案掩盖。

本批完成后，客户端能够建立、恢复和清除最小登录状态，后续帖子、评论和点赞端点可通过统一中间件获取可信用户 ID。

## 2. 前置条件

- Phase 1-01 已合并到配置的主远程 `main`，根 `VERSION` 与前一批目标版本一致。
- 已从该远程最新 `main` 创建总方案为本批分配的开发分支。
- `users` 表、迁移命令和统一 HTTP 契约已可用。
- `AUTH_JWT_SECRET`、`AUTH_JWT_TTL`、`AUTH_COOKIE_NAME` 和 `AUTH_COOKIE_SECURE` 配置已经过校验。
- Phase 1-01 实施记录已补记实际遗漏和本批移交边界，未把未执行项写为已通过。
- WSL2 中的 Go、Node.js、npm、Bash 和 Docker Compose 可用，活动仓库位于 WSL Linux 文件系统。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 Phase-01-01 遗留整改

- 移除 readiness checker 当前无界嵌套 goroutine；异常 checker 忽略 context 时，连续请求的后台执行数量仍必须有界。
- 在 checker 隔离边界恢复 panic，将对应依赖稳定映射为 `down`，且不终止 Backend 或泄漏敏感配置。
- 把 HTTP Server 创建收敛到可测试构造函数，设置 `ReadHeaderTimeout=5s`、`ReadTimeout=10s`、`WriteTimeout=15s`、`IdleTimeout=60s` 和 `MaxHeaderBytes=1 MiB`，保留现有 5 秒优雅关闭。
- 根据 `APP_ENV` 在 Router 创建前映射 Gin debug、test、release mode，并测试三个合法值和非法配置。
- Vite 使用 `loadEnv` 或等价显式注入读取 `HTTP_PORT`，让 `/health`、`/ready` 和 `/api/v1` 使用同一 Backend target；增加非默认端口回归。
- GitHub Actions 在 Linux runner 上提供隔离 MySQL/Redis 服务，执行向上迁移并运行 `go test -count=1 -tags=integration ./...`；依赖缺失必须失败，不得 skip 后假绿。
- 更新 README，使当前版本、迁移、配置、WSL 开发入口和已知限制与 `0.2.1` 之后的实际行为一致。

### 3.2 User 模块

建立用户的领域模型、MySQL Repository、Service 和 HTTP Handler。

- Repository 提供按 ID 和规范化用户名查询、创建用户等最小操作。
- 用户名去除首尾空白后必须匹配 `[A-Za-z0-9_]{3,32}`。
- 唯一性以 MySQL 约束为最终边界，不使用“先查再写”替代唯一约束。
- 用户输出 DTO 只包含 `id`、`username`、`created_at`，不允许密码哈希进入通用 JSON 序列化路径。

### 3.3 密码处理

- 密码最少 8 个字符、最多 72 字节，不去除首尾空白。
- 使用固定版本的 `golang.org/x/crypto/bcrypt`，使用项目明确选定的 cost；未特别调优时使用库默认 cost。
- 哈希在写入 MySQL 前完成，哈希失败时不创建用户。
- 日志、错误、测试失败输出和 HTTP 响应都不包含明文密码或哈希。

### 3.4 JWT 与 Cookie

- JWT 使用 HS256，仅接受预期签名算法，拒绝 `none` 或算法替换。
- `sub` 是十进制用户 ID 字符串，并必须能解析为正整数。
- `iat` 和 `exp` 必须存在，有效期默认 2 小时，验证时使用可测试的时钟依赖。
- Cookie 设置 `HttpOnly`、`SameSite=Lax`、`Path=/`，`Max-Age` 与 JWT 有效期协调。
- `APP_ENV=production` 时强制 `Secure`，`development`/`test` 可为本地 HTTP 显式关闭；不设置宽泛 `Domain`。
- 退出使用同名、同 Path 的过期 Cookie，无 Cookie 时仍幂等返回 204。

### 3.5 认证中间件

- 从指定 Cookie 读取 JWT，统一验证签名、算法、声明和过期时间。
- 验证成功后将类型明确的用户 ID 写入请求 context，Handler 不再解析 JWT。
- 缺失、过期、篡改或非法 JWT 统一映射为 HTTP 401 和 `authentication_required`。
- 中间件错误不回显令牌、签名错误或用户 ID 解析细节。

### 3.6 API 契约

#### `POST /api/v1/auth/register`

请求：

```json
{"username":"alice","password":"example-password"}
```

- 创建用户并设置登录 Cookie，返回 HTTP 201 和用户 DTO。
- 用户名冲突返回 HTTP 409 和 `username_conflict`。
- 输入错误返回 HTTP 400 和 `validation_failed`。

#### `POST /api/v1/auth/login`

- 验证用户名密码，设置登录 Cookie，返回 HTTP 200 和用户 DTO。
- 用户不存在与密码错误都返回 HTTP 401 和 `invalid_credentials`。

#### `POST /api/v1/auth/logout`

- 匿名可调用，清除 Cookie 并返回 HTTP 204。

#### `GET /api/v1/users/me`

- 必须通过认证中间件，返回 HTTP 200 和当前用户 DTO。
- JWT 中用户已不存在时清除 Cookie 并返回 401，不伪造用户数据。

## 4. 明确不做的内容

- 不实现邮箱、手机号、用户资料或头像。
- 不实现刷新令牌、JWT 黑名单、Redis 会话、多设备会话或找回密码。
- 不将 JWT 返回到 JSON 响应，不让 Frontend 写入 Web Storage。
- 不实现 OAuth、角色、管理员、细粒度权限或限流。
- 不实现 Frontend 注册登录页，该工作属于 Phase 1-06。
- 不实现帖子、评论或点赞业务。

## 5. 目标文件和目录

```text
backend/internal/auth/
backend/internal/user/
backend/internal/http/middleware/
backend/internal/http/
backend/cmd/server/
backend/go.mod
backend/go.sum
frontend/vite.config.ts
.github/workflows/quality-gates.yml
README.md
VERSION
dev/logs/Phase-01/Phase-01-02-用户与认证.md
```

## 6. 详细实施步骤

1. 检查 Phase 1-01 交接物、补记后的实施记录和 Git 状态。
2. 重构 readiness checker 执行边界，增加忽略 context、连续超时、释放收敛和 panic 隔离测试。
3. 建立可测试的 HTTP Server 构造函数，补齐全部超时与请求头限制及精确断言。
4. 完成 `APP_ENV` 到 Gin mode 的映射，并让 Vite proxy 从同一配置来源读取 `HTTP_PORT` 和代理 `/api/v1`。
5. 建立 Linux integration job 的隔离 MySQL/Redis、迁移、目标白名单和依赖缺失失败语义。
6. 更新 README 的当前版本、WSL 环境、迁移、配置和端口说明。
7. 定义 User 领域模型、输入模型和公开输出 DTO。
8. 实现用户名与密码输入校验，为字符长度和字节长度编写边界测试。
9. 实现 User MySQL Repository，将唯一约束错误转换为稳定领域错误。
10. 实现 bcrypt 哈希和验证边界。
11. 实现 JWT 签发与验证，使用可注入时钟测试过期语义。
12. 实现 Cookie 写入、清除和环境安全属性。
13. 实现注册、登录、退出和当前用户 Service/Handler。
14. 实现认证中间件和类型安全的当前用户 ID 获取边界。
15. 将端点注册到 `/api/v1`，确保匿名与受保护路由分组正确。
16. 使用真实 MySQL 验证注册持久化、大小写冲突、登录和 Backend 重启。
17. 运行 Backend 全部测试、vet、race、integration 测试、Frontend 回归、Bash 语法和 Compose 配置检查。
18. 将根 `VERSION` 更新为总方案为本批分配的目标版本，并创建本批实施记录，如实区分遗留整改与认证实现结果。

## 7. 测试与验收标准

### 7.1 Phase-01-01 遗留整改验收

- 永久阻塞且忽略 context 的 checker 在连续请求后不会造成 goroutine 或后台检查数量线性增长，释放后能够收敛。
- checker panic 不终止 Backend；对应依赖返回既有 `down`/503 契约，日志不包含连接串或凭据。
- HTTP Server 构造测试精确覆盖 ReadHeader、Read、Write、Idle 超时和 `MaxHeaderBytes`，5 秒优雅关闭回归通过。
- `APP_ENV` 三个合法值映射到预期 Gin mode，非法值在启动前失败。
- 非默认 `HTTP_PORT` 下，Backend、Vite 的 `/health`、`/ready`、`/api/v1` proxy 和 Bash `verify.sh` 使用一致端口。
- Linux CI 的 integration job 从空隔离库执行迁移并运行真实 MySQL/Redis 测试；依赖缺失负向场景返回失败，不得静默 skip。

### 7.2 单元测试

- 用户名最小/最大长度、非法字符、空白和大小写输入正确。
- 密码 8 字符、72 字节、Unicode 字节边界和超限情况正确。
- bcrypt 哈希不等于明文，正确密码通过，错误密码失败。
- JWT 正常、过期、篡改、错误算法、缺失声明和非法 `sub` 均被精确处理。
- Cookie 的 HttpOnly、SameSite、Secure、Path、Max-Age 和清除属性正确。
- `APP_ENV=production` 且显式关闭 Secure Cookie 时配置拒绝启动，development/test 的本地 HTTP 行为与 Phase-01-01 契约一致。
- 注册、登录、退出和当前用户的状态码与 JSON 契约正确。
- 用户不存在和密码错误的对外响应不可区分。
- 响应、错误和日志不包含明文密码、密码哈希、JWT 或签名密钥。

### 7.3 真实数据库验收

1. 注册新用户返回 201，MySQL 仅保存 bcrypt 哈希。
2. 使用不同大小写重复注册返回 409，只存在一条用户。
3. 注册成功返回的 Cookie 可访问 `/api/v1/users/me`。
4. 退出后访问受保护端点返回 401。
5. 正确凭据重新登录成功，错误凭据返回统一 401。
6. 重启 Backend 后用户数据仍存在，可重新登录。

### 7.4 工程检查

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go test -race ./...` 通过。
- `go test -count=1 -tags=integration ./...` 在本批建立的隔离 MySQL/Redis 环境通过，且依赖缺失不得静默 skip。
- Frontend 测试、类型检查和生产构建通过；Bash 生命周期脚本语法与 Compose 配置检查通过。
- Phase 0 `/health` 和 `/ready` 回归通过。
- 不需要 Redis 即可完成注册、登录和认证。

## 8. 完成定义

- 四个认证/用户端点按契约可用。
- Phase-01-01 移交的 readiness、HTTP Server、Vite proxy、Gin mode、integration CI 和 README 缺口全部关闭，不再转交 Phase-01-03。
- 密码和 JWT 处理具有单元测试保护。
- 认证中间件可向后续业务 Handler 提供可信用户 ID。
- Backend 重启不丢失用户事实。
- WSL2/Bash 开发与验收入口通过；原生 PowerShell 脚本保持 `0.2.1` 冻结状态且不属于本批验收。
- 本批实施记录已创建，根 `VERSION` 已更新为总方案分配的本批目标版本，仅提交本批文件。

## 9. 下一批次交接条件

交付给 Phase 1-03 前必须提供：

- 可从请求 context 获取当前用户 ID 的受测试中间件。
- 可按 ID 获取用户公开摘要的服务或查询边界。
- 稳定的 `authentication_required`、`validation_failed` 和内部错误映射。
- 已验证的 Cookie 客户端调用方式。
