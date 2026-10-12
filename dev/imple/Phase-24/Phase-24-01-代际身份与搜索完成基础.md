# Phase-24-01：代际身份与搜索完成基础

> 状态：已完成。目标版本 `2.6.1`，分支 `develop/2.6.1`；实际结果见[实施日志](../../logs/Phase-24/Phase-24-01-代际身份与搜索完成基础.md)。
> 结果：新消息生产启用前，业务代际、兼容消费者、搜索完成依据、清理屏障及恢复准备已经可用。
> 分配、共同参数和范围见[总方案](Phase-24-总实施方案.md)；本批正常生产方仍输出 v1。

本批已完成 migration 14、代际/完成证据持久结构、v1 兼容消费准入、v2 fixture 与恢复准备、实际搜索 generation 校验、留存屏障及 lifecycle 接线；固定后端、lifecycle、devtools、business integration、Compose 和文档门禁均通过。生产仍为 `legacy_v1`；发帖 v2 生产与命令幂等属于 02，状态接口/管理页面属于 03/04，完整 V1 冻结矩阵属于 05，均不在本批完成声明内。

## 1. 进入条件与允许文件

Phase 24 规划已进入主远端 main，Phase-23-06 完成事实可定位。
先 fetch 主远端，再从 main 创建本批分支；核对实际 schema target 为 13、数据库状态 clean/current，
以及现有搜索编辑、删除、重建保护。dirty/ahead 不自动修复。

| 允许修改 | 文件/目录与具体目的 |
| --- | --- |
| 持久结构 | 拟新增 `backend/migrations/000014_business_stage_evidence.up.sql` / `.down.sql`；该目录现有 migration validation、embed 和真实 MySQL 测试 |
| 领域身份与记录 | 拟新增 `backend/internal/businessstate/`，只负责 epoch、消费义务、应用/失败摘要、水位、停放及本批 Repository 测试；命令表访问行为留给 02 |
| 消息/投递/消费 | `backend/internal/bus/`、`outbox/`、`worker/`、`notification/processor.go` 及对应测试；兼容解码、epoch 校验、完成屏障和确认顺序 |
| 搜索 | `backend/internal/search/`、`backend/cmd/search-indexer/`、`backend/cmd/search-reindex/`；实际代际、完成持久化、旧 epoch 拒绝及已有竞态回归 |
| 装配与缓存隔离 | `backend/cmd/server/roles.go` 及角色测试、`backend/cmd/business-worker/`、`backend/internal/config/`、`backend/internal/post/cache.go`、`backend/internal/platform/{redis,runtime_schema}.go`、`backend/internal/search/service.go` 及相应测试 |
| 恢复/部署与测试接线 | `lifecycle/internal/control/recovery.go`、`data.go`、`state.go` 及测试；`deploy/docker/backend.Dockerfile`、`deploy/compose.yaml`、`deploy/runtime-contracts.json` / `.schema.json`、`.env.example`；`devtools/internal/{testenv,devrun,envfile}/` 仅补代际初始化与配置，不改编排模型 |
| 合同与使用正文 | 拟新增 `dev/contracts/business-stages.md`；`dev/contracts/README.md`、`migration-state.md`、`runtime-contracts.md`、`backup-format-v1.md`、`backend/README.md`、`deploy/release/BUNDLE-README.md` 中直接相关内容 |
| 实际结果与完成元数据 | 同名日志、能力状态、本总/分方案结果标记；成功后按总方案统一完成规则同步版本元数据 |

不改三份冻结设计、历史回执或既有 migration 1～13 的 SQL。
目录授权只涵盖上表行为及直接测试；新模块、共享库发布、独立 dispatcher 和新重建算法不在本批。

## 2. 持久结构与身份规则

migration 14 至少承接以下领域数据，实际索引/约束写入新合同并由真实 MySQL 测试证明：

| 数据 | 必需内容与唯一/查询范围 |
| --- | --- |
| `business_environment` | 单一当前 epoch、ready/restoring 状态、legacy_v1/v2 模式、v1 兼容许可与启用修订；初始化一次，普通重启不换 UUID |
| `business_commands` | epoch、command_id、user_id、command_type、nullable key hash、request digest、post/event ID、成功状态、提交/保留时间；唯一键覆盖总方案作用域；不以 posts 外键级联删除命令结果 |
| `business_event_requirements` | epoch/event/consumer/路由修订、适用对象版本和闭合时间；与未来新发帖 Outbox 同事务写入 |
| `business_application_results` | epoch/event/consumer/路由修订/真实 generation 唯一；结果、实际版本、完成时间；不能仅按 event_id 去重 |
| 失败与停放 | 每个事件/消费者一份有限尝试计数、最近原因/时间和 dead/retry 摘要；未知或旧代际原 payload/来源的持久停放，禁止无限追加尝试明细 |
| `business_async_budget` | 按 epoch 的未闭合义务数，跨所有 epoch 的 V1 全局保留组数/计费字节；增减与领域事务原子提交，初始化/校准有维护入口；恢复换 epoch 不释放历史保留预算 |

记录不存标题/正文、登录凭据或未经清洗的依赖原始错误。
停放 payload 继续沿用 16 KiB 消息上限，最多 10,000 条/160 MiB 逻辑 payload；
容量不足时保留 Broker 原消息且不确认，不静默丢弃。
建表不是历史回填：旧命令/完成未知，旧 Outbox 和 notification 的既有唯一语义保持。
v2 发帖缺少相应持久义务/来源且无法核实已过重放窗口时，也按无依据停放，
不临时补造命令或应用义务；DB 查询失败则按暂时失败重试。

Envelope v2 支持现有六类事件，epoch 必填；post.created 还要求 command_id 和正 content_revision。
v1 继续按原 shape 严格解码，并显式分类 legacy_unknown；未批准的新字段/版本仍拒绝。
新版本编码能力通过 fixture 测试，本批生产路径禁止发送 v2。
消费者处理前核对持久当前 epoch/ready；不以缓存的旧 epoch 或新标签接受旧消息。
epoch 不符及未知必要意图只有持久停放确认后才能处置原消息。

## 3. 搜索完成与清理屏障

复用现有 MySQLDocumentStore、Processor、ElasticsearchRepository、Worker ack/retry 流程。
v2 post.created 在当前事实基础上应用投影；保留 ES I/O 期间的事实锁和现有重建/删除协调。
不修改物理删除生命周期或用补充完成表代替删除屏障。

1. 当前 epoch 的索引在 strict mapping 中保存 epoch，index metadata 同样标识 epoch；创建/重建生成新的物理 generation。
2. ES 写入返回实际 `_index`、文档 ID/版本等可验证结果；检查 index epoch，成功后记录实际 generation。不能在成功后再解析 alias 并猜测刚才写到哪里。
3. 索引 409 必须校验版本冲突类型及实际存储的 epoch、版本和规范投影；未经核实不能当作完成。高版本/错误 generation 也不能以“ES 已有数据”直接覆盖事实依据。
4. 缺失事实按现有删除协调确认不产生正文投影，删除副作用成功后记录 `no_effect_resource_absent`。这证明此次无须正文投影，不证明所有代际永远不可见。
5. 外部副作用成功、完成记录提交失败时返回可重试失败。再次投递以同事件执行安全投影并落库；完成提交成功后处理器才允许 Ack。
6. 正确代际已完成时可复用记录；代际变化仍检查新代际投影。重建扫描不伪造任何事件的应用记录。

Outbox 的 claim/lease/owner、发布事务与网络发布分离不变。
新义务存在时，`CleanupPublished` 只有在全部必要消费者闭合且再保留 7 日后才删除；
命令和完成引用同时受屏障保护。清理按已有 500 条/小时入口、有限事务执行。
重复完成只释放一次未闭合计数；清理只释放本次实际删除的组/字节，失败整体回滚。
legacy 事件明确沿用历史留存范围，不能把它们解释为已满足新完成合同。

## 4. 兼容部署与恢复准备

新 SQL 与新二进制 target 必须一致；采用受控停机迁移。
停止 API、Worker、Indexer，运行新 migration validate/status/up，启动兼容消费者，
在既有 search-reindex 入口为当前事实构建带 epoch 的索引，核验后启动 API。
本批生产仍为 legacy_v1 模式，v1 不产生新应用完成承诺；02 才启用 v2 生产与新恢复协议。
旧二进制不可直接回退到 schema 14；回退使用兼容构建或原有备份恢复，不使用 migration down。

扩展现有 search-reindex 维护入口承接“恢复代际准备”，参数包含 restore operation、
expected old epoch 和预先生成的新 epoch，校验严格，非 HTTP 故障接口。
lifecycle 在 journal 内持久记录同一 operation/new epoch，接续不得重新随机生成。
顺序固定为：

1. 按原流程导入并完成 MySQL/搜索原字节、数量及 hash 校验。
2. 在任何产品应用启动前设置 restoring，持久新 epoch，禁用未携带 epoch 的历史消息执行。
3. 帖子缓存使用新 epoch 键空间；搜索游标绑定 epoch/generation。旧缓存回填和旧游标不能混入恢复后的查询。
4. 在原有协调下从恢复事实重建当前 epoch 的新索引，检查真实 alias；保留旧完成的历史作用域。
5. 验证旧 epoch 拒绝、正常新读写前置和恢复结果，才设置 ready、记录 journal 并启动产品。

上述新准备分支在 v2 模式失败时 restore-pending 保留、ready 不成立；
本批以 v2 fixture 直接验证，生产 legacy_v1 恢复仍沿用原排空合同，不绕用新完成声明。
备份增加已启用新必要义务全部闭合检查，
不能只凭“没有 unpublished Outbox”推断可安全备份。
本批只扩展既有同 Bundle、已排空备份流程；不声称跨版本/任意时点恢复完成。

## 5. 已有覆盖与新增直接检查

| 对象 | 已有覆盖与缺口 | 最低有效层及验收条件 |
| --- | --- | --- |
| migration/消息 | migration CLI dirty/ahead/锁；bus strict v1；缺新约束和双版本 | Go + 真实 MySQL：v14 up/repeat、约束/索引、旧 target ahead 拒绝、v1/v2 合法与畸形消息 |
| 外部写入/完成 | search edit/delete/PIT、Worker retry；缺 ES→完成→ack 窗口 | Go 确定性失败点 + 真实 MySQL/ES/RabbitMQ：写入后失败重投、完成后 Ack 丢失、记录一次、实际 generation、409 不误判 |
| 留存/水位 | Outbox owner/cleanup；缺 published 未完成保留 | 真实 MySQL：未完成保留、全部完成/时间届满才清理、并发首次完成/清理计数不重放、不下溢 |
| epoch/恢复 | lifecycle 导入 hash/归属；缺后置新代际准备 | lifecycle Go journal 顺序/故障接续 + backend 真实 MySQL/ES 恢复准备 integration：已恢复事实 fixture 不变，再换 epoch/重建；错误保留 pending，旧消息/缓存/游标被拒 |
| 现有正确性 | edit/delete/reindex 既有 integration | 原用例继续通过；新增代际不能破坏协调或绕过 MySQL 过滤 |

失败点通过测试内依赖替身、既有 fixture 或进程停止控制实现；不新增产品后门。
恢复准备的真依赖用例复用 backend 既有 integration 环境与领域入口；
lifecycle 测试证明原 hash 核验到准备/启动的调用次序和 journal 接续。
两层都要通过，本批不把它们声称为完整 Bundle 恢复；后者在 05 的 V11 执行。

## 6. 固定门禁、预算与完成

工作目录为仓库根。基础检查使用已有共同模块入口，包含 check/lint/test/race/build；
lint 遵从现有 baseline 模式，不能将存量告警表述为严格零告警。

```bash
rtk proxy make verify MODULE=backend BASE_REF=upstream/main
rtk proxy make verify MODULE=lifecycle BASE_REF=upstream/main
rtk proxy make verify MODULE=devtools BASE_REF=upstream/main
rtk proxy make integration SCOPE=business
rtk proxy make check-compose
rtk proxy make docs-check BASE_REF=upstream/main
```

上述命令为现有入口；本批新增直接用例必须由对应入口实际选择并执行。
同候选恢复准备预检由上述 Go/真实 integration 新用例执行，原始结果绑定源码和配置，
不能用纯 mock 代替真实数据代际/索引重建；不足时先补本批用例，不借用尚未实现的 V1 正式入口。

真实验证预计 60 分钟、累计上限 120 分钟：准备/必要镜像 25/50、integration 与短恢复 20/40、
有限诊断及归属清理 15/30。一次只建立一个 owned 项目；每次就绪最多 7 分钟、
恢复短预检最多 15 分钟、清理最多 5 分钟。最多两次各 10 分钟最小诊断，计入上限；
达到上限或基础设施失败，按总方案执行停止与接续，不延长窗口等成功。

完成需全部固定门禁和上表直接检查通过，新消费者/完成屏障实际可用，生产仍为 v1，
恢复失败停点及清理有证据；记录同名日志、实际合同与能力边界。
成功后同步 `2.6.1` 元数据，运行版本/分支 completion 校验并提交。
不把结构就绪写成发帖幂等、平台查证或完整 V1 已交付。
