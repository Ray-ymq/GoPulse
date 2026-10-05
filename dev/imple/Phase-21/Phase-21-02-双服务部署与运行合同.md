# Phase-21-02：双服务部署与运行合同

> 状态：未开始。目标 `2.3.2` / `develop/2.3.2`，分配以[总方案](Phase-21-总实施方案.md)为准。
> 主要结果：最终 Compose 将业务和管理分开运行，两个前端经原同源入口访问正确角色；运行/采集、Bundle 和定向工具合同一致。

## 1. 进入条件与范围

01 的固定门禁、同名日志和完成 `2.3.1` 提交已进入 main。先 fetch 主远端、重读总方案/01 日志，从最新 `upstream/main` 创建 `develop/2.3.2`。角色产品实现已可用，不把 01 未完成问题转移到本文件。

本批改部署及其直接支撑代码，并实现范围固定的 Phase 21 定向 runner/verifier。产品与工具在本批检查，正式冻结候选验收归 03。不增加业务、第二种 Backend 镜像、采集源类型或泛用矩阵框架，不重跑容量认证。

## 2. 允许修改的精确文件

| 用途 | 文件 |
| --- | --- |
| 部署/同源代理 | `deploy/compose.yaml`、`deploy/compose.debug.yaml`、`deploy/docker/frontend/nginx.conf`、`.env.example` |
| 运行/采集角色元数据 | `deploy/runtime-contracts.json`、`deploy/runtime-contracts.schema.json`、`componentmetrics/runtime.go`、`runtime_test.go`、`config.go`、`probe.go`、`probe_test.go`、`backend/cmd/server/roles.go`、`roles_test.go`、`backend/cmd/server/main.go`（仅运行身份接线） |
| 合同验证与 Compose 入口 | `scripts/ci/verify_runtime_contracts.py`、`test_runtime_contracts.py`、`test_compose_acceptance_env.py`、`scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh`（仅补新增角色环境/拓扑兼容） |
| Bundle/生命周期固定别名与环境 | `scripts/ci/release_artifacts.py`、`release_manifest.py`、`test_release_manifest.py`、`test_release_snapshot.py`；新增 `scripts/ci/test_release_candidate_version.py`；`lifecycle/internal/control/compose.go`、`compose_test.go`（新增）、`control_test.go`、`lifecycle/internal/release/manifest.go`、`manifest_test.go` |
| 定向工具（新增） | `scripts/verify-phase21-split.sh`、`scripts/verify-phase21-evidence.py`、`scripts/ci/phase21_split.py`、`scripts/ci/phase21_evidence.py`、`scripts/ci/test_phase21_split.py`、`scripts/ci/test_phase21_evidence.py` |
| 说明/工具合同 | `dev/contracts/runtime-contracts.md`、`dev/contracts/component-metrics.md`、`dev/validation/README.md`；新增 `dev/validation/Phase-21/phase21-split.md`；`deploy/release/BUNDLE-README.md`、`deploy/release/README.md`、`backend/README.md`、`README.md`、`使用手册.md`、`dev/status/capability-status.md` |
| 完成时版本/记录 | `VERSION`、两个前端的 `package.json`/`package-lock.json`，本分方案必要修订，`dev/logs/Phase-21/Phase-21-02-双服务部署与运行合同.md` |

新工具优先调用现有候选/Compose/浏览器/插件夹具公共接口；不修改历史 Phase runner 或证据。发现实际被调用助手名或源文件超出集合时，先记录精确路径、必要性与预算影响，不能把范围扩为全生命周期重写。两个前端 API 合同无须改动，界面代码不在允许集合。

## 3. 迁移步骤与确定决策

1. 保留 `backend`/`backend-2` 名称与双副本，设 role=business；新增 `platform-api` 单实例 role=platform，使用同一个 `GOPULSE_BACKEND_IMAGE` digest。保留共享 MySQL、会话 secret/Cookie/引导账号配置，不创建新数据卷。旧环境未设 role 仍可 combined。
2. 按角色删减环境/依赖：business 不等待观测查询 ES/管理 API；platform 不依赖 Redis、RabbitMQ 和业务 ES，不启动业务后台任务。两个角色所需日志投递网络和 shared MySQL 保留；网络连接仅按真实依赖取舍，不把共享 Monitor/状态层说成完整隔离。四个探针有各自所属角色语义。
3. 在实际入口 `deploy/docker/frontend/nginx.conf` 建 platform upstream；用总方案列出的四组精确基路径和子路径转发。其余 API 与登录/current-user 仍到业务池，两个前端继续同源。保留插件上传 65 MiB 限制和既有 30 秒控制超时；错误不得反向回退到业务池。直接服务端口只在私网，debug override 只允许显式 loopback 调试。
4. 登记 `platform-api` 为封闭运行单元：role、Compose 服务、探针/关停、pool=4、HTTP=32、instance=`platform-api-1`、制品与指标 component 别名均为 backend。运行合同升至版本 `2`，探针与验证程序同源同步；原固定指标目录和业务响应不变。固定 inventory 由 12 个运行单元增至 13 个，不能放宽为任意字符串。
5. 修改当前校验中“进程 ID 必须等于指标组件”和“全局监听端口唯一”的假设：显式白名单别名 platform-api→backend；端口只在同一命名空间要求无冲突，backend 与 platform 容器可共用 8080/19101。继续严格检查角色、真实入口、source/metrics 目录、实例、私网、探针及版本；不能删除门禁或绕过动态装配检查。
6. `BACKEND_ENDPOINTS=backend,backend-2,platform-api`，仍走 Backend token/固定 families，沿用 `GOPULSE_INSTANCE_ID` 区分实例。不改 Monitor 收集算法或增加 plugin 多目标支持；保存真实 Monitor→Router→Marshaller→存储中的实例证据。平台不跑 Outbox sampler，其不适用值不能用于判定业务 backlog。
7. 将常驻六个业务/Worker/Indexer 池上限从 10 调至 8；平台新增变量 `PLATFORM_API_MYSQL_MAX_OPEN_CONNS=4`，映射容器 `MYSQL_MAX_OPEN_CONNS`；平台 idle 不超过 2。`PLATFORM_API_HTTP_MAX_CONCURRENCY=32` 映射 `BACKEND_HTTP_MAX_CONCURRENCY`，业务保持每进程 128。常驻 52 + 余量 8 = 总额 60；禁止未核算的 replicas/滚动重叠。维护池上限 4，验收/探针/Exporter 合计使用剩余 4，启动前验证实际连接开销能满足。
8. Bundle 生成和 lifecycle prepare 复用 backend 镜像项，显式处理 backend-2 和 platform-api 固定别名，并核对既有 Worker/Indexer/Router/Marshaller 副本映射；不要求 manifest 增加 platform-api 镜像。保留 ownership labels 与角色/实例 labels，避免 prepare 覆盖掉运行身份。init/up/verify/down 对新增服务正确生效，生成的部署产物与仓库 Compose 一致。
9. 候选构建器新增有界 `build --candidate-version 2.3.2` 接口（待实现）：只接受当前已分配批次目标，默认仍读取完成 VERSION。构建参数、镜像标签/OCI 元数据、manifest 和生成 Bundle 的产品版本统一使用目标；只在生成工件内同步 runtime contract 的 product_version，源树 VERSION/版本字段不改写。绑定源码 hash 与生成后合同 hash，记录唯一版本字段变换，行为配置必须原样。拒绝任意跳版本、未分配目标、非干净源码、已有 manifest 覆盖以及 source/artifact 身份不符，增加直接自测。不能从取消批次借用候选。
10. 实现下节有限 runner/verifier，先验证入口模式、助手调用、schema、非零失败传播、证据身份及安全清理，再建立真实 preflight 项目。preflight 通过后冻结工具合同，记录 03 命令、文件/二进制与实际耗时。

迁移为同数据库滚动到目标拓扑的有界维护窗口，不声明零停机：先保存当前配置/数据按现有操作程序，停止旧 combined 后启业务角色和平台，核对路由/就绪；避免新旧告警任务同时运行。回退使用原制品/Compose 的 combined；既有数据和插件 desired state 不丢弃，回退前停止新角色，确认只保留一套任务。实际更新不在 preflight 的临时数据上冒充生产演练。

## 4. 定向工具接口与证据合同

以下接口为**本批待实现**，当前仓库不能直接调用；实现、`--help`、模式/参数错误自测和真实 preflight 通过后才解锁 03：

```bash
rtk proxy bash scripts/verify-phase21-split.sh --self-test
rtk proxy bash scripts/verify-phase21-split.sh --preflight --manifest "$PHASE21_MANIFEST" --work "$PHASE21_PREFLIGHT"
rtk proxy bash scripts/verify-phase21-split.sh --manifest "$PHASE21_MANIFEST" --work "$PHASE21_RUN"
rtk proxy python3 scripts/verify-phase21-evidence.py --source "$PHASE21_RUN"
rtk proxy python3 scripts/verify-phase21-evidence.py --source "$PHASE21_RUN" --publish "$PHASE21_PUBLICATION"
rtk proxy python3 scripts/verify-phase21-evidence.py --source "$PHASE21_RUN" --verify-publication "$PHASE21_PUBLICATION"
```

`PHASE21_*` 是每次执行登记的绝对路径，work 必须新建且不能覆盖旧证据。preflight 与正式模式互斥；preflight 只验证可执行边界和代表性短操作，不能生成正式 S01～S07 全通过回执。manifest 必须是既有严格 release manifest/Bundle，验证实际镜像 digest 和 source revision；不接受仅填写版本/revision 的占位 JSON。

工具只运行总方案 S01～S07，一次归属明确的 Compose 项目，按固定顺序执行。用真实 HTTP/浏览器和现有插件包，不新建生产延迟端点；隔离测试依赖可有受控短延迟用于准入。正式行为定义在 03，与 `phase21-split.md` 保持引用关系，验收文档不另减门禁。

证据固定结构至少含 `candidate.json`、`cases.json`、`raw/`、`cleanup.json`、`summary.json`。绑定完整 source revision、版本、Backend/其余镜像 digest、Bundle/manifest/Compose/runtime-contract/工具/脱敏输入 hash、运行角色与实例、project ownership、执行环境及命令/时刻/退出码。状态分别为 passed/failed/not_started，结果区分 product_failure/infrastructure_failure；skip/缺证据不能成功。每 case 记动作、期限、请求结果和来源，失败非零退出且仍有有界 cleanup。

verifier 从原始结果和身份计算结论，拒绝漏 case、候选混用、错角色、假清理及手改摘要。发布只输出安全白名单摘要/回执，移除 password、Cookie、token、个人内容和宿主路径；保留 source→publication 文件映射/hash。最后验证实际 publication，不能用未选中的 private 结果代替。工具不提供进程内或单 case 恢复参数；清理后需新项目重跑，不能承诺 `--resume`。

## 5. 固定门禁、成本和有限 preflight

| gate | 命令/范围 | 通过条件 |
| --- | --- | --- |
| D01 | `rtk proxy python3 scripts/ci/verify_runtime_contracts.py`；`rtk proxy python3 -m unittest scripts.ci.test_runtime_contracts scripts.ci.test_compose_acceptance_env scripts.ci.test_release_manifest scripts.ci.test_release_snapshot scripts.ci.test_release_candidate_version`（最后一个新增） | 角色/固定别名、namespace 端口、数据库总额、Bundle/Compose 映射及候选版本变换严格通过；负例实际拒绝 |
| D02 | `rtk go -C componentmetrics test ./...`；`rtk go -C backend test ./cmd/server ./internal/config ./internal/http`；`rtk go -C lifecycle test ./internal/control ./internal/release` | 受影响身份/探针/装配/生命周期回归通过 |
| D03 | 上节 `--self-test` 与 evidence 自测（由 self-test 调用） | CLI、跨模块方法、schema、失败退出、非归属资源保护、脱敏/verifier 拒绝样例通过，未建真实 stack |
| D04 | 上节 `--preflight`；当前提交目标制品，Linux amd64，有界临时项目 | 新角色起停、登录/一个管理查询、网关四组路由、一个业务写入、插件控制边界、三个采集实例、pool 上限与清理实际通过；最长等待就绪 10 分钟，单恢复 60 秒 |
| D05 | 完成时 `rtk proxy python3 scripts/ci/validate_versions.py`；`rtk proxy python3 scripts/ci/validate_branch.py --branch develop/2.3.2 --base-ref upstream/main`；`rtk git diff --check` | 版本/分配/文档检查通过，D04 身份及工具合同可复核 |

D04 候选从本批已提交产品/工具源构建；不得为构建提前改写完成 VERSION。使用本批补齐的 `scripts/ci/release_artifacts.py build --candidate-version 2.3.2 --registry <已配置候选仓库> --platform linux/amd64 --output <新目录>`（candidate-version 参数待实现），复用缓存。只用已授权/配置的候选仓库，不 promote、替换发布标签或重写完整 manifest。该构建接口和生成后的严格 Bundle/合同检查属于 D01/D03 前置，缺失则 D04 不可开始；03 不承担此工具开发。

预计 **120 分钟**、累计上限 **180 分钟**（包含诊断/失败/清理）：

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 有限发现、部署/合同与工具实现 | 55 | 70 |
| 直接检查、自测 | 15 | 20 |
| 构建/Bundle/临时环境准备 | 20 | 30 |
| D04 短 preflight 与诊断 | 20 | 35 |
| 证据/说明/日志/提交/清理 | 10 | 25 |
| 合计 | 120 | 180 |

实验为 1 临时项目 ×（就绪最多 10 分钟 + 路由/权限/采集短探测最多 5 分钟 + 恢复最多 1 分钟 + 清理最多 5 分钟）；实际 setup/build 与诊断另按上表计时，预检不作为长期运行证据。若首个冷构建无法容纳 30 分钟预算，停在真实可复核 checkpoint，重新规划余下工作，不移到 03 隐藏。

第一次基础设施失败停止真实 preflight，最多两次各 10 分钟最小复现；复现原因、实际修正及对应 D01～D03 通过才允许重启项目。变更前记录源/config→镜像/工具→gate；只重建受影响 frontend/backend/lifecycle/工具产物，未变镜像缓存和确定性检查可复用，旧候选证据不能改绑。

## 6. 停止、接续和完成

证据保存 `.run/phase21-02-<id>/`；记录 D01～D05 和 preflight 子项状态、所有候选/config/工具摘要及累计活跃成本。无进程内断点续跑，清理后新项目才能重跑真实 preflight。D04 与最终完成提交身份的差异必须记录；改变实际产品、工具或部署配置需重跑受影响 preflight，不能把旧结果标到新 revision。

90/144 分钟报告完成/剩余与下一步；超过累计 180 分钟、两次诊断耗尽或用户停止即停止新工作，执行最多 5 分钟归属清理并保存 incomplete checkpoint，不更新完成版本、不建完成提交。继续须修订有限剩余预算并得到用户明确继续指令。

完成条件：D01～D05 全部通过；最终部署与生成 Bundle 的角色、制品、会话、路由、实例和资源配置一致；工具接口/schema/实际调用已可执行且 preflight 清理有证据；同名日志和能力状态只登记本批结果；完成 `VERSION=2.3.2` 同步并提交。正式 Phase 21 功能/故障结论仍待 03。
