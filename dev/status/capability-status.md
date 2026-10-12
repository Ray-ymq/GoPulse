# GoPulse 当前能力状态

> 基线：产品完成版本 `2.6.4`，2026-10-12。版本完成、验收执行完成和能力达标是三个不同概念。
> 2026-10-05 范围调整：Phase 20 仅保留已完成的 01～04，在 `2.2.4` 结束；原 05、06 已取消。

## 已实施阶段

- Phase 22 在 `2.4.5` 完成五批收口：本机 Go/Vite 开发与隔离测试入口、业务与可观测原生模块/真实链路、用户与管理浏览器验证、按共同祖先改动选择 CI/开发完成分支校验和文档映射，以及迁移 CLI 的 Go 原生 MySQL 验收与一次性 Python 工具退役。Phase-22-04 的实际门禁与限制见 [实施日志](../logs/Phase-22/Phase-22-04-CI与文档收口.md)；Phase-22-05 的迁移覆盖、退役边界和门禁见 [实施日志](../logs/Phase-22/Phase-22-05-原生验收与脚本精简.md)。Phase-22-04 真实 CI run `37781739950` 的治理、模块、集成、浏览器和 full-stack Compose job 全部通过，05 的实际 CI 记录在同名日志中。
- Phase 23-05 在 `2.5.5` 完成验收能力原生承接与执行器退役：构建缓存和日常容器栈由 `devtools` 承接，业务/可观测/插件/告警/角色/页面/生命周期验收由 `acceptance` Go 模块承接，插件制品由 Monitor Make 入口承接，发布 Bundle 合同由 `lifecycle` 保持。Compose 验收镜像直接以 Playwright 为入口，不再嵌入外部执行器。
- Phase 23-06 在 `2.5.6` 完成工程稳定性改造：根 Make 与 CI 共用模块选择，doctor/bootstrap 可靠判断本机工具和依赖，Go/前端新增缺陷门禁，失败诊断在清理后仍可定位，开发、integration、E2E、Compose、Bundle/lifecycle 均有本地和远端实际验证。统一 lint 的存量告警仍如实保留，新增告警为零；最终质量 workflow `38123004454` 的 44 个 job 全部成功。
- Phase-24-01 在 `2.6.1` 完成代际身份与搜索完成基础：[实施日志](../logs/Phase-24/Phase-24-01-代际身份与搜索完成基础.md)记录 migration 14、epoch/完成证据/有界停放与留存预算、v1 兼容消费者准入、v2 fixture、实际搜索 generation 与 `_meta` 代际核验、清理屏障及 lifecycle 恢复准备；生产仍保持 `legacy_v1`。
- Phase-24-02 在 `2.6.2` 完成发帖命令幂等与安全重试：[实施日志](../logs/Phase-24/Phase-24-02-发帖幂等与安全重试.md)记录 migration 15、v2 activation、同事务 command/post/Outbox/search 义务、稳定结果与用户端身份复用；真实 MySQL/RabbitMQ/Redis/Elasticsearch business integration 和 3/3 business E2E 通过。管理端阶段查证、平台适配和完整 V1 冻结矩阵仍属于后续批次。
- Phase-24-03 在 `2.6.3` 完成受限阶段查证：[实施日志](../logs/Phase-24/Phase-24-03-受限状态接口与证据关联.md)记录 business 私网查询、platform 受信适配、实时管理授权、当前搜索代际可见性、Logs 关联质量和严格字段传播；固定模块门禁、business/observe 真实链路、Compose 与 docs-check 均通过。管理端阶段页面和完整 V1 冻结矩阵仍属于后续批次。
- Phase-24-04 的管理端页面、typed Logs 关联、阶段浏览器 spec、V1 receipt/validator、pinned candidate preflight 工具和 acceptance fault fixture 已在 `develop/2.6.4` 实施；模块级测试、前端 typecheck/build、固定门禁、真实 observe native e2e、linux/amd64 `RUNTIME=1` 候选、V1 preflight V01 与 Verify Evidence 均已通过，实际候选和清理记录见[同名实施日志](../logs/Phase-24/Phase-24-04-管理端阶段页面与原生验收.md)。Phase-24-05 已在 `develop/2.6.5` 执行候选构建与既有 runtime gate，但 lifecycle failure matrix 低磁盘子项发生验收基础设施失败，PREFLIGHT 与完整 V01～V12 均未开始；本批不产生完整 V1 能力结论，实际停点见[同名实施日志](../logs/Phase-24/Phase-24-05-V1冻结验收与阶段收口.md)。
- Phase 23-05 的退役决定明确保留 `ci/` 治理 Python，只迁移路径、不重写规则；Phase 16–21 运行时与正式矩阵、Phase 18–20 容量/长期/证据执行器、历史闭包路径、交付 Python 库、Windows 转发器和零引用入口不再重跑，删除前已有的历史结论、方法、日志与原始回执继续有效。`component-probe.go`、`plugin-fault-router.go` 迁入 `acceptance/internal/fixtures`，迁移锁夹具迁入 `backend/testdata`，`runtime-http.go` 随其唯一运行时矩阵退役。
- [Phase 21 总实施方案](../imple/Phase-21/Phase-21-总实施方案.md)分配的三批 `2.3.1`～`2.3.3` 已完成。Phase-21-01 已在 `2.3.1` 完成：Backend 支持 `combined`、`business`、`platform` 三种角色，按角色装配依赖、路由和后台任务，并通过 R01～R05 固定门禁及最小真实三角色进程检查。Phase-21-02 已在 `2.3.2` 完成双服务 Compose、同源代理、运行/采集合同、Bundle/lifecycle 固定别名及 service-split 源码预检；Phase-21-03 已在 `2.3.3` 完成冻结 `2.3.2` 候选的正式定向验收与阶段收口。
- 共用账号、数据库和会话；Outbox dispatcher 留在业务服务。目标验收包含管理单侧停止时已有会话的业务可用，不声明隔离共享数据库故障、容量或长期稳定。

## 已验证

- 社交业务、搜索、通知、插件、Metrics、Logs、Events、内部告警与双 Frontend 端到端闭环。
- 迁移 CLI 通过所属 Go 包的真实 MySQL 集成验收：空库并发与重复 up、锁超时、dirty/ahead 拒绝、DDL 权限失败后的 dirty 保留、显式 v12 恢复、退出码/脱敏和 owner-checked 容器及匿名卷清理；该回执只覆盖迁移状态边界，不替代 Phase 17～21 的备份恢复、容量、长期运行或发布专项验收。
- Phase-24-01 固定门禁通过：backend、lifecycle、devtools 的 verify（含测试、race、build，新增 lint 告警为零）、business integration、Compose 检查和 docs-check 均通过；真实 MySQL/Elasticsearch/RabbitMQ 恢复准备用例验证事实摘要不变、新旧 epoch 行为及失败后 `restore-pending` 保留。
- Phase-24-02 固定行为已通过真实 business integration、business E2E，以及 backend/frontend/lifecycle/devtools verify、Compose 和 docs 门禁；前端 Vitest/typecheck、迁移 15、command 并发/冲突/删除结果与业务搜索收敛均在本批日志中记录。
- Phase-24-03 固定行为已通过 backend、monitor、marshaller、lifecycle、devtools verify（含 race/build，新增 lint 告警为零）、business/observe integration、Compose 检查和 docs-check；observe 真实链路覆盖 command、Outbox/broker、search consumer、当前索引可见性和 Logs 关联。Trace 查询仍按合同表达为未配置/采样质量，不声明 Trace 存储可用。
- Linux `amd64` Compose 与 Bundle 生命周期、同 Bundle 备份恢复和私有网络边界。
- Backend、Business Worker、Search Indexer、Router、Marshaller 双副本计算层。
- Outbox lease/owner、RabbitMQ ack/retry、Kafka generation ownership/manual commit 和终态闭合。
- 业务搜索 Elasticsearch 与可观测 Elasticsearch 独立服务、卷和故障域。
- 有界 HTTP 并发、连接池、prefetch、Kafka buffer、Marshaller in-flight/retry 与关停预算。
- Backend 业务/管理 API 的有限准入与探针隔离；槽位耗尽时固定返回 `503 backend_busy`，四个运行时探针仍保留自身依赖和停止语义。
- Backend 固定低基数 HTTP 延迟分布（bucket/count/sum）以及 in-flight、并发上限、拒绝计数的 Monitor → Envelope → Marshaller → VictoriaMetrics → 查询链路。
- Phase-18-05 冻结候选的扩缩容、局部故障、SIGTERM、重建、独立诊断和清理矩阵。
- Phase-19-02 的容量 profile、开环四阶梯/三重复 runner、独立资源采样、原值统计和严格 evidence verifier 已完成自测与 calibration；这只表示工具合同冻结。
- Phase-19-03 已完成正式入口、逐轮确定性配方、轮次 endpoint、进度/资源/恢复原始证据和严格 verifier 的工具修订；这只表示验收基础设施完成，尚无正式容量结论。
- Phase-19-04 已对冻结的 `2.1.3` candidate（revision `7251d32a20bc`）完成一次正式三重复认证；执行状态为 `complete`，能力状态为 `boundary_found`。固定同步请求门禁全部通过，但异步/观测恢复门禁未通过，正式结果已发布到 [`Phase-19-04 evidence`](../logs/Phase-19/Phase-19-04-evidence/summary.json)。

- Phase-20-01 在冻结的 `2.1.4` 产品候选上完成独立空项目的十二个阶梯单元，新 profile 结果为 `complete / target_met`：50/100/150/200 RPS 各三次均通过固定同步门禁与四个独立恢复门禁，最长恢复观察上界 44.97 秒。接受/终态台账、真实插件 Event、精确业务事实/观测水位、开销对照和安全清理均由原始证据重算；[脱敏证据](../logs/Phase-20/Phase-20-01-evidence/baseline.json) 已严格校验。完成版本为 `2.2.1`，本批没有产品优化。
- 新旧 profile 的隔离、恢复和采样语义不同；新结果不改写 Phase 19 历史，不构成产品优化收益或旧超时单一根因的证明。
- Phase-20-02 在 `2.2.2` 完成发帖→Outbox→RabbitMQ→Indexer→搜索可见的单链路 Trace/新鲜度与 C01～C07 真实案例，受控依赖延迟、Collector 故障、重试、异常上下文、重启和权限边界均有实际证据；范围见 [02 实施日志](../logs/Phase-20/Phase-20-02-业务链路关联与新鲜度.md)。
- Phase-20-03 在冻结 `2.2.2` B0 制品上完成四阶梯三重复：`complete / target_met / not_needed`。90,000 个测量请求的固定错误项为零，最高 P95/P99 为 46.67/148.58 ms，四维独立恢复最长 40.74 秒，12 个项目均完成归属清理。完成版本 `2.2.3` 只同步登记的版本元数据，产品源码/行为配置与 B0 一致；无 B1、无改善率。[原值与聚合](../validation/Phase-20/phase20-optimization.md) 已从严格校验的私有证据生成并核对。
- Phase-20-04 在 `2.2.4` 完成 R01～R08 生命周期验收：Logs/Events 按 UTC 日历边界保留 7 日，真实 Elasticsearch 清理验证了固定集群身份、strict mapping/归属标记、alias、删除前阻断、重试、权限失败、双副本幂等和在途写入竞态；迟到数据永久处理且不复活旧索引。VictoriaMetrics 使用原生 `30d` retentionPeriod，当前查询闭合；短时窗口未加速物理回收。Collector Trace 工件在固定归属路径内观察到轮转，文件/总量预算未越界；本结果不声明长期 Trace 存储。
- Phase-21-01 在 `2.3.1` 完成角色配置、接口路由互斥、业务/平台独立准入、共享账号与实时管理授权的装配迁移。R04 逐一启动并停止三种角色，验证业务 current-user、平台管理授权、错误角色 `404` 与 combined 并集；本结果不声明双服务部署或 S01～S07 阶段收口。
- Phase-21-02 在 `2.3.2` 完成：Compose 运行两个 business Backend 和一个 singleton platform-api；同源入口把管理基路径固定转发到平台服务；Backend 指标以三个实例身份进入采集合同；生命周期与制品合同保留 `platform-api → backend` 别名；D01～D05 及最终源码 preflight 的 S01～S07、归属清理均通过。该源码 preflight 不是 Phase-21-03 的冻结 Bundle 验收。
- Phase-21-03 在完成版本 `2.3.3` 收口：冻结候选 `2.3.2`、revision `ca0aa5efb6f0` 完成严格 Bundle/preflight、正式 S01～S07、清理和 publication verifier；[白名单证据](../logs/Phase-21/Phase-21-03-evidence/summary.json)记录全部案例通过及候选身份。该结果只覆盖代表性业务/管理运行、单侧停止和有限准入/采集核对，不声明共享状态高可用、容量或长期稳定性。

## 已发现边界

- Phase-18-01/02 未证明稳定 `150 RPS`；历史运行出现重复性失败、burst 错误和 Outbox 积压。
- Phase-18-04 的原冻结候选结果为 `boundary_found`；其历史证据不因后续修复而改写。
- 固定延迟桶支持 P50/P95/P99 的查询表达；Phase-19-04 正式 profile 已执行，但未达到全部容量目标，不能据此声明目标阶梯 `target_met`。
- Phase-19-04 首个观察到的边界为 `50 RPS` 阶梯的异步/观测恢复：三次重复的恢复时间均超过 `120s` 门禁；同窗可见 Kafka lag、Outbox pending 与 Kafka 高 CPU，但这些是相关窗口证据，不构成单一根因证明。
- 原 Phase-19-03 deterministic preflight 发现正式 runner 的配方物化、轮次 endpoint、宿主版本合同和真实恢复证据不闭合；正式容量入口未调用，没有容量结论。修订由新的 Phase-19-03 承担，正式认证已由 Phase-19-04 完成并收口为 `boundary_found`。
- Monitor 是插件生命周期唯一所有者；状态层仍可为单节点。

## 已取消的实施范围

- 原 Phase-20-05 已开工但正式预算验收为 `incomplete`，现已取消；原 Phase-20-06 未开始，现已取消。两份实施文件已移除，后续不再以它们完成为前置条件。[范围与历史记录](../imple/Phase-20/Phase-20-总实施方案.md)
- 原 05 冻结候选 `8a4d780` 的两次正式 B02 窗口出现 HTTP 500 与已写入业务事实/Outbox；具体失败阶段和根因尚未证明，两次短诊断未复现不能替代正式失败。取消不表示修复，该未完成候选的现象也不直接外推为 `2.2.4` 的已复现缺陷。
- Milestone 7 仅按四批实际交付范围收口；原资源预算、独立观测开销、最终候选完整矩阵和持续运行目标未交付。历史分支、日志及原始证据保留，不改写为完成。

## 已退役的执行器

- 2026-10-09：Phase 18–20 的正式矩阵执行器整体退役删除，共 44 个文件 / 11,422 行——16 个阶段执行器（`phase18_*` / `phase19_*` / `phase20_*`）、16 个对应自测、3 个顶层证据校验器（`verify-phase1{8,9}-evidence.py`、`verify-phase20-evidence.py`）、9 个 Shell 转发器（`verify-phase18/19/20-*.sh`）。依据：用户确认 Phase 16–20 正式矩阵不再重跑，退役不需要等价替代。源码与原始回执由 Git 提交保留；方法与结论记录保留在 [Phase-19](../validation/Phase-19/capacity-methodology.md)、[Phase-20](../validation/Phase-20/phase20-capacity-methodology.md) 验证文档及历史实施日志中。
- 合同影响：[运行契约](../contracts/runtime-contracts.md)、[Trace 与新鲜度契约](../contracts/phase20-trace-and-freshness.md)、[可观测保留契约](../contracts/observability-retention.md)、[迁移状态契约](../contracts/migration-state.md) 的条款继续有效，仅"验证入口"不再指向已退役执行器。
- 本批保留的治理例外只有 `ci/validate_versions.py`、`ci/validate_branch.py`、`ci/quality_scope.py`、`ci/sync_version_metadata.py` 及其自测和 `ci/AGENTS.md`；它们仍由治理 job 调用，Python 规则与失败语义保持不变。
- 本批删除的验收、矩阵、交付和 Windows 执行器不构成能力替代声明：当前能力边界以原生 Make/Go 入口和本文件的“尚未验证”条目为准；历史文件与证据只保留其既有上下文，不被改写。
- 本退役不改变任何已发布结论：Phase-18～20 的 `boundary_found` / `target_met` 等历史结果不因执行器删除而失效或改写。

## 尚未验证

- Phase-24-05 的完整 V1 冻结矩阵尚未交付；本批候选 runtime gate 在 lifecycle failure matrix 的低磁盘 direct 子项因 `0:0` 无法读取 `0700` Bundle 目录而返回 manifest code `10`，未进入容量断言，属于验收基础设施失败。PREFLIGHT、V01～V12、证据 publication 和 `2.6.5` 完成元数据均未执行；Phase-24-04 的管理端页面、真实 preflight V01 与证据核验仍按其同名日志成立，本批不声明完整 V1 能力达标。
- Phase-24-02 的 activation gate 只自动检查 MySQL 持久 Outbox 的 v1 `pending/leased` drain；RabbitMQ unacked/retry/dead 队列与 ES 当前 generation 仍须由 rollout/restore 步骤停排、排空或停放并核验，不由本批启动代码自动宣称完成。
- 恢复仅完成同 Bundle、已排空备份的代际准备与失败停点验证；跨版本、任意时点恢复和正式 Bundle 恢复矩阵尚未验证。
- 本批冻结环境/配方之外的吞吐、尾延迟、持续运行或单位资源容量声明。
- 原 05/06 规划的正式资源预算、观测/Trace/采样器开销、最终同一候选集成验收与两次独立 60 分钟持续运行；本阶段范围收口不补足这些证据。
- 生产端到端异步新鲜度 SLO、全站分布式 Trace；已完成的有界单链路传播不能外推为这些能力。
- Logs、Events 超过本批 7 日默认策略的长期容量、rollover、磁盘水位和成本合同；VictoriaMetrics 的实际 30 日物理回收时序；Trace 长期存储。
- Monitor 控制面 HA、状态层 HA、生产 TLS/SASL、跨地域、多活和 Kubernetes。

## 声明规则

- `implementation_complete`：计划内文件和行为已实现。
- `validation_complete`：冻结候选和要求的 evidence 完整且可复核。
- `target_met`：固定能力阈值达到。
- `boundary_found`：执行完整但阈值未达到或发现明确边界。
- 验收程序错误、候选漂移、证据缺失或不安全清理属于 `validation_incomplete`，不能产生能力结论。
