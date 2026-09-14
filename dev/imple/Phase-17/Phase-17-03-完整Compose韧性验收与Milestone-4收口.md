# Phase-17-03：完整 Compose 韧性验收与 Milestone 4 收口实施方案

> 目标版本：`1.14.3`
> 开发分支：`develop/1.14.3`
> 运行与验收平台：真实 Linux `amd64` Docker host/server

## 1. 批次目标

本批不首次实现产品功能。它冻结同一个 `1.14.3` Linux `amd64` 候选，从独立版本化 Bundle 运行 Phase 17 最终产品韧性矩阵，聚合可机器校验的脱敏 evidence，确认完整 Compose 产品无需 Kubernetes 即可成立，并完成 Milestone 4 收口。

```text
one 1.14.3 manifest + bundle + runtime contract
                         │
                         ▼
            real Linux amd64 server
                         │
       ┌─────────────────┼──────────────────┐
       ▼                 ▼                  ▼
 runtime/probes      state reliability   complete product
 config/signals      migration/messages  roles/frontends/plugins
       └─────────────────┼──────────────────┘
                         ▼
          one atomic Phase 17 evidence set
                         │
                         ▼
       Milestone 4 closure + Phase 18 input
```

只要最终矩阵仍暴露产品阻断，本批只能做最小直接修复并重跑受影响场景与最终聚合；不得通过放宽断言、移除场景或引入 Kubernetes 旁路完成收口。

## 2. 前置条件

- Phase-17-01/02 已按顺序合入最新 `upstream/main`，同名实施记录、版本、固定门禁和结构化 receipt 可复核。
- 从该主线运行 `scripts/start-development-batch.sh Phase-17-03 --remote upstream` 创建 `develop/1.14.3`。
- `1.14.3` release manifest、Bundle、runtime contract、9 个产品镜像、lifecycle 镜像、6 个 current plugin 和 6 个第三方 image 绑定同一 Git revision/digest 集合。
- 一个真实 Linux `amd64` host/server 的 OS/kernel/CPU、Docker server、Compose、CPU/内存/磁盘、端口和运行窗口已确认。
- 分配独立 Bundle 解压目录、clean project、direct-predecessor migration project、故障 project、backup/restore project 和随机 installation token；所有目标均满足 Phase 16 强归属合同。
- `1.13.6` 直接前序 manifest/当前数据配方可用；私有备份口令通过受控文件/管道提供，不进入仓库、命令行或 evidence。

缺少真实 server、同候选 digest、前序数据或隔离目录时，阻断最终收口，但不回滚前两批已通过且输入未变的结果。

## 3. 实施范围

### 3.1 候选冻结与预检

- 构建并发布 `1.14.3` Linux `amd64` 候选后冻结 release manifest、Bundle checksum、runtime contract digest、镜像/plugin digest 和 Git revision。
- 验收目录只按 digest 拉取，不访问 Git checkout 构建 Go/Node/plugin 源码；宿主产品路径只要求 Docker Engine、Compose v2 和 Bundle。
- lifecycle `doctor` 校验 Linux `amd64`、Compose 下限、资源、端口、路径、manifest/runtime contract/checksum 和 Secret 文件权限。
- 候选冻结后不替换 tag 指向或局部重建；发生代码修复时生成新的完整 manifest/Bundle/digest，并使旧 evidence 失效。

### 3.2 Acceptance runner 与 evidence

- 最终入口 `scripts/verify-phase17.sh`（或同步后的等价名称）只调用正式 Bundle lifecycle、edge/API/浏览器、Migration job 和服务进程，不内置第二套产品逻辑。
- 每个场景写独立原子 receipt：scenario ID、输入 digest、project/token hash、开始/结束、结果、稳定 reason code、事实摘要和脱敏附件引用。
- 聚合器只接受同一 `1.14.3` manifest、Bundle、runtime contract 和 Git revision 的 receipts；Phase 16/前两批 evidence 只可作为可追溯前置，不可直接填充最终候选运行结果。
- 同一候选同一输入的 Phase 16 runtime/full Compose 场景允许复用本次候选产生的 receipt，避免子脚本重复执行；复用依据、命令和 digest 写入实施记录。

### 3.3 Clean install、配置与运行时矩阵

从独立 Bundle 目录执行：

1. `doctor → init → up → verify → status → logs → down → up`，确认唯一 edge、版本、digest、初始化幂等和只读 verify。
2. 运行 runtime contract 校验，确认 12 个 Go 组件、`.env`、Compose、plugin manifest、Probe、hard/soft dependency 和 stop grace 一致。
3. 对每一组件采样 `startup/live/ready/health` 正常语义；选择 Backend、Worker、Marshaller、Monitor 和一种 Exporter 验证代表性硬/软依赖状态转换。
4. 注入缺失/非法配置、alias 冲突、容器 loopback 误配和 Secret 复用，验证监听前失败、非零退出、安全原因码和无资源残留。
5. 对 HTTP、Worker、Kafka consumer 和 Monitor/plugin 各运行一个 SIGTERM 在途场景；确认先撤 ready、停止新工作、在预算内完成/requeue/no-commit 并退出。
6. 对一个故意超过预算的代表场景验证非零终态、Compose restart 有界、无假 ready 或 orphan process。

### 3.4 Request ID、错误与日志矩阵

- 从唯一 edge 触发正常请求、validation、`401`、`403`、`404`、`405`、上游 unavailable 和代表 panic，核对响应头/body/日志中的同一 Request ID 和稳定 code。
- 验证畸形/多值外部 `X-Request-ID` 被替换或拒绝，不能注入日志字段；内部下游调用保持有效关联。
- 验证两个 Frontend 对已知/未知 code、backend unavailable、session expired 和 partial observability 的安全显示。
- 聚合全部 Go 组件启动、状态变化、consumer、Migration、告警和关停日志，按 schema 校验有限字段/枚举/长度。
- 扫描 stdout/stderr、lifecycle logs/status/doctor/diagnostic、Frontend bundle、receipts 和 evidence，确认无 token/cookie/password/DSN/userinfo、内部绝对路径、payload 或原始异常。

### 3.5 Migration 矩阵

- 空 project 运行 `validate/status/up` 至 current，重复 up 后状态和业务事实不变。
- 使用固定 `1.13.6` manifest 在隔离 project 通过正式入口生成当前数据并备份；恢复后由 `1.14.3` migration job 推进，验证 identity、role、social、search、plugin、metrics/logs/events、alert 和 audit 事实以及新增写入。
- 两个 runner 并发执行，证明单写者和最终 current；构造非 12 dirty、future version 和内嵌集合损坏的隔离输入，证明安全拒绝、无数据变化、Backend not ready。
- 对代表 migration 中断执行正式恢复/重试合同，不使用任意 force/down；最终 project 可继续启动和写入。
- 若 Phase-17-02 未新增 Schema，明确记录 target 与 `1.13.6` 相同，但仍保留 CLI、状态、直接前序数据和拒绝路径证据；不创建占位 migration。

### 3.6 RabbitMQ、Kafka 与告警矩阵

#### RabbitMQ

- 代表成功消息只产生一次 notification/search side effect 并 ack。
- 处理暂态失败进入已确认 retry；retry publish nack/return/connection loss 时原消息 requeue；重试耗尽/永久异常进入已确认 dead。
- broker 重启后自动重建 topology/channel/consumer 并收敛；重复 delivery/ack 丢失不产生重复事实。
- SIGTERM 在途消息要么完成并 ack，要么 requeue，不能静默消失或在退出后继续写入。

#### Kafka

- Router broker ack 后才成功；buffer full/topic unavailable/timeout/shutdown 返回稳定错误且不伪报投递。
- Marshaller 写入 VM/ES 成功后才 commit；store 故障期间 offset 不前进，恢复后写入/commit 并可查询。
- group rebalance/lost ownership 取消旧 lease，旧 owner 不晚提交；commit 失败超过终止条件时 ready 失败并非零重启。
- 永久无效 envelope 只按固定 reason 计数/提交跳过，不泄漏 payload、不阻塞后续合法 record。

#### 告警

- Metrics/Logs/Events 各触发一个规则/incident，重复 evaluation 不重复创建 active incident 或 trigger audit。
- 每个来源分别失败/恢复，只有对应规则进入 unknown/stale；恢复后继续并只产生一次 recover。
- Backend/alert scheduler 重启验证 lease expiry、pending 连续性、revision fencing 和关停后旧结果拒绝。

### 3.7 完整产品与权限矩阵

- 普通用户通过用户 Frontend 完成注册/登录、发现/关注、发帖/编辑/删除、评论/点赞、收藏、Following feed、通知和搜索代表流程。
- 超级管理员通过管理 Frontend 完成用户/角色、六插件、Metrics/Logs/Events、规则、incident、dashboard 和审计代表流程。
- 固定身份矩阵：未登录管理 API 为 `401`、普通用户全部管理 API 为 `403`、超级管理员成功、角色变更后重新分流、公开作者摘要不含 role、bootstrap 超级管理员不可降级。
- 浏览器/外部入口不能直达 Monitor、Router、Marshaller、Kafka、VictoriaMetrics、Elasticsearch、MySQL、Redis、RabbitMQ、Exporter 或内部 Probe/metrics。
- 依次停止 VictoriaMetrics、Elasticsearch、Monitor/Router 和 Kafka，验证相关页面/API partial/unavailable，代表社交流程仍运行；恢复后无需人工数据修补。

### 3.8 Backup/restore、资源归属与清理回归

- 因本阶段直接修改配置、Schema 检查、进程退出和消费者状态，使用 `1.14.3` 当前候选运行一次 Phase 16 format v1 backup/inspect/空 project restore 与恢复后写入回归。
- 验证 restore 后 runtime contract、Schema current、角色、业务、搜索、插件、告警和消费者可继续工作；不重复 Phase 16 A/B/C 全矩阵或宣称历史升级。
- 故障注入前后快照无关 containers/networks/volumes/images 和用户文件；正常/失败/中断 cleanup 只移除匹配 project、installation token、service/resource label 与 digest 的对象。
- 禁止全局 prune、宽泛名称/glob、未解析变量、宿主根路径递归删除或覆盖非空用户目录。

### 3.9 文档与 Milestone 收口

- 更新产品 runtime/config/Probe/error/log/Migration/consumer/故障处理文档及 Phase 18 交接说明。
- 创建同名实施记录，关联最终 evidence、所有真实命令、失败轮次、最小修复和外部发布边界。
- 确认 Phase-17-01/02/03 实施记录齐全、版本/分支顺序正确、根与受管版本为 `1.14.3`。
- 只有阶段级全部门禁通过且无阻断问题时标记 Milestone 4 完成；Kubernetes 不参与结论。

## 4. 不在本批范围

- 首次设计/实现运行时、Migration、consumer、alert 或产品功能。
- Kubernetes、Ingress、集群持久化、自观测和任何弥补 Compose 产品缺口的临时资源。
- 完整历史升级、跨架构、性能容量、生产 HA、镜像签名/SBOM/CVE 平台。
- 新 UI、API、插件、规则来源或 backup format。
- 一般性 Review、覆盖率提升、依赖升级和最终矩阵未暴露的重构。

## 5. 建议实施顺序

1. 确认真实 host/server、独立目录/project 和前两批证据，冻结 `1.14.3` 候选。
2. 运行 artifact/runtime contract 预检和 clean lifecycle。
3. 运行配置/Probe/signal、Request ID/error/log 矩阵。
4. 运行 Migration、Rabbit/Kafka/alert 状态矩阵。
5. 运行双 Frontend、权限、六插件、业务与可观测故障隔离。
6. 运行必要 backup/restore、Secret、ownership 和 cleanup 回归。
7. 聚合 evidence；若失败，只修直接阻断，重建完整候选并重跑受影响场景及聚合。
8. 更新文档、实施记录和版本，提交后停止。

## 6. 预计直接影响文件

- Phase 17 acceptance runner、receipt/evidence schema、聚合器与自测试
- release candidate manifest、Bundle metadata 和 runtime contract 收录/校验
- 只为最终矩阵实际阻断所需的最小产品文件
- runtime、Migration、消息、告警、故障排查、支持范围和 Phase 18 交接文档
- `dev/logs/Phase-17/Phase-17-03-完整Compose韧性验收与Milestone-4收口.md`
- `VERSION`、`.env.example` 和双 Frontend 版本元数据

本批不得以“阶段收口”为由重写前两批已通过的实现。

## 7. 批次与阶段验收标准

### 7.1 候选真实性

1. 最终 evidence 来自真实 Linux `amd64` host/server，Docker server arch 与 manifest/runtime contract 一致。
2. 全部场景绑定同一 `1.14.3` Git revision、release manifest、Bundle checksum、runtime contract 和 image/plugin digest。
3. 产品从独立 Bundle 按 digest 运行，宿主不使用 Git/Go/Node/Python 或基础设施客户端完成产品操作。

### 7.2 运行时与可诊断性

1. 12 组件合同校验、Probe/硬软依赖、配置负向、SIGTERM/SIGINT 和非零失败终态通过。
2. Request ID、统一错误、结构化日志、状态恢复和两个 Frontend 安全错误展示通过。
3. Secret/路径/payload/raw error 扫描无命中，新 Probe/metrics 不可从浏览器或外部入口直达。

### 7.3 状态可靠性

1. 空库与 `1.13.6` 当前数据 Migration、重复/并发和 dirty/future/中断拒绝通过，事实一致且可继续写入。
2. Rabbit ack/retry/dead/reconnect/duplicate/shutdown 无静默丢失，notification/search side effect 幂等。
3. Kafka store-before-commit、retry、ownership、permanent skip、commit failure/nonzero restart 的 offset 与目标事实正确。
4. 三源 alert failure/recovery/restart/dedup 正确，无重复 active incident/audit，不阻断社交业务。

### 7.4 完整产品与 Milestone 4

1. Lifecycle、唯一 edge、统一登录、双 Frontend、六插件、社交/搜索、Metrics/Logs/Events、告警和审计共同通过。
2. `user/super_admin` 权限矩阵、bootstrap 保护、内部服务负向、观测故障隔离和恢复全部通过。
3. 当前 backup/restore 必要回归、强归属、无关资源快照和正常/失败 cleanup 通过。
4. Phase 17 三份同名实施记录齐全且只记录真实结果；根完成版本为 `1.14.3`，三批按顺序合入主线，无阻断问题。

以上全部通过才完成本批、Phase 17 和 Milestone 4；任一 Kubernetes 结果都不能替代这些条件。

## 8. 固定验证命令与回归范围

最终候选固定入口如下；新增脚本由前两批/本批按计划落地，等价调整必须同步所有计划和实施记录：

```bash
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-phase17.sh --manifest dist/release-manifest.json --runtime-contract deploy/runtime-contracts.json --from-manifest "$GOPULSE_PHASE16_MANIFEST" --work "$GOPULSE_PHASE17_WORK" --evidence "$GOPULSE_PHASE17_WORK/evidence/linux-amd64.json"
python3 scripts/verify-phase17-evidence.py --linux "$GOPULSE_PHASE17_WORK/evidence/linux-amd64.json"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.14.3 --base-ref upstream/main
git diff --check
```

`verify-release-artifacts --runtime` 与 `verify-phase17` 若调用同一候选的相同完整 Compose 场景，必须复用本次 receipt，不重复运行。`verify-phase17` 内部固定覆盖第 3 节所有代表场景并执行项目清理；单独的 `scripts/verify-compose.sh`、`verify-business.sh`、`verify-marshaller.sh`、`verify-alerts.sh` 或 backup runner 只在诊断失败时使用，不作为同场景第二遍成功门禁。

若本批为阻断修复修改产品代码，先运行最小受影响 package/Frontend/脚本测试，再重建候选并重跑受影响场景和最终聚合；未受影响且输入未变的前批成功门禁继续有效。禁止因上下文压缩或尚有时间而重跑全部单元测试、开展 Review 或增加场景。

## 9. Evidence 聚合要求

最终 `linux-amd64.json` 至少包含：

- evidence schema、product/Git/release/Bundle/runtime contract/image/plugin digest；
- host/server/Compose inventory、执行窗口、project/token hash 和前序 manifest digest；
- runtime/config/probe/signal/error/log、Migration、Rabbit、Kafka、alert、产品/角色/故障隔离、backup/restore、Secret/ownership/cleanup 的场景结果；
- Schema from/to、signal duration/exit、queue/offset/ownership、incident/audit、业务/搜索/可观测事实摘要；
- 每个 receipt/附件的 checksum、脱敏标记和原子完成状态。

聚合器拒绝缺失/失败/中断场景、不同候选、非真实 Linux `amd64` runtime、Secret 命中、伪造/手工编辑 receipt、未清理受管资源或无关资源变化。

## 10. 实施记录与 Phase 18 交接

完成前创建 `dev/logs/Phase-17/Phase-17-03-完整Compose韧性验收与Milestone-4收口.md`，至少记录：

- host/server inventory、候选 manifest/Bundle/runtime contract 与全部 digest；
- 每条实际命令、场景结果、receipt/evidence 路径、失败轮次与最小修复；
- Probe/signal、Migration、Rabbit/Kafka/alert、权限/双 Frontend/六插件/业务隔离、backup/restore、Secret/cleanup 事实；
- receipt 复用依据、与计划偏差、外部发布状态、已知限制和非阻断后续项；
- Phase 17/Milestone 4 完成条件逐项对照。

交给 Phase 18 的固定输入是 `1.14.3` Linux `amd64` 同一完整产品、运行时合同、三类 Probe、关停预算、Schema/Migration、consumer/alert 可靠性和最终 Compose evidence。Phase 18 只改变部署位置与编排方式，不得补做产品功能；达到上述条件后立即停止。
