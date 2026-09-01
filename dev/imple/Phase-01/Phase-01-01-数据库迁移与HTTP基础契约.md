# Phase 1-01：数据库迁移与 HTTP 基础契约实施方案

> 执行序号：1 / 6
> 前置批次：Phase 0 已完成并通过验收
> 总方案来源：[Phase-01-总实施方案.md](Phase-01-总实施方案.md)

## 1. 批次目标

为 Phase 1 建立稳定的持久化和 HTTP 契约基线：通过可版本化迁移创建 User、Post、Comment、Like 四类核心业务表，并在现有 Gin Backend 中建立 `/api/v1` 路由组、严格 JSON 输入、统一响应和错误映射。

本批同时关闭 Phase-00-06 明确移交的三组 Review 整改：readiness goroutine 生命周期与 panic 隔离、HTTP Server 资源边界，以及 `HTTP_PORT`/Vite proxy/`APP_ENV` 端到端配置契约。整改不得改变 Phase 0 `/health`、`/ready` 的对外路径和响应语义。

本批不实现可对外使用的业务端点。完成后，后续批次可以在不重复定义 Schema、响应结构和配置规则的情况下实现业务。

## 2. 前置条件

- 在 Windows 开发环境执行本批实施。
- Phase 0 Backend、MySQL、Redis 和开发脚本已经可用。
- `/health` 和 `/ready` 现有测试全部通过。
- 根 `VERSION` 为 Phase 0 收口版本 `0.1.6`，Phase 0 全部六份实施记录与验收结果已核对。
- Phase-00-06 记录第 6 节的三个 Phase-01-01 后续项已加入本批范围，不再继续延期。
- 开始前获取配置的主远程最新状态，从该远程 `main` 创建总方案为本批分配的开发分支。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 迁移入口

新增：

```text
backend/cmd/migrate/
backend/migrations/
```

- 使用 Backend Go module 中固定版本的迁移库，不要求开发者全局安装 CLI。
- `migrate up` 执行尚未应用的向上迁移，重复执行幂等成功。
- `migrate down` 只允许开发者显式调用，开发启动和验收脚本不自动执行向下迁移。
- 命令复用 Phase 0 MySQL 配置和敏感信息保护，迁移错误不输出密码或完整 DSN。
- Backend Server 本身不在启动时隐式执行迁移。

### 3.2 Schema

使用 InnoDB、`utf8mb4` 和明确的不区分大小写 collation，按总方案创建：

```text
users
posts
comments
post_likes
```

建表顺序为 `users → posts → comments → post_likes`，回滚顺序相反。约束至少包括：

- `users.username` 唯一，不区分 ASCII 大小写。
- `posts.author_id` 关联 `users.id`。
- `comments.post_id` 关联 `posts.id`，`comments.author_id` 关联 `users.id`。
- `post_likes(post_id, user_id)` 作为联合主键，并分别关联帖子和用户。
- 外键的删除行为使用明确的 `RESTRICT`，不预先引入级联删除。
- 时间字段使用 `DATETIME(6)`，Backend 数据库连接与应用统一按 UTC 处理。
- 建立帖子列表、评论列表和按用户查询点赞所需索引。

迁移可以使用一个有明确序号的 Phase 1 初始 Schema 迁移，也可按业务表拆成多个连续迁移；必须保证从空库和已应用状态运行时的结果可重复。

### 3.3 HTTP 基础契约

在 Phase 0 Router 中增加 `/api/v1` 路由组和以下通用能力：

- 成功响应 `{"data": ...}`。
- 分页响应中的 `meta.next_cursor`。
- 错误响应 `{"error":{"code":"...","message":"..."}}`。
- 业务错误到 HTTP 状态码的集中映射。
- 严格 JSON 解析：拒绝未知字段、空请求体、多个 JSON 值和超限请求体。
- 业务 JSON 请求体默认上限设为 64 KiB，后续媒体上传不复用该上限。
- 正整数路径 ID 解析和非法参数响应。

本批可建立空路由组装入口，但不注册返回虚假成功的业务端点。`/health` 和 `/ready` 路径、状态码和响应保持不变。

### 3.4 Phase 0 Review 移交整改

#### 3.4.1 Readiness 生命周期与 panic 隔离

- 保持 `/ready` 的路径、HTTP 200/503、`status`、`service` 和三项依赖状态字段不变。
- 明确 `Checker.Check` 必须遵守传入 context，并移除当前每个 checker 的无界嵌套 goroutine。
- 对忽略 context 的异常 checker 使用有界并发、共享执行或等价隔离，使连续超时请求不会造成 goroutine 数量线性增长。
- checker panic 在隔离边界恢复并映射为该依赖 `down`；日志只记录依赖名称和安全诊断，不输出连接串或凭据。
- 增加确定性压力测试：永久阻塞 checker 在连续请求后保持并发/后台执行数量有界，释放测试 checker 后可正常收敛；panic checker 不终止测试进程。

#### 3.4.2 HTTP Server 资源边界

- 把 `http.Server` 创建移到可测试的构造函数，避免只在 `main` 中硬编码。
- 普通 JSON API 使用 `ReadHeaderTimeout=5s`、`ReadTimeout=10s`、`WriteTimeout=15s`、`IdleTimeout=60s` 和 `MaxHeaderBytes=1 MiB`。
- 保留现有 5 秒优雅关闭边界；未来流式接口必须单独评估，不得移除普通 API 的全局限制。
- 构造测试精确断言上述值，并保留监听地址来自配置的行为。

#### 3.4.3 `HTTP_PORT`、Vite proxy 与 `APP_ENV`

- Vite 使用 `loadEnv` 或等价的显式注入读取与 Backend、`dev`、`verify` 相同来源的 `HTTP_PORT`，为 `/health`、`/ready` 和 `/api/v1` 使用同一 Backend target。
- 增加非默认 `HTTP_PORT` 的端到端测试，验证 Backend、Frontend proxy 和验收脚本同时工作；不再要求标准流程固定使用 `8080`。
- `APP_ENV` 只接受 `development`、`test`、`production`，分别在 Router 创建前映射到 Gin debug、test、release mode。
- `APP_ENV=production` 强制 `AUTH_COOKIE_SECURE=true`；环境名称和 Gin mode 可进入启动日志，敏感配置不得输出。

### 3.5 配置、现有 `.env` 升级与开发脚本

在 `.env.example` 和 Backend 配置中增加总方案定义的认证与缓存参数。

- `AUTH_JWT_SECRET` 必填，最少 32 字节。
- 持续时间字段必须可解析、为正值且不超过明确上限。
- 本地开发示例密钥必须明确标记不得用于其他环境。
- 开发脚本在 MySQL healthy 之后、Backend 启动之前执行向上迁移。
- 迁移失败时不继续启动 Backend 或 Frontend。
- Phase 0 已存在的 `.env` 不得自动覆盖；如缺少新增必填键，PowerShell/Bash 脚本必须列出缺失键并提示开发者从 `.env.example` 手工补充。
- `cmd/migrate` 使用只包含 MySQL 的命令级配置加载边界，不因 JWT、Cookie、Redis 或 RabbitMQ 配置缺失而失败。
- 本批同步更新 README，记录 Phase 0 工作区升级 `.env`、迁移命令、非默认 HTTP 端口和 `APP_ENV` 行为，不把可启动说明延迟到 Phase 1-06。

### 3.6 真实基础设施测试与 CI

- 默认 `go test -count=1 ./...` 只运行不依赖外部服务的单元、Handler 和 HTTP 契约测试。
- 真实迁移、MySQL/Redis Repository 测试使用 `go test -count=1 -tags=integration ./...`；进入 integration 模式后依赖不可用必须失败，不得 skip 后返回成功。
- GitHub Actions 新增隔离 integration job，启动 MySQL/Redis 服务，从空的专用数据库执行迁移后运行 integration 测试。
- 数据库名、Redis DB 和相关清理目标必须经过严格校验；CI 和本地测试不得清空开发者数据库或 Redis DB。

## 4. 明确不做的内容

- 不实现注册、登录或认证中间件。
- 不实现帖子、评论或点赞 Handler、Service 和 Repository。
- 不向数据库写入演示用户或业务种子数据。
- 不实现 Redis 缓存键读写。
- 不修改 Phase 0 readiness 的对外路径、状态码和 JSON 判定语义；内部生命周期与 panic 隔离按 3.4.1 修复。
- 不执行开发数据库的自动向下迁移或数据清空。

## 5. 目标文件和目录

```text
backend/cmd/migrate/
backend/cmd/server/
backend/migrations/
backend/internal/config/
backend/internal/http/
backend/go.mod
backend/go.sum
.env.example
frontend/vite.config.ts
scripts/dev.ps1
scripts/dev.sh
.github/workflows/quality-gates.yml
README.md
VERSION
dev/logs/Phase-01/Phase-01-01-数据库迁移与HTTP基础契约.md
```

实际路径应与 Phase 0 已交付结构协调，不为了机械匹配预估树而移动已稳定的 Phase 0 代码。

## 6. 详细实施步骤

1. 检查 Phase 0 完成记录、当前版本、远端状态和工作树。
2. 核对总方案的本批版本与分支分配，从配置的主远程最新 `main` 创建对应开发分支。
3. 选择并固定 Go 迁移依赖，建立 `cmd/migrate` 入口与参数错误处理。
4. 编写向上和向下 SQL 迁移，按依赖顺序创建/删除四张表。
5. 实现数据库时区、字符集、collation、外键、唯一约束和索引。
6. 建立 HTTP 成功/错误模型、集中错误映射和严格 JSON 解析工具。
7. 建立 `/api/v1` 组装边界，保证 Phase 0 路由契约不变。
8. 重构 readiness 执行边界，增加有界阻塞 checker、panic 和并发压力测试。
9. 建立可测试的 HTTP Server 构造函数和完整资源边界。
10. 校验并映射 `APP_ENV`，让 Vite proxy、Backend、开发脚本和验收脚本共享 `HTTP_PORT` 来源。
11. 增加 Phase 1 配置项及字段级校验，保持 `cmd/migrate` 只依赖 MySQL 配置。
12. 更新 PowerShell 与 Bash 开发脚本，以相同顺序显式执行向上迁移；验证已有 `.env` 的缺键提示且不覆盖文件。
13. 更新 README 的 `.env` 升级、迁移、端口和运行环境说明。
14. 从空测试库执行迁移、重复执行向上迁移，并在临时隔离库验证向下迁移。
15. 更新质量门禁，使用隔离 MySQL/Redis 运行不得静默跳过的 integration 测试。
16. 运行 Phase 0 全部 Backend/Frontend 测试、脚本检查、非默认端口联调和就绪接口回归。
17. 将根 `VERSION` 更新为总方案为本批分配的目标版本。
18. 创建对应实施记录，只记载实际命令、结果和偏差。

## 7. 测试与验收标准

### 7.1 迁移

- 空数据库执行 `up` 后存在四张表及预期字段。
- 重复执行 `up` 不失败也不重建已应用迁移。
- 专用临时库执行 `down` 后业务表按正确顺序移除。
- 用户名大小写冲突、重复点赞、非法外键写入均被数据库拒绝。
- 查询 Schema 可确认列表和点赞访问所需索引存在。

### 7.2 HTTP 基础

- 成功、分页和错误响应精确匹配总方案。
- 未知字段、第二个 JSON 值、空请求体和超限请求体被安全拒绝。
- 错误响应不包含 SQL、密码、JWT 密钥或连接串。
- `/health` 仍返回 200，`/ready` 仍正确检查 MySQL、Redis、RabbitMQ。
- 永久阻塞 checker 的连续请求不会造成 goroutine 或后台检查数量线性增长。
- checker panic 不终止 Backend，`/ready` 将对应依赖标记为 `down` 并返回既有 503 契约。
- HTTP Server 构造测试精确覆盖 ReadHeader、Read、Write、Idle 超时与 MaxHeaderBytes。
- `APP_ENV` 三个合法值映射到预期 Gin mode，非法值和 production 下不安全 Cookie 配置导致启动失败。
- 非默认 `HTTP_PORT` 下 Backend、Vite proxy、`verify` 的 `/health`、`/ready` 和 `/api/v1` 路由一致可达。

### 7.3 工程检查

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go test -count=1 -tags=integration ./...` 在本地隔离库和 GitHub Actions integration job 中通过；依赖缺失负向测试返回失败。
- PowerShell 和 Bash 启动脚本的迁移顺序一致。
- 已有 Phase 0 `.env` 缺少新增键时两套脚本给出等价、可操作的错误且文件内容保持不变。
- 开发数据库未被向下迁移或清空。

## 8. 完成定义

- 可从空库建立全部 Phase 1 Schema。
- 约束、索引、UTC 时间和用户名匹配语义已验证。
- `/api/v1` 通用 HTTP 契约和严格输入边界有测试保护。
- Phase 0 健康与就绪对外契约未回归，readiness 生命周期、panic 隔离和 HTTP Server 资源边界已关闭 Phase-00-06 移交项。
- `HTTP_PORT`、Vite proxy、`APP_ENV` 与 Secure Cookie 形成受测试的端到端配置契约。
- Phase 0 已有 `.env` 有明确升级路径，迁移命令不依赖无关配置。
- 真实 MySQL/Redis integration 测试在 CI 强制执行且使用隔离资源。
- 对应实施记录已创建。
- 根 `VERSION` 已更新为总方案分配的本批目标版本，仅提交本批变更。

## 9. 下一批次交接条件

交付给 Phase 1-02 前必须提供：

- 可用的 `users` 表及用户名唯一约束。
- 稳定的迁移命令与开发脚本调用方式。
- 统一 JSON 解析、成功/错误响应和错误映射入口。
- 已校验的 JWT 和 Cookie 配置字段。
- 已关闭并有回归测试保护的 Phase 0 Review 三组移交整改。
- 可供后续 Repository 测试复用的隔离 integration job 和命令。

Phase 1-02 不应修改迁移历史来规避认证实现问题；必须修正已提交迁移时，按迁移兼容性新增后续迁移。
