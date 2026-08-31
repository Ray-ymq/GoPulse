# Phase 0-04：跨平台开发脚本实施记录

- 完成日期：2026-08-31（Asia/Shanghai）
- 分支：`develop/0.1.4`
- 目标版本：`0.1.4`
- 前置批次：Phase 0-01 至 Phase 0-03（`0.1.1` 至 `0.1.3`）
- 执行范围：Phase-00-04；未实现 Phase-00-05 的完整验收脚本

## 1. 实际完成内容

1. 新增 PowerShell 与 Bash 两套 `dev`、`down` 入口，均根据脚本自身位置解析仓库根目录，可从仓库外目录调用。
2. `dev` 检查 Go、Node.js、npm、Docker 和 Docker Compose；Bash 额外检查进程、锁、散列与进程组管理所需原生工具。`.env` 不存在时先使用 `.env.example` 完成配置预检，再复制生成，避免留下无效空文件。
3. 两个平台实现受限 dotenv 解析，只接受空行、整行注释和 `KEY=VALUE`，支持成对单引号或双引号值，拒绝 `export`、非法赋值、未闭合或嵌套引号及多行值。配置优先级固定为调用者环境、`.env`、脚本默认值，并校验 RabbitMQ URL 解码后的凭据与独立配置一致。
4. Compose 调用固定使用项目名 `gopulse`、根 `.env` 和 `deploy/compose.yaml`。启动前诊断端口占用；属于当前 Compose 项目且健康的基础设施端口允许复用。启动后等待 MySQL、Redis、RabbitMQ 全部通过 healthcheck，超时会输出失败服务与诊断命令。
5. Frontend 依赖根据 `package-lock.json` SHA256、Node 平台/架构指纹及 `npm ls` 共同判断；缺失或不一致时执行 `npm ci`，避免 Windows 与 WSL 共用 `node_modules` 时误判。
6. Backend 构建到 `.run/bin/` 后直接启动；Frontend 由 Node.js 直接执行项目内 Vite CLI。Frontend 子进程显式移除数据库、Redis、RabbitMQ 等配置，避免凭据透传。
7. `.run/dev.lock` 使用原子创建和平台锁防止并发启动。应用记录原子写入 PID、启动时间、可执行路径、工作目录和命令行标识；停止前同时验证这些身份信息，非法、陈旧或不匹配的记录不会触发误杀。
8. PowerShell 使用进程树终止能力，Bash 使用独立 session/process group。`Ctrl+C` 只清理本次 `dev` 启动的 Backend、Frontend、进程记录和运行锁，保留 Compose 容器及具名卷。
9. `down` 验证并停止记录中的应用进程，等待活动 `dev` 释放锁，执行不带 `-v` 的 Compose down；应用或容器已停止时保持幂等。
10. Bash 实测中修复了进程记录临时路径在同一 `local` 声明内提前展开的问题，并补齐 `main` 的显式失败传播与“仅清理本实例已启动进程”的保护，确保第二个 `dev.sh` 获取锁失败后立即退出且不影响首实例。
11. 根 `VERSION` 从 `0.1.3` 更新为 `0.1.4`。

## 2. 本批变更文件

- `scripts/dev.ps1`
- `scripts/down.ps1`
- `scripts/dev.sh`
- `scripts/down.sh`
- `dev/logs/Phase-00/Phase-00-04-跨平台开发脚本.md`
- `VERSION`

`.run/`、根 `.env`、`frontend/node_modules/` 和 `frontend/dist/` 均受现有忽略规则保护，未纳入版本控制。本批未修改 Backend、Frontend 业务代码、Compose 定义或 `.gitignore`。

## 3. 验证命令与结果

### 3.1 脚本语法

执行：

```powershell
# PowerShell AST 语法解析
[System.Management.Automation.Language.Parser]::ParseFile('scripts/dev.ps1', ...)
[System.Management.Automation.Language.Parser]::ParseFile('scripts/down.ps1', ...)

bash -n scripts/dev.sh
bash -n scripts/down.sh
```

结果：四个脚本均通过对应 shell 的语法检查。

### 3.2 Windows PowerShell 核心场景

从 `E:\` 调用 `E:\GoPulse\scripts\dev.ps1`，实际结果：

- 工具检查、端口检查、Compose 启动与三项 healthcheck 均通过。
- Frontend 首次依赖安装使用 `npm ci`；Backend 构建成功，Backend 与 Vite 均成功启动。
- `.run/backend.json`、`.run/frontend.json` 与 `.run/dev.lock` 生成成功，记录包含计划要求的身份字段。
- `GET /health` 返回 HTTP 200；`GET /ready` 返回 HTTP 200，MySQL、Redis、RabbitMQ 均为 `up`；Frontend 返回 HTTP 200。
- 第二次执行 `dev.ps1` 以退出码 1 安全失败，未删除首实例记录或停止首实例进程。
- `Ctrl+C` 后 Backend、Frontend、有效记录和运行锁被清理，三个 Compose 容器继续运行。
- `down.ps1` 停止并删除 Compose 容器但保留 `gopulse_mysql_data`、`gopulse_redis_data`、`gopulse_rabbitmq_data`；连续执行两次均返回 0。

### 3.3 WSL Bash 核心场景

从 `/tmp` 调用 `/mnt/e/GoPulse/scripts/dev.sh`。当前 WSL 发行版自带的 Docker daemon 被无关的持续重启容器拖慢，Docker API 与网络创建无法在合理时间内完成；为避免停止或修改用户的其他容器，本次 WSL 验证临时把 PATH 中的 `docker` 指向仅用于测试的 Docker Desktop CLI 路径转换桥接器。桥接器位于忽略的 `.run/`，不属于提交内容；Bash 脚本、Go、Node.js、npm、进程组和运行记录均实际运行在 WSL。

实际结果：

- 从非仓库目录正确定位仓库，Compose 三项服务达到 healthy。
- Node 平台指纹从 Windows 切换为 Linux 后执行一次 `npm ci`，后续调用正确复用依赖。
- Backend 与 Vite 启动成功；Bash JSON 记录包含 PID、启动时间、可执行路径、工作目录和命令行标识。
- WSL 内请求 `/health` 返回 HTTP 200；`/ready` 返回 HTTP 200 且三项依赖均为 `up`；Frontend 返回 HTTP 200。
- 第二个 `dev.sh` 输出已有会话错误并以退出码 1 结束；首实例 Backend/Frontend PID 保持不变，两个 HTTP 入口仍返回 200。
- `Ctrl+C` 停止 Backend 与 Frontend并删除记录和运行锁；Compose 容器保持 healthy。
- 从 `/tmp` 连续执行两次 `down.sh` 均返回 0；第一次停止并删除容器，第二次保持幂等；三项 `gopulse_*_data` 具名卷前后完全一致。

### 3.4 聚焦仓库检查

执行：

```powershell
git diff --check
git status --short --branch
git diff -- scripts VERSION dev/logs/Phase-00/Phase-00-04-跨平台开发脚本.md
```

结果：仅检查和提交本批四个脚本、实施记录与 `VERSION`。本批未修改应用代码，因此未重复运行 Backend/Frontend 完整测试套件，符合实施方案的比例化验证要求和本次“不做过度检查”的要求。

## 4. 偏差、限制与后续项

- 计划要求 Bash 在可记录的 Unix 环境执行核心场景；本次 WSL 的 Bash、Go、Node、npm 和进程管理均真实执行，但 Compose 通过临时 Docker Desktop CLI 桥接完成，原因是发行版原生 Docker daemon 被无关重启循环拖慢。未为完成验收而停止、删除或修改这些用户容器。
- PowerShell 与 Bash 最终可观察语义一致：重复启动安全失败、应用记录受到身份校验、`Ctrl+C` 保留基础设施、`down` 保留具名卷且幂等。
- 本批没有实现 Phase-00-05 的 `verify` 脚本，没有修改业务行为、创建应用 Dockerfile、删除数据卷或引入进程管理器。
- 后续 Phase-00-05 可直接复用固定 Compose project、服务名、端口、HTTP 地址和成功/失败退出码完成跨组件验收与 Phase 0 收口。
