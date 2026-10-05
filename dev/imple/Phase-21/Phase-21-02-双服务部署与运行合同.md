# Phase-21-02：双服务部署与运行合同

> 状态：未开始。目标 `2.3.2` / `develop/2.3.2`，分配以[总方案](Phase-21-总实施方案.md)为准。
> 主要结果：最终 Compose 将业务和管理分开运行，两个前端经原同源入口访问正确角色；运行/采集与 Bundle 合同一致。
> 2026-10-06 调整：复用运行验收入口及已有用例，只补角色分离的缺口；不另建 Phase 21 runner/verifier，不新增候选版本覆盖接口。

## 1. 进入条件与范围

01 的固定门禁、同名日志和完成 `2.3.1` 提交已进入 main。先 fetch 主远端、重读总方案/01 日志，从最新 `upstream/main` 创建 `develop/2.3.2`。角色产品实现已可用，不把 01 未完成问题转移到本文件。

本批以部署及直接支撑代码开发为主；适配现有运行验收入口、合同校验和 Playwright 用例，以非正式源码预检确认 03 所需边界已经可执行。正式冻结候选验收归 03。不增加业务、第二种 Backend 镜像、采集源类型或泛用矩阵框架，不重跑容量认证。

## 2. 允许修改的精确文件

| 用途 | 文件 |
| --- | --- |
| 部署/同源代理 | `deploy/compose.yaml`、`deploy/compose.debug.yaml`、`deploy/docker/frontend/nginx.conf`、`.env.example` |
| 运行/采集角色元数据 | `deploy/runtime-contracts.json`、`deploy/runtime-contracts.schema.json`、`componentmetrics/runtime.go`、`runtime_test.go`、`config.go`、`probe.go`、`probe_test.go`、`backend/cmd/server/roles.go`、`roles_test.go`、`backend/cmd/server/main.go`（仅运行身份接线） |
| 合同验证与 Compose 入口 | `scripts/ci/verify_runtime_contracts.py`、`test_runtime_contracts.py`、`test_compose_acceptance_env.py`、`scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh`（仅补新增角色环境/拓扑兼容） |
| Bundle/生命周期固定别名与环境 | `scripts/ci/release_artifacts.py`、`release_manifest.py`、`test_release_manifest.py`、`test_release_snapshot.py`；`lifecycle/internal/control/compose.go`、`compose_test.go`（新增）、`control_test.go`、`lifecycle/internal/release/manifest.go`、`manifest_test.go` |
| 复用执行入口与行为自测 | `scripts/ci/runtime_acceptance.py`；新增 `scripts/ci/test_runtime_acceptance.py`；复用上行合同检查中的 `verify_runtime_contracts.py`、`test_runtime_contracts.py` |
| 复用浏览器用例 | `frontend/e2e/compose-business.spec.ts`、`admin-frontend.spec.ts`、`phase15-closure.spec.ts`（仅补有限 service-split 用例/选择及真实夹具接线） |
| 说明/验收合同 | `dev/contracts/runtime-contracts.md`、`dev/contracts/component-metrics.md`、`dev/validation/README.md`；新增 `dev/validation/Phase-21/phase21-split.md`（用例与证据说明，不是执行器）；`deploy/release/BUNDLE-README.md`、`deploy/release/README.md`、`backend/README.md`、`README.md`、`使用手册.md`、`dev/status/capability-status.md` |
| 完成时版本/记录 | `VERSION`、两个前端的 `package.json`/`package-lock.json`，本分方案必要修订，`dev/logs/Phase-21/Phase-21-02-双服务部署与运行合同.md` |

不修改历史 Phase runner 或证据，不退役现有套件。新增 `test_runtime_acceptance.py` 的必要性是现有运行入口没有 CLI/失败传播/归属清理的行为自测，新增用例选择和预检模式会改变这些执行边界；它只保护这些边界，不另建环境或验收框架。发现文件超出集合时先记录精确路径、必要性与预算影响。两个前端 API 合同无须改动，界面代码不在允许集合。

## 3. 验证复用与具体缺口

| 改变的行为 | 已有覆盖与可复用部分 | 缺口 / 最低有效层 / 本批变更 / gate |
| --- | --- | --- |
| 运行单元、端口、池预算、固定镜像别名 | `verify_runtime_contracts.py`、`test_runtime_contracts.py` 校验封闭运行合同；`test_release_manifest.py`、`test_release_snapshot.py` 保护制品身份；lifecycle 的 `control_test.go` 保护操作流程 | 缺 platform-api 与副本别名、命名空间端口、52+8 连接分配。扩展现有静态/制品检查和 Go lifecycle 行为；真实配置另核对。D01/D02/D04 |
| 同源会话、权限、业务与管理查询 | `compose-business.spec.ts` 已有真实注册/登录、发帖/评论/通知/搜索；`admin-frontend.spec.ts` 已有 Cookie、401/403、降级、真实查询及插件启停；`phase15-closure.spec.ts` 有告警浏览器闭环 | 缺分离角色下的选择与单侧停止；编辑/点赞/收藏/关注按 S03 补代表性真实请求。保留浏览器验证同源边界，HTTP 验证具体业务事实；在现有 spec/运行入口补用例。D03/D04，完整链路归 03 |
| 告警与插件真实操作 | 上述管理 spec 可复用查询/启停；`scripts/ci/verify_plugin_metrics.py` 已有官方旧包→当前包的真实更新配方 | 原查询/启停用例不证明安装/更新；六插件采集故障矩阵也不能替代更新。只补一个 Redis 官方包的安装/更新与一个真实告警；复用既有包及配方数据，不导入整份历史执行器或运行六插件矩阵。D03/D04 |
| 独立准入与三个采集实例 | 01 的 `TestServiceRoleAdmissionIsolation` 证明有限准入及探针隔离；现有 `runtime_acceptance.py` 已有指标请求/收集路径 | 缺实际进程限额/角色/实例对应。Go 先执行固定行为检查，真实部署核对三个实例的限额指标、池配置与采集结果；不添加真实延迟注入器。D02/D04 |
| 执行、证据与归属清理 | `runtime_acceptance.py` 已管理独立项目、HTTP、源码/manifest 模式、失败后 finally 清理；`verify_runtime_contracts.py` 已有严格 JSON/身份校验 | 现有入口没有有限 service-split 选择，失败时外部 evidence 未必落盘，清理只核对容器；现有 checker 不验证运行回执/发布件。有限扩展这两个入口及行为自测，补网络/卷/私有凭证、失败回执和选定发布件核验。D03/D04 |

复用既有 `candidate_runtime.py`、`release_manifest.py` 的候选绑定与 Compose 公共能力，不增加跨历史执行器导入，不为本批提取通用框架。上述扩展最多占本批累计 30 分钟；发现所需工具开发无法落在范围/预算内时，先保留可复核缺口并修订合同，不把开发推迟到 03。

## 4. 迁移步骤与确定决策

1. 保留 `backend`/`backend-2` 名称与双副本，设 role=business；新增 `platform-api` 单实例 role=platform，使用同一个 `GOPULSE_BACKEND_IMAGE` digest。保留共享 MySQL、会话 secret/Cookie/引导账号配置，不创建新数据卷。旧环境未设 role 仍可 combined。
2. 按角色删减环境/依赖：business 不等待观测查询 ES/管理 API；platform 不依赖 Redis、RabbitMQ 和业务 ES，不启动业务后台任务。两个角色所需日志投递网络和 shared MySQL 保留；网络连接仅按真实依赖取舍，不把共享 Monitor/状态层说成完整隔离。四个探针有各自所属角色语义。
3. 在实际入口 `deploy/docker/frontend/nginx.conf` 建 platform upstream；用总方案列出的四组精确基路径和子路径转发。其余 API 与登录/current-user 仍到业务池，两个前端继续同源。保留插件上传 65 MiB 限制和既有 30 秒控制超时；错误不得反向回退到业务池。直接服务端口只在私网，debug override 只允许显式 loopback 调试。
4. 登记 `platform-api` 为封闭运行单元：role、Compose 服务、探针/关停、pool=4、HTTP=32、instance=`platform-api-1`、制品与指标 component 别名均为 backend。运行合同升至版本 `2`，探针与验证程序同源同步；原固定指标目录和业务响应不变。固定 inventory 由 12 个运行单元增至 13 个，不能放宽为任意字符串。
5. 修改当前校验中“进程 ID 必须等于指标组件”和“全局监听端口唯一”的假设：显式白名单别名 platform-api→backend；端口只在同一命名空间要求无冲突，backend 与 platform 容器可共用 8080/19101。继续严格检查角色、真实入口、source/metrics 目录、实例、私网、探针及版本；不能删除门禁或绕过动态装配检查。
6. `BACKEND_ENDPOINTS=backend,backend-2,platform-api`，仍走 Backend token/固定 families，沿用 `GOPULSE_INSTANCE_ID` 区分实例。不改 Monitor 收集算法或增加 plugin 多目标支持；保存真实 Monitor→Router→Marshaller→存储中的实例证据。平台不跑 Outbox sampler，其不适用值不能用于判定业务 backlog。
7. 将常驻六个业务/Worker/Indexer 池上限从 10 调至 8；平台新增变量 `PLATFORM_API_MYSQL_MAX_OPEN_CONNS=4`，映射容器 `MYSQL_MAX_OPEN_CONNS`；平台 idle 不超过 2。`PLATFORM_API_HTTP_MAX_CONCURRENCY=32` 映射 `BACKEND_HTTP_MAX_CONCURRENCY`，业务保持每进程 128。常驻 52 + 余量 8 = 总额 60；禁止未核算的 replicas/滚动重叠。维护池上限 4，验收/探针/Exporter 合计使用剩余 4，启动前验证实际连接开销能满足。
8. Bundle 生成和 lifecycle prepare 复用 backend 镜像项，显式处理 backend-2 和 platform-api 固定别名，并核对既有 Worker/Indexer/Router/Marshaller 副本映射；不要求 manifest 增加 platform-api 镜像。保留 ownership labels 与角色/实例 labels，避免 prepare 覆盖掉运行身份。init/up/verify/down 对新增服务正确生效，生成的部署产物与仓库 Compose 一致。
9. 本批非正式 preflight 使用源码模式及独立临时项目，记录源码/config hash、实际镜像身份、目标 2.3.2 与当前完成版本 2.3.1。不得提前改 `VERSION` 或把混合版本的预检镜像声称为正式 Bundle。候选构建器保留既有版本接口；02 用直接制品/Compose 测试验证新增别名，完整冻结 Bundle 在 03 从已完成 2.3.2 构建。
10. 在现有运行入口添加下节有限用例选择/预检，保留旧默认模式；合同 checker 补运行回执核验。先验证 CLI、跨模块实际调用、schema、非零失败传播和安全清理，再建立真实 preflight 项目。所有预检边界通过后，记录 03 的命令、fixture 身份与实际耗时。

迁移为同数据库滚动到目标拓扑的有界维护窗口，不声明零停机：先保存当前配置/数据按现有操作程序，停止旧 combined 后启业务角色和平台，核对路由/就绪；避免新旧告警任务同时运行。回退使用原制品/Compose 的 combined；既有数据和插件 desired state 不丢弃，回退前停止新角色，确认只保留一套任务。实际更新不在 preflight 的临时数据上冒充生产演练。

## 5. 复用入口与有限证据合同

以下 `--suite service-split`、`--preflight`、`--preflight-receipt` 和 checker 的 evidence/publish 接口为**本批待实现扩展**，不是当前已有能力；其余参数沿用现有入口。实现、自测和真实 preflight 通过后才解锁 03：

```bash
rtk proxy python3 scripts/ci/runtime_acceptance.py --suite service-split --preflight --candidate 2.3.2 --evidence "$PHASE21_PREFLIGHT_RECEIPT"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_PREFLIGHT_RECEIPT"
rtk proxy python3 scripts/ci/runtime_acceptance.py --suite service-split --candidate 2.3.2 --manifest "$PHASE21_MANIFEST" --preflight-receipt "$PHASE21_PREFLIGHT_RECEIPT" --evidence "$PHASE21_RECEIPT"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_RECEIPT"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_RECEIPT" --publish "$PHASE21_PUBLICATION"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_RECEIPT" --verify-publication "$PHASE21_PUBLICATION"
```

`PHASE21_*` 是每次登记的绝对路径；receipt/publication 必须新建，不能覆盖证据。沿用入口自动创建的 `.run/gopulse-runtime-<id>/` 与项目归属，不新增 work/resume 接口。默认运行套件不变；有限套件不执行六插件故障矩阵、容量或长时案例。preflight 只记录已探测边界，不能生成 formal complete。02 的源码 preflight 不传 manifest；03 使用同一扩展入口的严格 manifest 模式，缺 Bundle/digest/source 绑定直接失败，禁止退回源码构建。

service-split 的 preflight 在建立真实 stack 前，由同一入口执行 `rtk go -C backend test ./internal/http -run '^TestServiceRoleAdmissionIsolation$' -count=1 -timeout=60s -v`，自动保存命令、退出码、源码/config hash 与 raw 输出，失败即停止。正式模式必须传入同一冻结 manifest 的成功 `--preflight-receipt`，核验并复用其中的准入结果；不重新跑相同确定性检查，也不复用预检中的正式 HTTP/故障结果。源码预检 receipt 不能用于正式模式。该参数只引用既有预检回执，不支持任意测试结果导入或按 case 恢复；失配/缺 raw/非零退出必须拒绝。

preflight 必须实际执行每类新增边界的一个最小操作：网关四组基路径/子路径与相似前缀负例，普通用户管理 403，真实业务写入/搜索，一个管理查询、告警规则至真实评估结果，以及一个 Redis 官方插件安装/启停/更新。旧版本包的来源、版本/hash 和当前包必须先验证并绑定；不能改包版本字符串伪造更新。另核对三个采集实例、128/128/32 限额指标、池分配、独立角色起停、顺序切换 combined 临时配置和清理。S05 的完整 60 秒持续窗口留给正式验收。准入阻塞夹具只在 Go 测试内执行，不另建延迟工具。

沿用现有 `--evidence` 回执，增加封闭的 suite/mode、S01～S07 结果与 raw 文件引用、cleanup、命令/时刻/退出码字段；不设计泛用证据框架。绑定完整源码 revision/工作树摘要、实际镜像身份、Compose/runtime-contract/工具/fixture hash、角色/实例、项目 ownership 与资源预算。正式模式另严格绑定版本、全部镜像 digest、Bundle/manifest；源码预检明确区分目标和完成版本。状态为 passed/failed/not_started，失败区分 product_failure/infrastructure_failure；失败仍导出回执并有界清理。容器、网络、卷归属及其他资源前后清单必需；只删除私有临时凭证，不删除 raw 证据。

checker 的 evidence 模式从 raw 来源和身份验证结论，拒绝漏 case、候选混用、错角色、假清理及摘要不一致。S06 同时要求同候选 Go 行为输出与真实部署配置/采集证据。preflight 可验证有限探测，禁止发布为正式完成。发布仅生成白名单 `summary.json`、`publication.json`（包含 case/身份/cleanup 摘要及来源 hash 映射），移除 password、Cookie、token、个人内容和宿主路径；对实际选定 publication 再校验。现有静态检查及历史合同保持有效，不新增通用统计、resume 或证据转换系统。

## 6. 固定门禁、成本和非正式 preflight

| gate | 命令/范围 | 通过条件 |
| --- | --- | --- |
| D01 | `rtk proxy python3 scripts/ci/verify_runtime_contracts.py`；`rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_release_*.py'`；`rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_compose_acceptance_env.py'` | 角色/固定别名、namespace 端口、数据库总额、生成 Compose 与严格 manifest 映射通过；负例实际拒绝 |
| D02 | `rtk go -C componentmetrics test ./...`；`rtk go -C backend test ./cmd/server ./internal/config ./internal/http`；`rtk go -C lifecycle test ./internal/control ./internal/release` | 身份/探针/装配/生命周期及 `TestServiceRoleAdmissionIsolation` 通过；真实 preflight 前先证明阻塞/释放边界 |
| D03 | `rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_runtime_*.py'`；两个复用入口 `--help` | 用例选择/模式、跨模块实际调用、schema、子进程失败退出、失败证据、非归属资源保护、脱敏/发布件拒绝样例通过；默认套件回归，未建真实 stack |
| D04 | 上节源码 `--preflight` 及回执检查；Linux amd64，独立临时项目 | 上节所有最小边界实际通过，含真实插件更新/告警/三个实例及清理；就绪最多 10 分钟，单恢复/异步最多 60 秒，不用 skip 成功 |
| D05 | 完成时 `rtk proxy python3 scripts/ci/validate_versions.py`；`rtk proxy python3 scripts/ci/validate_branch.py --branch develop/2.3.2 --base-ref upstream/main`；`rtk git diff --check` | 版本/分配/文档检查通过，D04 输入与 03 接口可复核 |

D04 从本批已提交产品/工具源码运行，不提前改完成 VERSION。启动前列出源码/config→镜像/执行器→gate：只构建受影响的 Backend、入口 Frontend 与确有变化的验收镜像，其他依赖使用身份明确的未变缓存；缺缓存时准备成本仍计入本批。源码模式须验证实际运行配置但不声称正式候选身份；Bundle 映射先经 D01/D02，严格 manifest 模式的解析/失配拒绝/无构建回退由 D03 自测，真实冻结 Bundle 检验归 03。记录预检与完成提交差异；最终相关配置/代码变化须重跑受影响项。

预计 **120 分钟**、累计上限 **180 分钟**（包含诊断/失败/清理）：

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 有限发现、部署/合同及生命周期开发 | 60 | 85 |
| 复用入口/用例的必要扩展与行为自测 | 20 | 30 |
| 直接产品/制品检查 | 10 | 15 |
| 增量构建/临时环境准备 | 10 | 20 |
| D04 最小边界预检与有限诊断 | 10 | 15 |
| 证据/说明/日志/提交/安全清理 | 10 | 15 |
| 合计 | 120 | 180 |

实验为 1 临时项目 ×（就绪最多 10 分钟 + 有限探测最多 8 分钟 + 单侧恢复最多 1 分钟 + 清理最多 5 分钟）；就绪、探测、清理分别计入准备、D04、清理行，诊断最多 20 分钟在总上限内从尚未使用预算重分配并记录。预检不生成正式/长期运行结论。冷构建无法容纳准备预算、工具扩展超过 30 分钟或 D04 无法在余量内完成时，保留可复核 checkpoint，修订余下范围/预算，不移到 03 隐藏。

第一次基础设施失败停止真实 preflight，最多两次各 10 分钟最小复现；复现原因、实际修正及对应 D01～D03 通过才允许重启项目。变更前记录源/config→镜像/工具→gate；只重建受影响 frontend/backend/lifecycle/工具产物，未变镜像缓存和确定性检查可复用，旧候选证据不能改绑。

## 7. 停止、接续和完成

执行 raw 保存入口自动创建的 `.run/gopulse-runtime-<id>/`，D01～D05 汇总及外部 receipt 保存新的 `.run/phase21-02-<id>/`；绑定两者及累计活跃成本。无进程内断点续跑，清理后新项目才能重跑真实 preflight。D04 与最终完成提交身份的差异必须记录；改变实际产品、工具或部署配置需重跑受影响 preflight，不能把旧结果标到新 revision。

90/144 分钟报告完成/剩余与下一步；超过累计 180 分钟、两次诊断耗尽或用户停止即停止新工作，执行最多 5 分钟归属清理并保存 incomplete checkpoint，不更新完成版本、不建完成提交。继续须修订有限剩余预算并得到用户明确继续指令。

完成条件：D01～D05 全部通过；部署/制品生成检查与真实预检共同证明角色、制品映射、会话、路由、实例及资源配置一致；复用入口扩展已实现、模式/回执/发布自测通过且预检清理有证据；同名日志和能力状态只登记本批结果；完成 `VERSION=2.3.2` 同步并提交。完整冻结 Bundle 与正式 S01～S07 结论仍待 03。
