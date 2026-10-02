# Phase-20-02：业务链路关联与新鲜度实施记录

- 日期：2026-09-30。
- 分支：`develop/2.2.2`，基于 `origin/main` 的 `6e8ed02` 创建。
- 目标版本：`2.2.2`；根 `VERSION` 及封闭版本元数据已同步。
- 实现状态：代码、部署合同、真实 Compose C01～C07 矩阵和固定门禁均完成；候选 revision `1ee181394c11a8b365e9c0aec701afbc67449cf0` 已通过重算验证。

## 已完成的工作

1. 增加 OpenTelemetry Go `v1.37.0` tracing 封装，使用 W3C `traceparent`/`tracestate`，ParentBased 10% TraceIDRatio 采样，2048 队列、256 批次、1 秒批处理、2 秒导出超时和 5 秒关停超时。SDK 的 BatchSpanProcessor 保持非阻塞，OTLP 仅指向验收专属私有 Collector。
2. 将可选 Trace Context 写入现有业务 Envelope，经事务 Outbox 持久化并由 RabbitMQ headers 继续传播；旧消息兼容，损坏的可选上下文被清除、计数并从新根开始，合法业务字段仍保留并经过原有严格校验。每次发布/消费有独立有界 attempt ID，Rabbit 重试会生成新的 attempt ID，event ID 仍保持业务幂等身份。
3. 在 Backend、Worker、Search Indexer 接入 `http.server`、`post.commit`、`outbox.publish`、`worker.consume`、`search.process` 和 `search.index` 分段 span；日志只增加有界 `trace_id`/`span_id` 及链路所需业务身份字段。现有管理日志查询和既有页面增加 Trace/Span 精确过滤，没有新增管理页面。
4. 增加 commit、publish、consume、index、visible 的低基数新鲜度目录；指标标签固定为 `stage`/`result`，Trace、Span、业务 ID、正文、凭据和 baggage 不进入标签。Backend、Business Worker、Search Indexer 增加无标签 `trace_context_invalid_total`。
5. 增加固定 digest 的私有 OTLP Collector Compose overlay 和 file exporter 配置，补齐 `.env.example` 与 runtime contract；正常运行默认关闭 Trace，验收 overlay 将六个相关进程采样率提升到 100%。
6. 增加 `phase20_chain` 原始证据重算器、wrapper 和测试，校验七个案例的候选绑定、身份、父子 span、时间顺序、日志覆盖、低基数指标、搜索 miss→hit、重试、异常上下文和权限边界。

## 实际变更文件

- `.env.example`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`。
- `admin-frontend/package.json`、`admin-frontend/package-lock.json`、`admin-frontend/src/services/componentMetrics.ts`、`admin-frontend/src/services/observability.ts`、`admin-frontend/src/services/observability.test.ts`、`admin-frontend/src/types/observability.ts`、`admin-frontend/src/views/ObservabilityLogsView.vue`。
- `backend/go.mod`、`backend/go.sum`、`backend/cmd/business-worker/main.go`、`backend/cmd/search-indexer/main.go`、`backend/cmd/server/main.go`。
- `backend/internal/observability/tracing/tracing.go`、`backend/internal/observability/tracing/context.go`、`backend/internal/observability/tracing/tracing_test.go`。
- `backend/internal/bus/envelope.go`、`backend/internal/bus/envelope_test.go`、`backend/internal/config/config.go`、`backend/internal/config/config_test.go`、`backend/internal/config/search_indexer.go`、`backend/internal/config/search_test.go`、`backend/internal/config/worker.go`、`backend/internal/config/worker_test.go`。
- `backend/internal/http/middleware/request_logging.go`、`backend/internal/logquery/logquery.go`、`backend/internal/observability/logging/logging.go`、`backend/internal/outbox/dispatcher.go`、`backend/internal/outbox/repository.go`、`backend/internal/platform/rabbitmq_publisher.go`、`backend/internal/post/delete.go`、`backend/internal/post/edit.go`、`backend/internal/post/repository.go`、`backend/internal/search/processor.go`、`backend/internal/worker/handler.go`、`backend/internal/worker/handler_test.go`。
- `componentmetrics/backend.go`、`componentmetrics/backend_test.go`、`componentmetrics/catalog.go`、`componentmetrics/registry_test.go`。
- `deploy/compose.yaml`、`deploy/runtime-contracts.json`、`deploy/phase20-trace.yaml`、`deploy/otel/phase20-collector.yaml`。
- `marshaller/internal/elasticsearch/client.go`、`marshaller/internal/logs/transform_test.go`、`marshaller/internal/logs/validation.go`、`monitor/internal/logs/logs.go`、`monitor/internal/logs/logs_test.go`。
- `scripts/ci/phase20_chain.py`、`scripts/ci/test_phase20_chain.py`、`scripts/ci/phase20_evidence.py`、`scripts/ci/test_phase20_evidence.py`、`scripts/ci/verify_component_metrics.py`、`scripts/verify-phase20-chain.sh`、`scripts/verify-phase20-evidence.py`。
- `docs/phase20-trace-and-freshness.md`、`docs/component-metrics.md`。

## 已执行命令与结果

- `git fetch origin`；读取 Phase 20 总方案和本批方案，从 `origin/main` 创建 `develop/2.2.2`：成功。
- `GOPROXY=https://goproxy.cn,direct go get go.opentelemetry.io/otel/sdk@v1.37.0 go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.37.0` 与 `go mod tidy`：成功，仅加入本批所需 SDK/exporter 依赖。
- `gofmt -l` 复核所有变更 Go 文件：无未格式化文件。
- `go -C backend test -count=1 ./internal/bus ./internal/outbox ./internal/search ./internal/worker ./internal/platform ./internal/post ./internal/http/middleware ./internal/observability/tracing ./internal/observability/logging ./internal/logquery ./internal/metricquery ./internal/config ./cmd/server ./cmd/search-indexer`：全部通过。
- `go -C componentmetrics test -count=1 ./...`：全部通过。
- `go -C monitor test -count=1 ./internal/logs ./internal/metrics/collector`：全部通过。
- `go -C marshaller test -count=1 ./internal/logs ./internal/envelope ./internal/elasticsearch`：全部通过。
- `npm --prefix admin-frontend run test -- src/services/observability.test.ts src/services/management.test.ts`：2 个文件、21 个测试通过。
- `npm --prefix admin-frontend run typecheck`：通过。
- `python3 scripts/ci/verify_component_metrics.py --self-test`：通过，固定目录、预算和生成客户端一致。
- `python3 -m unittest scripts.ci.test_phase20_chain scripts.ci.test_phase20_evidence`：10 项通过。
- `python3 -m py_compile scripts/ci/phase20_chain.py scripts/ci/test_phase20_chain.py scripts/ci/phase20_evidence.py scripts/ci/test_phase20_evidence.py scripts/ci/verify_component_metrics.py scripts/verify-phase20-evidence.py`：通过。
- `scripts/verify-phase20-chain.sh --manifest <临时候选 manifest> --work <临时证据目录>` 与 `python3 scripts/verify-phase20-evidence.py --chain <同目录>`：使用 verifier 内置的完整七案例原始结构 fixture，C01～C07 全部通过候选绑定和重算。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.2`：12 个运行进程合同通过。
- `docker compose -f deploy/compose.yaml -f deploy/phase20-trace.yaml config --no-interpolate --format json`：通过；补充检查确认 Collector digest、`linux/amd64`、私有 `observability` 网络和 4317 内部暴露符合合同。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/2.2.2 --base-ref origin/main`：通过。
- `git diff --check`：通过。
- `go -C backend test ...`、`go -C componentmetrics test -count=1 ./...`、Monitor/Marshaller 固定包测试：最终候选 `1ee1813` 全部通过；Worker retry attempt ID 回归测试通过。
- `npm --prefix admin-frontend run test -- src/services/observability.test.ts src/services/management.test.ts` 与 `npm --prefix admin-frontend run typecheck`：最终候选通过，21 个前端测试通过。
- `env GOPULSE_VERSION=2.2.2 GOPULSE_REVISION=$(git rev-parse HEAD) GOPULSE_IMAGE_TAG=phase20-02-1ee1813 docker compose ... build ...`：最终候选镜像构建成功；全新私有栈 `gopulse-phase2002c` 启动并健康检查成功。
- `python3 .phase20_runner.py`：实际执行 C01～C07；C02/C04 Elasticsearch 延迟、C03 Collector 故障、C05 旧/损坏/非法消息、C06 RabbitMQ 阻断与 Backend 重启、C07 权限/分页均完成，生成 `/var/tmp/gopulse-phase20-02-20260930/live-c/chain-evidence.json`。该临时 runner 随后删除，未进入仓库。
- `scripts/verify-phase20-chain.sh --manifest /var/tmp/gopulse-phase20-02-20260930/candidate.json --work /var/tmp/gopulse-phase20-02-20260930/live-c` 与 `python3 scripts/verify-phase20-evidence.py --chain /var/tmp/gopulse-phase20-02-20260930/live-c`：最终候选七案例重算均为 `execution_status=complete`。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.2`、Compose overlay config、版本/分支校验和 `git diff --check`：全部通过。

## 偏差、限制及后续项

- 为使既有 Logs 页面可实际提交 Trace/Span 查询，修改了现有 `ObservabilityLogsView.vue` 的筛选模型和高级筛选项；未新增页面，也未改变 API 路径。
- `phase20_evidence.py` 的公共日志字段投影补登记了 `trace_id`、`span_id` 和 `content_revision`，并补充了模块执行方式下的导入兼容；这是新增日志字段通过既有证据入口所需的合同修正。
- 一次附加校验误把 Bash wrapper 传给 `py_compile`，命令失败后已使用正确的 Python 文件列表重跑并通过；一次 Compose JSON 断言误把 `networks` 当作列表，修正为 Compose 实际映射结构后通过。产品代码和 Compose 配置本身均未因这两次命令用法错误回退。
- 最终候选 `1ee181394c11a8b365e9c0aec701afbc67449cf0` 使用 `gopulse/backend:phase20-02-1ee1813`、`gopulse/business-worker:phase20-02-1ee1813` 和对应栈镜像，在全新私有 Compose 栈执行 C01～C07；原始受限证据保存于 `/var/tmp/gopulse-phase20-02-20260930/live-c/`，目录大小 452 KiB、35 个工件文件。
- C01～C07 的业务身份和链路均由真实 OTLP、ES 日志、组件指标和搜索探针生成；C01～C07 对应最终证据 post_id 为 2、3、5、6、7、8、9。C02 实际阻断 35.730 秒并记录一次受控重试；C03 Collector 实际不可用 31.269 秒，关停 190 ms，队列峰值/导出超时记录为 0；C04 verifier 确认同一 event_id 有两个不同 attempt_id；C05 记录 1 个无上下文旧消息、1 个损坏可选上下文、1 个无效上下文计数和严格 Envelope 拒绝；C06 在发布依赖阻断期间提交后重启 Backend，持久化上下文和最终可见均通过；C07 实际返回 401/403，授权分页通过且未发现正文、凭据或 baggage。
- C03 的私有 file exporter 在 Collector 重启时重新打开并截断文件，故故障期间成功提交的 post_id=4 作为 fault receipt 保留，post_id=5 作为恢复后的完整链路证据；这没有改变业务接受和搜索闭合结果。该行为属于验收专属 file exporter 的证据限制，已记录在 C03 工件中。
- 最终固定门禁：Backend、Component Metrics、Monitor、Marshaller Go 测试全部通过；前端 21 项测试和 typecheck 通过；Component Metrics self-test、Phase 20 Python 10 项测试、py_compile、链路 verifier、证据 verifier、runtime contract 12 进程、版本、分支和 `git diff --check` 全部通过。真实证据候选绑定为 manifest digest `sha256:031e61c8ffd29a1c0cd44d89c8bda0f5fc3a7f09c8c3bfd6e289bcd2acae6ecb`。
- `visible` 阶段由真实业务搜索探针记录；本批产品代码提供固定目录和链路字段，未把 Elasticsearch 写确认冒充搜索可见，也未声明全站 Trace 或生产新鲜度 SLO。
