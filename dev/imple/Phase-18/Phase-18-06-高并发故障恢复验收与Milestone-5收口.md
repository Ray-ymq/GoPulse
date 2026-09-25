# Phase-18-06：高并发故障恢复验收与 Milestone 5 收口实施方案

> 目标版本：`2.0.6`
> 开发分支：`develop/2.0.6`
> 候选边界：同一 Linux `amd64` manifest、Bundle、runtime contract、contract catalog 和 image/plugin digest

## 1. 批次目标

本批不首次设计产品能力。它冻结同一 `2.0.6` 候选，在固定 WSL2 Linux `amd64`、8 vCPU/12 GiB 环境中运行容量、多副本、背压、双 ES、诊断、合同、完整产品和恢复矩阵，发布脱敏容量报告并收口 Milestone 5。

若最终矩阵暴露阻断，只修直接缺陷，重建完整候选并使受影响证据失效；不删除场景、放宽 SLO 或用 Kubernetes 旁路通过。

## 2. 前置条件

- Phase-18-01/02/03/04/05 已按顺序合入最新 `upstream/main`，同名实施记录、版本和批次 evidence 可复核。
- 从该主线创建 `develop/2.0.6`，不复用任一前批分支。
- WSL2 可见配额精确为 8 vCPU、12 GiB RAM、8 GiB swap，工作区在 Linux 文件系统，可用 SSD 空间至少 80 GiB。
- 准备 clean、`1.14.5` migration、`2.0.5` direct-predecessor、fault、backup/restore 等独立 project 和随机归属 token。
- 所有候选制品已构建并按 digest 绑定；验收目录只使用 Bundle/lifecycle，不使用 Git、Go、Node 或基础设施客户端操作产品。

## 3. 候选冻结与证据

- 冻结 revision、release manifest、Bundle checksum、runtime contract digest、四个 contract catalog digest、全部产品/插件/第三方 image digest。
- 任何产品、验收脚本、schema、mapping、合同生成物或发布元数据改动都产生新候选；旧候选 receipt 不复用。
- 每个场景写原子 receipt，包含 scenario ID、全部候选 digest、project/token hash、起止时间、结果、reason code、事实摘要和脱敏附件 checksum。
- 只复用本候选、相同输入且相关代码/配置/环境未改变的成功 receipt，并在聚合证据中记录复用依据。

## 4. 最终验收矩阵

### 4.1 预检、clean lifecycle 与合同

- 执行 `doctor → init → up → verify → status → logs → down → up`，验证唯一 edge、强归属、版本/digest、秘密和清理。
- 校验 runtime contract、双 ES 配置、容量参数、副本数、Probe、关停预算和 Compose 实现零漂移。
- 连续两次生成 `contracts/` 生成物并校验 checksum/`git diff`，执行全部未知字段/错误码/mapping/label 负向用例。

### 4.2 容量与多副本

- 从固定 seed 重建数据，执行三轮 5 分钟预热、15 分钟 150 RPS、2 分钟 300 RPS 和最长 10 分钟恢复。
- 验证读 P95/P99 不超过 500 ms/1.5 s，写不超过 800 ms/2 s，稳态错误不超过 1%，突发显式过载拒绝不超过 5%。
- 验证三轮 RPS 偏差不超过 ±5%、P95/P99 偏差不超过 ±10%，无 OOM，swap 增量不超过 256 MiB。
- 对五类可扩展组件重跑单/多副本对比：固定 8 vCPU、无 Backend CPU 隔离宿主上的 Backend
  饱和比值作为容量特征记录，并以三轮 150/300 RPS SLO、三副本流量分发和逐实例替换作为阻断门禁；
  其余四类至少 1.3 倍。逐实例替换必须无越权提交、重复副作用或已接受数据丢失。
- 启动 Monitor 第二所有者，验证其安全退出且对现有六插件无状态影响。

### 4.3 背压、故障隔离与追赶

- 依次饱和 HTTP、MySQL pool、Outbox、Rabbit main/retry/dead、Router/Kafka、Marshaller/VM 和 Marshaller/观测 ES。
- 每个场景验证固定拒绝或积压、指标、无界资源负向、无伪成功以及解除后 10 分钟排空。
- 分别停止搜索 ES 和观测 ES，验证另一数据面与社交业务继续运行；恢复后无手工数据修补。
- 持续检查搜索/通知 30 秒 P99、Metrics/Logs/Events 60 秒 P99 收敛门禁。

### 4.4 独立诊断与 canary

- 在全部正常、Kafka 故障、VM 故障、观测 ES 故障和三者同时故障下运行 `status/doctor/verify --canary --json`。
- 证明 stdout 和原子 receipt 始终可用，并能区分进程、依赖、积压和数据链路不收敛，无 Secret/路径/payload 泄漏。

### 4.5 升级、备份恢复与完整产品

- 从 `1.14.5` 正式候选创建单 ES 当前数据和 format v1 备份，升级/恢复到 `2.0.6` 双 ES，验证业务、搜索、观测、插件、角色、告警和审计事实。
- 对 `2.0.6` format v2 执行 backup/inspect/空 project restore，验证双 ES 与恢复后新写入。
- 普通用户完成注册/登录、发现/关注、发帖/编辑/删除、评论/点赞/收藏、feed、通知和搜索。
- 超级管理员完成双 Frontend 统一登录、用户/角色、六插件、Metrics/Logs/Events、告警、dashboard 和审计；固定 `401/403/success` 矩阵通过。
- 正常、失败和中断 cleanup 只移除当前 project/token/digest 强归属资源，无全局 prune 或宿主宽泛删除。

## 5. 容量报告与 Milestone 5

脱敏报告必须记录：候选全部 digest，参考宿主与执行窗口，recipe/seed，三轮 RPS/P95/P99/错误，
资源峰值与 swap，各队列/lag/收敛，扩容比率，两个 ES 故障与恢复，过载拒绝，第一瓶颈，
可扩展组件、状态单点和适用边界。报告明确不是生产 HA 或其他硬件的容量承诺。

只有全部场景通过、六份实施记录齐全、无阻断问题且 `VERSION=2.0.6` 时，才能完成 Phase 18 和 Milestone 5。

## 6. 批次与阶段完成条件

1. 同一 `2.0.6` 候选的宿主预检、三轮容量、多副本、替换、背压、双 ES、诊断/canary 和合同漂移全部通过。
2. `1.14.5 → 2.0.6`、format v1/v2 restore、恢复后新写入和完整产品/权限回归通过。
3. 最终 evidence 只包含同候选完整 receipt，Secret/路径/payload 扫描、强归属和无关资源对比通过。
4. 容量报告明确固定环境、第一瓶颈、可扩展组件、状态单点、超容量行为和适用边界。
5. Phase-18-01 至 18-06 同名实施记录齐全，根与受管版本为 `2.0.6`，无阻断问题。

## 7. 不在本批范围

- 首次实现负载器、多副本、背压、双 ES、诊断或合同生成。
- Kubernetes、生产 HA、新 UI/API/插件、通用优化、依赖升级或覆盖率活动。
- 在最终矩阵未暴露阻断时进行一般性 Review 或机会性重构。

## 8. 固定验证命令

```bash
python3 scripts/ci/generate_contracts.py --check
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-phase18.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK" --evidence "$GOPULSE_PHASE18_WORK/evidence/linux-amd64.json"
python3 scripts/verify-phase18-evidence.py --linux "$GOPULSE_PHASE18_WORK/evidence/linux-amd64.json"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.6 --base-ref upstream/main
git diff --check
```

`verify-phase18.sh` 必须聚合而不重复执行本候选已成功且输入未变的贵重场景。若候选修复改变了相关输入，
重跑受影响场景和最终聚合，不因收口而重跑无关成功检查。

## 9. 实施记录

创建 `dev/logs/Phase-18/Phase-18-06-高并发故障恢复验收与Milestone-5收口.md`，记录候选/宿主、所有实际命令与结果、
失败轮次与最小修复、receipt 复用依据、容量报告、偏差、限制和外部发布边界。完成后更新 `VERSION=2.0.6`，提交并停止。
