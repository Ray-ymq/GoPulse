# GoPulse Phase 0 暂停交接记录

- 记录时间：2026-08-31（Asia/Shanghai）
- 最后环境复核：2026-08-31 16:29（Asia/Shanghai）
- 当前分支：`develop/1.2.0`
- 基线提交：`7c5a9b5`（`origin/main`）
- 用户指定的当前版本：`1.1.0`
- Phase 0 统一目标版本：`1.2.0`
- 当前执行位置：Phase-00-01 进行中，尚未达到完成定义，Phase-00-02 至 Phase-00-05 均未开始

## 1. 已完成的准备工作

1. 已读取并遵循仓库根目录 `AGENTS.md`。
2. 已执行 `git fetch origin --prune --tags`；当时 `origin/main` 指向 `7c5a9b5`。
3. 根目录和 `origin/main` 均不存在 `VERSION`，仓库也不存在有效 SemVer tag；用户随后明确指定初始版本为 `1.1.0`。
4. 根据整个 Phase 0 将新增 Backend、Frontend、基础设施及开发入口的向后兼容功能，目标版本一次性确定为 `1.2.0`。
5. 已从最新 `origin/main` 创建 `develop/1.2.0`，未推送远端。
6. 已阅读 `Phase-00-总实施方案.md` 及 00-01 至 00-05 五份开发文件，并确认必须顺序实施。
7. 开始前发现用户拥有的未跟踪文件 `VSRSION`。该文件未被修改、覆盖、暂存或提交，恢复工作时必须继续保护。

## 2. Phase-00-01 当前实现状态

目前已起草但尚未提交以下文件：

- `.editorconfig`
- `.env.example`
- `.gitignore`
- `deploy/compose.yaml`
- `monitor/README.md`
- `router/README.md`
- `marshaller/README.md`
- `exporters/README.md`

已创建但 Git 不跟踪的空目录包括 `backend/`、`frontend/`、`scripts/`；后续批次会写入实际文件。

当前草案内容：

- `.gitignore` 忽略 `.env`、`.run/`、Frontend 依赖与构建产物、Backend 测试/构建产物及常见编辑器文件。
- `.env.example` 提供 Phase 0 所需本地开发变量和仅用于本地开发的默认凭据。
- Compose 仅包含 `mysql`、`redis`、`rabbitmq`，使用固定镜像版本、独立具名卷、healthcheck 和 `unless-stopped`。
- Compose 必需变量使用缺失即失败的参数展开。
- MySQL、Redis 和 RabbitMQ 宿主机端口可由环境变量覆盖；`.env.example` 仍使用方案要求的默认端口。
- 四个未来组件目录仅包含职责说明，没有提前实现代码。

当前运行状态：

- Windows Docker Desktop Engine 已恢复可用。
- Windows Docker Desktop 中的 GoPulse MySQL、Redis、RabbitMQ 容器正在运行，三者均为 `healthy`。
- 当前发布端口为 MySQL `13306`、Redis `16379`、RabbitMQ AMQP `5672`、RabbitMQ Management `15672`。
- Ubuntu WSL 中曾成功启动同一 Compose 项目并使三项服务全部进入 `healthy`；随后已执行普通 `down`，容器和网络已移除，三个具名卷仍保留。

尚未完成：

- RabbitMQ Management 页面的实际 HTTP 访问和登录检查。
- MySQL、Redis、RabbitMQ 的直接客户端连通性检查。
- 在 Windows Docker Desktop 中执行普通停止、确认具名卷保留、重新启动并再次确认 `healthy` 的完整生命周期验收。
- 使用默认 `3306` 和 `6379` 端口执行端口冲突失败场景验证。
- `dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md`。
- Phase-00-01 的独立 Conventional Commit。
- 对当前草案的最终主 Agent 审查和必要修正。

## 3. 已实际执行的检查

### 3.1 工具链

2026-08-31 环境复核实际检测到：

- Git：`2.51.1.windows.1`
- Go：`1.26.1 windows/amd64`
- Node.js：`24.14.1`
- npm：`11.11.0`
- Windows Docker CLI / Engine：`29.4.0`
- Windows Docker Compose：`5.1.1`
- Bash：`5.2.21`
- Ubuntu WSL Docker Engine：`29.3.0`
- Ubuntu WSL Docker Compose：`5.1.0`
- Ray-Work WSL Docker Engine：`29.5.3`
- Ray-Work WSL Docker Compose：`5.1.4`

### 3.2 Compose 静态检查

以下命令已成功：

```powershell
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml config --quiet
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml config --services
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml config --volumes
```

确认结果：

- 服务仅为 `mysql`、`rabbitmq`、`redis`。
- 具名卷为 `gopulse_mysql_data`、`gopulse_redis_data`、`gopulse_rabbitmq_data`。
- 当前本地 `.env` 渲染出的端口为 `13306`、`16379`、`5672`、`15672`。
- `.env.example` 保留的标准默认端口为 `3306`、`6379`、`5672`、`15672`。
- 镜像渲染为 `mysql:8.4.0`、`redis:7.2.5-alpine`、`rabbitmq:3.13.3-management-alpine`。

使用删除了 `MYSQL_USER` 的临时 env 文件运行 Compose 配置时，命令按预期以退出码 1 失败，并明确报告 `MYSQL_USER is required`。

以下忽略检查已成功：

```powershell
git check-ignore -v .env .run/test frontend/node_modules/pkg frontend/dist/index.html
```

### 3.3 Ubuntu WSL 运行检查

已实际通过 Ubuntu WSL 的独立 Docker Engine 执行：

```powershell
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/GoPulse && docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml up -d --wait --wait-timeout 240'
```

实际结果：

- 三个固定版本镜像成功拉取。
- MySQL、Redis、RabbitMQ 容器均成功启动并进入 `healthy`。
- 发布端口为 `13306`、`16379`、`5672`、`15672`。
- 随后执行普通 `docker compose down`，容器和网络被移除。
- 普通 `down` 后，Ubuntu Docker Engine 中的 `gopulse_mysql_data`、`gopulse_redis_data`、`gopulse_rabbitmq_data` 三个具名卷仍然存在。
- Ubuntu WSL 当前没有运行 GoPulse 容器。

说明：初次拉取镜像速度很慢，但最终完成。Ubuntu WSL 的 Docker Engine 与 Docker Desktop 是两个独立 Engine，其镜像、容器和具名卷不共享。

### 3.4 Windows Docker Desktop 运行检查

2026-08-31 15:31 左右，Docker Desktop 用户态进程启动，Windows CLI 的 `desktop-linux` context 恢复连接。实际检查成功：

```powershell
docker desktop status
docker version --format 'Client={{.Client.Version}} Server={{.Server.Version}} OS={{.Server.Os}}'
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml up -d --wait --wait-timeout 300
docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml ps -a
```

确认结果：

- `docker desktop status` 返回 `running`。
- Windows Docker Client 和 Server 均为 `29.4.0`，Server OS 为 Linux。
- Compose 启动退出码为 0。
- MySQL、Redis、RabbitMQ 均进入 `healthy`。
- 2026-08-31 16:29 复核时，三个容器已连续运行约 48 分钟且仍为 `healthy`。
- 当前 Windows Docker Desktop 中的 GoPulse 容器保持运行，供后续连通性和生命周期验收使用。

## 4. 当前环境结论

### 4.1 Windows Docker Desktop 已恢复可用

Windows Docker CLI 当前可连接：

```text
Context: desktop-linux
Client: 29.4.0
Server: 29.4.0
Docker Desktop status: running
```

`com.docker.service` 仍显示 `Stopped` 且启动类型为 `Manual`，但 Docker Desktop 的用户态进程和 Linux Engine 已正常运行，因此该服务状态当前不构成阻塞。除非 Engine 再次失联，不应仅因为该 Windows Service 为 `Stopped` 就判定 Docker Desktop 不可用。

后续 Phase-00-01 验收应优先使用 Windows Docker Desktop，以符合 Windows 开发环境要求。

### 4.2 WSL Docker 可作为独立回退环境

`Ubuntu` 与 `Ray-Work` WSL 发行版均有可工作的独立 Docker Engine。若 Docker Desktop 再次失联，可以使用以下方式进行隔离排查：

```powershell
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/GoPulse && docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml ...'
```

但必须注意：WSL 独立 Engine 与 Docker Desktop 不共享镜像、容器和卷；切换 Engine 前应先停止另一侧的 GoPulse Compose 项目，避免端口占用和状态判断混淆。

### 4.3 默认端口被用户环境占用

Windows 宿主机当前已有服务监听：

- `3306`：`MySQL84` / `mysqld.exe`
- `6379`：`Redis` / `redis-server.exe`

这些是预先存在的外部服务，未被停止或修改，后续也不得为了测试而直接终止。默认端口冲突本身可作为端口冲突失败场景验证。

为安全运行 Compose 验收，当前忽略的根目录 `.env` 使用：

```text
MYSQL_PORT=13306
REDIS_PORT=16379
RABBITMQ_PORT=5672
RABBITMQ_MANAGEMENT_PORT=15672
```

`.env.example` 仍保留标准默认端口。根目录 `.env` 已被 `.gitignore` 忽略，不得提交。

## 5. Git 状态与提交边界

更新本交接记录前，工作树显示以下未跟踪项目：

```text
.editorconfig
.env.example
.gitignore
deploy/
exporters/
marshaller/
monitor/
router/
VSRSION
```

其中 `VSRSION` 是用户拥有的预先存在文件，必须永远排除在 Phase 0 的暂存和提交之外。

Phase 批次实现提交目前为零。`handleoff.md` 是暂停点说明，应作为独立文档提交；它不表示 Phase-00-01 已完成，也不替代对应实施日志。

## 6. Subagent 使用情况

曾调用 subagent 对总体方案及 Phase-00-01 做只读审查或受限实现，但三次调用均以 `completed: null` 结束，没有返回可审查报告或可用修改。因此当前文件草案由主 Agent 完成，未把任何未经审查的 subagent 结果视为已完成工作。

恢复后若用户再次明确要求使用 subagent，应先分配小而明确、写入范围不重叠的检查或实现任务；主 Agent 必须检查其实际输出。

## 7. 建议恢复顺序

1. 阅读 `AGENTS.md`、本文件、Phase-00 总方案和 Phase-00-01 文件。
2. 执行 `git status --short --branch`，确认位于 `develop/1.2.0`，继续保护 `VSRSION`。
3. 审查当前八个 Phase-00-01 草案文件，不要假定其已完成。
4. 确认 Docker Desktop 为 `running`，并核对当前三个 Windows Compose 容器仍为 `healthy`。
5. 检查 MySQL、Redis、RabbitMQ 的直接客户端连通性，以及 RabbitMQ Management 页面和登录。
6. 对 Windows Compose 执行普通 `down`，确认三个具名卷仍存在，再次启动并确认复用和 `healthy`。
7. 使用临时 env 文件恢复默认 `3306` 和 `6379`，验证现有 MySQL/Redis 监听导致的明确端口冲突失败，但不要停止或误杀外部服务；完成后恢复当前安全端口配置。
8. 恢复到已知运行或停止状态，并只把实际执行结果写入 Phase-00-01 实施日志。
9. 仅暂存 Phase-00-01 文件及其日志，排除 `VSRSION` 和本地 `.env`，然后创建独立英文 Conventional Commit。
10. 主 Agent 确认 Phase-00-01 前置条件全部满足后，才进入 Phase-00-02。
11. 后续继续严格按 00-02 → 00-03 → 00-04 → 00-05 顺序执行；仅在 00-05 收口时把根目录 `VERSION` 写为 `1.2.0`。

## 8. 重要限制

- 分支尚未推送，未创建 Pull Request、Git tag 或远端分支。
- `VERSION` 尚未创建或修改；最终版本更新只能在整个 Phase 0 收口时完成。
- Windows Docker Desktop 当前可用，但 Phase-00-01 的管理页面、直接客户端连通性、Windows 卷保留/重启和端口冲突验收仍未全部完成。
- Windows Docker Desktop 当前运行三个 GoPulse 容器；恢复工作时不要误判为外部用户容器，也不要使用 `down -v`。
- Ubuntu WSL 当前没有运行 GoPulse 容器，但保留了本次检查创建的三个独立具名卷。
- Phase-00-01 实施日志和实现提交仍未完成。
- Phase-00-02 至 00-05 尚未开始，不得越过 Phase-00-01 的阻塞项继续。
