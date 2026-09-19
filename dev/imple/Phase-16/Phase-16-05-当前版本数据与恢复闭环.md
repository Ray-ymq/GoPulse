# Phase-16-05：当前版本数据与恢复闭环实施方案

> 目标版本：`1.13.5`
> 开发分支：`develop/1.13.5`
> 运行与验收平台：真实 Linux `amd64`
> 范围修订（2026-09-14）：按用户要求不考虑旧版。本方案替代原《1.9.4 升级与恢复闭环》，批次顺序、版本和分支编号不变；这是规划调整，不是实施或验收完成声明。

## 1. 批次目标

以当前产品候选 `1.13.5` 为唯一数据来源，在独立安装环境通过正式业务入口和真实采集生成测试数据，验证完整产品数据的恢复与持续使用：

```text
current candidate clean install
  → real API/browser operations and current plugin collection
  → source facts + encrypted backup A + independent inspect
  → restore to empty project B using the same manifest
  → compare facts + continue writing/collecting/alerting
  → encrypted backup B + independent inspect
  → restore to empty project C + verify new and original facts
```

本批不实现或验收跨版本升级。相同版本、相同 manifest 的恢复、重启或重复执行不得被称为升级成功，也不能据此声明支持 `1.9.4 → current` 或 `1.13.4 → 1.13.5`。

## 2. 前置条件与实施顺序约束

- 从 Phase-16-04 合入后的最新主远程 `main` 开始；已有 `develop/1.13.5` 时先核对其提交与远端状态，按仓库规则协调同步，不静默重置、重命名或强推。
- 复用 Phase-16-04 已通过的 backup format v1、独立 inspect、空 project restore、operation lock、强归属和诊断合同。
- 为本批构建并冻结 `1.13.5` Linux `amd64` 候选及受信 current v2 插件 catalog。真实运行前确认所有 digest 可拉取；不要求开工前已有测试数据或本批发布包。
- 实施可先完善必要产品入口和验收 runner，再运行当前候选生成数据。不以历史源码、旧数据库、旧镜像、legacy 插件或人工预制 fixture 为前置。
- 分配独立源项目 A、恢复项目 B/C、私有数据目录和随机 installation token；不得读取、复用或覆盖用户现有业务数据卷。

## 3. 实施范围

### 3.1 当前版本的真实测试数据

- 通过正式初始化、edge API、浏览器和插件管理接口创建用户/管理员、角色关系、业务对象、目标及必要配置。
- 六类 current v2 插件从 Linux `amd64` 受信 catalog 启动并真实采集。Redis 使用当前插件，不从旧版 registry 或旧包迁移。
- 经实际操作和采集生成业务历史、搜索结果、指标、事件、日志、告警及操作审计；告警与审计来自当前功能的真实执行。
- 固定最小代表性数据配方、操作顺序和逻辑标识；凭据随机生成并私有保存。不得直接写数据库制造成功断言，不把运行变化的时间戳等当作可复现的字节常量。
- 保存候选版本、Git revision、manifest/Bundle checksum、镜像/plugin digest、数据域摘要及生成结果。只有真实生成并验证完成的数据集才可标记可用；不提交明文业务快照或 Secret。

### 3.2 一致性基线与备份 A

- 使用正式 backup 命令完成停写/排空、一致切点、format v1 加密导出和独立 inspect/checksum；不能另建验收专用备份格式。
- 在同一一致切点固定各数据域对照：身份与角色、业务对象与历史、搜索逻辑结果、历史指标、告警状态、审计和插件配置/运行意图。
- 采用 Phase-16-04 已定义的重建、会话及队列语义；列明哪些事实精确保持、哪些运行字段允许变化，不能用“数量相等”代替关键对象内容一致。
- 验证前后源项目可正常使用；备份未完成或检查失败时，不进入恢复。

### 3.3 恢复 B、继续使用与二次恢复 C

- 仅在相同 Linux `amd64`、相同 release manifest 的空 project B 恢复备份 A；不扩大 Phase-16-04 的版本/manifest 兼容合同。
- 通过正式入口核对全部约定事实，以及用户/管理员登录和角色边界、业务搜索、双 Frontend 展示、插件继续采集与告警运行。
- 在 B 真实产生至少一项新业务写入及其可查询结果、新采集事实、告警/审计事实；区分原始事实与恢复后新增事实。
- B 生成新备份 B，独立 inspect 后恢复到空 project C；核对原始与新增事实均存在且 C 能继续写入。仅生成新备份而不恢复，不算闭环完成。
- 保持空目标/强归属合同：对已经恢复成功的非空目标重复恢复应安全拒绝且不改数据，不伪装成“无需升级”。失败重试采用正式恢复合同允许的状态和目标，不强行覆盖成功安装。

### 3.4 代表故障、恢复与证据

- 在同一候选中选择一个真实数据导入失败和一个 restore 中断，验证完成标记、稳定失败状态、诊断、源事实不变和正式重试/清理路径；失败状态不得呈现 ready。
- 保留可用的加密备份、operation id、已达阶段和安全恢复指引；不得记录 Secret 或把残缺输出发布为完成 evidence。
- 复用 Phase-16-04 的错误口令、tamper、磁盘、归属等已有实现和通过记录；只有本批相关变更或观测到回归时才扩展验证，并先记录原因。
- 保存结构化 evidence，关联 A/B/C、候选 digest、备份 checksum、各阶段摘要、真实命令和结果。独立项目按强归属精确清理，无关资源保持不变。

## 4. 不在本批范围

- 所有旧版本 fixture、历史数据迁移、跨版本/跨 manifest 恢复或原地替换旧数据卷。
- Redis v1 到 v2 历史插件迁移；这里的 v1/v2 是 GoPulse 插件 Manifest 格式，不是 Redis 数据库版本。
- 删除现有 legacy 包、兼容代码、历史测试或历史实施记录；缩小验收范围不等于移除已有能力。
- 重写 Phase-16-04 备份恢复引擎、变更 backup format 或重复其完整低层故障矩阵。
- macOS、Windows、`linux/arm64`、在线零停机、集群/高可用与 Kubernetes。

## 5. 建议实施顺序

1. 确认当前恢复公共接口与数据域对照语义，列明 Phase-16-04 已完成能力和本批必要增量。
2. 实现或复用真实数据生成及产品恢复验收入口，只修直接阻断合同的产品问题。
3. 构建并冻结当前候选，在独立 A 生成代表数据和事实摘要。
4. 完成 A → backup A → B，核对并继续写入、采集和告警。
5. 完成 B → backup B → C，验证二次恢复和持续使用。
6. 完成代表故障、重复目标拒绝、隔离及固定回归，聚合脱敏证据。
7. 更新同名实施记录与 `VERSION`，提交后停止。

## 6. 预计直接影响文件

- 当前候选数据生成与产品级恢复 acceptance runner、evidence schema
- `lifecycle/**`、业务/插件接口中真实验证暴露的最小必要修复
- 当前版本数据配方与备份恢复、支持边界文档
- `dev/logs/Phase-16/Phase-16-05-当前版本数据与恢复闭环.md`
- `VERSION` 及受管版本元数据

原《1.9.4 升级与恢复闭环》实施记录保留为历史尝试，不改写为本方案完成记录；新同名记录应说明范围变更及与旧记录的关系。

## 7. 批次验收标准

1. A 的数据真实来自同一 `1.13.5/linux/amd64` 候选，生成配方、摘要和 digest 可复核，无历史来源要求。
2. 备份 A/B 均为完成并通过独立 inspect/checksum 的 format v1 加密备份，来源和恢复候选 manifest 相同。
3. B 保持身份、角色、业务、搜索、指标、告警、审计和 current 插件关键事实，双 Frontend 可使用。
4. B 继续写入、采集和告警产生的新事实可经备份 B 恢复到 C；C 同时保持原有事实并可继续使用。
5. 重复恢复至非空目标安全拒绝且数据不变；代表导入失败、中断可按正式合同重试或清理后恢复，无半完成 ready。
6. 凭据、业务快照、备份和诊断受私有权限保护；证据脱敏，无关资源和用户文件保持不变。
7. 固定门禁全部通过、同名实施记录完整、根与受管版本为 `1.13.5` 且无阻断问题后，本批才完成。不要求跨版本升级通过，不新增历史升级支持声明。

## 8. 固定验证命令与回归范围

拟扩展现有 `scripts/verify-backup-restore.sh`，复用其底层能力而不是另建产品恢复实现：

```bash
scripts/verify-backup-restore.sh --manifest dist/release-manifest.json --platform linux/amd64 --current-product
scripts/verify-backup-restore.sh --manifest dist/release-manifest.json --platform linux/amd64 --current-product --failure-matrix
scripts/verify-compose.sh
```

`--current-product` 和此处的 `--failure-matrix` 是本批待实现/对齐的验收接口，不声称当前脚本已经支持。第一项包含真实数据生成、A/B/C 两次恢复、继续使用及非空目标重复拒绝；第二项使用同一候选和可复用的权威备份，只覆盖 §3.4 两个代表失败场景。数据生成步骤是第一项的组成部分，不增加独立历史 fixture 构建门禁。

若实际参数不同，先同步本计划、总方案与 Phase-16-06，保持上述固定语义。开发期只跑最小受影响检查；最终 diff 执行固定门禁，已成功且输入/环境未变的步骤从记录恢复进度，不重复运行。不得以测试数据生成、同版本恢复或版本号修改代替跨版本升级证据。

## 9. 实施记录与下一批交接

记录真实数据配方、候选 digest、A/B/C 事实对照、两份加密备份、失败轮次、恢复/继续写入结果、命令、偏差及支持限制；仅记录实际执行结果。

交给 Phase-16-06 的输入为可复用的当前产品数据配方、恢复验收 runner、结构化 evidence 合同和 `1.13.5` 实际结果。Phase-16-06 必须在其 `1.13.6` 候选重新生成自己的同候选数据与备份，不直接恢复 `1.13.5` 的备份来冒充同 manifest 验收。
