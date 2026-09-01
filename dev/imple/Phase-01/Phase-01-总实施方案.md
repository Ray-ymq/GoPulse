# Phase 1：最小业务闭环总实施方案

## 1. 实施目标

在 Phase 0 工程骨架上完成 GoPulse 的最小社交业务闭环，使用户能够完成：

```text
注册
→ 登录
→ 发布帖子
→ 查看帖子列表与详情
→ 发表评论
→ 点赞或取消点赞
```

本阶段延续单体 Backend 形态，在单体内按 User、Post、Comment、Like 业务边界进行模块化，不拆分微服务。MySQL 保存全部核心业务事实，Redis 只缓存可从 MySQL 重建的帖子详情投影。

本阶段完成后，应得到可独立启动、可自动化测试、可通过真实 MySQL 和 Redis 验收的最小业务系统，并为 Phase 2 的 RabbitMQ 业务异步化提供稳定的同步事实写入基础。

## 2. 前置条件与仓库约束

### 2.1 前置条件

- Phase 0 全部实施批次已完成并通过验收。
- Backend、Frontend、MySQL、Redis 和统一开发脚本已可用。
- Phase 0 的 `/health` 和 `/ready` 契约保持可用。
- 根目录存在有效的 `VERSION` 文件，或仓库存在可用的 SemVer Git 标签。
- Windows 开发环境可以运行 Go、Node.js、npm 和 Docker Compose。

Phase 0 未完成时不得跳过前置验收直接实施 Phase 1，也不得为了实现 Phase 1 而在业务批次中重建 Phase 0 工程骨架。

### 2.2 版本与分支

Phase 1 使用 `0.2.x` 版本线，`0.2.0` 保留为阶段基线，不对应可执行批次。下表是本阶段批次到版本与分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-01-01 | `0.2.1` | `develop/0.2.1` | 已完成 |
| Phase-01-02 | `0.2.2` | `develop/0.2.2` | 待实施 |
| Phase-01-03 | `0.2.3` | `develop/0.2.3` | 待实施 |
| Phase-01-04 | `0.2.4` | `develop/0.2.4` | 待实施 |
| Phase-01-05 | `0.2.5` | `develop/0.2.5` | 待实施 |
| Phase-01-06 | `0.2.6` | `develop/0.2.6` | 待实施 |

执行规则：

- Phase 0 完成时的 `0.1.5` 是 Phase-01-01 的输入基线；不需要先创建仅包含 `VERSION=0.2.0` 的空批次。
- 每个批次是独立开发任务。开始前获取配置的主远程最新状态，在前置批次已合并后从该远程 `main` 创建表中对应分支。
- 每批完成时将根 `VERSION` 更新为本批目标版本，与实施记录一起提交；不把六批变更累积到阶段末一次升版。
- 批次完成或已打开 Pull Request 后，不自动在该分支继续下一批；下一批必须使用自己的版本分支。
- 如批次数量或顺序在实施前调整，先更新本表并重算尚未创建的分支；已推送分支不得静默改名或重新编号。

## 3. 范围与边界

### 3.1 本阶段实现

- 用户名和密码注册、登录、退出和当前用户查询。
- 帖子发布、游标分页列表和详情查询。
- 评论发布和分页查询。
- 帖子点赞和取消点赞。
- MySQL Schema 迁移、索引、约束和业务持久化。
- Redis 帖子详情公共投影缓存与降级。
- Vue Router 业务页面、认证状态恢复和最小交互。
- Backend、Frontend 与真实基础设施的自动化测试和集成验收。

### 3.2 本阶段不实现

- 邮箱、手机号、验证码、找回密码、刷新令牌和多设备会话管理。
- 用户资料编辑、头像上传、关注、收藏、私信和通知。
- 帖子编辑、帖子删除、媒体上传、标签和富文本。
- 评论回复、评论编辑、评论删除和评论点赞。
- 热点排名、推荐算法、全文搜索和运营管理。
- RabbitMQ Producer、Consumer、通知异步任务和最终一致消息治理。
- Elasticsearch、Kafka、可观测系统、应用容器化和 Kubernetes。
- 管理员、角色、细粒度权限、限流、验证码和生产级安全防护。

## 4. 整体业务架构

### 4.1 请求链路

```text
Vue 3 Frontend
  → Vite 开发代理
Gin HTTP Router
  → 认证中间件
  → 业务 Handler
  → Service
  → Repository
  → MySQL
  ⇄ Redis（仅帖子详情公共投影缓存）
```

### 4.2 Backend 目标结构

在 Phase 0 Backend 结构上增加业务模块：

```text
backend/
├── cmd/
│   ├── server/
│   └── migrate/
├── internal/
│   ├── auth/
│   ├── user/
│   ├── post/
│   ├── comment/
│   ├── like/
│   ├── config/
│   ├── http/
│   │   ├── middleware/
│   │   └── response/
│   └── platform/
│       ├── mysql/
│       └── redis/
└── migrations/
```

每个业务模块内部根据实际需要包含：

```text
handler → service → repository
```

约束如下：

- Handler 只处理 HTTP 参数、身份上下文、输入校验和响应映射。
- Service 处理业务规则、幂等语义、权限和缓存协调。
- Repository 只处理 MySQL 或 Redis 访问，不依赖 Gin。
- 模块之间优先依赖精简接口或服务，不直接访问其他模块的私有 Repository。
- 不为每个结构机械地创建接口；只在测试替身、多实现或隔离边界需要时定义接口。

### 4.3 Frontend 目标结构

```text
frontend/src/
├── router/
├── views/
│   ├── RegisterView.vue
│   ├── LoginView.vue
│   ├── PostListView.vue
│   ├── PostCreateView.vue
│   └── PostDetailView.vue
├── components/
├── composables/
│   └── useAuth.ts
├── services/
└── types/
```

本阶段引入 Vue Router，不引入 Pinia。当前用户状态由轻量 `useAuth` composable 管理，页面刷新时通过当前用户接口恢复认证状态。

## 5. 数据库设计

### 5.1 迁移管理

- 使用提交到仓库的双向 SQL 迁移文件管理 Schema。
- 通过 `backend/cmd/migrate` 提供可版本化的 `up` 和 `down` 入口，不依赖未固定的全局 CLI。
- Backend Server 启动时不自动执行破坏性迁移；开发脚本在启动 Backend 前显式执行向上迁移。
- 迁移失败时必须停止应用启动并输出可定位但不包含凭据的错误。
- 迁移方案必须能从空数据库建立全部 Phase 1 Schema。

### 5.2 `users`

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `id` | `BIGINT UNSIGNED` | 主键，自增 |
| `username` | `VARCHAR(32)` | 非空，唯一 |
| `password_hash` | `VARCHAR(255)` | 非空 |
| `created_at` | `DATETIME(6)` | 非空 |
| `updated_at` | `DATETIME(6)` | 非空 |

用户名输入先去除首尾空白，只允许 ASCII 字母、数字和下划线，长度为 3–32 个字符。用户名匹配和唯一性不区分大小写，API 返回注册时保存的形式。

### 5.3 `posts`

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `id` | `BIGINT UNSIGNED` | 主键，自增 |
| `author_id` | `BIGINT UNSIGNED` | 非空，外键关联 `users.id` |
| `title` | `VARCHAR(120)` | 非空 |
| `content` | `TEXT` | 非空 |
| `created_at` | `DATETIME(6)` | 非空 |
| `updated_at` | `DATETIME(6)` | 非空 |

标题去除首尾空白后长度为 1–120 个字符，正文去除首尾空白后长度为 1–10000 个字符。建立 `(created_at, id)` 或等价的列表查询索引。

### 5.4 `comments`

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `id` | `BIGINT UNSIGNED` | 主键，自增 |
| `post_id` | `BIGINT UNSIGNED` | 非空，外键关联 `posts.id` |
| `author_id` | `BIGINT UNSIGNED` | 非空，外键关联 `users.id` |
| `content` | `VARCHAR(2000)` | 非空 |
| `created_at` | `DATETIME(6)` | 非空 |

评论内容去除首尾空白后长度为 1–2000 个字符。建立 `(post_id, id)` 索引以支持按帖子分页查询。

### 5.5 `post_likes`

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `post_id` | `BIGINT UNSIGNED` | 联合主键，外键关联 `posts.id` |
| `user_id` | `BIGINT UNSIGNED` | 联合主键，外键关联 `users.id` |
| `created_at` | `DATETIME(6)` | 非空 |

联合主键在数据库层防止重复点赞，并建立支持按用户查询的 `(user_id, post_id)` 索引。

### 5.6 事实与计数原则

- MySQL 是用户、帖子、评论和点赞的唯一业务事实来源。
- Phase 1 不在 `posts` 中保存冗余点赞数或评论数，查询时从事实表聚合。
- 写入成功以 MySQL 事务提交为准，Redis 操作不得成为 MySQL 事实写入的前置条件。
- 本阶段不实现软删除，因为不提供业务删除接口。

## 6. 认证与安全基线

### 6.1 注册与密码

- 注册使用用户名和密码。
- 密码最少 8 个字符，最多 72 字节，不自动去除首尾空白。
- 使用 Go `x/crypto/bcrypt` 产生和验证密码哈希，不保存、返回或记录明文密码。
- 用户名唯一性由数据库唯一约束最终保证，并将冲突稳定映射为业务错误。
- 注册成功后直接建立登录状态，减少最小闭环中的重复操作。

### 6.2 JWT 与 Cookie

- 使用 HS256 签名的短期 JWT 表示登录身份。
- JWT 至少包含 `sub`、`iat` 和 `exp`，`sub` 为用户 ID。
- 默认有效期为 2 小时，通过受校验的环境变量配置。
- JWT 只通过 `HttpOnly` Cookie 传输，不写入 `localStorage` 或 `sessionStorage`。
- Cookie 使用 `SameSite=Lax`、`Path=/`；非本地开发环境必须启用 `Secure`。
- JWT 签名密钥从环境变量读取，不得使用代码内置默认密钥，也不得在日志或错误中输出。
- 退出登录通过清除 Cookie 完成；Phase 1 不建立 JWT 黑名单或 Redis 会话库。

Phase 1 的认证方案是最小业务基线，不代表完整生产级身份系统。Frontend 与 Backend 通过同源相对路径通信，不在本阶段开放宽泛的跨域 Cookie 访问。

### 6.3 认证边界

- 注册和登录接口匿名可用。
- 退出接口可在 Cookie 已过期时幂等调用。
- 当前用户、帖子、评论和点赞 API 都需要有效登录身份。
- 未登录用户访问受保护 API 时统一返回 HTTP 401，不在 Handler 中重复解析 JWT。

## 7. HTTP API 契约

### 7.1 通用规则

- 业务 API 统一使用 `/api/v1` 前缀。
- 请求和响应使用 JSON，除 HTTP 204 外响应体使用统一结构。
- 成功响应使用 `{"data": ...}`，分页响应同时返回 `meta.next_cursor`。
- 失败响应使用 `{"error":{"code":"...","message":"..."}}`。
- `code` 是可稳定测试的机器可读错误码，`message` 是不暴露底层细节的用户可读描述。
- JSON 输入必须拒绝未知字段、多个 JSON 值和超过明确上限的请求体。
- 路径 ID 必须是正整数，无效 ID 返回 HTTP 400，资源不存在返回 HTTP 404。
- 服务器内部错误返回 HTTP 500，不向客户端返回 SQL、Redis、JWT 或堆栈细节。

### 7.2 认证和用户

| 方法 | 路径 | 认证 | 成功状态 | 语义 |
| --- | --- | --- | --- | --- |
| `POST` | `/api/v1/auth/register` | 否 | 201 | 创建用户并设置登录 Cookie |
| `POST` | `/api/v1/auth/login` | 否 | 200 | 验证用户名密码并设置登录 Cookie |
| `POST` | `/api/v1/auth/logout` | 否 | 204 | 幂等清除登录 Cookie |
| `GET` | `/api/v1/users/me` | 是 | 200 | 返回当前用户公开字段 |

注册、登录和当前用户响应只返回 `id`、`username`、`created_at` 等公开字段，任何情况都不返回 `password_hash`。用户名已存在返回 HTTP 409；登录失败统一返回 HTTP 401，不区分用户不存在和密码错误。

### 7.3 帖子

| 方法 | 路径 | 成功状态 | 语义 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/posts` | 201 | 以当前用户身份发布帖子 |
| `GET` | `/api/v1/posts` | 200 | 按新到旧分页查询帖子 |
| `GET` | `/api/v1/posts/:postId` | 200 | 查询帖子详情 |

列表和详情至少返回：

- 帖子 `id`、`title`、`content`、`created_at`、`updated_at`。
- 作者 `id` 和 `username`。
- `comment_count` 和 `like_count`。
- 与当前用户相关的 `liked_by_me`。

帖子列表接受 `cursor` 和 `limit`，`limit` 默认 20，最大 50。游标对客户端保持不透明，后端使用稳定唯一顺序键生成和解析游标。无效游标返回 HTTP 400。

### 7.4 评论

| 方法 | 路径 | 成功状态 | 语义 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/posts/:postId/comments` | 201 | 以当前用户身份发表评论 |
| `GET` | `/api/v1/posts/:postId/comments` | 200 | 按新到旧分页查询评论 |

评论列表使用与帖子列表一致的 `cursor` 和 `limit` 语义，每项返回评论公开字段和作者摘要。目标帖子不存在时，发表和查询都返回 HTTP 404。

### 7.5 点赞

| 方法 | 路径 | 成功状态 | 语义 |
| --- | --- | --- | --- |
| `PUT` | `/api/v1/posts/:postId/like` | 204 | 确保当前用户已点赞 |
| `DELETE` | `/api/v1/posts/:postId/like` | 204 | 确保当前用户未点赞 |

点赞和取消点赞必须幂等。重复 `PUT` 不新增第二条记录，重复 `DELETE` 不返回业务错误。不设计会因并发请求而产生不确定结果的“切换点赞”接口。

## 8. Redis 缓存设计

### 8.1 缓存范围

Phase 1 只缓存帖子详情的公共投影：

```text
key: gopulse:post:detail:v1:{postId}
TTL: 5 分钟
```

投影可包含帖子、作者摘要、点赞数和评论数，不包含与当前用户相关的 `liked_by_me`。`liked_by_me` 始终根据 MySQL 点赞事实单独查询，避免在用户之间泄漏个性化状态或建立高基数缓存键。

帖子列表、评论列表、JWT、登录会话和点赞事实不进入 Redis。

### 8.2 Cache-aside 流程

读取帖子详情：

1. 尝试从 Redis 读取公共投影。
2. 命中且内容合法时使用缓存数据。
3. 未命中、超时、连接失败或内容无法解析时查询 MySQL。
4. MySQL 查询成功后尝试以 TTL 回填 Redis。
5. Redis 回填失败时仍向客户端返回 MySQL 结果。

发表评论、点赞或取消点赞：

1. 先完成 MySQL 事实写入或删除。
2. MySQL 成功后尝试删除目标帖子详情缓存。
3. Redis 删除失败不回滚 MySQL，不将已成功的业务写入改成失败。
4. 失效失败可能在 TTL 内产生短期陈旧计数，这是 Phase 1 明确接受的最终一致语义。

### 8.3 故障边界

- Redis 完全停止时，注册、登录、帖子、评论和点赞仍必须可用。
- Redis 读写必须使用独立短超时，不让缓存故障长时间占用业务请求。
- 缓存中的未知版本、非法 JSON 或字段缺失按缓存未命中处理，不向客户端返回损坏数据。
- Phase 1 不引入分布式锁、singleflight、空值缓存、延迟双删或复杂缓存预热。

## 9. Frontend 设计

### 9.1 路由与页面

| 路径 | 页面 | 用途 |
| --- | --- | --- |
| `/register` | 注册页 | 创建用户并进入帖子列表 |
| `/login` | 登录页 | 建立登录状态 |
| `/posts` | 帖子列表 | 分页浏览帖子 |
| `/posts/new` | 发布帖子 | 创建帖子 |
| `/posts/:postId` | 帖子详情 | 查看帖子、评论、点赞和取消点赞 |

根路径根据当前认证状态导航到 `/posts` 或 `/login`。所有业务页面需要登录，路由守卫必须等待首次认证恢复完成，避免页面刷新时误判为未登录。

### 9.2 请求与状态

- Frontend 使用同源 `/api/v1` 相对路径，Vite 代理到 Backend。
- 请求明确包含 Cookie 凭据，统一解析成功响应和错误响应。
- 收到 HTTP 401 时清除内存中的认证状态并导航到登录页，不尝试在 Frontend 自行解析或续期 JWT。
- 表单显示必填、长度和格式错误，但 Backend 仍是输入校验的最终边界。
- 发布、评论和点赞期间防止重复提交，并为加载、空列表、业务错误和网络错误提供明确状态。
- 评论成功后更新或重新加载评论列表；点赞成功后重新获取帖子详情，不长期依赖未确认的乐观计数。
- 本阶段不建立全局状态库，当跨页状态超出认证与路由需求后再评估 Pinia。

### 9.3 Phase 0 页面处理

Phase 0 的基础设施连通性页面不再作为业务首页。保留 `/health` 和 `/ready` Backend 契约，并将原连通性展示放到不干扰业务流程的开发诊断路由，或在详细实施批次中明确移除页面但保留相应测试入口。不允许为了业务页面破坏 Backend readiness 能力。

## 10. 配置与运行参数

在 Phase 0 配置基础上至少增加：

```text
AUTH_JWT_SECRET
AUTH_JWT_TTL
AUTH_COOKIE_NAME
AUTH_COOKIE_SECURE

REDIS_POST_DETAIL_TTL
REDIS_OPERATION_TIMEOUT
```

配置规则：

- `AUTH_JWT_SECRET` 必填且必须满足明确的最小长度，`.env.example` 只保存标记为本地开发用的非生产示例值。
- 持续时间使用可解析且有上下限的 Go duration 字符串。
- Cookie 名称必须是合法的 HTTP Cookie 名称。
- 生产环境配置不允许关闭 Secure Cookie，本地 HTTP 开发可显式关闭。
- 配置错误导致 Backend 非零退出，日志不输出密钥、密码或完整连接串。
- 本地开发脚本继续从根 `.env` 向 Backend 子进程注入配置。

## 11. 错误处理与一致性

### 11.1 稳定业务错误

至少定义并测试：

```text
validation_failed
authentication_required
invalid_credentials
username_conflict
post_not_found
internal_error
```

Service 产生业务语义错误，HTTP 层统一映射状态码和错误响应。不在多个 Handler 中分别解析 MySQL 驱动错误字符串。

### 11.2 写入顺序

- 注册以数据库唯一约束解决并发用户名冲突。
- 评论先确认帖子存在，再写入评论；数据库外键是最终完整性边界。
- 点赞通过联合主键和幂等 SQL 消除重复记录。
- 单条事实写入不为了形式上的分层强制包装额外事务；需要多条 SQL 共同成功时才使用明确事务。
- Redis 回填或失效一律在 MySQL 成功之后执行，不纳入 MySQL 事务。

## 12. 测试方案

### 12.1 Backend 单元测试

使用 fake 或精简接口隔离外部依赖，至少覆盖：

- 注册输入校验、密码哈希和用户名冲突。
- 登录成功、用户不存在、密码错误和统一失败响应。
- JWT 签名、过期、非法签名、缺失声明和认证中间件。
- 帖子发布、输入校验、列表分页和详情不存在。
- 评论发布、分页和目标帖子不存在。
- 点赞和取消点赞的幂等语义。
- 缓存命中、未命中、回填、无效内容和 Redis 失败降级。
- MySQL 成功但缓存失效失败时，业务写入仍返回成功。
- HTTP 状态码、统一响应、请求体边界和敏感信息不泄漏。

### 12.2 Repository 与迁移测试

在可重建的真实 MySQL 测试库上验证：

- 从空库执行全部向上迁移成功。
- 主键、唯一约束、外键和必要索引存在。
- Repository 的创建、查询、分页、聚合计数和点赞幂等行为正确。
- Backend 重启后可继续读取重启前的用户、帖子、评论和点赞。

真实数据库测试必须使用专用测试库或可确认的隔离数据库，不得清空开发者的日常数据库。

### 12.3 Frontend 测试

- TypeScript 类型检查通过。
- 注册、登录、退出和认证恢复路径正确。
- 未登录访问业务页面时导航到登录页。
- 帖子列表的加载、空状态、分页和错误状态正确。
- 发布帖子后进入新帖子详情或按确定路径返回列表。
- 评论成功后页面可看到新评论。
- 点赞与取消点赞幂等，连续交互不产生未控制并发请求。
- HTTP 401、验证错误、业务错误和网络失败显示正确。
- Frontend 自动化测试、类型检查和生产构建通过。

### 12.4 集成验收

1. 从空业务库执行迁移并启动完整开发环境。
2. 注册用户并验证注册后已登录。
3. 退出后受保护接口返回 401，重新登录后恢复访问。
4. 发布多条帖子，验证列表顺序、游标分页和详情内容。
5. 发表多条评论，验证评论分页与 `comment_count`。
6. 重复点赞只保留一条事实，取消点赞后 `like_count` 和 `liked_by_me` 正确。
7. 重启 Backend，验证用户、帖子、评论和点赞全部仍存在。
8. 清空 Redis 后验证帖子详情从 MySQL 恢复并重建缓存。
9. 停止 Redis，验证注册、登录、发布、查询、评论和点赞仍可完成。
10. 恢复 Redis，不重启 Backend 即可恢复缓存能力。
11. 验证 `/health` 和 `/ready` 仍符合 Phase 0 契约，Redis 停止时 `/ready` 可报告依赖异常，但业务降级语义不受破坏。

## 13. 实施批次

Phase 1 拆分为六个顺序批次，每个批次单独编写详细实施方案：

### 13.1 [Phase 1-01：数据库迁移与 HTTP 基础契约](Phase-01-01-数据库迁移与HTTP基础契约.md)

- 建立迁移命令、四张核心表、索引和约束。
- 建立业务 API 路由组、统一成功/错误响应和严格 JSON 解析。
- 扩展配置、开发脚本和测试数据库边界。

### 13.2 [Phase 1-02：用户与认证](Phase-01-02-用户与认证.md)

- 实现注册、登录、退出和当前用户接口。
- 实现 bcrypt、JWT、Cookie 和认证中间件。
- 完成用户与认证单元测试和 Repository 验证。

### 13.3 [Phase 1-03：帖子发布与查询](Phase-01-03-帖子发布与查询.md)

- 实现帖子发布、列表、详情、聚合计数和个性化点赞状态查询。
- 实现不透明游标和分页边界。
- 完成 Post Handler、Service 和 Repository 测试。

### 13.4 [Phase 1-04：评论与点赞](Phase-01-04-评论与点赞.md)

- 实现评论发布和分页查询。
- 实现点赞和取消点赞的幂等语义。
- 完成并发重复请求、外键和聚合计数验收。

### 13.5 [Phase 1-05：Redis 帖子详情缓存](Phase-01-05-Redis帖子详情缓存.md)

- 实现帖子公共投影的 cache-aside 读取与回填。
- 实现评论和点赞后的缓存失效。
- 验证 Redis 超时、宕机、损坏值、清空和恢复场景。

### 13.6 [Phase 1-06：Frontend 业务闭环与阶段收口](Phase-01-06-Frontend业务闭环与阶段收口.md)

- 实现路由、注册、登录、帖子列表、发布、详情、评论和点赞页面。
- 保持默认验收脚本只读，新增使用专用验收数据库的完整业务验收入口，完成真实基础设施和 Backend 重启验收。
- 更新开发入口文档、本批 `VERSION` 和 Phase 1 实施记录，完成阶段收口。

## 14. 实施记录

每个详细实施方案完成后，必须在对应镜像路径创建或更新实施记录：

```text
dev/imple/Phase-01/Phase-01-XX-<名称>.md
dev/logs/Phase-01/Phase-01-XX-<名称>.md
```

每份记录必须只记载实际完成的工作，并包含：

- 实际完成的功能与行为。
- 实际变更的文件。
- 实际执行的验证命令和结果。
- 与实施方案的偏差及原因。
- 已知限制、未完成项和后续建议。

总实施方案本身不建立虚构的实施记录；只有真正执行实施批次后才创建对应记录。

## 15. 完成定义

- 注册、登录、发布、查询、评论和点赞闭环可从 Frontend 完整操作。
- 所有核心业务事实保存在 MySQL，Backend 重启后数据仍存在。
- Redis 清空、故障和恢复不会造成业务事实丢失，核心业务可降级到 MySQL。
- 认证 Cookie 不向 JavaScript 暴露，密码、密钥、连接串和底层错误不出现在 HTTP 响应或日志中。
- 数据库迁移能从空库建立 Phase 1 Schema，约束、索引和幂等语义经过验证。
- Backend 单元测试、Repository 集成测试、`go test ./...`、`go vet ./...` 全部通过。
- Frontend 自动化测试、TypeScript 类型检查和生产构建全部通过。
- 真实 MySQL、Redis、Backend 重启和 Redis 故障/恢复集成验收通过。
- `/health` 和 `/ready` 保持 Phase 0 契约，未引入 Phase 2 或后续阶段能力。
- 六个批次的实施记录与实际工作一致。
- 六个批次均在各自分支完成版本更新，阶段收口时根 `VERSION` 为 `0.2.6`，且各批提交均不包含无关文件。

## 16. Phase 2 交接边界

Phase 1 交付给 Phase 2 的是已提交的 MySQL 核心事实，而不是 Redis 缓存或 RabbitMQ 消息。

Phase 2 可在以下业务事件上增加异步动作：

```text
comment.created
post.liked
post.unliked
```

但 Phase 2 不得改变以下原则：

- 评论或点赞的 MySQL 事实必须先成功。
- RabbitMQ 暂时不可用时，已提交的核心业务事实不得丢失或回滚。
- 异步消费结果不成为 Phase 1 查询接口的事实来源。
