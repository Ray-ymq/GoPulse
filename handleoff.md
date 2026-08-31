# GoPulse Phase 0 暂停交接记录

- 记录时间：2026-08-31（Asia/Shanghai）
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

尚未完成：

- 三个容器的实际启动、healthy 等待、管理台/端口检查、普通停止、具名卷保留和重新启动验收。
- `dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md`。
- Phase-00-01 的独立 Conventional Commit。
- 对当前草案的最终主 Agent 审查和必要修正。

## 3. 已实际执行的检查

### 3.1 工具链

实际检测到：

- Git：`2.51.1.windows.1`
- Go：`1.26.1 windows/amd64`
- Node.js：`24.14.1`
- npm：`11.11.0`
- Windows Docker CLI：`29.4.0`
- Windows Docker Compose：`5.1.1`
- Bash：`5.2.21`
- Ubuntu WSL Docker Engine：`29.3.0`
- Ubuntu WSL Docker Compose：`5.1.0`

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
- 默认配置渲染出的端口为 `3306`、`6379`、`5672`、`15672`。
- 镜像渲染为 `mysql:8.4.0`、`redis:7.2.5-alpine`、`rabbitmq:3.13.3-management-alpine`。

使用删除了 `MYSQL_USER` 的临时 env 文件运行 Compose 配置时，命令按预期以退出码 1 失败，并明确报告 `MYSQL_USER is required`。

以下忽略检查已成功：

```powershell
git check-ignore -v .env .run/test frontend/node_modules/pkg frontend/dist/index.html
```

### 3.3 尚未执行的运行检查

没有启动任何 GoPulse 容器。暂停时，Ubuntu WSL 中：

```text
docker compose ... ps -a
```

没有返回 GoPulse 服务。不得把 Phase-00-01 的运行验收记为通过。

## 4. 当前环境问题

### 4.1 Windows Docker Desktop 不可用

Windows CLI 无法连接：

```text
failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine
```

`com.docker.service` 处于 Stopped 状态，当前进程没有启动该服务的权限。尝试隐藏启动 Docker Desktop 后，Engine 在三分钟内仍未就绪。

### 4.2 WSL Docker 可用

`Ubuntu` 与 `Ray-Work` WSL 发行版均有可工作的独立 Docker Engine。后续可以优先使用 Ubuntu WSL 执行 Compose 和 Bash 运行验收：

```powershell
wsl -d Ubuntu -- bash -lc 'cd /mnt/e/GoPulse && docker compose --project-name gopulse --env-file .env -f deploy/compose.yaml ...'
```

### 4.3 默认端口被用户环境占用

Windows 宿主机当前已有服务监听：

- `3306`：`MySQL84` / `mysqld.exe`
- `6379`：`Redis` / `redis-server.exe`

这些是预先存在的外部服务，未被停止或修改，后续也不得为了测试而直接终止。默认端口冲突本身可作为端口冲突失败场景验证。

为安全运行后续 Compose 验收，当前忽略的根目录 `.env` 已临时改为：

```text
MYSQL_PORT=13306
REDIS_PORT=16379
RABBITMQ_PORT=5672
RABBITMQ_MANAGEMENT_PORT=15672
```

`.env.example` 仍保留标准默认端口。根目录 `.env` 已被 `.gitignore` 忽略，不得提交。

## 5. Git 状态与提交边界

暂停前的工作树应显示以下未跟踪项目：

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

Phase 批次实现提交目前为零。`handleoff.md` 是暂停点说明，应单独作为文档提交；它不表示 Phase-00-01 已完成，也不替代对应实施日志。

## 6. Subagent 使用情况

曾调用 subagent 对总体方案及 Phase-00-01 做只读审查或受限实现，但三次调用均以 `completed: null` 结束，没有返回可审查报告或可用修改。因此当前文件草案由主 Agent 完成，未把任何未经审查的 subagent 结果视为已完成工作。

恢复后仍应按用户要求使用 subagent，但先分配小而明确、写入范围不重叠的检查或实现任务；主 Agent 必须检查其实际输出。

## 7. 建议恢复顺序

1. 阅读 `AGENTS.md`、本文件、Phase-00 总方案和 Phase-00-01 文件。
2. 执行 `git status --short --branch`，确认位于 `develop/1.2.0`，继续保护 `VSRSION`。
3. 审查当前八个 Phase-00-01 草案文件，不要假定其已完成。
4. 使用 Ubuntu WSL 和当前忽略的 `.env` 启动 Compose；等待三个服务 healthy。
5. 检查 MySQL、Redis、RabbitMQ 端口及 RabbitMQ Management 页面。
6. 执行普通 Compose down，确认具名卷仍存在，再次启动并确认复用和 healthy。
7. 验证默认端口配置因现有 MySQL/Redis 监听而明确失败，但不要停止或误杀外部服务。
8. 恢复到已知状态，并只把实际执行结果写入 Phase-00-01 实施日志。
9. 仅暂存 Phase-00-01 文件及其日志，排除 `VSRSION` 和本地 `.env`，然后创建独立英文 Conventional Commit。
10. 主 Agent 确认 Phase-00-01 前置条件全部满足后，才进入 Phase-00-02。
11. 后续继续严格按 00-02 → 00-03 → 00-04 → 00-05 顺序执行；仅在 00-05 收口时把根目录 `VERSION` 写为 `1.2.0`。

## 8. 重要限制

- 分支尚未推送，未创建 Pull Request、Git tag 或远端分支。
- `VERSION` 尚未创建或修改；最终版本更新只能在整个 Phase 0 收口时完成。
- Phase-00-01 运行验收、实施日志和提交均未完成。
- Phase-00-02 至 00-05 尚未开始，不得越过 Phase-00-01 的阻塞项继续。
