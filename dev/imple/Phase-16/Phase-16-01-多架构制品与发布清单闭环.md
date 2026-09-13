# Phase-16-01：多架构制品与发布清单闭环实施方案

> 状态：已完成，目标版本 `1.13.1`，分支 `develop/1.13.1`。本文件名与标题为保持已完成计划和同名实施记录的可追溯关系而保留。Phase 16 后续支持范围已收敛为 Linux `amd64`；本批已经生成的额外架构制品属于历史构建结果，不构成后续支持、运行验收或阻断条件。

## 1. 批次目标

本批建立后续 Linux 产品化所需的不可变制品事实：

- 9 个逻辑产品镜像、生命周期工具镜像和 6 类 current 插件具备可校验的 release metadata。
- Linux `amd64` 镜像可从候选 registry 按 digest 拉取并在真实 Docker server 上运行。
- Compose、release manifest、checksums、配置模板和文档形成版本化 Bundle 骨架。
- 候选 push、pull-by-digest 与不重建晋升可在隔离 registry 中验证。
- `1.9.4/linux/amd64` Redis v1 legacy 包具备固定来源和 digest，供 Phase-16-05 使用。

## 2. 已确认输入

- 开工基线、Docker/Compose inventory、registry 边界和构建工具链以同名实施记录为准。
- Phase 15 的完整 Compose、双 Frontend、六插件和三源告警能力是产品输入。
- Release manifest 是后续批次消费 image、plugin、Bundle 和 source revision 的唯一映射。
- 本批的真实运行结论仅来自 Linux `amd64`；其他已产出 metadata 不扩大 Phase 16 支持范围。

## 3. 实施范围

### 3.1 Linux 产品镜像

- Backend、Business Worker、Search Indexer、用户 Frontend、管理 Frontend、Router、Marshaller、Monitor 和 Redis Exporter 形成不可变 image digest。
- Manifest 记录版本、revision、`os=linux`、`arch=amd64`、image reference、digest 和构建时间。
- 镜像运行身份、healthcheck、read-only filesystem 和必要 writable mount 可检查。
- 第三方 MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 锁定 Linux `amd64` digest。

### 3.2 插件与 legacy 输入

- Redis、MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics current v2 archive、entrypoint、Schema 和 digest 进入只读 catalog。
- Monitor 只按 server OS/arch 和受信 catalog 选择插件，不从可写 volume 建立信任。
- 从固定源码和工具链重建 `1.9.4/linux/amd64` Redis v1 archive，只登记为升级 fixture 输入。

### 3.3 Release manifest 与 Bundle

- Manifest 关联根版本、Git revision、产品镜像、第三方镜像、插件、生命周期镜像和 Bundle checksum。
- Bundle 只包含运行所需 Compose、manifest、checksum、配置模板和产品文档，不包含源码树、明文 Secret 或未登记二进制。
- Compose 使用 digest 引用，候选晋升只复制既有 manifest/artifact，不重新构建。

### 3.4 发布边界

- 外部 registry 认证不可用时，允许在 loopback OCI registry 完成候选 push/pull-by-digest 和 same-digest promotion probe。
- 实施记录必须明确区分本地验证、候选可拉取和外部已发布；不得把本地 registry 写成正式发布。
- 任何额外架构制品都保留为历史输出，不进入 Phase-16-02 至 Phase-16-06 固定门禁。

## 4. 不在本批范围

- 完整 lifecycle 命令、统一登录、Frontend 体验、backup/restore、upgrade 和最终阶段收口。
- macOS、Windows、`linux/arm64` 产品支持或真实运行声明。
- Kubernetes、镜像签名/SBOM/CVE 平台和第三方镜像一般性升级。

## 5. 批次验收标准

1. 9 个产品镜像、生命周期镜像、6 类插件和 6 个第三方镜像均有可解析 digest 与 source/version 映射。
2. Linux `amd64` 产品镜像按 digest 在真实 Linux Docker server 上运行，container arch 与 manifest 一致。
3. 六类 current 插件的 archive、entrypoint、Schema、catalog 和 digest 一致，代表采集链路通过。
4. `1.9.4/linux/amd64` legacy 包来源、工具链和 digest 可重复核对。
5. Bundle checksum、内容 allowlist、Compose digest 引用和解压后可读性通过。
6. 临时 registry 的 push、pull-by-digest 和不重建晋升保持相同 digest。
7. 根与受管版本为 `1.13.1`，同名实施记录只包含实际执行结果、发布边界和已知限制。

## 6. 固定验证命令

本批完成时实际命令和结果以同名实施记录为准；后续不得仅因计划修订而重跑已经成功的门禁。核心入口为：

```bash
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-release-promotion.sh --manifest dist/release-manifest.json
scripts/verify-compose.sh
```

若脚本最终名称与上例不同，以实施记录中已经运行的等价命令为准。

## 7. 实施记录与后续交接

权威记录为 `dev/logs/Phase-16/Phase-16-01-多架构制品与发布清单闭环.md`。该记录保留原始构建、digest、真实 Linux runtime、额外 metadata、registry 和偏差事实，不因后续范围调整重写历史。

交给 Phase-16-02 的固定输入仅包括 Linux `amd64` release manifest、可按 digest 消费的 Compose、生命周期镜像骨架、Bundle 骨架、current 插件 catalog 和 legacy 升级包。

