# GoPulse 开发指南

GoPulse 是以社交业务为真实负载的 Go 可观测与可靠性实验项目。按本次改变的组件阅读和验证。

## Repository Map and Reading Order

| 修改什么 | 先从哪里看 | 验证入口（仓库根目录） |
| --- | --- | --- |
| 业务 API、运行角色 | [API](backend/internal/http/api.go)、[角色装配](backend/cmd/server/roles.go)、[Backend](backend/README.md) | `make test MODULE=backend` |
| 数据与异步任务 | [Outbox](backend/internal/outbox/)、[Worker](backend/internal/worker/)、[搜索](backend/internal/search/)、[消息合同](dev/contracts/migration-state.md) | `make integration SCOPE=business` |
| 用户端、管理端 | [用户路由](frontend/src/router/index.ts)、[管理路由](admin-frontend/src/router/index.ts)、[管理端说明](admin-frontend/README.md) | `make test MODULE=frontend` / `admin-frontend`；浏览器用 `make e2e SCOPE=business` / `observe` |
| 插件和采集 | [Monitor](monitor/README.md)、[Exporters](exporters/README.md) | 所属模块测试；真实观测用 `make integration SCOPE=observe` |
| 观测传输与存储 | [Router](router/README.md)、[Marshaller](marshaller/README.md)、[技术契约](dev/contracts/README.md) | `make test MODULE=router` / `marshaller`；真实观测用 `make integration SCOPE=observe` |
| 共享指标 | [componentmetrics](componentmetrics/)、[指标合同](dev/contracts/component-metrics.md) | `make test MODULE=componentmetrics`，并按需跑受影响消费者的模块测试 |
| 本机环境、测试、构建 | [devenv](devtools/cmd/devenv/main.go)、[原生入口登记](dev/validation/local-development-tests.md) | `make test MODULE=devtools` |
| Compose 验收、Bundle | [acceptance](acceptance/cmd/gopulse-acceptance/main.go)、[lifecycle](lifecycle/)、[Bundle 指南](deploy/release/BUNDLE-README.md) | 模块测试及当前计划指定的 `make verify-*` / `make package` |
| 规则、文档、CI | [工程入口](dev/README.md)、[工具规则](ci/AGENTS.md)、[质量 workflow](.github/workflows/quality-gates.yml) | 治理检查；核对改动正文的链接和实际命令 |

模块名与构建方式从根 [Makefile](Makefile)读取，依赖版本从对应 go.mod/package-lock 读取。
产品工作先读[能力状态](dev/status/capability-status.md)和[已分配实施计划](dev/phases/README.md)；
修改公开合同再读相关正文，规划验证时读 ci/AGENTS.md。

## 分支与版本

- 只读分析直接进行；规则、规划和文档维护使用 `update`。产品代码、模块测试和应用验证使用已分配实施批次。
- 新产品批次先 fetch 主远端，读取总方案的目标版本和分配，从主远端 main 创建分支；
  分支必须匹配 `^develop/[0-9]+\.[0-9]+\.[0-9]+$`。
- 批次完成或 PR 打开后，不继续使用该开发分支。调整未开始分配先修改总方案；已推送分支的改名/重编号与用户协调。
- `VERSION` 是已完成版本的唯一来源，格式为 major.minor.patch。成功收口时与同名日志一起提交；update 不修改它。
- 可执行 CI/验收工具改动也纳入批次。正式退役能力的工具、测试和有效引用清理可在 update，注明决定、删除能力和仍有效的覆盖。
- update PR 使用 merge commit 并保留分支；普通开发 PR 可 squash 并删除分支。

## 实现与验证

- 按实施计划交付。总方案定义阶段结果、批次和里程碑；分方案定义文件范围、验收条件与固定门禁。
- 聚焦直接相关代码，复用现有接口、助手和测试。测试证明改变的行为、真实缺陷或安全/持久化/公开合同。
- 实现中跑最小检查，最终候选跑固定门禁。有效检查只在相关代码、配置、依赖、身份或环境改变后重跑。
- 最终检查本次 diff 的接口兼容、事实源、权限、消息确认、取消和资源归属；细节见[代码约定](dev/README.md#代码改动约定)。
- 昂贵构建、真实验收和长时实验提前在计划中约定成本、窗口和停止点，按[成本与接续规则](dev/rules/implementation-execution-budget.md)执行。
- 通过即收口；无关问题记录后续项。失败或未完成工作保留实际进度，不能作完成声明。

## 架构与证据

- [设计索引](dev/design/README.md)登记生效目标；[能力状态](dev/status/capability-status.md)记录当前事实；
  总/分实施方案定义执行合同，日志记录实际结果，导航不另建版本、状态或门禁副本。
- 下级文档只补充细节，不得放宽本文的分支、版本、安全、证据或提交约束；
  权威文档冲突或工作无法归入有效批次时，停止并询问用户。
- 2026-10-06 的[高并发设计](<docs/GoPulse 高并发架构设计.md>)、
  [可观测设计](<docs/GoPulse 可观测架构设计.md>)和[落地大纲](dev/design/GoPulse-目标架构落地大纲.md)保留原路径与原文。
  改变目标需用户批准新基线；草案不预留 Phase、版本或分支，也不改写已分配合同。
- 冻结验收绑定 revision、Bundle、manifest、合同和回执；相关变动重验受影响证据，不编辑回执或换绑候选。
- 只清理本次拥有的进程、容器、卷与文件，不使用全局 prune。发布前核验实际选定的脱敏证据。
- 已完成计划、日志、评审和原始证据保留历史上下文。执行计划时更新 logs 下同名文件，只记录实际工作和命令结果。

## 提交与常见陷阱

- 成功修改项目文件后提交当前任务文件；使用英文 Conventional Commits。先做相称检查，不带入用户或其他任务的改动。
- Go 是多模块仓库，根 `go test ./...` 不代表全量；裸 `make build` 会构建镜像，局部构建应指定 MODULE。
- 前端锁文件变化后在相应应用运行 `npm ci`；现有目录不会可靠触发自动重装。
- `make dev` 是宿主源码开发，`make stack-up` 是 Compose 栈；测试项目与开发数据隔离。
- 历史图和矩阵按原版本阅读；拟新增的 doctor/bootstrap/lint/verify/docs-check 以[改造方案](dev/design/GoPulse-工程稳定性改造方案.md)状态为准。
