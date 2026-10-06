# Phase-21-03：定向验收与阶段收口

> 状态：已完成。完成版本 `2.3.3` / `develop/2.3.3`；实际验收候选为冻结产品 `2.3.2`，分配以[总方案](Phase-21-总实施方案.md)为准。
> 主要结果：对完成 2.3.2 的冻结候选执行一次 S01～S07，核验真实发布证据，按实际结果收口。

> 2026-10-06 合同调整：使用 02 适配后的现有运行验收/合同校验入口；S06 采用同候选 Go 准入测试与真实配置/采集共同验证，取消真实系统延迟注入饱和步骤。三批版本、S01～S07 的功能结果、S05 的 60 秒窗口、严格候选/证据规则及 90/180 分钟预算保留。

> 2026-10-06 修复补充：首次 candidate 构建暴露候选 Bundle 服务映射缺口；经用户授权，按[Phase-21-03R 修复合同](Phase-21-03R-候选Bundle映射修复.md)只修正发布工具及其回归测试。该补充不修改产品/部署/验收标准，不重置本批累计预算；修复后的 candidate、manifest 和 receipt 必须全部重新建立。

> 2026-10-06 收口结果：revision `ca0aa5efb6f0` 的候选 `2.3.2-candidate-ca0aa5efb6f0` 完成严格 preflight 与正式 S01～S07；所有案例、清理和选定 publication 均通过 verifier。白名单证据见 `dev/logs/Phase-21/Phase-21-03-evidence/`。

## 1. 进入条件与允许文件

02 的 D01～D05、实际 preflight、同名日志及完成提交已进入 main。Linux amd64、Docker Compose v2、既有工具链与候选仓库可用；现有入口的 service-split 选择、预检、严格 manifest 模式、失败回执及清理/发布自测已经实现。任何缺口先停止本批，不边验收边开发工具。

先 fetch 主远端并从最新 `upstream/main` 创建 `develop/2.3.3`。记录 main 中完成 `2.3.2` 的具体 revision，在独立干净的冻结 checkout 构建/执行，任务工作树中的日志不改变该 checkout。候选的代码/部署/工具与 Bundle 身份冻结，正式执行期间不改入口、镜像、验收标准、manifest 或回执。

| 允许修改 | 精确文件/路径 |
| --- | --- |
| 实际日志与白名单发布证据 | `dev/logs/Phase-21/Phase-21-03-定向验收与阶段收口.md`；`dev/logs/Phase-21/Phase-21-03-evidence/` 内 `summary.json`、`publication.json`（由 02 checker 白名单生成，含有限 case/身份/cleanup 摘要与来源 hash） |
| 当前能力事实 | `dev/status/capability-status.md`；只登记实际交付、候选证据与限制 |
| 按需修复导航链接 | `dev/phases/Phase-21-用户业务与管理服务分离.md`、`dev/phases/README.md`、`dev/README.md`、`docs/README.md`、`README.md`；链接仍有效时无需修改，不复制版本分配、执行状态或验收数值 |
| 本阶段合同的实际完成标记 | 本总方案/本分方案；只能登记结果与偏差，不追溯弱化验收标准 |
| 成功后的版本元数据 | `VERSION`、`.env.example` 的版本字段、两个前端的 `package.json`/`package-lock.json`、`deploy/runtime-contracts.json` 的产品版本字段 |

产品/测试/工具源码、正式配置与历史日志不在原始执行范围。若出现需修正的产品或验收工具失败，先保存候选/结果及归属清理，停止正式矩阵；形成合法的新修复合同与有限预算后才可修正，不能在本批偷改候选或手修 evidence。本次工具修复仅按上方有界补充合同执行。

`dev/phases/Plan.md` 保留 2026-10-05 历史快照，不属于本批收口修改范围。

本批完成版本 `2.3.3` 仅登记验收与版本元数据；实际被测制品是冻结的 `2.3.2`。收口文档必须区分这两者，不声称测试过重新标成 2.3.3 的镜像。元数据提交不替换 candidate tags，也不产生未执行的发布回执。

## 2. 复用覆盖与本批验证边界

| 验证对象 | 已有覆盖 / 剩余缺口 | 最低有效层 / 本批动作 / 固定门禁 |
| --- | --- | --- |
| 装配、权限与并发隔离 | 01 Go 角色/权限/准入测试及真实起停已经开发完成；02 核对实际部署，但不能代替同一冻结候选 | 复核冻结源码的确定性测试；真实角色起停、同 Cookie 授权及 S06 配置/实例另取证。S01/S02/S06 |
| 真实业务与管理流程 | 02 复用 `compose-business.spec.ts`、`admin-frontend.spec.ts`、`phase15-closure.spec.ts` 及 HTTP 操作完成短预检；缺完整最终部署功能结果 | 现有 `runtime_acceptance.py --suite service-split` 选择已有/补充的有限用例，浏览器证明两个前端，真实存储/HTTP 证明具体事实。S02/S03/S04 |
| 单侧停止与资源清理 | 现有入口管理 Compose，02 补停启、网络/卷/凭证清理及行为自测；短预检没有完成 60 秒业务连续性或正式候选清理 | 同一候选真实故障窗口、恢复与资源前后清单。S05/S07；Go/静态通过不能替代 |
| 候选与发布件身份 | 现有 `release_artifacts.py`/`release_manifest.py` 保留严格 Bundle；02 扩展 `verify_runtime_contracts.py` 回执/发布检查并自测 | 严格制品/原始证据核验与实际 publication 复核；不新增执行器、不改源码。全体 case 及完成门禁 |

本批只执行、取证、收口；不补用例或夹具。02 的预检只解锁正式运行，不能复制成正式 passed 回执。

## 3. 冻结、preflight 与执行命令

以下扩展接口在 02 实现后使用，本文件不把未实现命令视为现成能力。执行者登记候选仓库/新候选目录、`PHASE21_MANIFEST`、`PHASE21_PREFLIGHT_RECEIPT`、`PHASE21_RECEIPT`、`PHASE21_PUBLICATION` 的绝对路径；原始运行目录沿用入口自动创建的 `.run/gopulse-runtime-<id>/`。命令依次执行，任何前置失败即停：

```bash
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --candidate 2.3.2
rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_runtime_*.py'
rtk proxy python3 scripts/ci/release_artifacts.py build --registry "$PHASE21_REGISTRY" --platform linux/amd64 --output "$PHASE21_CANDIDATE_DIR"
rtk proxy python3 scripts/ci/runtime_acceptance.py --suite service-split --preflight --candidate 2.3.2 --manifest "$PHASE21_MANIFEST" --evidence "$PHASE21_PREFLIGHT_RECEIPT"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_PREFLIGHT_RECEIPT"
rtk proxy python3 scripts/ci/runtime_acceptance.py --suite service-split --candidate 2.3.2 --manifest "$PHASE21_MANIFEST" --preflight-receipt "$PHASE21_PREFLIGHT_RECEIPT" --evidence "$PHASE21_RECEIPT"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_RECEIPT"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_RECEIPT" --publish "$PHASE21_PUBLICATION"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_RECEIPT" --verify-publication "$PHASE21_PUBLICATION"
```

先完成静态/工具确定性检查，再构建；已有相关输入、候选元数据与环境不变的合格结果可复用，须保留实际输出与 hash，不能只填 passed。严格 manifest preflight 在启动 stack 前自动执行冻结源码的准入 Go 测试，命令及 60 秒期限由 02 规定；回执绑定命令、退出码、raw 输出与源码/config hash。正式模式通过 `--preflight-receipt` 核验并复用这项确定性结果，真实功能/故障案例仍重新执行。构建用已配置/授权的候选仓库，禁止 promote 或替换已有标签；若已有完全相同 revision/Bundle/digest 的合格候选则复用，不为开始本批重复重建。构建前列出源码/config 与各产物的对应，保留缓存。Bundle/manifest 完整验证后才允许启动真实 preflight；成功且有清理回执才允许正式矩阵，禁止使用源码模式回退。

绑定 source 完整 revision、candidate=2.3.2、Backend 共用 digest、所有镜像/Bundle/manifest、Compose/runtime-contract、工具/案例/schema、环境与资源预算、instance/role/project identity。直接运行和证据校验必须使用同一工具 revision，不能临时引用工作树助手。最终完成 metadata 检查在任务分支执行，不能反向改冻结 checkout。

## 4. S01～S07 固定案例

只执行以下有限功能/权限/故障案例；[总方案](Phase-21-总实施方案.md)定义阶段结果，02 编写的 `dev/validation/Phase-21/phase21-split.md` 记录实际步骤/fixture/schema。S06 的分层方法已在本次权威合同中明确修订；执行时不得再删案例、缩 S05 窗口或扩大为 Phase 20 持续容量矩阵。

| case_id | 动作与通过条件 | 固定期限/原始证据 |
| --- | --- | --- |
| S01 | 在目标 Compose 逐一确认两个业务实例和平台角色，私有探针/路由互斥；停平台时业务仍就绪，停业务时平台能独立起停并查询已有管理数据；默认 combined 用一次最小兼容启动验证；SIGTERM 各角色无遗留任务 | 项目就绪最多 10 分钟；每次停止按 Backend 5 秒、Compose grace 15 秒；角色配置、实际路由、probe、退出码/时刻和 task 证据 |
| S02 | 同源用户端注册/登录，管理端复用管理员会话；未登录管理 401、普通用户 403、正确管理员成功；用非引导账号升/降级，原 Cookie 下一次管理请求即遵从新角色；引导账号降级被拒，业务内容不泄漏角色 | 每步请求遵守现有 HTTP 超时；两前端至少各一条真实浏览器路径及 HTTP/数据库角色回执，不发布凭证 |
| S03 | 用户发帖后，经真实 Outbox/RabbitMQ/Indexer 搜索可见；评论和现有通知闭环；读详情/列表、编辑、点赞、收藏/取消、关注/取消及退出作为代表性兼容回归 | 每个异步结果最多 60 秒，失败即记录，不延长直到成功；真实请求、业务事实/Outbox、消费者与搜索/通知结果；此期限仅是本案例门禁 |
| S04 | 管理端查询 Metrics/Logs/Events；创建/启用一个规则、真实评估生成内部告警并查询，随后恢复规则；使用既有官方插件包完成安装、启动、停止、更新与状态查询，确认 Monitor 仍为唯一生命周期所有者 | 查询/控制沿用现有超时；等待一个评估/采集结果最多 60 秒；查询响应、alert/插件持久结果和真实 Event，不接受伪造数据替代 |
| S05 | 停止 platform-api，保持已有普通用户 Cookie，在完整 60 秒窗口每 5 秒执行一次详情/列表/通知请求，并完成一次发帖与评论；所有既有同步业务请求按原合同成功、搜索仍可见；管理请求失败如实记录；启动平台后同会话继续管理 | 不可压缩的故障观察 60 秒；业务异步最多 60 秒；恢复平台就绪最多 60 秒；逐请求时刻/状态/摘要和容器状态，业务不经管理网络鉴权 |
| S06 | 同候选 `TestServiceRoleAdmissionIsolation` 用测试内阻塞 handler 证明 business=128/platform=32 各自满额 503、另一角色及四探针可用、释放后恢复；真实部署直连三个进程，限额指标为 128/128/32 且实例为 backend-1/backend-2/platform-api-1，采集/存储可区分并关联职责；常驻 MySQL 上限 52、含余量总额 60，临时任务不超支 | Go 每角色阻塞最多 10 秒，总命令最多 60 秒；实际采集结果等待最多 60 秒。必须同时有 Go 原始结果/源码绑定、真实 probe/限额指标、池配置及存储 instance；不做真实系统饱和或容量结论 |
| S07 | 核对生成 Bundle 与实际 Compose 的角色/同镜像、内部端口和有限实例；运行 lifecycle 状态/verify；终止 runner 所属任务后 down/清理项目容器/网络/卷和 owned 临时凭证；其他项目资源不变 | 清理最多 5 分钟；资源所有权/前后清单、镜像/manifest/digest、退出码、零残留与非归属资源保护回执 |

S01 的停业务及 combined 检查使用已预检的同候选临时配置，在同一 owned 项目内顺序切换，不让 combined 与正式告警/dispatcher 同时争用数据；副本/pool 总额仍需满足合同。S06 从验收容器直连各进程私网端口核对实际限额，避免业务 upstream 分流掩盖实例配置。准入语义由实际 router/配置接线的 Go 测试证明，实际部署和采集由真实检查证明，任一缺失均失败；不引入真实延迟夹具、不用低槽位结果冒充 128/32。临时数据库客户端仍登记资源上限。

每 case 记录前置身份、操作、期望、实际结果、窗口/期限、raw 文件来源、资源归属、passed/failed/not_started 与失败分类。产品行为不符为 product_failure；启动工具、依赖夹具、身份漂移、schema、证据或清理错误为 infrastructure_failure。任意固定门禁失败、未执行或缺证据均不能声明阶段完成。基础设施失败不能推出产品能力结论；共享数据库故障也不能声称由服务分离隔离。

## 5. 预算与不可压缩实验时间

预计总活跃时间 **90 分钟**，跨所有候选/目录/诊断累计上限 **180 分钟**。

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 身份、工具、确定性 preflight | 15 | 30 |
| 缓存构建/Bundle、环境建立 | 20 | 40 |
| 一次 S01～S07 与有限诊断 | 30 | 60 |
| 证据核验/脱敏发布 | 15 | 25 |
| 日志/版本/提交与安全清理 | 10 | 25 |
| 合计 | 90 | 180 |

不可压缩观察为 1 × 60 秒平台停止窗口；准入 Go 测试为 2 × 最多 10 秒阻塞，真实恢复/异步单次最多 60 秒，不转为长时测量。成本按 1 个正式项目 ×（最多 10 分钟就绪 + 固定案例最多 20 分钟 + 最多 5 分钟清理），另计 1 个短 preflight 项目、构建、证据核验和最多 20 分钟诊断，均落在上表。临时角色配置在项目内顺序切换，不额外并行建 stack。未证明长期稳定、容量或观测开销，不用短运行外推。

第一次基础设施失败立即停止整组，最多两次各 10 分钟在最小边界诊断，包含在 180 分钟内。本批不修改工具/产品；若诊断发现修正需求，保存 failed/not_started 与修复建议，转为有明确预算的新合同后再执行。仅有环境配置误操作且不改变冻结合同，可先纠正、跑受影响 preflight，再在新目录重跑；新候选或重试不重置预算。

## 6. 证据接续、发布与阶段完成

原始运行结果留在自动创建的 `.run/gopulse-runtime-<id>/`，同候选 Go 输出、门禁汇总及外部 receipt 留在新的 `.run/phase21-03-<id>/`，登记两者绑定，不得手改回执。私有目录按既有规则保护；passed/failed/not_started、累计成本、命令与环境身份在退出前落盘。复用入口不支持单 case/进程内 resume：清理后重新建立项目；相同身份的确定性检查可以保留，不拿旧正式请求或故障窗口拼接成另一候选的完整矩阵。

到 90/144 分钟报告已过案例、未开始 gate、累计成本与下一步。剩余预算不足以跑预计命令及清理、累计到 180 分钟、诊断两次耗尽或用户停止时，停止新工作，执行归属清理，保留真实 checkpoint，记录具体续作路径。不更新完成 VERSION，不建完成提交；额外执行先修订有限计划并由用户明确要求继续。

固定门禁通过后，先用现有 checker 的 evidence 扩展验证 private receipt/raw，再白名单生成本批 `summary.json`、`publication.json`，对**实际选定 publication**再次验证。日志登记候选 2.3.2 和完成 2.3.3 的区别、case 结果、分层 S06 的证据范围、共享依赖限制与实际清理。没有真实 source→publication 映射/校验不能宣布收口。

最终完成检查：

```bash
rtk proxy python3 scripts/ci/validate_versions.py
rtk proxy python3 scripts/ci/validate_branch.py --branch develop/2.3.3 --base-ref upstream/main
rtk git diff --check
```

完成条件：冻结同一候选 S01～S07 全部 passed、formal execution complete、归属清理完整、选定脱敏证据严格验证；登记能力状态、本阶段合同完成标记和同名日志，导航仅按需修复链接，成功后 `VERSION=2.3.3` 及登记元数据同步并提交。本阶段只声明业务/管理运行与代表性流程/单侧停止通过；共享 MySQL 故障、状态层 HA、独立 dispatcher、多目标、容量与长期稳定仍未验证。
