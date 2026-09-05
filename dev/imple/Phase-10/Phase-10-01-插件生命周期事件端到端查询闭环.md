# Phase-10-01：插件生命周期事件端到端查询闭环实施方案

> 权威目标版本与开发分支以 `Phase-10-总实施方案.md` 第 3.2 节为准：本批对应 `1.7.1` / `develop/1.7.1`。
>
> 当前状态：本地实施与固定门禁已通过，待提交、Pull Request、远程门禁与合入。

## 1. 批次目标

以 Redis Exporter 插件 install/start/stop/update 的真实成功转换为第一条产品纵向切片，一次性交付从事件源到管理员查询的可运行闭环：

```text
Backend admin 插件操作
  → Monitor Plugin Manager 真实终态转换
  → 进程内 EventMonitor 有界非阻塞记录
  → Router → Kafka → Marshaller events target
  → gopulse-events-v1-* / gopulse-events-v1-read
  → Backend admin Events API
```

本批完成后，管理员必须能用时间范围、event name 或 plugin ID 精确查到刚完成的插件操作；未登录和普通用户必须在 Elasticsearch 调用前分别得到 `401` 与 `403`。本批不是模块骨架批次，不得把 Router/Kafka、Marshaller/Events Store、Elasticsearch 或 Backend 查询中任一必需环节留给 Phase-10-02。

本批只接入成功生命周期事件：`exporter_plugin_installed`、`exporter_plugin_started`、`exporter_plugin_stopped` 和 `exporter_plugin_updated`。插件终态失败、异常退出、Metrics 采集失败/恢复、target unavailable/recovered、episode 去抖与完整故障矩阵属于 Phase-10-02。

## 2. 前置条件

- 开工前 fetch 主远程，确认 Phase 9 最终能力已合入最新 `main`，根与 Frontend 版本为 `1.6.4`；若基线不一致，先更新 Phase 10 总/拆分方案。
- 从最新 `upstream/main` 创建 `develop/1.7.1`，不沿用 `update`、Phase 9 分支或已完成分支。
- 核对 Plugin Manager install/start/stop/update 状态提交点、Monitor Router Publisher、Router Envelope/Topic、Marshaller typed target/ownership 状态机、Logs Elasticsearch 写后合同验证和 Backend logquery/admin 授权。
- 在 WSL2 Linux filesystem 与唯一 Docker daemon 中实施；保存 Git、日常进程/端口、Compose project/container/network/volume、Kafka group/offset、插件根和 Elasticsearch 索引快照。
- 只阅读 Monitor events/plugin/config/main、Router envelope/routing、Marshaller envelope/processor/ES、Backend eventquery 接线与直接测试；不将本批扩展为全仓审计。

## 3. 实施范围

### 3.1 EventMonitor 有界记录器

- 在 `monitor/internal/events` 建立独立 Event v1 contract、canonical encoder、message ID 生成、有界队列和单发送 worker，不把 Events 混入 `monitor/internal/logs` 或 metrics Envelope package。
- 向真实事件源暴露窄 `Record(Event) bool` 或等价接口；返回值只表示远程副本是否被本地队列接管，不是插件操作结果。
- Record 严格验证固定 event name/source/severity/message 和按事件类型限定的 metadata，生成 32 位小写十六进制 `message_id`，并在不等待网络的前提下入队。
- 事件队列默认 `256`、允许 `1..4096`；单条最大 `16 KiB`；queue full 立即拒绝当前远程副本。
- 发送 worker 调用现有 Monitor Router Publisher；只有 Router `202` 移除队首，暂时失败使用有界指数退避与可测 jitter，确定性 `4xx` 有限记录后跳过并继续。
- 状态日志使用 stdout-only logger 并对 queue full/available、transport unavailable/recovered 执行状态转换节流；不反向产生 Events，不输出原事件、URL、token 或底层错误。
- Record/Close 使用明确线性化边界；Close 后拒绝新记录，对此前已接受记录在默认 `5s`、最大 `30s` 内 drain，超时不卡死 Monitor shutdown。

### 3.2 成功插件状态转换接线

- 将 EventRecorder 通过构造参数或窄 attach 接口注入 Plugin Manager，默认 nop recorder 保持既有单元测试和无 Events 配置的局部使用可控。
- `Install` 只在 package 安装、registry/current 持久化、插件启动、runtime/state 提交和 MetricsMonitor enable 均完成后记录一个 `exporter_plugin_installed`；不再重复记录 started。
- `Start` 只在非 running 状态真实转为 running 后记录 `exporter_plugin_started`；已 running 且 ownership 有效的 no-op 不产生事件。
- `Stop` 只在管理员请求使 desired/observed state 最终为 stopped，且所有需要的持久化/进程收口完成后记录 `exporter_plugin_stopped`；Monitor shutdown 不复用此事件。
- `Update` 只在新版本已持久化并回到原 desired state 后记录一个 `exporter_plugin_updated`；metadata 同时包含新旧 SemVer，内部 stop/start 不对外拆成多个事件。
- EventMonitor enqueue 失败不得回滚已成功的插件状态，也不得把 Backend 成功响应改为错误。
- 无效 package、not found、conflict、in-progress、认证失败与操作中途失败本批不产生 Events；后者的终态安全事件在 Phase-10-02 接入。

### 3.3 Events payload/Envelope v1

- 实现总方案第 7 节的六个必需顶层 payload 字段和 strict metadata，本批只开放四个成功生命周期 event name 及对应字段组合。
- event name 与固定 severity/message 一对一；source 固定 `monitor`；timestamp 由真实终态提交时点获取并使用 UTC RFC3339Nano。
- metadata 必须根据事件要求包含 `plugin_id`、`plugin_version`、`operation`、`from_state`、`to_state`；update 额外必须包含 `previous_plugin_version`。
- 递归拒绝重复/未知字段、尾随 token、非 UTF-8、`null`、错误类型、未知词汇、无效 SemVer、控制字符和敏感哨兵。
- 生成 `schema_version=1,type=events,source=monitor` 的 Envelope，确保 Envelope/payload source 和 timestamp 一致，且同一队列项重试保持 ID 与内容不变。

### 3.4 Router Events 路由

- Router 顶层 validator 接受既有 metrics/logs 组合与新增 `events/monitor`；未知 events source、伪造 schema 和类型/source 错配继续返回 `422 message_type_unsupported`。
- `routing.Topic("events")` 或等价显式路由只返回 `gopulse-observability-v1`，不允许配置、请求或 payload 选择 Topic。
- 代表性测试必须证明合法 events 写入 Kafka 的 value 与 HTTP body 逐 byte 一致，key 等于 message ID，metrics/logs 路由不回归。

### 3.5 Marshaller events target 与 Events Store

- 公共 Envelope decoder 显式增加 `events/monitor`，不改变现有 metrics/logs 的 payload 分派与严格契约。
- 在 `marshaller/internal/events` 建立独立 validator/transformer，二次校验第 3.3 节，并将 `timestamp` 确定性映射为 `@timestamp`，其余只保留 canonical 白名单。
- 在 Elasticsearch package 中建立与 Logs Store 职责分离的 Events Store，可复用受限 HTTP transport/helper，但不共享 index/template/alias/mapping 常量或将两种文档放入一个泛化 map writer。
- Events Store 幂等 PUT `gopulse-events-v1-template`，按 Envelope UTC 日期写 `gopulse-events-v1-YYYY.MM.DD`，以 message ID 为 `_id`，并在成功响应后重验 strict mapping 与 `gopulse-events-v1-read`。
- Marshaller target registry 新增 `events/monitor`，保持正式 group、ownership/commit、永久错误跳过和暂时写入重试语义。
- Marshaller readiness 同时验证 VM、Logs Store 和 Events Store 的必要合同；`/health` 继续是纯 liveness。一个 ES 节点不需要重复端口或凭据。

### 3.6 Backend 管理员 Events 查询

- 新建 `backend/internal/eventquery` contract、options/cursor、Elasticsearch repository、service 和 handler；不复用帖子 Document、Logs DTO 或可变 index 输入。
- repository 只读 `gopulse-events-v1-read`，使用 PIT + `search_after`，排序固定 `@timestamp desc,_shard_doc desc`，对 index prefix、`_source`、sort 和响应大小再次校验。
- `GET /api/v1/observability/events` 必须位于 Authentication 和数据库实时 RequireAdmin 之后；未登录/普通用户测试使用 fake repository 断言调用次数为零。
- 首页默认 15 分钟、最大 24 小时、limit `1..100`；支持总方案固定 exact filters。过滤值必须来自当前已开放的成功生命周期词汇。
- cursor 使用独立 event-query domain key，固化实际时间窗/过滤/limit/PIT/last sort/过期时间；续页请求不得再携带其他参数。
- 响应仅返回 `timestamp,event_name,source,severity,message,metadata`；空 alias 返回空页，存储不可用返回 `503 events_unavailable`。
- 在 `apperror`、Backend server 组装与 route 中完成显式接线，不改变 Logs 查询路由、授权或 error code。

### 3.7 配置、生命周期、文档与门禁

- 在 Monitor/Marshaller/Backend config 与 `.env.example` 增加总方案第 12 节固定配置，严格校验容量、duration、template/prefix/alias 和不可打印值。
- `scripts/dev.sh` 将 Events target 纳入现有启动顺序；`scripts/down.sh` 执行 Monitor EventMonitor 有界 drain 后沿用既有强归属关闭。
- `scripts/verify.sh` 只读检查 Events API route、template/alias 与组件健康；不制造真实插件事件。
- 创建 `scripts/verify-events.sh`，同时提供安全 `--self-test` 和真实闭环；自检覆盖不安全端口、token、index/alias、plugin root、PID/Compose/volume 与清理目标拒绝。
- 更新根/Monitor/Router/Marshaller README、HTTP/Events 契约、准确 best-effort/at-least-once 边界与 Phase 11 使用注意；不增加 Frontend 页面。
- CI 增加或扩展 Events pipeline job，运行直接模块门禁、Events self-test/真实闭环、版本和分支治理；不弱化现有 Metrics/Logs jobs。
- 完成同名实施记录，同步根与 Frontend 版本为 `1.7.1`。

## 4. 实施边界与非目标

- 不接入失败、unexpected exit、scrape/publish/target 状态转换；它们由 Phase-10-02 完成。
- 不向浏览器或其他进程暴露 Event ingest HTTP API，不增加新 token。
- 不实现 Frontend Events 页、告警、聚合、全文检索、Kubernetes 事件、ILM、spool、Topic/group 拆分或 exactly-once。
- 不把业务 Outbox event 或 Logs 中的 `event_id` 字段搬运为可观测 Events。
- 不修改冻结 PowerShell，不执行原生 Windows 验收，不创建应用容器镜像。

## 5. 预计文件与交付物

```text
monitor/internal/events/**
monitor/internal/plugin/**
monitor/internal/config/**
monitor/cmd/monitor/**
monitor/README.md
router/internal/envelope/**
router/internal/routing/**
router/README.md
marshaller/internal/envelope/**
marshaller/internal/events/**
marshaller/internal/elasticsearch/**
marshaller/internal/config/**
marshaller/internal/httpserver/**
marshaller/cmd/marshaller/**
marshaller/README.md
backend/internal/eventquery/**
backend/internal/apperror/**
backend/internal/config/**
backend/internal/http/**
backend/cmd/server/**
README.md
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-events.sh
scripts/verify-monitor.sh
scripts/verify-router.sh
scripts/verify-marshaller.sh
scripts/ci/**
.github/workflows/quality-gates.yml
dev/logs/Phase-10/Phase-10-01-插件生命周期事件端到端查询闭环.md
dev/imple/Phase-10/Phase-10-总实施方案.md（仅状态/真实偏差）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是边界而非强制修改清单。新的 production 字段、endpoint、index 或配置必须有对应契约、测试、README 和验收；无实际需求的文件不制造修改。

## 6. 详细实施步骤

1. fetch 最新 `main`，确认 Phase 9 合入、版本、远程门禁和实施记录，创建 `develop/1.7.1` 并保存资源快照。
2. 建立 Events v1 contract 与 EventMonitor 有界 worker，先通过非阻塞、重试 ID、queue full、关闭线性化和敏感边界单元测试。
3. 将四个成功终态转换接入 Plugin Manager，验证 no-op、被拒绝请求、内部 update stop/start 与 Monitor shutdown 不发假事件。
4. 扩展 Router `events/monitor` 组合与固定 Topic 路由，保持原始 bytes 和既有类型回归。
5. 扩展 Marshaller 公共 Envelope/target registry，实现独立 events validator/transformer/Events Store 及 template/index/mapping/alias 写后验证。
6. 实现 Backend eventquery repository/service/handler、admin route、受限 filters、PIT/cursor、安全 DTO、空 alias 和 `events_unavailable`。
7. 先执行各受影响 package 的最小测试；仅对真实失败做有限修复，不增加无关边界测试。
8. 对齐 `.env.example`、Bash 生命周期、Events 验收/self-test、CI 和 README；在隔离环境运行真实插件事件闭环。
9. 在最终 diff 上完成第 8 节固定门禁，同步版本 `1.7.1`，如实编写同名实施记录。
10. 只暂存本批文件并提交；push、创建 PR，查询真实远程 checks 与合入状态。

## 7. 风险与控制

- **事件记录反向破坏操作**：事件只在主状态提交后 best-effort enqueue，recorder 失败不参与操作事务或 HTTP 响应。
- **update 产生重复事件**：只记录一个最终 updated，内部 stop/start 使用不产生公共事件的私有路径。
- **旧 ES 数据形成假通过**：使用随机 volume、当前 offset、窄 UTC 窗口和真实版本转换，不直接写 ES 充当源证据。
- **复用 Logs Store 造成索引混写**：可共享受限 transport/helper，但 template/prefix/alias/mapping/writer/repository 合同必须独立。
- **单 Topic 假设被破坏**：保持 Topic/group/partition，Events Store 失败时不越过 offset；不为通过验收手工提交。
- **admin 被误当作无限权限**：实时 role 授权之后仍执行参数词汇、固定 alias、响应 DTO 和敏感扫描。
- **资源清理误伤日常环境**：每个 PID/container/network/volume/plugin root 操作前验证本批强归属，unknown/mismatch 安全拒绝。
- **范围扩张**：本批只交付成功生命周期最小闭环；失败源、完整故障矩阵和阶段收口各归后续批次。

## 8. 固定验证命令与必要回归

最终 diff 稳定后执行：

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./internal/events ./internal/elasticsearch ./internal/consumer)
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/eventquery ./internal/http/...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.1 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-events.sh \
  scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh \
  scripts/verify-logs.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-events.sh --self-test
scripts/verify-events.sh
scripts/verify-monitor.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-logs.sh --self-test
git diff --check
```

- `verify-events.sh` 必须用真实 Backend admin 插件 API 产生至少 install/stop/start 代表转换，经完整链路由 Backend Events API 查询；update 使用真实可验证 package 且不以手工 ES 文档替代。
- 对现有 Metrics/Logs 仅执行因 Envelope/Router/Marshaller/ES 共享边界而必需的单元回归和 self-test；不在本批重跑 Phase 9 全部真实故障矩阵。
- 若命令因工具/环境缺失无法执行，实施记录必须明确标记未验证，不得用 mock 或 macOS 结果代替 WSL2 真实闭环。

## 9. 批次验收标准

- install/start/stop/update 的真实成功转换在最终状态提交后各产生符合契约的事件；no-op、被拒绝请求、update 内部 stop/start 和 Monitor shutdown 不产生重复/假事件。
- EventMonitor 的 Record 非阻塞、队列/重试/关闭有界、同一队列项 ID 不变；queue/publisher 失败不改变插件主结果。
- `events/monitor` 经 Router 原样写入固定 Kafka Topic，Marshaller 二次校验后写入 Events 独立 strict 索引，写后合同成立才提交 offset。
- 同 message ID 重放只有一个 Events 文档；永久坏 payload 不调用 writer 并安全继续；暂时 Events Store 错误不提交。
- Events template/index/read alias/mapping 与 Logs/帖子完全隔离，既有 Metrics 和 Logs 直接契约回归通过。
- 未登录 `401`、普通用户 `403`、admin `200`；前两者 repository 零调用，admin 只能使用固定过滤/cursor 并只获得安全 DTO。
- EventMonitor/Router/Marshaller/Elasticsearch 没有新增浏览器入口，stdout、Kafka/ES、Backend 响应、Frontend bundle 和验收制品不含敏感哨兵或内部字段。
- 生命周期、self-test、真实 Events 闭环、版本/分支治理与直接模块门禁通过，实施记录真实完整，根与 Frontend 版本均为 `1.7.1`。

## 10. 明确完成条件

只有第 9 节全部满足、Phase-10-01 Pull Request 已合入主远程 `main`、远程固定门禁成功，且 `dev/logs/Phase-10/Phase-10-01-插件生命周期事件端到端查询闭环.md` 与真实提交一致，本批才完成。缺少真实插件源、完整传输、Events 索引、admin 授权或敏感/资源安全中任一证据时不得标记完成。

完成后立即停止功能扩展。Phase-10-02 仅在本批已合入契约上接入故障/恢复事件和可靠性矩阵，不重写已验收的基础闭环。

## 11. Phase-10-02 交接

- 固定 Events v1 payload/Envelope 词汇、非阻塞有界 EventMonitor 和 stdout-only 运输状态。
- `events/monitor` 的 Router/Kafka/Marshaller target、正式 group offset/ownership、Events Store 写后合同验证和同 ID 幂等。
- Backend Events API 的实时 admin 授权、有限 filters、PIT 签名 cursor、安全 DTO 和索引隔离。
- 可复用的 `verify-events.sh`、强资源归属、真实插件 package 与日常生命周期。
