# Phase-24-05：V1 冻结验收与阶段收口

> 状态：未完成（更新于 2026-10-13）。正式矩阵已执行到 V03：V01/V02 通过，V03 基础设施失败，后续未开始；接续候选又在 runtime gate 拉取镜像时失败。现已修订本地镜像复用和故障诊断工具，仍需重新冻结及验收，不产生 `validation_complete` 或阶段完成结论。下文按执行时间保留各次实际结果。
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
