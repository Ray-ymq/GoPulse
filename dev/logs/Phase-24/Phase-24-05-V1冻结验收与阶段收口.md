# Phase-24-05：V1 冻结验收与阶段收口

> 状态：未完成。2026-10-12 首次候选在 lifecycle failure matrix 的低磁盘子项发生 `infrastructure_failure`；修复验收 fixture 后，候选 runtime gate、V1 preflight 和 Verify Evidence 通过。随后按用户明确要求补齐 formal V01～V12 native driver，但尚未绑定新候选执行正式矩阵，不产生 `validation_complete` 或产品能力结论。
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
