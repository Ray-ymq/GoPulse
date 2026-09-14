# Phase-16-06：Linux 产品矩阵与阶段收口实施方案

> 目标版本：`1.13.6`
> 开发分支：`develop/1.13.6`
> 运行与验收平台：真实 Linux `amd64`
> 范围修订（2026-09-14）：消费 Phase-16-05 当前版本数据与恢复合同，取消历史升级矩阵；不把同版本恢复记为跨版本升级。

## 1. 批次目标

本批不首次实现产品功能，而是冻结同一个 `1.13.6` 候选，在真实 Linux `amd64` Docker server 上从独立解压的 Bundle 运行 Phase 16 最终产品矩阵，聚合可机器校验的脱敏证据并完成阶段交接。

```text
one candidate manifest + bundle checksum
                  │
                  ▼
real Linux amd64 host/server
                  │
                  ├─ clean install and lifecycle
                  ├─ edge, login and two frontends
                  ├─ six plugins and three alert sources
                  ├─ backup and same-arch restore
                  ├─ current data and repeat recovery
                  └─ Linux failure/isolation scenarios
                  │
                  ▼
single verified evidence set
                  │
                  ▼
Phase 16 acceptance and Phase 17 handoff
```

## 2. 前置条件

- 从 Phase-16-05 合入后的最新 `upstream/main` 创建 `develop/1.13.6`。
- Phase-16-01 至 Phase-16-05 的实施记录、版本、manifest、Bundle、当前数据配方和固定门禁均已完成。
- 真实 Linux `amd64` host/server 的访问方式、owner、运行窗口、CPU、内存、磁盘、Compose 和 registry 拉取条件已确认。
- 候选 image/plugin/Bundle 均绑定同一 Git revision 和 release manifest digest。
- 独立交付目录、独立 clean-install project、restore project B 和二次 restore project C 已分配随机名称与 installation token。
- Acceptance runner 只调用正式生命周期、API 和浏览器入口，不内置第二套产品逻辑。

未满足 host/server、候选一致性或独立目录条件时，只阻断本批最终收口，不回滚前五批已经通过的固定门禁。

## 3. 实施范围

### 3.1 候选冻结与预检

- 冻结 `1.13.6` release manifest、Bundle checksum、9 个产品镜像、生命周期镜像、6 个 third-party image 和 6 个 current plugin digest。
- 继承已合入主线的 `1.13.5` 源码与合同，完成本批必要改动后构建并发布 `1.13.6` 候选；候选冻结后，从 registry 按 digest 拉取验收，不允许验收路径依赖本地源码构建或晋升后的 digest 漂移。
- 运行 `doctor` 验证 host/server 为 Linux `amd64`、Compose 版本、资源、端口、路径和候选完整性。
- 记录 host OS/kernel/CPU、Docker server OS/arch、Compose、资源和时间窗口；不记录凭据。

### 3.2 统一 acceptance runner 与 evidence

- Acceptance runner 由产品 Compose 的 `acceptance` profile 调用正式生命周期、edge、API 和浏览器。
- 每个场景写结构化 JSON：scenario id、开始/结束时间、候选 digest、project/token hash、结果、关键事实摘要和脱敏日志引用。
- Runner 不直接修改数据库完成业务断言，不绕过 edge 建立用户可见流程，也不执行全局 Docker 清理。
- 证据文件使用原子完成标记；失败或中断的 evidence 不得被聚合器接受。

### 3.3 Clean-install 与生命周期矩阵

从空的独立 Bundle 目录运行：

1. `doctor`、`init`、`up`、`verify`、`status`、`logs`、`down`、再次 `up`。
2. 校验用户只依赖 Docker/Compose 和 Bundle，不调用宿主 Git、Go、Node.js、Python 或基础设施客户端。
3. 校验 Secret 私有且不出现在参数、环境转储、日志、诊断或 evidence。
4. 校验唯一 edge、初始化 job 幂等、服务健康、digest 与运行 revision 一致。
5. 校验 `verify` 严格只读，前后 Docker 与业务快照不变。
6. 校验并发操作、重复 down、SIGINT/SIGTERM 和启动中断得到稳定终态。

### 3.4 产品与双 Frontend 矩阵

- 通过唯一 edge 完成统一登录、session 恢复、刷新、登出、用户/管理员角色分流和越权拒绝。
- 用户 Frontend 覆盖目标、详情、指标、事件、日志、告警和操作历史。
- 管理 Frontend 覆盖用户、角色、插件、目标和必要运维状态。
- 覆盖 loading、empty、partial、stale、permission denied 和 backend unavailable。
- 浏览器覆盖 desktop 与 narrow viewport、键盘导航、focus、语义标签和 `Asia/Shanghai` 非 UTC 时间场景。
- 校验 cookie、CSRF、CSP、frame、content-type、cache 和安全回跳合同。

### 3.5 插件、业务与告警矩阵

- Redis、MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 六类 current v2 插件从 Linux `amd64` 受信 catalog 真实启动和采集。
- Monitor 到 Backend、Search Indexer、Business Worker 和两个 Frontend 的关键链路可查询。
- 指标、事件、日志三源告警各产生至少一个代表事实，覆盖规则、通知状态和操作审计。
- 采集/告警停止与恢复不破坏历史，重启后继续采集。

### 3.6 当前数据、backup 与恢复后再备份矩阵

- 在 `1.13.6` clean-install project A 按 Phase-16-05 的数据配方通过正式入口生成当前数据，记录候选 digest 与事实摘要；六插件、业务和告警矩阵的数据可复用，不重复生成等价场景。
- A 创建 format v1 加密备份 A 并独立 inspect/checksum；使用同一 `1.13.6` manifest 在空 project B 恢复，核对身份、业务、搜索、指标、告警、审计和插件事实，验证双 Frontend 可使用。
- B 继续写入、采集和产生告警/审计，再创建备份 B；独立 inspect 后恢复到同 manifest 的空 project C，验证原有与新增事实及 C 继续写入。
- 对已成功恢复的非空目标重复恢复，必须安全拒绝且不修改数据；不将拒绝或重启称为升级幂等。
- 选择一个代表数据导入失败和一个 restore 中断，验证可诊断、按正式合同重试或清理后恢复且不留下伪 ready。复用 Phase-16-05 runner，不复制故障/恢复实现。
- 不使用 `1.9.4` fixture、Redis v1 迁移或跨版本 state 切换。不得恢复 Phase-16-05 的 `1.13.5` 备份来冒充本候选同 manifest 恢复；历史证据仅用于追溯，不混入最终候选通过证据。

### 3.7 Linux 故障、隔离与清理

- 覆盖端口占用、磁盘不足、daemon 不可用、manifest tamper、错误 backup 口令和服务启动失败。
- 覆盖 Linux 文件权限、空格路径、私有 temp、signal 和中断。
- 在操作前后快照无关 Docker resources、其他 project 和用户文件；结果必须不变。
- 所有删除/停止目标同时满足 project、installation token、service/resource label 和 digest。
- 禁止全局 prune、宽泛 name/glob、未解析变量和宿主根目录递归删除。

### 3.8 Evidence 聚合与阶段文档

- 聚合器验证 schema、必填场景、host/server arch、候选 digest、Bundle checksum、project 隔离、结果和脱敏标记。
- 聚合器拒绝缺失场景、失败/中断 evidence、不同候选 digest、非真实 Linux `amd64` runtime 或 Secret 命中。
- 更新产品安装、生命周期、备份恢复、当前数据生成、支持范围和故障处理文档。
- 生成 Phase-16-06 同名实施记录，明确外部发布状态和任何非阻断限制。

## 4. 不在本批范围

- macOS、Windows、`linux/arm64` 产品运行或支持验证。
- 新产品功能、视觉重设计、backup format 变更、所有历史或跨版本升级、旧版 fixture 与 Redis v1 迁移。
- 删除已有历史插件包或兼容实现；不因本次范围收敛改变既有历史记录。
- Kubernetes、生产高可用、性能容量认证、镜像签名/SBOM/CVE 平台。
- 因最终矩阵之外的发现开展一般性审计、重构或覆盖率活动。

## 5. 建议实施顺序

1. 确认 Linux host/server 与资源，冻结候选 manifest 和 Bundle checksum。
2. 在独立目录运行 candidate preflight 与 artifact runtime。
3. 运行 clean-install/lifecycle 和产品/Frontend 矩阵。
4. 运行六插件、业务和三源告警矩阵。
5. 复用同候选当前数据，运行 A → backup A → B → 新数据/backup B → C 的恢复闭环及代表故障。
6. 运行固定故障、隔离和清理场景。
7. 聚合 evidence，运行 Secret 扫描与阶段门禁。
8. 更新文档、实施记录和 `VERSION`，提交后停止。

## 6. 预计直接影响文件

- Acceptance runner、evidence schema 和聚合器
- 候选 manifest、Bundle metadata 与发布验证脚本
- 只为最终矩阵暴露的直接阻断所需产品文件
- Linux 安装、生命周期、备份恢复、支持边界和故障处理文档
- `dev/logs/Phase-16/Phase-16-06-Linux产品矩阵与阶段收口.md`
- `VERSION`

本批不得借“收口”扩大改动范围。若发现前批合同缺陷，只修复复现场景所需的最小直接问题并记录原因。

## 7. 批次与阶段验收标准

### 7.1 候选真实性

1. 最终 evidence 来自真实 Linux `amd64` host/server，Docker server arch 与 manifest 一致。
2. 所有场景绑定同一 `1.13.6` release manifest、Git revision 和 Bundle checksum。
3. 所有 image/plugin 按 digest 拉取，未在验收期间重建或漂移。
4. 产品路径不依赖宿主源码工具或基础设施客户端。

### 7.2 产品共同闭环

1. Lifecycle clean install、停止、重启、只读验证、状态和日志通过。
2. 唯一 edge、统一登录、角色隔离和两个 Frontend 通过。
3. desktop/narrow、keyboard、focus、错误状态和非 UTC 时间通过。
4. 六插件采集、业务、搜索、可观测与三源告警通过。
5. Secret 不泄露，归属、并发、signal 和清理合同通过。

### 7.3 数据闭环

1. Current backup 可在全新 Linux `amd64` project 恢复，事实一致并能继续写入。
2. 恢复项目产生的新业务、采集与告警/审计事实经再次 backup/restore 保持，并能继续使用；非空目标重复恢复安全拒绝且不改数据。
3. 数据导入失败、restore 中断、错误口令和 tamper 不产生半完成 ready 状态。
4. 无关 Docker 资源、其他 project 和用户文件保持不变。

### 7.4 Phase 完成条件

- Phase-16-01 至 Phase-16-06 的同名实施记录齐全且只记录真实结果。
- `Phase-16-总实施方案.md` 第 15 节全部阶段级验收通过，无阻断问题。
- 根与受管版本为 `1.13.6`，全部批次按顺序合入主线。
- 支持文档明确 Phase 16 的产品范围为 Linux `amd64`。
- 交接给 Phase 17 的候选、Bundle、evidence、当前数据配方和同候选 backup 可复核；不宣称已支持历史/跨版本升级。

满足后 Phase 16 完成，但不提前宣称 Milestone 4 完成。

## 8. 固定验证命令与回归范围

```bash
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
docker compose -f deploy/phase16-acceptance.yaml --profile acceptance run --rm --no-deps acceptance phase16 --manifest "$GOPULSE_PHASE16_BUNDLE/release-manifest.json" --work "$GOPULSE_PHASE16_WORK" --artifact-receipt "$GOPULSE_PHASE16_WORK/verification-amd64.json" --evidence "$GOPULSE_PHASE16_WORK/evidence/linux-amd64.json"
python3 scripts/verify-phase16-evidence.py --linux dist/evidence/linux-amd64.json
scripts/verify-product-lifecycle.sh --platform linux/amd64 --clean-install
scripts/verify-backup-restore.sh --manifest dist/release-manifest.json --platform linux/amd64 --current-product
scripts/verify-backup-restore.sh --manifest dist/release-manifest.json --platform linux/amd64 --current-product --failure-matrix
scripts/verify-compose.sh
```

`--current-product` 与这里的 `--failure-matrix` 由 Phase-16-05 实现/对齐，语义见该批方案；这里不是对现有脚本参数已可用的声明。聚合 runner 与单项命令若覆盖同一候选同一场景，应复用通过 evidence，不执行两遍；候选级 evidence 不得引用不同版本的通过结果。

实现入口可按仓库实际名称等价调整，但必须保持以上场景和证据语义。前五批已经成功且未受最终修复影响的门禁不得无依据重跑；最终矩阵只重跑候选级集成所必需的固定门禁。

## 9. 实施记录与 Phase 17 交接

完成前创建 `dev/logs/Phase-16/Phase-16-06-Linux产品矩阵与阶段收口.md`，至少记录：

- Linux host/server inventory、候选 manifest、Bundle 和全部 digest；
- 各场景实际命令、结果、evidence 路径、失败轮次与最小修复；
- clean install、双 Frontend、六插件、告警、backup/restore、恢复后再备份恢复、隔离和清理事实；
- Secret 扫描、发布边界、偏差、已知限制和非阻断后续事项。

交给 Phase 17 的固定输入是 `1.13.6` Linux `amd64` 完整 Compose 产品、版本化 Bundle、不可变制品、共享生命周期、统一双 Frontend、backup format v1、当前数据配方、同 manifest 恢复与持续使用合同和最终 evidence。Phase 17 只做 Kubernetes 前工程质量收口，不把 Kubernetes 作为验证 Compose 产品的条件。


### Phase-16-06 实际入口对齐

最终 runner 使用独立验收 Compose 文件 `deploy/phase16-acceptance.yaml` 的 `acceptance` profile；仍只调用 Bundle 正式生命周期、API 和浏览器。环境变量、独立解压与同候选 artifact receipt 合同见 `dev/phase16-linux-matrix.md`。`verify-release-artifacts --runtime` 已包含固定完整 Compose 门禁，不重复运行 `verify-compose.sh`；聚合 runner 包含 clean-install、Linux failure-matrix 和当前数据恢复/失败/清理三个子模式，不重复运行等价子命令。
