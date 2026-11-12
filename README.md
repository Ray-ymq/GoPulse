# GoPulse

GoPulse 提供社交内容业务：注册登录、用户资料、关注、帖子、评论、点赞、收藏、搜索和异步通知。

## 技术与模块

| 目录 | 职责 |
| --- | --- |
| `backend/business` | Go、Gin API，MySQL 业务数据，Redis 缓存，RabbitMQ 异步任务，Elasticsearch 搜索 |
| `backend/runtimekit` | 共享日志、健康探针和进程退出 |
| `frontend/web` | Vue 3、TypeScript、Vite 用户前端 |
| `tools/devtools` | 开发环境、构建、部署和验证 |
| `tools/perflab` | 业务压测、数据核对和报告 |
| `tools/ci` | CI 模块选择和提交校验 |
| `deploy` | Docker、Compose、Kubernetes 部署配置 |

用户前端位于 `frontend/web/`，使用 TypeScript、Vue 3 和 Vite，采用纸墨主题。开发时先执行 `make -C frontend/web install`，再在独立终端执行 `make -C frontend/web dev`。`make dev` 当前只启动后端业务进程。完整操作见[前端说明](frontend/web/README.md)。

模块清单以根 Makefile 为准。详细版本见[技术栈说明](docs/GoPulse%20技术栈.md)。

## 开发与验证

要求 Go 1.26、Node.js 24（最低 24.20.0）、npm 11（最低 11.19.0）、Python 3.12 或更新版本，以及 Docker Compose v2、Buildx、Git 和 Make。

```sh
make doctor
make bootstrap
make dev
make stop
```

环境配置示例在 `.env.example`，本地实际配置存入 `.env`，不得提交敏感值。

```sh
make verify MODULE=backend/business
make test MODULE=frontend/web
make build MODULE=frontend/web
make build-images
```

部署入口见[部署说明](deploy/README.md)，压测入口见[压测说明](tools/perflab/flow/README.md)，协作规则见[贡献说明](CONTRIBUTING.md)。

## 可观测平台移除

产品已移除管理前端、平台 API、插件管理、Monitor、Marshaller、Kafka、VictoriaMetrics、日志和事件查询及告警。指标埋点、采集器、指标接口和 OpenTelemetry 追踪已一并移除。业务进程仅保留本地结构化日志、健康探针和退出管理。

历史迁移编号保持不变。迁移 `000021` 删除告警表；执行升级前应备份需要保留的告警历史。降级只能恢复表结构，无法恢复已删除数据。本次源码变更不操作已有容器、数据库或持久卷。
