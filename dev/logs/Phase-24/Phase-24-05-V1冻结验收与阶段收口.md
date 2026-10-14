# Phase-24-05：V1 冻结验收与阶段收口

> 状态：未完成（更新于 2026-10-14）。历史候选的正式矩阵在 V07 暴露真实产品失败：24 个请求中只有 21 个 search application 完成；接续修复候选 `6dc98f6…` 的 runtime gate 又在 lifecycle `up` 的 `services` 阶段发生基础设施失败（code 18），因此 preflight 和正式矩阵尚未开始。仍不产生 `validation_complete` 或阶段完成结论。下文按执行时间保留各次实际结果。
> 执行分支：`develop/2.6.5`；候选产品版本：`2.6.4`。

## 实际执行范围

- 从刷新后的 `origin/main` 创建 `develop/2.6.5`，规划基线为 `bd777ecd7ffff5f10171f0a9556ff93ea6b6ff65`。针对首次验收暴露的 fixture 权限问题，按用户后续修复要求提交 `701e4ff5e2ad4d77e55c23fb2bab361d183346f8`；该修复只改变 acceptance lifecycle direct fixture，不改变产品运行时代码。
- 宿主满足合同：Linux `amd64`，8 CPU，Docker Engine `29.7.2`，Compose `v5.5.0`，Docker 可用内存约 `12.67 GiB`，swap `16 GiB`，磁盘可用超过合同下限。每次 acceptance/lifecycle 项目均按 owner 清理。
- 直接门禁通过：backend、acceptance、lifecycle 的 verify（存量 lint 分别 107、24、46，新增均为 0，含 test/race/vet/build）和 `make check-compose`。修复后 acceptance 单测包含低磁盘 direct 参数身份断言。

## 首次失败与修复

- 首次候选目录 `.run/phase24-05/candidate-bd777ec-v1` 为 `0700`。failure matrix 的 low-disk direct 分支使用 `0:0`、`--cap-drop ALL` 和 `no-new-privileges`，容器无法读取只读挂载的 `/bundle/release-manifest.json`，实际返回 code `10`（manifest），没有进入预期的 code `13`（disk）。该失败属于验收 fixture 权限边界，不是产品失败。
- 修复后 direct 分支使用验收工具自身 UID/GID，并将 `1 MiB` 私有 tmpfs 显式设置为相同 UID/GID；单测验证生成参数。对修复后候选的精确 low-disk doctor 复现返回 `{"code":13,"stage":"disk"}`。

## 修复后候选与 runtime

- 候选目录：`.run/phase24-05/candidate-701e4ff-v1`，目录权限 `0700`，未提交。版本 `2.6.4`，候选 revision `701e4ff5e2ad4d77e55c23fb2bab361d183346f8`。
- `release-manifest.json` SHA-256：`sha256:10463bb291294369caf18171c8e3cdf173e9c99bf8d83fc2d1dcb2c23a76baa6`；Bundle archive SHA-256：`sha256:bfb964437faffa17beb7266cb9d1e5fc9ceea20584d5219439b1f1da1d575434`；manifest payload：`sha256:ea25f43ae22bc4335b849a661138c02498a34d6cdb12ea57b23477d994769443`；`checksums` SHA-256：`sha256:dc15d02c31ebb436eb28f796d08f8c3da1971d00c941d0563ac696b8aecc9347`；runtime contract SHA-256：`sha256:475127e5aedbbf450ada59574854c4d2187218373f050371128ac63412f25536`。
- registry 为本批自有 `gopulse-phase24-05-registry`，namespace `127.0.0.1:15025/gopulse`，镜像固定为 `registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373`。候选 metadata、third-party digest 和 image digest 检查通过。
- 首次 `make package ... RUNTIME=1` 在候选已生成后遭遇外部 MySQL registry index 瞬时不可用；未改候选。随后对同一候选执行 `gopulse-package verify --runtime`，Compose smoke/observability、lifecycle clean 和完整 failure matrix 均通过，`verification-amd64.json` 为 `amd64-runtime-and-compose-passed`。

## V1 状态与停点

- `PREFLIGHT=1` 已通过：回执 `.run/phase24-05/preflight-receipt-701e4ff-v1.json` 为 `status=passed`、`mode=preflight`、候选 revision 为 `701e4ff5e2ad4d77e55c23fb2bab361d183346f8`，V01 为 `passed`，raw SHA-256 为 `sha256:bcc3d09eba8536a2b6e7a6accc06d4b6dc0f892bd55f6fcc9bf6c04583a4efc1`，顶层和 V01 cleanup 均通过。
- preflight 的 `VERIFY_EVIDENCE=1` 已通过；它只核验 preflight receipt/raw，不等价于正式矩阵。
- 首次正式入口探测未执行矩阵：当时 `acceptance/internal/scenario/v1.go` 的 formal 分支在 V01 baseline 后明确返回 `formal V1 matrix is incomplete: Phase-24-05 actions remain not_started`，因此 V02～V12 没有被误标为通过，也没有拼接旧回执。
- 后续实施已补齐 `acceptance/internal/scenario/v1_driver.go`：V02～V12 使用候选栈上的原生 HTTP、Compose 服务窗口和 MySQL 最小持久事实查询；`receipt` 增加固定名称的非负 numeric facts 与私有 raw 原子写入/校验；response-loss fixture 只丢真实 201 发帖响应，fixture 自测覆盖冲突和非发帖响应不被丢弃。driver 对没有真实提交身份的案例保持 `not_started`，对已有身份但缺少确定故障控制的案例记录 `failed/infrastructure_failure`，前置失败后的后续案例保持 `not_started`。
- 本次源码实施已通过 acceptance `go test ./...`、`go vet ./...`、模块 test/race/check，以及相对基线新增 lint 为 0；这些是工具自测，不是冻结候选正式验收。
- 因此正式 V01～V12、正式 receipt、publication 和 `2.6.5` 完成元数据均未产生；`VERSION`、环境版本、前端 package/lock、runtime contract 产品版本仍为 `2.6.4`。这次 preflight/runtime 结果不声明完整 V1 能力达标。

## 清理、证据与后续

- 本批 acceptance/lifecycle 项目、容器、网络和卷已按归属清理；registry 容器和本批候选镜像标签也已显式清理，没有使用全局 prune。候选 Bundle、manifest、preflight raw 和私有失败/修复摘要保留在 `.run/phase24-05/` 供复核，不入 Git。
- 首次失败证据仍保留在 `.run/phase24-05/failure-evidence.json`；修复后 runtime/preflight 事实摘要保留在 `.run/phase24-05/repair-evidence.json`。未以新 receipt 重置正式预算。
- 下一步必须从包含上述实现的提交重新构建 Bundle/manifest/镜像，建立新的正式目录和 receipt，再执行 formal 与 Verify Evidence；旧候选 `701e4ff...` 的 preflight 不能复用于新工具，也不能把本次 preflight 或 runtime gate 改称阶段完成。此前记录的 V04/V05/V06/V08/V11/V12 控制缺口已在本轮源码中补齐，仍需新候选实际运行验证。

## 本轮补强实现（2026-10-13）

- V03 新增 acceptance 私网 MySQL packet proxy。`commit-result` 模式只丢真实成功 `COMMIT` 返回包并关闭该业务数据库连接；既有 HTTP response-loss 子项仍独立运行。回执同时记录上游 effect、丢包、断连、重试和 MySQL 最小持久事实，未把 HTTP 丢响应当作 COMMIT 结果证明。
- V04/V06 新增 raw AMQP 0-9-1 proxy。发布侧解析真实 Basic.Publish 内容和 publisher confirm，消费侧解析指定 Search Indexer 队列的 Basic.Deliver、完成后的 Basic.Ack、连接断开和 redelivery；只对指定事件丢一次 confirm/ack，恢复后核对稳定 event_id 和唯一完成。
- V05 新增 ES HTTP effect proxy 与 MySQL completion-write 控制。两个子项在 effect/响应丢失后立即停止 Indexer，分别核对投影存在、完成记录不存在，再移除代理并重投；第二个子项单独阻断完成 INSERT 的成功响应。
- V08 将 `BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS` 接入 Backend、Worker、Indexer、Reindex 和 runtime contract，默认保持 `10000`，formal profile 设为 `8`，由八个真实创建请求占满后检查第九个 503 和真实计数释放；配置、默认值、边界和 Repository limit 均有单测。
- V11 通过已有 lifecycle drained backup、空 owned restore target 和 `search-reindex` hand-off，补充新 epoch、旧 epoch admission rejection、新 alias generation，以及旧事件在新 epoch 中无 completion row 的独立 SQL fence。V12 启动真实 stage/fact polling 子任务，取消后写入私有 canceled child receipt 并删除其归属临时资源；receipt verifier 校验子回执 hash、身份、时序和私有权限。
- 本轮自测已通过 `cd acceptance && go test ./...`、Backend 受影响包测试和 `make check-compose`；formal V01～V12 尚未绑定新 Bundle 执行，旧候选 receipt 不适用于本轮源码/合同变更，阶段仍未完成。

## 接续验收与本地镜像核对（2026-10-13）

- 候选 `.run/phase24-05/candidate-644c0e3-v1` 完成 package runtime/Compose/lifecycle 门禁；同候选 preflight 和其 Verify Evidence 通过。正式回执 `.run/phase24-05/receipt-644c0e3-v1.json` 在 V03 MySQL 协议代理启动后读取控制端口时失败，Docker 输出 `{"19125/tcp":null}`；该回执及原始证据继续保留，不能据此声明 COMMIT 故障子项通过。
- `d3dafbb9edea9ccfb290d3eb9c468e070ff0070d` 为控制端口加入 10 秒有界等待，acceptance 模块测试通过。该变化尚未由正式矩阵验证；端口返回 null 的底层原因仍需最小复现，不能将竞态推测记成已证实根因。
- 新候选 `.run/phase24-05/candidate-d3dafbb-v1` 在 lifecycle failure matrix 中提前返回 `cannot pull manifest digest for elasticsearch`，实际 code 10，而测试预期进入注入故障后的 code 18；没有生成成功 runtime 回执。Docker 日志随后有同一 Elasticsearch 平台摘要成功拉取记录；原失败的底层 stderr 被 lifecycle 抑制，不能确定为某个具体网络错误。
- 用户明确要求本次使用本地镜像，并指定 `gpt-5.6-luna`、推理强度 `max` 子 agent，由主 agent 调度。CLI 实际版本为 `0.162.1`，该模型已成功执行只读子任务；子 agent 核对上述新候选的 16 个固定 Linux/amd64 镜像，结果为全部存在、无缺失、无平台或 RepoDigest 不匹配。核对使用精确平台摘要的 `docker image inspect --platform linux/amd64`，未启动产品栈或拉取镜像。
- 本地镜像复用已接入 acceptance、lifecycle up/restore 与 package runtime probe：精确平台摘要和 RepoDigest 匹配才复用；仅明确缺失才拉取，拉取后再次校验。平台、摘要、元数据或 Docker daemon 异常直接失败；RepoDigest 比较规范化 tag，同时保留 registry 端口和仓库归属。acceptance/lifecycle 模块测试通过，覆盖缓存命中、缺失、错误平台/摘要及拉取后复核。
- V03 最小诊断未复现原端口 null，原代理已删除，仍无法确证根因。失败路径现于恢复/删除代理之前通过 harness 私有失败证据记录 `.State`、端口映射和有界日志，不输出完整容器 Env。预冻结审查还修复 ES effect proxy upstream 缺少 `http://` 的确定启动缺陷，参数测试通过。
- 接续修订的完整直接门禁通过：`make verify MODULE=acceptance BASE_REF=upstream/main` 与 `make verify MODULE=lifecycle BASE_REF=upstream/main`（含 vet、test、race、build）；新增 lint 均为 0，存量分别 24、45。新增 fixture 的关闭操作明确为 best-effort，测试清理检查错误，确认无调用后删除旧 `runBoundary` 助手；未放宽正式案例。文档治理检查通过。
- 上述工具修改提交后必须重新冻结候选，历史 manifest/回执保持原身份；正式矩阵、publication 与完成元数据仍未通过。此前跨构建、失败和诊断的成本继续累计，本次记录不表示重置原预算。
- 本地复用修订已提交为 `8c22f8d`，native acceptance 已重新构建；该提交尚无新候选真实验收。预算复核对现存顶层 24 份 receipt/preflight 的命令耗时去重，得到可确认活跃下限 `2,086,649 ms`（34 分 46.649 秒）；起止区间并集为 43 分 53.075 秒，不能把区间内等待全部当作硬下限。10 份 package verification 没有耗时字段，跨构建、远端等待与人工成本仍未知，无法证明原 180 分钟上限的剩余量，故未启动新的完整栈。接续建议为最多 60 分钟：新候选构建/runtime 25 分钟、preflight/核验 5 分钟、formal 25 分钟、清理/记录 5 分钟；任一步失败立即停止，不追加诊断或重试、不延长固定案例窗口。此窗口须用户明确批准预算调整，不能视作原累计成本重置。

## 批准接续执行结果（2026-10-13）

- 用户批准最多 60 分钟接续窗口；历史 Phase24 累计预算未重置。执行前确认 `HEAD=63eb3f5bdf499135869671d1d3b9ca8edfef2aeb`，其父提交为 `8c22f8d`，并保留日志提交 `63eb3f5`。tracked 工作树无修改；未跟踪的已重建 `acceptance/bin/gopulse-acceptance` 未被修改或删除，`VERSION` 仍为 `2.6.4`。
- 从该 HEAD 新建候选 `.run/phase24-05/candidate-63eb3f5-v1`，registry 为本轮独立 `127.0.0.1:15027/gopulse`，使用本地已核验的 pinned `registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373`。候选 manifest 绑定 revision `63eb3f5bdf499135869671d1d3b9ca8edfef2aeb`，manifest SHA-256 为 `sha256:e7dbb316c2966142231d7b747bc80bcddae56b39b7bf1e3cad3d540c6db61148`，Bundle archive SHA-256 为 `sha256:faddb5e39ab01bf153aafa103714a7b4ef253b9d3f0f76d002c474638f469f80`。
- `make package PLATFORM=linux/amd64 OUTPUT=.run/phase24-05/candidate-63eb3f5-v1 REGISTRY=127.0.0.1:15027/gopulse RUNTIME=1` 在 `2026-10-13T20:09:49+08:00` 至 `2026-10-13T20:16:55+08:00` 执行。10 个产品/lifecycle 镜像已构建推送，候选 manifest/Bundle 已生成，产品与 third-party metadata 检查通过；runtime Compose observability closure 通过并清理。
- runtime 的 lifecycle failure matrix 随后在既有固定入口失败：`{"code":10,"stage":"digest","message":"registry platform digest unavailable for kafka"}`，上层记录 `lifecycle doctor exited 10, expected 0`，package 退出状态为 2。因此这是 infrastructure failure 停点，不能产生产品能力结论；候选没有 `verification-amd64.json`。
- 由于 runtime gate 失败，短 preflight、preflight Verify Evidence、正式 V01～V12、正式 receipt、publication 均未开始/未产生。失败原始输出保留于 `.run/phase24-05/candidate-63eb3f5-v1/build-runtime.log`；本候选目录及 Bundle、manifest、build metadata 继续保留。没有编辑任何历史候选或回执。
- 归属清理已完成：runtime closure 的本轮 acceptance/lifecycle 容器、网络、卷均已清理；本轮 registry 容器 `gopulse-phase24-05-registry-63eb3f5` 已删除。此前 registry `gopulse-phase24-05-registry-4b5d16f` 未触碰；未使用全局 prune。该接续从 `20:09:48` 到 `20:16:55` 实际约 7 分 07 秒，计入历史累计，不重置 Phase24 预算。

## 本轮正式矩阵结果（2026-10-14）

- 为修正 V07 恢复等待只看阶段就绪、未绑定每条完成记录的问题，提交 `2e90582` 增加了同一 120 秒恢复期限内的逐元数据持久 `business_application_results` 轮询；`make verify MODULE=acceptance BASE_REF=upstream/main` 通过，存量 lint 24、新增 0。该修订仅修正验收工具的等待/断言时序，未改变产品重试配置。
- 从该提交构建的新候选为 `.run/phase24-05/candidate-2e90582-v1`，本地 registry 为 `127.0.0.1:15030/gopulse`。候选 manifest revision 为 `2e9058219003c8f305c828edc82d593ee4225a1e`，manifest 文件 SHA-256 为 `04bd5262045e21e44d84eff3bd8ecfc7525e49181b2629614fb62fd1537f2f15`，Bundle 文件 SHA-256 为 `b49a85ccb16a36ae0fce487eaf2ba9b24b2eacf2149c86c40862f65b8a26dfd3`；本地镜像平台摘要和 runtime/Compose gate 均通过。
- 同候选第一次正式启动回执 `.run/phase24-05/receipt-2e90582-v1.json` 发生在 V01 前的基础设施失败：`migrate` 连接失败且 `kafka-init` 等待节点分配超时。回执 SHA-256 为 `83d670860ffdeb4ff819bd3737c84d49e7babf544e54dff4944c945f2bfa7734`，原始 Compose 日志保留在 `.run/local/e4e11fe0ac91/failures/20261014T031530.083532897Z-acceptance-aad0143b1d06/logs/compose-logs.txt`；归属清理通过。没有把该次基础设施失败解释成产品结论。
- 同候选第二次正式回执 `.run/phase24-05/receipt-2e90582-v2.json` 启动成功，V01～V06 均通过；回执 SHA-256 为 `79d284cca9d8920f44fbc2c30f36b13a77f38edf38cbdfed912569604211125b`。V07 两个 60 秒 ES 故障窗口实际发出 24 个请求，HTTP/命令/post/义务均为 24，但持久 search application 只有 21，恢复耗时 261 秒，超过固定 120 秒期限，故为 `product_failure`；V08～V12 按前置失败规则为 `not_started`，归属清理通过。
- V07 原始回执 `.run/phase24-05/receipt-2e90582-v2.json.raw-5ce8992b64f9/cases/V07.json` SHA-256 为 `00fdd17e1607b47016e510ce1dc1d4efdf2117572a24e00e156d4ce82d30dec0`。缺失的 3 条是 post 10、11、12；对应 search-indexer 日志显示它们在 ES 不可用/延迟窗口中按 `30s` 间隔连续失败，达到 `SEARCH_INDEXER_MAX_RETRIES=3` 后以 `retries_exhausted` dead-letter，恢复后已没有可消费重试。完整 Compose 原始日志 SHA-256 为 `350b42608728eee617ba5a8dc61f87ded8f38f620912b48cd5a8d5404bc4c205`，路径为 `.run/local/e4e11fe0ac91/failures/20261014T032346.359227192Z-acceptance-5ce8992b64f9/logs/compose-logs.txt`。
- 该结果不是验收工具误判，也不能用延长恢复期限、减少窗口、删除完成记录断言或临时修改 retry profile 修复。Phase-24-05 已冻结现有 Worker/Indexer 的 prefetch、retry、lease 配置，并要求产品失败后停止矩阵；因此当前不进入 V08～V12。后续必须先在有效的产品修复合同/所属实施批次中解决“两个连续 60 秒依赖故障期间必要事件不能耗尽而丢失”的语义，完成相称测试与直接门禁，再以新 revision、新 Bundle/manifest、新候选和新 receipt 重跑受影响矩阵；旧 receipt、raw 和候选保持不变。

## 接续修复与直接门禁（2026-10-14）

- 已按接续合同限定修改范围：`backend/internal/worker/profile.go`、`handler.go` 及其单测补齐 Search Profile 的 V2 post 临时故障恢复；`acceptance/internal/scenario/v1_driver.go`、`acceptance/internal/receipt/v1.go` 及对应单测修正 V07 子窗口和证据校验。没有修改 retry profile、migration、epoch 准入、Outbox 身份/清理合同或历史候选/receipt/raw。
- 产品行为现在对 V2 `post.created`、`post.updated`、`post.deleted` 在有限重试耗尽后自动把原事件以 attempt `0` 重投 search retry exchange；二次发布确认后才 ack 原投递。事件 body、MessageId、event identity、command、epoch 和 routing key 保留，永久处理错误仍进入 search dead exchange，业务 Profile 的耗尽行为不变。现有完成记录唯一性和完成前置于确认的顺序继续负责副作用去重与未闭合义务留存。
- V07 driver 现在每个 60 秒故障窗口独立执行：撤除故障、等待 Elasticsearch 健康、等待该窗口全部 `business_application_results` 和当前可见性，再进入下一窗口；单窗恢复截止从该窗故障结束计算 120 秒。新 receipt 私有字段记录每窗故障起止、依赖就绪、义务关闭、可见时刻及恢复秒数，不能再把两个窗口和控制耗时合并成一个恢复值。
- 直接验证通过：`make test MODULE=backend`、`make test MODULE=acceptance`、`make check MODULE=backend`、`make check MODULE=acceptance`、`make check-compose`、`make integration SCOPE=business`；`make verify MODULE=backend BASE_REF=origin/main`、`make verify MODULE=acceptance BASE_REF=origin/main`、`make verify MODULE=lifecycle BASE_REF=origin/main` 也通过。各模块的既有 lint 告警分别为 backend 107、acceptance 24、lifecycle 45，本次新增均为 0；acceptance verify 首次发现的本次计时静态检查已修正后重跑通过。
- 接续产品修复已绑定新 candidate / Bundle / manifest，但 runtime gate 尚未通过，因此未启动新的 preflight 或正式矩阵；旧候选 `2e90582…`、旧 receipt、raw 及原始 Compose 日志保持原身份。

## 接续候选 runtime 停点（2026-10-14）

- 提交 `6dc98f6` 的新候选目录为 `.run/phase24-05/candidate-6dc98f6-v1`，版本仍为 `2.6.4`；manifest 绑定完整 revision `6dc98f63e556b2646c04d5917e739c6675cdaa8b`，manifest SHA-256 为 `sha256:16572edfda0e6938b0ab53f6fd7fa5a32de873be29ee4088ead9d85ecb6c2957`，Bundle SHA-256 为 `sha256:dda0ca64aca3471965e123ed2e3c7d538df6844e49ca8a08aea8b6d3cf7e9776`。所有产品/插件镜像的 metadata 和 Linux/amd64 digest 检查在 runtime 启动前通过。
- 该候选第一次尝试因指定的独立 registry 尚未启动而在首个 backend push 返回 connection refused；按归属启动唯一的 pinned `registry:2@sha256:a3d8…` 容器后重新执行，镜像和 Bundle 构建完成。该环境准备偏差未改变候选源码、manifest 或旧证据。
- 第二次 runtime closure 的 Compose smoke、observability closure 和栈健康检查通过；lifecycle failure matrix 在固定 `up` 服务阶段返回 `{"code":18,"stage":"services","message":"Compose failed; use status/logs; data and resources retained for retry"}`，package 以 code 2 停止。该次没有生成 runtime success verification，也没有启动 V1 preflight、Verify Evidence 或正式矩阵，不能据此产生产品失败结论。
- 本候选的失败摘要保留在 `.run/phase24-05/candidate-6dc98f6-v1/runtime-failure.json`，candidate / Bundle / manifest 保留供复核；本次 registry `gopulse-phase24-05-registry-6dc98f6` 已按归属删除，未触碰仍在运行的旧 registry 或旧候选。当前必须先处理该 runtime 基础设施停点，再从同一修复 revision 或新的实际修订重新绑定 candidate；不复用未通过 runtime gate 的候选进入 preflight。

## 接续 runtime 失败步重跑（2026-10-14）

- 旧停点 `.run/phase24-05/candidate-6dc98f6-v1/runtime-failure.json` 保持原样。为满足候选 manifest 的 revision 身份校验，本次在 detached `6dc98f6` worktree 中只执行原 runtime 的 lifecycle 子步骤，没有重建镜像、Bundle、manifest，也没有启动 preflight、Verify Evidence 或正式 V01～V12。
- 先对 clean lifecycle 子步骤做同候选复现，`up` 到 `ready`，后续 verify、停启和 down 均通过；随后严格重跑原停点对应的 `lifecycle --install clean --failure-matrix`。该命令退出 `0`，未输出 `[gopulse-acceptance] ERROR:`；原 code 18 在本次仍由暂停 MySQL 的预期负向用例产生，之后完成解冻、down 和清理。原始重跑日志为 `.run/phase24-05/runtime-retry-2.log`，摘要为 `.run/phase24-05/candidate-6dc98f6-v1/runtime-retry-2.json`。
- 对照旧 raw 失败目录 `.run/local/e4e11fe0ac91/failures/20261014T052344.278783122Z-acceptance-b3e026170b94/`，code 18 是 failure matrix 的预期返回码，并非产品能力失败；结合本次无错误标记的成功重跑，现有证据支持 runtime gate 在预期故障注入的服务失败处发生了一次性基础设施/时序误停。进一步检查发现本机复用的 `acceptance/bin/gopulse-acceptance` 内嵌 revision 为旧的 `8da3c07`，而候选绑定 `6dc98f6`；已修复 `lifecycle/internal/packaging/verify.go`，runtime gate 现在每次从当前 acceptance 源码构建临时 runner，并由 `lifecycle/internal/packaging/verify_test.go` 防止过期二进制复用。
- 本次重跑的专属 project、容器、网络、卷和安装目录均已清理；旧错误摘要、candidate、Bundle、manifest 和历史 raw 均保留。由于没有重新执行完整 `make package ... RUNTIME=1`，不能补写 `verification-amd64.json`，也不把该局部重跑改称 runtime gate 的正式成功；preflight 和正式矩阵继续停在原计划边界。

## 修复后完整候选与正式验收停点（2026-10-14）

- 针对上一轮 V10 验收工具探针问题，提交 `d1fd597` 仅修正 acceptance private HTTP client 绕过宿主代理，并为 admin overview 补齐 `range=15m` 合同；相关单测、`go vet ./internal/scenario` 和 `git diff --check` 已通过。未修改产品业务代码。
- 以 `d1fd5978858469b493e0c87661a1e747bf818ef5` 构建的新候选为 `.run/phase24-05/candidate-d1fd597-v1`，版本仍为 `2.6.4`。release manifest SHA-256 为 `sha256:272efdc3b4be90dc4b7bb0ee52949498729c7035ae9ea4c44f67e7099a1f882b`，Bundle SHA-256 为 `sha256:91fe45672d230cbec930e148849f4a649af3162684935423db6c573dd4eb345c`。本轮 pinned registry `gopulse-phase24-05-registry-d1fd597` 已删除；历史 registry 未触碰。
- `make package PLATFORM=linux/amd64 OUTPUT=.run/phase24-05/candidate-d1fd597-v1 REGISTRY=127.0.0.1:15034/gopulse RUNTIME=1` 完整通过 runtime/Compose/lifecycle 检查，日志为 `.run/phase24-05/build-runtime-d1fd597.log`，最终输出 `amd64-runtime-and-compose-passed`。
- 短预检通过：`.run/phase24-05/preflight-receipt-d1fd597-v1.json` 的 `status=passed`，日志为 `.run/phase24-05/preflight-d1fd597-v1.log`；其 `VERIFY_EVIDENCE=1` 也通过，日志为 `.run/phase24-05/preflight-evidence-d1fd597-v1.log`。
- 正式验收从 V01 开始执行，回执为 `.run/phase24-05/receipt-d1fd597-v1.json`，原始 case 目录为 `.run/phase24-05/receipt-d1fd597-v1.json.raw-ae45d6450c41`。V01～V10 通过；V07 两个 ES 故障窗口恢复后，待搜索入库计数均收敛到 `0`，对应 raw 为 `cases/V07.json`。V11 在 lifecycle 新安装首次 `up` 停止，回执记录 `failure_class=infrastructure_failure`、`lifecycle up exited 18, expected 0`；V12 按首个失败规则为 `not_started`。正式日志为 `.run/phase24-05/formal-d1fd597-v1.log`，验收 Compose cleanup 通过。
- V11 的失败命令已在 receipt 中保留：`lifecycle up` 在 `infrastructure` 阶段返回 code `18`，耗时约 `1.4s`；候选 lifecycle 工具按安全合同抑制底层 Compose stderr，现有冻结证据只能确定为 lifecycle infrastructure failure，不能把具体 Docker 子服务推测成根因。未连续重跑 V11，也未启动 Verify Evidence、publication、版本更新或 Phase 收口。
- 新正式回执 SHA-256 为 `sha256:e3b1e7ad1192645bee30fc17ef1882d215720b8db3b744e5c22f1c3e9c06f4af`；旧失败回执 `.run/phase24-05/receipt-6d3eb10-v1.json` SHA-256 仍为 `sha256:a885b6c95bcb3c2e0129b975b42e2f6d526a2cf1c24db22c728a28d0a51ad139`。`VERSION` 仍为 `2.6.4`，Phase-24-05 仍未完成。
