# Phase-04-01：HTTP 请求链路与业务日志闭环实施方案

> 执行序号：1 / 2
>
> 前置条件：Milestone 1 release-only 动作已经完成，主远程 `main` 产品版本为 `1.0.0`
>
> 总方案来源：[Phase-04-总实施方案.md](Phase-04-总实施方案.md)

## 1. 批次目标

为 GoPulse Backend 建立统一的 stdout JSON 日志基础，并完成服务端生成 `request_id`、HTTP 完成日志、结构化 panic Recovery、安全错误码和关键业务动作日志。

本批必须以真实主要 API 证明：客户端收到的 `X-Request-ID` 可以关联同一请求的最终访问结果和成功业务动作，正常、错误及降级路径均遵循同一 Schema，且用户内容、认证材料和基础设施凭据不会进入日志。

本批完成后，HTTP 请求到结构化业务日志的 Phase 4 最小端到端闭环已经成立；Outbox、Business Worker、Search Indexer 和 search-reindex 的全面迁移留给 Phase-04-02。

## 2. 前置条件

- `develop/1.0.0` 已合入配置的主远程 `main`，远程必要门禁成功，根 `VERSION` 与 Frontend npm 元数据均为 `1.0.0`。
- 已 fetch 主远程并从最新 `main` 创建 `develop/1.1.1`；工作树、日常应用进程和基础设施资源状态已经记录。
- Phase 3 的 HTTP、认证、业务、通知、搜索、缓存和错误响应契约均为本批回归基线，不重新解释其业务语义。
- 实施与真实应用验收在 WSL2 Linux filesystem 中完成；只维护 Bash 入口，不修改冻结 PowerShell。
- 初始发现限制在日志基础、Gin middleware/response、状态变更 Handler、现有缓存日志点、Server 装配和直接测试；10 分钟内进入第一个 in-scope 实现改动。

## 3. 实施范围

### 3.1 统一 JSON 日志基础

- 在 Backend Go module 内新增单一内部日志包，使用标准库 `log/slog`，不增加第三方依赖。
- 提供显式构造函数，接收固定 `service` 与 `io.Writer`，返回包含 `log_schema_version=1` 和 service 基础属性的 `*slog.Logger`。
- 自定义 Handler 属性规范：`time` 输出为 `timestamp`、`msg` 输出为 `message`、level 输出为小写；timestamp 使用 UTC RFC3339Nano。
- 固定最低输出等级为 info，不新增环境变量；允许应用调用 info、warn、error。
- 提供 module child logger，以及将 request-scoped logger 写入/读取 `context.Context` 的小型辅助接口。
- 测试注入 buffer 并逐行 JSON 反序列化；缺少请求 logger 时使用显式注入的 JSON fallback 或 discard test logger，不回退到文本标准 logger。
- 防止调用点覆盖 `log_schema_version`、`service`、`timestamp`、`level`、`module` 和 `message` 等保留字段。

### 3.2 服务端 request ID 与 Gin middleware

- 新增可注入生成器的 request-id middleware；生产生成器用 `crypto/rand` 读取 16 bytes，再编码为 32 位小写十六进制。
- 忽略客户端发送的 `X-Request-ID`，为每次请求生成新值；在进入认证或 Handler 前写入 request context 和响应头。
- 随机源失败时不使用客户端 ID、时间戳或弱随机 fallback；返回统一 500 `internal_error` 并输出不伪造 request ID 的结构化 error。
- 新增 access middleware，在请求完成后输出唯一 `http request completed` 记录：`request_id`、`method`、Gin `route` template、`status`、整数 `duration_ms`、`response_bytes`；认证成功后附 `user_id`。
- 未匹配路由统一记录 `route=unmatched`，不得记录原始 path、路径参数、query string、IP、User-Agent 或 Referer。
- 2xx/3xx 记录 info，4xx 记录 warn，5xx 记录 error。
- 用自定义结构化 Recovery 替换 `gin.Recovery()`：捕获 panic、写统一 500 错误 Envelope、记录 `http panic recovered`，但不记录 panic 值、堆栈、headers 或 body。
- 全局 middleware 顺序固定为 `request-id → access logger → structured recovery`，API authentication 保持路由组内既有位置。

### 3.3 安全错误元数据

- 扩展统一 response 层，在写错误响应时把最终公共 `apperror.Code` 保存到 Gin context，供 access middleware 在完成时读取。
- 不改变现有状态码、JSON `{error:{code,message}}`、客户端安全 message 或 `apperror` 包装规则。
- 未知错误只记录 `internal_error`，不得把 `err.Error()`、unwrap cause、SQL、连接地址或第三方响应写入 HTTP 日志。
- Recovery 复用相同的安全错误响应路径，确保 panic 的 body 与普通内部错误一致。

### 3.4 业务成功日志

在对应事实成功后、HTTP 响应完成前输出以下固定日志；失败路径不得输出成功日志：

- Auth：`user registered`、`user logged in`、`user logged out`。
- Post：`post created`。
- Comment：`comment created`。
- Like：`post liked`、`post unliked`。
- Notification：`notification marked read`。

每条日志从 request context 继承 request ID，并只附已经确认的正整数 user/post/comment/notification ID。不得记录 username、credentials、标题、正文、评论内容、Cookie、token 或请求 body。

GET 列表、详情、当前用户、搜索和通知查询不新增高频业务日志，由统一 HTTP 完成记录覆盖。相同业务事实不在 Handler、Service 和 Repository 三层重复记录。

### 3.5 请求内降级日志

- 将 Post detail cache 读取、回填和 Comment/Like cache invalidation 的既有 `log.Printf` 改为结构化 warn。
- 日志从传入的 context 继承 request ID，使用 `module=cache` 或所属业务 module、固定 message、post ID 和有限 reason。
- 保持 Redis 故障时 MySQL fallback、非阻断返回和 timeout 行为不变；不记录 cache key、缓存 value、Redis URL 或原始错误。
- 不在本批迁移 Outbox Dispatcher 的后台循环日志，避免跨越 Phase-04-02 的进程边界。

### 3.6 定向真实日志验收

- 为 `scripts/verify-business.sh` 增加 `--logging-live` 模式，复用既有随机 token、隔离 Compose project、动态端口、临时目录和清理白名单。
- 模式启动完成主要 API 所需的真实 MySQL、Redis、RabbitMQ、Elasticsearch、Backend 与必要 Worker，不访问或清理日常资源。
- HTTP helper 额外捕获 response headers 和本次请求 ID，不改变既有状态/body 断言。
- 使用真实路由覆盖注册、登录、退出、当前用户、帖子创建/列表/详情、评论创建/列表、点赞/取消、搜索、通知列表/已读；按路由表证明所有主要 API 均经过统一 middleware。
- 覆盖一个代表性 400、401、404 和 503；panic 用可注入的 Go router test 证明，不为生产注册调试 panic 路由。
- 对本批新增的 HTTP/业务/cache 记录逐行 JSON 解析，按字段匹配而不是 grep 拼接文本；只临时允许交接清单中尚待 Phase-04-02 迁移的 Backend lifecycle/Outbox 旧文本行，其他非 JSON 应用输出必须失败。断言 ID 格式、header/log 一致、伪造客户端 ID 被替换、完成日志唯一和等级正确。
- 在请求中注入唯一 username/password/title/content/comment/search/cookie 哨兵并扫描日志，确认禁止内容未出现。

## 4. 接口与兼容性

### 4.1 新增公共 HTTP 行为

- 所有由 Backend 接收并成功生成 ID 的响应增加 `X-Request-ID: <32-lower-hex>`。
- 请求与响应 JSON Schema、API 路径、认证 Cookie 和状态码保持不变。
- 客户端传入的 `X-Request-ID` 没有信任语义；本批不增加 CORS 暴露、Frontend 展示或重试逻辑。

### 4.2 内部接口

- 日志包暴露构造、module child、context attach/read 和 request ID 读取所需的最小内部 API。
- request-id 生成器作为 middleware 依赖可在测试注入固定值或错误。
- response 层暴露 access middleware 读取安全 error code 的只读辅助边界，不暴露原始 error。
- Handler 与已有缓存日志点接收或取得 `*slog.Logger`，不使用 package-level 文本 logger。

这些接口只在 Backend Go module 内使用；不建立跨 module 公共库，也不要求 Phase 5 的独立组件导入 Backend `internal` package。

## 5. 实施边界与非目标

- 不迁移 Worker、Search Indexer、search-reindex 或 Outbox 后台日志；它们属于下一批。
- 不给 RabbitMQ Envelope/headers 增加 request ID，不建立 HTTP 与异步处理的分布式 trace。
- 不记录读取请求的结果数量、搜索词、cursor、原始 URL 或用户内容。
- 不增加 `LOG_LEVEL`、文件 sink、轮转、采样、OpenTelemetry 或 LogMonitor。
- 不调整业务错误分类、HTTP timeout、认证、缓存 fallback、Outbox 写入或搜索语义。
- 不修改数据库 migration、Frontend 功能、Compose 服务、Kafka 或 PowerShell。
- 不为每条路由复制相同中间件单测；以路由注册检查加一条真实全路由日志模式证明覆盖。

## 6. 预计文件与交付物

预计触达：

```text
backend/internal/observability/logging/
backend/internal/http/router.go
backend/internal/http/middleware/
backend/internal/http/response/
backend/internal/auth/handler.go
backend/internal/post/
backend/internal/comment/
backend/internal/like/
backend/internal/notification/handler.go
backend/cmd/server/
scripts/verify-business.sh
scripts/ci/
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-04/Phase-04-01-HTTP请求链路与业务日志闭环.md
```

预计文件仅表示允许触达的边界；Search Handler 只需统一 access log 时不为对称性增加业务日志。Frontend 除版本元数据外不做功能改动。

## 7. 详细实施步骤

1. 从总方案提取 Schema、保留字段、敏感字段和本批固定门禁，核对最新 `main=1.0.0` 与既有 HTTP 契约。
2. 实现标准库 JSON logger、属性替换、module child、context 辅助和可注入 writer，先用最低层测试固定字段、类型、时间、等级与转义。
3. 实现 128-bit request ID 生成器和 middleware，覆盖格式、客户端 header 忽略与随机源失败。
4. 实现 access logger 和安全 route/status/duration/size/user/error_code 读取，固定等级映射与唯一完成记录。
5. 用结构化 Recovery 替换默认 Recovery，复用统一 500 body；通过测试路由验证 panic 后 access log 仍读取最终 500。
6. 扩展 response error metadata，不改变公共 body；覆盖已知与未知错误映射。
7. 在状态变更 Handler 成功点增加业务日志，使用 context request ID 与已确认资源 ID。
8. 迁移请求内 cache 降级日志，保持 fallback 行为并加入 request ID 断言。
9. 扩展 `--logging-live`，捕获 headers、逐行解析日志并执行主要 API、错误等级、伪造 ID 和敏感哨兵矩阵。
10. 更新 README 的日志使用说明、根/Frontend 版本与本批实施记录，只记录实际命令和结果。

## 8. 风险与控制

- **Middleware 顺序导致 panic 状态误记**：access 包围 Recovery，单测断言 panic 完成日志为 500/error。
- **客户端伪造关联键**：始终生成服务端 ID；测试发送合法形状的哨兵 header 并证明未复用。
- **错误日志泄漏底层原因**：access 只读取公共 error code，Recovery 不输出 panic/stack，敏感哨兵执行负面扫描。
- **日志改变业务结果**：日志只在事实成功后执行，logger 写入失败不改变已提交事实或 HTTP 状态。
- **高基数和内容泄漏**：route 使用 Gin template，message 固定，所有动态值使用白名单字段。
- **重复日志扩大噪声**：每请求只有一条完成日志，业务动作只在 Handler 成功边界记录一次，GET 不增加重复业务日志。
- **验收脚本误伤资源**：完全复用既有 token、project、端口、进程和 volume 归属校验，新增模式不得放宽清理白名单。

## 9. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败后只重跑受修复影响的项目：

```bash
(cd backend && go test ./internal/observability/logging ./internal/http/... ./internal/auth ./internal/post ./internal/comment ./internal/like ./internal/notification ./internal/search ./cmd/server)
(cd backend && go vet ./internal/observability/logging ./internal/http/... ./internal/auth ./internal/post ./internal/comment ./internal/like ./internal/notification ./internal/search ./cmd/server)
(cd backend && go test -race ./internal/observability/logging ./internal/http/...)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
scripts/verify-business.sh --self-test
scripts/verify-business.sh --logging-live
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.1.1 --base-ref upstream/main
git diff --check
```

本批改变全局 HTTP middleware、response error metadata 和多项业务 Handler，因此回归直接受影响的 HTTP/业务 package，并用一次真实 `--logging-live` 证明跨路由闭环。Outbox/Worker/Search Indexer 的完整 retry、重启和故障矩阵不重复执行，留给 Phase-04-02；Frontend 主体未改，不在本批为版本元数据单独重复组件/E2E 全量，远程固定门禁仍按仓库配置运行。

完整应用验收只能在 WSL2 Linux filesystem 和可确认归属的隔离资源中执行。macOS 可用于代码与文档检查，但不得用 mock 或源码阅读替代 `--logging-live`。

## 10. 验收标准

- 本批新增的 Backend HTTP、业务与 cache 日志均为单行合法 JSON，包含 Schema v1、UTC timestamp、小写 level、service、module 和固定 message；暂存的 lifecycle/Outbox 文本仅限下一批交接清单中的既有调用点。
- 所有主要 API 响应服务端生成的 `X-Request-ID`；其值能关联唯一 HTTP 完成日志，客户端 header 不被复用。
- 完成日志使用 route template，并正确记录 method/status/duration/size、认证 user ID 和安全 error code；不出现原始 URL/query/body。
- 注册、登录、退出、发帖、评论、点赞、取消点赞和通知已读在成功后产生同 request ID 的业务日志，失败路径不产生成功日志。
- 代表性 400、401、404、503 与 panic 的 status/level/error code 正确，panic 不泄漏值或 stack。
- 代表性 Redis 降级 warn 继承 request ID，既有 MySQL fallback 和 HTTP 结果保持不变。
- 日志不包含本批敏感哨兵、凭据、用户内容、搜索词、Cookie/JWT、连接 URL、Payload、DSL 或 PIT。
- 第 9 节定向测试、真实日志模式、治理和远程门禁通过；版本元数据为 `1.1.1`，实施记录真实完整。

## 11. 明确完成条件

只有统一 JSON 基础、服务端请求标识、主要 API 完成/业务/错误日志、请求内降级日志和真实 `--logging-live` 验收全部通过，且现有 HTTP 与业务契约没有阻断回归时，才可标记本批完成并合入主远程。

未迁移的 Outbox 后台循环、Business Worker、Search Indexer 和 search-reindex 必须如实保留为下一批范围，不得因 HTTP 最小闭环通过而把整个 Phase 4 标记完成。

## 12. 下一批交接

向 Phase-04-02 提供：

- 已验证的 JSON Handler、Schema v1、service/module/message 和敏感信息约束。
- Backend request context logger 与真实 HTTP 日志验收入口。
- 尚未迁移的 Outbox、Worker、Indexer、reindex 文本日志清单。
- `scripts/verify-business.sh --logging-live` 的隔离资源、header 捕获和 JSON 字段断言基础。
