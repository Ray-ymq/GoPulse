# Phase-24-05：V1 冻结验收与阶段收口

> 状态：未完成。2026-10-12 首次候选在 lifecycle failure matrix 的低磁盘子项发生 `infrastructure_failure`；修复验收 fixture 后，候选 runtime gate、V1 preflight 和 Verify Evidence 通过，但正式 V01～V12 未开始，不产生 `validation_complete` 或产品能力结论。
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
- 未执行正式 V1 命令：当前 `acceptance/internal/scenario/v1.go` 的 formal 分支在 V01 baseline 后明确返回 `formal V1 matrix is incomplete: Phase-24-05 actions remain not_started`，尚未实现 V02～V12 固定动作。按本分方案“正式矩阵中不得开发工具”的约束，不能启动半实现正式矩阵，也不能拼接旧回执。
- 因此正式 V01～V12、正式 receipt、publication 和 `2.6.5` 完成元数据均未产生；`VERSION`、环境版本、前端 package/lock、runtime contract 产品版本仍为 `2.6.4`。这次 preflight/runtime 结果不声明完整 V1 能力达标。

## 清理、证据与后续

- 本批 acceptance/lifecycle 项目、容器、网络和卷已按归属清理；registry 容器和本批候选镜像标签也已显式清理，没有使用全局 prune。候选 Bundle、manifest、preflight raw 和私有失败/修复摘要保留在 `.run/phase24-05/` 供复核，不入 Git。
- 首次失败证据仍保留在 `.run/phase24-05/failure-evidence.json`；修复后 runtime/preflight 事实摘要保留在 `.run/phase24-05/repair-evidence.json`。未以新 receipt 重置正式预算。
- 后续必须先为 V02～V12 补齐并审定正式验收工具/合同，再新建正式目录和 receipt 执行；不能把本次 preflight 或 runtime gate 改称阶段完成。
