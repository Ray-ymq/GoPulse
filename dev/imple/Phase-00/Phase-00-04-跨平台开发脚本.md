# Phase 0-04：跨平台开发脚本实施方案

> 执行序号：4 / 5
> 前置批次：Phase 0-01 至 Phase 0-03 已完成并通过验收
> 总方案来源：[Phase-00-总实施方案.md](../../phases/Phase-00/Phase-00-总实施方案.md)

## 1. 批次目标

提供语义一致的 PowerShell 与 Bash 开发脚本，把基础设施、Backend 和 Frontend 组合成一条命令可启动、可中断、可再次清理的本地开发环境。

本批重点是跨平台进程所有权、环境变量注入、错误诊断和资源生命周期，不新增应用功能。

## 2. 前置条件

- Compose 基础设施已稳定并全部通过 healthcheck。
- Backend 和 Frontend 均可从命令行独立启动、退出和联调。
- Backend 仅从进程环境变量读取配置。
- Frontend 已有固定的 npm 安装与开发命令。
- 本机至少具备 PowerShell 或 Bash 环境用于当前平台验收；最终提交前两套脚本都要经过对应 shell 的语法检查。

## 3. 实施范围

### 3.1 脚本入口

```text
scripts/dev.ps1
scripts/dev.sh
scripts/down.ps1
scripts/down.sh
```

Windows：

```powershell
.\scripts\dev.ps1
```

Unix：

```bash
./scripts/dev.sh
```

脚本必须根据脚本文件自身路径解析仓库根目录，不能假定调用者当前位于仓库根目录。

### 3.2 `dev` 行为

按固定顺序执行：

1. 检查 Go、Node.js、npm、Docker 和 Docker Compose。
2. 检查所需端口是否已被非本项目进程占用，并输出可定位信息。
3. 若根目录 `.env` 不存在，从 `.env.example` 创建。
4. 读取 `.env`，把配置注入 Compose 命令以及 Backend、Frontend 子进程环境。
5. 启动 MySQL、Redis、RabbitMQ。
6. 等待三个服务通过容器 healthcheck；超时后指出失败服务和诊断命令。
7. 若 Frontend 依赖未安装，优先按 lockfile 执行可重复安装。
8. 启动 Gin Backend 与 Vite Frontend，并记录进程信息。
9. 输出 Frontend、Backend、`/health`、`/ready` 和 RabbitMQ 管理台地址。
10. 在前台等待应用；收到 `Ctrl+C` 时停止本次脚本启动的应用进程。

`Ctrl+C` 后：

- 停止 Backend 和 Frontend，包括其明确属于本项目的子进程。
- 删除对应的有效 PID 文件。
- 保留 MySQL、Redis、RabbitMQ 容器运行。
- 不删除具名卷。

### 3.3 进程所有权与 `.run/`

运行时信息存放在仓库根目录：

```text
.run/
├── backend.pid
└── frontend.pid
```

规则：

- `.run/` 必须被 Git 忽略。
- PID 文件只在进程成功启动后写入。
- 写入应尽量原子化，避免留下半写文件。
- 读取 PID 后先检查进程是否存在，再验证命令行、工作目录或其他可用标识确属当前 GoPulse 应用。
- PID 不存在、格式非法、进程不存在或身份不匹配时，视为陈旧记录，只清理 PID 文件，不终止无关进程。
- PowerShell 与 Bash 使用各自平台可靠的进程组/子进程终止方式，结果语义保持一致。
- 脚本退出路径必须统一清理自身创建的运行时记录。

### 3.4 `down` 行为

`down` 按以下顺序执行：

1. 解析仓库根目录和 `.run/`。
2. 验证并停止记录中的 Frontend、Backend 进程。
3. 清理陈旧或已终止进程的 PID 文件。
4. 执行 Compose down。
5. 默认保留具名卷。
6. 输出实际停止内容；某项已停止时保持幂等，不把它当作致命错误。

本批不提供默认删除数据卷的行为。如未来增加清理参数，必须是显式、清楚命名的破坏性选项。

### 3.5 PowerShell/Bash 一致性

两套脚本必须保持以下一致：

- 执行顺序。
- 环境变量优先级。
- 成功与失败退出码。
- 健康等待超时语义。
- PID 验证和清理规则。
- `Ctrl+C` 与 `down` 的资源保留行为。
- 输出的服务地址和关键诊断信息。

不要求机械共享代码；优先使用各 shell 的安全原生能力。

## 4. 明确不做的内容

- 不修改 Backend 或 Frontend 业务行为。
- 不创建 Frontend、Backend Dockerfile，不把应用加入 Compose。
- 不实现 CI 或生产进程管理器。
- 不在脚本中创建业务 Schema、RabbitMQ 拓扑或测试数据。
- 不默认删除具名卷。
- 不通过模糊进程名批量终止 Go、Node 或 npm 进程。
- 本批不实现完整 `verify` 验收脚本；该工作属于第五批。

## 5. 目标文件和目录

```text
scripts/dev.ps1
scripts/dev.sh
scripts/down.ps1
scripts/down.sh
.run/                    # 运行时创建，不提交
```

必要时增量更新 `.gitignore`，确保 `.run/` 不被跟踪。

## 6. 详细实施步骤

1. 定义两套脚本共同的行为表、退出码和消息内容。
2. 实现仓库根目录定位和路径拼接，不使用易受空格影响的字符串命令。
3. 实现工具版本/可用性检查和明确错误输出。
4. 实现 `.env` 创建、解析和子进程环境注入；拒绝明显非法配置。
5. 实现 Compose 启动及基于容器 health 状态的等待逻辑。
6. 实现 Frontend 依赖检测，存在 lockfile 时使用可重复安装方式。
7. 实现 Backend、Frontend 启动和 PID/进程身份记录。
8. 实现前台等待、应用提前退出检测和 `Ctrl+C` 清理。
9. 实现幂等 `down`，包括陈旧 PID、已退出进程和 Compose 未启动场景。
10. 分别在 PowerShell 与 Bash 环境执行语法、正常路径和失败路径验证。

## 7. 测试与验收标准

### 7.1 正常场景

- 从非仓库根目录调用脚本仍能正确运行。
- 首次运行时自动从 `.env.example` 创建 `.env`。
- 三项基础设施进入 healthy 后才启动应用。
- Frontend 与 Backend 均成功启动并生成有效 PID 文件。
- 所有服务地址被正确输出。
- `Ctrl+C` 停止两个应用，保留基础设施和数据卷。
- 再次运行 `dev` 可以正常启动，不遗留冲突。
- `down` 停止应用和 Compose，第二次执行仍成功或安全提示已停止。

### 7.2 失败场景

- 缺少 Go、Node、npm、Docker 或 Compose 时明确指出缺失项并非零退出。
- `.env.example` 缺失时不创建空 `.env`。
- 端口已被无关进程占用时，不误杀进程并明确失败。
- 任一容器未在时限内 healthy 时，不继续启动应用，并输出诊断命令。
- Backend 或 Frontend 启动后立即退出时，脚本报告具体应用和退出码，并清理自己启动的另一应用。
- PID 文件格式非法、PID 已复用或进程身份不匹配时，不终止无关进程。
- `.env` 中密码和完整连接串不被输出到日志。

### 7.3 跨平台检查

- PowerShell 脚本通过语法检查并在 Windows 执行核心场景。
- Bash 脚本通过 `bash -n`，并在 Unix/Git Bash/WSL 中执行核心场景。
- 两个平台的可观察行为、退出码和资源保留结果一致。
- Bash 脚本具有可执行权限或 README 明确提供兼容调用方式。

## 8. 完成定义

- `dev` 和 `down` 在 PowerShell、Bash 下语义一致。
- `.env` 能正确注入 Compose 与 Backend 子进程。
- 健康等待、进程记录、中断清理和幂等停止均已验证。
- 脚本不会因陈旧 PID 或宽泛进程名误杀无关进程。
- 默认停止流程保留基础设施数据卷。
- 不需要手工分别启动 Frontend 和 Backend 即可进入开发状态。

## 9. 下一批次交接条件

交付给 Phase 0-05 前必须提供：

- 两个平台稳定的 `dev`、`down` 入口。
- 可供验收脚本复用或观察的 Compose 服务名和访问地址。
- 明确的成功/失败退出码约定。
- 冷启动、重复启动、中断和停止的验证记录。
- 已知平台差异及其不影响语义一致性的说明。

Phase 0-05 只能验证和收口现有行为，不应通过放宽验收标准掩盖第四批缺陷。
