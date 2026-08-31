# Phase 0-02：Backend 与就绪检查实施记录

- 完成日期：2026-08-31（Asia/Shanghai）
- 分支：`develop/0.1.2`
- 目标版本：`0.1.2`
- 前置批次：Phase 0-01（`0.1.1`）
- 执行范围：Phase-00-02；未实现 Phase-00-03 及后续批次

## 1. 实际完成内容

1. 在 `backend/` 建立独立 Go module `github.com/Ray-ymq/GoPulse/backend`，固定 Gin、MySQL、Redis 和 AMQP 客户端依赖版本并生成 `go.sum`。
2. 实现只读取进程环境变量的配置加载：为应用环境、监听地址、MySQL/Redis 主机与端口、Redis DB 提供明确默认值；对 MySQL 数据库/用户/密码、Redis 密码和 RabbitMQ URL 执行必填校验；校验端口范围、Redis DB 和 AMQP URL scheme/host。配置错误只返回字段级信息，不回显密码或完整连接串。
3. 实现 `GET /health`，固定返回 HTTP 200 与 `{"status":"ok","service":"backend"}`，且不访问外部依赖。
4. 实现 `GET /ready`，并发执行 MySQL `PingContext`、Redis `PING` 和 RabbitMQ 短连接检查。每项检查具有独立 1 秒上限，整个请求具有 1.5 秒上限；响应固定包含三项 `up`/`down`，任一失败时返回 HTTP 503，且不暴露底层错误。
5. MySQL 与 Redis 客户端在进程生命周期内复用；RabbitMQ 每次 readiness 请求使用带 context、拨号超时和连接 deadline 的短生命周期连接，完成后立即关闭。
6. Backend 启动阶段只构造客户端，不探测外部依赖，因此依赖暂时不可达不会阻止 HTTP Server 启动；依赖恢复后下一次 `/ready` 会重新检查并自动恢复。
7. 实现 HTTP Server 启动错误处理、Windows/Unix 终止信号监听、5 秒 graceful shutdown，以及 Server 关闭后释放 Redis/MySQL 资源。生命周期单元测试覆盖正常取消返回 `nil`、端口释放和启动失败传播。
8. 增加配置、HTTP 契约、失败/多失败、单项超时、忽略 context 的阻塞 checker、并发执行、敏感信息保护、客户端在依赖不可达时仍可构造，以及 Server 生命周期测试。
9. 将根 `VERSION` 从 `0.1.1` 更新为 `0.1.2`。

## 2. 本批变更文件

- `backend/go.mod`
- `backend/go.sum`
- `backend/cmd/server/main.go`
- `backend/cmd/server/main_test.go`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/internal/http/router.go`
- `backend/internal/http/router_test.go`
- `backend/internal/platform/mysql.go`
- `backend/internal/platform/redis.go`
- `backend/internal/platform/rabbitmq.go`
- `backend/internal/platform/platform_test.go`
- `dev/logs/Phase-00/Phase-00-02-Backend与就绪检查.md`
- `VERSION`

用户已有且未跟踪的 `VSRSION` 未修改、未暂存、未提交。本地 `.env`、Compose 文件和数据卷也未修改。

## 3. 自动化验证与结果

执行环境：Windows，Go `go1.26.1 windows/amd64`，Docker Engine `29.4.0`。

### 3.1 格式、模块、测试与静态检查

执行：

```powershell
cd backend
gofmt -w .
go mod tidy
go mod verify
go test ./...
go test -race ./...
go test -cover ./...
go vet ./...
```

结果：

- `go mod tidy` 成功并生成完整 `go.sum`。
- `go mod verify` 返回 `all modules verified`。
- `go test ./...` 全部通过。
- `go test -race ./...` 全部通过，未发现数据竞争。
- `go vet ./...` 无输出并以 0 退出。
- 覆盖率：`cmd/server` 32.4%、`internal/config` 94.0%、`internal/http` 95.8%、`internal/platform` 80.5%。
- `git diff --check` 通过。

### 3.2 配置与 `.env` 边界

在 `backend/` 下清除本批配置环境变量后执行：

```powershell
go run ./cmd/server
```

结果：进程以非零状态退出，错误为 `load configuration: MYSQL_DATABASE is required`。虽然仓库根目录存在本地 `.env`，Backend 没有隐式加载该文件；错误不包含密码或连接串。

### 3.3 真实基础设施联调

联调使用 Phase 0-01 已运行且 healthy 的 Compose 服务，以及本地 `.env` 的安全宿主机端口：MySQL `13306`、Redis `16379`、RabbitMQ AMQP `5672`、Management `15672`。Backend 监听 `0.0.0.0:8080`。

实际结果：

1. 三项依赖 healthy 时，`GET /health` 返回 HTTP 200 和精确存活响应；`GET /ready` 返回 HTTP 200，三项均为 `up`，一次实测耗时 152 ms。
2. 运行期停止 Redis 后，`/health` 仍为 200；`/ready` 在 1011 ms 内返回 503，只有 Redis 为 `down`。重新启动 Redis 并进入 healthy 后，无需重启 Backend，`/ready` 恢复 200。
3. 同时停止 MySQL 与 Redis 后，`/ready` 在 1058 ms 内返回 503，MySQL/Redis 为 `down`、RabbitMQ 为 `up`。两项恢复 healthy 后 Backend 可继续使用。
4. 运行期停止 RabbitMQ 后，`/health` 仍为 200；`/ready` 返回 503，只有 RabbitMQ 为 `down`。
5. 在 RabbitMQ 已停止的情况下重新启动 Backend，进程仍成功监听 8080；`/health` 返回 200，`/ready` 返回 503 且只标记 RabbitMQ 为 `down`。RabbitMQ 恢复 healthy 后，无需重启 Backend，`/ready` 自动恢复 200。
6. 使用 Windows `CTRL_BREAK_EVENT` 向独立进程组中的编译后 Backend 发送终止事件，进程完成 graceful shutdown 并返回退出码 0；随后确认 8080 端口可重新绑定。测试结束后三项 Compose 服务均恢复为 healthy，未执行 `down -v`，具名卷保留。

## 4. 偏差、限制与后续项

- 远端 `origin/main` 尚未包含已完成的 Phase 0-01。按分支生命周期规则先从最新 `origin/main` 创建 `develop/0.1.2`，再快进纳入本地 `develop/0.1.1` 的前置提交，未重写或重新编号已存在分支。
- 用户要求优先由 subagent 执行；两个 subagent 均异常以 `completed:null` 结束且未生成文件，随后按用户授权由主 agent 完成实现、测试和联调。
- MySQL/Redis 客户端库在连接失败时可能向进程标准错误输出不含凭据的主机端口或协议级错误；HTTP 响应始终只公开 `up`/`down`。未观察到密码或完整 RabbitMQ URL 泄漏。
- 本批没有添加业务 Schema、迁移、业务 API、RabbitMQ 业务拓扑、Redis 缓存策略、Backend Dockerfile、完整日志/指标/追踪体系或 `.env` 加载能力，符合 Phase 0-02 边界。
- Frontend 和统一开发脚本属于 Phase 0-03/0-04，本批未提前实现。Phase 0-03 可依赖固定路径 `/health`、`/ready`、默认监听地址 `0.0.0.0:8080` 及本记录中的三种可复现状态。
