# Phase 6-02：MetricsMonitor 周期采集与标准消息闭环实施方案

> 执行序号：2 / 3
>
> 前置批次：Phase-06-01 已完成并通过验收
>
> 总方案来源：[Phase-06-总实施方案.md](Phase-06-总实施方案.md)

## 1. 批次目标

在 Phase-06-01 已建立管理员到 Redis Exporter 运行状态的完整所有权之上，纵向交付“真实 Redis → Redis Exporter → MetricsMonitor 周期 HTTP Pull → Prometheus 解析与基础校验 → GoPulse metrics Envelope → HTTP 捕获端”闭环。

本批把已安装 `metrics-exporter` 的运行状态转换为可调度采集目标，固定成功、目标不可用、超时和畸形数据的处理语义，并通过可选 HTTP Publisher 为 Phase 7 交付稳定线上契约。本批不实现 Message Router 或 Kafka，也不把指标转换为 VictoriaMetrics 最终格式。

## 2. 前置条件

- Phase-06-01 已从 `develop/1.3.1` 完成并合入主远程，根版本为 `1.3.1`，实施记录、本地验收和远程门禁齐全。
- 管理员可通过 Backend 安装/启停/更新 Redis Exporter，Plugin Manager 是进程与 desired state 的唯一所有者。
- 真实 Redis Exporter 安装包、Manifest v1、`current` release、运行环境、`/health` 和 `/metrics` 端点已通过 Phase-06-01 验收。
- 已 fetch 主远程，从包含 Phase-06-01 的最新 `main` 创建 `develop/1.3.2`，没有沿用前一批分支。
- WSL2 Linux filesystem 可启动隔离 Redis、Backend、Monitor、Exporter 和本地 HTTP 捕获端，并且已保存日常资源快照。

## 3. 实施范围

### 3.1 采集目标与状态联动

- 在 Monitor 内建立 MetricsMonitor，一个已安装 `metrics-exporter` 自动形成一个 target，Phase 6 固定 target ID 为 `redis-exporter-local`。
- target 的 source、plugin ID/version、health path 和 metrics path 来自已验证 Manifest，host/port 来自 Monitor 受信运行配置，不接收 API 或 archive 中的任意 URL。
- Plugin Manager 安装或 start 在 health 通过后启用 target 并立即采集；stop 先禁用 target 与取消在途 scrape，再停止进程；update 成功后替换 plugin version 并重新立即采集。
- Plugin Manager 与 MetricsMonitor 通过明确的内部状态事件/接口协作，不让 MetricsMonitor 读取 Registry 文件、操作 PID 或修改 desired state。
- Monitor 重启时先恢复插件，再为 health 通过的 running 插件启动 target；恢复失败的插件不启动采集。

### 3.2 周期调度与并发边界

- 默认 `MONITOR_SCRAPE_INTERVAL=15s`、`MONITOR_SCRAPE_TIMEOUT=3s`，配置 loader 强制时限为有界正值且 timeout 严格小于 interval。
- target 启用后立即执行一次 scrape，之后以可注入 clock/ticker 周期触发；不将首次采集延迟到一个完整间隔之后。
- 每个 target 最多一个在途 scrape；新 tick 遇到前一次未完成时跳过本次而不创建额外 goroutine，记录有限 `scrape_in_progress`。
- 每次 scrape 使用独立 context deadline、专用 HTTP client 和禁止无界跳转/自动解压的运输配置；不使用默认无界 client。
- stop/update/Monitor shutdown 取消相关 target 的在途请求并有界等待；取消不得在旧版已停止后又发布延迟的旧版消息。

### 3.3 Prometheus HTTP 和解析契约

- 只发送 `GET`，不发送 query、body、Cookie 或用户 header；目标 URL 只能是由受信配置派生的回环 HTTP URL。
- 响应体上限固定默认 1 MiB，超限立即失败；不接受 gzip/br 自动解压、multipart、HTML、JSON 或其他格式作为 metrics。
- 使用 Prometheus 官方 text parser，不自行用行分割/正则解析公共契约。
- 只接受 Phase 5 固定 family、gauge/counter type、有限数值与固定 `mode`/数字 `db` 标签；上限为 128 个 family、1024 个 sample、每 sample 16 个 label、128 bytes 名称和 256 bytes label value。
- 拒绝缺失/重复 family、重复 name+规范化 labels、未知 family/type/label、`NaN`、`Inf`、负 counter、文本内时间戳和部分成功数据。
- `200` 只在全部 Phase 5 成功契约完整且 `gopulse_redis_up=1` 时生成 `success`；`503` 只在正文为唯一合法 `gopulse_redis_up=0` 时生成 `target_unavailable`。
- 超时、网络、超限、解析、契约或其他 HTTP 状态失败都不产生 Envelope，不回放上一次 payload；只更新安全 `last_error`。

### 3.4 结构化 samples 与 Envelope v1

- 将合法 metric family 展平为 `name/kind/labels/value` samples，在封装前按 name 和规范化 label key/value 稳定排序。
- Envelope 固定包含 `schema_version=1`、安全随机 32 位小写十六进制 `message_id`、`type=metrics`、`source=redis`、Monitor 生成的 UTC RFC3339Nano `timestamp` 和 `payload`。
- payload 固定包含 `plugin_id`、`plugin_version`、`target_id`、`scrape_status` 和 `samples`；`target_unavailable` 只包含唯一 `up=0` sample。
- `message_id` 生成失败导致本次消息失败，不使用时间、计数器或弱随机回退。
- timestamp 是完成响应读取和校验的时间，不使用 Exporter 自带时间或开始请求前时间。
- 不将 Prometheus HELP/原文、HTTP body、Redis 凭据/地址、绝对路径、PID 或未清洗错误放入 Envelope。

### 3.5 HTTP Publisher 与状态更新

- 建立 `Publish(context.Context, Envelope) error` 内部接口，MetricsMonitor 不导入任何 Kafka SDK，不选择 Topic，不执行存储格式转换。
- 当 `MONITOR_ROUTER_URL` 为空时，可运行 publisher 只接收 Envelope 并更新内存中最近消息时间，不保存 payload 或写入文件；Monitor 仍可就绪。
- URL 非空时使用专用有界 HTTP client，POST JSON 到 `<MONITOR_ROUTER_URL>/internal/v1/messages`，携带 Bearer token 和 `Idempotency-Key=message_id`，仅 `202` 成功。
- `success` 在完成严格校验后立即更新 `last_scrape_at` 和 `last_success_at`；`target_unavailable` 是有效采集结果，只更新 `last_scrape_at`，`last_success_at` 保留最近 `success` 时间。
- Publisher 不自动重试且不落盘队列。发布失败记录 `publish_failed`，但不撤销已经成立的采集时间/成功事实；下一采集周期继续。后续发布成功只清除已恢复的发布错误，不改写采集时间语义。
- Backend 插件状态 API 代理新采集字段与有限错误，不返回最近 Envelope 正文，不将状态 API 变成临时指标查询端点。

### 3.6 验收捕获端

- 扩展 `scripts/verify-monitor.sh` 的隔离编排，启动一个本次专用、回环监听、带随机 token 的最小 HTTP 捕获端。
- 捕获端只实现 `POST /internal/v1/messages`，校验 method、content type、Bearer token、Idempotency-Key、响应大小和严格 Envelope Schema，成功返回 `202`。
- 捕获端是验收 fixture，不放入日常 `dev.sh`，不伪装 Message Router，不引入 Kafka，不在生产组件中存储历史消息。

## 4. 实施边界与非目标

- 不修改 Phase-06-01 已稳定的管理员角色、安装包、原子布局、Registry、进程归属或更新回滚语义，除非本批真实联动失败证明其阻断采集。
- 不增加采集目标 CRUD、多插件并发、服务发现、动态 scrape 参数 API 或运行期热加载。
- 不计算 rate/ratio、不聚合/降采样、不添加自定义标签、不保存历史或上一次成功 payload。
- 不实现 Router 进程、Kafka Producer/Consumer/Topic、Marshaller、VictoriaMetrics 或任何指标查询 API。
- 不将捕获端、内存 publisher 或状态字段描述为持久消息可靠性；该能力由后续 Router/Kafka 和工程化阶段完成。
- 不实现 LogMonitor/EventMonitor，不提前产生插件启停/采集失败 events。

## 5. 预计文件与交付物

```text
monitor/internal/config/**
monitor/internal/metrics/collector/**
monitor/internal/metrics/envelope/**
monitor/internal/metrics/publisher/**
monitor/internal/plugin/**
monitor/cmd/monitor/**
monitor/go.mod
monitor/go.sum
monitor/README.md
backend/internal/monitor/**
backend/internal/http/**
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-monitor.sh
scripts/ci/**
.github/workflows/quality-gates.yml
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-06/Phase-06-02-MetricsMonitor周期采集与标准消息闭环.md
```

预计文件只表示允许边界；如已有 Phase-06-01 模块边界可直接容纳本批功能，不为追求目录形式而人为重构。实际未修改文件不写入实施记录。

## 6. 详细实施步骤

1. 核对 Phase-06-01 实施记录、Plugin Manager 状态事实、Redis Exporter Manifest 和 Phase 5 最终 Prometheus family 契约。
2. 在 Monitor 建立 target 模型、Plugin Manager 状态订阅/查询边界，固定 target ID 和启用/禁用/版本切换顺序。
3. 实现立即+周期调度、每 target 单在途限制、超时、取消与 Monitor 关闭等待，用 fake clock/client 证明不重叠。
4. 实现专用 HTTP client、回环目标 URL 派生、跳转/压缩禁止、响应大小上限和可控错误分类。
5. 接入 Prometheus 官方 parser，实现 Phase 5 固定 family/type/label/value 契约、重复检查和 samples 稳定排序。
6. 分别实现 `200/up1 → success`、严格 `503/up0 → target_unavailable` 与其他失败不生成消息的状态机。
7. 实现 Envelope v1 类型、安全 message ID、Monitor UTC timestamp、payload/samples 严格 JSON 序列化和敏感信息负面断言。
8. 实现 Publisher 接口、Router URL 为空时的无历史运行模式，以及带 Bearer/Idempotency-Key/超时/严格 `202` 的 HTTP Publisher。
9. 将采集/发布结果接入插件公共状态，保持 payload 不持久化，并经 Backend 安全代理时间和有限错误。
10. 扩展 `verify-monitor.sh`，启动独立捕获端，用真实 Redis 变化核对 Exporter、Envelope 和捕获 body，再验证 Redis 故障/恢复、畸形数据与 Publisher 故障。
11. 更新 Monitor README、根 README、配置示例、CI Monitor job 和 Bash 验证，不在日常栈中伪造 Router。
12. 将根与 Frontend 版本更新为 `1.3.2`，创建同名实施记录，只写入真实命令、结果、偏差和限制。

## 7. 风险与控制

- **定时器重叠拖垮 Monitor**：每 target 固定单在途 guard，到期只跳过，不创建无界 goroutine 或请求队列。
- **stop/update 后发布旧版数据**：先禁用并取消 target，等待在途任务退出，再停止/切换进程；封装前再核对 target generation。
- **Prometheus 解析成为无界输入**：HTTP bytes、family/sample/label 数量、文本长度、type 和数值都有固定上限，拒绝压缩响应。
- **目标故障指标被丢弃**：对 Phase 5 严格 `503/up0` 保留一条 `target_unavailable` Envelope，同时拒绝任何部分指标或畸形 `503`。
- **陈旧数据被当作当前采集**：不缓存/重放 payload，任一无法严格校验的当次响应都不生成消息。
- **Publisher 反压或重试雪崩**：请求有界、无自动重试/磁盘队列，失败记录后让下一 scrape 正常运行。
- **Monitor 越界承担 Marshaller**：只输出基础 samples，不计算派生指标、聚合、清洗或存储特定行协议。
- **验收捕获端被当作 Router**：fixture 只存在于隔离验收，日常栈不启动它，README 明确正式 Router/Kafka 属于 Phase 7。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败修复后只重跑受影响的命令或场景：

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-monitor.sh --self-test
scripts/verify-monitor.sh
scripts/verify-exporter.sh
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.3.2 --base-ref upstream/main
git diff --check
```

Monitor 单元测试固定代表性成功和失败：一个 `200/up1`、一个严格 `503/up0`、一个畸形/超限拒绝，以及一个无重叠调度场景。标签/数值更多排列只在真实 parser 失败或公共契约风险需要时增加。

`scripts/verify-monitor.sh` 是本批真实 Redis、Exporter、Monitor 和 HTTP 捕获端的主证据；不再新建重复的全栈脚本。若本批未修改共享业务资源或生命周期基础，不在此批重跑完整 `verify-business.sh`；该回归留给 Phase-06-03。

## 9. 验收标准

- 已安装/running Redis Exporter 自动形成唯一 target，install/start/update 后立即采集，stop 后不再发起采集或发布延迟旧消息。
- 默认 15s 周期和 3s 超时有效，单 target 没有重叠 scrape、无界 goroutine 或无界待处理队列。
- 真实 Redis 数值变化可在 Exporter `/metrics` 和捕获的 `success` Envelope samples 中对应，证明未使用静态 fixture。
- `200/up1` 必须满足完整 Phase 5 family/type/label 契约才生成 `success`；严格 `503/up0` 生成唯一 sample 的 `target_unavailable`。
- 超时、网络错误、超限响应、畸形/重复/非有限 Prometheus 不生成 Envelope，不重放上次数据，且只记录安全有限错误。
- Envelope v1 的 schema version、message ID、type、source、Monitor timestamp、payload 和稳定 samples 序列化符合总方案，不含敏感或原始数据。
- HTTP 捕获端收到带 Bearer token、Idempotency-Key 和完整 JSON 的请求，且只有 `202` 被 Monitor 认为成功。
- Router URL 为空时 Monitor 可正常采集；Publisher 不可用时下一周期继续，不产生磁盘队列或无界重试。
- Backend 状态可读取最近采集/成功时间和安全错误，但不暴露 Envelope 正文、内部 URL/token 或原始错误。
- Monitor 不导入 Kafka SDK，不包含 Topic、Marshaller、VictoriaMetrics 或历史指标代码。
- 第 8 节固定验证和远程门禁通过，版本元数据为 `1.3.2`，实施记录真实完整。

## 10. 明确完成条件

只有插件状态联动、立即和周期调度、单在途限制、Prometheus 严格解析、成功/目标故障语义、Envelope v1、HTTP Publisher、捕获端和真实 Redis 恢复全部通过，且没有阻断验收的失败，才可标记 Phase-06-02 完成。只有 parser 单元测试、手工构造 JSON 或静态指标文本不足以完成本批。

## 11. 下一批交接

- 已经真实 Redis Exporter 验证的立即/周期采集、target 状态联动、超时和非重叠调度。
- Phase 5 固定 Prometheus family 的严格 parser/校验器和稳定结构化 samples。
- Envelope v1 的固定类型、JSON Schema 行为、成功/目标故障语义与脱敏边界。
- 可选 HTTP Publisher、`POST /internal/v1/messages`、Bearer token、Idempotency-Key、`202` 契约和验收捕获端。
- Phase-06-03 只需在同一最终构建上执行管理与采集跨批闭环、Phase 0～5 必要回归、资源安全、远程门禁和 Phase 7 交接，不得扩大功能范围。
