# Phase 6：Monitor 总实施方案

## 1. 实施目标

在 Phase 5 已交付独立 Redis Exporter、固定 `/health`/`/metrics` 契约和可接管进程边界的基础上，交付可独立运行的 Monitor，完成管理员授权、插件安装包导入、Exporter 生命周期管理、MetricsMonitor 周期拉取、基础校验与 GoPulse 标准 metrics 消息封装。

本阶段固定形成三条共同可验证的最小闭环：

```text
身份与授权：
普通用户注册/登录 → 当前用户 role=user → 运维 CLI 显式提升
                  → 同一账号/会话 role=admin → 服务端 admin 授权

插件管理：
已登录管理员 → Backend 管理 API → Monitor Plugin Manager
                 → 安全解包与原子安装 → Exporter 进程状态变化

指标采集：
真实 Redis → Redis Exporter → MetricsMonitor 周期 HTTP Pull
          → Prometheus 解析与基础校验 → GoPulse metrics Envelope
          → HTTP 捕获端验证完整消息
```

阶段完成必须同时证明：普通用户与管理员继续共用现有身份系统且社交业务无回归，普通用户无权访问可观测管理，合法安装包可被导入并自动运行，更新失败可回滚，Monitor 重启可恢复期望状态，真实指标可持续转换为标准消息。只新增角色字段、只做前端隐藏、只交付进程包装、只实现定时器、使用静态 fixture 替代真实链路，或绕过 Backend 直接调用 Monitor，均不构成完成。

## 2. 当前真实基线

本方案编写时的代码基线是主远程 `main` 提交 `7566c65` 及根版本 `0.4.4`，Phase 4 与 Phase 5 已完成详细规划但尚未实施：

- Backend 是独立 Go module，已有用户、登录、JWT/Cookie 会话与受保护 `/api/v1` 路由，但 `users` 没有角色字段，也没有管理员授权边界。
- `monitor/` 仍只有 Phase 0 占位说明，没有 module、HTTP 服务、调度器、插件注册表或运行时实现。
- 日常 Bash 脚本已使用 `.run/*.json` 对进程的 cwd、绝对 executable、start ticks 和 command marker 进行归属校验；Phase 5 规划将 Redis Exporter 纳入同一边界。
- Phase 5 固定 Redis Exporter 为常驻、被动拉取进程，`/health` 只表达进程存活，`/metrics` 成功返回 Prometheus text exposition 0.0.4 和 `gopulse_redis_up 1`，目标故障返回 `503` 与唯一 `up 0` 样本。
- Phase 5 规划默认 Exporter 监听 `127.0.0.1:9121`，使用 Redis 环境变量、结构化日志和有界信号退出，并允许 Phase 6 把日常启停所有权从 `dev.sh` 转交给 Plugin Manager。
- 当前 CI 只覆盖 Backend、Frontend、脚本/Compose 和已有集成验收；Monitor 与插件包尚无独立门禁。

Phase 6 实施开始时必须重新核对最新主远程、Phase 4/5 实施记录和真实公共契约。若 Phase 5 最终改变安装包需要的 executable、环境变量、端点、信号或 PID 归属边界，必须先更新本总方案和未开始的拆分方案。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 4 和 Phase 5 全部批次已合入主远程 `main`，远程固定门禁成功，实施记录与真实提交一致。
- 主远程根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 均为 `1.2.2`。
- Redis Exporter 的真实成功、目标故障、自动恢复、进程归属和安全清理验收已通过。
- 每批开始前 fetch 主远程，从包含全部前置批次的最新 `main` 创建本方案分配的独立分支；不得沿用 `update` 或已完成的开发分支。
- 实施和应用验收固定在 Windows 宿主的 WSL2 Linux filesystem 执行，Bash 是唯一维护的本地生命周期和验收入口。
- 开始前保存 Git、日常 Compose project、volume、端口、`.run` 进程和插件根目录快照，不覆盖、暂存、停止或提交其他任务的资源。

### 3.2 权威批次、版本与开发分支

Phase 6 使用 `1.3.x` 版本线，`1.3.0` 只作为阶段基线，不创建空批次。下表是本阶段执行顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-06-01 | `1.3.1` | `develop/1.3.1` | 已完成；PR #63 已合入 `main` |
| Phase-06-02 | `1.3.2` | `develop/1.3.2` | 已完成；PR #64 已合入 `main` |
| Phase-06-03 | `1.3.3` | `develop/1.3.3` | 已完成；PR #65 已合入 `main` |
| Phase-06-04 | `1.3.4` | `develop/1.3.4` | 本地实施与固定验收完成；待远程门禁和合入 |

执行规则：

- 同一批次的全部提交共享目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建同名 `dev/logs/Phase-06/Phase-06-XX-*.md`，只记录真实工作、真实验证、偏差和限制。
- Phase-06-01 独立交付持久化角色、运维提升、当前用户 role 与服务端授权闭环，先稳定横跨后续阶段的安全边界。
- Phase-06-02 交付管理员到 Exporter 状态变化的纵向闭环；Phase-06-03 交付真实 Redis 到标准 metrics 消息的纵向闭环。
- Phase-06-04 是固定集成验收和阶段收口批次，不引入新功能，只允许最小修复真实验收暴露的阻断问题。
- 已推送分支不得静默改名或重新编号；批次数量或顺序变化时，先更新本表，再创建尚未开始的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- `monitor` 独立 Go module、Monitor HTTP 进程、健康/就绪接口、配置、结构化日志和有界退出。
- `users` 持久化 `user|admin` 角色、服务器运维授权 CLI、管理员中间件和当前用户角色契约。
- Backend 管理代理与 Monitor 内部 API，覆盖插件列表、单项查询、安装、启动、停止和更新。
- `tar.gz` 插件安装包、Manifest v1、受限解包、原子版本目录、`current` 激活链接、持久化 Registry 和失败回滚。
- Redis Exporter 的自动安装启动、幂等启停、健康确认、进程归属、Monitor 重启恢复和更新保态。
- MetricsMonitor 根据插件状态管理单一 Redis 采集目标，立即拉取并每 15 秒周期采集。
- Prometheus 0.0.4 解析、有界校验、结构化 samples、GoPulse metrics Envelope v1 和可选 HTTP Publisher。
- Bash 日常启停/验证、插件包构建、独立隔离验收、CI Monitor 门禁、README 和实施记录。

### 4.2 明确不做

- 不实现插件管理 Frontend；Phase 6 使用 API 验证，Phase 11 再交付管理页面。
- 不实现用户列表、用户禁用、前端角色管理或普通用户管理 API。
- 不实现远程插件仓库、市场、数字签名、在线下载、任意脚本 hook 或通用插件平台。
- 不使用 Linux mount、bind mount、loop、FUSE 或容器 volume 安装插件；“挂载”只表示解压到固定目录并激活运行。
- 不实现 MySQL Exporter、多 Redis target、动态服务发现或独立采集目标 CRUD。
- 不实现 LogMonitor、EventMonitor、Kafka、Message Router 服务、Marshaller、VictoriaMetrics 或指标存储。
- 不对指标做比率、速率、聚合、降采样或最终存储格式转换。
- 不建立磁盘消息队列或指标历史，不为 Publisher 失败提供无界重试。
- 不创建应用容器镜像，不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 组件与运行架构

### 5.1 独立 Monitor module

`monitor` 建立独立 Go module，建议结构：

```text
monitor/
├── go.mod
├── go.sum
├── cmd/monitor/
├── internal/config/
├── internal/httpserver/
├── internal/plugin/
│   ├── package/
│   ├── registry/
│   └── runtime/
├── internal/metrics/
│   ├── collector/
│   ├── envelope/
│   └── publisher/
└── README.md
```

- module path 固定为 `github.com/Ray-ymq/GoPulse/monitor`，Go 版本与实施时仓库基线一致。
- 不导入 `backend/internal/*` 或 `exporters/redis/internal/*`，跨进程数据交换只使用固定 HTTP/JSON、文件和进程契约。
- 不新增根 `go.work`；Backend、Redis Exporter 与 Monitor 分别构建、测试和管理依赖。
- Monitor 复用 Phase 4 JSON 日志 Schema 和构造模式但不跨 module 导入 internal 实现；`service=monitor`，module 至少包含 `lifecycle`、`http`、`plugin`、`runtime`、`metrics` 和 `publisher`。

### 5.2 运行职责和所有权

- Backend 只负责用户会话、管理员授权、请求大小限制、安全错误映射和流式代理；不解包、不启动 Exporter、不保存插件运行状态。
- Plugin Manager 是 Exporter 安装、版本激活、desired state、进程启停、实际健康状态和重启协调的唯一所有者。
- MetricsMonitor 只采集 observed state 为 `running` 的 metrics-exporter，不直接改变插件进程。
- Phase 6 实施后，日常 `dev.sh` 先启动 Monitor，再由 Monitor 恢复/管理 Exporter；脚本不得与 Plugin Manager 双重启动同一 executable。
- Monitor 优雅退出时停止子进程但不改写其 `desired_state=running`；下次 Monitor 启动重新校验并恢复该插件。

## 6. 管理员角色与授权契约

### 6.1 持久化角色

- 新增顺序迁移，在 `users` 增加非空 `role` 字段，有效值只能是 `user` 或 `admin`，默认 `user`。
- 迁移不自动把最早用户或任意已有用户提升为管理员，避免隐式权限提升。
- 用户 Repository 读取角色；JWT 仍只承载已有用户 ID，管理路由每次根据数据库当前角色授权，不信任可能过期的客户端角色或 Cookie 字段。
- 新增安全错误码 `permission_denied`，未登录仍为 `401 authentication_required`，已登录但非管理员固定为 `403 permission_denied`。

### 6.2 管理员引导与前端交接

- 增加服务器运维 CLI，以规范化 username 查找已注册用户并显式提升为 `admin`；用户不存在时非零退出，重复提升幂等成功。
- CLI 只支持授予管理员权限，不创建第二套密码、会话或用户管理系统；多次对不同用户执行即可形成多管理员。
- 注册、登录和 `/api/v1/users/me` 的当前用户 DTO 增加 `role`；业务作者摘要仍只有 `id` 和 `username`。
- Phase 6 只更新 Frontend 的当前用户类型、响应校验与回归测试，不新增管理页面；Phase 11 根据该 `role` 控制可观测管理导航和页面。

### 6.3 两种产品使用态与跨阶段授权边界

- GoPulse 只有一套用户身份、注册、登录和会话系统，但具有两种产品使用态：普通用户使用社交业务；管理员在保留社交能力的同时获得可观测查询和管理能力。
- 管理员是普通用户的权限超集，不是公开作者类型；任何用户都不能从帖子、评论、通知、搜索或公开缓存判断作者是否为管理员。
- Metrics、Logs、Events 查询以及 Exporter 查询/安装/启停/更新全部属于可观测管理域，必须由 Backend 根据数据库当前 `admin` 角色授权；普通用户固定为 `403 permission_denied`。
- Frontend 的角色导航与路由守卫只负责用户体验，不能代替 Backend 授权；直接构造 URL 或 API 请求仍必须被服务端拒绝。
- Monitor、Message Router、Marshaller、Kafka、VictoriaMetrics、Elasticsearch 等内部组件不接受浏览器或普通用户直接访问，只通过独立服务身份和受控网络协作。
- Phase 7～16 的接口、页面、容器、Kubernetes、Ingress 和最终回归必须继承本节；后续总实施方案不得把“用户”笼统解释为可操作可观测能力的普通用户。

| 能力域 | 未登录 | 普通用户 `user` | 管理员 `admin` | 内部服务身份 |
| --- | --- | --- | --- | --- |
| 社交业务 | 按既有公开/登录契约 | 允许 | 允许 | 不适用 |
| Metrics/Logs/Events 查询 | `401` | `403` | 允许 | 按内部契约 |
| Exporter 管理 | `401` | `403` | 允许 | Monitor Bearer token |
| 可观测内部接口与存储 | 拒绝 | 拒绝 | 浏览器不直连 | 独立鉴权与受控网络 |

## 7. 插件安装包契约

### 7.1 包形式与 Manifest v1

首版只接受 gzip 压缩的 tar archive，固定最低结构：

```text
plugin.json
bin/gopulse-redis-exporter
```

`plugin.json` 固定字段：

| 字段 | 类型 | Phase 6 约束 |
| --- | --- | --- |
| `schema_version` | integer | 固定 `1` |
| `id` | string | 固定 `redis-exporter`，符合小写 kebab-case |
| `name` | string | 固定、非空的显示名，不用作路径 |
| `version` | string | 三段 SemVer，不接受 prerelease/build metadata |
| `kind` | string | 固定 `metrics-exporter` |
| `source` | string | 固定 `redis` |
| `os` | string | 固定 `linux` |
| `arch` | string | 必须与 Monitor 当前运行架构一致 |
| `entrypoint` | string | 固定 `bin/gopulse-redis-exporter` |
| `entrypoint_sha256` | string | 入口文件的 64 位小写十六进制 SHA-256 |
| `health_path` | string | 固定 `/health` |
| `metrics_path` | string | 固定 `/metrics` |

- Manifest 严格拒绝未知字段、缺失字段、重复 JSON key、无效 UTF-8 和非法值。
- 安装包不包含 Redis 密码、Monitor/Router token、运行环境文件、安装脚本、启动脚本或自动执行 hook。
- 插件运行配置由 Monitor 从服务器受信环境按固定白名单传递；Manifest 无权指定任意环境变量、命令行或绝对路径。

### 7.2 上传和解包安全

- Backend 和 Monitor 同时强制 64 MiB 压缩包上限；Backend 使用有界 reader 流式转发 multipart `package`，不将完整包读入内存。
- Monitor 先写入自身插件根下的随机 staging 目录；限制最多 32 个 archive entry、128 MiB 总解压字节、96 MiB 单普通文件、64 KiB `plugin.json` 和 240 bytes 路径，任一超限立即失败并清理 staging。
- archive entry 必须是规范化相对路径且位于 staging 内；拒绝绝对路径、空路径、`.`/`..`、路径穿越、重复 entry、符号/硬链接、device、FIFO 和 socket。
- 解包后从普通文件重新读取 Manifest，核对平台、入口规范路径、SHA-256 和可执行权限；只对已验证入口设置固定执行权限。
- 同 ID 已安装时，install 返回冲突；update 必须是同 ID 且版本严格高于当前版本，拒绝相同版本、降级或已存在的 release 目录。
- 失败响应和日志只使用稳定 reason code，不返回 staging 路径、安装根、原始 archive 名、进程参数或未经清洗的解包/执行错误。

## 8. 安装布局、Registry 与生命周期

### 8.1 固定布局

`MONITOR_PLUGIN_ROOT` 必须在启动时解析为绝对路径。Bash 本地运行将其设为 `$REPO_ROOT/.run/plugins`，部署文档约定 `/var/lib/gopulse/plugins`，内部布局固定为：

```text
<plugin-root>/
├── registry.json
├── .staging/
└── redis-exporter/
    ├── current -> releases/<version>
    ├── releases/<version>/
    │   ├── plugin.json
    │   └── bin/gopulse-redis-exporter
    └── runtime/process.json
```

- release 在 staging 中完整验证后，以同文件系统 rename 进入版本目录；`current` 通过临时相对链接加 rename 原子切换。
- `registry.json` 只保存插件标识、Manifest 公开元数据、当前版本、安装/更新时间和 `desired_state`；使用临时文件、fsync 和 rename 原子更新。
- Registry 不保存 Redis 配置、token、PID 或原始错误。PID 记录位于 runtime 目录，并继续绑定 cwd、绝对 executable、start ticks 和 command marker。

### 8.2 状态模型

- `desired_state` 只能是 `running` 或 `stopped`。
- `observed_state` 只能是 `installing`、`starting`、`running`、`stopping`、`stopped`、`updating` 或 `failed`。
- 公共状态包含 `id`、`name`、`version`、`kind`、`source`、`desired_state`、`observed_state`、`installed_at`、`updated_at`、`started_at`、`last_scrape_at`、`last_success_at` 和可选 `last_error {code,message,at}`。
- 时间使用 UTC RFC3339Nano；不适用的时间省略或返回 `null`，不伪造零值。`last_error.message` 只能是有限的客户端安全文案。
- 每个插件的 install/start/stop/update 在 Monitor 内串行化；并发冲突返回 `409 plugin_operation_in_progress`，不启动第二个运行时。

### 8.3 安装、启停、更新与恢复

- 首次 install 在安全解包后注册 release、设置 `current`、将 desired state 设为 `running`，直接执行 entrypoint，并在启动时限内要求 `/health` 返回 Phase 5 固定 `200` JSON。
- 首次启动或 health 确认失败时，停止已启动进程，移除新 release/current/Registry 项和临时记录；返回失败而不留下半安装状态。
- start/stop 幂等：已运行的 start 和已停止的 stop 返回当前成功状态。start 先原子持久化 `desired_state=running`，只在 health 通过后设置 `observed_state=running`；启动失败保留 desired running 且 observed failed，允许后续 start 或 Monitor 重启恢复。stop 先持久化 desired stopped，再对已校验进程发送 `SIGTERM`。
- stop 等待固定关闭时限，超时后只对仍满足归属记录的进程/进程组执行强制退出；归属不匹配时拒绝发信号并返回安全错误。
- Linux 运行时为插件建立独立进程组并设置 parent-death `SIGTERM`；Monitor 异常退出后若仍发现归属完整匹配的遗留进程，启动恢复必须先有界停止它，再根据 desired state 创建唯一新进程。
- update 先完整验证新 release。原状态为 running 时停止旧进程、原子切换、启动新版并验证 health；失败时恢复旧 current/Registry 并重新启动旧版。原状态为 stopped 时只切换版本，不自动启动。
- Monitor 启动时校验 Registry、current 和 release 完整性，清理仅属于本根的过期 staging/无效 PID 记录，然后重新启动 desired state 为 running 的插件。
- 恢复中的单一插件失败不阻止 Monitor 管理 API 启动；该插件进入 `failed`，记录安全 reason，可由管理员再次 start/update。

## 9. HTTP 管理 API

### 9.1 Backend 公共 API

以下路由必须先通过现有登录会话，再通过管理员授权：

| Method | Path | 语义 | 成功状态 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/exporter-plugins` | 按 ID 稳定排序列出全部已安装插件 | `200` |
| `GET` | `/api/v1/exporter-plugins/:pluginId` | 返回单个插件状态 | `200` |
| `POST` | `/api/v1/exporter-plugins/install` | 上传 multipart `package`，安装并自动启动 | `201` |
| `POST` | `/api/v1/exporter-plugins/:pluginId/start` | 幂等启动 | `200` |
| `POST` | `/api/v1/exporter-plugins/:pluginId/stop` | 幂等停止 | `200` |
| `POST` | `/api/v1/exporter-plugins/:pluginId/update` | 上传 multipart `package`，更新并保持原 desired state | `200` |

- 成功响应使用既有 `{"data": ...}` Envelope；install/update 只接受单个名为 `package` 的文件 part，拒绝重复、未知字段和额外文件。
- 公共错误至少固定 `permission_denied`、`plugin_package_invalid`、`plugin_not_found`、`plugin_conflict`、`plugin_operation_in_progress`、`plugin_operation_failed` 和 `monitor_unavailable`，分别映射为安全的 `403/400/404/409/409/422/503`。
- Backend 不返回 Monitor 原始 body、内部 URL/token、文件系统路径、PID 或底层网络/进程错误；响应超时和连接失败统一为 `monitor_unavailable`。
- Monitor 不纳入 Backend 既有业务 readiness 的强制依赖；Monitor 不可用时业务 API 继续工作，只有插件管理返回 `503`。

### 9.2 Monitor 内部 API

- Monitor 提供 `GET /health` 表达进程存活，`GET /ready` 表达配置、Registry 和 Plugin Manager 已完成初始化；单个插件失败不使 Monitor 失去 readiness。
- 对应管理路由位于 `/internal/v1/exporter-plugins`，方法、语义、multipart 字段和状态 DTO 与 Backend 代理一致。
- 除 `/health` 外，Monitor 内部接口要求 `Authorization: Bearer <MONITOR_API_TOKEN>`；token 至少 32 bytes，使用常量时间比较，默认监听回环地址。
- Backend 与 Monitor 各自设置有界读取、写入、header、idle、请求和关闭超时；客户端取消传入解包和生命周期操作，但不在不可回滚的切换中留下不一致状态。

## 10. MetricsMonitor 采集契约

### 10.1 目标管理和调度

- 一个已安装 `metrics-exporter` 自动对应一个受管采集目标；target ID 固定由经验证的 plugin ID 派生，Phase 6 为 `redis-exporter-local`。
- install/start 在 health 通过后启用目标并立即触发一次采集；stop 先禁用新采集并取消在途请求，再停止进程；update 成功后重新绑定当前版本。
- 默认采集间隔 15s，采集超时 3s；限定可配范围并保证 timeout 严格小于 interval。
- 单目标最多一个在途 scrape；定时到达时若上一次仍未结束，不启动并行请求，而是记录有限 `scrape_in_progress` 状态并等待下一周期。
- 调度器使用 context 取消和可注入 clock，Monitor 关闭时停止新采集并在固定时限内等待已取消任务退出。

### 10.2 HTTP 和 Prometheus 解析

- 只向由已验证 Manifest 和服务器配置派生的回环 URL 发送 `GET /metrics`；不接收用户传入的任意 URL、query、header 或请求体。
- 响应体固定上限 1 MiB，超限、压缩响应、不支持的 content type、超时或网络错误不进入消息封装。
- 使用 Prometheus 官方 parser 读取 metric family，最多接受 128 个 family、1024 个 sample、每 sample 16 个 label、128 bytes 名称和 256 bytes label value；只接受 gauge/counter、有限数值和合法指标/标签名，拒绝重复 sample key、`NaN`、`Inf` 和输出中自带的时间戳。
- `200` 必须包含且只包含 Phase 5 允许的 family，`gopulse_redis_up=1`，其余必需 family、type 和有限标签契约完整。
- `503` 只在 body 能严格解析为唯一 `gopulse_redis_up` gauge 且值为 `0` 时封装 `target_unavailable` 消息；其他非 `200` 响应均视为采集失败并不产生 Envelope。
- samples 在封装前按 name 和规范化 labels 稳定排序；Monitor 不原样嵌入 Prometheus 文本、HELP、原始 HTTP body 或底层错误。

## 11. GoPulse metrics Envelope 与 HTTP Publisher

### 11.1 Envelope v1

标准消息固定结构：

```json
{
  "schema_version": 1,
  "message_id": "32-lowercase-hex",
  "type": "metrics",
  "source": "redis",
  "timestamp": "2026-01-01T00:00:00.000000000Z",
  "payload": {
    "plugin_id": "redis-exporter",
    "plugin_version": "1.2.2",
    "target_id": "redis-exporter-local",
    "scrape_status": "success",
    "samples": [
      {
        "name": "gopulse_redis_up",
        "kind": "gauge",
        "labels": {},
        "value": 1
      }
    ]
  }
}
```

- `message_id` 使用服务端安全随机 16 bytes 的 32 位小写十六进制；生成失败时不伪造 ID，本次消息失败。
- `timestamp` 是 Monitor 完成响应读取和基础校验时的 UTC RFC3339Nano 时间，不信任 Exporter 或远程时间戳。
- `scrape_status` 只能是 `success` 或 `target_unavailable`；后者的 samples 只包含 `gopulse_redis_up=0`。
- sample `kind` 只能是 `gauge` 或 `counter`，labels 是有限 string map，value 是有限 JSON number。
- Envelope 不包含安装路径、进程 ID、Redis 地址/密码、Monitor token、原始错误、HELP 文本或上一次成功数据。

### 11.2 Publisher 交接

- MetricsMonitor 只依赖 `Publish(context.Context, Envelope) error` 抽象，不导入 Kafka SDK，不识别 Topic，不调用 Marshaller。
- 配置 `MONITOR_ROUTER_URL` 时，HTTP Publisher 将 Envelope 作为 `application/json` POST 到 `<MONITOR_ROUTER_URL>/internal/v1/messages`，携带 `Authorization: Bearer <MONITOR_ROUTER_TOKEN>` 和 `Idempotency-Key: <message_id>`。
- 只有 `202 Accepted` 表示发布成功；其他状态、超时、无效 URL 或连接失败记录 `publish_failed`，不将原始响应 body 记入状态或日志。
- `MONITOR_ROUTER_URL` 在 Phase 6 允许为空；Monitor 仍完成解析和 Envelope 构造，仅在内存状态中更新最近采集/消息时间，不持久化 payload。
- Phase 6 不重试、不持久化、不无界排队失败发布；后续 scrape 继续运行。Phase 7 实现正式 Router 和 Kafka 时复用该线上契约。

## 12. 配置与安全边界

Monitor 至少新增以下服务器配置：

```text
MONITOR_HTTP_HOST=127.0.0.1
MONITOR_HTTP_PORT=9090
MONITOR_API_TOKEN=<minimum-32-bytes>
MONITOR_PLUGIN_ROOT=<absolute-path>
MONITOR_MAX_PACKAGE_BYTES=67108864
MONITOR_PLUGIN_START_TIMEOUT=5s
MONITOR_PLUGIN_STOP_TIMEOUT=5s
MONITOR_SHUTDOWN_TIMEOUT=10s
MONITOR_SCRAPE_INTERVAL=15s
MONITOR_SCRAPE_TIMEOUT=3s
MONITOR_MAX_METRICS_BYTES=1048576
MONITOR_ROUTER_URL=
MONITOR_ROUTER_TOKEN=
MONITOR_PUBLISH_TIMEOUT=3s
```

Backend 新增：

```text
MONITOR_URL=http://127.0.0.1:9090
MONITOR_API_TOKEN=<same-internal-token>
MONITOR_REQUEST_TIMEOUT=30s
MONITOR_MAX_PACKAGE_BYTES=67108864
```

- port 必须位于 `1..65535`；start/stop timeout 允许 `1s..30s`，shutdown 允许 `1s..60s`，scrape interval 允许 `1s..5m`，scrape timeout 允许 `100ms..30s` 且必须严格小于 interval，publish timeout 允许 `100ms..30s`，Backend Monitor request timeout 允许 `1s..60s`。
- `MONITOR_PLUGIN_ROOT` 必须是已规范化绝对路径，不得为 `/`、用户 home 或仓库根；创建后不跟随根目录替换或越界 symlink。
- Redis Exporter 继续使用 Phase 5 固定的 `REDIS_*` 与 `REDIS_EXPORTER_*` 环境变量；Plugin Manager 只传递该白名单和必需运行字段，不盲目继承 Monitor 全部环境。
- `MONITOR_ROUTER_TOKEN` 只在 Router URL 非空时必填；URL 必须是无 credentials/query/fragment 的 HTTP(S) base URL。
- 日志和 HTTP 错误不输出 token、Cookie/JWT、插件 archive 内容、Redis 凭据、绝对路径、进程命令行、原始 metrics body 或下游响应。

## 13. Bash 生命周期、安装包和隔离验收

- 新增可重复的 Redis Exporter 插件打包入口，从 Phase 5 源码构建当前 Linux/arch 可执行文件，生成固定 Manifest 和 digest，使用稳定顺序/时间/所有者创建 `tar.gz`。
- `scripts/dev.sh` 构建 Monitor，配置 `$REPO_ROOT/.run/plugins`，在基础设施与 Backend 就绪后启动 Monitor；Exporter 不再由脚本直接启动。
- `scripts/verify.sh` 保持只读，校验 Monitor PID 归属、health/readiness、已安装插件运行状态和实际 `/metrics` 基础契约。
- `scripts/down.sh` 先要求 Monitor 有界停止它管理的 Exporter，再校验并停止 Monitor；不使用旧 Exporter 记录重复发信号。
- 新增 `scripts/verify-monitor.sh`，使用随机白名 token 派生独立 Compose project、Redis/Backend/Monitor/Exporter/捕获端端口、数据库、插件根、进程目录、volume 和临时包。
- `--self-test` 只执行无 Docker 的路径、token、PID、project/container/port 归属与拒绝破坏性目标测试；默认模式执行真实安装、启停、更新、回滚、重启恢复、采集和 Publisher 捕获。
- 成功、失败、超时和中断都只清理本次带强归属证据的进程、目录、container、network、volume、端口和临时文件，并对比日常栈前后快照。

## 14. 跨批次依赖与摘要

```text
Phase-06-01 管理员身份与双用户态授权边界（1.3.1）
  ↓
Phase-06-02 插件安装包与生命周期管理闭环（1.3.2）
  ↓
Phase-06-03 MetricsMonitor 周期采集与标准消息闭环（1.3.3）
  ↓
Phase-06-04 集成验收与阶段收口（1.3.4）
  ↓
Phase 7 Message Router 与 Kafka
```

- [Phase-06-01：管理员身份与双用户态授权边界](Phase-06-01-管理员身份与双用户态授权边界.md)：交付持久化角色、运维提升 CLI、当前用户契约、统一 admin 中间件和社交/可观测权限矩阵。
- [Phase-06-02：插件安装包与生命周期管理闭环](Phase-06-02-插件安装包与生命周期管理闭环.md)：交付安全安装包、Monitor Plugin Manager、Backend 管理代理与真实 Exporter 生命周期。
- [Phase-06-03：MetricsMonitor 周期采集与标准消息闭环](Phase-06-03-MetricsMonitor周期采集与标准消息闭环.md)：将已管理的 Redis Exporter 转换为可调度 target，交付真实 Prometheus 解析、Envelope v1 和 HTTP Publisher 捕获。
- [Phase-06-04：集成验收与阶段收口](Phase-06-04-集成验收与阶段收口.md)：不新增功能，在同一最终构建上验证身份、管理和采集闭环、安全回滚、恢复、资源归属和 Phase 7 交接。

三个纵向功能批次加一个集成收口批次超出阶段提纲默认 2～3 批，是因为持久化角色、会话语义和服务端授权属于独立安全边界，必须先于插件文件/进程管理完成并拥有单独迁移与社交回归证据；其余批次仍按用户可验证的纵向闭环切分，没有按 Backend、Monitor、解包、调度、消息模型或测试等技术层机械分割。

## 15. 测试策略与固定验收矩阵

### 15.1 执行效率与停止规则

- 每批开始时只核对本批直接契约、前一批实施记录和最新公共接口，在 10 分钟内进入首个范围内生产修改。
- 无具体编译、运行或必需测试失败时不读取第三方依赖源码；新测试只证明新验收标准、复现真实缺陷或保护授权/文件/进程/消息公共边界。
- 最终 diff 上固定门禁各执行一次；修复后只重跑受影响的命令或场景，已成功结果不因上下文压缩而重复。
- 当固定验收通过且无阻断失败时，立即更新实施记录、版本并停止；不追加通用 RBAC、插件签名、多 target、性能压测或机会性重构。

### 15.2 批次验证边界

| 批次 | 本批直接证据 | 固定必要回归 | 明确留后/不重复 |
| --- | --- | --- | --- |
| Phase-06-01 | role 迁移、提升 CLI、当前用户 role 契约、数据库实时 admin 授权、`401/403/允许` | Backend/Frontend 认证、迁移、作者摘要和必要社交回归 | 不实现插件、Monitor、管理页面或通用 RBAC |
| Phase-06-02 | archive 安全、管理 API、自动安装启动、幂等启停、更新回滚、重启恢复、Backend 流式代理 | Phase-06-01 授权契约、Phase 5 Exporter、脚本进程归属 | 不实现采集调度/Envelope；不重做身份系统 |
| Phase-06-03 | 真实 Redis 数值，非重叠周期，`200/up1`、`503/up0`、恢复，严格解析，Envelope，HTTP 捕获 | Monitor 生命周期、Exporter 契约、Backend 状态代理、脚本/CI | 不实现 Router/Kafka/存储；不添加多 target 排列 |
| Phase-06-04 | 管理员到插件与 Redis 到 HTTP 捕获端的最终闭环，双用户态隔离、重启、回滚、发布失败、资源清理 | Phase 0～5 必要业务回归，Monitor/Backend/Frontend/Exporter/脚本固定门禁和远程 CI | 不新增功能；不做一般审计、长时压测或全排列故障测试 |

### 15.3 阶段级封闭端到端矩阵

`scripts/verify-monitor.sh` 在可验证归属的隔离资源中固定覆盖：

1. 注册普通用户并通过运维 CLI 提升指定账号；普通用户对全部插件接口获得 `403`，管理员沿用同一登录/Cookie 成功操作。
2. 生成真实 Redis Exporter `tar.gz`，通过 Backend multipart 上传；Monitor 流式接收、受限解包、原子安装、自动启动并在 health 通过后返回 `201/running`。
3. 对路径穿越、symlink/hardlink/device、压缩/解压超限、错误 digest、平台不符、未知 Manifest 字段、重复版本和降级包返回稳定安全错误，且没有越界文件或半安装 Registry。
4. 重复 start/stop 保持幂等；停止和强制退出只能针对归属完整匹配的进程，伪造/过期 PID 记录不得导致误杀。
5. 运行中插件成功更新到更高版本并保持 running；新版启动/health 失败时回滚 current/Registry，恢复旧版和原 desired state。
6. Monitor 正常重启后恢复 running 插件；stopped 插件不被启动；不完整单项进入 failed 但 Monitor API 仍 ready。
7. 对真实 Redis 写入 key 并执行代表性命令，捕获的 `success` Envelope 与同一 Redis `INFO` 及 Exporter `/metrics` 值一致，证明非静态数据。
8. 停止 Redis 后 Exporter 保持同一进程，MetricsMonitor 将严格 `503/up0` 封装为 `target_unavailable`；恢复 Redis 后无需重启任何进程即重新产生 `success`。
9. 超时、超限、畸形 Prometheus、重复/非有限样本不生成 Envelope；上一次成功 payload 不被重放，错误只以安全状态表达。
10. HTTP 捕获端校验 Bearer token、`Idempotency-Key`、Envelope 完整字段和 `202`；捕获端不可用后后续周期继续且不建立磁盘队列。
11. 日常 `dev.sh → verify.sh → down.sh` 可同时管理既有应用、Monitor 和 Monitor 所有的 Exporter，不存在双重启动、遗留端口或误杀。
12. 成功、失败和中断清理只移除本次随机 project、container、network、volume、插件根、进程和临时文件，日常栈快照不变。

以上是封闭矩阵。除非真实失败证明角色迁移、共享 Bash 归属或 Phase 5 契约存在回归，不追加通用 RBAC 矩阵、多架构包、高并发安装、长时稳定性或网络故障全排列。

## 16. CI 与固定完成门槛

- Reusable Quality Gates 新增独立 `Monitor` job，以 `monitor/go.mod`/`go.sum` 为 Go 版本和缓存依据，执行 formatting、unit、vet、race 与所需真实 Redis Exporter integration。
- Backend job 覆盖 role 迁移、授权、管理代理和错误映射；Frontend job 覆盖新当前用户 `role` 类型及已有页面回归。
- Scripts and Compose job 增加 Monitor/打包 Bash 的 LF、syntax、self-test 与回环端口检查，保留 Phase 5 Exporter 和已有业务门禁。
- Phase-06-04 最终远程门禁至少包含 Branch governance、Backend、Frontend、Redis Exporter、Monitor、Scripts and Compose、Integration 以及仓库当时实际配置的自动 PR/合并检查。
- 只能在实施记录中写入实际执行的本地命令、Pull Request、提交和远程结果；计划命令不得预写为已通过。

## 17. 实施记录规则

每批完成后创建同名镜像记录：

```text
dev/imple/Phase-06/Phase-06-01-管理员身份与双用户态授权边界.md
dev/logs/Phase-06/Phase-06-01-管理员身份与双用户态授权边界.md

dev/imple/Phase-06/Phase-06-02-插件安装包与生命周期管理闭环.md
dev/logs/Phase-06/Phase-06-02-插件安装包与生命周期管理闭环.md

dev/imple/Phase-06/Phase-06-03-MetricsMonitor周期采集与标准消息闭环.md
dev/logs/Phase-06/Phase-06-03-MetricsMonitor周期采集与标准消息闭环.md

dev/imple/Phase-06/Phase-06-04-集成验收与阶段收口.md
dev/logs/Phase-06/Phase-06-04-集成验收与阶段收口.md
```

记录必须包含实际完成工作、实际变更文件、验证命令与结果、相对方案偏差、已知限制和跟进项。规划阶段不创建空记录，不把未来验证写成已完成。

## 18. Phase 6 验收、完成与 Phase 7 交接

Phase 6 完成必须同时满足：

- 管理员沿用普通登录与 Cookie 操作插件，普通用户对全部插件读写接口稳定获得 `403`。
- Redis Exporter 安装包可通过 Backend 导入，安全解压到固定布局，自动启动并通过 health；无效包和启动失败不留残缺安装。
- start/stop 幂等，运行更新保持 desired state，更新失败原子回滚，Monitor 重启恢复 running 插件且不误杀不归属进程。
- MetricsMonitor 立即并每 15 秒采集真实 Exporter，不并发重叠，可区分并处理成功、目标不可用、超时和畸形数据。
- 成功和严格 `503/up0` 输出已被转换为 Envelope v1，HTTP 捕获端可读取完整消息，Monitor 不依赖 Kafka SDK，不承担 Router、Marshaller 或存储职责。
- 日志、API、Registry、Envelope 和实施记录不泄漏凭据、token、绝对路径、用户内容、原始 metrics 或未清洗底层错误。
- Phase 0～5 必要业务、搜索、异步处理、结构化日志与 Exporter 在 Monitor 接管后无回归，固定矩阵和远程门禁通过。
- 四份实施记录与实际提交一致，Phase-06-04 合入后根与 Frontend 版本均为 `1.3.4`。

只有 Phase-06-01、Phase-06-02、Phase-06-03 和 Phase-06-04 都从权威分支完成并合入主远程，第 15.3 节封闭矩阵在 WSL2/Bash 真实通过，远程门禁成功且实施记录齐全，才可标记 Phase 6 完成。达到条件后立即停止扩展。

向 Phase 7 交付：

- 稳定的 `user|admin` 身份、同一会话模型和权限矩阵；后续可观测查询/管理只能通过 Backend admin 授权，内部组件不形成浏览器入口。
- 可独立运行的 Monitor、稳定的 Plugin Manager 状态和管理员代理 API。
- 已管理、可恢复、可更新回滚的 Redis Exporter 以及固定 target ID。
- Envelope v1 的完整 schema、成功/目标故障语义、稳定序列化和真实捕获证据。
- `POST /internal/v1/messages`、Bearer token、`Idempotency-Key` 和 `202 Accepted` 的 HTTP Publisher 契约。
- 明确保留给 Phase 7 的工作：Message Router 服务、消息类型路由、Kafka Producer/Topic、Consumer 验证与可观测消息传输可靠性。
