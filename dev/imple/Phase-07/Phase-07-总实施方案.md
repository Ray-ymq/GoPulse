# Phase 7：Message Router 与 Kafka 总实施方案

## 1. 实施目标

在 Phase 6 已交付 MetricsMonitor、GoPulse metrics Envelope v1 和 HTTP Publisher 的基础上，交付可独立运行的 Message Router，并通过单节点 Kafka 建立统一可观测消息入口，使标准 metrics 消息能够在不引入 Kafka SDK 到 Monitor 的前提下成为 Phase 8 Marshaller 可直接消费的输入。

本阶段固定形成一条可独立验证的最小闭环：

```text
真实 Redis → Redis Exporter → MetricsMonitor 周期采集
          → POST /internal/v1/messages → Message Router
          → gopulse-observability-v1 → Kafka 验证 Consumer
```

阶段完成必须同时证明：Monitor 继续只依赖 HTTP Publisher 抽象；Router 完成内部服务鉴权、消息类型识别和 Kafka 路由；Kafka Consumer 读取到与 Router 接收正文完全一致的 metrics Envelope；Router/Kafka 故障只影响可观测消息发布，不改变普通用户社交 API、管理员授权或 RabbitMQ 业务任务。只验证 Router 单元测试、手工向 Kafka 写入、使用静态 JSON 绕过 Monitor，或只证明 Kafka 端口可连接，均不构成完成。

## 2. 当前真实基线

本方案编写时的产品代码基线是主远程 `main` 提交 `636fde1`，根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 均为 `1.3.4`：

- `monitor` 已是独立 Go 1.26 module，可从真实 Redis Exporter 周期采集并生成 Envelope v1。
- metrics Envelope 固定包含 `schema_version`、`message_id`、`type`、`source`、`timestamp` 和 `payload`；当前生产消息固定为 `schema_version=1`、`type=metrics`、`source=redis`。
- Monitor HTTP Publisher 已固定使用 `POST <MONITOR_ROUTER_URL>/internal/v1/messages`、`application/json`、`Authorization: Bearer <MONITOR_ROUTER_TOKEN>`、`Idempotency-Key=<message_id>`，且只有 `202 Accepted` 被视为成功。
- Monitor 的 Router URL 仍可为空；Publisher 不自动重试、不持久化消息且不形成无界队列，失败只记录有限 `publish_failed` 并继续下一周期采集。
- `router/` 和 `marshaller/` 仍只有占位 README，没有 Go module、HTTP 服务、Kafka 客户端或消费实现。
- `deploy/compose.yaml` 已包含 MySQL、Redis、RabbitMQ 和 Elasticsearch，但尚无 Kafka；应用进程仍由 Bash 在 WSL2 Linux filesystem 中运行。
- `scripts/dev.sh`、`down.sh` 和 `verify.sh` 已具备强归属的进程、端口、Compose project、volume 和插件根保护，Phase 7 必须沿用而不能另建弱化清理路径。
- CI 已覆盖 Backend、Frontend、Monitor、Redis Exporter、脚本/Compose 和业务集成，但尚无 Router module 或真实 Kafka 门禁。

Phase 7 实施开始前必须重新核对最新主远程、Phase 6 四份实施记录和上述线上契约。若 Phase 6 后续修复改变 Endpoint、Header、Envelope 或 Publisher 超时语义，必须先同步更新本总方案和所有尚未开始的拆分方案。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 6 全部批次和 Review 整改已合入主远程 `main`，远程固定门禁成功，实施记录与真实提交一致。
- 根与 Frontend 版本均为 `1.3.4`，真实 Redis → Exporter → MetricsMonitor → HTTP 捕获端链路已通过。
- 实施环境固定为 Windows 宿主上的 WSL2，仓库位于 Linux filesystem；Bash 是唯一维护的本地生命周期和验收入口。
- 每个开发批次开始前 fetch 主远程，从包含全部前置批次的最新 `main` 创建本方案分配的独立 `develop/x.x.x` 分支，不沿用 `update` 或已完成分支。
- 开始前保存 Git 状态、日常 Compose project/volume、`.run` 进程、端口和插件根快照，不停止、删除、暂存或提交其他任务资源。

### 3.2 权威批次、版本与开发分支

Phase 7 使用 `1.4.x` 版本线，`1.4.0` 只作为阶段基线，不创建空批次。下表是本阶段执行顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-07-01 | `1.4.1` | `develop/1.4.1` | 已合入 `main`（PR #68） |
| Phase-07-02 | `1.4.2` | `develop/1.4.2` | 已完成并合入 `main`（PR #69） |
| Phase-07-03 | `1.4.3` | `develop/1.4.3` | 本地整改与固定验收完成，待远程门禁和 PR 合入 |

执行规则：

- 同一批次全部提交共享目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建同名 `dev/logs/Phase-07/Phase-07-XX-*.md`，只记录实际改动、验证、偏差、失败和限制。
- Phase-07-01 交付从真实 Monitor HTTP 发布到 Kafka Consumer 的完整纵向能力，不按 HTTP、Router、Producer、Kafka、Consumer 或测试机械拆批。
- Phase-07-02 只执行真实跨组件集成验收、必要回归、文档和阶段状态收口；不得加入新 Topic、新消息类型或新产品能力。
- Phase-07-03 只关闭 2026-09-04 Phase 7 Review 的 P1/P2：严格 schema token、Producer 有界并发/取消、Router 验收矩阵与资源安全、端口唯一性和治理记录；不得扩展 Phase 8 能力。
- 已推送分支不得静默改名或重新编号；若批次数量或顺序在实施前变化，先更新本表并重新计算尚未创建的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 独立 `router` Go module、Message Router HTTP 进程、结构化日志、健康/就绪接口和有界退出。
- Phase 6 Publisher 线上契约的正式接收端，包括独立服务身份、请求上限、严格 Envelope 顶层校验和安全错误响应。
- `metrics` 消息类型识别及显式路由表；首版单一 Kafka Topic `gopulse-observability-v1`。
- Kafka Producer 的有界同步确认、受控缓冲、客户端内部幂等重试和安全故障表达。
- Kafka 官方镜像的单节点 KRaft 本地运行、显式建 Topic、loopback 发布、健康检查和隔离 volume。
- 验证 Consumer 对 Kafka record key、原始 value、消息 ID、类型和完整 Envelope 的读取校验。
- Monitor 到 Router 的日常配置接线，以及 Kafka、Router、Monitor 的 Bash 启停、只读验证和隔离验收。
- Router unit/race、真实 Kafka 集成、CI 门禁、README、版本元数据和实施记录。

### 4.2 明确不做

- 不实现 Marshaller、VictoriaMetrics、指标映射、清洗、聚合、过滤、存储或查询。
- 不实现 LogMonitor、EventMonitor，也不接受尚无稳定 Envelope 的 `logs` 或 `events` 消息。
- 不拆分 metrics/logs/events 多 Topic，不引入 Schema Registry、死信 Topic、重放 API 或长期保留/容量治理。
- 不实现应用级持久去重、Router 本地磁盘队列、跨请求事务或 exactly-once 端到端语义。
- 不把 Kafka 用于通知、索引、邮件等 RabbitMQ 业务任务，也不把两种消息系统抽象成统一总线。
- 不新增普通用户或管理员直接调用的 Router/Kafka 公共 API，不让 Backend 或浏览器代理写入 Kafka。
- 不实现 Kafka SASL/TLS、多 broker、副本容灾、ACL 平台或生产集群拓扑；本阶段依靠受控网络和 loopback 完成本地 MVP 边界。
- 不创建 Router 应用容器镜像，不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 组件与运行架构

### 5.1 独立 Router module

`router` 建立独立 Go module，建议结构：

```text
router/
├── go.mod
├── go.sum
├── cmd/router/
├── internal/config/
├── internal/envelope/
├── internal/httpserver/
├── internal/routing/
└── internal/kafka/
```

- module path 固定为 `github.com/Ray-ymq/GoPulse/router`，Go 版本与实施时仓库基线一致，初始 Kafka 客户端固定为 `github.com/twmb/franz-go v1.21.0`。
- 不新增根 `go.work`，不导入 `monitor/internal/*`、`backend/internal/*` 或未来 `marshaller/internal/*`；跨进程契约只通过固定 HTTP/JSON 和 Kafka record 表达。
- HTTP Server、Envelope Validator、Router 和 Kafka Producer 通过最小接口解耦，使请求边界与生产确认可在不启动 broker 的情况下做定向单元测试。
- Router 复用项目结构化 JSON 日志字段语义但不跨 module 导入 internal 实现；`service=router`，module 至少包含 `lifecycle`、`http`、`routing` 和 `kafka`。

### 5.2 运行职责和所有权

- MetricsMonitor 只负责采集、第一次基础校验、Envelope 封装和 HTTP 发布，不识别 Topic、不导入 Kafka SDK。
- Message Router 只负责接收、服务鉴权、Envelope 路由字段校验、类型到 Topic 的选择和 Kafka 写入确认。
- Kafka 只负责可观测数据传输和保留；不理解 metrics payload，不参与用户认证，不承担 RabbitMQ 业务任务。
- Phase 7 验证 Consumer 只用于读取并证明交接契约，不成为常驻产品服务；Phase 8 由 Marshaller 建立正式 Consumer group。
- Backend readiness、普通用户社交 API 和管理员授权均不依赖 Router/Kafka；发布链路故障不得导致业务服务退出或放宽权限。

### 5.3 启动和恢复顺序

日常顺序固定为：

```text
基础设施 Compose（含 Kafka）
→ 显式创建/核对 gopulse-observability-v1
→ Message Router HTTP 进程
→ Monitor 与其管理的 Redis Exporter
→ Backend / Workers / Frontend
```

- Router 在配置合法时允许 HTTP 进程启动，即使 broker 暂时不可用；`/health` 保持存活，`/ready` 和消息发布反映 Kafka 不可用。
- Kafka 与 Topic 恢复后，Router 必须通过客户端元数据刷新恢复，无需重启 Router 或 Monitor。
- 关闭时先停止 Monitor 及其 Exporter，避免新发布进入；随后有界关闭 Router Kafka client，再由 Compose 清理 Kafka。

## 6. HTTP、身份与错误契约

### 6.1 接口

| Method | Path | 身份 | 语义 | 成功状态 |
| --- | --- | --- | --- | --- |
| `GET` | `/health` | 无，但只允许受控监听 | 仅表达 Router 进程存活 | `200` |
| `GET` | `/ready` | Router Bearer token | 验证 Kafka broker 和目标 Topic 可用 | `200` |
| `POST` | `/internal/v1/messages` | Router Bearer token | 校验、路由并等待 Kafka 确认 | `202` |

- Router token 配置名固定为 `ROUTER_API_TOKEN`，必须至少 32 bytes 且不得包含 CR/LF；Monitor 使用同值的 `MONITOR_ROUTER_TOKEN`。
- Bearer token 使用常量时间比较；JWT、登录 Cookie、管理员 Cookie、query token 和浏览器 Origin 均不能替代服务身份。
- `/health` 不查询 Kafka，返回 `{"status":"ok","service":"router"}`；`/ready` 在 broker 或 Topic 不可用时返回 `503`，且不暴露内部地址。
- `POST` 只接受 `application/json`，拒绝非空 `Content-Encoding`，通过有界 reader 支持固定上限内的 Content-Length 或 chunked body。

### 6.2 安全错误响应

错误统一为：

```json
{
  "error": {
    "code": "message_invalid",
    "message": "message is invalid"
  }
}
```

固定最小映射：

| 条件 | HTTP | code |
| --- | --- | --- |
| Bearer token 缺失或错误 | `401` | `internal_authentication_required` |
| Content-Type、JSON、Envelope 顶层或 Idempotency-Key 非法 | `400` | `message_invalid` |
| 请求正文超过 1 MiB | `413` | `message_too_large` |
| schema/type/source 尚不支持 | `422` | `message_type_unsupported` |
| Kafka 不可达、Topic 缺失、超时或有界缓冲拒绝 | `503` | `kafka_unavailable` |
| 未分类内部故障 | `500` | `internal_error` |

- 错误正文和日志不得包含 token、原始 payload、broker 地址、Topic 元数据、partition/offset、底层 Kafka 错误、服务器路径或调用栈。
- 只有已收到 Kafka 成功确认的请求返回 `202`；校验失败和明确生产失败不得返回成功。

## 7. Envelope 校验与路由契约

### 7.1 Router 的校验边界

Router 对收到的原始 body 执行一次有界解析，但不会重新序列化成功消息。顶层对象必须且只能包含：

```text
schema_version
message_id
type
source
timestamp
payload
```

固定规则：

- JSON 必须是唯一顶层 object、有效 UTF-8、无重复顶层 key、无未知/缺失顶层字段且无尾随 token。
- `schema_version` 固定为整数 `1`；`message_id` 固定为 32 位小写十六进制。
- `type` 当前只接受 `metrics`，`source` 当前只接受 `redis`；其他组合返回 `message_type_unsupported` 且不写 Kafka。
- `timestamp` 必须是有效 UTC RFC3339Nano；`payload` 必须是非 null JSON object。
- `Idempotency-Key` 必须存在、只出现一次且逐字等于 `message_id`。
- Router 不重复执行 Phase 6 的指标 family、sample、label、kind 或业务数值校验；这些字段保持透明，Phase 8 负责第二次处理和异常过滤。

### 7.2 路由表

Router 使用显式只读路由表：

```text
metrics → gopulse-observability-v1
```

- 不能从请求 query、Header、payload、source 或客户端提供的 Topic 名选择任意 Topic。
- 代码结构允许后续加入 `logs`、`events` 映射，但 Phase 7 不预建相应 schema 或 Topic。
- 单 Topic 是首版交接契约；长期 Topic 拆分、partition key 和保留策略在真实负载明确后另行规划。

## 8. Kafka 与 Record 契约

### 8.1 本地 Kafka

- `deploy/compose.yaml` 使用固定 `apache/kafka:4.3.1` JVM 镜像和单节点 KRaft combined mode，不引入 ZooKeeper。
- 设置独立 controller、internal 和 external listener；容器内 Topic 初始化使用 internal listener，WSL2 主机上的 Router 使用发布到 `127.0.0.1:${KAFKA_PORT}` 的 external listener。
- 数据保存在独立 `kafka_data` volume；所有容器、network、volume 和端口继续受 Compose project 归属约束。
- Topic 由受控初始化步骤显式以 `--if-not-exists` 创建，Phase 7 本地参数固定为 1 partition、replication factor 1；Router 客户端禁止自动创建 Topic。
- 不自定义长期 retention、compaction 或容量策略，使用本阶段固定镜像默认值并在 README 标明其仅用于开发/验收。

### 8.2 Kafka record

成功请求写入：

| Record 部分 | 固定值 |
| --- | --- |
| Topic | `gopulse-observability-v1` |
| Key | Envelope `message_id` 的 UTF-8 bytes |
| Value | Router 收到的原始 HTTP body bytes |
| 业务时间 | 以 Envelope `timestamp` 为权威，不由 Router 重写 |

- Router 不对 JSON 做 marshal、字段排序、默认值填充、时间替换、payload 清洗或压缩；Consumer 必须能够逐 byte 对比 record value 与原始 HTTP body。
- Kafka record metadata、partition 和 offset 是传输事实，不写回 Envelope，也不通过 HTTP 成功响应暴露给 Monitor。
- key 只提供稳定消息标识和下游相关性，不等价于 Kafka 去重；同一 `message_id` 在应用重试下仍可能出现多条 record。

## 9. Producer 可靠性与失败语义

- 使用 franz-go 同步生产确认，保留客户端默认幂等生产，固定 `acks=all`，不配置 transactional ID。
- `ROUTER_KAFKA_PRODUCE_TIMEOUT` 同时约束 HTTP 等待与 record delivery；配置客户端允许有界取消，避免 broker 故障使请求或关闭无限阻塞。
- 生产缓冲固定受 `ROUTER_KAFKA_MAX_BUFFERED_RECORDS=256` 和 `ROUTER_KAFKA_MAX_BUFFERED_BYTES=8388608` 限制，不建立额外 goroutine 队列或磁盘 spool。
- 客户端只在单次有界生产窗口内做协议级重试；Router 不在 HTTP handler 外另起无限重试，也不在返回失败后后台重新生产。
- 请求在生产已发出后超时可能处于“Kafka 已写入但调用方未得到确认”的不确定状态；Router 返回失败或连接关闭，不伪造 `202`。Phase 7 不建设 receipt 查询或持久去重，`message_id` 必须原样保留供 Phase 8 识别潜在重复。
- Kafka 不可用不会使 Router `/health` 失败，不会使 Backend readiness 失败，也不会阻止 Monitor 后续 scrape；恢复后只传输新产生的消息，不补发 Phase 6 已丢弃的历史发布。
- Router 优雅关闭先停止接收新请求，再在 `ROUTER_SHUTDOWN_TIMEOUT` 内等待在途 handler 和 producer 结束，最后关闭客户端；超时后以有限错误退出，不无限等待。

## 10. 配置与安全边界

Phase 7 最小配置：

```text
KAFKA_PORT=9092
ROUTER_HTTP_HOST=127.0.0.1
ROUTER_HTTP_PORT=9091
ROUTER_API_TOKEN=<minimum-32-bytes>
ROUTER_REQUEST_TIMEOUT=5s
ROUTER_SHUTDOWN_TIMEOUT=10s
ROUTER_MAX_MESSAGE_BYTES=1048576
ROUTER_KAFKA_BROKERS=127.0.0.1:9092
ROUTER_KAFKA_TOPIC=gopulse-observability-v1
ROUTER_KAFKA_PRODUCE_TIMEOUT=3s
ROUTER_KAFKA_MAX_BUFFERED_RECORDS=256
ROUTER_KAFKA_MAX_BUFFERED_BYTES=8388608
MONITOR_ROUTER_URL=http://127.0.0.1:9091
MONITOR_ROUTER_TOKEN=<same-as-ROUTER_API_TOKEN>
```

- Router host 必须是 IP，日常默认 loopback；port 范围 `1..65535`，且不得与仓库其他已分配端口冲突。
- request timeout 允许 `1s..30s`；produce timeout 允许 `100ms..10s` 且必须小于 request timeout；shutdown timeout 允许 `1s..60s`。
- message bytes 允许 `1 KiB..1 MiB`；buffered records 允许 `1..1024`；buffered bytes 允许 `1 MiB..64 MiB` 且不小于 message bytes。
- brokers 是非空、去重、有限列表，只接受合法 `host:port`；Topic 固定符合 Kafka 安全命名且不得由请求覆盖。
- `.env.example` 只提供开发用示例 token；真实环境必须更换。日志不得输出任何 token、整个配置环境、原始 body 或内部 URL。
- 普通用户和管理员浏览器即使携带现有登录 Cookie，也必须被 Router 拒绝；Kafka 端口只绑定 loopback，后续部署通过内部网络访问。

## 11. Bash 生命周期、Consumer 与隔离验收

- `scripts/dev.sh` 将 Kafka 纳入已有 Compose 基础设施，等待 broker 健康、幂等创建 Topic、构建/启动 Router并确认鉴权 readiness，再启动配置为正式 Router URL 的 Monitor。
- Router PID 记录继续包含 cwd、绝对 executable、start ticks 和 command marker；启动前拒绝占用端口或不匹配的遗留进程。
- `scripts/verify.sh` 保持只读，验证 Kafka container/volume/project 归属、Topic 存在、Router PID/health/readiness、Monitor/Exporter 状态和版本，不消费或提交业务 offset。
- `scripts/down.sh` 先有界停止 Monitor/Exporter，再按归属停止 Router，最后通过既有 Compose project 清理基础设施；不得单独按名称删除未知 Kafka container/volume。
- 新增 `scripts/verify-router.sh`：`--self-test` 只执行无 Docker 的 token、PID、project、container、volume、port、Topic 和清理目标负向验证；默认模式使用随机隔离资源执行完整真实链路。
- 验证 Consumer 使用与生产端相同的 franz-go module、唯一测试身份和显式 offset 范围读取，不加入常驻日常进程；它输出有限机器可读证据，不打印 token 或 broker 凭据。
- 默认验收创建独立 Compose project、Kafka/Redis/Backend/Monitor/Exporter/Router 端口、数据库、插件根、进程目录和临时 Consumer 身份；成功、失败、超时和中断路径都只清理强归属资源。

## 12. 跨批次依赖与摘要

| 批次 | 纵向交付 | 关键输入 | 关键输出 |
| --- | --- | --- | --- |
| Phase-07-01 | Router 与 Kafka 传输闭环 | Phase 6 Envelope/Publisher、真实 Redis Exporter | Router、单 Topic、原始消息 record、验证 Consumer、生命周期与 CI |
| Phase-07-02 | 集成验收与阶段收口 | 已合入的 `1.4.1` 纵向能力 | 故障/恢复、权限与业务隔离证据，`1.4.2` 阶段交接 |

- 07-01 必须自身可运行、可验证，不能把 Kafka Consumer 完整性或 Monitor 接线推迟到收口批次。
- 07-02 不重新设计接口，只在最终构建和干净隔离资源上验证跨组件事实；只允许修复已复现的阻断问题。
- Phase 8 只能依赖已记录的 Topic、record key/value、Envelope、故障和重复语义，不能依赖验收脚本的临时 Consumer 实现细节。

## 13. 测试策略与固定验收矩阵

### 13.1 执行效率与停止规则

- 每批首次探索限于直接相关的 Router、Monitor Publisher、Compose、Bash 生命周期和验收代码；不进行一般依赖审计、全仓覆盖率活动或 Kafka 源码阅读。
- 单元测试只保护本阶段新接口和失败边界；一个代表性成功和一个代表性失败优先放在最低有效层，不跨 unit/integration/end-to-end 重复证明相同行为。
- 验证按 Router package → Monitor Publisher 必要回归 → 真实 Kafka/全链路逐级执行；只有实际失败或共享基础设施风险才扩大范围并记录原因。
- 已成功命令在代码、配置、依赖和环境未变化时不得因上下文压缩而重复；最终固定门禁通过且无阻断失败后立即记录、提交并停止。

### 13.2 批次验证边界

- Phase-07-01：Router format/unit/vet/race，Monitor Publisher 回归，Compose 渲染，脚本语法/自检，真实 Kafka record 以及真实 Monitor 纵向闭环。
- Phase-07-02：最终构建的完整传输矩阵、Kafka 故障恢复、内部访问负向、社交业务代表回归、资源清理、版本/分支治理和远程门禁。若 07-01 通过后相关代码/配置未改变，可引用已记录的 package 结果，不本地重复无影响检查；远程 CI 按仓库规则正常执行。
- Phase-07-03：Router Envelope/Producer 定向 unit/race、无 Docker Router self-test、单次隔离 Kafka 非写入矩阵、Kafka outage/recovery、直接受影响的 Monitor/Exporter/Backend/Frontend 与治理门禁。只有具体共享基础设施回归才扩大验证。

### 13.3 阶段级端到端矩阵

1. **真实成功链路**：真实 Redis 数值变化经 Exporter、MetricsMonitor 和 Router 写入 Kafka；Consumer 找到相同 `message_id`，key 等于 ID，value 与 Router 接收 body 逐 byte 一致。
2. **目标故障消息**：停止 Redis 后，Phase 6 合法 `target_unavailable/up0` Envelope 仍完整通过 Router/Kafka；Router 不把它误判为自身失败。
3. **鉴权与输入拒绝**：无 token、错误 token、用户/admin Cookie、错误 Content-Type、压缩、超限、重复 key、尾随 JSON、Idempotency-Key 不匹配和未支持类型均不新增目标 Kafka record。
4. **Kafka 故障与恢复**：停止 broker 后 Router `/health=200`、`/ready=503`、发布非 `202`；Monitor 后续采集继续，Backend 社交 API 可用。恢复原 broker/Topic 后不重启 Router/Monitor即可传输新消息。
5. **职责隔离**：Monitor module 无 Kafka SDK/Topic，Router value 无字段变化，RabbitMQ 业务异步链路无 Kafka 依赖，Router 无清洗/存储代码。
6. **资源隔离**：正常、失败和中断清理均不误杀日常进程、不删除非归属 container/network/volume/plugin root，不占用遗留端口。

## 14. CI 与固定完成门槛

CI 增加独立 `Router` job：Router format/test/vet/race 和 `scripts/verify-router.sh`；`Scripts and Compose` 加入 Router 文件的 LF、脚本语法/自检、Kafka loopback 与 Topic 配置检查。现有 Monitor、Exporter、Backend、Frontend 和 Integration 职责不被 Router job 替代。

阶段最终门槛至少包括：

```bash
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd exporters/redis && go test -count=1 ./...)
(cd backend && go test -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.4.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-router.sh --self-test
scripts/verify-router.sh
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

`verify-router.sh` 是 Phase 7 传输闭环、Kafka 故障恢复和资源安全的唯一主验收入口；`verify-monitor.sh` 保护 Phase 6 插件/采集/Publisher 契约，`verify-business.sh` 证明可观测故障没有破坏身份、社交、通知、搜索和日志必要能力。不得用手工 curl、Kafka CLI 截图或源码阅读替代真实闭环。

完整验收只在 WSL2 Linux filesystem 和可确认归属的隔离资源执行。环境缺失时不得标记完成，也不得以 mock broker、静态 JSON 或直接 Kafka produce 替代真实 Monitor 链路。

## 15. 实施记录规则

每批在提交前创建：

```text
dev/logs/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md
dev/logs/Phase-07/Phase-07-02-集成验收与阶段收口.md
```

每份记录必须包含：

- 实际完成的行为和实际变更文件。
- 实际执行的验证命令、结果和必要的有限输出摘要。
- 相对方案的偏差、实施中真实失败及其最小修复。
- 未完成限制、后续事项、Pull Request、远程检查和合入状态。

不得把计划命令写成已通过，不得在未推送、未观察远程 checks 或未合入时把阶段标记为完成。

## 16. Phase 7 验收、完成与 Phase 8 交接

### 16.1 阶段验收标准

- MetricsMonitor 仍只依赖 HTTP Publisher，`monitor/go.mod` 不含 Kafka SDK，消息链路固定经过 Router。
- Router 只接受合法服务令牌；用户/admin Cookie、浏览器请求和未经授权客户端不能发布消息或读取 readiness。
- Router 对 Envelope v1 顶层、消息 ID、类型、source、timestamp、payload object 和 Idempotency-Key 执行固定有界校验，非法输入不写 Kafka。
- `metrics` 固定路由到显式创建的 `gopulse-observability-v1`，Router 不接受客户端指定 Topic，不启用自动建 Topic。
- Kafka Consumer 读取的 key 等于 `message_id`，value 与 Router 接收原始 JSON 完全一致；Router 不改写业务字段。
- Router 只在 Kafka 确认后返回 `202`；Kafka 故障、超时和缓冲耗尽有界失败且不泄漏内部信息。
- Kafka 停止时 Router 进程存活、Monitor 继续采集、普通用户社交闭环和管理员权限边界不变；Kafka 恢复后新消息无需重启即可继续传输。
- Kafka 只承载可观测消息，RabbitMQ 业务异步职责、Phase 0～6 必要能力、日常生命周期和资源归属无回归。
- 三批实施记录真实完整；Phase-07-03 的固定本地/远程门禁通过，根与 Frontend 版本均为 `1.4.3`。
- `schema_version` 只接受 JSON integer `1`；字符串或小数表示不能进入 Producer/Kafka。
- Producer 真实使用 franz-go 的 records/bytes 上限并在缓冲耗尽时立即拒绝；请求取消不全局中止其他 record，readiness 不与 publish 共享串行锁，shutdown 保持有界。
- Router 无 Docker self-test 覆盖 token、PID、project、container、volume、port、Topic 和清理目标；真实拒绝矩阵全部证明 Kafka offset 不增长，五个随机端口两两唯一。

### 16.2 完成与停止条件

只有第 16.1 节全部满足、Phase-07-03 已合入主远程 `main`、远程门禁成功且三份实施记录与真实提交一致，Phase 7 Review 整改才完成。任一真实 Monitor 输入、Consumer 完整性、原始字节不变、内部身份、Kafka 故障恢复、社交业务隔离、资源清理或远程状态证据缺失时，不得标记完成。

阶段验收通过后立即停止。多 Topic、其他消息类型、持久去重、重放、Schema Registry、SASL/TLS、多 broker、Marshaller、存储和长期 Kafka 治理全部作为后续事项，不继续占用 Phase 7。

### 16.3 Phase 8 交接

- Kafka bootstrap 配置方式和显式 Topic `gopulse-observability-v1`。
- record key 为 `message_id`、value 为未经 Router 改写的 Envelope v1 原始 JSON。
- 当前合法类型为 `metrics`、source 为 `redis`，payload 继续遵循 Phase 6 metrics 契约。
- 只有 broker 确认才产生 Router `202`；调用超时存在不确定写入，Consumer 必须按 `message_id` 识别潜在重复。
- Kafka/Topic 恢复、Router readiness、受控网络和安全日志边界。
- 验证 Consumer 的真实读取证据；Phase 8 应建立自己的正式 consumer group、offset、异常消息和 VictoriaMetrics 写入契约，不复用临时验收身份。
