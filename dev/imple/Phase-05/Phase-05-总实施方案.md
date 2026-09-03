# Phase 5：Exporter Plugin 原型总实施方案

## 1. 实施目标

在 Phase 4 完成的业务系统与统一日志基线上，交付 GoPulse 第一个独立、可运行、可被拉取的 Redis Exporter。Exporter 只在收到 HTTP 采集请求时连接一个真实 Redis 目标，将当前 `INFO` 数据转换为固定的 Prometheus text exposition 0.0.4 指标；它不主动推送、不保存历史数据，也不因 Redis 暂时不可用而退出。

本阶段固定形成以下最小闭环：

```text
真实 Redis 7.2.x
    ↑ 每次请求即时执行有界 INFO
Redis Exporter 常驻进程
    ├── GET /health  → 进程存活状态
    └── GET /metrics → HTTP 200 + 当前指标
                      或 HTTP 503 + gopulse_redis_up 0
```

阶段完成必须同时证明真实指标采集、被动拉取、目标故障隔离、无需重启的自动恢复、有界退出和仓库生命周期集成。静态样例指标、后台定时采集、只验证 Redis 连接或在故障时直接退出均不构成完成。

## 2. 当前真实基线

本方案编写时的规划基线是主远程 `main` 提交 `7566c65` 及根版本 `0.4.4`：

- Phase 3 已交付 MySQL、Redis、RabbitMQ、Elasticsearch、Backend、Business Worker、Search Indexer 和 Frontend 的本地运行闭环。
- Redis 固定为 `redis:7.2.5-alpine`，启用密码认证与 AOF，默认通过回环地址发布端口；Backend 使用 `REDIS_HOST`、`REDIS_PORT`、`REDIS_PASSWORD` 和 `REDIS_DB`。
- `scripts/dev.sh`、`scripts/down.sh` 和 `scripts/verify.sh` 已使用 `.run/*.json` 对本地应用进程执行 cwd、可执行文件、启动 ticks 和命令标记校验。
- `scripts/verify-business.sh` 已具备随机 Compose project、端口、数据库、临时目录、volume 与 PID 归属保护，可作为隔离验收模式的基础。
- `exporters/` 目前只有 Phase 0 占位说明；仓库不存在 Exporter 模块、指标协议、采集器、HTTP 服务或管理接口。
- 当前 CI 只为 Backend、Frontend、脚本/Compose 和既有集成测试设置门禁；尚无独立 Exporter job。
- Phase 4 尚未实施。Phase 5 可以提前完成规划，但不得从当前 `0.4.4` 基线直接创建开发分支或开始实现。

Phase 5 实施开始时必须重新核对最新主远程。若 Phase 4 最终改变结构化日志、Bash 生命周期或验收入口契约，应先更新本总方案和未开始的拆分方案，再创建 `develop/1.2.x` 分支。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Milestone 1 的 `1.0.0` release-only 动作已合入主远程 `main`，Phase 4 全部批次也已完成、合入并通过其固定验收。
- 主远程根版本处于 Phase 4 的 `1.1.x` 完成版本，Phase 4 实施记录与实际提交一致。
- 每批开始前 fetch 主远程，从包含全部前置批次的最新 `main` 创建本方案分配的独立分支；不得沿用 `update` 或已完成的开发分支。
- 在 Windows 宿主机的 WSL2 Linux filesystem 中实施和验收，使用 Bash 与一个明确的 Docker daemon；活动仓库不得位于 `/mnt/c`、`/mnt/d` 等 Windows 挂载目录。
- 开始前记录 Git、日常 Compose 栈、volume 与 `.run` 进程状态，不覆盖、暂存、停止或提交用户及其他任务的资源。

### 3.2 权威批次、版本与开发分支

Phase 5 使用 `1.2.x` 版本线，`1.2.0` 只作为阶段基线，不创建空批次。下表是本阶段批次、顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-05-01 | `1.2.1` | `develop/1.2.1` | 已完成并合入 `main`（PR #58） |
| Phase-05-02 | `1.2.2` | `develop/1.2.2` | 本地验收完成，待远程门禁与合入 |

执行规则：

- 两批全部提交分别共享其目标版本；每批完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建同名 `dev/logs/Phase-05/Phase-05-XX-*.md`，只记录实际完成工作和实际验证结果。
- Phase-05-01 是唯一功能实现批次，纵向交付成功采集、故障隔离、运行集成和定向验收。
- Phase-05-02 是固定的集成验收与阶段收口批次，不引入新 Exporter 功能，只允许最小修复真实验收暴露的阻断问题。
- 已推送分支不得静默改名或重新编号；批次数量或顺序变化时，先更新本表，再创建尚未开始的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- `exporters/redis` 独立 Go module、独立配置、Redis 采集器、Prometheus 编码与常驻 HTTP 进程。
- 对一个受密码保护的 Redis 7.2.x 实例和一个配置数据库执行按请求即时采集。
- `GET /health` 进程存活接口和 `GET /metrics` 指标/采集失败接口。
- 真实 Redis 指标、固定命名/类型/标签契约，以及无陈旧值的失败响应。
- Redis 停止、认证错误、连接超时和响应异常时的有界失败、进程存活及目标恢复后自动恢复。
- `SIGINT`/`SIGTERM` 有界关闭、结构化脱敏日志与默认回环监听。
- Bash 日常启停、只读检查、进程归属保护、独立隔离验收和 CI Exporter 门禁。
- Exporter 运行说明、Phase 6 采集与生命周期接管所需的固定进程/端点边界。

### 4.2 明确不做

- 不实现 MySQL Exporter、多个 Redis 目标、动态服务发现或任意自定义指标查询。
- 不主动向 Monitor、Backend、Kafka、VictoriaMetrics 或其他服务推送数据。
- 不缓存上次成功采集结果，不建立本地文件、数据库、时间序列或累计采集历史。
- 不实现 Plugin Manager、插件安装/更新 API、Backend 管理 API 或 Frontend 管理页面。
- 不定义 GoPulse 标准 metrics Envelope；该转换属于 Phase 6 的 MetricsMonitor。
- 不引入 Kafka、Message Router、Marshaller 或 VictoriaMetrics；分别由 Phase 7 和 Phase 8 完成。
- 不为应用创建 Docker image 或将 Exporter 作为 Compose 应用服务；应用容器化属于 Phase 12。
- 不实现 Exporter HTTP 鉴权、TLS、跨主机暴露、限流平台、生产级高可用或容量压测。
- 不修改冻结的 `scripts/*.ps1`，不增加 Windows runner 或原生 Windows 验收。

## 5. 模块与运行架构

### 5.1 独立模块

`exporters/redis` 建立独立 Go module，建议结构如下：

```text
exporters/redis/
├── go.mod
├── go.sum
├── cmd/redis-exporter/
├── internal/config/
├── internal/collector/
├── internal/httpserver/
└── README.md
```

- module path 固定为 `github.com/Ray-ymq/GoPulse/exporters/redis`，Go 版本与实施时仓库基线一致。
- 不导入 `backend/internal/*`，不依赖 Backend、Frontend、RabbitMQ 或 Elasticsearch 初始化。
- 不新增根 `go.work`；Backend 与 Redis Exporter 分别在自己的 module 中构建、测试和管理依赖。
- 使用 `github.com/redis/go-redis/v9` 连接 Redis，使用 Prometheus 官方 Go 库的 DTO/编码能力生成文本格式；不注册默认 Go/runtime/process collectors。
- 每次 scrape 先得到完整的内存快照，再统一编码响应；采集失败时丢弃本次所有非 `up` 样本，不能混合部分值或上次值。

### 5.2 进程职责

- 启动时严格校验全部配置，建立惰性 Redis client 和 HTTP server；启动不得要求 Redis 当时可用。
- 只有 `/metrics` handler 可以触发 Redis `INFO`；`/health`、启动、空闲等待和后台 goroutine 均不得访问目标 Redis。
- 复用连接池仅是连接资源管理，不得保存指标数据；一次 scrape 使用一个有截止时间的 context。
- 收到 `SIGINT` 或 `SIGTERM` 后停止接受新请求，在关闭时限内等待在途请求并关闭 Redis client；超时返回非零。
- 监听失败、无效配置或不可恢复的 HTTP server 错误使进程非零退出；单次目标采集失败只影响该 HTTP 响应。

## 6. HTTP 接口契约

### 6.1 `GET /health`

- 表达 Redis Exporter 进程存活，不探测 Redis，也不作为目标 readiness。
- 固定返回 HTTP `200`、`Content-Type: application/json` 和：

```json
{"status":"ok","service":"redis-exporter"}
```

- Redis 停止、认证失败或尚未启动时仍返回上述结果。
- 非 `GET` 方法返回 `405 Method Not Allowed`；未知路径返回 `404 Not Found`。

### 6.2 `GET /metrics`

- 响应 `Content-Type` 固定为 `text/plain; version=0.0.4; charset=utf-8`，并设置 `Cache-Control: no-store`。
- 成功时即时执行一次 Redis `INFO server clients memory stats cpu keyspace`，返回 HTTP `200`、全部固定指标和 `gopulse_redis_up 1`。
- 连接拒绝、DNS/网络错误、认证失败、context 超时、Redis 命令失败或必填字段解析失败统一返回 HTTP `503`。
- 失败正文必须仍是合法 Prometheus 文本，只包含 `gopulse_redis_up` 的 `HELP`、`TYPE` 与值 `0`；不得返回其他部分指标或上次成功值。
- 配置数据库在 `INFO keyspace` 中不存在时，keys 与 expiring keys 按 `0` 输出，不视为采集失败。
- 非 `GET` 方法返回 `405`；接口不接受 query、请求体、目标地址、命令、字段或标签参数。

## 7. 指标契约

首版指标名称、类型和 Redis 来源固定如下：

| 指标 | 类型 | Redis `INFO` 来源 | 说明 |
| --- | --- | --- | --- |
| `gopulse_redis_up` | gauge | 本次 scrape 结果 | 成功为 `1`，任一采集失败为 `0` |
| `gopulse_redis_uptime_seconds` | gauge | `uptime_in_seconds` | 当前 Redis 进程运行时长 |
| `gopulse_redis_connected_clients` | gauge | `connected_clients` | 当前客户端连接数 |
| `gopulse_redis_used_memory_bytes` | gauge | `used_memory` | 当前 Redis 已使用内存 |
| `gopulse_redis_commands_processed_total` | counter | `total_commands_processed` | Redis 自启动以来处理的命令数 |
| `gopulse_redis_keyspace_hits_total` | counter | `keyspace_hits` | Redis 自启动以来 keyspace 命中数 |
| `gopulse_redis_keyspace_misses_total` | counter | `keyspace_misses` | Redis 自启动以来 keyspace 未命中数 |
| `gopulse_redis_cpu_seconds_total{mode="user"}` | counter | `used_cpu_user` | Redis 用户态 CPU 秒数 |
| `gopulse_redis_cpu_seconds_total{mode="system"}` | counter | `used_cpu_sys` | Redis 内核态 CPU 秒数 |
| `gopulse_redis_db_keys{db="<REDIS_DB>"}` | gauge | `dbN.keys` | 配置数据库当前 key 数 |
| `gopulse_redis_db_expiring_keys{db="<REDIS_DB>"}` | gauge | `dbN.expires` | 配置数据库当前带过期时间的 key 数 |

契约约束：

- 每个 metric family 必须带固定 `HELP` 和正确 `TYPE`；输出只包含有限数值与固定 `mode`/数字 `db` 标签。
- 不把 Redis host、port、密码、原始错误、版本字符串、命令名、key、用户数据或请求内容写入标签。
- `NaN`、`Inf`、负的计数器、整数溢出、重复必填字段或无效数值使本次 scrape 整体失败。
- 指标值是 Redis 当前 `INFO` 快照；Exporter 不对命中率、请求率、CPU 使用率等跨 scrape 派生值做计算。
- 该文本契约是 Phase 6 MetricsMonitor 的采集输入，但 GoPulse 标准消息中的 `type/source/timestamp/payload` 仍由 Phase 6 定义和封装。

## 8. 配置、安全与故障边界

### 8.1 配置

目标 Redis 复用已有变量：

```text
REDIS_HOST
REDIS_PORT
REDIS_PASSWORD
REDIS_DB
```

新增 Exporter 专属变量：

```text
REDIS_EXPORTER_HTTP_HOST=127.0.0.1
REDIS_EXPORTER_HTTP_PORT=9121
REDIS_EXPORTER_SCRAPE_TIMEOUT=2s
REDIS_EXPORTER_SHUTDOWN_TIMEOUT=5s
```

- host 必须非空；端口必须为 `1..65535`；DB 必须为非负整数。
- scrape timeout 固定允许 `100ms..10s`，shutdown timeout 固定允许 `1s..30s`。
- 默认只监听回环地址；显式非回环配置是调用方的远程暴露选择，Phase 5 不为其提供鉴权或 TLS 承诺。
- 配置错误必须在监听前报告稳定、脱敏的字段级错误并非零退出；不得把密码拼入地址后再记录或返回。

### 8.2 日志与故障表达

- 使用 Phase 4 已确定的 JSON 结构化日志字段与级别约定；`service` 固定为 `redis-exporter`，模块至少区分 `config`、`http`、`collector` 和 `runtime`。
- scrape 失败日志只记录有限 reason code，例如 `redis_unavailable`、`redis_auth_failed`、`redis_timeout`、`redis_response_invalid`，不记录原始连接串、密码或完整 Redis 响应。
- HTTP `503` 是目标采集失败；`/health` 的 `200` 证明 Exporter 自身仍活着。不得通过使 `/health` 失败或退出进程来表达目标故障。
- Redis 恢复后下一次 `/metrics` 必须直接重新采集并返回 `200`，不要求重启 Exporter、清理缓存或执行管理操作。

## 9. Bash 生命周期与验收资源

- `scripts/dev.sh` 构建 `.run/bin/gopulse-redis-exporter`，在基础设施健康后启动 Exporter，并创建 `.run/redis-exporter.json`。
- PID 记录继续绑定进程 cwd、绝对 executable、start ticks 和 command marker；Redis Exporter cwd 固定为 `exporters/redis`。
- 端口冲突检查加入 `REDIS_EXPORTER_HTTP_PORT`，与 Backend、Frontend 和基础设施端口执行两两冲突校验。
- `scripts/verify.sh` 保持只读，验证 Exporter PID 归属、`/health` 契约和正常运行栈下 `/metrics` 的 HTTP/Prometheus 基础契约。
- `scripts/down.sh` 先校验后停止 Exporter，且只删除匹配记录；停止顺序为 Frontend、Redis Exporter、Search Indexer、Business Worker、Backend。
- 新增 `scripts/verify-exporter.sh`，使用随机白名单 token 派生独立 Compose project、Redis 端口、Exporter 端口、临时配置、进程目录和 volume；停止 Redis 或删除 volume 前必须验证 project label、container ID、端口绑定和进程身份。
- `--self-test` 只执行无 Docker 的负向归属测试；默认模式执行真实成功、故障、恢复和清理矩阵。
- 成功、失败、超时和信号退出均清理本次隔离进程、容器、network、volume 与临时文件，并对比日常栈前后快照。

Phase 6 接入 Plugin Manager 时可以改变 Redis Exporter 的启动所有者，但必须复用本阶段的 executable、环境变量、端点、信号关闭和进程身份边界；不得把 Phase 5 的 `dev.sh` 所有权误写成永久插件管理架构。

## 10. 跨批次依赖与摘要

```text
Phase-05-01 Redis Exporter 采集与故障隔离闭环（1.2.1）
  ↓
Phase-05-02 集成验收与阶段收口（1.2.2）
  ↓
Phase 6 MetricsMonitor 与 Plugin Manager
```

- [Phase-05-01：Redis Exporter 采集与故障隔离闭环](Phase-05-01-Redis-Exporter采集与故障隔离闭环.md)：交付本阶段全部生产能力及定向真实验收，使一个 Redis Exporter 可以独立运行、采集、失败并恢复。
- [Phase-05-02：集成验收与阶段收口](Phase-05-02-集成验收与阶段收口.md)：不新增功能，在同一最终构建上完成跨组件、资源安全、必要业务回归、文档状态和 Phase 6 交接。

一个纵向功能批次加一个固定集成收口批次符合阶段提纲的 2～3 批约束；没有按配置、采集器、HTTP、脚本或测试等技术层机械拆分。

## 11. 测试策略与固定矩阵

### 11.1 执行效率与停止规则

- 每批只读取直接受影响代码、Phase 4 最终公共契约和固定验收入口，在 10 分钟内进入实现。
- 没有具体编译、运行或必需测试失败时不读取第三方依赖源码；新测试只证明本方案验收、复现真实缺陷或保护进程/资源边界。
- 最终 diff 上固定门禁各执行一次；修复后只重跑可能受影响的命令或场景，上下文压缩不触发重复检查。
- 固定门禁通过且无阻断验收的失败后立即更新实施记录、版本并提交，不追加 MySQL 指标、多目标、性能测试或机会性重构。

### 11.2 批次验证边界

| 批次 | 本批直接证据 | 固定必要回归 | 明确留后/不重复 |
| --- | --- | --- | --- |
| Phase-05-01 | 配置、INFO 解析、Prometheus 契约、真实 Redis 成功/失败/恢复、Bash PID 归属 | 脚本/Compose、版本治理、Phase 4 日志格式兼容 | 不实现 Monitor/管理 API；不重复完整业务故障矩阵 |
| Phase-05-02 | Phase 0～5 同栈、被动采集、故障隔离、有界退出、资源清理、Phase 6 交接 | Exporter 全门禁、Backend/Frontend/脚本必要回归、远程 CI | 不新增指标或配置；不做压测、多版本 Redis 或多故障排列 |

### 11.3 阶段级端到端验收矩阵

`scripts/verify-exporter.sh` 在随机且可验证归属的隔离资源中固定覆盖：

1. 使用密码启动真实 Redis 7.2.5 与 Redis Exporter；`/health` 返回固定 JSON，首次 `/metrics` 返回 `200` 和 `up 1`。
2. 写入代表性 key 并执行命中、未命中和普通命令后，将 Exporter 样本与同一 Redis 的 `INFO` 数值核对，证明不是静态指标。
3. 验证全部 metric family 的名称、类型、有限标签和数值，配置 DB 无 key 时两个 DB gauge 为零。
4. 在 collector spy 单元测试中多次请求 `/health` 并等待空闲窗口，确认未触发采集；只有 `/metrics` 调用 collector。
5. 停止隔离 Redis 后，`/metrics` 在 scrape timeout 上限内返回 `503` 和唯一的 `up 0` 样本；`/health` 仍为 `200`，Exporter PID/启动身份不变。
6. 以错误密码启动或切换隔离场景，验证认证失败使用相同公共 `503` 契约，日志与响应不含凭据或原始地址。
7. 恢复同一 Redis 后，不重启 Exporter 即可在下一次 `/metrics` 获得 `200`、`up 1` 与当前值，失败期间没有陈旧样本。
8. 发送 `SIGTERM`，Exporter 在 shutdown timeout 内退出且不遗留监听端口或子进程；异常记录不能导致误杀无关 PID。
9. 成功、失败和中断清理只移除本次 project、container、network、volume、进程与临时目录，日常栈快照保持不变。
10. 日常 `dev.sh → verify.sh → down.sh` 可同时管理既有应用和 Redis Exporter；Phase 0～4 的必要业务闭环无回归。

以上是封闭矩阵。除非真实失败证明共享脚本、Phase 4 日志基础或 Redis 使用边界存在回归，不追加 Redis 版本矩阵、高并发 scrape、长时稳定性或网络故障全排列。

## 12. CI 与固定完成门槛

- Reusable Quality Gates 新增独立 `Redis Exporter` job，以 `exporters/redis/go.mod` 和 `go.sum` 作为 Go 版本/缓存依据，执行 formatting、unit、vet、race 和真实 Redis integration。
- 既有 Backend job 继续只在 `backend` module 运行；不得用根 `go.work` 合并模块或改变 Backend 依赖边界。
- Scripts and Compose job 增加 Exporter Bash 文件的 LF/syntax/self-test 检查，并继续验证回环发布数量和现有脚本。
- Phase-05-02 的最终远程门禁至少包含 Branch governance、Backend、Frontend、Redis Exporter、Scripts and Compose、Integration 以及仓库当时实际配置的自动 PR/合并检查。
- 只能记录实际观察到的本地命令、Pull Request、提交和远程检查结果；计划命令不得预写为已通过。

## 13. 实施记录规则

每批完成后创建同名镜像记录：

```text
dev/imple/Phase-05/Phase-05-01-Redis-Exporter采集与故障隔离闭环.md
dev/logs/Phase-05/Phase-05-01-Redis-Exporter采集与故障隔离闭环.md

dev/imple/Phase-05/Phase-05-02-集成验收与阶段收口.md
dev/logs/Phase-05/Phase-05-02-集成验收与阶段收口.md
```

记录必须包含实际完成工作、实际变更文件、验证命令与结果、相对方案偏差、已知限制和跟进项。本次规划不创建空实施记录，不把未来测试写成已完成。

## 14. Phase 5 验收标准

- 独立 Redis Exporter 可以在没有 Backend、Frontend、RabbitMQ、Elasticsearch 或 Monitor 的情况下启动和响应。
- `/health` 只表达进程存活；目标 Redis 不可用时仍为 `200`，且 Exporter 进程保持存活。
- `/metrics` 成功时返回来自真实 Redis 当前 `INFO` 的固定 Prometheus 指标，失败时在有界时间内返回 `503` 与 `gopulse_redis_up 0`。
- Exporter 只在 `/metrics` 请求中采集，不主动推送、不后台轮询、不保存历史或上次成功样本。
- 指标名称、类型、来源与标签符合第 7 节；输出和结构化日志不泄漏密码、连接串、key 或原始目标错误。
- Redis 停止或认证失败不导致进程退出；Redis 恢复后无需重启 Exporter 即恢复正常采集。
- `SIGINT`/`SIGTERM`、HTTP 和 Redis 操作均有固定时限，启动/退出错误可诊断且不误伤其他进程。
- Bash 生命周期、只读验证、隔离验收和 CI Redis Exporter job 可用，PowerShell 保持 `0.2.1` 历史基线。
- Phase 0～4 的必要能力与 Exporter 可共同运行；固定矩阵和远程门禁通过，没有使阶段验收不成立的失败。
- 两份实施记录与实际提交一致，Phase-05-02 完成后根与 Frontend 版本均为 `1.2.2`。

## 15. 完成、停止与 Phase 6 交接

只有 Phase-05-01 与 Phase-05-02 都从权威分支完成并合入主远程，第 11.3 节封闭矩阵在 WSL2/Bash 真实通过，远程门禁成功且实施记录齐全，Phase 5 才可标记完成。达到条件后立即停止扩展；未执行的检查、PR、合并或远程结果不得写成通过。

截至 2026-09-03，Phase-05-01 已由 PR #58 合入 `main`，其最终 push workflow 的 Branch governance、Backend、Frontend、Redis Exporter、Scripts and Compose、Integration 与自动 PR/合并 job 均成功。Phase-05-02 已在 `develop/1.2.2` 完成本地实现与固定验收；在该分支实际合入且远程门禁成功前，本总方案保持 Phase 5 尚未完成。

向 Phase 6 交付：

- 独立可执行的 `gopulse-redis-exporter` 及其环境变量、信号关闭和默认监听契约。
- 稳定的 `/health` 与 `/metrics` HTTP 契约，以及固定 Prometheus 指标名称、类型和失败语义。
- 真实成功、目标故障、恢复和资源清理的自动化证据。
- Phase 6 可复用的进程身份与运行说明；Plugin Manager 可以接管启停所有权，但不得改变 Exporter 被动拉取、无历史存储和单目标边界。
