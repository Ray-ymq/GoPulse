# Phase-21-01：运行角色与接口装配分离

> 状态：未开始。目标 `2.3.1` / `develop/2.3.1`，分配以[总方案](Phase-21-总实施方案.md)为准。
> 主要结果：一个 Backend 制品能选择 business、platform 或兼容 combined，各自只装配所属接口、依赖和任务。

## 1. 进入条件与范围

从主远端最新 main 的完成 `2.2.4` 基线开始；总方案和本文件已合入 main。先 fetch 当前主远端 `upstream`、核对 `VERSION`/总方案/工作树，再创建 `develop/2.3.1`。不导入取消批次或改写历史证据。

本批改启动装配及最低有效层回归。生产 Compose 暂保留默认 combined，双服务部署归 02，最终同一冻结候选定向验收归 03。保持现有领域实现、API payload/错误、Cookie、数据库 Schema、消息合同和角色写入规则。

## 2. 允许修改的精确文件

以下为允许集合，按实际必要性修改，不要求全部改动；表外文件需先在总/分方案记录具体原因，范围冲突按仓库规则处理。

| 用途 | 文件 |
| --- | --- |
| 入口/装配 | `backend/cmd/server/main.go`、`main_test.go`；新增同目录 `roles.go`、`roles_test.go`、`roles_integration_test.go` |
| 角色与依赖配置 | `backend/internal/config/config.go`、`config_test.go`；新增同目录 `service_role.go`、`service_role_test.go` |
| API/准入 | `backend/internal/http/api.go`、`router.go`、`router_test.go`、`router_auth_test.go`、`component_metrics_test.go`；新增同目录 `router_roles_test.go` |
| 受影响管理回归 | `backend/internal/http/router_metrics_test.go`、`router_logs_test.go`、`router_events_test.go`、`router_alerts_test.go`、`router_exporter_plugin_test.go` |
| 最小真实依赖夹具 | `backend/internal/integrationtest/environment.go`（仅在现有隔离校验不足以支持角色测试时补充，不修改 CI 全局服务） |
| 运行环境/指标的角色验证 | `componentmetrics/runtime.go`、`runtime_test.go`、`config.go`、`backend_test.go` |
| 说明 | `backend/README.md`、`dev/contracts/runtime-contracts.md`、`dev/status/capability-status.md` |
| 完成时元数据 | `VERSION`、`.env.example` 的版本字段、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`、`deploy/runtime-contracts.json` 的产品版本字段 |
| 合同/真实日志 | 本分方案的必要事实修订；`dev/logs/Phase-21/Phase-21-01-运行角色与接口装配分离.md` |

不移动所有领域目录，不新增服务间认证 RPC，不改变 Monitor、Worker、Indexer 实现，不构造新制品或全量验收框架。若共享组件 API 确有变化，只补直接调用者的编译/合同检查，先列出表外文件再继续。

## 3. 装配与迁移步骤

1. 先记录实际基线：当前注册路由、MySQL/Redis/ES/RabbitMQ/管理客户端和后台任务、探针依赖、关停顺序。限定读取上述入口及直接调用接口，不作一般架构/依赖审计。
2. 增加 `BACKEND_SERVICE_ROLE`，空值取 combined；角色与 host/container 模式独立。无效角色在资源创建前报错。配置按角色加载/校验，允许未使用依赖缺省，不能为省配置校验而接受该角色实际使用的无效地址/secret。
3. 提取有类型的装配函数，明确共享资源和角色资源。business 保留社交 API、Redis、业务 ES、publisher、dispatcher/Outbox 采样；platform 仅构造账号/授权、管理查询/控制和告警评估；combined 沿用两组。可选 dispatcher 的启动/退出需显式处理，不能要求 platform 传入伪 dispatcher。
4. 两种角色都装配 MySQL 用户库、既有 JWT/Cookie 校验、请求 ID、日志/私有指标和四个探针。platform 不创建注册/登录/current-user 业务 handler；由同源入口路由到 business。管理授权每次读取现有数据库角色，不能缓存到 JWT 后失去降级效果。
5. API 路由按总方案表注册；未所属路由不注册且返回既有 404。保留所属路由的权限顺序和响应，包括上传与管理错误。准入只作用于 `/api/v1`，每进程一个有限 semaphore，探针不占槽位，指标 route 模板仍为有限目录。
6. 就绪只检查所属角色的既有必要依赖/Schema/引导账号条件，不能给 business 添加管理服务健康依赖；可选观测查询后端仍按既有错误合同处理。角色无关 secret 不成为新的启动前置。共享日志/Trace 采用既有有界故障策略。
7. 退出遵守既有 5 秒上限，依次停止接收、取消实际启动任务、关闭所属连接。platform 不等待未启动的 dispatcher；告警调度只随 platform/combined 生命周期运行。
8. 01 仍使用既有 backend 进程/指标合同；新 Compose 运行单元与固定制品/指标别名由 02 登记。补默认 combined 与两个角色的直接检查、最小真实启动证据，完成后登记能力范围，不宣称双服务部署已验收。

回退为选择 combined 并使用旧 Compose；不需要数据迁移。回退不会撤销平台实际做过的角色、插件或告警变更，操作仍服从既有持久状态/审计合同。

## 4. 已有覆盖、缺口与固定门禁

| 改变的行为 | 已有覆盖与具体缺口 | 最低有效层 / 本批补充 / gate |
| --- | --- | --- |
| 三角色的配置、路由、依赖与任务 | `backend/internal/config/config_test.go`、`backend/internal/http/router_test.go`、`backend/cmd/server/main_test.go` 已覆盖配置、路由和关停；缺少角色选择及未使用依赖不创建的断言 | Go 单元补 `roles_test.go`、`service_role_test.go`、`router_roles_test.go`；真实进程补 `roles_integration_test.go`。R01/R03/R04 |
| 既有会话、实时授权与管理接口 | `backend/internal/http/router_auth_test.go`、`management_handler_test.go`、`auth_integration_test.go` 及各 `router_*_test.go` 已覆盖账号/管理行为；缺少跨两个装配的同 Cookie 与路由互斥 | 复用现有断言，在允许的新角色测试补跨装配成功、401/403、降级及引导账号保护；不重写领域用例。R01/R04 |
| 独立准入与探针 | `router_test.go` 的 `TestAPIBusinessAdmissionDoesNotConsumeProbeSlot` 已用 started/release channel 持有测试 handler，证明满额 503、探针可用和释放；缺少两角色实际限额与互不共享槽位 | 在 `router_roles_test.go` 补 `TestServiceRoleAdmissionIsolation`，沿用该夹具及实际 router/准入接线。R01/R03；03 同候选复核 |
| 生命周期与固定指标 | `backend/cmd/server/main_test.go` 已覆盖有界退出，`componentmetrics/runtime_test.go`、`backend_test.go` 覆盖运行配置与准入信号；缺少角色资源只启动/只关闭一次 | 扩展已有生命周期/指标检查及真实进程起停；不扩指标目录。R02/R03/R04 |

`TestServiceRoleAdmissionIsolation` 是待实现的固定行为测试：按 business=128、platform=32 的实际角色配置构造两个独立 router；逐一确认 N 个请求已进入测试 handler，再断言 N+1 返回既有 `503 backend_busy`、另一角色与四探针可用、释放后恢复。每角色阻塞最多 10 秒，所有 goroutine 有界回收。handler 仅定义在测试内，不加入产品路由，也不依赖数据库延迟注入。这证明准入及隔离语义；实际容器使用同样限额由 02/03 核对。

下面新增测试名为**本批待实现接口**；必须先可执行，真实依赖不可用不能用 skip 代替通过。现有命令使用当前 Go/Python 工具链，在 Linux amd64 记录结果。

| gate | 检查/命令 | 通过条件与证据 |
| --- | --- | --- |
| R01 | `rtk go -C backend test ./cmd/server ./internal/config ./internal/http` | 三角色配置、有限路由/权限、`TestServiceRoleAdmissionIsolation` 与装配任务检查通过；保存命令、退出码、测试输出 |
| R02 | `rtk go -C componentmetrics test ./...` | 受影响环境验证、固定目录/探针/Backend 指标合同回归通过；不增加开放标签 |
| R03 | `rtk go -C backend test -race ./cmd/server ./internal/config ./internal/http` | 新装配生命周期及准入无已检出的竞争；不扩大为全项目 race 审计 |
| R04 | `rtk go -C backend test -tags integration ./cmd/server -run '^TestIntegrationBackendServiceRoles$' -count=1 -timeout=180s -v`（新增） | 每角色真实启动/就绪/退出及最小账号权限验证通过，所有子案例实际执行 |
| R05 | 完成候选执行 `rtk proxy python3 scripts/ci/validate_versions.py`、`rtk proxy python3 scripts/ci/validate_branch.py --branch develop/2.3.1 --base-ref upstream/main`、`rtk git diff --check` | 版本同步、唯一分配和 diff 检查通过；完成前不提前改 VERSION |

R04 复用 `backend/internal/integrationtest/environment.go` 的安全约束与 `.github/workflows/quality-gates.yml` 中 integration job 的既有服务/迁移配方：`INTEGRATION_TESTS=1`、`APP_ENV=test`、独立 `gopulse_integration` MySQL 库/账户、Redis DB 15 及既有凭证变量。现有 helper 只校验环境，不负责创建。执行者在自有临时项目/数据卷中准备 MySQL、Redis、RabbitMQ、业务 ES，显式 loopback 非默认端口，迁移后才运行 Go 命令；准备最多 10 分钟，计入本批预算，不藏在 180 秒测试期限中。不得指向现有开发/生产数据。platform 子案例验证业务依赖未配置/不可达时仍能装配，不启动全套管理后端；查询闭环留给 03。需要额外就绪依赖时先记录直接原因及成本。

| R04 子案例 | 固定动作与期限 | 证据/失败归属 |
| --- | --- | --- |
| R04-a | 三角色各一次启动；进程就绪最多 30 秒，SIGTERM 后最多 5 秒退出；夹具建立最多 10 分钟 | 探针状态、进程退出码、资源关闭/残留清单；启动语义不符为 product_failure，夹具失败为 infrastructure_failure |
| R04-b | 从实际 router 获取模板，断言两组路由的互斥及 combined 并集；错误角色 API 404 | 有限路由清单与代表性真实 HTTP；不能只测配置枚举 |
| R04-c | 同一真实账号 Cookie：业务 current-user 正常、管理 user 403、管理员管理成功、降级后原会话 403、引导账号不可降级 | 状态/响应摘要及数据库角色结果；不发布 Cookie/密码 |

必须回归现有注册/登录/退出/角色校验、业务写入路径及管理查询/插件/告警路由的受影响行为。沿用已有测试，仅新增改变的装配/权限/生命周期边界，不为未改领域建立覆盖率计划。R04 不替代 S03/S04 的全链路结果。

## 5. 预算与实验成本

预计总活跃时间 **120 分钟**，本文件跨所有重试/候选累计上限 **180 分钟**。

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 有限发现与装配实现 | 60 | 80 |
| 直接检查/受影响回归 | 15 | 20 |
| 构建与最小夹具准备 | 15 | 25 |
| R03/R04 真实检查与诊断 | 20 | 35 |
| 证据、日志、提交与安全清理 | 10 | 20 |
| 合计 | 120 | 180 |

实验成本为 3 角色 ×（最多 30 秒就绪 + 5 秒退出），另计 Go 准入测试 2 × 最多 10 秒阻塞；必要固定观察不足 3 分钟。账号/路由操作、夹具准备最多 10 分钟、检查与清理开销计入上表。没有长时测量或稳定性结论。每次命令设期限，夹具建立或构建超过阶段预算先停，不无限等候。

同一未解决原因最多两次、每次最多 10 分钟最小诊断，包含在 180 分钟内。第一次基础设施失败停止 R04 整组，先复现最小边界；没有实际修正不重跑整组。产品失败修正后记录源文件→构建产物→受影响 gate，只重建相应 Backend/测试产物，复用未变配置和直接检查。

## 6. 证据、停点、接续与完成

私有证据用新的 `.run/phase21-01-<id>/`，记录目标版本、源码 revision/工作树摘要、角色配置摘要、测试/夹具版本、命令时间/退出码、资源归属及 R01～R05 的 passed/failed/not_started。真实依赖日志保存原值；输出脱敏。单元成功仅在相关输入不变时可复用；更改装配、配置或退出代码需重跑受影响角色门禁。

本批真实测试不承诺进程内断点续跑；退出/清理后重新建立最小夹具，已通过的无关确定性检查可保留。候选变化不能修改旧回执，不能把本批证据标成 03 正式验收。

90/144 分钟时报告累计成本、完成项、剩余 gate 与下一步；启动命令前确认预计耗时加清理仍可容纳。到 180 分钟、两次诊断耗尽或用户停止时：停止新工作，终止本测试拥有的进程/Compose 资源，保留证据，写未完成 checkpoint 与剩余修改/门禁，不更新完成版本或创建完成提交。用户明确要求继续前，修订剩余工作及有限预算；停止不自动恢复。

完成条件：R01～R05 全部通过；三角色装配符合合同，无阻断项；最小真实检查和归属清理有证据；更新同名实施日志及状态，完成版本同步为 `2.3.1` 并提交。此时只交付角色能力，02 才开始部署迁移。
