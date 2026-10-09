# Phase-22 原生验证入口登记

本文件登记本批执行过的日常原生入口及仍保留的专项入口，避免把模块单测误记为跨进程验收。

## 已执行的原生入口

| 能力 | 原生承接 | 实际门禁 |
| --- | --- | --- |
| 业务注册、登录、发帖、评论、点赞 | Backend HTTP 集成用例及 `business_flow_integration_test.go` | `make integration SCOPE=business` |
| Outbox、RabbitMQ、通知、搜索 | 真实 test MySQL/RabbitMQ/Elasticsearch 与 Backend worker/search-indexer | `make integration SCOPE=business` |
| 告警状态、租约、恢复、审计 | `repository_integration_test.go` 与共享 MySQL 断言 | `make integration SCOPE=business` |
| 指标、日志、运行事件 | Redis exporter → Monitor → Router → Kafka → Marshaller → 查询 API | `make integration SCOPE=observe` |
| 观测权限失败 | `observability_flow_integration_test.go` 的管理员、普通用户、401/403 断言 | `make integration SCOPE=observe` |
| 依赖隔离与生命周期 | `devtools/internal/testenv` 的 test scope、锁、端口预检、候选版本项目和归属清理 | `make test MODULE=devtools`、Compose config、两条 integration scope |
| 迁移 CLI 状态与恢复 | `backend/cmd/migrate/main_integration_test.go` 的自有 MySQL 验收：空库并发、重复、锁超时、dirty/ahead、DDL 失败、v12 恢复、退出/脱敏/清理 | `INTEGRATION_TESTS=1 go -C backend test -tags=integration ./cmd/migrate -run '^TestMigrationStateIntegration$' -count=1 -v -timeout 6m`；`make integration SCOPE=business` 自动纳入 |

## 模块与工具门禁

以下模块均实际执行过一次 `make test MODULE=...` 并通过：`backend`、`monitor`、`router`、`marshaller`、`componentmetrics`、`exporters/redis`、`exporters/mysql`、`exporters/rabbitmq`、`exporters/elasticsearch`、`exporters/kafka`、`exporters/victoriametrics`。

工具门禁为：`make test MODULE=devtools`（原 Python 自测的等价用例已迁入该模块）、`bash -n scripts/verify-business.sh`、`bash scripts/verify-business.sh --self-test`。`make integration` 默认委托 business，`scripts/verify-business.sh --native` 也只委托该入口。

CI Integration 门禁在 migration 后执行 `go run ./cmd/search-reindex --if-missing`，再使用 `go test -p 1 -count=1 -tags=integration ./...`；这样与本批原生入口一致，并避免共享 Elasticsearch alias 与 MySQL named lock 的并行竞态。迁移测试另行创建带 owner label 的随机 MySQL 8.4.0 容器，不连接 business/dev 数据库，并在 `t.Log` 中输出 `gopulse.migration-state.v1` JSON 回执。

## 保留的专项入口与边界

`verify-monitor.sh`、`verify-router.sh`、`verify-marshaller.sh`、`verify-exporter.sh`、`verify-component-metrics.sh`、`verify-logs.sh`、`verify-events.sh`、`verify-alerts.sh`、`verify-plugin-state.sh` 及 Phase 20 链路/证据工具仍保留。它们继续承担容器安全、插件制品、重启持久化、故障恢复、长期评估、容量、完整 Trace 和发布证据等本批没有替代的专项检查；本批原生入口不宣称覆盖这些能力。

一次性 `scripts/ci/verify_migration_state.py` 已由上述 Go 集成测试等价承接后退役。Phase 18 定界已结束，`phase18_diagnostic.py` 及其自测随其独有的 null error-list、后端日志摘要/脱敏、负载窗口/慢请求汇总和 Outbox 变化/速率诊断能力退役；历史日志和原始证据仍保留原路径。容量、告警、角色、页面及发布工具没有因本批删除或声称被迁移测试覆盖。

## Phase-22-03 前端与本机浏览器入口

| 能力 | 原生承接 | 实际门禁 |
| --- | --- | --- |
| 用户与管理组件、请求状态、代理端口 | 两前端 Vitest、Vite 配置测试、typecheck/build | `make test MODULE=frontend`、`make test MODULE=admin-frontend`、两前端 `npm run typecheck` 与 `npm run build` |
| 业务真实浏览器链路 | `business.spec.ts`、`compose-business.spec.ts` 的 business 场景 | `make e2e SCOPE=business`；最终 3 个用例通过，排除两条需要外部 search seed 的用例 |
| 管理同源、Cookie、401/403、角色降级 | `admin-frontend.spec.ts` 三条固定用例 | `make e2e SCOPE=observe` 的第一组；最终 3 个用例通过 |
| 指标、日志、运行事件与插件状态页面 | `compose-observability.spec.ts` 的 admin 场景及真实 test Monitor/Router/Marshaller | `make e2e SCOPE=observe` 的第二组；最终 1 个用例通过，要求实际数据点并完成归属清理 |
| native 入口 | `devtools/internal/testenv` 的 test Compose、锁、源码进程、Vite 启停和 trace 保留 | `make e2e` 默认 business；未知 scope/端口失败；`scripts/verify-admin-frontend.sh --native` 仅委托两前端检查 |

本批 native 浏览器不替代 Nginx 头、UID、只读根、镜像扫描、插件制品、恢复和容量专项检查；这些仍由既有容器工具保留。

## Phase-23-03 本机开发与观测入口的原生承接

`make deps`、`make dev`、`make dev-observe`、`make stop`、`make integration`
与 `make e2e` 已由 `devtools`（Go 模块，组件 Makefile 提供 `test`/`race`/`check`/`build`）
承接。
该助手持有工作区身份、私有状态与环境文件、源码进程归属判定、Compose 项目生命周期
以及按需准备/复用内容寻址的 Monitor 镜像；`stop` 仅在记录的进程身份仍然匹配时终止
进程，并且从不下沉 `--volumes`。独立的 Monitor 镜像准备目标与其两个 CI 步骤已取消：
观测入口在需要时按内容摘要 tag 准备或复用镜像。原 Python 编排脚本及其自测已整体退役，
仓库内不再有 Python 编排实现，其等价用例迁入 `devtools` 模块。

本批开发态门禁为：`make test MODULE=devtools`、`make race MODULE=devtools`、
`make check MODULE=devtools`、`python3 -m unittest discover -s scripts/ci -p 'test_*.py'`，
以及真实执行的 `make deps`（两次）、`make dev`（两次）、`make stop`、
`make dev-observe`、两条 `make integration`、两条 `make e2e` 与失败注入（未归属端口、
被占用的测试端口、非法 `VERSION`、非法 `HTTP_PORT`、未知 scope）。

## Phase-22-04 CI 选择与保留映射

`scripts/ci/quality_scope.py` 使用目标分支与主线共同祖先的完整 diff，按实际
路径选择下列已有入口；空 diff 和文档-only diff 只运行治理检查。

| 变更类别 | 选择的日常门禁 |
| --- | --- |
| Backend、业务 HTTP 或本地 Go replace 依赖 | 对应 Backend/消费者模块 Go 测试；必要时 `make integration SCOPE=business` 和 business 浏览器 |
| `internal/observability`、Metrics/Logs/Events/alert/query、Monitor/Router/Marshaller 或六个 Exporter | 对应模块 Go 测试；`make integration SCOPE=observe`；管理代理/权限/入口变化再选 `make e2e SCOPE=observe` |
| `componentmetrics` | componentmetrics 及所有实际 `replace` 消费者，不只测试共享库；观测真实链路按路径选择 |
| 用户 Frontend | Frontend Vitest/build 和 `make e2e SCOPE=business`；代理或管理入口变化同时选择 observe 浏览器 |
| Admin Frontend | Admin Frontend Vitest/build 和 `make e2e SCOPE=observe` |
| `deploy/`、Dockerfile 或 Compose/action 输入 | 既有 Compose/container gate；不把普通源码测试升级为完整镜像矩阵 |
| `scripts/` 工具 | 工具 Python unittest、Bash 语法和该工具拥有的原生入口；旧工具调用保持可达 |
| 未知非文档路径 | 保守选择完整产品检查，不静默跳过 |

CI 为 backend、router、marshaller、monitor、componentmetrics 和六个 Exporter
提供独立 Go job；业务与观测集成分别调用 `make integration SCOPE=business|observe`，
浏览器按选择调用对应 `make e2e`。Monitor/plugin 镜像只在观测门禁准备步骤中显式
构建或复用；日常 Go、文档和普通页面改动不预热全部 Compose 镜像。

以下专项入口及其独有断言继续保留：`verify-compose.sh`、
`verify-compose-observability.sh`、`verify-monitor.sh`、`verify-router.sh`、
`verify-marshaller.sh`、`verify-exporter.sh`、`verify-component-metrics.sh`、
`verify-logs.sh`、`verify-events.sh`、`verify-alerts.sh`、
`verify-plugin-state.sh`，以及 Phase 18～20 的容量、恢复、长期评估、制品和
证据工具。它们不因本机入口可用而从 CI 或仓库删除；持续告警周期、容器安全、
完整插件生命周期、故障恢复、容量、Bundle 和发布证据仍按各自计划触发。
