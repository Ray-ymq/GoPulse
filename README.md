# GoPulse

GoPulse 是社交内容应用，提供注册登录、用户资料、关注、帖子、评论、点赞、收藏、搜索和异步通知。用户前端采用纸墨主题，通过业务 API 访问数据。超级管理员可以在同一前端查询账号角色，并授予或撤销超级管理员权限。

## 模块与技术

| 路径 | 技术与职责 |
| --- | --- |
| `backend/business/` | Go、Gin；提供 API、Outbox 分发、通知任务、搜索索引及数据库迁移 |
| `frontend/web/` | Vue 3、Vue Router、TypeScript、Vite；提供用户界面和超级管理员角色管理页面 |
| `tools/ci/` | Python 与 Node.js；执行模块选择、提交校验及工具测试 |

MySQL 保存业务数据，Redis 提供缓存和共享限流，RabbitMQ 传递异步任务，Elasticsearch 提供搜索。模块清单以根 [Makefile](Makefile) 为准，产品版本以 [VERSION](VERSION) 为准。详细技术说明见 [技术栈说明](docs/GoPulse%20技术栈.md)。

## 目录导航

```text
GoPulse/
├── backend/          # 后端说明与业务 Go 模块
├── frontend/web/     # Vue 用户前端与角色管理页面
├── tools/ci/         # CI 模块选择、验证与提交规范工具
├── tools/make/       # 共享 Makefile 片段
├── docs/             # 技术与设计资料
├── .github/          # GitHub Actions 与 PR 模板
├── .githooks/        # Git 提交钩子
├── .env.example      # 配置示例
├── Makefile          # 模块清单与统一验证入口
└── VERSION           # 产品版本
```

后端目录、进程入口和维护操作见 [后端说明](backend/README.md)。前端开发与交互说明见 [前端说明](frontend/web/README.md)。

## 开发准备

源码开发需要 Go 1.26、Node.js 24.20.0 或更新版本、Python 3.12 或更新版本，以及 Git 和 Make。单机容器构建与部署使用 Docker Compose，步骤见 [容器部署说明](deploy/README.md)。仓库不提供压测工具。

1. 自行准备 MySQL、Redis、RabbitMQ 和 Elasticsearch，并创建应用使用的数据库与账号。
2. 仅在 `.env` 不存在时复制配置示例，避免覆盖已有配置：

   ```sh
   test -f .env || cp .env.example .env
   ```

3. 按实际依赖修改 `.env`。示例凭据仅用于本地开发，不得用于生产环境。JWT 密钥至少需要 32 字节；非本地开发和测试环境必须设置 `AUTH_COOKIE_SECURE=true`。
4. 安装前端依赖：

   ```sh
   make -C frontend/web install
   ```

`.env` 已被 Git 忽略。后端只读取进程环境变量，不会自动加载 `.env`。在每个运行后端命令的终端中，从仓库根目录加载配置；包含空格、`$` 或其他 shell 特殊字符的值需按 shell 语法正确引用：

```sh
set -a
. ./.env
set +a
cd backend/business
```

示例使用 `GOPULSE_RUNTIME_MODE=host`，API 和依赖地址必须指向本机回环地址。自行准备容器部署时可使用 `container` 模式：API 监听地址须为未指定地址，依赖主机须为服务 DNS 名称。Worker 和 Indexer 的健康检查地址需要分别配置。

## 数据库与进程

以下命令在已加载环境变量的 `backend/business/` 目录执行。

升级已有数据库前，先备份需要保留的数据。迁移 `000021_remove_observability` 会删除三张告警表；降级只能恢复表结构，不能恢复已删除的数据。迁移 `down` 仅用于本地开发，产品数据恢复需要备份与恢复流程。

```sh
go run ./cmd/migrate validate
go run ./cmd/migrate status
go run ./cmd/migrate up
```

首次使用搜索前，在 MySQL 和 Elasticsearch 可用时初始化搜索索引；已有搜索别名时此命令跳过重建：

```sh
go run ./cmd/search-reindex --if-missing
```

分别在三个终端加载配置并启动进程，使用 Ctrl+C 停止：

| 命令 | 职责 | 默认监听地址 |
| --- | --- | --- |
| `go run ./cmd/server` | 业务 API 与 Outbox 分发 | `127.0.0.1:8080` |
| `go run ./cmd/business-worker` | 消费 RabbitMQ 消息并处理通知 | 健康检查 `127.0.0.1:19102` |
| `go run ./cmd/search-indexer` | 消费 RabbitMQ 消息并更新 Elasticsearch 索引 | 健康检查 `127.0.0.1:19103` |

进程提供 `/startup`、`/live`、`/health` 和 `/ready` 探针。`/ready` 检查必要依赖。日志通过标准库 `slog` 输出 JSON。

完整业务入口见 [后端说明](backend/README.md)，HTTP 契约见 [OpenAPI](backend/business/api/openapi.yaml)。

## 启动前端

另开终端，在仓库根目录执行：

```sh
make -C frontend/web dev
```

默认访问 `http://127.0.0.1:5173`。Vite 从根 `.env` 读取 `FRONTEND_PORT`、`HTTP_PORT` 和 `GOPULSE_API_TARGET`。`GOPULSE_API_TARGET` 留空时，`/api` 代理到 `http://127.0.0.1:${HTTP_PORT:-8080}`。

登录与注册使用后端下发的 HttpOnly 会话 Cookie。部署静态产物时，需自行配置 `/api` 同源代理和前端路由回退。页面、草稿保存和浏览器验证说明见 [前端说明](frontend/web/README.md)。

## 管理员角色

先通过前端注册账号。系统维护者随后在已加载配置的 `backend/business/` 目录中，按该账号的用户 ID 设置初始超级管理员。执行前，将尖括号占位内容替换为实际 ID，并移除尖括号：

```sh
go run ./cmd/admin-role bootstrap --user-id <已注册用户的ID>
```

该账号重新登录或刷新页面后，导航会显示“角色管理”，页面地址为 `/admin/roles`。管理员可按完整用户名查找账号，确认后授予或撤销超级管理员权限。普通用户不能访问管理接口，初始超级管理员的权限不能撤销。非初始管理员撤销自己的权限后，页面返回首页。

初始管理员设置与日常角色管理的区别、接口和审计规则见 [后端角色管理说明](backend/README.md#管理员角色)。

## 验证与构建

在仓库根目录执行：

```sh
make ci-tools
make ci-check MODULE=backend/business
make ci-test MODULE=backend/business
make ci-build MODULE=backend/business
make check MODULE=frontend/web
make test MODULE=frontend/web
make build MODULE=frontend/web
```

`ci-check` 检查 Go 格式、静态分析和 lint；`ci-test` 运行带竞态检测的 Go 测试。前端构建产物位于 `frontend/web/dist/`。这些检查不能代替真实依赖联调。

使用 `make help` 查看入口。协作与提交要求见 [AGENTS.md](AGENTS.md) 和 [贡献说明](CONTRIBUTING.md)，CI 行为见 [CI 与提交规范](tools/ci/CI.md)。

## 当前范围

项目已移除独立管理前端、平台 API、插件管理、Monitor、Marshaller、Kafka、VictoriaMetrics、日志与事件查询、告警、指标埋点与采集接口，以及 OpenTelemetry 追踪。业务进程保留本地结构化日志、健康检查和退出管理。当前角色管理页面属于 `frontend/web/`，不依赖已移除的管理平台。历史迁移编号保持不变。
