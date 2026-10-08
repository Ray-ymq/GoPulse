# Phase-22-02：业务与可观测原生验证

> 状态：已完成。2026-10-08 修订。目标 `2.4.2`，分支 `develop/2.4.2`；依赖 01 已完成及本次规划修订进入 main。保留原方案文件路径，版本和分支不变。

## 1. 交付与允许文件

交付业务和可观测共同使用的原生验证入口：普通断言使用对应模块的 Go 测试，真实链路使用隔离依赖。证明注册/登录/发帖/评论/点赞经 Outbox/RabbitMQ 到通知与搜索；同时证明本次产生的指标、日志、运行事件经过观测链路后可查询，并证明告警数据库状态和租约语义。服务就绪、历史数据非空和测试跳过均不能代替这些结果。

允许文件：`Makefile`、`scripts/ci/local_development.py`、`scripts/ci/test_local_development.py`、`deploy/compose.local.yaml`、`deploy/compose.local-linux.yaml`、`backend/internal/integrationtest/environment.go`、`backend/cmd/server/roles_integration_test.go`、`backend/internal/http/business_flow_integration_test.go`、`backend/internal/http/observability_flow_integration_test.go`、`backend/internal/alert/repository_test.go`、`backend/internal/alert/repository_integration_test.go`、`monitor/internal/plugin/redis_config.go`、`monitor/internal/plugin/contract_v2_test.go`、`monitor/cmd/monitor/main.go`、`monitor/cmd/monitor/main_test.go`、`scripts/verify-business.sh`、`dev/validation/local-development-tests.md`；完成元数据 VERSION/.env.example/两前端 package.json/package-lock.json 与同名日志。`roles_integration_test.go` 仅调整为接受调用方提供的隔离测试依赖身份。`monitor/internal/plugin/redis_config.go` 仅适配 host 模式的 loopback 测试端口并保留合法端口范围校验；container 模式继续要求 `redis:6379`，对应测试只证明该边界。`monitor/cmd/monitor/main.go` 仅让 Redis 采集器使用配置中已校验的 `REDIS_EXPORTER_HTTP_PORT`，其它官方插件继续使用其目录端口；对应单测只保护该端口选择。两个 HTTP 链路文件和告警集成文件已实现为 Go 用例；新增测试环境清理只删除本批次确定创建的 observe 管理员及 bootstrap 记录，不新建 Python 断言框架或 Phase 执行器。Compose 仅补齐观测测试隔离和访问本机进程所需配置，不增加应用 build 定义，不改正式部署合同或业务实现。

复用 backend/internal/integrationtest、既有 auth/http/post/comment/like/outbox/worker/search 集成夹具与 Go API；观测复用既有模块测试、官方插件 HTTP 接口及 `backend/cmd/admin-role` 引导命令。六个 Exporter 和观测模块的现有测试原样运行，没有已证明缺口时不增加重复用例。旧工具中的容器安全、重启持久化、故障恢复、完整插件制品和历史调用保持原语义。

## 2. 入口、隔离与交接

- `make integration` 默认等同 `make integration SCOPE=business`：设置 INTEGRATION_TESTS=1、APP_ENV=test、gopulse_integration 账号/库、Redis DB 15，在独立 test 依赖上执行串行 Go 包。scripts/verify-business.sh 增加明确的 --native 委托路径，旧参数和默认容器/历史路径保留。
- `make integration SCOPE=observe` 是本批待实现接口：锁定同一 test 环境，准备独立 Kafka、VictoriaMetrics、观测 Elasticsearch、插件状态卷；运行源码 Backend/Router/Marshaller，复用已准备的固定 Linux Monitor。Go 用例承担 HTTP/数据断言，助手只准备环境、启动进程和传播退出码。仅运行选定的观测链路，不重复完整业务集成套件。
- 项目、卷、端口、凭据和插件状态均与开发环境分开。测试配置保持既有主题、索引和身份合同；观测端口必须指向 test 项目，白名单在任何写入、安装插件或启动应用前验证。缺少依赖、Monitor 镜像或必要配置直接失败，不自动 build、不 skip。Monitor 首次准备仍使用 01 的显式命令。
- 经用户确认，本批测试依赖端口调整为 MySQL `23306`、Redis `26379`、RabbitMQ `25672`/管理端口 `25673`、业务 Elasticsearch `29200`、观测 Elasticsearch `29201`；开发端口合同不变。
- 同一测试锁保护 business/observe/e2e。开发进程占用固定组件端口时拒绝测试启动，不停止他人的进程，不修改私有端口合同。退出停止自己启动的源码程序及测试 Monitor，保留依赖数据卷；测试账号、规则、样本使用归属明确的唯一标识，清理仅处理自己创建的记录。
- 03 复用这里的观测 test 配置、锁、原生 admin-role 和官方插件配置方式；其浏览器夹具必须在启动 Playwright 前准备真实管理员、普通用户及指标/日志/事件数据。不得另复制一套环境管理器。

## 3. 验证映射

| 对象/案例 | 已有覆盖与具体缺口 | 最低有效层级及本批变更 |
| --- | --- | --- |
| L03 注册、登录、Cookie/权限失败 | backend/internal/auth/integration_test.go、internal/http/auth_integration_test.go、router_auth_test.go、middleware/*_test.go | 复用 Go HTTP/集成，不再新增 Python 普通断言 |
| L03 发帖/评论/点赞及事务 | internal/http/post_integration_test.go、comment_like_integration_test.go；internal/post/comment/like 集成测试 | 复用原生 Go；business_flow_integration_test.go 连接既有 Outbox/Worker/Search 边界，检查通知接收者、重复点赞不重复通知和搜索可见 |
| L03 Outbox→RabbitMQ→通知/搜索 | backend/internal/outbox/integration_test.go、internal/worker/integration_test.go、internal/search/edit_integration_test.go、delete_integration_test.go 已有分段覆盖；缺一个普通功能完整链路 | business_flow_integration_test.go 使用真实 DB/Rabbit/ES 和原生组件，唯一数据、有界等待，不重写分段验证框架 |
| O01 采集、传输、转换、查询及普通失败 | monitor/internal/metrics/collector/collector_test.go、publisher/publisher_test.go、internal/plugin/*_test.go；router/internal/httpserver/server_test.go、internal/envelope/envelope_test.go；marshaller/internal/consumer/processor_test.go、metrics/transform_test.go、logs/transform_test.go、events/events_test.go；componentmetrics/*_test.go；六个 exporters/*/internal/collector/collector_test.go | 直接运行所属模块 Go 测试。后端 internal/metricquery、logquery、eventquery、alert、observability 及 router_metrics/logs/events/alerts 测试随 backend 执行；复用已有非法数据、上游失败和鉴权断言 |
| O02 告警状态、租约和审计 | backend/internal/alert/repository_test.go 的 TestOwnedMySQLStateAndLease 已有真实 MySQL 断言，但只接受旧 gopulse-p1401 临时项目，普通 go test 会跳过 | 在该文件提取共享测试断言，旧项目/DSN 白名单保持不变；新增 repository_integration_test.go（integration tag），使用既有 integrationtest 白名单和受控时钟验证同一组状态/租约/恢复/审计行为。必须实际执行，不把 skip 算通过；不证明真实评估周期或长时运行 |
| O03 新指标端到端 | Monitor/Router/Marshaller 分段 Go 测试及 scripts/verify-marshaller.sh、verify-component-metrics.sh 有真实专项覆盖；缺本机隔离入口下的普通链路 | 新 observability_flow_integration_test.go（integration + observability_integration tags），通过实际 Redis 插件采集→Monitor→Router→Kafka→Marshaller→VM→后端查询。要求目标身份正确且存在开始本次采集后产生的数值样本，不能只看历史序列非空 |
| O04 新日志与运行事件 | monitor/internal/httpserver/logs_test.go、internal/logs/logs_test.go、internal/events/monitor_test.go；后端日志/事件查询测试；scripts/verify-logs.sh、verify-events.sh 的真实链路 | 同一 Go 链路用例发出带新 request_id 的业务请求，查询到该请求日志；通过本次插件启动/停止动作查询到关联运行事件，验证来源和时间。真实走 Monitor/Router/Kafka/Marshaller/观测 ES，不直接写 ES 来冒充传输成功 |
| O05 管理权限与必要失败 | backend/internal/http/router_metrics_test.go、router_logs_test.go、router_events_test.go、router_alerts_test.go 已有 401/403；内部服务已有 token 拒绝测试 | 原生已有失败测试复用；新真实链路证明管理员获得正确结果、普通用户访问观测 API 被拒绝。完整容器隔离和外部故障矩阵保留专项检查 |
| L02 资源与数据隔离 | 01 助手、backend/internal/integrationtest/environment.go | 扩展观测端点白名单和 test 资源身份检查；运行前后核对开发 DB/观测记录未被改动，拥有进程清理完成、依赖卷保留 |
| 旧工具覆盖与调用方 | scripts/verify-business.sh、verify-monitor.sh、verify-router.sh、verify-marshaller.sh、verify-exporter.sh、verify-component-metrics.sh、verify-logs.sh、verify-events.sh、verify-alerts.sh、verify-plugin-state.sh；正式计划/CI/导入仍在使用 | 在 local-development-tests.md 登记每类普通断言、原生承接位置、仍保留的容器/插件/恢复/容量/制品/证据断言。除 verify-business.sh 的明确委托外，不批量改写或删除这些旧入口；04 调整日常 CI 调用 |

上述新增用例和参数已在本批实现并通过固定门禁，供 03/04 复用。源码 Go 测试中的 Linux 专属平台分支可以按既有平台合同记录跳过；O02～O05 选定的真实检查不允许因环境不足跳过，macOS 的官方插件真实运行由固定 Monitor 容器承担。

Trace 的上下文传播、采样及失败处理复用 backend/internal/observability/tracing/tracing_test.go 等已有 Go 用例；本阶段不重跑完整跨进程 Span 图及新鲜度测量。原 scripts/verify-phase20-chain.sh 和 scripts/ci/phase20_chain.py 的特殊链路/证据合同继续保留，覆盖映射明确这个边界，不把指标/日志/事件短链路登记为完整 Trace 专项通过。

## 4. 固定命令与通过结果

1. 以下模块各执行一次 `make test MODULE=<模块>`：`backend`、`monitor`、`router`、`marshaller`、`componentmetrics`、`exporters/redis`、`exporters/mysql`、`exporters/rabbitmq`、`exporters/elasticsearch`、`exporters/kafka`、`exporters/victoriametrics`。O01 现有成功/失败用例通过，未变且身份可证明的成功项不重复。
2. `make integration SCOPE=business` 实际执行 `go -C backend test -p 1 -tags=integration ./...`：L03 全链路及 O02 的新增 MySQL 用例实际通过。普通 `make integration` 默认行为及 verify-business.sh --native 用直接委托测试核对，不再重复该全集。
3. `make integration SCOPE=observe` 在准备好且白名单正确的 test 观测环境中，实际执行 `go -C backend test -p 1 -tags=integration,observability_integration ./internal/http -run '^TestObservabilityFlowIntegration$' -count=1 -timeout 6m`：O03～O05 所有选定子案例通过，记录本次数据标识及实际查询结果。没有当前数据或必要进程退出必须非零失败。
4. `python3 -m unittest discover -s scripts/ci -p test_local_development.py`；`bash -n scripts/verify-business.sh`；`bash scripts/verify-business.sh --self-test`。助手自测保护 scope 参数、配置白名单、未知 scope 拒绝、测试锁、失败退出和归属停止，不复制 Go 功能断言。如委托实现影响现有 Python 业务工具，运行直接受影响的 test_verify_business.py。
5. 核对 L02：业务及观测 test 资源与 dev 不重叠、开发数据未变、无自己启动的遗留应用进程、卷保留、未构建业务镜像。保存源码/测试/配置 digest、依赖镜像和资源身份、Go/系统版本、实际命令与结果；私有凭据不得进入日志。

完成元数据/分支校验在最后执行。浏览器、真实故障恢复、完整六插件生命周期、长期告警评估、容量和正式制品验收不由上述原生结果替代；分别由 03 或保留的专项工具承担。

## 5. 预算与实验成本

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现/实现/覆盖映射 | 50 | 75 |
| 模块与工具直接检查 | 20 | 25 |
| 业务/观测测试依赖及源码准备 | 15 | 25 |
| 真实业务、告警与观测链路门禁 | 25 | 40 |
| 日志/元数据/提交/归属清理 | 10 | 15 |
| 总计 | 120 | 180 |

一次业务集成全集（含受控时钟告警数据库测试），Go 命令最多 15 分钟；一次观测链路，Go 命令最多 6 分钟。观测含指标、日志、事件三种数据及必要角色失败检查；指标冷启动等待上限 90 秒、日志 90 秒、事件 60 秒，保持现有实际采集周期及 VM 查询延迟，不通过缩短周期冒充正常链路。依赖准备单次最多 420 秒、源码就绪最多 180 秒、归属停止最多 30 秒；复用未变依赖/Monitor/Go 缓存，不创建业务镜像，无容量或长时实验。

本批仅增加两条普通链路及一个复用告警断言的入口，不同时迁移全部历史验收程序。若发现这三个缺口无法在剩余预算内完成，停在可审阅的实际边界并先修订有限接续方案；不得删减 O02～O05、扩展为通用框架或重置预算。

## 6. 停止、接续与完成

首次基础设施失败停止套件并复现最小边界；同因最多两次各 10 分钟，累计计入预算。90/144 分钟报告进度；保存 L02/L03/O01～O05 的 passed/failed/not_started、源码/测试配置/依赖身份、真实命令结果、实际成本和未执行列表，分别记观察到的失败、已证明原因和允许修正的边界。

模块 Go 测试按受影响模块重验；业务全集自身不支持回执拼接。观测数据只绑定产生它的当前源码/配置/环境，新增一轮请求或插件动作必须保存新标识，不能把历史记录改绑为本次成功。业务门禁相关输入未变时，不因观测助手或浏览器后续修改而重复业务全集；无法证明身份有效时如实待核对。

L02/L03/L07、O01～O05 和工具固定门禁已全部实际通过，原工具覆盖映射已登记且归属清理完成；已记录同名实际日志、同步 2.4.2，并在完成提交后交接 03。日志区分模块测试、实际数据链路和保留专项验收的能力边界。
