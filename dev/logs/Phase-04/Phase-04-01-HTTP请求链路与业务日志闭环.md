# Phase-04-01：HTTP 请求链路与业务日志闭环开发记录

## 1. 执行信息

- 执行日期：2026-09-03
- 开发分支：`develop/1.1.1`
- 基线：已 fetch `origin`，从 `origin/main` 的 `6bb716c` 创建分支；基线 `VERSION=1.0.0`
- 完成版本：`1.1.1`
- 初始工作树：无未提交变更
- 初始应用进程：未发现 GoPulse Backend、Worker、Indexer、Frontend 或 Vite 日常进程
- 初始基础设施：保留既有 `gopulse-*` 与 `gopulse-phase0203-integration-*` MySQL、Redis、RabbitMQ 容器；真实验收使用独立随机 Compose project，清理后日常资源快照未变化

## 2. 实际完成工作

### 2.1 统一 JSON 日志基础

- 新增 `backend/internal/observability/logging`，基于标准库 `log/slog` 输出单行 JSON。
- 固定 `log_schema_version=1`、`service`、UTC RFC3339Nano `timestamp`、小写 `level`、`module` 与 `message`。
- 实现 module child、request context attach/read、显式 discard logger，并过滤调用点对保留字段的覆盖。
- 最低等级固定为 info；未增加环境变量或第三方日志依赖。

### 2.2 HTTP 请求链路

- 新增 128-bit 服务端 request ID 生成器，使用 `crypto/rand` 和 32 位小写十六进制编码。
- 全局 middleware 顺序调整为 `request-id → access logger → structured recovery`，认证仍位于受保护 API 路由组。
- 忽略客户端 `X-Request-ID`，成功生成后写入响应头、request context 与 request-scoped logger。
- 随机生成失败时返回安全 `500 internal_error`，不使用客户端、时间或弱随机 fallback，也不伪造 request ID。
- 每请求输出唯一 `http request completed`，记录白名单字段：request ID、method、Gin route template、status、整数 duration/response size、可用的认证 user ID 和公共 error code。
- 未匹配路由使用 `unmatched`；不记录原始 path、query、IP、User-Agent、Referer、headers 或 body。
- 自定义 Recovery 输出安全 `http panic recovered` 与统一内部错误 Envelope，不记录 panic 值或 stack。

### 2.3 安全错误与业务动作

- response 层保存最终公共 `apperror.Code`，供 access middleware 只读获取；既有 HTTP 状态、错误 JSON 和安全 message 未改变。
- 在事实成功边界增加并关联 request ID：`user registered`、`user logged in`、`user logged out`、`post created`、`comment created`、`post liked`、`post unliked`、`notification marked read`。
- 动态字段仅包含已经确认的正整数 user/post/comment/notification ID；未记录用户名、密码、标题、正文、评论、Cookie 或 token。

### 2.4 请求内缓存降级

- 将 Post cache read/fill 与 Comment/Like cache invalidation 的文本日志迁移为结构化 warn。
- 日志继承 request ID，使用 `module=cache`、固定 message、post ID、有限 `reason=cache_unavailable` 和必要的固定 operation。
- Redis 失败时既有 MySQL fallback、HTTP 结果和非阻断语义保持不变。
- 真实验收发现 go-redis 连接池会直接向 stderr 输出包含连接地址的文本行；随后通过 go-redis 公共 `SetLogger`/`VoidLogger` 接口关闭依赖内部文本日志，由应用自己的安全 cache warn 作为唯一请求内降级记录。

### 2.5 真实日志验收与文档

- 为 `scripts/verify-business.sh` 新增 `--logging-live`：复用随机 token、独立 Compose project/database/ports/temp dir、进程归属校验和清理白名单。
- HTTP helper 捕获响应 headers 与 request ID；模式覆盖注册、登录、退出、当前用户、帖子创建/列表/详情、评论创建/列表、点赞/取消、搜索、通知列表/已读，以及代表性 400、401、404、503 和 Redis fallback。
- 验收逐行解析 Backend 日志，检查 Schema、字段类型、等级、唯一完成记录、header/log 关联、业务资源 ID、伪造 ID 替换和敏感哨兵负面扫描。
- 仅临时允许 Phase-04-02 范围内的 Backend lifecycle/Outbox 既有文本行。
- README 增加 Backend 结构化日志契约和验收入口说明；根与 Frontend 版本元数据同步为 `1.1.1`。

## 3. 变更文件

- 日志基础：`backend/internal/observability/logging/logging.go`、`logging_test.go`
- HTTP 链路：`backend/internal/http/router.go`、`backend/internal/http/middleware/request_logging.go`、`request_logging_test.go`
- 安全错误元数据：`backend/internal/http/response/response.go`、`response_test.go`
- 业务日志：`backend/internal/auth/handler.go`、`backend/internal/post/handler.go`、`backend/internal/comment/handler.go`、`backend/internal/like/handler.go`、`backend/internal/notification/handler.go`
- cache 降级：`backend/internal/post/service.go`、`backend/internal/comment/service.go`、`backend/internal/like/service.go`
- 应用装配：`backend/cmd/server/main.go`
- 验收与治理测试：`scripts/verify-business.sh`、`scripts/ci/test_verify_business.py`
- 文档与版本：`README.md`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`
- 本记录：`dev/logs/Phase-04/Phase-04-01-HTTP请求链路与业务日志闭环.md`

## 4. 实际验证

以下固定门禁在最终生产代码与脚本变更上执行并通过：

```bash
(cd backend && go test ./internal/observability/logging ./internal/http/... ./internal/auth ./internal/post ./internal/comment ./internal/like ./internal/notification ./internal/search ./cmd/server)
(cd backend && go vet ./internal/observability/logging ./internal/http/... ./internal/auth ./internal/post ./internal/comment ./internal/like ./internal/notification ./internal/search ./cmd/server)
(cd backend && go test -race ./internal/observability/logging ./internal/http/...)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
scripts/verify-business.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.1.1 --base-ref upstream/main
git diff --check
```

结果摘要：

- 定向 Go test、Go vet 和 HTTP/logging race test 全部通过。
- Bash safety self-test 接受 1 个安全目标并在访问 Docker 前拒绝 6 个不安全目标。
- Python CI 单测共 24 项通过。
- 版本一致性与 `develop/1.1.1` 分支治理通过。
- `git diff --check` 通过。

真实验收：

```bash
scripts/verify-business.sh --logging-live
```

- 首次执行捕获到 go-redis 连接池的非 JSON stderr 文本并按规则失败；该失败直接促成依赖内部 logger 的安全关闭。
- 修复后重新执行通过：解析 35 条 Schema v1 JSON 记录，关联 20 个真实 HTTP 请求，允许 1 条待 Phase-04-02 迁移的 Backend lifecycle 文本行。
- 伪造客户端 request ID、用户名、密码、标题、正文、评论、搜索词和 Cookie/JWT 值均未出现在 Backend 日志中。
- 独立验收资源成功清理，日常开发资源快照保持不变。

## 5. 与方案的偏差

- 没有功能范围缩减。
- 为解决真实 `--logging-live` 暴露的非 JSON、含连接地址依赖日志，检查了 go-redis 公共 `SetLogger`/`VoidLogger` 最小接口并关闭其内部 stderr logger。这是由具体运行失败触发的最小第三方源码/API 检查，不是依赖审计；应用层 cache warn 仍完整保留并通过 request ID 关联验收。
- 未增加生产 panic 调试路由；按方案由 Gin middleware 测试验证 panic 的安全 body、error 记录和最终 500 access log。

## 6. 已知限制与后续项

- Outbox Dispatcher、Business Worker、Search Indexer、search-reindex 和 Backend lifecycle 的既有文本日志仍属于 Phase-04-02；本批未将 HTTP request ID 写入 AMQP Envelope，也未建立异步分布式 trace。
- 未增加日志文件 sink、轮转、采样、OpenTelemetry、LogMonitor、Kafka 或新的日志环境变量。
- 本记录仅标记 Phase-04-01 批次完成，不标记整个 Phase 4 完成。
