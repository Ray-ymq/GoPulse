# Phase 0-02：Backend 与就绪检查实施方案

> 执行序号：2 / 5
> 前置批次：Phase 0-01 已完成并通过验收
> 总方案来源：[Phase-00-总实施方案.md](Phase-00-总实施方案.md)

## 1. 批次目标

建立可独立运行的 Go + Gin Backend，实现进程存活检查和 MySQL、Redis、RabbitMQ 就绪检查，为 Frontend 和统一开发脚本提供稳定的 HTTP 契约。

本批完成后，即使没有 Frontend，也应能启动 Backend、调用 `/health` 与 `/ready`，并验证基础设施故障和恢复行为。

## 2. 前置条件

- Phase 0-01 已完成，三项 Compose 服务能够启动并进入 healthy。
- 根目录 `.env.example` 已确定 Backend 使用的变量名、端口和开发凭据。
- 本机具备 Go 1.26 开发环境。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 Backend 结构

建立 module path 为 `github.com/Ray-ymq/GoPulse/backend` 的独立 Go module 和以下分层：

```text
backend/
├── cmd/server/               # 程序入口
├── internal/
│   ├── config/               # 环境变量加载与校验
│   ├── http/                 # Gin 路由、Handler、响应模型
│   └── platform/             # MySQL、Redis、RabbitMQ 适配
├── go.mod
└── go.sum
```

入口负责：

1. 从进程环境变量加载并校验配置。
2. 初始化 MySQL 连接池与 Redis 客户端。
3. 创建 RabbitMQ readiness checker。
4. 组装基础设施 checker 和 Gin 路由。
5. 启动监听 `HTTP_HOST:HTTP_PORT` 的 HTTP Server。
6. 监听终止信号并执行优雅关闭。
7. 关闭已成功创建的客户端资源。

### 3.2 配置规则

Backend 只读取进程环境变量，不隐式查找或加载 `.env` 文件。第四批开发脚本负责把根目录 `.env` 注入 Backend 子进程。

至少加载：

```text
APP_ENV
HTTP_HOST
HTTP_PORT
MYSQL_HOST
MYSQL_PORT
MYSQL_DATABASE
MYSQL_USER
MYSQL_PASSWORD
REDIS_HOST
REDIS_PORT
REDIS_PASSWORD
REDIS_DB
RABBITMQ_URL
```

配置行为：

- 对允许使用默认值的字段采用明确默认值。
- 必填字段缺失时返回可定位字段的错误并非零退出。
- 端口必须位于合法范围。
- `REDIS_DB` 必须为合法整数。
- `RABBITMQ_URL` 必须能够解析且使用支持的 AMQP scheme。
- 错误和日志不得包含密码或完整连接串。
- 只有配置非法、客户端构造失败或 HTTP Server 无法启动才导致进程启动失败；基础设施暂时不可达不应阻止 Backend 启动，而由 `/ready` 报告 503。

### 3.3 HTTP 契约

#### `GET /health`

- 只证明进程和 HTTP Server 存活，不访问任何外部依赖。
- 正常返回 HTTP 200：

```json
{
  "status": "ok",
  "service": "backend"
}
```

#### `GET /ready`

- 并行检查 MySQL、Redis、RabbitMQ。
- 每个 checker 使用独立的 1 秒超时，整个 `/ready` 请求的服务端检查上限为 1.5 秒。
- 全部为 `up` 时返回 HTTP 200 和 `status: ready`。
- 任一异常或超时时返回 HTTP 503 和 `status: not_ready`。
- 无论几项失败，响应都必须包含三项检查结果。
- 对外只暴露 `up`/`down`，不暴露原始错误、密码或连接串。

成功响应：

```json
{
  "status": "ready",
  "service": "backend",
  "checks": {
    "mysql": "up",
    "redis": "up",
    "rabbitmq": "up"
  }
}
```

失败响应遵循同一结构，仅把失败项标记为 `down`。

### 3.4 基础设施检查

- MySQL：复用 `database/sql` 连接池，使用带 context 的 ping。
- Redis：复用 Redis 客户端，执行带 context 的 `PING`。
- RabbitMQ：每次检查通过支持 context 或显式网络超时的 dialer 创建短生命周期 AMQP 连接，成功后立即关闭，不允许默认拨号无限挂起。
- Handler 依赖 checker 接口，不直接依赖具体客户端，单元测试使用 fake checker。
- 三项检查必须并发，总请求耗时接近最慢单项超时，而不是三个超时之和。
- checker 每次请求重新检查当前状态，基础设施恢复后无需重启 Backend。

### 3.5 生命周期

- 初始化过程中任一非连通性步骤失败，应关闭此前已经成功创建的资源。
- 接收到终止信号后，停止接收新请求，并使用 5 秒 graceful shutdown 上限关闭 HTTP Server。
- Server 关闭后再释放 MySQL 与 Redis 客户端。
- 正常完成优雅关闭时返回 0；配置、绑定端口、Server 运行或关闭超时等异常返回非 0。

## 4. 明确不做的内容

- 不创建业务表或数据库迁移。
- 不实现 User、Post、Comment、Like、认证或业务 API。
- 不声明 RabbitMQ 业务 exchange、queue、binding 或 consumer。
- 不设计 Redis 缓存键和缓存策略。
- 不加入完整日志体系、指标系统、追踪系统或 CI。
- 不创建 Backend Dockerfile。
- 不加载 `.env` 文件；该职责属于第四批脚本。

## 5. 目标文件和目录

预计涉及：

```text
backend/go.mod
backend/go.sum
backend/cmd/server/
backend/internal/config/
backend/internal/http/
backend/internal/platform/
dev/logs/Phase-00/Phase-00-02-Backend与就绪检查.md
```

测试文件与对应包放置，不依赖真实容器完成 Handler 单元测试。

## 6. 详细实施步骤

1. 初始化 Backend Go module，固定 Gin、MySQL、Redis、AMQP 等依赖版本并提交 `go.sum`。
2. 实现配置结构、环境变量读取、默认值和字段级校验测试。
3. 建立 Gin Router，先实现不访问外部依赖的 `/health` 及测试。
4. 定义 readiness checker 接口和统一检查结果模型。
5. 使用 fake checker 实现 `/ready` Handler，覆盖成功、单项失败、多项失败和超时。
6. 分别实现 MySQL、Redis、RabbitMQ checker 及资源关闭行为。
7. 实现三项 checker 并发调度、独立超时和结果汇总。
8. 组装程序入口、HTTP Server、信号监听和优雅关闭。
9. 对日志和错误路径进行敏感信息检查。
10. 在依赖全部正常、启动时已有依赖不可达、运行期依赖故障与恢复四类状态下执行真实联调。
11. 在对应的 `dev/logs/Phase-00/Phase-00-02-Backend与就绪检查.md` 中记录实际完成内容、变更文件、验证命令与结果、偏差和已知限制。

## 7. 测试与验收标准

### 7.1 单元测试

必须覆盖：

- `/health` 返回 HTTP 200 和精确响应结构。
- `/health` 不调用任何 checker。
- 三项正常时 `/ready` 返回 HTTP 200。
- 任一项失败时返回 HTTP 503 并准确标记。
- 多项失败时全部失败项均被标记。
- 单项超时能够结束请求并标记为 `down`。
- 三项检查并发执行，不累加各自超时时间。
- 基础设施不可达不会使 Backend 启动失败，`/health` 仍返回 200，`/ready` 在规定上限内返回 503。
- 响应不包含底层错误、密码或连接串。
- 配置默认值、覆盖值、缺失值、非法端口、非法 Redis DB 和非法 RabbitMQ URL。

### 7.2 静态与构建检查

- `go test ./...` 通过。
- `go vet ./...` 通过。
- Go module 与依赖校验文件完整。
- 没有跨越 `internal` 边界的错误依赖。

### 7.3 集成验收

1. 三项基础设施运行时，Backend 成功启动。
2. `/health` 返回 HTTP 200。
3. `/ready` 返回 HTTP 200，三项均为 `up`。
4. 停止任意一个基础设施容器后，`/health` 仍为 200。
5. 同一情况下 `/ready` 返回 503，且只标记实际故障项。
6. 同时停止多项依赖，响应包含所有失败项。
7. 恢复容器后，`/ready` 无需重启 Backend 即恢复为 200。
8. 终止 Backend 后端口释放，资源正常关闭。
9. 在启动 Backend 前先停止任意一项依赖，Backend 仍成功启动，`/health` 为 200，`/ready` 为 503；恢复依赖后自动转为 200。

## 8. 完成定义

- Backend 可通过环境变量独立启动。
- `/health` 与 `/ready` 契约稳定并有单元测试保护。
- 三项 readiness 检查并发、可超时且可自动恢复。
- 初始化失败和终止信号均能正确释放资源。
- Go 测试、vet 和真实基础设施联调通过。
- 对应实施记录已创建且只记载实际执行的工作和检查。
- 没有实现 Phase 0 之外的业务能力。

## 9. 下一批次交接条件

交付给 Phase 0-03 前必须提供：

- `/health`、`/ready` 的固定路径、状态码和 JSON 结构。
- Backend 默认监听地址 `0.0.0.0:8080`。
- 可用于 Frontend 联调的启动命令。
- 全部正常、部分异常和 Backend 停止三种可复现状态。

Phase 0-03 只能依赖上述公开 HTTP 契约，不直接依赖 Backend 内部实现。
