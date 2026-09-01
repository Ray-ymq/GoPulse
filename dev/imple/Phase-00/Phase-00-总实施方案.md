# Phase 0：工程骨架实施方案

## 版本与分支规划

- Phase 版本线为 `0.1.x`，`0.1.0` 为阶段基线。
- 每个执行批次使用独立开发分支，patch 与批次编号一一对应。
- 下表是本阶段具体版本与分支分配的唯一实施依据；各批次实施文件不重复声明版本约束。

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-00-01 | `0.1.1` | `develop/0.1.1` | 已完成 |
| Phase-00-02 | `0.1.2` | `develop/0.1.2` | 已完成 |
| Phase-00-03 | `0.1.3` | `develop/0.1.3` | 已完成 |
| Phase-00-04 | `0.1.4` | `develop/0.1.4` | 已完成 |
| Phase-00-05 | `0.1.5` | `develop/0.1.5` | 已完成 |

若实施前调整批次数量或顺序，先更新本表，再创建尚未开始的开发分支；已经推送的分支不得静默改名或重新编号。

## 当前实施基线

- 根 `VERSION` 当前为 `0.1.5`，Phase-00-01 至 Phase-00-05 已全部合并到主线，对应实施记录均已存在。
- 已交付 Compose 基础设施、Backend `/health` 与 `/ready`、Frontend 连通性页面、跨平台 `dev`/`down`/`verify` 脚本及其自动化测试与集成验收。
- Phase 0 已完成阶段级验收和收口，没有剩余实施批次；后续开发从 Phase 1 的 `0.2.x` 版本线继续。
- 五个批次均按表中顺序从当时配置的主远程最新 `main` 创建独立开发分支，并在前置批次合并后开始下一批。
- 每批完成时均已将 `VERSION` 更新为本表分配的目标版本，并与对应实施记录一起提交。

## 1. 实施目标

建立 Vue 3 前端、Gin Backend、本地基础设施和跨平台开发脚本，形成以下最小可运行链路：

```text
Vue 3（5173）
  → Vite 开发代理
Gin Backend（8080）
  → MySQL（3306）
  → Redis（6379）
  → RabbitMQ（5672 / 管理台 15672）
```

应用在宿主机运行以保留热更新能力；Docker Compose 只承载基础设施，避免提前进入 Phase 12 的全面容器化。

本阶段完成后，开发者应能通过一条命令启动开发环境，在浏览器访问前端，并从页面和 Backend 就绪接口确认三项基础设施均可连接。

## 2. 工程结构与工具链

### 2.1 目标目录

```text
gopulse/
├── frontend/                 # Vue 3 + TypeScript + Vite
├── backend/                  # Go + Gin 单体 Backend
│   ├── cmd/server/           # Backend 程序入口
│   └── internal/
│       ├── config/           # 环境变量配置加载与校验
│       ├── http/             # 路由、Handler 与响应模型
│       └── platform/         # MySQL、Redis、RabbitMQ 连接适配
├── monitor/                  # 后续阶段组件占位与职责说明
├── router/                   # 后续阶段组件占位与职责说明
├── marshaller/               # 后续阶段组件占位与职责说明
├── exporters/                # 后续阶段组件占位与职责说明
├── deploy/                   # 本地 Docker Compose 配置
├── scripts/                  # Windows、Unix 开发及验收脚本
├── dev/                      # 阶段设计、实施批次与记录
├── docs/                     # 项目架构文档
├── .env.example              # 可提交的本地配置模板
├── .editorconfig
├── .gitignore
└── README.md                 # 本地开发入口
```

### 2.2 技术约束

- Frontend 使用 TypeScript、Vue 3、Vite 和 npm，并提交 `package-lock.json`。
- Backend 使用独立 Go module 和 Gin，并提交 `go.mod`、`go.sum`。
- 以当前开发环境的 Go 1.26、Node.js 24、npm 11 为开发基准。
- 依赖版本必须写入清单和 lockfile，禁止依赖未固定的全局脚手架。
- `monitor/`、`router/`、`marshaller/`、`exporters/` 只保存职责说明或占位文件，不实现组件代码。
- 本阶段不创建 Frontend 或 Backend Dockerfile。
- 本阶段不引入 Kafka、Elasticsearch、VictoriaMetrics 或 Kubernetes。

## 3. Backend 设计

### 3.1 服务入口

Backend 默认监听：

```text
0.0.0.0:8080
```

程序入口放在 `backend/cmd/server`。入口负责：

1. 加载并校验配置。
2. 初始化 MySQL 和 Redis 客户端。
3. 创建基础设施 readiness checker。
4. 注册 Gin 路由。
5. 启动 HTTP Server。
6. 进程退出时关闭已创建的客户端资源。

Phase 0 不建立业务表、数据库迁移、认证、业务 API 或消息消费者。

### 3.2 健康检查接口

#### `GET /health`

用途：确认 Backend 进程和 HTTP Server 存活。

- 不访问外部依赖。
- 正常时返回 HTTP 200。
- 响应示例：

```json
{
  "status": "ok",
  "service": "backend"
}
```

#### `GET /ready`

用途：确认 Backend 当前能够连接全部基础设施。

- 并行检查 MySQL、Redis、RabbitMQ。
- 全部正常时返回 HTTP 200。
- 任一检查异常或超时时返回 HTTP 503。
- 每项检查使用独立的短超时，避免单一依赖无限阻塞请求。
- 对外只返回 `up` 或 `down`，不得泄露密码、连接串或内部错误详情。
- 响应示例：

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

异常示例：

```json
{
  "status": "not_ready",
  "service": "backend",
  "checks": {
    "mysql": "up",
    "redis": "down",
    "rabbitmq": "up"
  }
}
```

### 3.3 基础设施检查方式

- MySQL：复用数据库连接池，通过 `PingContext` 检查连接。
- Redis：复用 Redis 客户端，通过 `PING` 检查连接。
- RabbitMQ：readiness 检查创建短生命周期 AMQP 连接，连接成功后立即关闭。
- 三项检查并行执行，总响应时间不得变成三个超时时间之和。
- 基础设施恢复后，`/ready` 应能够自动恢复为 HTTP 200，无需重启 Backend。

### 3.4 配置规则

配置全部从环境变量加载。至少包括：

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

规则如下：

- `.env.example` 保存可提交的本地开发默认值。
- `.env` 保存开发者本地配置并加入 `.gitignore`。
- 缺失必需配置、端口非法或 URL 无法解析时，Backend 输出明确错误并以非零状态退出。
- 日志和 HTTP 响应不得打印密码或完整连接串。

## 4. Frontend 设计

### 4.1 最小页面

首页只实现工程连通性展示，包含：

- GoPulse 项目标题。
- Backend 存活状态。
- MySQL 就绪状态。
- Redis 就绪状态。
- RabbitMQ 就绪状态。
- 手动刷新按钮。

页面加载时分别调用 `/health` 和 `/ready`，并区分：

- Backend 正常且全部依赖正常。
- Backend 正常但部分依赖不可用。
- Backend 无法访问。
- 请求正在进行。

### 4.2 开发代理

Vite 开发服务器将以下路径代理到 Backend：

```text
/health → http://localhost:8080/health
/ready  → http://localhost:8080/ready
```

Frontend 使用同源相对路径发起请求，本阶段不为开发环境额外引入 CORS 配置。

Frontend 默认监听端口为 `5173`。本阶段不实现 Vue Router、全局状态管理、登录或业务页面。

## 5. 本地基础设施

### 5.1 Docker Compose 服务

Compose 只提供以下服务：

- MySQL：创建本地 `gopulse` 数据库，使用具名卷保存数据。
- Redis：使用具名卷保存本地数据。
- RabbitMQ management：开放 AMQP 端口和管理控制台端口，使用具名卷保存数据。

默认端口：

| 服务 | 端口 | 用途 |
| --- | --- | --- |
| Frontend | 5173 | Vite 开发页面 |
| Backend | 8080 | HTTP API |
| MySQL | 3306 | 数据库连接 |
| Redis | 6379 | 缓存连接 |
| RabbitMQ | 5672 | AMQP 连接 |
| RabbitMQ Management | 15672 | 管理控制台 |

每个容器必须配置：

- 明确且稳定的服务名。
- 容器级 healthcheck。
- 适用于本地开发的重启策略。
- 通过环境变量注入的本地开发凭据。
- 具名数据卷。

执行普通停止命令时保留具名卷；只有显式清理命令才允许删除本地数据。

## 6. 统一开发命令

### 6.1 脚本入口

提供语义一致的 PowerShell 与 Bash 脚本：

```text
scripts/dev.ps1
scripts/dev.sh
scripts/down.ps1
scripts/down.sh
scripts/verify.ps1
scripts/verify.sh
```

Windows 启动命令：

```powershell
.\scripts\dev.ps1
```

Unix 启动命令：

```bash
./scripts/dev.sh
```

### 6.2 `dev` 行为

`dev` 脚本按固定顺序执行：

1. 检查 Go、Node.js、npm、Docker 和 Docker Compose 是否可用。
2. 若 `.env` 不存在，则从 `.env.example` 创建本地 `.env`。
3. 启动 MySQL、Redis、RabbitMQ。
4. 等待三个容器通过 healthcheck，超时后给出失败服务和诊断命令。
5. 若 Frontend 依赖尚未安装，则执行 npm 安装。
6. 在前台托管 Gin Backend 与 Vite Frontend。
7. 输出前端、Backend、健康接口和 RabbitMQ 管理台地址。

按下 `Ctrl+C` 时：

- 终止脚本启动的 Frontend 和 Backend 进程。
- 保留基础设施容器运行，以加快下次开发启动。
- 不删除数据卷。

### 6.3 `down` 与 `verify` 行为

`down`：

- 停止脚本托管的本地应用进程。
- 执行 Compose down。
- 默认保留具名卷。

`verify`：

- 检查三个 Compose 服务均为 healthy。
- 检查 `/health` 返回 HTTP 200。
- 检查 `/ready` 返回 HTTP 200，且三个 checks 均为 `up`。
- 检查 Frontend 首页可以通过 HTTP 访问。
- 任一检查失败时，以非零状态退出并明确指出失败项。

## 7. 测试方案

### 7.1 Backend 单元测试

- `/health` 返回 HTTP 200 和预期 JSON。
- 三项依赖全部正常时，`/ready` 返回 HTTP 200。
- 任一依赖异常时，`/ready` 返回 HTTP 503，并准确标记失败项。
- 多项依赖异常时，所有失败项均被标记。
- readiness 检查超时时能够结束请求。
- 响应不包含连接串、密码或原始内部错误。
- 环境变量默认值、覆盖值、缺失值和非法值校验正确。

依赖检查通过接口抽象注入 Handler，单元测试使用 fake checker，不依赖真实容器。

### 7.2 Frontend 测试

- TypeScript 类型检查通过。
- Frontend 生产构建通过。
- 使用模拟 HTTP 响应验证以下页面状态：
  - 全部正常。
  - 部分基础设施异常。
  - Backend 不可达。
  - 请求加载中。

### 7.3 集成验收

1. 从未启动环境执行一条 `dev` 命令。
2. 三个 Compose 服务进入 healthy。
3. `GET /health` 返回 HTTP 200。
4. `GET /ready` 返回 HTTP 200，三个 checks 均为 `up`。
5. 浏览器访问 `http://localhost:5173`，页面显示 Backend 和三项基础设施正常。
6. 停止任意一个基础设施容器后，`/health` 仍返回 HTTP 200。
7. 同一情况下，`/ready` 返回 HTTP 503，并正确标记失败项。
8. 恢复容器后，`/ready` 无需重启 Backend 即恢复为 HTTP 200。
9. 执行 `down` 后应用和容器停止，具名卷仍然保留。

## 8. 实施顺序

1. 建立顶层目录、忽略规则、配置模板和职责说明。
2. 建立 Compose 基础设施及 healthcheck。
3. 实现 Backend 配置加载和三项依赖连接检查。
4. 实现 `/health`、`/ready` 及 Backend 单元测试。
5. 建立 Vue 3 页面和 Vite 开发代理。
6. 实现 PowerShell 与 Bash 开发脚本。
7. 实现跨组件验收脚本。
8. 完善根 README 与 Phase 0 阶段记录。
9. 运行 Go 测试、Frontend 测试、类型检查、生产构建和集成验收。
10. 仅暂存 Phase 0 本次新增或修改的文件，按仓库规范创建 Conventional Commit。

## 9. 阶段边界与默认决策

- MySQL 是后续业务事实来源；Phase 0 只验证连接，不创建业务 Schema。
- Redis 在本阶段只验证连接，不设计缓存键或缓存策略。
- RabbitMQ 在本阶段只验证连接，不声明业务 exchange、queue 或 consumer。
- 本地开发凭据仅用于开发环境，不提供生产级密钥管理。
- 不增加 CI、代码生成、数据库迁移框架、完整日志体系或 Kubernetes 配置。
- 不提前实现 Phase 1 的 User、Post、Comment、Like 业务。
- 不提前实现 Phase 2 的 RabbitMQ 异步业务处理。
- 不提前实现 Phase 12 的 Frontend、Backend 容器化。
- Phase 1 在现有 Backend module 中按业务边界增加模块，并复用 Phase 0 的 MySQL、Redis 和配置入口。

## 10. 阶段级验收与完成条件

Phase 0 仅在以下条件全部满足时完成：

- Windows 和 Unix 均提供语义一致的 `dev`、`down`、`verify` 入口，未能实际运行的平台必须明确记录限制，不得写为已通过。
- 从冷状态执行一条 `dev` 命令后，MySQL、Redis、RabbitMQ 达到 healthy，Backend 与 Frontend 均可访问。
- `/health` 和 `/ready` 的正常、单项故障、多项故障与恢复语义通过端到端验收，Frontend 实际渲染状态与 Backend 一致。
- `Ctrl+C` 只停止本次启动的应用进程，`down` 可幂等停止应用与容器，两者都不默认删除具名卷。
- Backend 测试与 vet、Frontend 测试/类型检查/生产构建、脚本语法检查和必要的集成回归全部通过。
- Phase-00-01 至 Phase-00-05 的实施记录与实际工作一致，根 README 可作为新开发者的唯一启动入口。
- 根 `VERSION` 为 `0.1.5`，未引入 Phase 1 业务 Schema、API 或其他越界能力。
