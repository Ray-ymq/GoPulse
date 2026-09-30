# Phase-20-02：业务链路关联与新鲜度

> 目标版本：2.2.2；开发分支：develop/2.2.2；当前状态：未开始，依赖 01 完成。

## 1. 目标与范围

只贯穿“发帖 → MySQL 事实及 Outbox → RabbitMQ → Search Indexer → Elasticsearch 搜索可见”
一条业务链路，交付标准上下文传播、分段观测和端到端新鲜度，不扩展全部业务或自建 Trace 平台。

- 使用 OpenTelemetry Go SDK 与 W3C Trace Context；HTTP 上下文在事务 Outbox 中持久化，
  经 RabbitMQ 传播。进程重启、重试和重复投递后仍能关联原操作。
- 保留业务 event_id 的幂等身份；span/attempt 独立，不把重复尝试伪装成同一次执行。
- 旧消息无 Trace 字段仍合法；无效上下文按冻结的边界处理，不绕过业务消息严格校验。
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
| componentmetrics/backend.go、backend_test.go、catalog.go、registry_test.go、validation.go | 新鲜度指标标签有界；固定目录兼容现有分布 |
| monitor/internal/metrics/collector/components.go、components_test.go、marshaller/internal/envelope/envelope.go、components_test.go（components_test 同对应目录）、backend/internal/metricquery/metricquery.go、metricquery_test.go | 新指标端到端合同一致，禁止高基数 ID |
| backend/internal/config/config.go、config_test.go、search_indexer.go、search_indexer_test.go | Trace 配置可关闭、队列与超时有界、启动时验证 |
| deploy/phase20-trace.yaml、deploy/otel/phase20-collector.yaml、deploy/runtime-contracts.json、runtime-contracts.schema.json（后者同 deploy 目录） | 验收专属私有 Collector、固定镜像 digest、工件归属和 Trace 预算 |
| scripts/ci/phase20_chain.py、test_phase20_chain.py、scripts/verify-phase20-chain.sh | 原始 span/日志/终态关联及真实搜索可见证明 |
| docs/phase20-trace-and-freshness.md、docs/component-metrics.md | 路径、采样、时钟、低基数合同和覆盖限制 |

文件名带“同目录”表示逐一明确列出的文件，不是通配授权。如实现需新增迁移、业务 API、
管理 UI 或其他文件，先在 update 细化清单并合入 main，不在开发分支事后补登记。

## 3. 验收标准、命令与回归

代表性成功：接受一条发帖，关联到持久 Outbox、RabbitMQ、Indexer 和真实搜索命中，保留各阶段
时间及完整因果关系。代表性失败：延迟索引依赖，随后恢复；Trace 出口不可用时业务仍闭合。
再验证旧消息无 Trace、重复投递不重复业务投影、重试 span 可区分及无权限查询被拒绝。

直接检查：go -C backend test ./internal/bus ./internal/outbox ./internal/search
./internal/observability/tracing ./internal/observability/logging ./internal/logquery ./internal/config；
go -C componentmetrics test ./...；go -C monitor test ./internal/logs ./internal/metrics/collector；
go -C marshaller test ./internal/logs ./internal/envelope ./internal/elasticsearch。
待实现固定入口：scripts/verify-phase20-chain.sh；
python3 scripts/verify-phase20-evidence.py --chain <目录>。
最终运行受影响组件指标生成/验证、真实日志查询、业务消息集成与 runtime contract 门禁；
准确参数和新增固定指标在实现前冻结到本文件，不接受未定义的外部变量代替合同。

## 4. 完成条件

单链路真实成功/延迟恢复/出口失效、旧消息与日志兼容、消息所有权、幂等、严格字段与授权门禁
全部通过，公布实际 Trace/新鲜度开销及覆盖限制；创建同名日志、更新 2.2.2 并提交。
只声明这一链路已验证，不声明全站 Trace 或生产新鲜度 SLO。

## 5. 公共参考

- [OpenTelemetry 上下文传播](https://opentelemetry.io/docs/concepts/context-propagation/)
- [Go exporter](https://opentelemetry.io/docs/languages/go/exporters/)
- [Collector file exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/exporter/fileexporter/README.md)

依赖与 Collector 版本/digest 在本批实施前按现有锁文件和公共文档冻结，不使用 latest。
