# Phase-20-02：业务链路关联与新鲜度

> 目标版本：2.2.2；开发分支：develop/2.2.2；当前状态：未开始，依赖 01 完成。

## 1. 目标与范围

只贯穿“发帖 → MySQL 事实及 Outbox → RabbitMQ → Search Indexer → Elasticsearch 搜索可见”
一条业务链路，交付标准上下文传播、分段观测和端到端新鲜度，不扩展全部业务或自建 Trace 平台。

- 使用 OpenTelemetry Go SDK 与 W3C Trace Context；HTTP 上下文在事务 Outbox 中持久化，
  经 RabbitMQ 传播。进程重启、重试和重复投递后仍能关联原操作。
- 保留业务 event_id 的幂等身份；span/attempt 独立，不把重复尝试伪装成同一次执行。
- 旧消息无 Trace 字段仍合法；无效 Trace 上下文被丢弃并计数，从新的根 span 开始，
  不绕过业务消息严格校验，也不因可选 Trace 字段损坏丢失合法业务事件。
- Logs 中加入有界 trace_id/span_id，依次通过 Monitor、Marshaller、ES 映射和管理员查询；
  这些 ID 不进入指标标签，也不暴露给未授权查询。
- 分别记录事务提交、Outbox 排队/发布、消费、索引写入和查询可见；SDK span 与显式终态
  marker 配合。真实搜索探测负责可查询时刻，不把 ES 写成功当成搜索已可见。
- 以低基数分布/计数表达处理及新鲜度，注明异步时钟误差。Trace 采样不能决定业务是否完成。
- span、日志及传播元数据只含有界技术字段，不记录正文、凭据或任意 baggage。
- OTLP 仅连接私有、验收专属 Collector file exporter，以原始 span 工件复核单链路；
  持续运行默认使用有界采样。不新增 Trace 存储/UI，不改变 Kafka 的三类数据职责。

## 2. 允许变更文件与验收

| 文件 | 验收要求 |
| --- | --- |
| backend/go.mod、backend/go.sum | SDK/exporter 精确依赖，使用公共 API，不无关升级 |
| backend/internal/observability/tracing/tracing.go、tracing_test.go、context.go、context_test.go（同目录） | 标准传播、有界队列/超时/采样及安全关闭；Exporter 故障不阻塞业务 |
| backend/internal/http/middleware/request_logging.go、request_logging_test.go（同目录） | HTTP Trace 与日志关联，外部上下文处理有明确边界 |
| backend/internal/post/repository.go、integration_test.go（同目录） | 事务/Outbox 因果关系真实，不增加第二个非原子事件写入 |
| backend/internal/bus/envelope.go、envelope_test.go（同目录） | 可选上下文严格校验，旧消息兼容，event_id 语义不变 |
| backend/internal/outbox/repository.go、dispatcher.go、dispatcher_test.go、integration_test.go（同目录） | 上下文持久化、发布及重试关联；lease/owner 语义不退化 |
| backend/internal/platform/rabbitmq_publisher.go、backend/internal/worker/decoder.go、runtime.go、runtime_test.go（runtime 同 worker 目录） | 传播公共消息头/Envelope 上下文；消费所有权与 ack/retry 不退化 |
| backend/internal/search/processor.go、processor_test.go、backend/cmd/server/main.go、backend/cmd/search-indexer/main.go | 索引分段 span、进程初始化/关停，与业务终态一致 |
| backend/internal/observability/logging/logging.go、logging_test.go、monitor/internal/logs/logs.go、logs_test.go | 新日志字段经过两端严格验证，无任意属性透传 |
| marshaller/internal/logs/validation.go、transform.go、transform_test.go、marshaller/internal/elasticsearch/client.go、client_test.go | 二次校验和 ES strict 映射兼容，旧日志可读 |
| backend/internal/logquery/logquery.go、logquery_test.go、handler.go | 管理员可按 Trace 关联定位日志；授权及现有分页不退化 |
| admin-frontend/src/types/observability.ts、admin-frontend/src/services/observability.ts、observability.test.ts（测试同 services 目录） | 严格客户端接受新增关联字段，拒绝未知字段，兼容旧记录；不新增管理页面 |
| componentmetrics/backend.go、backend_test.go、catalog.go、registry_test.go、validation.go | 新鲜度指标标签有界；固定目录兼容现有分布 |
| componentmetrics/cmd/catalog/main.go、admin-frontend/src/services/componentMetrics.ts、management.ts、management.test.ts（后三者同 services 目录）、scripts/ci/verify_component_metrics.py | 生成目录与严格客户端一致；保留旧指标子集，只增加已登记族/标签及对应校验 |
| monitor/internal/metrics/collector/components.go、components_test.go、marshaller/internal/envelope/envelope.go、components_test.go（components_test 同对应目录）、backend/internal/metricquery/metricquery.go、metricquery_test.go | 新指标端到端合同一致，禁止高基数 ID |
| backend/internal/config/config.go、config_test.go、search_indexer.go、search_indexer_test.go | Trace 配置可关闭、队列与超时有界、启动时验证 |
| deploy/phase20-trace.yaml、deploy/otel/phase20-collector.yaml、deploy/runtime-contracts.json、runtime-contracts.schema.json（后者同 deploy 目录） | 验收专属私有 Collector、固定镜像 digest、工件归属和 Trace 预算 |
| scripts/ci/phase20_chain.py、test_phase20_chain.py、phase20_evidence.py、test_phase20_evidence.py（均在 scripts/ci）、scripts/verify-phase20-chain.sh、scripts/verify-phase20-evidence.py | 原始 span/日志/终态关联及真实搜索可见证明 |
| docs/phase20-trace-and-freshness.md、docs/component-metrics.md | 路径、采样、时钟、低基数合同和覆盖限制 |

文件名带“同目录”表示逐一明确列出的文件，不是通配授权。如实现需新增迁移、业务 API、
管理 UI 或其他文件，先在 update 细化清单并合入 main，不在开发分支事后补登记。

## 3. 开工前合同与强制关联证据

创建分支前在 update 登记并合入 main：SDK/Collector 精确版本与 digest、传播字段/数据库
持久化方式、span 名称及父子关系、固定指标族/标签、正常采样率、队列容量、导出超时、
关停时限、工件上限、轮询间隔和最大时钟误差。每项有实际值、单位、来源及校验方式，
不得以 SDK 默认值、任意环境变量或“有界”代替配置合同；需要迁移则先补入具体文件。

每条受控发帖保存 request_id、post_id、content_revision、event_id、outbox_id、trace_id，
以及各次发布/消费的 attempt_id 和 span_id。Trace ID 为合法 32 位十六进制，Span ID 为合法
16 位十六进制且非全零；event_id 不随重试变化，每次执行有独立 span 身份。
父子关系在开工前冻结为一张确定的关系表，允许异步 link，但不能只按相近时间拼接链路。
已提交 Outbox 的持久上下文必须在进程重启后仍可使用；不以跨进程常驻 HTTP span 实现关联。

| 必须记录的时刻/区间 | 原始来源与判据 |
| --- | --- |
| t_request_start、t_accept | HTTP server span 和压测客户端成功响应记录；二者分开，响应完成不是事务提交时刻 |
| t_commit | 记录事实/Outbox 所在同一事务的 Commit 成功确认点；该时刻是提交观察上界，不能以 Envelope occurred_at 代替 |
| t_publish_start、t_publish_ack | 每次 Outbox 发布 span，明确排队、发布耗时及 RabbitMQ 确认结果；重试/lease 单独记录 |
| t_consume_start、t_consume_end | 每次 Indexer 消费 span，与 event_id/attempt_id、ack/retry 结果关联 |
| t_index_start、t_index_ack | 实际索引请求 span 和成功/失败响应，区分依赖等待与本地处理 |
| t_visible | 真实业务搜索首次命中正确 post_id/revision 的客户端探测；记录此前未命中和轮询间隔 |

必须给出提交到可见、提交到首次发布、消费等待、每次处理/索引耗时及接受响应后的可见等待。
并行阶段使用实际区间解释，不能把重叠耗时简单相加。可见早于响应时保留原时序并说明，
不能修改原值制造顺序。单进程耗时使用单调时钟；跨进程时刻附时钟误差估计和探测开销。
误差超出冻结上限或缺时钟证据时，该跨进程分段为 incomplete，不能把负值截成零后宣称精确。
可查询时刻是轮询观察上界，报告区间，不把精度写得高于轮询与时钟能力。

受控成功/延迟案例的采样率固定为 100%，独占验收流量并有界保存全部 span，用于验证传播。
正常运行的冻结采样率另行报告；未采样业务仍按 01 的事实/事件/搜索判据闭合。
span 数量、接受/导出/丢弃数量和丢弃原因可核对；Trace 丢弃不伪装成业务丢失或链路完整。

## 4. 固定案例、命令与回归

| case_id | 操作与通过规则 |
| --- | --- |
| C01 正常发帖 | 所有必需身份、时刻与因果关系可重算，日志可按 trace_id 关联；搜索返回正确版本，业务在 120 秒内闭合 |
| C02 索引依赖延迟 | 在首次索引请求前阻断该依赖 30 秒后恢复，保存真实阻断区间；至少一次失败/重试或等待被正确归因。报告阻断与处理区间及已有重试间隔，不把重试等待计成存储执行时间，恢复后业务闭合 |
| C03 Trace 出口故障 | 私有 Collector 不可用 30 秒后恢复，合法业务持续接受并闭合；队列峰值、丢弃计数、导出超时及关停均符合合同，不要求补回已声明丢弃的 span |
| C04 重复与重试 | 同一 event_id 重复投递并触发一次受控重试；attempt/span 可区分，搜索最终版本正确，业务投影无重复或回退 |
| C05 旧消息与无效上下文 | 无 Trace 的合法旧消息和损坏可选上下文的合法消息均能闭合；无效上下文按第 1 节处理。非法业务 Envelope 仍被拒绝，旧日志仍可查询 |
| C06 提交后重启 | 阻断发布依赖，确认事实及 Outbox 已提交后重启对应 Backend，再恢复依赖；保留原 event_id 与持久上下文，最终搜索可见，lease/ack/retry 语义不退化 |
| C07 权限与字段边界 | 未认证/非管理员关联查询被拒绝，授权分页兼容；正文/凭据/baggage 不进入 Trace，trace_id/span_id/event_id 不进入指标标签，管理客户端接受合法新旧日志并拒绝未知字段 |

每例保存故障生效/恢复回执、原始 span、日志、业务事实与搜索探测，不用摘要布尔值证明通过。
业务恢复以 01 的独立 120 秒门禁计时；C02/C06 的受控故障时间与恢复时间分别展示。
安全/传播判据必须通过；缺失 span 或身份不能归为普通容量边界。

固定命令如下；phase20 入口待实现，完整案例集合由合同读取，禁止临时删例：

```bash
go -C backend test -count=1 ./internal/bus ./internal/outbox ./internal/search ./internal/worker ./internal/platform ./internal/post ./internal/http/middleware ./internal/observability/tracing ./internal/observability/logging ./internal/logquery ./internal/metricquery ./internal/config ./cmd/server ./cmd/search-indexer
go -C componentmetrics test -count=1 ./...
go -C monitor test -count=1 ./internal/logs ./internal/metrics/collector
go -C marshaller test -count=1 ./internal/logs ./internal/envelope ./internal/elasticsearch
npm --prefix admin-frontend run test -- src/services/observability.test.ts src/services/management.test.ts
npm --prefix admin-frontend run typecheck
python3 scripts/ci/verify_component_metrics.py --self-test
python3 -m unittest scripts.ci.test_phase20_chain scripts.ci.test_phase20_evidence
scripts/verify-phase20-chain.sh --manifest <候选manifest> --work <新目录>
python3 scripts/verify-phase20-evidence.py --chain <同目录>
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.2
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.2 --base-ref origin/main
git diff --check
```

生成目录校验必须比较 generator 输出与提交的 componentMetrics.ts，并验证旧指标子集保留；
具体新增族和预期计数在开工前登记进本方案与校验器。真实消息、日志写查和前端严格合同
由 C01/C04/C05/C07 覆盖，不另跑不适用于本候选的历史整套产品矩阵。

## 5. 完成条件

C01～C07、生成目录、严格客户端及上述固定门禁全部通过，关联证据经 verifier 重算，
执行状态 complete；公布实际 Trace/新鲜度开销、时钟精度及覆盖限制，
创建同名日志、更新 2.2.2 并提交。
只声明这一链路已验证，不声明全站 Trace 或生产新鲜度 SLO。

## 6. 公共参考

- [OpenTelemetry 上下文传播](https://opentelemetry.io/docs/concepts/context-propagation/)
- [Go exporter](https://opentelemetry.io/docs/languages/go/exporters/)
- [Collector file exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/exporter/fileexporter/README.md)

依赖与 Collector 版本/digest 在本批实施前按现有锁文件和公共文档冻结，不使用 latest。
