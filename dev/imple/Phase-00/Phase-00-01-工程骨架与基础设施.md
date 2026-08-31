# Phase 0-01：工程骨架与基础设施实施方案

> 执行序号：1 / 5
> 前置批次：无
> 总方案来源：[Phase-00-总实施方案.md](Phase-00-总实施方案.md)

## 1. 批次目标

建立 Phase 0 后续工作的目录、配置和本地基础设施基线，使 MySQL、Redis、RabbitMQ 可以通过 Docker Compose 独立启动、健康检查和安全停止。

本批完成后，后续批次可以在不重复定义端口、凭据、服务名和数据卷的前提下，直接连接三项基础设施。

## 2. 前置条件

- 在仓库根目录执行本批工作。
- 本机已安装 Git、Docker，并支持 Compose v2 引入的 `docker compose` 子命令；后续兼容版本不因主版本号高于 2 而被拒绝。
- 默认端口 `3306`、`6379`、`5672`、`15672` 未被其他程序占用。
- 开始前记录仓库状态，不覆盖或提交与本批无关的用户改动。

## 3. 实施范围

### 3.1 工程骨架

建立或补齐以下结构：

```text
gopulse/
├── frontend/                 # 第 3 批实现
├── backend/                  # 第 2 批实现
├── monitor/                  # 后续阶段组件职责说明
├── router/                   # 后续阶段组件职责说明
├── marshaller/               # 后续阶段组件职责说明
├── exporters/                # 后续阶段组件职责说明
├── deploy/                   # 本地 Docker Compose
├── scripts/                  # 第 4、5 批实现
├── dev/                      # 阶段方案、实施批次和记录
├── docs/                     # 架构文档
├── .env.example
├── .editorconfig
├── .gitignore
└── README.md                 # 第 5 批完善
```

尚无实际代码的目录使用职责说明或占位文件保留，不为后续组件提前创建实现代码。

### 3.2 基础配置

- `.editorconfig` 统一基础缩进、换行和文件末尾换行规则。
- `.gitignore` 至少忽略 `.env`、依赖目录、构建产物、`.run/` 运行时记录和常见编辑器文件。
- `.env.example` 提供可提交的本地开发默认值，至少包括：

```text
APP_ENV
HTTP_HOST
HTTP_PORT
MYSQL_HOST
MYSQL_PORT
MYSQL_DATABASE
MYSQL_USER
MYSQL_PASSWORD
MYSQL_ROOT_PASSWORD
REDIS_HOST
REDIS_PORT
REDIS_PASSWORD
REDIS_DB
RABBITMQ_USER
RABBITMQ_PASSWORD
RABBITMQ_URL
```

- `.env` 由开发者本地使用，不提交到 Git。
- 本地凭据只能用于开发环境，不声明为生产级配置。
- `MYSQL_ROOT_PASSWORD` 只用于 MySQL 容器初始化；Backend 使用 `MYSQL_USER` 与 `MYSQL_PASSWORD`。
- `RABBITMQ_USER`、`RABBITMQ_PASSWORD` 用于容器初始化，必须与 Backend 使用的 `RABBITMQ_URL` 保持一致；凭据写入 URL 时必须执行 URL 编码。
- `.env.example` 为每个必需变量提供明确的本地默认值；Compose 文件对必需变量使用缺失即失败的参数展开，不在 Compose 中再隐式回退到另一组凭据。

### 3.3 Docker Compose

Compose 仅承载：

| 服务 | 宿主机端口 | 用途 |
| --- | ---: | --- |
| MySQL | 3306 | 数据库连接 |
| Redis | 6379 | 缓存连接 |
| RabbitMQ | 5672 | AMQP 连接 |
| RabbitMQ Management | 15672 | 管理控制台 |

每项服务必须具备：

- 明确、稳定的 Compose 服务名。
- 固定到明确版本的官方镜像，不使用无约束的 `latest`。
- 通过环境变量注入的本地开发配置和凭据。
- 容器级 healthcheck，并设置合理的启动宽限、间隔、超时和重试次数。
- 适合本地开发的重启策略。
- 独立具名数据卷。

具体要求：

- MySQL 初始化本地 `gopulse` 数据库，并通过数据库可用性命令执行健康检查。
- Redis 启用持久数据卷；如配置密码，healthcheck 必须使用同一密码。
- RabbitMQ 使用 management 镜像，同时开放 AMQP 与管理端口，healthcheck 使用 RabbitMQ 自身诊断命令。
- 普通 `docker compose down` 不删除具名卷；只有显式清理命令才允许附带删除卷参数。
- 所有手工命令和后续脚本都必须显式指定根目录 `.env`、`deploy/compose.yaml` 和稳定的 Compose project name `gopulse`，不依赖调用者当前目录或 Compose 的隐式项目名推导。

## 4. 明确不做的内容

- 不创建 Go module 或 Backend 实现。
- 不创建 Vue、Vite 或 Frontend 实现。
- 不实现 `dev`、`down`、`verify` 脚本。
- 不创建业务表、数据库迁移、缓存键或 RabbitMQ 业务 exchange/queue。
- 不创建 Frontend 或 Backend Dockerfile。
- 不引入 Kafka、Elasticsearch、VictoriaMetrics、Kubernetes 或 CI。

## 5. 目标文件和目录

本批预计新增或修改：

```text
.env.example
.editorconfig
.gitignore
deploy/compose.yaml
monitor/README.md
router/README.md
marshaller/README.md
exporters/README.md
dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md
```

若仓库已有同用途文件，应在保留现有有效规则的基础上增量修改，不直接覆盖。

## 6. 详细实施步骤

1. 检查当前仓库结构和 Git 状态，确认已有文件及用户改动。
2. 创建 Backend、Frontend、部署、脚本和后续组件所需目录；空目录使用说明文件保留。
3. 为 `monitor`、`router`、`marshaller`、`exporters` 编写简短职责说明，明确本阶段不实现代码。
4. 补充 `.editorconfig` 与 `.gitignore`，尤其忽略 `.env`、`.run/`、`node_modules/` 和构建产物。
5. 编写 `.env.example`，保证 Compose 与后续应用使用同一套变量名和默认端口。
6. 编写 `deploy/compose.yaml`，声明三个服务、端口、环境变量、健康检查和具名卷。
7. 使用显式 `--project-name`、`--env-file` 和 `-f` 参数的 Compose 配置渲染命令检查语法、变量替换和最终端口。
8. 启动三项服务并等待全部进入 healthy。
9. 验证 RabbitMQ 管理台和三项服务端口。
10. 执行普通停止，确认容器被移除但具名卷保留；重新启动确认数据卷可复用。
11. 在对应的 `dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md` 中记录本批实际完成内容、变更文件、验证命令与结果、偏差和已知限制。

## 7. 测试与验收标准

### 7.1 静态检查

- Compose 配置可以成功渲染，不存在未解析变量或重复端口。
- `.env.example` 包含总方案要求的全部环境变量。
- MySQL 与 RabbitMQ 的容器初始化凭据和 Backend 连接凭据可以明确对应，不存在两组默认值。
- `.env`、`.run/` 和本地依赖/构建产物不会被 Git 跟踪。
- Compose 中不存在 Frontend、Backend 或超出 Phase 0 的基础设施。

### 7.2 运行验收

从干净的本地基础设施状态开始：

1. 根据 `.env.example` 创建本地 `.env`。
2. 启动 Compose。
3. MySQL、Redis、RabbitMQ 均在限定时间内进入 `healthy`。
4. RabbitMQ 管理台可通过 `http://localhost:15672` 访问。
5. 三项服务的宿主机端口与文档一致。
6. 停止 Compose 后具名卷仍存在。
7. 再次启动后服务仍能正常进入 `healthy`。

### 7.3 失败场景

- 端口被占用时，启动失败信息能够定位具体服务和端口。
- 配置缺失时，Compose 配置渲染或启动明确失败，而不是使用不可预期值。
- healthcheck 失败时，可以通过 Compose 状态和容器日志定位失败服务。

## 8. 完成定义

满足以下条件才可结束本批：

- 工程骨架和占位职责说明已建立。
- `.env.example`、忽略规则和 Compose 配置一致。
- 三项基础设施可以独立启动并全部 healthy。
- 普通停止不会删除数据卷。
- 验收命令执行成功，相关结果已记录。
- 对应实施记录已创建且只记载实际执行的工作和检查。
- 仅提交本批文件，不包含用户已有改动。

## 9. 下一批次交接条件

交付给 Phase 0-02 前必须提供：

- Compose 服务名及稳定端口。
- Backend 所需环境变量清单和本地默认值。
- 可正常工作的 MySQL、Redis、RabbitMQ 本地连接方式。
- 三项服务均通过 healthcheck 的验证结果。

Phase 0-02 只能在上述交接条件满足后开始。
