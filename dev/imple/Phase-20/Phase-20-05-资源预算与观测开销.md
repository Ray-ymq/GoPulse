# Phase-20-05：资源预算与观测开销

> 目标版本：2.2.5；开发分支：develop/2.2.5；当前状态：B07 已通过，S3 因 B02 业务 500 阻断，尚未通过验收。
> 2026-10-02 修订测量口径、分阶段执行和工具交付门禁；本次规划修订不代表实现或验收完成。
> 同日补充暂停检查点与累计成本规则；规划推送不代表恢复执行。
> 2026-10-03：最新执行入口为 2.8；2.5～2.7 保留为历史检查点，不再默认停在单次 B06。

## 1. 目标与范围

把既有进程内部并发预算与宿主/容器/磁盘预算结合，量化观测对业务的成本并冻结最终验收合同。
沿用 Linux amd64 Compose，状态层与 Monitor 保持现有单节点边界。

- 在独立机器合同中记录宿主规格、每服务 CPU/RSS/连接/队列预算、磁盘安全水位、
  Trace/日志采样、采集频率、预期数据增长及超限行为；固定目录与 runtime contract 一致。
- 将应由运行环境限制的 CPU/内存预算落实到受支持 Compose 配置，验证实际 inspect 值；
  不用一份未生效 YAML 宣称资源隔离。磁盘逻辑水位与实际配额区分。
- 不在本批无证据调整产品吞吐参数；资源布局改变后重新记录最终候选基线，不把加机器/加内存
  描述为 03 的算法收益。
- 观测正常/观测出口故障时比较业务终态和延迟；队列满、丢弃、重试与关停有界且可解释。
- 在相同业务负载下固定测量观测启用/关闭、Trace 采样差异和采样器本身的开销，记录开销口径、
  宿主共用 CPU/I/O 以及预算不足时的降级。诊断对照不作为正式容量认证。
- 依据每分钟实际增长估算磁盘保留成本，注明压缩、索引放大和原生回收的误差。
- 完成 06 所需编排、verifier、持续运行 profile、故障步骤和脱敏发布工具；先完成自测与预检。

初版预算数值、开销阈值及最终 profile 已在创建本批分支前登记。当前批次的合同修复在
update 完成并合入 main，再并入尚未完成的 develop/2.2.5 后实施；保留已创建分支和目标版本。
既有失败证据保持原候选身份。完成本批时构建配方及全部执行合同冻结；实际最终镜像/
Bundle/manifest 在 06 从包含本批完成提交的 main 构建后冻结。06 不再修改任何可执行或验收配置。

## 2. 预算记录与开工前冻结项

每条预算必须包含 budget_id、服务/进程/队列或磁盘目标、测量来源、单位、窗口起点/长度、
聚合统计、阈值、触发所需连续样本/持续时长、超限动作、恢复条件/时限及原始证据路径。
schema 拒绝空值、无限值、TBD、未登记目标与单位不符。所有限值绑定前序实测证据；
只记录 Compose limit 或“超限告警”而没有实际行为和结果，不能通过。

| 类别 | 必填统计及规则 |
| --- | --- |
| CPU | cores 或核秒/秒，明确区间平均/峰值和采样间隔；区分 quota、实际用量及 throttling；SUT、压测器、采样器、Collector 分别核算，另记宿主合计 |
| 内存 | RSS bytes 与容器 memory.current/limit 分列；峰值、OOM/非预期重启、正常结束和稳定窗口增长上限；不把 RSS 等同容器用量 |
| 连接/队列 | 对象名称、单位 count、配置容量、实际峰值、满载触发、拒绝/丢弃/重试/背压动作及恢复时限；业务队列与尽力而为 Trace 队列分别说明 |
| 磁盘 | 明确数据目录/归属卷，used/free bytes、安全水位、每分钟增长、预计保留成本和触发动作；逻辑水位与 OS/容器强制配额分列 |
| 观测与观察者开销 | 启用/关闭的具体组件集合、Trace 采样率、采样器配置、比较负载/时长/三重复、CPU/RSS/业务尾延迟差阈值及测量误差 |
| 故障/饱和 | case_id、精确目标、注入/恢复动作与时刻、最长持续时间、安全停止条件、期望动作/计数及最终恢复判据 |

稳定窗口内存增长同时检查峰值和窗口趋势。最终每次 60 分钟运行比较第 10～15 分钟与
第 55～60 分钟 RSS/容器用量的 median，并计算固定间隔样本的增长斜率；分别冻结绝对增长
和 bytes/minute 上限，不强制 GC、不事后挑低点。磁盘以真实卷读数计算增量与斜率，
保留索引/压缩/原生回收影响；预测成本不能代替安全水位门禁。

最终持续运行 profile 在开工前必须填入：稳定 RPS（来自前序已通过的稳定阶梯，06 不再
根据 U1 临时选择）、同一配方/业务比例、预热时长、采样间隔/缺样容忍上限、各资源预算引用，
以及观测存储出口的一次有界故障。两次运行都先完成初始化/预热，再计时 60 分钟；
第 15 分钟注入已登记的观测存储故障，持续 60 秒后恢复，期间业务继续。
实际故障目标/实现方式和预期计数在开工前确定；宿主/业务状态依赖不属于该注入范围。
恢复后仍按独立 120 秒业务/观测水位门禁验证；第 60 分钟停止调度、排空和最终恢复另记。
故障未实际生效、持续时长不符或缺少回执均为 incomplete。

持续负载中至少在预热后和故障恢复后各执行一次检查点：使用不会被背景编辑/删除的独立
发帖探针，按接受事实/事件/真实搜索证明业务新鲜度，三通道仍按 01 产生真实标记并独立
计时；不用全局 lag=0 或变化中的全表快照证明闭合。探针与背景压测统计分开，开销计入预算。
这些检查点只证明被探测链路的恢复；全量已接受背景请求的台账/最终投影在停止并排空后
按 01 核对，不能把一个探针通过宣称为所有后台请求均已完成。

开工冻结表还必须登记：机器规格/可用空间、每个预算实际数值及依据、所有 B 案例的准确
动作与证据位置、最终 U1～U4 清单、依赖/第三方镜像 digest、构建配方、发布脱敏清单。
任一项未填不得创建 develop/2.2.5；不能在验收过程中补阈值使结果通过。

### 2.1 已冻结开工登记

以下登记在创建 `develop/2.2.5` 前完成，初版冻结编号为
`phase20-05-budget-contract-20261001`。登记基线为 `origin/main`
`f44ab45d857aa5981a6a278dd0c03d576b7ffa11`；本节是开工合同，不是 05 的执行结果。

#### 宿主、前序事实与测量口径

| 项目 | 冻结值与来源 |
| --- | --- |
| 宿主 | Linux `amd64`、WSL2 kernel `6.6.87.2-microsoft-standard-WSL2`、8 CPU、`MemTotal=13281496 kB`、swap `16777216 kB`、`/var/lib/docker` 可用 `105056022528` bytes；Docker Server `29.7.2`、Compose `5.5.0`。CPU/内存/swap 下限分别为 8 CPU、12 GiB、8 GiB；初版磁盘下限为 100 GB，经已授权的 r3 修订后为 50 GB（`50000000000` bytes）。执行前再次写入 `host.json`，不以本表替代实际 preflight。 |
| 业务配方 | 沿用 Phase 20 配方 seed `18002005`、recipe digest `sha256:0e61a5473f72d735ab322261e312290249f39997b649837fea32bfe2c947cf14`、`1024` virtual users、50/100/150/200 RPS、15 秒预热、60 秒测量、三重复；原始 profile 为 `loadtest/phase20-capacity-profile.json`。 |
| 前序资源依据 | 03 的严格复验 `dev/validation/Phase-20/phase20-optimization.md` / 私有 contract digest `sha256:190405c1c6c2b72656d6699afcc5772b6cd2e0284be4431a44682bc92d8a1635`：容器 CPU 峰值 `806.6%`、RSS 峰值 `4462.235990524292 MiB`、Kafka lag 峰值 `7268`、Rabbit ready 峰值 `0`、unacked 峰值 `2`；观测 ES 单元增长最大 `13677487` bytes。 |
| 前序开销依据 | 01 的同宿主采样器对照为启用/停用各 250 次、50 RPS、5 秒窗口，P95 `23.14/19.85 ms`、验收进程 CPU `3.656/3.259 s`；该值仅为历史诊断事实，不能证明 200 RPS/60 秒的 P99 差或独立采样器 CPU 峰值符合本批门禁。正式执行前须通过 2.4 的测量自测与真实短预检。 |
| Trace/保留依据 | 04 已冻结 Collector `0.138.0` digest `sha256:d535a52679b1df0a95b1b6fc4322cb74ecddd61f0b550cb43444d2b22cedec0c`、单文件 `16 MiB`、总量 `64 MiB`、最多 `3` 个备份；VM `30d`、Logs/Events `7d` 的生命周期合同保持不变。 |

#### 预算与超限合同

所有 CPU/RSS/队列样本间隔固定为 `5s`；持续运行的窗口趋势使用固定样本，比较第
10～15 分钟与第 55～60 分钟的 median。下表保留既有冻结阈值；03 的总量峰值只支撑
对应资源总量，不能代替单服务 quota 或观测成本差值的依据。每服务 quota 须核对前序该
服务峰值，开销门禁按 2.4 的一致测量方法验证。B04/B05 的注入阈值不能覆盖正常窗口阈值。

| budget_id | 目标与冻结限值 | 超限动作与恢复判据 |
| --- | --- | --- |
| `cpu.sut_peak_cores` | 所有归属 SUT 容器 CPU 峰值合计 `<=8.75 cores`；另列 load generator `<=1.50 cores`、sampler `<=0.75 cores`、Collector `<=0.50 cores`、宿主忙核 `<=8.00 cores`；记录 quota、实际用量和 throttling。 | 连续 3 个样本超限为 `budget_exceeded`，停止当前正式单元并保留原始样本；无 throttling、停止调度后 120 秒内水位恢复且下一独立单元从空项目开始。 |
| `memory.sut_rss_peak` | SUT 容器 RSS 合计峰值 `<=6 GiB`；容器 `memory.current` 合计 `<=8 GiB`，分别记录每服务峰值；两次持续运行稳定窗口绝对增长 `<=512 MiB`、斜率 `<=16 MiB/min`。 | OOM、非预期重启或连续 3 个样本超限立即停止并标为失败；正常关停且无 OOM、窗口趋势均在限值内才恢复。 |
| `queue.business` | Outbox pending 正常峰值 `<=100`；Rabbit ready `<=50`、unacked `<=16`；Kafka 观测 group lag 合计峰值 `<=9000`。 | 记录拒绝、重试、丢弃或背压的计数；接受事实不能丢失，停止请求后独立业务/观测水位均在 120 秒内闭合。 |
| `queue.trace` | Trace SDK queue capacity 固定 `2048`，Collector file exporter 单轮观察的队列/失败计数均记录；正常窗口不允许业务错误，允许尽力而为 span 在出口故障时有界丢弃。 | Collector 出口失败只触发 bounded retry/drop 和故障 receipt，不得阻塞或丢失已接受业务事件；恢复后 Collector 与业务探针在 120 秒内闭合。 |
| `disk.host` | 每个归属项目记录真实卷 used/free；宿主 free bytes `>=50000000000` 为通过，`50000000000 <= free <= 55000000000` 为 warning，`free < 50000000000` 为 safety 失败；不得用逻辑水位代替 OS/容器配额。 | warning 停止增长型 fixture 并记录；safety 立即停止本单元并安全清理，禁止填满宿主或执行 global prune。 |
| `disk.observability_growth` | 观测 ES 真实卷增长 `<=32 MiB/min`；Trace 目录单文件 `<=16 MiB`、总量 `<=64 MiB`、文件总数 `<=4`；按真实 docs/store/卷读数报告压缩、索引放大和回收误差。 | 触发保留/轮转合同，超限保存原始读数并失败；只删除归属目录，其他项目和业务索引不进入清理请求。 |
| `overhead.observability` | O0/O1/O2/O3 各三重复：正常观测 `O3-O0` 的业务 P99 增量 `<=150 ms` 且比例 `<=25%`；SUT CPU 峰值增量 `<=1.50 cores`、RSS 峰值增量 `<=512 MiB`；Trace `O2-O1` 的业务 P99 增量 `<=200 ms`、CPU 峰值增量 `<=1.00 core`、RSS 峰值增量 `<=256 MiB`。 | 三重复按各组合 median 求差；差值保留正负，绝对阈值指 ms/cores/bytes 门限，不取差值绝对值。Trace 不额外增加未登记比例门禁。缺原始三重复或负载不一致为 incomplete，已执行的有效数据超阈值为 fail。 |
| `overhead.sampler` | 采样器 `O1-O3` 的业务 P99 增量 `<=50 ms`；独立采样器进程及其采集子进程的固定 5 秒 CPU 峰值 `<=0.75 core`；采样缺样 `<=1%`。四组合均使用相同独立低开销计数源。 | 压测器与基准计数器 CPU 分列，禁止把整体验收进程 CPU/运行时长当作采样器峰值。缺测量身份或原始计数为 incomplete，有效测量超阈值为 fail；业务结果另按独立业务门禁判定。 |

Compose 的实际 limit 也在本批实现并由 B01 inspect 核对；初版冻结服务额度为：MySQL
`2.0 CPU/1536 MiB`、Redis `0.5/256 MiB`、RabbitMQ `1.0/512 MiB`、业务与观测
Elasticsearch 各 `2.0/1536 MiB`、Kafka `1.5/768 MiB`、VictoriaMetrics
`0.75/512 MiB`、Backend 每副本 `2.0/512 MiB`、Business Worker 与 Search
Indexer 每副本 `0.75/384 MiB`、Router 每副本 `0.5/256 MiB`、Marshaller 每副本
`0.75/384 MiB`、Monitor `0.75/384 MiB`、Redis Exporter `0.25/128 MiB`、
Frontend/Admin Frontend 各 `0.25/128 MiB`、验收 Collector `0.25/128 MiB`。
一次性 migrate/search-init/admin-role/acceptance 使用同一组构建限制但不计入并发
稳定窗口；B01 必须证明渲染后的 `NanoCpus` 与 `Memory` 均生效。

#### 2.2 授权后的冻结预算修订

首次 B02 实际执行证明初版 quota 低于前序 03 的单服务峰值：Backend 两副本分别达到
约 `4.15/3.12 cores`，Kafka 约 `2.03 cores`，RabbitMQ 约 `1.53 cores`，Monitor
约 `1.10 cores`；初版限制导致真实 cgroup throttling，并在 200 RPS 窗口产生 15 个
HTTP 500。用户已明确授权修订冻结合同；初版失败证据保留在私有目录，不作为通过结果。

新的冻结编号为 `phase20-05-budget-contract-20261001-r2`，仅调整运行时 quota，预算
统计阈值和 B/U 操作不变。修订后的服务额度为：MySQL `2.5 CPU/2048 MiB`、Redis
`0.5/256 MiB`、RabbitMQ `2.0/768 MiB`、业务与观测 Elasticsearch 各
`2.0/2048 MiB`、Kafka `2.5/1024 MiB`、VictoriaMetrics `1.0/768 MiB`、Backend
每副本 `4.5/768 MiB`、Business Worker 与 Search Indexer 每副本 `1.0/512 MiB`、
Router 每副本 `0.75/256 MiB`、Marshaller 每副本 `1.0/384 MiB`、Monitor
`1.5/512 MiB`、Redis Exporter `0.5/128 MiB`、Frontend/Admin Frontend 各
`0.5/128 MiB`、验收 Collector `0.25/128 MiB`。修订后的合同合入 `main` 后，必须
从新的候选 revision 和新证据目录重新执行 B01～B07；不得复用初版候选或失败结果。

#### 2.3 已授权的宿主磁盘水位修订

现有 develop/2.2.5 的规划记录已登记用户于 2026-10-01 授权把初版 100 GB profile
下限与 90 GiB 运行时安全水位调整为 50 GB，合同编号为
`phase20-05-budget-contract-20261001-r3`。50 GB 精确为 `50000000000` bytes，
绑定 platform、disk budget、容量/持续运行 profile 及 B04/B05 安全停止条件。
本次将这项既有规划同步到 update；不新增磁盘门限放宽。r2 的失败记录保持原身份，
不得改写为 r3 结果。历史资源清理仍仅限已授权、已停止且无运行引用的精确归属资源。

#### 2.4 测量与执行合同修复

本次修订编号为 `phase20-05-budget-contract-20261002-r4`，保留 r2 的服务 quota、
r3 的磁盘门限及其余预算数值，修正比较配对、测量身份、执行次序和工具交付要求。
该编号须在规划合入 main 后由本批实现同步到预算/profile/schema；仅修改本文不使工具
或候选自动成为 r4。所有相关旧候选证据失效，原始文件保留，不能补写为通过。

**测量合同：**

- B02/B03 使用相同既有 Go loadtest 二进制、配方、业务比例、1024 virtual users、
  HTTP 连接策略与接受台账。业务 P99 使用测量窗口内从调度到终态的延迟；另列实际发送
  到完成的延迟及调度滞后。仅对 `/users/me` 发请求的 Python 对照不能替代该业务负载。
- 四组合的基准计数器、采样间隔、CPU/RSS 数据源及测量窗口相同；O0/O3 关闭增强采样器
  后仍保存完整基准资源序列。预热样本单独记录，开销判定只使用 60 秒测量窗口。
- 采样器在独立进程运行；CPU 使用进程及采集子进程的累计核秒增量除以实际采样间隔，
  再取窗口峰值，同时保留累计量和区间平均。压测器、基准计数器、Collector 和宿主另列。
  RSS 读取实际进程 RSS，容器 memory.current 使用 cgroup 原始读数，不互相改名代替。
- Kafka 客户端绑定本单元的 broker/项目；重试受单轮 5 秒采样预算约束。按冻结时间表计算
  应有样本数、实际数、错窗数和缺样率，不仅以成功返回的样本为分母。采样失败立即通知
  编排器停止当前单元，不等待所有 HTTP 请求或其余组合完成后才发现线程失败。
- 自测必须证明三种配对独立：只改变 O0 应影响正常观测比较；只改变 O2 应影响 Trace
  比较；只改变采样器原始 CPU 应影响采样器门禁。额外验证压测 CPU 不计入采样器、缺样/
  预热/错窗被拒绝；全部原值相等的成功 fixture 不足以证明比较公式正确。

**执行次序：** 编号只标识案例，不代表先运行 B03 再做 B07。

| 阶段 | 必须完成的检查 | 停点 |
| --- | --- | --- |
| S0 确定性检查 | schema、所有 CLI 参数/模式、预算/profile/runtime 一致性、观测 Python 依赖及版本、源码 revision、磁盘和端口、测量公式自测；构建前核对每服务历史峰值与 quota，构建后核对实际 manifest/镜像身份 | 任一失败先修最小层，不启动真实矩阵 |
| S1 局部真实冒烟 | O0/O3/O1/O2 各一次独立空项目、200 RPS、5 秒预热+10 秒测量；验证负载业务比例、计数器、首批资源样本、Kafka 身份、终态和清理 | formal=false；不判正式开销、不计入三重复；工具失败立即停止 |
| S2 B07 当前候选预检 | 下表冻结的真实 U1～U4 短预检及外部 evidence 校验 | 未 complete 不得进入 S3；单元测试结果不能代替真实依赖 receipt |
| S3 固定预算验收 | B01、B02、B04、B05、B06、B03；B03 每个重复按 O0→O3→O1→O2 执行，共十二个独立空项目 | 每单元完成即写入不可变原始记录及回执，工具错误、安全失败或业务事实丢失立即停止 |
| S4 发布与完成检查 | 重算 B01～B07、来源 digest、实际脱敏发布工件、版本/分支与归属清理 | 全部门禁通过才写完成日志、同步 2.2.5 并提交 |

S0 之后的候选镜像构建阶段总上限为 60 分钟；S1～S4 的实际执行总上限为 180 分钟，
其中 B03 总上限为 90 分钟。上限是执行保护，不是产品能力门限或预计完成保证；依据
2026-10-01 r18 的 B03 约 57 分钟设置诊断停点，达到上限时保存阶段耗时、证据和清理回执，
标为 infrastructure_timeout/incomplete，停止后续单元。逐项记录构建、启动、配方、收敛、
预热、测量、恢复、校验和清理耗时，不以延长外层等待掩盖具体阶段失败。
达到执行保护上限先停止新请求，再按既有 grace 和归属清理合同有界收尾，收尾耗时单列；
不能为满足外层 deadline 跳过清理或缩短产品恢复门禁。S3 中逐单元可判定的采样失败、
业务事实丢失和安全超限立即停止；三重复 median 的开销结论在规定样本完整后计算。

**局部诊断与重验：**

- 预算入口支持 `--case B01|B02|B03|B04|B05|B06`；B03 可进一步用
  `--combination O0|O1|O2|O3 --repeat 1|2|3` 定位。局部模式固定 formal=false，
  仍读取冻结参数，不接受阈值覆盖，不输出完整预算验收通过。S1 用 `--smoke` 固定短窗。
- 入口保留案例和组合单元级检查点；只有候选 revision、manifest、工具/config/依赖 digest、
  宿主条件均一致且旧 receipt 未失败时，才可 `--resume <同候选未完成目录>` 执行尚未开始的
  单元。失败单元不在原目录改写或筛选重跑；故障诊断写入新目录。
- 首次基础设施失败立即停止全套入口，先执行最小失败案例并定位原因；同一未解决原因
  最多两次有界诊断，不能在两次之间重启全套。有效修复及
  定向回归通过后再生成新候选。比较公式、资源数据源或窗口修复影响所有 B03 组合，不只
  重跑最后失败组合。新 revision 不复用旧候选的受影响产品或验收证据。
- 每次修复登记改动文件、影响的 case_id、必要重验及仍有效的确定性检查。相关条件未变的
  静态检查按 AGENTS.md 保留；旧产品证据只作为诊断来源，不重新绑定给新候选。
- 正确测量仍超既有预算时输出 budget_exceeded/fail 并停止验收。阈值或 quota 的进一步
  调整必须先有同负载、同统计口径的事实依据，在 update 修订并合入 main；不得把历史 P95
  或整体验收进程平均 CPU 解释成当前 P99/独立采样器峰值的证明，不反复重跑寻找好结果。

#### 2.5 暂停检查点、接续与累计成本

2026-10-02 用户明确要求先停止另一执行窗口，再补充规划。执行窗口已停止继续构建、
诊断与验收并完成收尾；无残留验收进程或 Compose 容器。当前分支为 develop/2.2.5，
已提交 revision 为 `22fe5931957c4cb9fda79c2056d2aa2be3459306`，根 VERSION 仍为 2.2.4。
规划修订不自动恢复任务，也不要求因引入耗时规则重头运行 S0～S3。

| 检查点 | 已核对事实及接续边界 |
| --- | --- |
| 原始证据 | 私有工作根 `/var/tmp/gopulse-phase20-05-22fe593`，保留原 manifest、smoke、preflight 和 diagnostic 目录；不修改失败回执 |
| S0/S1 | 执行窗口报告通过；S1 四组合原始报告均无业务错误。接续前核对实际回执、候选与工具/config 身份，修复确实影响到的检查仍需重新验证 |
| S2/B07 | 未通过；正式预检 `preflight/closure.json` 为 incomplete，停止原因为 business mix did not recompute。U1 两个短单元报告无业务错误，不能据此宣称整个 B07 通过 |
| S2 U2 原始失败 | 200 RPS / 300 秒正式短预检报告有 31 个 rejected_503 和 1,153 个 unexpected_errors；零业务错误门禁未满足，不能将该失败仅改称调度滞后误报 |
| 后续诊断 | diagnostic-no-fault 与 diagnostic-fault-debug 的 60,000 请求测量窗口均为零业务错误；它们是诊断事实，不覆盖或替换原 U2 失败回执。控制动作、负载调度与运行差异的因果仍需核对 |
| 未提交工作 | scripts/ci/phase20_closure.py、phase20_evidence.py、test_phase20_evidence.py 三个文件已被执行窗口修改但未提交；保留原工作树，由后续实施审查，不纳入本次规划提交 |
| S3/S4 | 正式预算验收和完成收口尚未开始；本批未完成，不更新完成版本 |

恢复前先审查上述三个文件与对应原始证据，明确正常容量与 sustained 故障窗口分别使用
哪个已冻结 profile、哪些门禁，以及修复影响的 case_id。U2 的诊断通过不豁免零业务错误、
请求/台账完整性、吞吐/延迟、资源/恢复和安全清理；正常 B02/B03 的 100ms 调度门禁保持。
存在实际合同歧义时，先在 update 登记准确口径并合入 main；不能在开发分支静默放宽。
若修复使工具或候选身份变化，按实际影响重新验证，不把旧证据换绑给新 revision。

- 恢复后完整通过：直接进入剩余 S4 校验与原完成条件，不追加容量/开销重复。
- 同候选中断：满足原身份/环境条件且工具确实支持相应粒度时，从有效检查点执行未开始项。
- 当前失败接续：退出全套入口，保存失败边界；最多两次、每次最多 10 分钟定向诊断。
  原因未证明或未修正时，不重启整套 B07/B03。实际修复后逐项记录必要重验范围；
  比较公式或数据源变更确实影响全部组合时，仍须核验受影响集合，不能借时间预算免验。
- 接续目标为 120 分钟、累计上限为 180 分钟。参考阶段预算如下；恢复前按仍有效的
  证据和实际工具能力核对，预计无法容纳时先报告剩余阻断项与最小方案，不自动再运行三小时。
- 该预算从用户明确恢复后的首次接续操作计时，本次暂停与过去耗时单列；后续换目录、修订候选或重启 agent 都不
  重置预算。到达上限保存实际进度及接续清单，尚未通过时不更新 VERSION 或声明完成。

| 剩余阶段 | 预计分钟 | 上限分钟 | 开始/停止条件 |
| --- | --- | --- | --- |
| 原因审查与直接检查 | 20 | 30 | 先核对原始失败与未提交修复；同原因两次诊断仍无结论就停止 |
| 必要构建与启动 | 20 | 30 | 明确受影响目标和候选身份；复用有效构建缓存，不能默认重建全部制品 |
| 必要预检/预算验收 | 60 | 90 | 按失效集合及实际恢复粒度执行；第一次工具失败退出全套，旧失败回执不覆盖 |
| 证据收口与安全清理 | 20 | 30 | 仅固定门禁通过时完成版本/日志；失败或超时保存未完成事实并收尾 |
| 合计 | 120 | 180 | 累计包括失败尝试；90/144 分钟报告进度，预计命令及收尾超剩余预算时不启动 |

2.4 的单次构建/执行保护继续有效，不能代替上述跨尝试累计预算。后续落地还须检查这些
限时字段是否被实际执行器读取，并将阶段/子进程的等待限制为剩余预算；只有配置声明
不能证明保护已生效。此次规则补充不要求把已有有效证据重头生成；是否重验由实际修改
和既有候选身份规则决定。等待用户明确恢复后再实施，不因规划推送自动运行。
完整流程见 [实施耗时预算与验收接续](../../rules/implementation-execution-budget.md)。

#### 2.6 U2 阻断修复与本次推送范围

2026-10-02 用户在 r6 失败后明确授权“修复吧，然后推送”。本次交付限定为已证明的
事务失败边界、直接回归和修复提交；不自动恢复完整 B07/B01～B06，也不宣布 05 完成。
继续使用 develop/2.2.5 和目标 2.2.5，根 VERSION 保持 2.2.4。

最新候选为 `1384c8193d4026a8830780225c1868e539e69132`，证据根为
`/var/tmp/gopulse-phase20-05-candidate-r6-EY3t06`。S0、候选构建、S1 和 U1 已报告通过，
S2 U2 的 60,000 次测量请求有 18 次 HTTP 500；原始 ledger 实际为 4 次评论、14 次
bookmark，与执行窗口的 3/15 汇总不同。bookmark 是私有关系事实，产品合同不产生
Outbox 事件，不能以无事件认定丢失。评论 500 与持久事实/事件的关系须逐请求核对。
U3/U4 和正式预算尚未开始；所有旧证据保留原候选身份，不改写失败事实。

已发现普通 MySQL 连接使用固定 1 秒读写超时，失败请求的实际耗时集中在约 1 秒。
先用受控 MySQL 协议故障复现提交应答丢失与提交前连接失败；该现象不证明 r6 每条
错误的具体 SQL 原因。禁止以延长连接超时、降低零业务错误门禁或无限重试作为修复。

允许新增的封闭文件集合如下，除此以外仍按第 3 节办理：

| 文件 | 本次允许行为 |
| --- | --- |
| backend/internal/platform/mysql_transaction.go、mysql_transaction_test.go | 最多一次重试、共享有限上下文、取消传播及暂态错误识别；不改变驱动超时或连接池预算 |
| backend/internal/comment/repository.go | 提交前暂态失败在原事务收尾后有界重试；提交结果未知时只读核对确切 comment ID/作者/正文及应有 Outbox event ID，未证明成功不得返回成功或重放 INSERT |
| backend/internal/bookmark/repository.go | 有界重试幂等关系事务，保持父对象锁、重复收藏/取消语义和零 Outbox 副作用；永久错误不重试 |
| backend/internal/integrationtest/mysql_proxy.go | 仅 integration 构建的归属 TCP 故障代理，复现应答丢失，关闭所有自有连接；不改产品协议或外部环境 |
| backend/internal/http/transaction_recovery_integration_test.go | 真实 MySQL 与 HTTP 回归：评论提交应答丢失仍只有一条事实/事件；未提交不能伪报成功；bookmark 提交前/提交时故障后终态和幂等正确，无事件副作用 |

固定回归要求与已知命令：

- 在 backend 执行 `go test ./internal/platform ./internal/comment ./internal/bookmark ./internal/http`。
- 在白名单 `gopulse_integration` 独占 MySQL 中执行
  `go test -tags integration ./internal/http -run '^TestIntegrationTransactionRecovery$' -count=1 -timeout=90s`。
  先在修复前记录失败复现，再在修复后核对 HTTP、持久事实和事件；不得用 mock 退出码
  代替真实事务。setup/migration/受控清理计入成本，不启动完整验收栈。
- 永久失败/无法证明提交、取消及重试耗尽不得被改写为成功；事务最多两次尝试，评论
  不重放未知结果的 COMMIT，所有等待共享有限上下文。普通 MySQL 1 秒读写限制保持。
- `python3 scripts/ci/validate_versions.py` 与 `git diff --check`；批次完成前不强行运行
  要求 VERSION=2.2.5 的最终完成检查。修复提交及日志明确为未完成批次的中间进度。

本次额外执行预算目标 60 分钟、上限 90 分钟，计时从此次授权后的首次操作开始：
定位/规划/故障复现 15/20 分钟，修复 20/25 分钟，直接回归与独占依赖启动 15/25 分钟，
记录/推送/清理 10/20 分钟（前数为预计、后数为上限）。45/72 分钟报告进度；同原因
诊断最多两次、每次 10 分钟。该增量因用户明确授权登记，不重置 2.5 的已有累计成本。
已有恢复任务缺少精确跨命令活跃时间台账，必须保留并如实登记该缺口，不能把文件 mtime
或窗口跨度伪写成实测分钟。达到增量上限或诊断停点时保留修复进度、受控清理并停止。

产品变化要求新候选。未受影响的确定性检查和构建缓存按依赖继续有效；包含这两个
业务路径的 S1、U1/U2 及相关预算证据必须重新评估。当前 closure CLI 无 --resume，
不能拼接旧候选回执宣称新候选通过。完整预检/预算的执行粒度、剩余成本和失效清单在
下一次明确恢复前登记；本次推送不自动触发该矩阵。零业务错误、固定测量窗口及最终
同候选完整证据要求保持。

#### 2.7 B06 停止、恢复编排修复与历史接续入口

2026-10-02 用户再次明确授权“修改，然后推送”。当前开发分支/目标仍为
develop/2.2.5 / 2.2.5，根 VERSION=2.2.4；本次交付是 B06 工具修复、定向验证和推送，
不自动恢复完整矩阵。该次接续从本节核对检查点，不从历史 2.5/2.6 默认重跑。
该修复完成不等于 B06 产品验收或整个 05 完成；当前最新入口已更新为 2.8。

最新已提交候选 r10 为 `2e83d08670c8697b7eaa45f7c5fb2625341d4f42`，私有证据在
`/var/tmp/gopulse-phase20-05-candidate-r10/b06-diagnostic`。执行窗口报告事务回归、
S1/U1/U2、C01 重验及 27 项定向检查通过；这些报告须按实际候选/工具身份逐项核对，
不能统称 r10 的完整 B07 已通过。r10 的 B06 已制造积压，恢复命令失败；本次核对
cleanup inventory 前后一致、owned=true、global_prune=false。S3/S4 未执行。

已确认旧 B06 在造积压前 stop worker，之后又对已停止的 worker 发 SIGTERM；对
Collector 发信号后没有等待退出，立即执行 up --wait。restart: no 仅禁止自动重启，
不能据此更改 Collector 配置或认定产品恢复失败。修复仅允许
scripts/ci/phase20_budget.py、scripts/ci/test_phase20_budget.py 和同名实施日志；
不修改产品代码、Compose restart 策略、冻结负载、quota 或恢复门限。

固定 B06 流程与通过条件：

1. 保存并核对三个目标的唯一容器 ID、项目/服务标签及运行状态，缺失或错归属立即停止。
2. 以 SIGSTOP 临时暂停两个仍存活的 worker 进程消费，制造四条 201 已接受写入；
   60 秒内取得真实 Outbox/Rabbit 非空积压。该辅助暂停不算已退出或关停通过，不改变
   业务/队列配置；SIGTERM 前三个原容器必须仍 Running，记录该辅助控制及积压证据。
3. 向原 ID 发送 SIGTERM，随后 SIGCONT 解除 worker 的临时暂停，让存活进程处理
   待决停止信号。所有动作与轮询共用发送 SIGTERM 后的 30 秒截止，不能仅凭信号命令
   成功或一次 inspect 宣布关停；超时/OOM/错归属阻断恢复。
4. 在任何恢复动作前保存 shutdown.json，含原 ID、发送/退出时刻、停止后的状态、
   退出码及积压事实。实际停止才算注入生效；未完全退出不得启动后续恢复。
5. 显式 compose start Collector，确认原 ID Running；再 start 两个 worker，并确认
   原 ID Running/有 healthcheck 时 Healthy。启动及等待共用 30 秒上限，保持 restart: no，
   不用 up --wait 重建或替换目标。恢复启动失败立即停止，不进入水位检查。
6. 沿用独立 120 秒业务/Outbox/Rabbit/Kafka 恢复判据和原有事实安全/归属清理门禁。
   失败也须保留 shutdown.json；finally 先解除仍有效目标的辅助暂停，再做有界归属清理。

本次修复固定检查为
`python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure scripts.ci.test_phase20_evidence`、
`python3 scripts/ci/validate_versions.py` 和 `git diff --check`。定向测试必须覆盖
信号时目标仍存活且有积压、异步退出后才恢复、退出超时/错归属阻止后续、启动失败不
进入恢复，以及失败收尾。另以独占小型 Compose 项目和缓存的固定 Collector/fixture
镜像真实验证 SIGSTOP/SIGTERM/SIGCONT、restart: no 的手动启动和原 ID 保持；该
协议验证明确 formal=false，不产生 B06 产品通过回执，不替代真实业务关停验收。

本次追加成本预计 30/上限 45 分钟：定位/规划 5/8、修复 12/15、定向测试与真实协议
验证 8/12、记录/推送/清理 5/10。22.5/36 分钟报告进度，同原因诊断最多两次、每次
最多 10 分钟。与历史接续及 2.6 的已发生成本分别登记，不清零累计成本；到上限保存
实际进度并停止，不能因换候选或目录自动再开一个窗口。

再次明确执行 05 时，先核对修复及有效直接检查，从新提交建立受影响候选身份，再用
已支持的 `scripts/verify-phase20-budget.sh --case B06 --manifest <新候选manifest>
--work <新私有诊断目录>` 单独验证真实 B06（formal=false）。记录启动/物化/恢复/清理
成本，单元数为一次 B06，不以 full B07/B03 探查该工具错误。工具变化影响 B06、U3/B07
及共享模块指纹绑定的回执；保留仍有效的确定性检查/构建缓存和所有历史原始证据。
当前 closure 无 --resume，不能换绑 r10 回执或承诺 B06 后直接进入 S3；只有当前候选
完整 B07 通过才解锁 S3。恢复完整矩阵前据实际成本登记有限剩余预算；超出剩余预算
时停止并给出具体接续清单。原十二个 B03 单元、恢复窗口和最终完成条件不减少。

#### 2.8 B02 写入阻断修复、候选冻结与连续收口

2026-10-03 用户授权修改推进方案。本次规划修改不运行产品验收。规划在 update 合入
main 后，同步到未完成的 develop/2.2.5；后续明确“执行本文件”以本节为入口，在一个
已登记预算内连续推进修复、受影响检查、B07、S3、S4。成功检查点不另设用户确认，
不再把“一次 B06 完成”解释为整个执行请求的终点。版本/分支分配及所有正式门禁不变。

**当前事实与因果边界：**

| 项目 | 最新检查点与接续要求 |
| --- | --- |
| 开发提交/完成版本 | 已推送 `5c628f3`，工作树干净；根 VERSION=2.2.4，批次未完成 |
| 被测候选 | `8a4d7805492392ae7632b11b3a615ea50d018ca8`；私有根 `/var/tmp/gopulse-phase20-05-candidate-8a4d780`；manifest SHA-256=`d2b0e1a87486365b194771a4b52f2af95c324bb9a127c0b0c798ef504784b76c` |
| 已通过 | 该候选 B07 U1～U4、外部 evidence 和两次 S3 的 B01；此前真实 B06 定向通过，formal=false。各结果保留原候选身份，不合称整个 S3 已通过 |
| 正式失败 | `budget-s3`：4 次 PATCH 500；`budget-s3-r2`：7 次 DELETE、6 次 POST 500。日志记录事实/Outbox 副作用；错误约 1 秒，关联器随后因响应无法明确关联而停止。两份正式结果均 incomplete，禁止改写 |
| 已用诊断 | `diagnostic-b02-r1`、`diagnostic-b02-r3` 各 12,000 测量请求、零业务错误；两次未复现不能替代正式失败，也不能因换目录再增加同类负载探测次数 |
| 待执行 | B02 正式通过、B04/B05/B06 正式单元、B03 四组合三重复、S4；完整 B07 只对上述旧候选有效 |
| 已知局部实现 | 评论/bookmark 已有有界恢复；帖子 Create/Update/Delete 的提交与响应路径尚未覆盖。现象相似不证明原负载的 SQL/提交/提交后读取失败阶段相同 |

HTTP 500 和事实/事件已存在，是已观察到的业务响应一致性失败。MySQL 1 秒读写配置与
耗时相符，只能作为线索；不能直接宣布已证明 COMMIT 应答丢失、配额不足或根因已经修复。
先按失败 request_id 核对已有请求台账、原始事实/事件、已有日志/Trace，记录可证明的
失败阶段和底层错误。日志缺少底层错误时如实记录缺口，使用已具备的 MySQL 协议故障
代理证明具体代码边界；不重启整套负载采集来碰运气，也不把人工注入当成原负载根因证明。

**一次修复的封闭范围与通过条件：**

| 允许文件 | 必须证明的行为 |
| --- | --- |
| backend/internal/post/repository.go、edit.go、delete.go、service.go | 仅修复已暴露的创建/更新/删除事务与提交后响应边界；保留底层错误及失败阶段，不重写其他业务 |
| backend/internal/post/transaction_recovery.go、transaction_recovery_test.go（必要时新增） | 共用最小恢复逻辑与直接回归；不为整个项目引入新的重试框架 |
| backend/internal/post/integration_test.go、edit_integration_test.go | 原子 Outbox、编辑 revision/no-op、删除事实及陈旧缓存的受影响回归 |
| backend/internal/http/transaction_recovery_integration_test.go | 在现有独占 MySQL/HTTP fixture 中覆盖三条已暴露写路由的提交前失败、未知提交结果和失败负例 |
| backend/internal/platform/mysql_transaction.go、mysql_transaction_test.go | 仅当现有公共辅助边界确实阻碍上述修复时修改；保留评论/bookmark 的有界与取消合同 |
| backend/internal/integrationtest/mysql_proxy.go | 仅补充证明已观察写入边界所需的故障动作；不得变为依赖审计或通用故障平台 |
| 本批同名实施日志 | 只记录实际修改、命令、结果、成本、偏差与限制；执行过程先在仓库外追加 |

不调整 mysql.go 的通用读写超时、Compose quota、负载/窗口、错误率门禁或 evidence 关联规则。
如果证据最终只支持资源合同变化而非上述产品修复，保存因果与同条件对照，停止该修复路径，
先修订确切资源合同；不能以本节授权自行加资源。其他产品文件仍按根规则先修订规划。

1. 提交前已知未成功的暂态失败，仅在事务收尾后允许一次安全重试；普通错误、永久错误、
   权限失败和取消不重试。复用现有最多 3 秒的总恢复上下文，继承更短调用方期限。
2. 提交结果未知时禁止重放创建、revision 增量或删除事件。仅以本次精确对象、作者、
   内容/revision 及预先生成的唯一 Outbox event_id 核对事实；删除须同时证明删除终态
   与本次删除事件，不能凭“现在不存在”返回成功。不能用另一请求的同内容/事件冒认成功。
3. 提交后读取/构造响应失败要与 Commit 失败区分。成功响应返回本次可证明的 ID/revision，
   不能把后续并发更新结果冒认成本次提交；证明不充分仍返回错误，不以假成功闭合台账。
4. 保持行锁、作者权限、原子事实/Outbox、编辑 no-op 不新增事件/revision、删除子事实及
   搜索重建协调锁、缓存失效合同。数据库实际回滚、应有事件缺失、取消/超时均须有负例。
5. 内部错误保留原始 cause，并记录可关联 request/attempt/event、阶段及耗时；不得输出
   凭据/正文/完整 SQL，不把高基数标识放入指标标签。验证修复前故障用例失败、修复后通过。

**固定低成本回归：** 下述新增 `TestIntegrationPostTransactionRecovery` 是本节的计划
测试名称，实现前不能记为已有能力。使用 integrationtest 的白名单独占数据库与现有
迁移，不需要完整观测栈；真实测试不得以 skip 记为通过。先验证实际故障，后验证修复。

```bash
# backend 目录；普通直接检查
go test ./internal/platform ./internal/post ./internal/comment ./internal/bookmark ./internal/http
# 独占 MySQL/HTTP 的协议故障与既有事务恢复；新增名称须先实现
go test -tags integration ./internal/http -run '^TestIntegration(TransactionRecovery|PostTransactionRecovery)$' -count=1 -timeout=90s -v
# 保持原子事件、编辑和删除安全边界
go test -tags integration ./internal/post -run '^TestIntegration(PostCreateCommitsOutboxAtomicallyAndRollsBackOnOutboxFailure|EditTransactionNoopAndStaleCache|PermanentDeleteAtomicityAndStaleCache)$' -count=1 -timeout=90s -v
```

只有实际更改代理并发行为/公共恢复逻辑时增加对应 race 检查，不扩大到全库集成测试。
预算/closure/evidence 工具及配置未变的成功直接检查继续有效；若实际变更，重验相应
入口分发、模式/参数、失败传播、schema 和归属清理，再允许构建真实验收栈。

**冻结与复用：**

- 在直接修复与回归完成后提交产品修复，列出“文件→构建目标→gate→失效原因”。新候选
  仍必须绑定全部实际镜像/OCI 元数据、product tree、manifest 和工具指纹；复用已有
  build cache 与第三方 digest，不重绑旧回执或换标签冒充新候选。
- 用 `git worktree add --detach <新冻结源码目录> <完整已提交revision>` 建立干净工作树。
  从该目录调用已有构建/验证脚本，记录实际源码根、HEAD、manifest 和各输入 digest；
  S1～S4 使用这一源码根，禁止中途切换到更新后的主工作树。原 manifest 不覆盖。
- 冻结期间过程日志/时间台账写在私有工作根，成功后或实际停点再向本批同名实施日志
  同步事实。纯日志提交不需要重建该冻结工作树或宣告它失效；验收始终验证原源码和
  原始候选身份。这通过固定执行来源实现，不修改 verifier、跳过 HEAD 校验或伪造来源。
- 8a4d780 的 B07/B01 和此前 B06 都保留。产品写入变化使新候选的受影响 S1/B02/B07/S3
  必须重验；旧候选回执不移入新目录。无变化的直接检查/构建缓存可以复用。
- closure 当前没有 --resume，只能完成一次当前候选完整 B07，不能承诺复用旧 U 单元。
  budget 的 --resume 仅用于同候选、输入/环境一致、成功回执完整且未执行到失败的
  中断目录，先核对已有回执 digest；正式失败目录不作为成功检查点续跑。新候选完整
  S3 仍使用正式入口，局部 formal=false B02/B06 不升级为正式通过。

**成功后连续推进的固定路径：**

| 阶段 | 执行动作与门槛 | 成功后动作 |
| --- | --- | --- |
| R0 直接修复 | 上述最低层故障、修复和安全回归通过；保留尚未证明的原负载因果缺口，明确重验集合 | 提交产品修复并冻结新源码工作树，不结束整个实施请求 |
| R1 物化/冒烟 | 从冻结根建立新 manifest；执行受影响 S0/S1 和既有 dry-run，核对实际镜像与工具身份 | 同候选进入 B07；不添加第四次正式重复 |
| R2 B07 | 完整 U1～U4 与外部 `--preflight` evidence 校验 complete | 剩余预算足够则立即进入 S3，不要求另一次确认 |
| R3 S3 | 正式入口引用同候选 B07，按 B01→B02→B04→B05→B06→B03 执行；B03 O0/O3/O1/O2 各三次 | 外部 `--budget` 重算通过后进入 S4，不再额外跑单次 B02/B06 探针 |
| R4 S4 | 固定 publication/source、工具交付门禁、版本/分支和选定脱敏工件验证；记录实际结果 | 全部门禁通过才完成日志和 VERSION=2.2.5，创建完成提交；05 完成不自动执行 06 |

R1～R4 从固定源码目录执行 4.2 已登记入口；不使用无 --preflight 的 closure 来代替
05 预算验收，否则会误启动属于 06 的十二阶梯与两个 60 分钟正式单元。预算正式命令为
`scripts/verify-phase20-budget.sh --manifest <新manifest> --work <新正式目录>
--preflight-evidence <同候选B07目录>`。真实 HTTP 500、事实不一致或安全失败立即停止；
不等待后续关联器将其统一分类为工具错误。任何工具失败先退出完整入口，修复最小层。
已用两次 B02 负载诊断保留计数；不增加同类负载探测，不靠重复矩阵寻找零错误窗口。
只有具体代码边界可复现、实际修正且直接回归通过后，才进入上述受影响真实验收。

**一个追加窗口的预算与停点：** 历史 15 小时报告、已登记修复成本及缺失时间台账分别
保留。本节不创建新批次、重置文件成本或自动授予重试窗口。用户再次明确执行时，先登记
可证历史成本与缺口，才启动本次剩余窗口；跨命令、候选、源码目录和 agent 累加不清零。

| 剩余阶段（含该阶段失败收尾） | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 已有证据核对与最低层因果回归 | 5 | 10 |
| 三条写路由的有界修复与直接检查 | 25 | 30 |
| 候选构建、物化与受影响 S0/S1 | 15 | 25 |
| 同候选完整 B07 与外部校验 | 15 | 20 |
| S3 固定预算矩阵 | 55 | 90 |
| S4、日志/版本/提交与最终清理 | 5 | 5 |
| 合计 | 120 | 180 |

正式测量不可压缩部分至少为 B02 的 15+60 秒、B03 的 12×(15+60)=900 秒，以及 B04
的 Collector 60 秒/worker 10 秒故障；各阶段还须保留实际恢复门禁和归属清理。B03 曾
约 57 分钟，说明总成本包含大幅环境开销，不能按上述测量秒数声称整矩阵只需十几分钟。
R1/R2 实测初始化/物化/恢复/清理后，按剩余单元数预测 R3 成本；预测加有界清理超过
阶段或累计余量时不启动。上限是停止条件，不是保证能装下剩余工作的估计。
90/144 分钟报告进度；同一原因既有诊断次数不清零，任何新工具原因仍最多两次、每次
最多 10 分钟且受总余量约束。必要实验窗口、零业务错误和最终完整候选门禁不能缩短。
到累计/阶段停点或重复诊断停点，保存失败、实际成本、未开始案例、有效身份和最小
后续清单，解除辅助控制并做有界归属清理；只记录未完成，不更新 VERSION。
继续成功路径无需逐项确认；预算耗尽后的新执行仍须修订有限剩余计划并明确恢复。

#### B 案例、U 清单与固定证据位置

每个候选使用新的私有工作根 `<work>`；每个 B 案例生成独立目录
`<work>/budget/<case_id>/`，包含 `receipt.json`、原始样本、候选绑定和清理回执。
路径由 manifest 登记为相对路径并做归属校验；历史固定目录保留为历史证据，禁止覆盖。
以下正式动作和统计窗口保持固定，S1/S2 的短窗均另标 formal=false：

| case_id | 冻结操作、实际判据与证据位置 |
| --- | --- |
| B01 | `docker compose config --format json`、逐服务 `docker inspect`、runtime env/队列配置重算 CPU/内存/连接/队列；输出 `budget-contract.json`、`compose.json`、`inspect.json`。 |
| B02 | 独立空项目以 200 RPS 预热 15 秒、测量 60 秒，5 秒采样，停止后最多排空 30 秒，再独立执行业务/Logs/Metrics/Events 120 秒恢复；输出 `normal-window.jsonl`、`resources.jsonl`、`recovery.jsonl` 和闭合 receipt。 |
| B03 | 同一宿主、recipe/业务比例、Go 压测器、200 RPS、15+60 秒和三重复；O0=`业务+HTTP基准，Trace/Router/Marshaller/Monitor 关闭`、O1=`正常观测+Trace关闭+sampler开启`、O2=`正常观测+Trace 100%+sampler开启`、O3=`正常观测+Trace关闭+sampler关闭`。四组合均使用相同独立低开销计数源，按 2.4 执行次序和配对输出原值、median/min/max/CV、差值和门禁。 |
| B04 | 在独立 200 RPS 故障窗口停止验收 Collector 60 秒，验证 Trace queue/导出失败和业务不阻塞；另在可清理的短窗口暂停 Business Worker 10 秒制造 Rabbit ready backlog 后恢复，验证接受事实、重试/背压、最终水位；输出注入/恢复时刻、计数、故障 receipt。 |
| B05 | 只在 `phase20_trace_data` 归属卷/fixture 触发 64 MiB 轮转水位，记录 volume used/free、文件列表和 Collector 回执；不写宿主根目录、不删除其他项目；输出 `disk-waterline.json` 与原始目录清单。 |
| B06 | 在 B04 产生可控积压后对归属 worker/Collector 发送 SIGTERM，核对 grace/lease/offset/业务事实、排空和归属清理；保留 `shutdown.json`、容器状态和 cleanup inventory，禁止 global prune。 |
| B07 | S3 之前运行固定 U1～U4 真实短预检：启动、recipe、负载、三通道水位、C01 链路、R02/R03/R04/R06/R07/R08 生命周期、B04 故障、B06 关停、发布清单与归属清理；输出 `<work>/preflight/`，不能引用本批正式结果。 |

| B07 单元 | 固定短预检与证据 |
| --- | --- |
| U1 | 50/200 RPS 各一次独立空项目、5 秒预热+10 秒测量，完整台账与业务/三通道独立恢复；验证使用正式编排路径但不计入正式重复 |
| U2 | 200 RPS、15 秒预热后持续 300 秒；第 60 秒停止 Collector，持续 60 秒后恢复，期间业务继续；故障后执行独立业务/观测 120 秒恢复及探针，结束排空；不生成 60 分钟稳定性结论 |
| U3 | 当前候选真实执行 C01 和 R02/R03/R04/R06/R07/R08，记录原始 span/查询/删除及恢复事实；B06 用真实积压验证 SIGTERM；调用已登记的真实 chain/retention/fault 入口，单元测试另列 |
| U4 | 对上述实际候选、原始证据及选定脱敏工件执行来源 digest、凭据与归属检查；验证每个 cleanup inventory；生成并外部校验 B07 receipt |

06 正式 U1～U4 的固定内容分别为：U1 执行 50/100/150/200 RPS 四阶梯三重复；U2 执行两次
相同 recipe/200 RPS 的 60 分钟运行，预热不计时，第 15 分钟执行上表 Collector 故障
60 秒，结束后排空并恢复；U3 执行当前候选的 C01 与 R01～R08，只有候选/配置/依赖/
环境均可证明未变时才逐项引用前序 receipt；U4 重算候选 manifest、所有原始 digest、
脱敏白名单、凭据/归属泄漏和每个清理 inventory。U1～U4 的证据分别固定在
`<work>/closure/u1/` 至 `u4/`，与当前 manifest 绑定；06 只能读取本节最终冻结合同。

#### 依赖、构建配方与发布清单

第三方镜像按 `deploy/release/third-party.lock.json` 的 `linux/amd64` digest 冻结：
MySQL `sha256:3e5649c69e6d75cf88fc6f8f39f877453faa4e5167b5e648007e45f54bb17f6b`、
Redis `sha256:015185fd658093359cc83aa8396e06c1f39ba36df3c93f79528ec23ab409e73f`、
RabbitMQ `sha256:c01be497320fe7f7dbd599f4cea0b47098e397f122899d016638972d52c7e245`、
Elasticsearch `sha256:4dd70111e8be29321bb7cdbc621032a77d5aec426e5f46522e05072056f912f7`、
Kafka `sha256:ccd1314e47ec76909e01f86308b4dcf2064f19f7c89759234322314b0e319e26`、
VictoriaMetrics `sha256:fe3c311a3f1cddf52985dae7ceaf7de8aa111f5c5f591c81b6ab03b19bbeba19`，
Collector 使用上表 04 digest。自研制品不在 05 开工前伪造 digest；05 预检从实现完成
revision 以 `GOPULSE_VERSION=2.2.5`、`GOPULSE_REVISION=<git revision>`、
`GOPULSE_IMAGE_TAG=phase20-05-<short revision>` 构建 backend、worker、search-indexer、
router、marshaller、monitor、redis-exporter、两前端和 acceptance，并将实际 image ID
写入预检 manifest。06 必须从含 05 完成提交的 main 重新构建最终自研制品。

05 允许发布到仓库的脱敏集合冻结为：同名实施日志、`docs/observability-resource-budgets.md`、
`docs/phase20-acceptance-matrix.md`、`dev/validation/Phase-20/phase20-capacity-methodology.md`、预算/profile/
schema、runtime/Trace 配置、`README.md`、`dev/status/capability-status.md`、六处阶段状态/版本元数据、
严格 verifier 生成的 `summary.json`/`evidence-manifest.json`；原始业务身份、正文、凭据、
容器环境和私有 `/var/tmp` 证据只保留在仓库外。发布前必须以来源 digest 校验该白名单，不能
把未构建的最终自研镜像 digest 写入本批日志。

## 3. 允许变更文件与验收

| 文件 | 验收要求 |
| --- | --- |
| deploy/phase20-resource-budgets.json、deploy/phase20-resource-budgets.schema.json | 精确可机读预算、测量身份/窗口、三种比较配对、执行保护时限、超限与采样/Trace 开销阈值 |
| deploy/compose.yaml、deploy/phase20-trace.yaml、deploy/runtime-contracts.json、deploy/runtime-contracts.schema.json | 预算真正生效、私有网络和现有角色一致 |
| loadtest/phase20-capacity-profile.json、loadtest/phase20-capacity-profile.schema.json、loadtest/phase20-sustained-profile.json、loadtest/phase20-sustained-profile.schema.json | 最终四阶梯/三重复及两次 60 分钟运行的负载、阈值和故障合同 |
| scripts/ci/phase20_budget.py、test_phase20_budget.py、scripts/verify-phase20-budget.sh | 实际 inspect、增长、独立测量的开销、饱和和故障降级证据，支持 smoke/case/combination/resume 并严格区分诊断与完整验收 |
| scripts/ci/phase20_closure.py、test_phase20_closure.py、scripts/verify-phase20-closure.sh | 完整 U1～U4 正式及短预检编排、dry-run 任务图、阶段停点、检查点和归属清理，拒绝临时阈值覆盖 |
| scripts/ci/phase20_sampler.py、test_phase20_sampler.py、phase20_evidence.py、test_phase20_evidence.py（均在 scripts/ci） | 最终资源/生命周期/链路证据可复核，拒绝缺失、错窗和漂移 |
| scripts/verify-phase20-evidence.py、scripts/ci/verify_runtime_contracts.py、test_runtime_contracts.py（后者同 scripts/ci） | 最终入口与机器合同严格验证，不降低历史合同 |
| docs/observability-resource-budgets.md、docs/phase20-acceptance-matrix.md、dev/validation/Phase-20/phase20-capacity-methodology.md | 最终矩阵、候选绑定、数据增长、开销与超限合同 |

产品行为若暴露新的阻断缺陷，不得在预算工作中泛化修复；先按实际风险修订规划及具体文件
清单，再进入实现。日志、状态与版本元数据文件遵循总方案。

## 4. 固定验收、开销口径与回归

| case_id | 操作与通过规则 |
| --- | --- |
| B01 实际预算 | schema/runtime 合同与 Compose 渲染一致；逐容器 inspect 核对 CPU/内存，连接/队列核对生效配置及受控峰值，单位换算可重算，无缺失目标或未生效限制 |
| B02 正常窗口 | 冻结稳定负载下业务/观测闭合，CPU/RSS/容器用量/队列/磁盘均符合各自统计和阈值，采样缺失/自身开销符合合同；不能以全程平均掩盖峰值 |
| B03 开销对照 | 同宿主/配方/负载，观测关闭、正常观测、Trace 关闭/启用及采样器关闭/启用按登记组合各三次独立短运行；逐组合报告原值、差值及开销门禁 |
| B04 观测故障与饱和 | 对已登记出口、Trace 队列及至少一个可安全触发的有界连接/队列实施故障或饱和，保存真实触发证据；拒绝/丢弃/重试/背压符合预算，已接受业务事实不丢失，恢复后水位闭合 |
| B05 磁盘安全水位 | 在独占归属目录或有限配额 fixture 触发冻结水位，不填满宿主；验证触发动作、停止/恢复、增长读数和 Trace 轮转上限；不得触及其他项目数据 |
| B06 关停与清理 | 有积压时 SIGTERM，按合同完成关闭或有理由的有限退出；lease/offset/业务事实不破坏，归属工件清理可核对，无全局 prune |
| B07 最终工具预检 | U1～U4 固定编排自测；同一非正式候选真实短预检覆盖启动、负载、业务/三通道水位、C01 关联、R02/R03/R04/R06/R07/R08 生命周期、故障、关停、发布校验和归属清理 |

开销比较以各组合三重复的 median 为统计，按候选相同稳定负载比较业务 P99、CPU 核秒/RSS
等原值，CPU/RSS 峰值增量门禁与累计 CPU/区间平均分列。固定比较为正常观测 O3-O0、
Trace O2-O1、采样器 O1-O3；禁止同一对照差值充当两个独立成本。关闭到启用的差值与比例
都保留；基线为零时比例为 null，只判冻结绝对差阈值，不能增加未登记的比例门禁。
“关闭观测”的具体范围在 profile 列出；若进程仍在产生日志/指标，差值只代表被关闭的
采集/运输/存储部分，不能声称测出所有观测成本。停止采样器的对照使用同样冻结的低开销
基准计数源收集业务及资源结果，不能以无记录为零开销。

饱和测试不得通过临时放宽 quota/阈值使结果通过；与正常容量 profile 不同的注入或诊断负载
明确 formal=false。Trace 的有理由丢弃与可靠业务事件分别判定；普通观测出口失败不能
解释已接受业务丢失。缺少触发或恢复证据属于 incomplete，动作不符合合同或有效测量超阈值
属于门禁失败；分别记录 execution_status 与 case status/失败分类，不能把所有异常统一记成工具 incomplete。
连接/队列无法安全触发时必须开工前冻结最低层受控验证，不可执行后静默删例。

### 4.1 最终工具交付门禁

05 必须实现 06 将执行的完整接口，正式路径只能在 06 执行昂贵矩阵，但必须在 05 完成
以下定向自测和真实短预检：

| 交付项 | 05 的必要证明 |
| --- | --- |
| 完整正式编排 | 无 `--preflight` 的路径确实编排 U1 十二个单元、U2 两个 60 分钟单元、U3 C01/R01～R08、U4 发布校验；`--dry-run` 输出同一正式任务图与身份/profile 绑定，仅不执行依赖动作，formal=false，不产生真实通过 receipt |
| 持续运行 runner/verifier | B07 U2 用同一 runner 的冻结短 profile 执行；定向 fixture 验证正式 10～15/55～60 分钟窗口、增长斜率、故障时长、缺样/错窗及最终全量台账，不用短运行证明长窗口通过 |
| 真实链路与生命周期 | B07 U3 从当前候选真实入口取得原始事实并外部重算；禁止以 `unittest` 退出码、占位 `pass`、容器仅 Running 或旧版本 receipt 代替产品事实 |
| 完整证据接口 | 支持 `--budget`、`--preflight`、`--closure`、`--publication <目录> --source <源目录>`；校验当前源候选、实际选定发布集合及各原始 digest，缺源/缺案例/错候选/错窗/回执篡改均拒绝 |
| 停点与恢复 | 验证阶段失败阻止后续启动、采样线程失败可及时终止负载、相同候选 resume 只执行未开始单元、候选或工具漂移拒绝 resume；归属清理保存 inventory，不能 global prune |

正式接口返回“本阶段尚未实现”、只支持 preflight 或接受人工拼装的 pass 摘要，均不能
通过本批工具交付门禁。单元测试只证明工具逻辑，真实产品通过必须另有原始执行事实。

### 4.2 执行命令与回归

下列新增 smoke/case/combination/resume/dry-run/preflight-evidence 及 closure/publication
参数是本批必须实现的合同接口，不声明当前已存在。按 2.4 顺序执行；诊断短窗由 profile 固定：

```bash
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure scripts.ci.test_phase20_sampler scripts.ci.test_phase20_evidence
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.5 --skip-version
scripts/verify-phase20-closure.sh --build-manifest <本批候选manifest> --revision <已提交实现revision>
scripts/verify-phase20-closure.sh --dry-run --manifest <本批候选manifest> --work <任务图新目录>
scripts/verify-phase20-budget.sh --smoke --manifest <同候选manifest> --work <冒烟新目录>
scripts/verify-phase20-closure.sh --preflight --manifest <同候选manifest> --work <预检新目录>
python3 scripts/verify-phase20-evidence.py --preflight <预检目录>
scripts/verify-phase20-budget.sh --manifest <同候选manifest> --work <预算新目录> --preflight-evidence <已校验预检目录>
python3 scripts/verify-phase20-evidence.py --budget <预算目录>
python3 scripts/verify-phase20-evidence.py --publication <实际选定发布目录> --source <本批证据根目录>
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.5
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.5 --base-ref origin/main
git diff --check
```

完成前根 VERSION 保持上个已完成版本，候选版本由构建参数和 runtime/manifest 绑定为
2.2.5；S0 的 `--skip-version` 只跳过根完成版本相等检查，候选/runtime/预算身份仍必须
校验。S4 的最后三项版本/分支检查和不带 `--skip-version` 的 runtime 校验，在验收通过并
同步完成元数据后、完成提交前执行；不能提前改 VERSION 让 S0 通过。

正式预算入口必须校验 `--preflight-evidence` 的 B07 当前候选、工具/profile/config/digest
与实际环境身份，缺失或漂移即拒绝执行。局部定位示例为
`scripts/verify-phase20-budget.sh --case B03 --combination O3 --repeat 1 --manifest <候选manifest> --work <诊断新目录>`；
该结果不代替固定十二单元。对同候选尚未开始单元的恢复使用 `--resume <未完成目录>`，
仍需提供原 manifest 和同身份的预检证据，不能绕过失败处理。

回归角色/端口边界、真实资源限制、生命周期、观测失败不阻塞业务以及安全清理；
不在本批提前运行 06 的正式三重复容量或两次 60 分钟矩阵。

## 5. 冻结时点与完成条件

B01～B07、2.4 的测量身份/执行停点及 4.1 的完整工具交付门禁全部通过，执行状态 complete，
预算与行为一致，开销及增长事实完整。必要证据含同候选 S0/S1/B07 的通过记录、B01～B06
正式回执及十二个 B03 原始单元、完整正式 dry-run 任务图、阶段耗时、外部校验和实际发布校验。
局部诊断、短预检、文档修订或单元测试成功均不单独构成本批完成。
冻结构建配方、依赖/第三方镜像 digest、矩阵/profile/工具、预算/保留/Trace 合同和发布清单，
创建同名实际日志、更新 2.2.5 并提交后完成。本批不替代最终容量或持续运行结论。

05 的预检 manifest 只绑定该预检的 revision 和制品，不冻结尚未构建的最终自研 digest。
06 fetch 后选定包含本批完成提交的 main revision，按冻结配方构建实际 Bundle/镜像，再
冻结最终 manifest 与宿主证据，先执行该候选预检再执行正式矩阵。不能将 05 receipt 的
revision 改成 06 候选，也不能以配方相同为由省略 06 当前候选预检。
