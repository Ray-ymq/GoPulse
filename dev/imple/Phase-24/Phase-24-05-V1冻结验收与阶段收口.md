# Phase-24-05：V1 冻结验收与阶段收口

> 状态：已完成（2026-10-14；冻结 `2.6.4` 产品候选的 runtime gate、preflight、formal V01～V12、清理及正式证据核验均通过）。完成版本 `2.6.5`，分支 `develop/2.6.5`。
> 实际产品候选：04 已完成的 `2.6.4`，完整 revision 与不可变 Bundle/manifest 在开工时记录。
> 实际结果：详见同名日志。正式 V01～V12、publication 和完成元数据均已按合同核验；不重标 `2.6.4` 产品候选或其回执。

## 1. 进入条件与允许文件

04 的页面、原生入口、协议 fixture、证据负例自测、短预检和同名日志已进入主远端 main。
先 fetch 主远端，从 main 创建本批分支。执行前确认所有扩展命令确已实现，
并核对当前 `2.6.4` 完成提交、消费启用修订、schema/current epoch/索引和共享预算。
任何前置缺口停止本批，不能在正式矩阵里开发工具。

| 允许修改 | 范围 |
| --- | --- |
| 实际结果 | 本分方案同名日志；`dev/status/capability-status.md`；本总/分方案实际结果标记，不能降低标准 |
| 发布摘要 | 拟新增 `dev/logs/Phase-24/Phase-24-05-evidence/`，只放 validator 白名单生成的 `summary.json` / `publication.json` |
| 成功完成元数据 | `VERSION`、`.env.example` 的产品版本字段、两个前端 `package.json` / `package-lock.json`、`deploy/runtime-contracts.json` 的产品版本字段 |
| 必要导航 | 阶段/设计索引及相关现有入口，只修真实链接，不复制版本、案例或状态 |

历史回执不在本批修改范围。此前正式入口缺少 V02～V12 driver；本次按用户明确要求，
在尚未启动新的正式矩阵前补齐 acceptance driver、生产配置接线、raw facts 校验和 fault fixture 自测，不降低固定案例、
证据、预算或清理标准。该实现提交后必须重新构建并绑定新的候选，不能复用旧候选或旧回执。
产品失败或工具需要修正时，保存 failed/not_started、原始证据和清理结果，
停止矩阵，按仓库规则另行明确有效修复合同后执行；不能改完候选继续用旧 receipt。

### 接续修复合同（2026-10-14）

本次 V07 停点已完成归属核对，接续修复允许修改的文件固定为：

| 归属 | 允许文件 | 固定范围 |
| --- | --- | --- |
| B6 / B7 | `backend/internal/worker/profile.go`、`backend/internal/worker/handler.go`、`backend/internal/worker/handler_test.go` | 仅补齐 Search Profile 对 V2 post 搜索临时故障的有界重试耗尽后自动重驱动；不改 retry profile、数据库 schema、epoch 准入、Outbox 身份或完成记录合同 |
| M8 | `acceptance/internal/scenario/v1_driver.go`、`acceptance/internal/receipt/v1.go`、对应 receipt / scenario 单测 | 仅修正 V07 子窗口边界、恢复截止和逐窗证据；不缩短故障窗口、不放宽恢复期限、不改旧回执或 raw |
| 其他 | 无 | 不修改 migration、公开 API、生产版本字段、旧候选、旧 receipt、旧 raw、旧 Compose 日志 |

产品修复的完成条件是：V2 search post.created / post.updated / post.deleted 的临时处理错误在有限 retry 耗尽后，仍以原始 body、MessageId、event identity、epoch 和 routing key 确认发布到 search retry exchange，并把下一轮尝试从 0 开始；原投递只在二次发布确认后 ack。重复完成仍由现有 `(epoch,event,consumer,routing contract)` 唯一完成记录去重，完成记录写入后才释放未闭合义务；永久错误继续进入 search dead exchange，业务 Profile 的耗尽行为不变。必要义务未闭合时不得清理其 Outbox / intent。

直接门禁固定为 backend worker 单测、acceptance receipt / scenario 单测、backend 与 acceptance 模块门禁，以及受影响的真实 business integration。通过后必须重新构建新的 candidate / Bundle / manifest，再按本文件的 preflight、Verify Evidence 和正式 V01～V07 入口执行；V07 两个窗口各自必须完成故障撤除、依赖健康、全部义务闭合和当前可见，单窗故障至少 60 秒、故障撤除起恢复最多 120 秒。任何一个窗口或 V01～V07 失败，后续案例保持 `not_started`。

### Runtime gate runner 修复合同（2026-10-14）

本次 runtime 停点的归属核对发现，`lifecycle/internal/packaging/verify.go` 原先按文件存在性复用未跟踪的 `acceptance/bin/gopulse-acceptance`，可能执行早于候选 revision 的验收逻辑。允许追加修改该文件及 `lifecycle/internal/packaging/verify_test.go`，范围仅限于每次 runtime gate 从当前 acceptance 源码构建临时 runner，并用回归测试证明过期的同名二进制不会被复用；不改变候选产品镜像、生命周期业务语义、正式案例、既有失败证据或历史 candidate / receipt / raw。

完成条件是：包装模块测试、vet、race、build 和固定 verify 通过，lint 相对基线无新增；旧 candidate 的失败摘要保持不变，修复后必须以新的 revision / Bundle / manifest 重新绑定 candidate，不能把局部重跑结果写成旧 candidate 的正式 runtime verification，也不能据此启动 preflight 或正式矩阵。

## 2. 候选、环境与预检

在独立干净 checkout 冻结完整 `2.6.4` revision。
记录 product/tool/fixture/contract/config/base-ref 的身份和校验值、全部生产 image digest、
Bundle 归档与 payload 各自 hash、manifest、schema target、epoch/generation、Compose、
各角色实例及有效限制。正式执行不替换同名 tag、manifest、配置或验收条件。
验收镜像/fixture 可以从该固定源码构建，不能偷偷取当前任务工作树助手。

目标为 Linux/amd64、Docker/Compose 和锁定依赖。实时预检记录 CPU/内存/交换区/磁盘、
Docker 版本、活动项目、依赖限制及端口；历史 transfer 证据只作参考。
磁盘可用至少 20 GiB，确认资源能启动既有产品栈；不足时停止，不把历史宿主视为当前机器。
一个完整产品栈串行运行；恢复目标启动前停止原栈，保留其 owned 事实/卷用于复核，
不得同时启动两个完整栈争用宿主资源。

MySQL 常驻 52、含维护/工具总额 60；API 128/128、平台 32，现有 Worker/Indexer
prefetch、retry、lease 与关停配置保持冻结值。新增诊断 2 槽/业务实例、平台 4 槽、
500 ms/2 s/3 s 期限实际接线；不增加 unaccounted pool。
V08 的低水位 profile 在冻结前登记：未闭合上限 8，保留组数 100,000、计费 1 GiB，
其余配置与正常 profile 一致；只证明水位行为，不声称低 profile 是默认容量。
各 profile 的实际配置/hash 和切换步骤预先冻结；切换时受控停止相关实例并核验重新启动的有效值，
不能在矩阵中临时调小限额来补过案例。

负载是有限功能配方：V02 同键 10 并发；其他普通创建串行。
每个 60 秒故障窗口在 t=0,5,…,55 秒发起一次普通请求，共 12 次，
使用不同新命令身份、有限标准正文及固定 sampled traceparent；
记录计划/实际发出、状态、ID、提交结果、工具取消/丢弃，不能只报告 HTTP 成功率。
低负载窗口不用于 QPS、尾延迟、观测开销或稳定运行认证。

先完成直接检查/fixture 自测，建立新 Bundle 和私有 registry，再跑同候选 preflight。
registry 必须覆盖后续独立验收拉取，直到验收/核验完成才按归属清理。
缺 manifest、不可拉 digest、清理/证据失败立即停，禁止回退源码模式。

2026-10-13 用户追加本次接续使用本地镜像：运行验收前对 manifest 指定的不可变
Linux/amd64 平台摘要核对本地镜像，摘要与平台均匹配时复用本地内容，避免每次
启动重复拉取。仅有同名 tag、错误平台或不匹配摘要不能满足此条件；缺失镜像仍须
按原摘要获取并校验，获取失败停止。制品的 metadata、Bundle、候选身份与正式证据
门禁继续有效。本地复用记录不证明当时远端可获取；工具修改提交后重新冻结候选。

## 3. V01～V12 固定案例

下面每个子项都必须执行并有原始依据。通常异步完成/可见最多 60 秒，
故障撤除后的恢复最多 120 秒；窗口不能延长到成功后再宣布通过。
响应未知、发布记录、应用完成、消费者 ack 和查询可见分别记录，
没有某项依据时保留未知，不拿日志推断它已经完成。

| case_id | 固定动作与通过条件 | 必要依据与期限 |
| --- | --- | --- |
| V01 正常与兼容 | 有键发帖完成命令→Outbox→search 应用→受限可见，管理端真实展示来源；无键旧请求仍 201，评论/通知及代表性业务继续工作 | HTTP/最小结果、真实 MySQL 四项、ES 实际代际/查询、管理浏览器及精确 Logs；异步各最多 60 s |
| V02 重复与冲突 | 两业务 API 同用户/epoch/键/摘要 10 并发，只有一个 command/post/event/义务；不同摘要 409；同键不同用户互不复用 | 原始请求台账、事务行数/唯一关联、返回 ID 和归属；不能只对比标题 |
| V03 创建结果丢失 | 分别丢提交后 HTTP 响应、丢真实 COMMIT 的返回结果；同键查证/重试返回原成功，无额外事实；原请求不能宣称确定未执行 | fixture 确认真实提交、代理控制时刻/连接、客户端未收结果、最小结果/计数；两子项独立留证 |
| V04 发布确认丢失 | Broker 已接受指定事件，publisher confirm 未到达，保留 pending/lease 或不确定发布依据；租约恢复/重投后必要 search 完成 | Broker/代理实际接收与丢 confirm 证据、owner/租约/发布记录、稳定 event_id、唯一完成；移除故障后最多 120 s |
| V05 外部效果与完成窗口 | ES 已写入指定投影但处理器尚未得到响应时停止 Indexer；另一个子项在 ES 成功后阻断完成落库并停止。重启/重投安全完成 | ES 实际索引/版本/可见，故障时无持久完成的查询，重投后正确记录；不能仅制造 500；恢复各最多 120 s |
| V06 消费确认丢失 | 完成已持久提交，原 consumer ack 丢失/连接断开造成重投；没有额外帖子/命令/事件，完成去重且下一次 ack 流程可终止 | 完成时间早于丢 ack、Broker redelivery、稳定身份/完成行数及原始连接/消息台账；最多 120 s |
| V07 ES 故障 | 分别执行 ES 不可用、请求延迟超过既有请求期限两个 60 s 子窗口；普通提交不等待 ES，新必要意图不按 published 清理；恢复后全部完成/可见 | 每窗 12 请求、业务事实与发布/未闭合义务、重试/恢复时刻、ES/实例状态；恢复最多 120 s |
| V08 水位与查证预算 | 低水位 profile 阻断 search，使 8 个真实义务占满，再并发争取新创建：固定 503 且无部分写入；已提交重放/普通读取可用。恢复后计数释放，新的命令可提交 | 原子配额/真实计数、拒绝/重放/读取台账；未完成意图保留；恢复最多 120 s。诊断限额另用同候选 Go 阻塞测试+部署接线证明 |
| V09 观测出口故障 | OTLP/Collector、Monitor→Router 日志接入、可观测 Logs ES 分别不可用/阻塞 60 s；每窗 12 次普通业务提交和必要副作用继续完成；缺遥测质量准确 | 独立业务响应/事实/完成/确认依据，出口失败与有界队列/重试/丢弃依据；恢复最多 120 s，不以“没有 Span”代替业务证明 |
| V10 平台单侧停止与权限 | platform-api 停止完整 60 s，已有 Cookie 继续发帖/查证本人结果/搜索，管理查询失败保留事实；恢复后管理员查证。同 Cookie 升降级、无会话/普通用户/错误私网凭证/公开 internal 拒绝 | 12 请求、两前端同源路由、实时角色和拒绝结果；平台恢复最多 60 s；不得把平台故障说成共享 MySQL 隔离 |
| V11 代际与恢复 | 同一数据 epoch 重建后旧 generation 完成不冒充新完成；当前可见独立查证，再重投记录实际新代际。按同 Bundle 排空备份恢复到空 owned 目标，建立新 epoch；重送旧事件/旧 header/旧游标并注入旧缓存键不得混入 | 原/新 epoch、alias/index metadata、完成作用域、journal 原事实 hash 与后置准备、旧事件停放；旧事件不改变当前投影版本/序列。恢复最多 15 min，失败必须 pending/非 ready |
| V12 取消、清理与证据 | 取消一个受控验收子任务，子回执如实保存执行/未开始且有限恢复/归属清理；父场景验证后继续。最终完整矩阵清理、原始 hash 和实际 publication 校验通过 | 子回执保持 canceled/incomplete，不改成 completed；父案例验证预期取消。清理各最多 5 min，非归属资源前后不变 |

V03～V06 的故障通过 acceptance 私网协议/HTTP 代理控制真实 COMMIT、AMQP confirm/ack、ES effect 和完成落库窗口，无生产后门。
V11 停止源栈后启动恢复目标，保留独立 owner 与来源关系，清理两者都要核对所有权。
旧事件拒绝须查当前索引序列/版本及新 epoch 完成表，正文 hash 相同不足以证明未发生旧写入。
恢复事实重建不制造旧命令在新 epoch 的完成结果。

V08 的限额/取消层次复用同候选 Go 阻塞 handler/client 测试，最多 10 秒：
实际 2/4 槽满后拒绝，普通业务和探针能进展，取消后槽位释放；
真实部署另外核验实际配置和来源实例。两层缺一不能通过，短测试不当作实际容量测量。
7 日留存/清理和另外两类水位硬限由同候选时间控制/真实 MySQL 直接测试证明，
不伪造正式时间戳或等待七天。

V09 同时绑定同候选日志/Tracing 的确定性饱和、取消、重试和关停测试，以及真实出口故障。
SDK 内部未提供的瞬时队列/丢弃计数注明不可观测；配置上限、饱和测试与真实失败/终止
分别留证，不能称测得不可观测计数或依赖缺失 Span 推算精确丢弃。
测试层的缓冲上限证明不能替代真实窗口内的提交、应用完成和消息确认依据。
Trace 结果按受控采样/文件输出范围解释，不声称公共 Trace 查询或全站覆盖。

任何案例的结果、来源、期限、身份或清理不符即 failed；前置失败的后续项记 not_started。
工具/身份/原始数据/清理错误为 infrastructure_failure，不产生产品能力结论；
真实行为不符为 product_failure，未知根因仍如实保留，不能用短诊断改写正式失败。

## 4. 固定命令与证据核验

下面 V1 扩展已在本次正式矩阵启动前补齐：formal runner 负责候选栈上的 native V02～V12 动作、
受限持久事实、协议故障控制、V08 有效配置、V11 lifecycle hand-off、V12 子回执和每案例 raw 写入；
V01 仍由浏览器基线负责。重新执行前必须从包含该实现的 revision
构建新的 Bundle/manifest/镜像，并以新的 receipt 绑定它。
在冻结 checkout 的仓库根运行，PHASE24_* 均为登记的私有绝对路径；
BASE_REF 固定到规划/开发检查使用的具体 revision，不在矩阵中漂移。

```bash
rtk proxy make verify MODULE=backend BASE_REF="$PHASE24_BASE_REF"
rtk proxy make verify MODULE=acceptance BASE_REF="$PHASE24_BASE_REF"
rtk proxy make verify MODULE=lifecycle BASE_REF="$PHASE24_BASE_REF"
rtk proxy make check-compose
rtk proxy make package PLATFORM=linux/amd64 OUTPUT="$PHASE24_CANDIDATE_DIR" REGISTRY="$PHASE24_REGISTRY" RUNTIME=1
rtk proxy make verify-business SCOPE=v1 MANIFEST="$PHASE24_MANIFEST" RECEIPT="$PHASE24_PREFLIGHT_RECEIPT" PREFLIGHT=1 BUDGET=30m
rtk proxy make verify-business SCOPE=v1 MANIFEST="$PHASE24_MANIFEST" RECEIPT="$PHASE24_PREFLIGHT_RECEIPT" VERIFY_EVIDENCE=1
rtk proxy make verify-business SCOPE=v1 MANIFEST="$PHASE24_MANIFEST" RECEIPT="$PHASE24_RECEIPT" BUDGET="$PHASE24_REMAINING_BUDGET"
rtk proxy make verify-business SCOPE=v1 MANIFEST="$PHASE24_MANIFEST" RECEIPT="$PHASE24_RECEIPT" VERIFY_EVIDENCE=1 PUBLICATION="$PHASE24_PUBLICATION"
```

直接检查有效且相关源码/依赖/配置/身份未变时引用原记录，不无条件重跑；
需要补跑的检查及原因在日志登记。package 的既有 runtime 检查不代替 V01～V12。
V11 的空目标 lifecycle/backup/restore hand-off 已接入当前 driver，并额外核验新 epoch、旧事件拒绝、
新 ES generation 和旧事件在新 epoch 中没有完成记录；正式执行仍必须绑定包含该接线的新候选。

原始证据保留 HTTP 台账、最小事实/义务/完成查询、Broker/代理时序、实际 ES 结果、
来源质量、所需 Go 输出、恢复 journal 的安全摘要、资源前后清单和关键 hash。
private raw 不入仓库。validator 核验全部 case/subcase、窗口/恢复期限、源数据校验值、
候选一致性、配额与业务计数、清理/隔离，再生成白名单摘要并再次核验实际选定 publication。
被发布的文件必须与实际选择的源 receipt/raw 对应，不用旧 transfer 摘要充数。

现有入口不提供 case-level resume。本阶段同样不手工拼接半轮矩阵；
失败后重试要新建正式目录/项目，仍沿用同一累计成本。
同身份未变的直接检查可复用，已失败的真实窗口不能改成成功或移交给另一个候选。

## 5. 有限预算、停点与完成

预计总活跃成本 90 分钟，跨构建、预检、正式运行、诊断、核验、清理及接续
累计上限 180 分钟。子进程共用剩余预算，开长步骤前预留清理时间。

| 部分 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 实时环境/直接检查/fixture 与身份 | 15 | 30 |
| 缓存构建、Bundle、短 preflight | 25 | 50 |
| 一次正式 V01～V12 含恢复 | 30 | 60 |
| 核验/脱敏、有限诊断 | 10 | 20 |
| 归属清理、记录及完成检查 | 10 | 20 |
| 合计 | 90 | 180 |

不可压缩故障观察为 V07 两窗、V09 三窗、V10 一窗，合计 6 × 60 s；
其他丢结果/确认案例使用确定失败点，不虚构长窗口。
一次正式项目，另计一次短 preflight；V11 一个空恢复目标与已停止源栈串行，
启动各最多 7 min、恢复最多 15 min、归属清理最多 5 min。
最多两次各 10 min 最小诊断，计入上表；基础设施失败立即停整组。
应用/工具需要修改、诊断无法推进、剩余不够执行并清理、累计触顶或用户停止时，
保存实际通过/失败/未开始、候选/剩余工作及累计成本，有界清理后停止。
外部 package/远端等待未由本工具统一计时的部分人工累计，不把新 receipt 当预算重置。

成功后在任务分支同步完成元数据和同名日志，保持冻结 checkout/旧回执原样。
完成治理检查：

```bash
rtk proxy python3 ci/sync_version_metadata.py --version 2.6.5
rtk proxy python3 ci/validate_versions.py
rtk proxy python3 ci/validate_branch.py --branch develop/2.6.5 --base-ref upstream/main --mode completion
rtk proxy make docs-check BASE_REF=upstream/main
rtk git diff --check
```

VERSION 只在全部正式门禁/清理/证据通过后写入 `2.6.5`；
sync_version_metadata 只同步既有登记字段。按仓库规则提交并等待相应必需远端质量结果。
该工具当前覆盖 VERSION、环境版本及前端 package/lock；runtime-contracts 的 product_version
须同批显式同步并核验，不能假设工具已经覆盖。
日志、状态和 publication 都区分“2.6.4 产品候选被测”和“2.6.5 执行收口”；
不声称重新标记的 2.6.5 Bundle 已执行该矩阵。

阶段完成只声明总方案 V1 范围通过。独立 dispatcher、多目标采集、无跨系统持锁的搜索、
新重建/删除屏障、任意时点恢复、完整 A1～A12、容量和长期稳定仍按已有边界登记。
任一固定项未完成不能作阶段完成提交。
