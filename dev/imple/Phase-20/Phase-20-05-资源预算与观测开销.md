# Phase-20-05：资源预算与观测开销

> 目标版本：2.2.5；开发分支：develop/2.2.5；当前状态：已冻结开工合同，未开始实现。

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

预算数值、开销阈值及最终 profile 在创建本批分支前依据 01～04 证据，在 update 细化并合入
main。完成本批时构建配方及全部执行合同冻结；实际最终镜像/Bundle/manifest 在 06 从包含
本批完成提交的 main 构建后冻结。06 不再修改任何可执行或验收配置。

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
| 宿主 | Linux `amd64`、WSL2 kernel `6.6.87.2-microsoft-standard-WSL2`、8 CPU、`MemTotal=13281496 kB`、swap `16777216 kB`、`/var/lib/docker` 可用 `105056022528` bytes；Docker Server `29.7.2`、Compose `5.5.0`。初版 profile 下限为 100 GB；经授权的 r3 修订后，当前执行下限为 50 GB（`50000000000` bytes）。执行前再次写入 `host.json`，不以本表替代实际 preflight。 |
| 业务配方 | 沿用 Phase 20 配方 seed `18002005`、recipe digest `sha256:0e61a5473f72d735ab322261e312290249f39997b649837fea32bfe2c947cf14`、`1024` virtual users、50/100/150/200 RPS、15 秒预热、60 秒测量、三重复；原始 profile 为 `loadtest/phase20-capacity-profile.json`。 |
| 前序资源依据 | 03 的严格复验 `docs/phase20-optimization.md` / 私有 contract digest `sha256:190405c1c6c2b72656d6699afcc5772b6cd2e0284be4431a44682bc92d8a1635`：容器 CPU 峰值 `806.6%`、RSS 峰值 `4462.235990524292 MiB`、Kafka lag 峰值 `7268`、Rabbit ready 峰值 `0`、unacked 峰值 `2`；观测 ES 单元增长最大 `13677487` bytes。 |
| 前序开销依据 | 01 的同宿主采样器对照为启用/停用各 250 次、50 RPS、5 秒窗口，P95 `23.14/19.85 ms`、验收进程 CPU `3.656/3.259 s`；该值只冻结短窗口开销测量方法，不作为 05 正式容量结果。 |
| Trace/保留依据 | 04 已冻结 Collector `0.138.0` digest `sha256:d535a52679b1df0a95b1b6fc4322cb74ecddd61f0b550cb43444d2b22cedec0c`、单文件 `16 MiB`、总量 `64 MiB`、最多 `3` 个备份；VM `30d`、Logs/Events `7d` 的生命周期合同保持不变。 |

#### 预算与超限合同

所有 CPU/RSS/队列样本间隔固定为 `5s`；持续运行的窗口趋势使用固定样本，比较第
10～15 分钟与第 55～60 分钟的 median。正常 200 RPS 窗口的阈值如下，阈值均以
03 的实测最大值加明确余量冻结；B04/B05 的注入阈值不能覆盖这些正常窗口阈值。

| budget_id | 目标与冻结限值 | 超限动作与恢复判据 |
| --- | --- | --- |
| `cpu.sut_peak_cores` | 所有归属 SUT 容器 CPU 峰值合计 `<=8.75 cores`；另列 load generator `<=1.50 cores`、sampler `<=0.75 cores`、Collector `<=0.50 cores`、宿主忙核 `<=8.00 cores`；记录 quota、实际用量和 throttling。 | 连续 3 个样本超限为 `budget_exceeded`，停止当前正式单元并保留原始样本；无 throttling、停止调度后 120 秒内水位恢复且下一独立单元从空项目开始。 |
| `memory.sut_rss_peak` | SUT 容器 RSS 合计峰值 `<=6 GiB`；容器 `memory.current` 合计 `<=8 GiB`，分别记录每服务峰值；两次持续运行稳定窗口绝对增长 `<=512 MiB`、斜率 `<=16 MiB/min`。 | OOM、非预期重启或连续 3 个样本超限立即停止并标为失败；正常关停且无 OOM、窗口趋势均在限值内才恢复。 |
| `queue.business` | Outbox pending 正常峰值 `<=100`；Rabbit ready `<=50`、unacked `<=16`；Kafka 观测 group lag 合计峰值 `<=9000`。 | 记录拒绝、重试、丢弃或背压的计数；接受事实不能丢失，停止请求后独立业务/观测水位均在 120 秒内闭合。 |
| `queue.trace` | Trace SDK queue capacity 固定 `2048`，Collector file exporter 单轮观察的队列/失败计数均记录；正常窗口不允许业务错误，允许尽力而为 span 在出口故障时有界丢弃。 | Collector 出口失败只触发 bounded retry/drop 和故障 receipt，不得阻塞或丢失已接受业务事件；恢复后 Collector 与业务探针在 120 秒内闭合。 |
| `disk.host` | 每个归属项目记录真实卷 used/free；宿主 free bytes `>=50 GB`（`50000000000` bytes），逻辑水位 `>=55 GB` 为 warning；不得用逻辑水位代替 OS/容器配额。 | 达到 warning 停止增长型 fixture 并记录；达到 safety 水位立即停止本单元并安全清理，禁止填满宿主或执行 global prune。 |
| `disk.observability_growth` | 观测 ES 真实卷增长 `<=32 MiB/min`；Trace 目录单文件 `<=16 MiB`、总量 `<=64 MiB`、文件总数 `<=4`；按真实 docs/store/卷读数报告压缩、索引放大和回收误差。 | 触发保留/轮转合同，超限保存原始读数并失败；只删除归属目录，其他项目和业务索引不进入清理请求。 |
| `overhead.observability` | O0/O1/O2/O3 各三重复：正常观测相对观测关闭的业务 P99 绝对差 `<=150 ms` 且比例 `<=25%`；SUT CPU 差 `<=1.50 cores`、RSS 差 `<=512 MiB`；Trace 关闭/100% 采样业务 P99 差 `<=200 ms`、CPU 差 `<=1.00 core`、RSS 差 `<=256 MiB`。 | 只报告已登记组件集合的成本，不外推全站观测成本；任一组合缺原始三重复、负载不一致或基线为零而未使用绝对阈值则 incomplete。 |
| `overhead.sampler` | sampler 开启/关闭各三重复，使用相同低开销业务计数源；观察者 CPU 峰值 `<=0.75 core`，业务 P99 绝对差 `<=50 ms`，采样缺样 `<=1%`。 | 不得以“没有记录”记为零开销；超限保留两套原始记录，业务结果仍按独立业务门禁判定。 |

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

#### 2.3 授权后的宿主磁盘水位修订

用户于 2026-10-01 明确授权将本批次宿主磁盘最低要求从初版的 100 GB profile 下限与
90 GiB 运行时安全水位调整为 50 GB。调整后的冻结编号为
`phase20-05-budget-contract-20261001-r3`；50 GB 采用十进制定义，即精确为
`50000000000` bytes。该值同时绑定 resource budget 的 `platform.disk_free_bytes_min`、
`disk.host_free_min`、Phase 20 容量 profile、持续运行 profile 和 B04/B05 安全停止条件；
其他 CPU/内存/队列/观测开销阈值及 B/U 操作不变。

调整前的 r2 验收目录和失败证据保留为历史记录，不得改写为 r3 结果。合同编号或磁盘门禁
发生变化即视为候选失效；必须以包含 r3 的新 revision 重建候选并重新执行 B01～B07 及固定
门禁。历史 Docker 清理由用户单独授权，只能删除已停止且已核对无运行中引用的旧项目资源，
不得使用 global prune，也不得删除当前候选或第三方锁定依赖。

#### B 案例、U 清单与固定证据位置

每个 B 案例都必须生成独立目录 `/var/tmp/gopulse-phase20-05-20261001/budget/<case_id>/`，
包含 `receipt.json`、原始样本、候选绑定和清理回执；以下动作与位置在执行中不可删改：

| case_id | 冻结操作、实际判据与证据位置 |
| --- | --- |
| B01 | `docker compose config --format json`、逐服务 `docker inspect`、runtime env/队列配置重算 CPU/内存/连接/队列；输出 `budget-contract.json`、`compose.json`、`inspect.json`。 |
| B02 | 独立空项目以 200 RPS 预热 15 秒、测量 60 秒，5 秒采样，停止后最多排空 30 秒，再独立执行业务/Logs/Metrics/Events 120 秒恢复；输出 `normal-window.jsonl`、`resources.jsonl`、`recovery.jsonl` 和闭合 receipt。 |
| B03 | 同一宿主、recipe、200 RPS、15+60 秒和三重复，固定执行 O0=`业务+HTTP基准，Trace/Router/Marshaller/Monitor 关闭`、O1=`正常观测+Trace关闭+sampler开启`、O2=`正常观测+Trace 100%+sampler开启`、O3=`正常观测+Trace关闭+sampler关闭`；O3 使用独立低开销计数源，不能以无记录代替。四个组合均输出原值、median/min/max/CV、差值和门禁。 |
| B04 | 在独立 200 RPS 故障窗口停止验收 Collector 60 秒，验证 Trace queue/导出失败和业务不阻塞；另在可清理的短窗口暂停 Business Worker 10 秒制造 Rabbit ready backlog 后恢复，验证接受事实、重试/背压、最终水位；输出注入/恢复时刻、计数、故障 receipt。 |
| B05 | 只在 `phase20_trace_data` 归属卷/fixture 触发 64 MiB 轮转水位，记录 volume used/free、文件列表和 Collector 回执；不写宿主根目录、不删除其他项目；输出 `disk-waterline.json` 与原始目录清单。 |
| B06 | 在 B04 产生可控积压后对归属 worker/Collector 发送 SIGTERM，核对 grace/lease/offset/业务事实、排空和归属清理；保留 `shutdown.json`、容器状态和 cleanup inventory，禁止 global prune。 |
| B07 | 运行固定 U1～U4 短预检：启动、recipe、负载、三通道水位、C01 链路、R02/R03/R04/R06/R07/R08 生命周期、B04 故障、B06 关停、发布清单与归属清理；输出 `/var/tmp/gopulse-phase20-05-20261001/preflight/`，不能引用本批正式结果。 |

U1～U4 的固定内容分别为：U1 执行 50/100/150/200 RPS 四阶梯三重复；U2 执行两次
相同 recipe/200 RPS 的 60 分钟运行，预热不计时，第 15 分钟执行上表 Collector 故障
60 秒，结束后排空并恢复；U3 执行当前候选的 C01 与 R01～R08，只有候选/配置/依赖/
环境均可证明未变时才逐项引用前序 receipt；U4 重算候选 manifest、所有原始 digest、
脱敏白名单、凭据/归属泄漏和每个清理 inventory。U1～U4 的证据分别固定在
`/var/tmp/gopulse-phase20-05-20261001/closure/u1/` 至 `u4/`，06 只能读取本节冻结合同。

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
`docs/phase20-acceptance-matrix.md`、`docs/phase20-capacity-methodology.md`、预算/profile/
schema、runtime/Trace 配置、`README.md`、`docs/capability-status.md`、六处阶段状态/版本元数据、
严格 verifier 生成的 `summary.json`/`evidence-manifest.json`；原始业务身份、正文、凭据、
容器环境和私有 `/var/tmp` 证据只保留在仓库外。发布前必须以来源 digest 校验该白名单，不能
把未构建的最终自研镜像 digest 写入本批日志。

## 3. 允许变更文件与验收

| 文件 | 验收要求 |
| --- | --- |
| deploy/phase20-resource-budgets.json、deploy/phase20-resource-budgets.schema.json | 精确可机读预算、单位、超限与采样/Trace 开销阈值 |
| deploy/compose.yaml、deploy/phase20-trace.yaml、deploy/runtime-contracts.json、deploy/runtime-contracts.schema.json | 预算真正生效、私有网络和现有角色一致 |
| loadtest/phase20-capacity-profile.json、loadtest/phase20-capacity-profile.schema.json、loadtest/phase20-sustained-profile.json、loadtest/phase20-sustained-profile.schema.json | 最终四阶梯/三重复及两次 60 分钟运行的负载、阈值和故障合同 |
| scripts/ci/phase20_budget.py、test_phase20_budget.py、scripts/verify-phase20-budget.sh | 实际 inspect、增长、开销、饱和和故障降级证据 |
| scripts/ci/phase20_closure.py、test_phase20_closure.py、scripts/verify-phase20-closure.sh | 单候选固定编排、阶段停点与归属清理，拒绝临时阈值覆盖 |
| scripts/ci/phase20_sampler.py、test_phase20_sampler.py、phase20_evidence.py、test_phase20_evidence.py（均在 scripts/ci） | 最终资源/生命周期/链路证据可复核，拒绝缺失、错窗和漂移 |
| scripts/verify-phase20-evidence.py、scripts/ci/verify_runtime_contracts.py、test_runtime_contracts.py（后者同 scripts/ci） | 最终入口与机器合同严格验证，不降低历史合同 |
| docs/observability-resource-budgets.md、docs/phase20-acceptance-matrix.md、docs/phase20-capacity-methodology.md | 最终矩阵、候选绑定、数据增长、开销与超限合同 |

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
等原值。关闭到启用的差值与比例都保留；基线为零时比例为 null，只判冻结绝对差阈值。
“关闭观测”的具体范围在 profile 列出；若进程仍在产生日志/指标，差值只代表被关闭的
采集/运输/存储部分，不能声称测出所有观测成本。停止采样器的对照使用同样冻结的低开销
基准计数源收集业务及资源结果，不能以无记录为零开销。

饱和测试不得通过临时放宽 quota/阈值使结果通过；与正常容量 profile 不同的注入或诊断负载
明确 formal=false。Trace 的有理由丢弃与可靠业务事件分别判定；普通观测出口失败不能
解释已接受业务丢失。缺少触发或恢复证据属于 incomplete，动作不符合合同属于门禁失败。
连接/队列无法安全触发时必须开工前冻结最低层受控验证，不可执行后静默删例。

本批命令如下；phase20 入口待实现，B07 的短窗口与固定案例由预检合同读取：

```bash
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure scripts.ci.test_phase20_sampler scripts.ci.test_phase20_evidence
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.5
scripts/verify-phase20-budget.sh --manifest <本批非正式候选manifest> --work <预算新目录>
python3 scripts/verify-phase20-evidence.py --budget <预算目录>
scripts/verify-phase20-closure.sh --preflight --manifest <同候选manifest> --work <预检新目录>
python3 scripts/verify-phase20-evidence.py --preflight <预检目录>
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.5 --base-ref origin/main
git diff --check
```

回归角色/端口边界、真实资源限制、生命周期、观测失败不阻塞业务以及安全清理；
不在本批提前运行 06 的正式三重复容量或两次 60 分钟矩阵。

## 5. 冻结时点与完成条件

B01～B07 及固定门禁全部通过、执行状态 complete，预算与行为一致，开销及增长事实完整。
冻结构建配方、依赖/第三方镜像 digest、矩阵/profile/工具、预算/保留/Trace 合同和发布清单，
创建同名实际日志、更新 2.2.5 并提交后完成。本批不替代最终容量或持续运行结论。

05 的预检 manifest 只绑定该预检的 revision 和制品，不冻结尚未构建的最终自研 digest。
06 fetch 后选定包含本批完成提交的 main revision，按冻结配方构建实际 Bundle/镜像，再
冻结最终 manifest 与宿主证据，先执行该候选预检再执行正式矩阵。不能将 05 receipt 的
revision 改成 06 候选，也不能以配方相同为由省略 06 当前候选预检。
