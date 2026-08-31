# Phase 0-01：工程骨架与基础设施实施记录

- 完成日期：2026-08-31（Asia/Shanghai）
- 分支：`develop/1.2.0`
- 基线：`origin/main`（`7c5a9b5`）
- 执行范围：Phase-00-01；未进入 Phase-00-02 至 Phase-00-05

## 1. 实际完成内容

1. 审查并纳入本批工程骨架配置：`.editorconfig`、`.gitignore` 和 `.env.example`。
2. 确认 `deploy/compose.yaml` 只声明 `mysql`、`redis`、`rabbitmq` 三项基础设施，使用固定官方镜像、环境变量配置、容器级 healthcheck、`unless-stopped` 重启策略和独立具名卷。
3. 确认四个后续组件目录仅包含职责说明：`monitor/`、`router/`、`marshaller/`、`exporters/`；没有提前实现运行代码。
4. 保留现有本地 `.env` 的安全宿主机端口：MySQL `13306`、Redis `16379`、RabbitMQ AMQP `5672`、Management `15672`。
5. 创建本实施记录；未创建或修改 Backend、Frontend、脚本、`VERSION` 或业务代码。

## 2. 本批文件

本批纳入并准备提交的文件如下：

- `.editorconfig`
- `.env.example`
- `.gitignore`
- `deploy/compose.yaml`
- `monitor/README.md`
- `router/README.md`
- `marshaller/README.md`
- `exporters/README.md`
- `dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md`

以下文件明确排除：用户已有的 `VSRSION`、本地 `.env`、`handleoff.md` 以及根目录 `VERSION`（当前不存在；按 Phase 0 交接约定在 Phase-00-05 收口时统一写入 `1.2.0`）。

## 3. 实际验证与结果

### 3.1 Compose 静态检查

执行：

```powershell
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml config --quiet
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml config --services
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml config --volumes
```

结果：全部成功；服务为 `mysql`、`rabbitmq`、`redis`，卷为 `gopulse_mysql_data`、`gopulse_rabbitmq_data`、`gopulse_redis_data`。当前 `.env` 渲染出的发布端口为 `13306`、`16379`、`5672`、`15672`，镜像为 `mysql:8.4.0`、`redis:7.2.5-alpine`、`rabbitmq:3.13.3-management-alpine`。

使用 `.env.example` 渲染时 `config --quiet` 也成功，确认默认发布端口为 `3306`、`6379`、`5672`、`15672`。

检查 `.env.example` 所需变量：总方案和本批要求的 `APP_ENV`、`HTTP_HOST`、`HTTP_PORT`、MySQL、Redis、RabbitMQ 变量全部存在。检查 Compose 内容未发现 `latest`、Frontend 或 Backend 服务。

执行：

```powershell
git check-ignore -v .env .run/test frontend/node_modules/pkg frontend/dist/index.html
```

结果：`.env`、`.run/`、Frontend 依赖目录和构建产物均被正确忽略。

### 3.2 配置缺失失败场景

使用临时环境文件移除 `MYSQL_USER` 后执行：

```powershell
docker compose --project-name gopulse --env-file <temporary-env-without-MYSQL_USER> -f deploy/compose.yaml config --quiet
```

结果：退出码 `1`，错误明确为 `MYSQL_USER is required`；临时文件已清理。

### 3.3 Windows Docker Desktop 运行验收

执行显式指定项目名、环境文件和 Compose 文件的启动命令：

```powershell
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml up -d
```

结果：MySQL、Redis、RabbitMQ 均在等待后进入 `healthy`。最终容器映射为 MySQL `13306->3306`、Redis `16379->6379`、RabbitMQ `5672->5672` 和 `15672->15672`。

直接客户端和管理台检查结果：

- MySQL 容器内客户端执行 `SELECT 1`，返回 `1`。
- Redis 容器内 `redis-cli` 使用配置密码执行 `PING`，返回 `PONG`。
- RabbitMQ `rabbitmq-diagnostics -q ping` 返回 `Ping succeeded`。
- RabbitMQ 用户凭据认证返回 `Success`。
- RabbitMQ Management API `http://127.0.0.1:15672/api/overview` 使用配置凭据返回 HTTP `200`，版本为 `3.13.3`。
- 宿主机端口探测 `13306`、`16379`、`5672`、`15672` 均成功。

### 3.4 普通停止、卷保留和重新启动

执行：

```powershell
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml down
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml up -d
```

结果：普通 `down` 移除了三个容器和 Compose 网络，但没有删除三个具名卷。`gopulse_mysql_data`、`gopulse_redis_data`、`gopulse_rabbitmq_data` 在停止前后均保留，创建时间未改变。重新 `up -d` 后三项服务再次进入 `healthy`，卷被正常复用。整个过程中未使用 `down -v`。

### 3.5 端口冲突失败场景

Windows 宿主机在测试前确认已有外部监听：`3306` 由 `mysqld` 占用，`6379` 由 `redis-server` 占用；这些外部服务未停止或修改。

使用临时环境文件将 MySQL/Redis 改回 `3306`/`6379` 后执行 Compose 启动。Docker Desktop 本次返回退出码 `0`，但 `gopulse-mysql-1` 和 `gopulse-redis-1` 的 `NetworkSettings.Ports` 为空，两个容器没有获得宿主机发布端口，而外部服务仍保持监听；这与方案期望的“明确失败并指出端口”不一致，属于当前 Windows Docker Desktop 与既有 Windows 服务监听交互的已知偏差。测试后已执行普通 `down` 并恢复安全端口配置。

另外使用临时 Docker 容器占用 `19999`，将 `RABBITMQ_MANAGEMENT_PORT` 指向该端口，验证了 Compose/Engine 的确定性冲突路径：启动退出码为 `1`，错误包含 `Bind for 0.0.0.0:19999 failed: port is already allocated`。临时占用容器和环境文件均已清理，GoPulse 已恢复到安全端口并全部 `healthy`。

## 4. 偏差、限制与后续项

- 本批不创建 `VERSION`；Phase 0 的统一目标版本仍为 `1.2.0`，按交接记录在 Phase-00-05 收口时统一更新。
- `.env.example` 保留方案要求的标准默认端口；当前本地 `.env` 使用 `13306` 和 `16379` 避免触碰用户已有 MySQL/Redis 服务。
- 外部 Windows 服务占用 `3306`/`6379` 时，当前 Docker Desktop 没有返回预期的端口冲突错误，而是启动未发布这两个宿主机端口的容器；因此后续若需要强制验证默认端口失败，应继续使用明确的 Docker 端口占用者或在开发脚本中增加宿主机端口预检。本批没有提前创建脚本，符合 Phase-00-01 的边界。
- 当前运行状态：Windows Docker Desktop 中 GoPulse 的 MySQL、Redis、RabbitMQ 均为 `healthy`；具名卷保留。
- Phase-00-02 的 Backend 实现尚未开始。
