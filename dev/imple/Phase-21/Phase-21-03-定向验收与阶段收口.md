# Phase-21-03：定向验收与阶段收口

> 状态：未开始。目标 `2.3.3` / `develop/2.3.3`，分配以[总方案](Phase-21-总实施方案.md)为准。
> 主要结果：对完成 2.3.2 的冻结候选执行一次 S01～S07，核验真实发布证据，按实际结果收口。

> 2026-10-06 文档维护修订：收口事实集中到能力状态与本阶段合同，导航只按需维护链接；
> 批次分配、S01～S07、候选身份、90/180 分钟预算与完成门禁保持原合同。

## 1. 进入条件与允许文件

02 的 D01～D05、实际 preflight、同名日志及完成提交已进入 main。Linux amd64、Docker Compose v2、既有工具链与候选仓库可用；03 工具的 `--help`/self-test、身份/schema、失败传播与清理已经实现。任何缺口先停止本批，不边验收边开发工具。

先 fetch 主远端并从最新 `upstream/main` 创建 `develop/2.3.3`。记录 main 中完成 `2.3.2` 的具体 revision，在独立干净的冻结 checkout 构建/执行，任务工作树中的日志不改变该 checkout。候选的代码/部署/工具与 Bundle 身份冻结，正式执行期间不改入口、镜像、验收标准、manifest 或回执。

| 允许修改 | 精确文件/路径 |
| --- | --- |
| 实际日志与白名单发布证据 | `dev/logs/Phase-21/Phase-21-03-定向验收与阶段收口.md`；`dev/logs/Phase-21/Phase-21-03-evidence/` 内 `candidate.json`、`cases.json`、`cleanup.json`、`summary.json`、`publication.json`（由 02 工具实际安全 schema 生成） |
| 当前能力事实 | `dev/status/capability-status.md`；只登记实际交付、候选证据与限制 |
| 按需修复导航链接 | `dev/phases/Phase-21-用户业务与管理服务分离.md`、`dev/phases/README.md`、`dev/README.md`、`docs/README.md`、`README.md`；链接仍有效时无需修改，不复制版本分配、执行状态或验收数值 |
| 本阶段合同的实际完成标记 | 本总方案/本分方案；只能登记结果与偏差，不追溯弱化验收标准 |
| 成功后的版本元数据 | `VERSION`、`.env.example` 的版本字段、两个前端的 `package.json`/`package-lock.json`、`deploy/runtime-contracts.json` 的产品版本字段 |

产品/测试/工具源码、正式配置与历史日志不在允许范围。若出现需修正的产品或验收工具失败，先保存候选/结果及归属清理，停止正式矩阵；形成合法的新修复合同与有限预算后才可修正，不能在本批偷改候选或手修 evidence。

`dev/phases/Plan.md` 保留 2026-10-05 历史快照，不属于本批收口修改范围。

本批完成版本 `2.3.3` 仅登记验收与版本元数据；实际被测制品是冻结的 `2.3.2`。收口文档必须区分这两者，不声称测试过重新标成 2.3.3 的镜像。元数据提交不替换 candidate tags，也不产生未执行的发布回执。

## 2. 冻结、preflight 与执行命令

以下 Phase 21 接口在 02 实现后使用，本文件不把未实现命令视为现成能力。执行者登记 `PHASE21_MANIFEST`、`PHASE21_PREFLIGHT`、`PHASE21_RUN`、`PHASE21_PUBLICATION` 绝对路径，使用新的目录。

```bash
rtk proxy python3 scripts/ci/release_artifacts.py build --registry "$PHASE21_REGISTRY" --platform linux/amd64 --output "$PHASE21_CANDIDATE_DIR"
rtk proxy python3 scripts/ci/verify_runtime_contracts.py --candidate 2.3.2
rtk proxy bash scripts/verify-phase21-split.sh --self-test
rtk proxy bash scripts/verify-phase21-split.sh --preflight --manifest "$PHASE21_MANIFEST" --work "$PHASE21_PREFLIGHT"
rtk proxy bash scripts/verify-phase21-split.sh --manifest "$PHASE21_MANIFEST" --work "$PHASE21_RUN"
rtk proxy python3 scripts/verify-phase21-evidence.py --source "$PHASE21_RUN"
rtk proxy python3 scripts/verify-phase21-evidence.py --source "$PHASE21_RUN" --publish "$PHASE21_PUBLICATION"
rtk proxy python3 scripts/verify-phase21-evidence.py --source "$PHASE21_RUN" --verify-publication "$PHASE21_PUBLICATION"
```

构建用已配置/授权的候选仓库，禁止 promote 或替换已有标签；若已有完全相同 revision/Bundle/digest 的合格候选则复用，不为开始本批重复重建。构建前列出源码/config 与各产物的对应，保留缓存。Bundle/manifest 完整验证后才允许启动 real preflight，preflight 成功且有清理回执才允许正式矩阵。

绑定 source 完整 revision、candidate=2.3.2、Backend 共用 digest、所有镜像/Bundle/manifest、Compose/runtime-contract、工具/案例/schema、环境与资源预算、instance/role/project identity。直接运行和证据校验必须使用同一工具 revision，不能临时引用工作树助手。最终完成 metadata 检查在任务分支执行，不能反向改冻结 checkout。

## 3. S01～S07 固定真实案例

只执行以下有限功能/权限/故障案例；[总方案](Phase-21-总实施方案.md)定义阶段结果，02 编写的 `dev/validation/Phase-21/phase21-split.md` 记录已实现步骤/schema。不得删案例、缩测量窗口或扩大为 Phase 20 持续容量矩阵。

| case_id | 动作与通过条件 | 固定期限/原始证据 |
| --- | --- | --- |
| S01 | 在目标 Compose 逐一确认两个业务实例和平台角色，私有探针/路由互斥；停平台时业务仍就绪，停业务时平台能独立起停并查询已有管理数据；默认 combined 用一次最小兼容启动验证；SIGTERM 各角色无遗留任务 | 项目就绪最多 10 分钟；每次停止按 Backend 5 秒、Compose grace 15 秒；角色配置、实际路由、probe、退出码/时刻和 task 证据 |
| S02 | 同源用户端注册/登录，管理端复用管理员会话；未登录管理 401、普通用户 403、正确管理员成功；用非引导账号升/降级，原 Cookie 下一次管理请求即遵从新角色；引导账号降级被拒，业务内容不泄漏角色 | 每步请求遵守现有 HTTP 超时；两前端至少各一条真实浏览器路径及 HTTP/数据库角色回执，不发布凭证 |
| S03 | 用户发帖后，经真实 Outbox/RabbitMQ/Indexer 搜索可见；评论和现有通知闭环；读详情/列表、编辑、点赞、收藏/取消、关注/取消及退出作为代表性兼容回归 | 每个异步结果最多 60 秒，失败即记录，不延长直到成功；真实请求、业务事实/Outbox、消费者与搜索/通知结果；此期限仅是本案例门禁 |
| S04 | 管理端查询 Metrics/Logs/Events；创建/启用一个规则、真实评估生成内部告警并查询，随后恢复规则；使用既有官方插件包完成安装、启动、停止、更新与状态查询，确认 Monitor 仍为唯一生命周期所有者 | 查询/控制沿用现有超时；等待一个评估/采集结果最多 60 秒；查询响应、alert/插件持久结果和真实 Event，不接受伪造数据替代 |
| S05 | 停止 platform-api，保持已有普通用户 Cookie，在完整 60 秒窗口每 5 秒执行一次详情/列表/通知请求，并完成一次发帖与评论；所有既有同步业务请求按原合同成功、搜索仍可见；管理请求失败如实记录；启动平台后同会话继续管理 | 不可压缩的故障观察 60 秒；业务异步最多 60 秒；恢复平台就绪最多 60 秒；逐请求时刻/状态/摘要和容器状态，业务不经管理网络鉴权 |
| S06 | 以隔离短延迟依赖各保持业务/管理请求，分别填满声明的 128/32 槽位并观察额外请求 503；另一角色及四探针可用，释放后恢复；采集端/存储可查询三个 Backend 实例，角色能对应；实际常驻 MySQL 上限 52、总额含余量 60，新增任务不超支 | 每角色槽位保持最多 10 秒，恢复最多 30 秒；一次项目内有界操作，不测 RPS/P95；raw admission/probe/连接配置、Monitor/存储 instance 证据 |
| S07 | 核对生成 Bundle 与实际 Compose 的角色/同镜像、内部端口和有限实例；运行 lifecycle 状态/verify；终止 runner 所属任务后 down/清理项目容器/网络/卷和 owned 临时凭证；其他项目资源不变 | 清理最多 5 分钟；资源所有权/前后清单、镜像/manifest/digest、退出码、零残留与非归属资源保护回执 |

S01 的停业务及 combined 检查在独立的同候选临时配置中执行，不让 combined 与正式告警/dispatcher 同时争用同一套数据；其副本/pool 配置仍必须满足总额。S06 通过验收容器直连一个指定 business 实例和 platform 的私网端口，避免业务 upstream 分流掩盖单进程上限；受控延迟只由验收依赖/夹具提供，不向产品加入调试接口，不换小槽位冒充正式 128/32 配置。非业务请求/探针及数据库临时客户端仍登记资源上限。

每 case 记录前置身份、操作、期望、实际结果、窗口/期限、raw 文件来源、资源归属、passed/failed/not_started 与失败分类。产品行为不符为 product_failure；启动工具、依赖夹具、身份漂移、schema、证据或清理错误为 infrastructure_failure。任意固定门禁失败、未执行或缺证据均不能声明阶段完成。基础设施失败不能推出产品能力结论；共享数据库故障也不能声称由服务分离隔离。

## 4. 预算与不可压缩实验时间

预计总活跃时间 **90 分钟**，跨所有候选/目录/诊断累计上限 **180 分钟**。

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 身份、工具、确定性 preflight | 15 | 30 |
| 缓存构建/Bundle、环境建立 | 20 | 40 |
| 一次 S01～S07 与有限诊断 | 30 | 60 |
| 证据核验/脱敏发布 | 15 | 25 |
| 日志/版本/提交与安全清理 | 10 | 25 |
| 合计 | 90 | 180 |

不可压缩观察为 1 × 60 秒平台停止窗口；准入为 2 × 最多 10 秒保持，恢复/异步各单次最多 30/60 秒，不转为长时测量。成本按 1 个正式项目 ×（最多 10 分钟就绪 + 固定案例最多 20 分钟 + 最多 5 分钟清理），另计短 preflight、构建、证据核验和最多 20 分钟诊断，均落在上表。未证明长期稳定、容量或观测开销，不用短运行外推。

第一次基础设施失败立即停止整组，最多两次各 10 分钟在最小边界诊断，包含在 180 分钟内。本批不修改工具/产品；若诊断发现修正需求，保存 failed/not_started 与修复建议，转为有明确预算的新合同后再执行。仅有环境配置误操作且不改变冻结合同，可先纠正、跑受影响 preflight，再在新目录重跑；新候选或重试不重置预算。

## 5. 证据接续、发布与阶段完成

原始结果留在 `.run/phase21-03-<id>/`，不得手改；私有目录按既有规则保护。passed/failed/not_started、累计成本、命令与环境身份在退出前落盘。02 工具不支持单 case/进程内 resume：清理后重新建立项目；相同身份的确定性检查可以保留，不拿旧正式请求或故障窗口拼接成另一候选的完整矩阵。

到 90/144 分钟报告已过案例、未开始 gate、累计成本与下一步。剩余预算不足以跑预计命令及清理、累计到 180 分钟、诊断两次耗尽或用户停止时，停止新工作，执行归属清理，保留真实 checkpoint，记录具体续作路径。不更新完成 VERSION，不建完成提交；额外执行先修订有限计划并由用户明确要求继续。

固定门禁通过后，先验证 private source，再用 02 白名单发布器生成本批 evidence，并对**实际选定 publication**再验证。日志登记候选 2.3.2 和完成 2.3.3 的区别、case 结果、证据路径、共享依赖限制和实际清理。没有真实 source→publication 映射/校验不能宣布收口。

最终完成检查：

```bash
rtk proxy python3 scripts/ci/validate_versions.py
rtk proxy python3 scripts/ci/validate_branch.py --branch develop/2.3.3 --base-ref upstream/main
rtk git diff --check
```

完成条件：冻结同一候选 S01～S07 全部 passed、formal execution complete、归属清理完整、选定脱敏证据严格验证；登记能力状态、本阶段合同完成标记和同名日志，导航仅按需修复链接，成功后 `VERSION=2.3.3` 及登记元数据同步并提交。本阶段只声明业务/管理运行与代表性流程/单侧停止通过；共享 MySQL 故障、状态层 HA、独立 dispatcher、多目标、容量与长期稳定仍未验证。
