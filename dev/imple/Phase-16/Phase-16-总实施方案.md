# Phase 16：跨平台产品化与双前端交付总实施方案

> 规划基线（2026-09-13）：主远程 `upstream/main` 提交 `0e62b3d87b3a509b3494397ed5041300094350a3`，根完成版本为 `1.12.7`，Phase 15 的双角色、双 Frontend、六插件、三源告警和 Compose 收口能力已合入主线。本文档只规划 Phase 16，不修改 `VERSION`，也不把任何尚未运行的跨平台检查写成已完成。

Phase 16 使用 `1.13.x` 版本线，拆分为 6 个执行批次。本文档是 Phase 16 批次顺序、目标版本和开发分支的唯一权威来源。

## 1. 阶段目标

把 Phase 15 已完成的 Linux/Bash/本地构建 Compose 系统收敛为不依赖宿主 Go、Node.js、Python、数据库客户端或项目源码即可安装、运行、升级、备份、恢复和诊断的双前端产品：

```text
下载同一份 OS 中立版本化交付包
  → Docker Compose 调用同一生命周期工具镜像
       ├─ linux/amd64
       └─ linux/arm64
  → Docker Compose + 固定 release manifest
       ├─ linux/amd64 自研镜像与六插件
       └─ linux/arm64 自研镜像与六插件
  → 唯一 loopback Web origin
       ├─ user        → 用户 Frontend
       └─ super_admin → 管理 Frontend
  → 可重复升级、加密备份、空项目恢复和脱敏诊断
```

阶段完成必须同时证明：

- Linux `amd64`、macOS `arm64` 与 Windows `amd64` 三类真实宿主使用同一产品合同启动完整系统；macOS/Windows 运行 Linux 容器，不把自研服务改造成原生 daemon/service。
- 全部产品自研镜像、两个 Frontend 和六类受管插件形成真实可运行的 `linux/amd64`、`linux/arm64` 制品；交叉编译或 manifest inspection 只能作为构建证据，不能代替真实宿主运行。
- 用户只需受支持版本的 Docker Engine/Docker Desktop、Docker Compose v2 与对应交付包；产品路径不要求 Git、Bash、Go、Node.js、npm、Python、curl 或基础设施客户端。
- 初始化、启动、停止、状态、日志、只读验证、诊断、备份、恢复和升级由一个双架构 Linux 容器生命周期实现提供；三类宿主都通过 Docker Compose 调用它，不新增 macOS/Windows 原生实现，也不维护 Bash/PowerShell 两套业务逻辑。
- 浏览器只访问一个 loopback origin；登录、会话恢复和数据库当前角色将 `user` 与 `super_admin` 安全送入两个独立 Frontend，Backend 继续执行最终授权。
- 从锁定的 `1.9.4` 主线制品和持久数据可重复升级到本阶段；升级失败可由升级前备份恢复，不用手工改库、删卷或信任可写卷中的未知二进制。
- 加密备份和空项目恢复覆盖 MySQL 权威事实、Logs/Events 历史、VictoriaMetrics 历史、插件逻辑状态、角色、告警、审计及可移植配置；缓存、搜索投影和已排空消息可按合同重建。
- 正常、失败、中断和恢复路径均只操作经 project、service、workspace token 与 release digest 联合证明归属的资源，不误删用户其他容器、网络、卷、镜像、文件或端口监听。

只生成多架构镜像、只让旧 Bash 脚本在三个环境语法通过、只在 WSL 中调用 PowerShell、只保存数据卷 tar 包，或只验证两个页面能打开，均不构成 Phase 16 完成。

## 2. 产品范围与非目标

### 2.1 本阶段交付

- 一份 OS 中立交付包与一个双架构 Linux 生命周期工具镜像；Linux、macOS 和 Windows 均从本机 Docker Compose/Terminal 调用同一命令合同。
- 固定 release manifest、版本化 Compose 资产、交付包 checksum、不可变镜像 digest 与候选制品到正式版本的同 digest 晋升流程。
- Backend、Business Worker、Search Indexer、用户 Frontend、管理 Frontend、Router、Marshaller、Monitor 和独立 Redis Exporter 的 `linux/amd64`、`linux/arm64` 镜像。
- Redis、MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 六类受管 Exporter 当前包的双架构 release catalog；`1.9.4/linux/amd64` Redis v1 来源只作为锁定升级输入。
- `gopulse init/up/down/status/logs/verify/doctor/backup/restore/upgrade/version` 产品命令及跨进程互斥、强归属和安全输出。
- 产品初始化时的随机 Secret 生成、原子配置写入、已有配置拒绝覆盖、端口/目录/资源/架构/版本预检。
- 只发布一个 `127.0.0.1` edge 端口的同源双 Frontend 路由；Backend 和全部内部组件不直接发布宿主端口。
- 两个 Frontend 的共享设计令牌和公共状态/交互基础，以及独立信息架构、独立 bundle、独立路由和独立权限恢复。
- 有界停写与队列排空后的加密备份、空 project 恢复、恢复后重建/校验及失败清理。
- 从主线提交 `102aa4fa9bb5256dfd0733f42454b0ec5deaf043` 的 `1.9.4` Compose 数据升级到 Phase 16 当前版本的固定路径。
- 三类真实宿主的 clean install、保留状态重启、升级或恢复、双角色浏览器闭环、信号、路径、行尾、文件共享、端口和资源清理矩阵。

### 2.2 明确不做

- 原生 Windows Service、macOS LaunchDaemon、原生数据库/消息/搜索服务，或把 GoPulse 自研进程直接安装到宿主。
- macOS/Windows 原生 CLI、平台 adapter、GUI 安装器或平台专用启动器；平台差异只作为 Docker Desktop/Compose 兼容性验收项处理。
- 复活或扩展冻结在 `0.2.1` 的 `scripts/*.ps1`；它们继续是历史基线，不成为 Phase 16 产品入口。
- 为 Linux、macOS、Windows 分别实现一套生命周期、备份、升级或诊断逻辑。
- Kubernetes、Ingress、集群对象采集、Helm、Operator、生产级高可用或跨地域容灾。
- 公网暴露、TLS 证书自动化、外部域名、反向代理集成或云厂商安装器；Phase 16 的支持入口固定为宿主 loopback。
- 新业务、新插件类型、同类插件多实例、外部告警通知、细粒度 RBAC 或新的 Metrics/Logs/Events 协议。
- 在线无停机升级、跨版本任意回退、在线强一致热备、增量备份、定时备份或远程备份仓库。
- 默认依赖/CVE 审计、SBOM/签名体系、性能压测、容量承诺、独立代码 Review 或覆盖率活动；具体失败有直接风险依据时再单独处理。

## 3. 真实基线、验收环境与开工约束

### 3.1 Phase 15 输入基线

- 根版本是 `1.12.7`；两个 Frontend package、镜像元数据与受管插件当前版本均受现有版本门禁约束。
- 当前 `deploy/compose.yaml` 从源码构建本地 `gopulse/*:<VERSION>` 镜像，`scripts/dev.sh`、`verify.sh`、`down.sh` 依赖 Bash/Git/POSIX 工具；这仍是开发路径，不是 Phase 16 产品路径。
- 用户 Frontend 是当前唯一 edge，对 `/admin/` 代理内部管理 Frontend，对 `/api/v1/` 代理 Backend；日常 Compose 仍额外发布 Backend loopback 端口，产品交付必须收窄为唯一 edge 端口。
- `user|super_admin`、引导超级管理员保护、数据库实时授权、HttpOnly Cookie、三源告警状态机、管理审计、六类单实例插件和六组件指标均为必须保留的产品合同。
- Phase 15 的升级验收来源是 `1.11.5`，不能代替本阶段明确要求的 `1.9.4` 升级；Phase 14/15 的受信插件 catalog 也未证明 `linux/arm64`。
- 当前 `.env.example` 含开发用固定凭据。Phase 16 产品初始化必须生成随机 Secret，示例文件不得被交付入口静默复制为真实产品配置。

### 3.2 可安排的真实宿主矩阵

以下环境来源已定位，足以确定批次顺序；表中信息只是开工和调度输入，不是 Phase 16 通过证据：

| 宿主 | 规划时可用来源 | Phase 16 固定运行方式 | 开工/验收前必须重新确认 |
| --- | --- | --- | --- |
| Linux `amd64` | Phase 15 最近在维护中的 WSL2/Linux/Bash/单 Docker daemon 环境完成真实 Compose 验收 | Linux 文件系统中的独立交付目录；Docker Engine 或 Docker Desktop WSL integration；原生 `linux/amd64` 容器 | `uname`、Docker server arch、Compose v2、磁盘/内存、Linux 文件路径、唯一 daemon 与干净随机 project |
| macOS `arm64` | 当前规划工作区实际为 macOS `27.0`、`arm64`；Docker CLI `29.6.1` 与 Compose `v5.3.0` 已安装 | APFS 本地交付目录；Docker Desktop Linux containers；原生 `linux/arm64` 容器，不强制 amd64 模拟 | 规划时 Docker daemon 未运行；实施前必须启动并确认 server 为 `linux/arm64`、文件共享、资源配额和可用磁盘 |
| Windows `amd64` | 仓库 Phase 0 记录证明曾有原生 Windows/Docker Desktop 环境；本阶段按仓库平台规则重新安排真实 Windows 宿主 | NTFS 用户目录中的同一 OS 中立交付包；Windows PowerShell/Terminal 直接运行 Docker Compose 产品命令；Docker Desktop Linux containers | 当前规划会话不能替代 Windows 实机确认；必须记录 Windows build、Docker/Compose、Linux container mode、NTFS 路径、文件共享与可用资源 |

约束如下：

- Phase-16-01 开工前为三个环境分别记录可执行的 host owner、运行窗口和最低资源探测；任一目标环境无法安排时，先在 `update` 修订总/分方案，不创建一个无法完成的后续批次分支。
- 每个实际批次只在其直接需要的平台运行最小检查；最终 Phase-16-06 必须在三类真实宿主上运行同一候选交付包合同。
- Windows 验收必须从 PowerShell/Terminal 和 NTFS 路径执行，不能在 WSL shell、`/mnt/c` checkout 或 Linux 路径中冒充 Windows。
- Linux 验收的活动 checkout/交付目录必须位于 Linux 文件系统；如与 Windows 共用 Docker Desktop，两个矩阵串行运行并使用不同 project/token，避免交叉归属。
- macOS 必须运行 `linux/arm64` runtime image；Rosetta/QEMU 可用于有限构建探测，但不得成为最终产品运行证据。
- 支持的 Docker Engine/Desktop/Compose 版本范围由 Phase-16-01 对三台真实环境的共同可用能力确定并写入 `docs/platform-support.md`；本计划不根据单台客户端版本猜测最低版本。

### 3.3 分支与实施开工

- 每批开始前 fetch 主远程，从当时最新 `upstream/main` 创建总方案分配的独立 `develop/x.x.x` 分支；只有同一批或同一 PR 的跟进工作才继续原分支。
- 前一批必须已合入主线且实施记录、版本和固定门禁一致，下一批才开工；不得在一个长期分支中完成全部 Phase 16。
- 开工发现锁定第三方镜像无目标架构、Windows 文件共享不可用、交付 registry 无法提供 immutable digest，或 `1.9.4` fixture 不可重建时，先记录最小证据并修订计划，不静默降低平台或数据合同。
- 计划、文档、治理工作可留在 `update`；产品代码、运行验收和版本更新只能在分配的 `develop/1.13.x` 分支完成。

## 4. 权威批次、版本与分支分配

Phase 16 的 patch `0` 是阶段分配基线，不表示产品已经完成 `1.13.0`，也不创建空提交。6 个执行批次按下表顺序实施：

| 批次 | 目标版本 | 开发分支 | 可独立验收的闭环 |
| --- | --- | --- | --- |
| Phase-16-01 | `1.13.1` | `develop/1.13.1` | 双架构产品/生命周期工具镜像、六插件包、release manifest 与单一 OS 中立 Bundle 的不可变制品闭环 |
| Phase-16-02 | `1.13.2` | `develop/1.13.2` | 共享 `gopulse` 生命周期、安全初始化、唯一 edge、状态/日志/验证/诊断与强归属闭环 |
| Phase-16-03 | `1.13.3` | `develop/1.13.3` | 统一登录、双 Frontend 共享视觉/状态/响应式/键盘/时区产品体验闭环 |
| Phase-16-04 | `1.13.4` | `develop/1.13.4` | 停写排空、加密一致备份、空 project 恢复、跨架构插件逻辑状态恢复闭环 |
| Phase-16-05 | `1.13.5` | `develop/1.13.5` | 锁定 `1.9.4` 数据到当前版本的预检、升级、验证与备份恢复失败路径闭环 |
| Phase-16-06 | `1.13.6` | `develop/1.13.6` | Linux amd64、macOS arm64、Windows amd64 真实宿主矩阵与 Phase 16 阶段收口 |

同一批次的全部实现提交共享该批目标版本。批次完成时同步根 `VERSION`、两个 Frontend package/lockfile、生命周期工具镜像、release manifest、镜像和插件元数据；本次规划不修改这些版本文件。

已创建或已推送的分支不得静默改名。实施前若调整批次数量或顺序，必须先更新本表并重算所有尚未创建的分支；已经推送的分支需要与用户协调后再调整。

## 5. 跨批次顺序、依赖与拆分理由

```text
16-01 不可变双架构制品与交付包
   ↓
16-02 共享产品生命周期与唯一 edge
   ↓
16-03 双 Frontend 产品体验与统一登录
   ↓
16-04 加密一致备份与空项目恢复
   ↓
16-05 1.9.4 升级与失败恢复
   ↓
16-06 三类真实宿主矩阵与阶段收口
```

本阶段超过三个批次，是因为存在五类需要独立失败边界和真实证据的风险：

1. 多架构镜像、六插件包、第三方基础镜像和交付 registry 决定后续三平台能否运行，必须先独立建立不可变制品事实，不能边做升级边换架构。
2. 共享生命周期涉及工具容器进程、挂载路径、Docker Engine/Compose、配置 Secret、操作锁与资源清理；它是全部产品操作的控制面，不能分散进各平台脚本。
3. 两个 Frontend 已经物理拆分，但共享设计令牌、完整页面状态、窄屏、键盘焦点、时区和统一登录仍是独立产品风险，不应夹在底层制品或数据恢复中验收。
4. 备份/恢复直接处理全部持久事实和 Secret，必须先于升级交付；升级失败恢复应复用已经验收的产品备份，而不是临时复制卷。
5. 最后一批必须只消费前五批稳定合同，在三个真实宿主上验证实际交付包；如果同时首次实现功能，跨平台失败将无法定位到制品、生命周期、UI 或数据域。

每批测试和文档随能力完成。Phase-16-06 不做默认架构 Review、依赖审计或新功能；只允许修复固定矩阵暴露的直接阻断问题，并按影响范围重验。

## 6. 目标交付架构与信任边界

### 6.1 交付拓扑

```text
OS-neutral versioned bundle
  ├─ deploy/product/compose.yaml
  ├─ release-manifest.json
  ├─ checksums
  └─ platform-support / upgrade / backup documentation
          │
          ▼
Docker Compose-invoked lifecycle tool image
  ├─ linux/amd64 + linux/arm64 from one Go module
  ├─ daemon/arch/path/port/disk preflight
  ├─ private config + operation lock + ownership state
  ├─ pull images by immutable digest
  └─ operate only the proven owned project through Docker Engine
          │
          ▼
Linux containers on one Docker daemon
  ├─ frontend edge ── / + /admin/ + /api/v1/
  ├─ backend / worker / indexer
  ├─ monitor / router / marshaller / managed exporters
  └─ MySQL / Redis / RabbitMQ / Kafka / Elasticsearch / VictoriaMetrics
```

### 6.2 信任边界

- 交付包中的 release manifest 是版本、源码 revision、Compose 资产、生命周期工具镜像 digest、各逻辑镜像 index/platform digest、第三方镜像 digest、插件 catalog 和受支持升级来源的唯一发布事实。
- 产品运行只接受 manifest 中的 immutable digest，不以可变 `latest`、本地同名 tag、可写插件卷或上传包建立信任；候选到正式发布只移动引用，不重新构建内容。
- 短命生命周期工具容器通过 Docker Desktop/Engine 明确支持的本地端点执行编排，仅在产品命令期间存在并强制 project/token/digest 归属；Frontend、Backend、Monitor 等常驻产品容器不取得该端点。
- `.env`/Secret、生命周期状态和 backup 密钥材料相互分离。非敏感 state 可记录 project、版本、manifest digest 与资源 ID；Secret 不进入命令行、state、日志或 evidence JSON。
- Browser 只访问 frontend edge 的一个 loopback origin。Backend、admin-frontend 和全部数据/可观测组件只在内部网络；Frontend 路由守卫不替代 Backend 的数据库权威授权。
- Monitor 继续是插件配置、Secret、desired state 与进程的唯一所有者。跨架构恢复只导入逻辑状态，由目标架构的受信 catalog重新物化二进制，绝不执行备份中的旧 ELF。

## 7. 多架构制品与发布合同

### 7.1 产品镜像集合

必须发布并验证以下 9 个逻辑产品镜像的 `linux/amd64` 与 `linux/arm64` platform manifest：

`backend`、`business-worker`、`search-indexer`、`frontend`、`admin-frontend`、`router`、`marshaller`、`monitor`、`redis-exporter`。

- `migrate`、`search-init`、`admin-role` 复用 Backend 镜像，不另造重复镜像。
- `acceptance` 是测试工具，不作为用户运行产品镜像；若实际三平台浏览器门禁需要它，必须提供所需架构，但不得计入产品交付数量或进入日常 Compose。
- 每个平台镜像固定 numeric UID:GID、只读根、OCI version/revision/source/title、无宿主源码或构建工具，并从同一提交构建。
- image index 与两个 platform digest 都写入 release manifest；验收核对运行容器 image ID/architecture 与目标 platform digest。
- 第三方 MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 镜像必须锁定到已核对的双架构 digest。若当前版本缺少任一目标架构，只允许升级到最小兼容版本并运行直接数据/协议回归，不以模拟 amd64 作为 macOS 最终方案。

### 7.2 六插件双架构 catalog

- 六类 current v2 包都声明 `os=linux`，分别构建 `arch=amd64|arm64`，archive、entrypoint 与 Schema digest 进入对应 Monitor platform catalog。
- 同一 plugin ID/version/arch 的内容唯一；Monitor 启动时必须以容器实际 arch 选择精确 catalog，缺失或错架构安全失败，不尝试执行。
- `1.9.4/linux/amd64` Redis v1 包以提交 `102aa4f...` 的源码、锁定 Go 工具链与精确 digest 重建并登记为仅升级用 legacy；不声称存在 `1.9.4/linux/arm64`。
- arm64 备份恢复不携带或执行 amd64 package；逻辑状态按第 10 节映射到目标架构 current/retained 包，并把版本迁移记录进恢复报告。
- 生产 Monitor 不包含 acceptance-only 失败包；失败包只进入隔离验收 target，继续使用同一校验代码。

### 7.3 共享生命周期镜像与交付包

- `lifecycle/` 使用一个 Go module 和一个 `cmd/gopulse` 命令树，只构建为 `linux/amd64`、`linux/arm64` 工具镜像；不产生 Darwin/Windows 宿主二进制或平台 adapter。
- 只发布一份 OS 中立 Bundle，其 Compose、manifest、checksum 和文档完全相同；Linux Terminal、macOS Terminal 与 Windows PowerShell/Terminal 都以 Docker Compose v2 命令调用同一工具服务。
- 宿主路径、行尾、中断和文件共享差异由 Docker Desktop/Engine 与 Compose 支持合同吸收；产品不因此新增 macOS/Windows 源码分支、原生启动器或专用编排。
- 产品交付包不包含项目源码、Node modules、Go cache、开发 `.env.example` 固定 Secret、测试凭据、source map、宿主绝对路径或 registry credentials。
- Bundle checksum、生命周期命令 `version --json` 和 release manifest 的 version/revision 必须一致；工具镜像架构与 Docker server 不匹配时在创建产品资源前拒绝。

## 8. 共享产品生命周期合同

### 8.1 命令面

本方案后文中的`gopulse <command>`均是产品语义简写，实际交付入口是从OS中立Bundle执行`docker compose ... run --rm lifecycle <command>`或修订后的等价Compose命令，不代表存在macOS/Windows宿主二进制。

| 命令 | 产品语义 |
| --- | --- |
| `version` | 输出生命周期工具、manifest、Compose schema 与目标容器平台版本；不创建 Docker 资源 |
| `doctor` | 只读检查宿主、Docker server/Compose、架构、路径共享、端口、资源和 release digest，输出脱敏建议 |
| `init` | 在空安装目录原子生成随机 Secret与非敏感配置，建立 project identity；已有配置默认拒绝覆盖 |
| `up` | 获取操作锁，预检归属与镜像 digest，执行初始化作业和完整 Compose 启动，最后调用只读 verify |
| `down` | 只停止当前强归属 project并保留卷；删卷需要显式 `--volumes --confirm-project <exact>` |
| `status` | 只读显示版本、服务健康/就绪、edge URL、持久卷和安全摘要，不回显 Secret、PID 或内部地址 |
| `logs` | 读取当前 project结构化日志，支持 service/tail/since；不自行拼接 Secret，遵守现有日志脱敏合同 |
| `verify` | 只读验证服务、镜像 digest、唯一 edge、健康/就绪、两个 SPA/API smoke 和资源归属 |
| `backup` | 按第 10 节停写/排空并生成认证加密 backup，不覆盖已有目标文件 |
| `restore` | 只恢复到空且强归属的新 project，验证后才标记完成；失败删除本次新资源和明文临时文件 |
| `upgrade` | 仅接受 manifest 声明的来源版本，强制预升级 backup，运行迁移/重建/验证并记录稳定终态 |

### 8.2 初始化和配置

- `init` 使用系统加密随机源为 MySQL、Redis、RabbitMQ、JWT、内部 API、Metrics 与 VictoriaMetrics 等 Secret分别生成独立值；不复制开发默认值，不在 stdout/命令行显示。
- 配置文件在挂载安装目录中临时写入、同步并原子替换；Linux 使用最小权限，Docker Desktop 宿主以当前用户可读、Secret 不输出和加密 backup 为边界，不新增 Windows ACL 修改实现。若无法建立安全父目录，初始化失败。
- product config只允许服务端已知 key。宿主路径、project name、edge port、release digest 与可移植应用配置分层存放；未知 key、重复 key、控制字符、绝对容器路径或外部 listener 安全拒绝。
- `init` 不自动创建超级管理员。系统启动后按 Phase 15 合同注册首用户并通过受控生命周期 operation 声明 bootstrap；生命周期只提供受限封装，不在配置中写账号密码或直接 SQL 提权。

### 8.3 操作锁、归属和中断

- 每个安装目录只有一个原子 operation lock，记录不敏感的 operation ID、命令和开始时间；第二个变更命令安全冲突，只读命令按明确规则并行。
- 任何 stop/remove/volume restore前联合校验本地 project identity、Compose project/service label、working directory token、release digest、资源 ID 与允许集合；missing/multiple/mismatch 时停止，不猜测。
- 宿主 Docker CLI/Compose 将 Ctrl+C/终止转发到工具容器的 `SIGINT`/`SIGTERM` 有界取消：未提交临时文件清理，已启动的 Compose project按当前命令语义保留诊断或回到稳定状态，绝不扩大到其他 project。
- `up` 失败保留当前项目资源供 `status/logs/doctor` 诊断；只有用户显式 `down` 才停止。`backup/restore/upgrade` 各自使用第 10～11 节的更严格失败规则。
- 现有 Bash 脚本可继续服务源码开发与历史验收，但产品文档只介绍 OS 中立 Bundle 的 Docker Compose 生命周期入口；工具容器不得反向调用 `scripts/*.sh` 或 `scripts/*.ps1` 承担业务逻辑。

## 9. 唯一 origin 与双 Frontend 产品合同

### 9.1 路由、会话与授权

- 产品 Compose 只发布 `127.0.0.1:<edge-port>`。Backend 的宿主端口从产品 profile移除；开发 Compose 如需保留，必须通过明确的开发 override与产品配置分离。
- 固定公开路径为用户端 `/`、`/login` 及社交路由，管理端 `/admin/` 及管理路由，Backend `/api/v1/`，edge 健康 `/health` 与 `/ready`。
- `/admin` 规范化到 `/admin/`；两个 SPA deep link/刷新不落错 bundle。旧 `/admin/observability/*` 兼容重定向保持一个明确版本窗口，不复制旧页面。
- 唯一登录入口仍在用户 Frontend。`user` 默认到 `/posts`，`super_admin` 默认到 `/admin/`；安全 redirect只接受同 origin角色允许的绝对 path。
- 两端只使用同一 HttpOnly、Path `/` Cookie，不把 token放入 localStorage、URL 或 bundle。401进入登录/恢复，管理 API 403立即清空管理 DOM/候选 Secret并回到用户端；Backend继续按 MySQL 当前角色拒绝越权。

### 9.2 共享视觉与交互基础

- 建立一个只含设计令牌、基础可访问组件/状态协议和时间格式工具的共享前端包；两个应用保持独立 entry、router、layout、业务 view、build和runtime image。
- 固定颜色、字体、间距、圆角、阴影、focus ring、表单、按钮、notice、表格/卡片、loading/empty/error/expired骨架；不得复制两套漂移的基础 token。
- 用户端保持内容/社交信息架构，管理端保持密集运维信息架构；“统一”不等于把两端合并成一个 bundle或共用业务页面。
- 代表性验收 viewport固定覆盖窄屏 `390x844`、中等 `768x1024` 与桌面 `1440x900`。不允许横向页面溢出、不可达操作、遮挡焦点或仅靠 hover可用。
- 所有交互控件可用键盘到达，有可见 `:focus-visible`，提交/危险操作有明确 disabled/pending/result，异步刷新不把焦点无故重置到页面顶部。
- Backend 时间继续使用带 offset的 RFC3339；两端统一按浏览器本地时区显示，同时保留机器可读 `datetime` 与明确时区提示。无效时间安全降级，不显示 `Invalid Date` 或猜测 UTC。
- 每个新共享状态只用一个代表页面的成功和一个代表失败在最低有效层验证；最终浏览器矩阵覆盖用户时间线、管理大屏、插件/告警操作及 session过期，不做全页面视觉排列组合。

## 10. 一致备份与恢复合同

### 10.1 备份边界

Phase 16 交付的是有界维护窗口内的一致 backup，不宣称在线快照：

1. 获取安装级操作锁，记录当前服务运行状态，预检目标路径、剩余空间、manifest、project归属和无进行中的 upgrade/restore。
2. 关闭 edge新请求，停止 Monitor 等持续遥测生产者；等待 Backend Outbox、RabbitMQ业务队列和 Kafka consumer lag在有界时间内排空。
3. 排空成功后有序停止 Backend/Worker/Indexer、Router/Marshaller和告警评估写入，固定数据库/可观测时间边界；排空超时则恢复原运行状态并失败，不生成“成功”backup。
4. 使用锁定版本服务提供的逻辑 dump/snapshot接口导出 MySQL、Elasticsearch Logs/Events历史与 VictoriaMetrics历史；业务搜索投影允许在恢复后从 MySQL重建。
5. 通过 Monitor受限离线 export导出六插件的 logical identity、受信版本引用、desired state、非敏感配置和 Secret；不导出可执行文件、PID、active symlink或临时 revision。
6. 导出可移植配置 allowlist与 release/backup manifest；排除宿主路径、project name、edge端口、临时文件、cache、运行容器 ID和 registry凭据。
7. 在私有临时目录完成逐项 digest，再使用锁定的成熟认证加密实现生成单一 backup文件；密码从无回显终端输入或受限文件描述符读取，不进入 argv/env/log。
8. 原子发布 backup；删除明文临时目录并恢复原服务状态。任一步失败都不覆盖已有目标文件、不删除原数据，且明确报告系统是否已恢复运行。

Redis只保存可重建缓存；RabbitMQ/Kafka在成功排空后重建为空。它们不通过跨架构不透明卷 tar恢复。若排空不成立，backup失败而不是漏掉在途事实。

### 10.2 Backup format v1

加密载荷内至少包含：

- `backup-manifest.json`：format version、产品 version/revision、source project ID、创建时间、逻辑组件清单、各 payload digest、队列排空证据、支持的 restore范围。
- MySQL逻辑 dump：用户、业务、Outbox、角色/bootstrap、告警规则/状态/历史和管理审计等全部权威表。
- Elasticsearch snapshot/export：Logs、Events及其 template/alias metadata；业务搜索可包含，但恢复后仍以 MySQL校验或重建。
- VictoriaMetrics snapshot及锁定版本恢复 metadata。
- Monitor logical export：插件 config/Secret/desired state与已登记 source version，不含 platform binary。
- 可移植 product config与 Secret；宿主专属值只作为非敏感建议，不在目标宿主强制复用。

格式严格拒绝未知必需 section、重复路径、路径穿越、符号链接、超限大小、digest不匹配、错误产品/backup format和认证失败。解密或校验失败时不得创建 Docker资源。

### 10.3 恢复语义

- 默认只允许恢复到新的空安装目录和空 Compose project；已有容器、卷、配置或同名资源即拒绝。Phase 16 不提供破坏性原地覆盖。
- 先完整解密/验证到私有临时目录，再创建经新 identity标记的资源；导入失败只删除本次已证明归属的新资源，保留 backup与其他项目。
- MySQL先恢复权威事实并运行当前幂等 migration；Elasticsearch恢复 Logs/Events后重建业务搜索投影；VictoriaMetrics恢复历史；RabbitMQ/Kafka拓扑重新初始化为空。
- 插件在目标架构用 current/retained catalog重新物化。相同受信版本/arch存在时保持版本；不存在时执行显式、可记录的兼容迁移到目标架构 current，保留 config/Secret/desired state并真实采集后提交。
- 恢复完成后运行只读 verify、代表性用户/管理查询与新写入，再原子标记 project为可用。失败 project不冒充 running，可安全重试或清理。

## 11. 从 1.9.4 升级合同

### 11.1 唯一锁定来源

- Phase 16 最低直接升级来源固定为主线提交 `102aa4fa9bb5256dfd0733f42454b0ec5deaf043`、产品版本 `1.9.4` 的 Compose系统和已知 `linux/amd64` 制品/卷布局。
- 验收 fixture必须使用该提交的真实 migration、单 Frontend、`user|admin`、业务数据、RabbitMQ/Kafka、Redis v1插件、Metrics/Logs/Events历史和活动 Cookie；不得直接向当前 schema写入模拟“旧数据”。
- `1.9.4` 只声明在 amd64来源环境升级。Phase 16 不虚构从未发布的 `1.9.4/linux/arm64`；跨架构迁移通过先形成 Phase 16 backup再按第 10 节恢复验证。

### 11.2 升级事务

1. `upgrade` 获取操作锁，读取旧 VERSION/Compose labels/schema/plugin状态并精确识别 `1.9.4`；未知版本、dirty compose assets、未知镜像或不归属资源拒绝。
2. 运行旧系统只读 preflight，记录用户/管理员、业务表、Outbox/queue/offset、搜索、遥测历史、Redis v1插件、配置和资源快照。
3. 使用 Phase-16-04正式 backup能力创建并验证升级前加密备份；没有成功 backup不得继续。
4. 按 backup相同的停写/排空边界停止旧写入，拉取目标 digest，运行全部数据库 migration、Kafka/topic与ES template/alias幂等初始化。
5. 迁移 `admin → super_admin` 并稳定选择最小旧 admin ID为 bootstrap；现有 JWT/Cookie仅依赖用户 ID时保持有效，角色仍从数据库实时读取。
6. 识别 `1.9.4/linux/amd64` Redis v1受信包与持久状态，保留 config/Secret/desired state，经当前 catalog显式迁移；其余五插件按新装未配置，不伪造历史。
7. 启动双 Frontend与完整当前栈，重建/校验业务搜索，确认升级前 Metrics/Logs/Events历史可读，并执行代表性新业务、插件、告警和审计操作。
8. 全部验证通过后原子更新生命周期 state为目标版本并保留升级报告；失败时不把版本标为完成，按失败点恢复旧稳定系统或使用升级前 backup恢复到新 project。

直接二进制降级不是 Phase 16 回滚合同。数据库 migration或持久格式已提交后的失败，以经验证 backup恢复为唯一安全返回路径，不运行旧镜像猜测兼容。

## 12. 三类真实宿主支持合同

### 12.1 每个平台共同矩阵

每个宿主都必须从同一候选 release manifest对应的 OS 中立 Bundle 开始，并通过本机 Docker Compose 调用共享生命周期工具，至少执行：

- `version` 与 `doctor`：宿主/daemon架构、Compose能力、资源、路径共享、端口、manifest/digest通过；错误架构和 daemon不可达代表性失败在 Docker创建资源前返回。
- `init → up → verify → status/logs`：全新随机 project和随机 edge loopback端口启动，宿主无 Go/Node/Python/数据库客户端要求，全部 product container使用正确平台 digest。
- 浏览器：普通用户代表性社交闭环；超级管理员统一登录、管理大屏、六插件代表操作、Metrics/Logs/Events和告警闭环；未登录/普通用户管理 API负向矩阵。
- 持久与生命周期：保留卷 `down → up`、单服务替换、Docker CLI 向工具容器转发中断、有界失败诊断与恢复。
- backup/restore：当前架构 backup恢复到新 project；至少一次 `linux/amd64 → macOS linux/arm64` 逻辑跨架构恢复，证明插件状态不执行旧架构 binary。
- 强归属：正常、预检失败、启动失败、backup失败、restore失败和中断后的资源/端口/文件快照；无关 Docker资源与用户文件不变。

`1.9.4 → current` 升级固定在一个真实 amd64来源环境完整运行，并在另一个 amd64宿主执行最小重复升级证据，至少覆盖 Windows路径或Linux文件系统差异。三台宿主不必重复全部故障排列。

### 12.2 平台特有项

- Linux：Linux文件系统路径、可执行位、LF assets、Docker Engine/Desktop单 daemon、POSIX signal/权限、无宿主工具链。
- macOS：APFS含空格路径、Docker Desktop文件共享、`linux/arm64` 生命周期/产品容器、case-insensitive默认卷路径冲突、Terminal 中 Docker CLI 中断、窄屏/桌面浏览器。
- Windows：NTFS含空格路径、CRLF checkout/解压后 Compose与配置可读、PowerShell 中 Docker Compose 参数/引号、Docker CLI 中断转发、Docker Desktop Linux container mode、文件共享、端口占用、用户目录访问边界、cleanup和两个 Frontend。
- 三个平台均不得使用宽泛 home/root目录作为删除目标，不读取或修改其他 Compose project，不把宿主绝对私有路径写入 evidence、日志、API或 bundle。

### 12.3 证据合并

- 同一 acceptance命令生成结构化 evidence JSON，字段包含匿名 run ID、host OS/arch、Docker server OS/arch、Compose version、bundle/manifest/platform digest、project token、执行时间、各固定 gate结果和前后资源摘要。
- evidence不包含用户名、主机名、绝对路径、IP、Cookie、Secret、registry token或业务正文。三个文件由聚合器验证 schema、候选 digest一致、host/arch互异、时间有效和全部 gate通过。
- 交叉编译、buildx成功、静态 Compose config、QEMU运行、WSL中的Compose运行或复制另一个平台 evidence均不能填补缺失项。

## 13. 故障、安全与资源所有权

- 任何产品命令先做纯本地参数/path/config校验，再访问 Docker；危险路径、空 project、未知 manifest、错误 arch、无空间、端口不安全和归属冲突必须提前失败。
- edge固定 loopback；不允许 `0.0.0.0`、`::`、非本机域名或 `host.docker.internal`作为产品公开/内部旁路。远程访问留给后续部署阶段。
- 诊断 bundle只采集 allowlist版本、状态、健康、事件码、脱敏日志尾部和 Compose渲染摘要；不采集 `.env`、backup内容、Cookie、完整连接串、宿主用户目录或任意文件树。
- Frontend bundle、runtime image、release asset、生命周期命令 `--help/version`、API错误、日志、evidence和upgrade/restore报告都必须通过 Secret与私有路径哨兵扫描。
- backup加密失败、认证失败或输出路径已存在时原数据不变；restore/upgrade失败不能删除来源 backup或旧 project。所有部分产物使用 operation ID并在确认归属后清理。
- 可观测或告警失败继续不成为社交 readiness必要条件；产品化不得弱化 Phase 15的 `unknown`、重复抑制、审计和权限隔离。

## 14. 各批次职责边界

### Phase-16-01：多架构制品与发布清单闭环

- 建立双架构产品/生命周期工具镜像、六插件 catalog、单一 OS 中立 Bundle 和 immutable release manifest。
- 确认第三方镜像双架构与 registry/digest晋升能力，运行各架构最小容器 smoke。
- 不实现完整生命周期、UI重构、backup或upgrade。

### Phase-16-02：共享产品生命周期与安全初始化闭环

- 实现共享生命周期工具的 version/doctor/init/up/down/status/logs/verify，由三宿主的 Docker Compose 以相同语义调用，并完成唯一 edge 与强归属。
- 在 Linux amd64与当前可用 macOS arm64执行 clean install最小运行，尽早关闭路径/架构问题。
- 不实现 backup/restore/upgrade，也不把历史 PowerShell脚本升级为产品入口。

### Phase-16-03：统一登录与双 Frontend 产品体验闭环

- 建立共享令牌/基础状态、三个 viewport、键盘焦点、时区和 session过期体验。
- 保留两个独立 bundle和 Backend授权，通过唯一 edge完成双角色浏览器闭环。
- 不改变业务、告警或插件 API语义。

### Phase-16-04：一致备份恢复与诊断闭环

- 实现停写/排空、认证加密 backup format v1、空 project restore、插件逻辑 export/import和诊断报告。
- 验证同架构恢复及至少一个 amd64到arm64恢复路径。
- 不提供在线、增量、定时或原地覆盖恢复。

### Phase-16-05：1.9.4 升级与恢复闭环

- 用锁定主线 fixture完成真实 `1.9.4 → current`，复用正式 backup作为失败恢复。
- 验证角色/bootstrap、活动会话、业务、搜索、三类历史、Redis v1插件、双 Frontend和新告警。
- 不支持任意未声明来源或二进制降级。

### Phase-16-06：三宿主矩阵与阶段收口

- 对同一候选制品运行 Linux amd64、macOS arm64、Windows amd64固定矩阵并聚合脱敏证据。
- 完成跨批集成、平台特有负向、文档/版本/CI与强归属收口。
- 只修复真实阻断，不新增功能、Review或无关测试。

## 15. 阶段级验收标准

### 15.1 制品与安装

1. 9个产品镜像各有可解析且真实运行的 `linux/amd64`、`linux/arm64` digest；运行 arch、OCI metadata、numeric user、read-only filesystem和源码 revision正确。
2. 六插件 current包在两个架构真实启动、采集并经完整链路查询；错架构、未登记或digest错误包安全拒绝且不影响其他插件。
3. 单一 OS 中立 Bundle checksum、生命周期工具镜像 version、Compose 与 manifest 一致，交付包不含源码、工具链、固定产品 Secret、私有路径或 registry 凭据。
4. 产品通过digest pull运行，不依赖本地同名tag或源码build；候选晋升到正式版本不重建内容。

### 15.2 生命周期、入口与双 Frontend

1. 三平台均能仅依赖 Docker/Compose 与同一 Bundle 完成 `doctor/init/up/verify/status/logs/down/up`，无宿主 Go/Node/Python/基础设施客户端，也无 macOS/Windows 专用产品实现；正常与失败中断只影响当前 project。
2. 只有一个loopback edge端口，浏览器不能直达Backend、admin-frontend、Monitor、Router、Marshaller、Exporter或数据服务。
3. user/super_admin从统一登录进入正确应用，安全deep link和刷新有效，管理越权由Backend拒绝，角色降级后DOM/Secret清理且会话按普通用户继续。
4. 两个独立bundle共享稳定设计基础；390/768/1440 viewport、loading/empty/error/expired、键盘焦点和本地时区代表矩阵通过。

### 15.3 备份、恢复与升级

1. backup在停写、Outbox/RabbitMQ/Kafka排空后生成认证加密文件；超时/中断不发布部分backup并恢复原运行状态。
2. 空project恢复后，MySQL业务/角色/告警/审计、Logs/Events历史、VictoriaMetrics历史、插件config/Secret/desired state和可移植配置正确；Redis缓存、消息拓扑和业务搜索按合同重建。
3. amd64 backup可在macOS的linux/arm64栈恢复，插件使用目标架构受信binary；backup中的amd64 executable即使存在也必须被格式校验拒绝或忽略，绝不执行。
4. 真实`1.9.4` fixture直接升级后保留用户、password hash、旧admin、活动Cookie、帖子/关系/通知、Outbox事实、遥测历史和Redis v1配置/desired state；角色/bootstrap、双Frontend和新告警可用。
5. migration/初始化可重跑；升级失败不写完成版本，可由强制预升级backup恢复，不要求删卷或手工SQL。

### 15.4 三宿主与既有回归

1. Linux amd64、macOS arm64、Windows amd64 evidence分别来自真实host/server组合，并绑定同一候选manifest；无缺失或模拟替代。
2. 每个平台用户端完成注册/登录、发帖、评论或点赞、关注/Following、收藏、搜索、通知和编辑/删除代表性闭环。
3. 每个平台管理端完成大屏、六插件状态/代表操作、Metrics/Logs/Events查询、三源告警与角色/审计代表闭环；普通用户固定403。
4. 可观测局部故障不阻断社交主路；保留卷重启、容器替换、平台signal和edge端口变化后系统可恢复。
5. Windows 路径/CRLF/文件共享/PowerShell Docker Compose、macOS arm64/APFS/Terminal Docker Compose、Linux 权限/signal/单 daemon 特有项均有真实结果，且未新增 Darwin/Windows 二进制或平台源码分支。

### 15.5 阶段与里程碑条件

Phase 16 只在以上标准全部通过、6份split plan均有同名真实实施记录、根完成版本为`1.13.6`、全部批次按顺序合入主线且无阻断问题时完成。该结果把跨平台产品交给Phase 17；Milestone 4仍由Phase 17的Kubernetes前完整工程验收收口，Phase 16不得提前宣称Milestone 4完成。

## 16. 验证策略与固定阶段门禁

- 实施中先运行直接受影响module/package和无Docker self-test；每个split plan的固定门禁只对该批最终diff运行一次。
- Phase-16-01建立release artifact验证入口，Phase-16-02建立lifecycle self-test/owned runtime入口，Phase-16-03建立双Frontend产品UI入口，Phase-16-04建立backup/restore入口，Phase-16-05建立upgrade入口。
- Phase-16-06使用同一候选bundle在三host执行固定acceptance并聚合evidence；不重新跑前五批所有unit组合，除非相关输入变化或真实平台失败指出直接回归。
- 每个状态迁移优先一条代表成功与一条代表失败；路径/平台差异只补充会改变产品结果的案例，不枚举所有盘符、locale、shell或文件名组合。
- 真实平台结果必须记录Docker server而非只记录client；container arch、运行digest、bundle digest和resource snapshot进入evidence。
- 新增检查只证明本批验收、观测缺陷或改变的安全/持久/公共合同；达到固定门禁后停止，不延伸为通用供应链、性能或覆盖率项目。
- 每批固定运行版本/分支治理、计划对应实施记录检查、`git diff --check`和`git diff --cached --check`；提交后再核对`upstream/main...HEAD`范围。

最终阶段收口至少执行当时已经落地的等价固定入口：

```bash
(cd lifecycle && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json
scripts/verify-lifecycle.sh --self-test
scripts/verify-product-ui.sh --self-test
scripts/verify-backup-restore.sh --self-test
scripts/verify-upgrade.sh --self-test
python3 scripts/ci/verify_phase16_evidence.py \
  --linux <linux-evidence.json> \
  --macos <macos-evidence.json> \
  --windows <windows-evidence.json>
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.13.6 --base-ref upstream/main
git diff --check
git diff --cached --check
```

命令名是本阶段交付合同；若实现前有必要调整，必须先在`update`同步总方案和对应split plan，不能在开发记录里静默用另一套入口冒充。

## 17. 有限前置确认与禁止猜测清单

| 责任批次 | 必须确认的有限输入 | 完成证据与未满足处理 |
| --- | --- | --- |
| Phase-16-01 | registry不可变 digest/晋升能力、六个第三方镜像双架构支持、buildx platform metadata、生命周期工具镜像与 OS 中立 Bundle 构建 | 各逻辑镜像 index/platform digest与最小真实 run；缺架构则选择最小兼容锁定版本并记录直接回归 |
| Phase-16-02 | 三 host Docker/Compose 共同子集、工具容器的中断转发、macOS/Windows 文件共享、Compose pull-by-digest 行为 | fake engine self-test + Linux/macOS 最小真实 clean install；共同子集不足则先修订容器入口/支持版本，不写平台二进制或分支脚本绕过 |
| Phase-16-03 | 当前两端路由/DTO/状态清单、browser timezone与三个viewport、edge只发布一个端口后的API行为 | 两bundle build与唯一origin browser证据；不为统一视觉修改业务API |
| Phase-16-04 | 锁定MySQL/ES/VM的逻辑dump/snapshot与restore API、队列/offset排空判据、Monitor逻辑export边界、Linux工具容器加密临时文件与三宿主挂载行为 | 小数据真实backup/restore和故障注入；任一权威历史无法恢复则阻断，不降级为卷tar或“可重建”假设 |
| Phase-16-05 | `102aa4f...` compose/volume/schema/Redis v1 archive、旧Cookie合同、全部migration重跑行为 | 真实旧版本fixture前后快照；无法识别来源或恢复则停止，不手工改库补齐 |
| Phase-16-06 | 三host可用窗口、同一candidate bundle、前五批证据有效性和平台特有故障手段 | 三份schema有效evidence；缺一平台即Phase 16未完成，不用buildx/QEMU/WSL替代 |

只查看直接调用点、锁定版本公开文档和最小受控探测。只有具体编译/运行/验收失败无法从调用点与公开API解决时才检查第三方源码，并将理由和最小symbol记录到同名实施记录。

## 18. 实施记录、版本与提交要求

每个批次完成前：

- 创建或更新与计划同名的`dev/logs/Phase-16/Phase-16-XX-*.md`，记录实际文件、命令、host/server版本、artifact/digest、运行结果、故障注入、偏差、限制和下一批输入。
- 平台evidence、backup样本、私有路径和Secret不直接提交；记录脱敏摘要与checksum，并按CI artifact或调用方私有目录保留。
- 将根`VERSION`、两个Frontend package/lockfile、lifecycle工具镜像、manifest、镜像和插件metadata更新为权威表目标版本。
- 只暂存本批文件，使用英文Conventional Commit；本地成功、push、PR、远程checks和merge分开记录，未观察到的状态不得写成完成。
- 固定门禁失败时不更新完成状态；只修复直接阻断，成功后停止，不追加默认Review、依赖审计或无关重构。

## 19. 阶段完成、停止条件与 Phase 17 交接

Phase 16停止条件是第15节全部通过、三host evidence完整、无阻断失败，且文档、版本、release manifest、实际digest、实施记录和主线状态一致。达到条件后更新总/分方案真实状态，提交阶段收口并停止。

交给Phase 17的固定输入至少包括：

- 一个共享容器化产品生命周期、一份 OS 中立 Bundle、唯一 edge 与稳定操作/归属/诊断合同；不包含 macOS/Windows 原生产品实现。
- 九个双架构产品镜像、六插件双架构catalog、锁定第三方digest与可重用release manifest。
- 两个独立Frontend、统一登录/会话/设计基础和真实三host浏览器证据。
- backup format v1、空project恢复、`1.9.4`升级及失败恢复证据。
- Phase15业务、插件、告警、审计、权限与局部故障边界未被产品化弱化。

Phase 17可在完整Compose产品上做统一工程质量收口，但不得把Kubernetes变成其前置条件，也不应在没有直接影响时重跑Phase 16全部三宿主/升级/恢复矩阵。Phase 18后续复用同一多架构镜像和产品合同迁移到Kubernetes，不在Phase 16预建Kubernetes资源。
