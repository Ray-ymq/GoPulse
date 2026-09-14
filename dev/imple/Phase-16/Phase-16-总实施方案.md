# Phase 16：Linux 产品化与双前端交付总实施方案

> 规划修订基线（2026-09-13）：主远程 `upstream/main` 提交 `6f33dc0f98d3ef7903ddfd46905c0aed32bb2775`，根完成版本为 `1.13.1`，Phase-16-01 已完成并合入主线。本文档只规划 Phase 16 的 Linux `amd64` 产品化工作，不修改 `VERSION`，也不把已有额外架构制品当作本阶段支持或验收要求。


> 2026-09-14 范围修订：按用户要求，Phase-16-05/06 不再考虑旧版；以各批当前候选生成真实数据，验收同 manifest 备份恢复和持续使用，取消历史/跨版本升级及 Redis v1 迁移门禁。保留既有兼容代码、历史制品和实施记录。六批编号与分支不变，不因规划调整修改 `VERSION`。下述状态表沿用原规划基线；本次不重写前批执行结果，实际完成状态以主线同名实施记录为准。

## 1. 阶段目标

Phase 16 将 Phase 15 的完整 Compose 系统整理为可在真实 Linux `amd64` 环境安装、运行、维护和恢复的产品交付物：

```text
immutable linux/amd64 artifacts
            │
            ▼
versioned product bundle + release manifest
            │
            ▼
containerized lifecycle control
            │
            ├─ secure init / up / down
            ├─ status / logs / verify / doctor
            ├─ backup / restore
            └─ current data / recovery continuity
            │
            ▼
single edge origin
            ├─ user frontend
            ├─ admin frontend
            └─ backend API
            │
            ▼
real Linux amd64 product acceptance
```

阶段目标如下：

- 用户只需受支持版本的 Docker Engine、Docker Compose v2 与版本化交付包，不依赖宿主 Git、Go、Node.js、npm、Python、curl 或基础设施客户端。
- Backend、Business Worker、Search Indexer、两个 Frontend、Router、Marshaller、Monitor、Redis Exporter 和六类受管插件均以可追溯的 Linux `amd64` 制品交付。
- 初始化、启动、停止、状态、日志、只读验证、诊断、备份和恢复由一个容器化生命周期实现提供，不复制为多套宿主脚本。
- 用户 Frontend、管理 Frontend 和 Backend API 通过唯一 edge origin 对外暴露，统一登录、角色隔离、错误状态和响应式体验形成产品闭环。
- 当前候选生成真实数据，备份恢复、失败恢复、恢复后继续写入与再次备份恢复在真实 Linux `amd64` 环境可重复验证；不据此声明跨版本升级通过。
- 六个实施批次、跨批集成结果和最终阶段门禁均有与计划同名的真实实施记录。

仅生成镜像、仅做静态 Compose 检查、仅验证页面可打开，或只保存数据卷归档，均不构成 Phase 16 完成。

## 2. 产品范围与非目标

### 2.1 本阶段交付

- 一份版本化 Linux 产品 Bundle，包含 Compose、release manifest、checksum、配置模板、运维说明和生命周期入口。
- 一个 Linux `amd64` 生命周期工具镜像，以及 9 个 Linux `amd64` 产品镜像的不可变 digest 清单。
- 六类 current v2 插件的 Linux `amd64` 受信 catalog；已有 legacy 包作为历史制品保留，不作为 Phase-16-05/06 的运行或验收前置。
- 安全初始化、唯一 edge、统一登录、双 Frontend、诊断、加密备份/恢复、恢复后持续使用和失败恢复能力。
- 一个真实 Linux `amd64` 最终候选矩阵和可机器校验的脱敏证据集合。

### 2.2 明确不做

- macOS、Windows 或 `linux/arm64` 的产品支持、宿主适配、运行验收和支持声明。
- 原生宿主服务、GUI 安装器、自动更新器或 Kubernetes 资源。
- 将冻结的 `scripts/*.ps1` 扩展为当前产品生命周期实现。
- 在线热备、历史/跨版本升级、旧版 fixture 与历史插件迁移、生产高可用、镜像签名/SBOM/CVE 平台或通用灾备系统。
- 删除 Phase-16-01 已经生成的额外架构制品；这些制品保留为历史构建结果，但不进入后续验收和支持合同。

## 3. 真实基线、验收环境与开工约束

### 3.1 输入基线

- Phase 15 已交付双角色、双 Frontend、六插件、三源告警和完整 Compose 能力。
- Phase-16-01 已在 `1.13.1` 完成 release manifest、Bundle 骨架、制品 digest、临时 registry 闭环和真实 Linux `amd64` runtime 验证。
- Phase-16-01 的同名实施记录是其实际执行结果的权威证据；后续规划调整不重写历史结果。
- 后续批次只消费 Phase-16-01 已确认的 Linux `amd64` 制品合同。已有 `arm64` metadata 或制品不构成后续前置和完成条件。

### 3.2 唯一支持与验收环境

| 项目 | 固定要求 |
| --- | --- |
| 宿主 | WSL2/Linux 或原生 Linux，CPU 架构为 `amd64` |
| 工作区 | 位于 Linux 文件系统，不使用 Windows 挂载目录作为活动 checkout |
| 容器运行时 | 一个可访问的 Linux `amd64` Docker server |
| Compose | Docker Compose v2，具体最低版本由 Phase-16-02 的 `doctor` 合同锁定 |
| 资源 | 开工记录 CPU、内存、磁盘、Docker server、Compose、端口和 registry 可用性 |
| 验收方式 | 从独立交付目录按 digest 拉取候选制品并运行真实容器，不用交叉编译、QEMU 或静态检查替代 |

单批开工只要求其直接依赖和 Linux `amd64` 环境就绪。外部正式 registry 不可用时，可用隔离的 loopback registry 完成候选 push/pull/digest 与不重建晋升探测，并在实施记录中明确发布边界。

### 3.3 分支与实施开工

- Phase-16-01 已完成并使用 `develop/1.13.1`；不得重建、重命名或重新编号该已推送分支。
- Phase-16-02 至 Phase-16-06 开工前统一运行 `scripts/start-development-batch.sh Phase-XX-YY --remote <primary>`；脚本 fetch 主远程、从当时最新 `main` 创建表 4 中的目标分支，并同步根 `VERSION`、`.env.example` 和两个 Frontend 的 npm 元数据。不得手工只创建分支而遗漏版本同步。
- 若本地已有同名分支，先核对与远程和目标基线的关系；不得静默 reset、覆盖或重命名已推送分支。
- 每批只完成其计划范围；版本同步提交属于分支开工引导，固定门禁通过后才可更新同名实施记录、确认批次完成并停止。只有合入 `main` 后，该版本才成为已发布的完成产品版本。

## 4. 权威批次、版本与分支分配

Phase 16 对应 `1.13.x`。patch `0` 为阶段基线，六个批次保持既定顺序，不因本次平台范围收敛而重编号：

| 批次 | 目标版本 | 分支 | 交付主题 | 状态 |
| --- | --- | --- | --- | --- |
| Phase-16-01 | `1.13.1` | `develop/1.13.1` | 制品与发布清单闭环 | 已完成 |
| Phase-16-02 | `1.13.2` | `develop/1.13.2` | 共享产品生命周期与安全初始化闭环 | 待实施 |
| Phase-16-03 | `1.13.3` | `develop/1.13.3` | 统一登录与双 Frontend 产品体验闭环 | 待实施 |
| Phase-16-04 | `1.13.4` | `develop/1.13.4` | 一致备份恢复与诊断闭环 | 待实施 |
| Phase-16-05 | `1.13.5` | `develop/1.13.5` | 当前版本数据与恢复闭环 | 待实施 |
| Phase-16-06 | `1.13.6` | `develop/1.13.6` | Linux 产品矩阵与阶段收口 | 待实施 |

本次范围变化不改变批次拆分或执行顺序，已推送的 `develop/1.13.5` 不重编号或改名。

本表是 Phase 16 唯一权威的批次到版本、分支映射。规划文档提交在 `update` 上，不修改根 `VERSION`。

## 5. 跨批次顺序、依赖与拆分理由

```text
16-01 artifacts (complete)
        │
        ▼
16-02 lifecycle
        │
        ▼
16-03 edge + frontends
        │
        ▼
16-04 backup/restore
        │
        ▼
16-05 current data + recovery continuity
        │
        ▼
16-06 Linux product acceptance
```

1. 生命周期必须先锁定 manifest、digest、Bundle 和资源归属，后续批次不得绕过它直接编排产品。
2. 统一登录和双 Frontend 必须建立在唯一 edge 与稳定生命周期之上。
3. 备份恢复必须覆盖已经稳定的配置、身份、业务、搜索、可观测和插件逻辑状态。
4. 当前产品数据恢复必须复用 Phase-16-04 正式备份恢复能力；Phase-16-05 增量是当前真实数据配方、跨域事实对照和恢复后再备份恢复的产品级验收，不重写引擎或维护另一套回退路径。
5. 最终批次只消费前五批合同并运行候选矩阵，不首次实现产品功能。

## 6. 交付架构与信任边界

```text
versioned bundle
  ├─ compose.yaml
  ├─ release-manifest.json
  ├─ checksums
  ├─ config templates
  └─ operations guide
          │
          ▼
short-lived lifecycle container
          │
          ▼
project-scoped product containers and volumes
          │
          ▼
single edge origin
```

- release manifest、镜像 digest、插件 catalog 和 backup manifest 都是受信输入；校验失败必须在创建或修改产品资源前终止。
- Docker endpoint 只暴露给短命生命周期容器；常驻产品容器不得取得 Docker socket。
- 所有创建、停止、删除和恢复操作必须同时校验 project、installation token、service/resource label 与 digest。
- 不使用全局 prune、宽泛名称匹配、未解析变量或宿主根目录递归操作。
- 日志、证据和诊断包必须脱敏，不输出口令、token、cookie、私钥或完整连接串。

## 7. Linux 制品与发布合同

### 7.1 产品镜像

以下 9 个逻辑产品镜像必须发布并验证 Linux `amd64` digest：Backend、Business Worker、Search Indexer、用户 Frontend、管理 Frontend、Router、Marshaller、Monitor、Redis Exporter。

- release manifest 记录逻辑名、版本、Git revision、image reference、digest、`os=linux`、`arch=amd64` 和构建时间。
- 镜像以 numeric non-root user 运行；可行时使用 read-only root filesystem、显式 writable mount 和 capability drop。
- 按 digest 拉取后真实启动，运行架构、版本和源码 revision 与 manifest 一致。
- 第三方 MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 镜像锁定 Linux `amd64` digest；若版本变化，只运行直接受影响的数据、协议和 health 回归。

### 7.2 插件 catalog

- Redis、MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 的 current v2 包固定为 Linux `amd64`。
- archive、entrypoint、Schema、包 digest、目标版本和 catalog 签发关系可独立校验。
- Monitor 只从只读、镜像内受信 catalog 选择与 server 架构匹配的包，不从可写 volume 建立信任。
- 已有 Redis v1 legacy 包与兼容实现保留，但不要求本阶段后续批次执行历史迁移；v1/v2 指插件 Manifest 格式，不是 Redis 数据库版本。

### 7.3 生命周期镜像与 Bundle

- `lifecycle/` 使用一个 Go module 和一个 `cmd/gopulse` 命令树，构建为 Linux `amd64` 工具镜像。
- Bundle 以 checksum 和 allowlist 限定内容，不包含源码构建工具、明文 Secret 或未登记可执行文件。
- Compose 只按 release manifest 中的 digest 消费候选制品；晋升不得重建镜像或插件包。
- 正式 registry 不可用不阻断源码实现，但最终发布状态必须区分“本地验证”“候选可拉取”和“外部已发布”。

## 8. 共享产品生命周期合同

交付入口由 Bundle 中的 Docker Compose 调用生命周期容器。产品语义至少包含：

```text
version
doctor
init
up
down
status
logs
verify
backup
restore
```

- `doctor` 在创建资源前检查 Docker server、架构、Compose 版本、磁盘/内存、端口、manifest、digest 和安装目录。
- `init` 只在空安装目录生成私有 Secret、配置、installation token 与 state；文件同目录临时写入、同步并原子替换。
- `up` 先运行初始化 job，再启动常驻服务；只向宿主暴露唯一 edge。
- `verify` 默认只读，禁止隐式 pull/build/recreate/restart/修复或写业务数据。
- `status` 和 `logs` 使用稳定结构与退出码；Secret 永不出现在 stdout、stderr 或证据文件。
- 每个变更操作使用安装级互斥锁和 operation id；并发、重入和中断均得到可恢复终态。
- `down` 默认保留用户数据；破坏性清理必须显式确认并受强归属保护。

## 9. 唯一 origin 与双 Frontend 产品合同

- Edge 是唯一宿主入口；Backend 不从产品 profile 单独发布宿主端口。
- 用户 Frontend、管理 Frontend 和 API 使用同一 origin、统一 cookie/session/CSRF 与登出语义。
- 用户角色不能进入管理能力；管理员可进入管理 Frontend，越权 API 必须返回稳定状态且不泄露敏感信息。
- 两个 Frontend 共享 token、排版、颜色、间距、组件状态与错误模型，不复制一套漂移的视觉基础。
- 核心页面覆盖 loading、empty、partial、stale、permission denied 和 backend unavailable。
- 在 Linux 浏览器验收中覆盖桌面/窄屏、键盘导航、焦点可见、语义标签以及至少一个非 UTC 时区。

## 10. 一致备份与恢复合同

- Backup preflight 校验健康、空间、权限、manifest 和目标路径；进入维护窗口后阻止新写入并排空异步队列。
- MySQL 使用事务一致逻辑导出；Elasticsearch 和 VictoriaMetrics 使用其受支持的 snapshot/export 能力；RabbitMQ/Kafka 保存可重建拓扑、offset 和排空证据，不复制活动数据目录。
- Monitor 只导出逻辑身份、配置、授权、历史和版本意图，不携带运行中进程、宿主路径或平台二进制。
- Backup format v1 包含 schema、source version、artifact digest、文件清单、逐项 checksum、逻辑计数、时间范围、拓扑和完成标记。
- 完整性通过后再加密；口令从终端无回显或显式 secret source 读取，不进入参数、环境转储、日志或实施记录。
- Restore 只允许进入空且受当前 installation token 控制的 project；先验证格式、版本、digest、checksum、加密和空间，再创建资源。
- 失败恢复不得发布半恢复状态；清理仅限当前 operation 创建且具备完整归属的资源。
- 恢复后核对数据库、搜索、监控、角色、会话失效策略、审计和关键业务事实，并验证能够继续新写入。

## 11. 当前版本数据与持续恢复合同

- Phase-16-05 使用 `1.13.5`，Phase-16-06 使用 `1.13.6`；各批独立冻结候选，同批的 A/B/C 使用相同 manifest 和 Linux `amd64`，不测试跨版本或跨 manifest 恢复。
- 可先完善当前产品/验收入口，再在独立新安装项目通过正式 API、浏览器及 current v2 插件真实运行生成数据；不要求历史源码、旧数据或 legacy 插件。
- 复用 Phase-16-04 format v1、停写/排空、独立 inspect、空目标恢复和强归属合同；不另建备份格式，不覆盖用户现有环境。
- 按一致切点固定身份、业务、搜索、指标、告警、审计与插件事实；A 备份恢复到 B，B 继续写入/采集/告警后再次备份恢复到 C，比较原有与新增事实并验证继续使用。
- 对成功恢复的非空目标重复恢复应安全拒绝且不改数据；代表导入失败/中断应可按正式合同重试或清理后恢复，不出现伪 ready。
- evidence 绑定候选、数据配方、备份 checksum、事实摘要和实际命令；最终矩阵复用 runner 与配方，而非直接使用 `1.13.5` 备份或结果冒充 `1.13.6` 验收。
- 不把同版本恢复、重启、测试数据生成或版本号更新记为跨版本升级；支持文档不得新增未经验证的历史升级支持声明，也不删除已有兼容能力。

## 12. Linux 产品支持合同

最终候选必须在一个真实 Linux `amd64` Docker server 上，从独立解压的 Bundle 完成：

1. `doctor/init/up/verify/status/logs/down/up` clean-install 闭环。
2. 唯一 edge、双 Frontend、统一登录、用户/管理员角色与越权拒绝。
3. 六插件真实采集、三源告警、详情/指标/事件/日志/操作历史。
4. 加密 backup、同架构空 project restore、恢复后继续写入。
5. 当前真实数据经恢复继续写入、采集与告警，再备份恢复后新旧事实保持；重复非空目标恢复安全拒绝及代表导入失败/中断可恢复。
6. Linux 文件权限、signal、中断、端口占用、磁盘不足、daemon 不可用与 project 隔离。
7. 正常清理和失败清理均不影响无关 Docker 资源或用户文件。

证据 JSON 必须记录 host OS、CPU、Docker server OS/arch、Compose、Bundle checksum、release manifest digest、镜像/plugin digest、project/token hash、场景结果和脱敏日志摘要。最终聚合器拒绝缺字段、候选 digest 不一致、模拟架构或失败场景。

## 13. 故障、安全与资源所有权

- 端口冲突、磁盘不足、manifest 篡改、错误口令、数据导入失败、daemon 中断和用户取消必须有稳定退出码与可操作提示。
- Secret 不得进入镜像层、Compose 默认值、命令行参数、日志、诊断包或 evidence。
- 所有删除、覆盖和回滚先解析精确目标并验证强归属；不使用全局 Docker 清理。
- 安装目录、backup、restore temp 和诊断输出均采用最小权限与原子完成标记。
- 不相关改进记录为后续事项，不扩张本阶段验收范围。

## 14. 各批次职责边界

### Phase-16-01：制品与发布清单闭环

- 已完成 `1.13.1` 制品、manifest、Bundle 骨架、临时 registry 和真实 Linux `amd64` runtime 门禁。
- 已生成的额外架构制品作为历史结果保留，不是后续支持或验收前置。

### Phase-16-02：共享产品生命周期与安全初始化闭环

- 实现 Linux 产品生命周期、doctor、强归属、安全 init、唯一 edge 启停和只读 verify。
- 不实现 backup/restore/upgrade 或 Frontend 产品体验改造。

### Phase-16-03：统一登录与双 Frontend 产品体验闭环

- 完成唯一 origin、共享前端基础、统一登录、角色分流、错误状态、响应式和时区验收。
- 不改变持久数据迁移或备份格式。

### Phase-16-04：一致备份恢复与诊断闭环

- 实现 format v1 加密备份、同架构空 project 恢复、失败清理、恢复后写入和脱敏诊断。
- 不实现 `1.9.4` 升级。

### Phase-16-05：当前版本数据与恢复闭环

- 以 `1.13.5` 当前候选通过正式入口生成真实数据，完成 A/B/C 同 manifest 两次恢复、继续使用和代表故障验收；不涉及旧版数据。
- 权威拆分计划为 `Phase-16-05-当前版本数据与恢复闭环.md`，原历史升级方案由其替代；已有历史尝试记录保留，新实施记录与新方案同名。
- 不改变 Phase-16-04 的通用 backup format。

### Phase-16-06：Linux 产品矩阵与阶段收口

- 对同一 `1.13.6` 候选运行完整 Linux 产品矩阵并聚合证据。
- 只修复矩阵暴露的直接阻断，不新增功能或扩展平台范围。

## 15. 阶段级验收标准

### 15.1 制品与安装

1. 9 个产品镜像、生命周期镜像、6 个 current 插件和 6 个第三方镜像均有锁定的 Linux `amd64` digest。
2. Bundle checksum、manifest、allowlist 和按 digest 拉取可验证；产品路径无源码构建。
3. 在真实 Linux `amd64` Docker server 上从空目录完成安装和真实运行。

### 15.2 生命周期、入口与双 Frontend

1. 全部生命周期命令有稳定参数、退出码、锁、归属和中断语义。
2. 只有 edge 暴露宿主端口；统一登录、角色隔离、CSRF/session 和登出正确。
3. 两个 Frontend 的核心状态、桌面/窄屏、键盘和非 UTC 时间展示通过。

### 15.3 当前数据、备份与持续恢复

1. Backup format v1 一致、加密、可独立检查且不泄露 Secret。
2. Linux `amd64` backup 可恢复到同架构空 project，关键事实一致并能继续写入。
3. 恢复项目继续产生业务、采集、告警与审计事实，再次备份恢复后原有与新增事实一致且可继续使用；非空目标重复拒绝、代表导入失败/中断及恢复通过。

### 15.4 Linux 产品回归

1. 最终 evidence 来自真实 Linux `amd64` host/server 并绑定同一候选 manifest。
2. 六插件采集、双 Frontend、业务、搜索、可观测和三源告警集成结果通过。
3. 路径、权限、端口、signal、daemon、磁盘、清理和隔离场景通过。
4. Phase 15 直接受影响的 Compose 产品能力无回归。

### 15.5 阶段完成条件

Phase 16 只在以上标准全部通过、6 份拆分方案均有同名真实实施记录、根完成版本为 `1.13.6`、全部批次按顺序合入主线且无阻断问题时完成。Phase 17 继续执行 Kubernetes 前工程质量收口；Phase 16 不提前宣称 Milestone 4 完成。

## 16. 验证策略与固定阶段门禁

按风险最小化原则分层验证：开发中运行最小受影响包检查，每批最终 diff 只运行拆分方案固定门禁一次；仅在共享基础设施、安全边界、持久数据或公开合同存在具体风险时扩展。

最终候选的固定入口为：

```bash
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
docker compose --profile acceptance run --rm acceptance phase16 --evidence /evidence/linux-amd64.json
python3 scripts/verify-phase16-evidence.py --linux dist/evidence/linux-amd64.json
```

Phase-16-05/06 的当前数据恢复子门禁统一为以下拟扩展入口（参数由 Phase-16-05 实现/对齐，不声称当前已支持）：

```bash
scripts/verify-backup-restore.sh --manifest dist/release-manifest.json --platform linux/amd64 --current-product
scripts/verify-backup-restore.sh --manifest dist/release-manifest.json --platform linux/amd64 --current-product --failure-matrix
```

第一项覆盖当前数据生成、A/B/C 两次恢复、继续使用和非空目标重复拒绝；第二项覆盖代表导入失败与中断。聚合 runner 与子命令共享同候选 evidence，不重复执行已通过且输入/环境未变的场景。不要求历史 fixture 构建或 `verify-upgrade` 门禁。

命令名可在对应实现批次中按仓库事实做等价调整，但必须先更新拆分计划和总方案；不得以临时命令绕过证据结构或候选 digest 绑定。

固定阶段回归只覆盖：

- release manifest、Bundle 和 Linux `amd64` runtime；
- lifecycle clean install、唯一 edge、双 Frontend、六插件、三源告警；
- 当前真实数据、同 manifest backup/restore、恢复后写入与再次恢复；
- 直接受影响的 Phase 15 Compose 回归；
- 归属、Secret、失败注入、清理和 evidence 聚合。

## 17. 有限前置确认与禁止猜测清单

| 批次 | 开工前必须确认 | 未确认时的处理 |
| --- | --- | --- |
| Phase-16-01 | 已按原计划完成 | 以同名实施记录为准，不重跑历史门禁 |
| Phase-16-02 | Linux Docker/Compose 入口、工具容器 endpoint、版本下限 | 记录实际能力；不新增第二套生命周期 |
| Phase-16-03 | edge 路由、API 合同、浏览器 runner | 只修直接阻断，不扩大页面或 API 范围 |
| Phase-16-04 | MySQL/ES/VM 导出恢复 API、队列排空、加密临时文件 | 任一权威数据域不可一致恢复则阻断 |
| Phase-16-05 | 当前 backup/restore 公共接口、数据域对照、隔离项目与 Linux 环境 | 可先实现入口再生成当前数据；真实运行前冻结可拉取候选，不要求旧版 fixture |
| Phase-16-06 | Linux `amd64` host/server、候选 digest、独立交付目录 | 环境或候选不一致则阻断最终阶段收口 |

不得猜测 registry 已发布、历史数据已迁移、Secret 已脱敏、恢复可继续写入或候选已在真实容器运行；所有这些结论必须来自实际命令与实施记录。

## 18. 实施记录、版本与提交要求

- 每批完成前创建或更新 `dev/logs/Phase-16/` 下与拆分方案同名的 Markdown 记录。
- 记录实际变更文件、验证命令及结果、失败轮次、偏差、发布边界、已知限制和后续事项。
- Phase-16-02 至 Phase-16-06 完成时分别把根 `VERSION` 更新为 `1.13.2` 至 `1.13.6` 并纳入该批提交。
- 每批只提交本批文件，不包含用户或其他任务的未跟踪/未提交内容。
- 规划修订留在 `update`，不改变 `VERSION`。

## 19. 停止条件与 Phase 17 交接

当某批固定验收全部通过且无阻断问题时，更新实施记录、版本并提交后立即停止；不做机会性重构、覆盖率扩张或无直接风险依据的附加测试。

Phase 16 完成后交给 Phase 17 的固定输入是：

- `1.13.6` Linux `amd64` 完整 Compose 产品和不可变 release manifest；
- 版本化 Bundle、共享容器化生命周期、唯一 edge 与双 Frontend；
- 六插件、三源告警、backup format v1、当前数据配方与同 manifest 持续恢复合同；
- 一组来自真实 Linux `amd64` 候选运行的可复核脱敏证据。

Phase 17 不得把 Kubernetes 作为验证 Compose 产品的前置条件；Phase 18 以后在直接受影响处复用上述 Linux 产品合同迁移到 Kubernetes。

