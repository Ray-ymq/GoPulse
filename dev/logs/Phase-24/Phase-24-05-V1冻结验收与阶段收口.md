# Phase-24-05：V1 冻结验收与阶段收口

> 状态：未完成。2026-10-12 在同候选 runtime gate 的 lifecycle failure matrix 低磁盘子项发生 `infrastructure_failure` 后停止；正式 V1 矩阵未开始，不产生 `validation_complete` 或产品能力结论。
> 执行分支：`develop/2.6.5`；候选产品版本：`2.6.4`。

## 实际执行范围

- 已从刷新后的 `origin/main` 创建 `develop/2.6.5`。固定基线和候选 revision 均为 `bd777ecd7ffff5f10171f0a9556ff93ea6b6ff65`；执行期间工作树保持干净，未修改产品、测试、验收工具或正式配置。
- 实时宿主满足合同：Linux `amd64`，8 CPU，Docker Engine `29.7.2`，Compose `v5.5.0`，Docker 可用内存约 `12.67 GiB`，swap `16 GiB`，开工时磁盘可用约 `25 GiB`。没有发现活动的产品 Compose 项目。
- 直接门禁均通过：`make verify MODULE=backend`（存量 lint 107、新增 0，test/race/vet/build）、`make verify MODULE=acceptance`（24/0，test/race/vet/build）、`make verify MODULE=lifecycle`（46/0，test/race/vet/build）和 `make check-compose`。这些结果只证明候选入口和既有模块门禁，不替代 V01～V12。

## 候选与制品

- 私有候选目录：`.run/phase24-05/candidate-bd777ec-v1`；目录权限 `0700`，未提交。私有失败证据：`.run/phase24-05/failure-evidence.json`，权限 `0600`。
- Bundle `release-manifest.json` SHA-256 为 `sha256:1fa94c8d7e5e754c825fd3d9b4d9c7517bad983f92b42df23e8e5d1e33217d8f`；Bundle archive SHA-256 为 `sha256:9de49d30e2b003e8f63f999a30baff848630155b42e5e6ad2280885641b992bc`；manifest 记录的 Bundle payload SHA-256 为 `sha256:06c9d36a3d2c2500c44b0c8b9ce0e7c84d354e16685d48ab25918ea857d03cfc`；`checksums` SHA-256 为 `sha256:2ef8c34fa9ee88a6e6758bccb280ad672ccb2319587cb632100c2ab67aabe351`。
- manifest 的 runtime contract SHA-256 为 `sha256:475127e5aedbbf450ada59574854c4d2187218373f050371128ac63412f25536`。10 个 Linux/amd64 产品与 lifecycle image digest、registry、主机信息和原始诊断均保存在上述私有失败证据中。
- 本批自有 registry 为 `gopulse-phase24-05-registry`，namespace `127.0.0.1:15025/gopulse`，registry image 为 `registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373`。候选构建完成后 metadata/third-party digest 检查通过。

## 验证结果与停点

- 固定 package 命令已执行：

  `rtk proxy make package PLATFORM=linux/amd64 OUTPUT=/home/ray/GoPulse/.run/phase24-05/candidate-bd777ec-v1 REGISTRY=127.0.0.1:15025/gopulse RUNTIME=1`

- 同候选既有 runtime gate 的 Compose smoke/observability 和 lifecycle clean 通过。随后 lifecycle failure matrix 在低磁盘 doctor 子项停止：fixture 期望退出码 `13`，实际退出码 `10`，阶段为 `manifest`，消息为 `cannot read release manifest`；因此 package 以退出码 `2` 结束，未生成成功的 `verification-amd64.json`。
- 不改变候选的只读复现确认：候选目录必须保持 `0700`；failure matrix 的 direct 分支以 `0:0` 运行 lifecycle，并将该目录只读挂载到 `/bundle`。当前 Docker 用户映射下容器内 `/bundle` 仍为 `0700 1000:1000`，`stat /bundle/release-manifest.json` 返回 `Permission denied`。lifecycle 在容量检查前读取 manifest，故先得到 `10`，没有实际进入低磁盘容量断言。
- 该结果是验收 fixture/运行权限边界的基础设施失败，不是产品失败，也不是低磁盘能力结论。没有把短诊断改写为通过，没有替换候选、manifest、配置或回执。

## V1 矩阵状态

- `PREFLIGHT=1` 未启动；未创建 V1 preflight receipt/raw。
- V01～V12 均为 `not_started`；没有正式 HTTP 台账、业务事实/义务/完成、Broker/ES/观测窗口、恢复 journal、证据核验或 publication。没有新增 `Phase-24-05-evidence/`，因为 validator 从未得到可发布的正式 receipt。
- 版本收口命令、`sync_version_metadata --version 2.6.5`、completion 版本校验和正式 docs/质量收口未执行；`VERSION`、环境版本、前端 package/lock 和 runtime contract 产品版本仍保持 `2.6.4`。

## 清理、偏差与限制

- runtime gate 产生的 acceptance/lifecycle 项目、容器、网络和卷已按各自 owner 清理；本批 registry 容器已移除；10 个本批候选 image 本地标签及其本地镜像已显式移除。没有使用全局 prune，也没有触碰其他项目资源。
- Bundle/manifest 和私有失败证据按失败复核需要保留在 `.run/phase24-05/`；它们不进入 Git。实际活跃成本约在方案总上限 `180` 分钟以内，失败后未以新 receipt 重置预算。
- 继续执行必须先由独立修复合同处理 direct fixture 的 UID/Bundle 权限语义，并对受影响的 package/runtime gate 建立新的候选和新的原始证据；不能在本候选上拼接半轮矩阵或复用这次失败状态宣称阶段完成。
