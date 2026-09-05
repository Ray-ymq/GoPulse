# Phase-10-01：插件生命周期事件端到端查询闭环开发记录

## 1. 状态与基线

- 执行日期：2026-09-05。
- 开发分支：`develop/1.7.1`。
- 开工前执行 `git fetch upstream --prune`，以 `upstream/main` 的 `e6557e6` 为基线；根 `VERSION`、`frontend/package.json` 与 `frontend/package-lock.json` 均为 `1.6.4`。
- 原工作区 `/home/ray/GoPulse` 在开工前存在用户自有的未提交日志目录移动和 `使用指南.md`，因此使用独立工作树 `/home/ray/GoPulse-phase-10-01` 从 `upstream/main` 创建本批分支，未暂存、修改或清理原工作区改动。
- 本地实现、固定门禁和真实隔离闭环已通过。实现提交为 `a1ee27e`，Pull Request #89 于 2026-09-05 通过远程 `Auto PR and Merge` 固定门禁（run `33952342892`）并合入主远程 `main`；合入提交为 `50cb497`。本批完成条件已满足。

## 2. 实际完成工作

### 2.1 Monitor Events 源端

- 新增 `monitor/internal/events`：
  - 固定四个成功生命周期事件的 Events payload v1 合同、metadata 组合和 canonical Envelope；
  - 生成 16-byte `crypto/rand` message ID，并编码为 32 位小写十六进制；
  - 拒绝未知/重复字段、尾随 JSON、错误类型、非 UTF-8、控制字符、自由文本、无效 SemVer、错误状态组合和敏感片段；
  - 实现容量 `1..4096` 的有界队列、单发送 worker、同一队列项稳定 ID/body、临时错误有界指数退避与 jitter、确定性 4xx 跳过、queue/transport 状态转换日志和有界关闭 drain；
  - Router 请求只在 `202 Accepted` 时视为成功，并将 `429/5xx` 与确定性其他 `4xx` 区分为临时/永久结果。
- Plugin Manager 注入窄 `EventRecorder`，默认 nop recorder 保持无 Events 局部用法可控。
- install/start/stop/update 只在主状态、持久化、进程和 Metrics 生命周期完成后记录一个最终事件；install/update 内部动作、幂等 no-op、失败路径和 Monitor shutdown 不记录假事件。
- recorder 拒绝或 EventMonitor enqueue 失败不回滚插件状态，也不改变 Backend API 的成功结果。
- Monitor 配置增加队列、退避、关闭和固定 16 KiB 上限校验；HTTP listener 收紧为 loopback IP；关闭顺序在 Plugin Manager 收口后 drain EventMonitor。

### 2.2 Router、Kafka 与 Marshaller

- Router Envelope allowlist 新增且仅新增 `events/monitor`，Events 与既有 Metrics/Logs 共用固定 `gopulse-observability-v1` Topic；Kafka key 保持 message ID，value 保持 HTTP 原始 bytes。
- Marshaller 公共 Envelope decoder 和 target registry 新增 `events/monitor`。
- 新增独立 Events validator/transformer，将 payload `timestamp` 确定性映射为 `@timestamp`，输出只包含七个固定文档字段。
- 新增独立 `EventsClient`：
  - 固定 `gopulse-events-v1-template`、`gopulse-events-v1-YYYY.MM.DD` 和 `gopulse-events-v1-read`；
  - 使用 message ID 作为 `_id`；
  - 每次写前重建/确认 template，成功写后重验实际 index 的根/metadata strict mapping 与 alias；
  - 复用受限 HTTP transport，但未复用 Logs template、alias、mapping、writer 或文档模型。
- Marshaller readiness 现在依次验证 VictoriaMetrics、Logs Store 和 Events Store；`/health` 未改变。
- 真实 Elasticsearch 9.5.2 返回 object mapping 时会省略显式 `type: object`；合同验证据此接受“省略或等于 object”，但仍严格要求 metadata `dynamic: strict` 和六个 keyword 属性。

### 2.3 Backend 管理员查询

- 新增 `backend/internal/eventquery` 的 options、固定词汇过滤、Events-domain HMAC cursor、PIT/search_after service、Elasticsearch repository 和 HTTP handler。
- `GET /api/v1/observability/events` 位于现有 Authentication 与数据库实时 `RequireAdmin` 之后；直接路由测试确认 `401/403` 时 application/repository 调用为零，admin 才进入查询。
- 首页默认 15 分钟、最大 24 小时、limit `1..100`；仅允许 `source,event_name,severity,plugin_id,operation,error_code` exact filters，并拒绝不可能的 event/operation 组合。
- repository 只打开 `gopulse-events-v1-read` 的 PIT，固定 `@timestamp desc,_shard_doc desc`、固定 `_source`，并验证命中 index prefix、响应大小、sort 与严格安全 DTO。
- alias 不存在返回空页；存储故障或不可信响应映射为 `503 events_unavailable`。
- 响应只返回 `timestamp,event_name,source,severity,message,metadata`，不返回 `_index/_id/PIT/message_id/Kafka` 或内部错误。

### 2.4 配置、脚本、CI、文档与版本

- `.env.example`、Monitor/Marshaller/Backend config 和 `scripts/dev.sh` 对齐 Events 固定配置。
- `scripts/verify.sh` 增加只读 Events route/template/alias 检查，不制造事件。
- 新增 `scripts/verify-events.sh`：
  - `--self-test` 拒绝不安全 project、listener/port/token/event size/plugin root 和 PID cleanup 目标；
  - 真实模式使用随机强归属 Compose project、loopback ports、临时凭据、临时 plugin root 和临时进程；
  - 通过 Backend admin API 真实执行 install `1.7.0`、stop、start、update `1.7.1`；
  - 经 Router/Kafka/Marshaller/Events Store 后从 Backend Events API 查询四个事件；
  - 验证未登录 `401`、普通用户 `403`、admin `200`、安全 DTO、独立 template/alias、strict mapping、文档数和敏感哨兵；
  - 退出时验证并清理本批进程、container、network、volume 和 plugin root，不触碰已有日常/历史资源。
- CI 增加独立 Events pipeline job，并将 Events self-test 纳入脚本门禁；治理测试的 product-job 计数同步更新。
- 更新根、Monitor、Router、Marshaller README，并新增 `docs/events-v1.md`。
- 根与 Frontend 版本同步到 `1.7.1`。

## 3. 主要变更文件

- Monitor：`monitor/internal/events/**`、`monitor/internal/plugin/manager.go`、`monitor/internal/metrics/publisher/**`、`monitor/internal/config/config.go`、`monitor/cmd/monitor/main.go`、`monitor/README.md`。
- Router：`router/internal/envelope/**`、`router/internal/routing/routing.go`、`router/internal/httpserver/server_test.go`、`router/README.md`。
- Marshaller：`marshaller/internal/events/**`、`marshaller/internal/elasticsearch/events_client*`、`marshaller/internal/envelope/envelope.go`、`marshaller/internal/config/config.go`、`marshaller/cmd/marshaller/main.go`、`marshaller/README.md`。
- Backend：`backend/internal/eventquery/**`、`backend/internal/http/api.go`、`backend/internal/http/router_events_test.go`、`backend/internal/apperror/error.go`、`backend/internal/http/response/response.go`、`backend/internal/config/config.go`、`backend/cmd/server/main.go`。
- 运维/CI/文档：`.env.example`、`scripts/dev.sh`、`scripts/verify.sh`、`scripts/verify-events.sh`、`scripts/ci/test_auto_pr_workflow.py`、`.github/workflows/quality-gates.yml`、`README.md`、`docs/events-v1.md`。
- 版本/计划：`VERSION`、`frontend/package.json`、`frontend/package-lock.json`、Phase 10 总/拆分方案状态和本记录。

## 4. 实际验证

以下命令均在最终 production diff 上执行并通过：

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

真实最终闭环使用 project `gopulse-events-c3c35ad7f940`，输出：

```text
[verify-events] Lifecycle Events query closed end to end through index gopulse-events-v1-2026.09.05.
```

验收前后 Docker project 快照一致；最终检查没有残留任何 `gopulse-events-*` container、network 或 volume。已有 `gopulse-phase0203-integration` 停止容器和 `gopulse_*` 日常 volumes 均未被修改或删除。

## 5. 偏差、问题与处理

- 初次脚本运行把 `MONITOR_EVENT_RETRY_MIN` 设为 `20ms`，低于方案允许的 `100ms`；Monitor 按预期安全启动失败。脚本修正为 `100ms` 后 self-test 与真实运行通过。
- 首次 Events Store 真实写入已创建文档，但 Elasticsearch 9.5.2 的 mapping 响应省略显式 object type，导致写后合同验证保持 offset 未提交并重试。实现收敛为接受缺省 object type，同时继续严格验证 metadata 的 `dynamic: strict` 和完整属性集合；新增单元回归后真实闭环通过。
- 初次隔离脚本启用了 Backend LogShip，真实 Logs 写入失败按单 Topic 顺序阻塞后续 Events。该日志故障不属于本批真实 Events 验收范围；最终脚本关闭 Backend LogShip，只保留必要的 Metrics/Events 共享传输，既有 Logs 共享边界由固定 Go 回归和 `verify-logs.sh --self-test` 覆盖。未修改 Logs 生产合同。
- 未检查任何第三方依赖源代码；问题均从本地调用点、运行错误和真实 Elasticsearch 响应语义解决。

## 6. 已知限制与后续

- 本批只接入 install/start/stop/update 成功终态；失败、unexpected exit、Metrics collection/target failure/recovery 和 episode 去抖留给 Phase-10-02。
- EventMonitor 源端是内存有界 best-effort queue；Monitor 进程崩溃不会持久化未发送事件，不宣称 exactly-once。Router 接受后的 Kafka/Marshaller/Elasticsearch 路径保持现有至少一次重试与 `_id` 幂等收敛。
- 未增加 Frontend Events 页面、告警、聚合、全文检索、ILM、独立 Topic/group 或应用容器镜像。
- 原生 PowerShell 文件未修改，未执行原生 Windows 验收。
- Pull Request #89、远程固定门禁和主远程 `main` 合入均已完成；后续工作从 Phase-10-02 的独立 `develop/1.7.2` 分支开始，不继续使用本批分支。

## 7. 远程收口

- 本地实现提交：`a1ee27e feat: add plugin lifecycle event query pipeline`。
- 推送分支：`develop/1.7.1`；推送前分支治理通过，远程自动合入后分支已删除。
- Pull Request：#89，标题 `feat: add plugin lifecycle event query pipeline`。
- 远程门禁：GitHub Actions `Auto PR and Merge` run `33952342892`，结论 `success`。
- 主远程合入：`upstream/main` / `origin/main` 的 `50cb497 feat: add plugin lifecycle event query pipeline (#89)`。
- 本状态更新属于 `update` 规划分支范围，不改变产品版本 `1.7.1`。
