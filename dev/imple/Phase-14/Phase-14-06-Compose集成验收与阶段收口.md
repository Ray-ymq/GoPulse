# Phase-14-06：Compose 集成验收与阶段收口实施方案

> 当前状态：待实施。本文档定义 Phase 14 第六个执行批次及阶段收口的范围与验收合同；目标版本 1.11.6、开发分支 develop/1.11.6 和执行顺序以 Phase-14-总实施方案.md 为准。

## 1. 批次目标

不新增产品能力，把 Phase-14-01 至 05 的单批闭环组合成一次完整 Compose 产品验收，并完成 Phase 14 状态、版本、记录与 Phase 15 交接：

    Phase 13 Redis v1 卷升级 + 空卷冷启动
      → 6 官方插件 + 6 自研组件指标
      → 管理员 Browser / Backend catalog / Metrics + Logs + Events
      → 代表性目标、进程、传输、存储故障与恢复
      → Phase 13 普通用户业务回归
      → 强归属清理与阶段停止

本批只为固定阶段验收接线，或修复真实验收暴露的最小阻断问题。不做默认独立 Review、依赖审计、覆盖率活动、额外插件或指标，也不预实现 Phase 15。

## 2. 前置条件

- Phase-14-01 至 05 全部已合入 upstream/main，根与 Frontend 版本为 1.11.5，五份实施记录与真实提交一致。
- fetch 后从最新 upstream/main 创建 develop/1.11.6。
- 逐项读取总方案第 15 节与前五批记录，只列出尚无有效证据的跨批项；已成功且输入未变的 package 检查不重跑。
- 保存现有 Docker containers、networks、volumes、日常 Compose project、端口与 Git 状态快照，不停止、删除或复用非归属资源。
- 准备可重现的 Phase 13 Redis v1 plugin volume 升级 fixture 与空 volume 路径，均使用隔离测试凭据。

## 3. 实施范围

### 3.1 固定 Phase 14 Compose 入口

- 扩展 scripts/verify-compose.sh 为 --phase14 模式，或提供实施记录注明的等价唯一固定入口；复用现有随机 project、动态宿主端口、snapshot、ownership validation、trap cleanup 与 --self-test 安全边界。
- 验收镜像和容器内客户端执行 Browser、API 与内部检查；宿主不直连未发布的基础设施或组件端点。
- 所有密码与 token 使用随机隔离值且不打印、不提交；敏感特征串扫描只输出 pass/fail 和安全位置类别。

### 3.2 升级、冷启动与恢复

- 从 Phase 13 真实 Redis Manifest v1、Registry、running 与 stopped 状态升级，验证 v2 迁移、原 desired state、原版本/时间与历史 Redis series；重试无重复记录。
- 从空 monitor_plugin_data 冷启动，验证六个嵌入包的 Manifest、entrypoint、schema digest、独立安装调和与唯一进程所有权。
- 空卷管理场景跳过自动 bootstrap，由管理员在浏览器对至少一种代表性插件完成 Schema 配置、connection-test、install/start/stop/update 和指标查询；其他五种通过同契约 API 与运行时验证。
- 替换 Monitor 容器并复用同 volume，验证六个 desired state、config revision、Secret 与 release 恢复；stopped 类型不启动，不产生第二进程。

### 3.3 六插件与六组件查询

- 通过真实业务与可观测活动产生数据；六个官方 source 各查询 up=1 与至少一个实际运行值，六个 component 各查询一个处理结果和一个进度或最近成功值。
- Backend catalog 的 source、target 与 producer 匹配必须准确；未知 metric、label、range 和伪造 VictoriaMetrics series 不被公共 DTO 接受。
- 管理员从 Frontend 使用六插件列表、详情和 Metrics、Logs、Events；普通用户访问页面或 API 被 Backend 权威拒绝。
- 扫描公共 DTO、Frontend DOM、结构化日志、Events、metrics labels、Registry/config 摘要与验收输出，确认没有 Secret、完整连接串、内部路径、PID、业务 ID 或内容。

### 3.4 代表性故障矩阵

只选足以证明不同边界的代表性故障，不对 12 个 target 做全排列：

1. 停止一个非存储目标，证明选定插件 up=0 和安全状态，其他五插件与业务继续；恢复不重启 Exporter。
2. 使一个 Exporter 子进程意外退出，证明进程归属、安全 event/status 和其他五进程不变；再通过受控 start 或 recovery 恢复。
3. 上传通过 archive 校验但试启动失败的更新，证明指定 ID 回滚旧制品、配置与 desired state。
4. 停止 Router 或 Kafka 的一个代表性传输边界，证明无无界本地指标队列、社交业务不受影响；恢复后新快照可查。
5. 停止 VictoriaMetrics，证明 Backend metrics 局部 unavailable、安全实时状态、历史 volume 保留与恢复后新值；不伪造停机期 up=0 存储点。
6. 停止或破坏一个 component metrics endpoint，证明该组件主职责与其他 11 个 targets 继续。

### 3.5 Phase 13 与平台合同回归

- 使用至少两个普通用户完成资料和用户搜索、关注与 Following 通知、收藏、发帖、评论、点赞、编辑后缓存与搜索收敛、作者删除、墓碑与防复活代表性主路。
- 验证 UserAppShell 与 AdminLayout 隔离；普通用户不能访问管理页面/API，当前 admin 仍由数据库实时授权。
- 验证 Logs/Events、Kafka 单 Topic 与 Marshaller offset、Elasticsearch 业务/日志/事件 index、Redis 缓存和 VictoriaMetrics 历史数据不回归。
- 验证镜像版本和 revision 标签、numeric user、read-only filesystem、internal-only ports/networks、包 digest、子进程归属、SIGTERM 退出与强归属资源清理。

### 3.6 阶段文档收口

- 仅在固定门禁全部通过后，将根和 Frontend 版本更新为 1.11.6，创建同名实施记录，并更新 Phase 14 总方案的实际状态与证据。
- 核对六份 split plan 均有同名 log，版本、分支和合入记录符合真实 Git 状态；未合入批次不写成已合入。
- 记录 Phase 15 阻断、非阻断后续项，以及可依赖的固定 catalog、API、source、target 与 component 契约。

## 4. 不在本批范围

- 第七类插件或组件、更多 metrics family、同类多实例或多目标、第三方插件。
- 独立实现 Review、严重性报告、依赖升级、覆盖率补齐或机会性重构。
- 告警、super_admin、角色管理、独立管理 Frontend、大屏、跨平台发布或 Kubernetes。
- 对全部目标与全部故障做全排列；本批只执行边界覆盖明确的代表性矩阵。

## 5. 建议实施顺序

1. 核对总方案验收与前五批证据，只列出未满足的跨批项。
2. 扩展 Phase 14 Compose 固定入口与安全 self-test，先验证不触及非归属资源。
3. 完成 Phase 13 Redis v1 卷升级、空卷冷启动、管理员流程与同卷 Monitor 替换。
4. 在最终 Compose 状态查询 6 plugin 加 6 component，完成授权、脱敏和基数检查。
5. 按第 3.4 节串行运行代表性故障与恢复，每个边界恢复后再进入下一个。
6. 运行 Phase 13、Logs/Events、平台与清理回归，确认最终源码和 Git 不变。
7. 全部门禁通过后更新版本至 1.11.6，完成 log 和总方案收口证据，提交并停止。

## 6. 预计直接影响文件

- scripts/verify-compose.sh、scripts/verify-compose-observability.sh 或 Phase 14 等价固定入口
- scripts/verify-plugin-metrics.sh、scripts/verify-component-metrics.sh，仅用于接入最终门禁或修复实际阻断
- frontend/e2e 与 acceptance image/config，仅用于代表性管理员与 Phase 13 跨批场景
- deploy/compose.yaml、Dockerfiles、.env.example 或 README，仅当最终验收暴露必要接线缺失
- 真实阻断修复直接涉及的最小生产文件与测试
- VERSION 与 Frontend 版本元数据
- dev/imple/Phase-14/Phase-14-总实施方案.md，仅用于实际完成状态和证据
- dev/logs/Phase-14/Phase-14-06-Compose集成验收与阶段收口.md

如验收未暴露生产阻断，本批不修改前五批生产实现或新增边界测试。

## 7. 批次与阶段验收标准

### 7.1 批次完成条件

- 第 3.2 至 3.5 节的升级与冷启动、12 targets 查询、授权/脱敏/基数、代表性故障恢复、Phase 13 与平台回归全部通过。
- 六份 split plan 均有同名真实实施记录；记录中的命令确已执行，偏差与未执行项未被伪写为完成。
- 根、Frontend 与受管版本元数据为 1.11.6，分支为 develop/1.11.6；最终提交只包含本批接线、必要阻断修复、验收、记录和阶段收口文件。
- 所有强归属测试资源已清理，测试前存在的容器、网络、volume 与源码工作树不变。

### 7.2 Phase 14 完成与停止条件

Phase 14 仅在总方案第 15 节全部满足、六个执行批次已按顺序合入 upstream/main、主线根版本为 1.11.6 且无阻断问题时完成。本批本地验收通过但尚未合入时，应如实写“待合入”，不提前宣称主线阶段完成。

达到条件后停止。多实例、更多指标、告警、管理端重构、跨平台与 Kubernetes 只作后续项，不延长本阶段。Milestone 4 仍到 Phase 17 才最终收口。

## 8. 固定验证命令与回归范围

已成功且相关输入未变的 package 测试从前五批实施记录引用。本批最终 diff 固定运行：

    bash scripts/verify-compose.sh --self-test
    bash scripts/verify-plugin-metrics.sh --self-test
    bash scripts/verify-component-metrics.sh --self-test
    bash scripts/verify-compose.sh --phase14

    python3 scripts/ci/validate_versions.py
    python3 scripts/ci/validate_branch.py --branch develop/1.11.6 --base-ref upstream/main
    git diff --check upstream/main...HEAD

- --phase14 必须包含一次最终 Frontend tests、typecheck、build 制品、Browser/API 验收、镜像构建与完整 Compose 门禁；若实际脚本不包含，须在记录中列出并运行等价命令。
- 若本批修改某个生产 package，先重跑该直接 package tests，再运行最终 --phase14；未修改 package 不因阶段收口重复全量测试。
- 任何门禁未运行、失败或清理不完整都是阻断项，不得标记批次或阶段完成。

## 9. 实施记录与 Phase 15 交接

完成前创建 dev/logs/Phase-14/Phase-14-06-Compose集成验收与阶段收口.md。

记录必须包含实际 Compose project、版本与制品、升级与空卷步骤、12 targets 查询、Browser 流程、故障矩阵、Phase 13 回归、全部命令与结果和清理证据，并如实写偏差、限制与合入状态。交给 Phase 15 的内容至少包括：

- 六 plugin 的固定 ID/source/target/catalog、单实例不变式与安全状态/API。
- 六 component 的固定 producer/source/target 与低基数 metric catalog。
- Phase 15 规则评估可依赖的数据新鲜度、失败/恢复与查询 unavailable 语义。
- 仍不得越界的 Secret、连接串、高基数 label、用户端与 Compose/Kubernetes 边界。
