# Phase-18-04：可观测计算层多副本、双 ES 与背压隔离

> 目标版本：`2.0.4`
>
> 开发分支：`develop/2.0.4`
>
> 最终验收次数：每个验收单元固定 `2` 次（首次 + 唯一一次重复）。

## 1. 目标

让 Router、Marshaller 以至少两个副本工作，使至少两个 Marshaller group member 同时拥有
Kafka partition，并将业务搜索与 Logs/Events Elasticsearch 分为独立故障域。所有过载和远程
失败必须有界、可见且不回压核心社交业务。

## 2. 实施范围

- Monitor 聚合的 Metrics/Logs/Events 可到达至少两个 Router。
- Kafka Topic 具有足够 partition；Marshaller generation ownership 和手动提交在 rebalance 下正确。
- Logs/Events 存储故障时 Metrics 仍可前进；VictoriaMetrics 故障时 Logs/Events 不全局停顿。
- 搜索 ES 与观测 ES 使用独立服务、卷、地址和网络边界，禁止跨写。
- Router buffer、源队列、Marshaller in-flight/retry 和目标写入均有固定上限、timeout 和状态指标。
- 每个 Router/Marshaller 副本的处理量、重试、lag、ownership 和退出状态可单独诊断。

## 3. 允许变更文件与逐文件验收

| 文件 | 文件级验收条件 |
| --- | --- |
| `.env.example` | 双 ES、partition、副本入口和背压配置具有明确默认值/范围且无凭据泄漏 |
| `deploy/compose.yaml` | 搜索 ES 与观测 ES 服务/卷/网络分离；Router/Marshaller 可运行两个副本；无新增宿主端口 |
| `deploy/runtime-contracts.json`、`docs/runtime-contracts.md` | 双 ES 依赖、副本角色、配置 key 和探针与实际代码/Compose 一致 |
| `backend/internal/config/config.go`、`backend/internal/config/config_test.go` | 搜索与观测 ES 地址分别验证，禁止错误复用或不安全 container 地址 |
| `backend/internal/platform/elasticsearch.go`、`backend/internal/platform/platform_test.go` | client 明确绑定用途，timeout/redirect/body 上界保持 |
| `backend/cmd/server/main.go` | Backend 的业务搜索、日志查询、事件查询和统计分别绑定对应 ES client |
| `backend/internal/logquery/logquery.go`、`backend/internal/logquery/logquery_test.go` | 日志读取允许持久化的副本身份元数据并保持既有公开日志字段契约 |
| `backend/cmd/search-reindex/main.go`、`backend/cmd/search-reindex/main_test.go` | reindex 只访问业务搜索 ES，观测 ES 不可成为回退目标 |
| `backend/internal/search/elasticsearch.go`、`backend/internal/search/processor_test.go` | 搜索读写只进入业务 ES；故障后 RabbitMQ 语义保持 |
| `router/internal/config/config.go`、`router/internal/config/config_test.go` | 多副本入口和 producer 背压配置范围/交叉校验完整 |
| `router/internal/kafka/producer.go`、`router/internal/kafka/producer_test.go` | buffer 满快速返回固定错误；单请求取消不影响其他记录；关闭有界 |
| `router/cmd/router/main.go` | 注入实例身份并保持鉴权、Envelope 和 readiness 边界 |
| `marshaller/internal/config/config.go`、`marshaller/internal/config/config_test.go` | partition 并发、双目标和 in-flight/retry 上界强类型校验 |
| `marshaller/internal/consumer/kafka.go`、`marshaller/internal/consumer/ownership.go` | 两个 member 可同时拥有 partition；revoke 后旧 lease 不再处理/提交 |
| `marshaller/internal/consumer/processor.go`、`marshaller/internal/consumer/processor_test.go` | 目标级阻塞隔离、成功后提交、永久错误继续和重复写语义明确 |
| `marshaller/internal/elasticsearch/client.go`、`marshaller/internal/elasticsearch/events_client.go` | Logs/Events 只写观测 ES；错误不越过 offset |
| `marshaller/internal/elasticsearch/client_test.go`、`marshaller/internal/elasticsearch/events_client_test.go` | 覆盖观测 ES 路径、故障、重试、重复写和跨目标拒绝 |
| `marshaller/internal/logs/validation.go`、`marshaller/internal/logs/transform_test.go` | 共享日志结构中的副本实例身份在 Marshaller 校验、转换和永久错误边界保持一致 |
| `marshaller/cmd/marshaller/main.go` | 多 partition 处理和各目标关闭共享同一有界生命周期 |
| `monitor/internal/config/config.go`、`monitor/internal/config/config_test.go` | Router 多副本入口和源队列上界校验完整 |
| `monitor/internal/logs/logs.go`、`monitor/internal/logs/logs_test.go` | 远程日志校验允许副本实例身份字段，保证日志入口与共享日志结构一致 |
| `monitor/cmd/monitor/main.go` | 将已校验的多 Router endpoint 配置装配到有界 failover publisher，不回退为单地址或 discard |
| `monitor/internal/metrics/publisher/publisher.go`、`monitor/internal/metrics/publisher/publisher_test.go` | Router 选择/失败转移有界，不因重试产生重复无界请求 |
| `componentmetrics/catalog.go`、`componentmetrics/validation.go` | 新增 buffer/ownership/partition 指标词汇固定且标签基数有限 |
| `scripts/ci/verify_component_metrics.py` | 组件指标 self-test 使用当前权威目录的固定样本预算，并校验管理端生成合同无漂移 |
| `monitor/internal/metrics/collector/components.go`、`monitor/internal/metrics/collector/components_test.go` | 所有 Router/Marshaller 副本均被采集，不被 DNS 随机结果遗漏 |
| `scripts/verify-compose-observability.sh` | 完整 Compose 验收适配 Phase-18 多副本拓扑：前端是唯一宿主入口，Backend 副本保持内网监听并通过网络边界校验 |
| `.github/workflows/quality-gates.yml` | Compose 配置门禁按单一 Frontend 宿主入口校验回环发布数量，并保持其他产品服务无宿主发布 |
| `scripts/verify-phase18-observability-scale.sh` | 正式模式仅接受 `--repetitions 2`；run-1 失败仍保存/清理并继续 run-2，不全局 prune |
| `scripts/ci/phase18_observability_scale.py` | 两次同候选运行、逐次原始 evidence、平均摘要和失败阶段持久化 |
| `scripts/ci/test_phase18_observability_scale.py` | 覆盖固定次数、跨写拒绝、平均计算、失败结果和 evidence 不可覆盖 |
| `router/README.md`、`marshaller/README.md`、`monitor/README.md`、`backend/README.md`、`README.md` | 配置、双 ES、背压和多副本边界与实际实现一致，不宣称 broker/存储 HA |
| `scripts/verify-logs.sh`、`scripts/verify-events.sh` | 独立宿主验收为 Backend 查询显式提供观测 ES 地址，保持日志/事件读写落在同一观测存储 |
| `admin-frontend/src/views/ObservabilityMetricsView.vue`、`admin-frontend/src/views/ObservabilityMetricsView.test.ts` | Metrics 查询在代理保留 503 状态但错误码变化时仍显示 VictoriaMetrics 不可用提示，并保持并发请求状态隔离 |
| `admin-frontend/src/services/componentMetrics.ts` | 管理端生成的组件指标合同与 `componentmetrics` 权威目录保持一致，catalog 校验不会在发起 Metrics 查询前误拒绝多副本容量指标 |
| `admin-frontend/src/services/management.ts`、`admin-frontend/src/services/management.test.ts` | 管理端告警目录校验覆盖当前组件指标的 `reason`、`partition` 标签，并保护目录加载后的规则创建能力 |
| `dev/logs/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md` | 记录实际文件、两次命令/结果、均值、故障隔离结论与限制 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 六处产品版本一致为 `2.0.4` |

## 4. 固定最终验收单元

正式 runner 以一次 `--repetitions 2` 调用创建 `run-1` 和 `run-2`；下列单元在两个 run
中各执行一次：

| 单元 | 验证内容 | 合并方式 |
| --- | --- | --- |
| U1 | Router、Marshaller、Monitor、Backend 搜索/ES 和 componentmetrics 直接测试 | `0/2`～`2/2` |
| U2 | runner/self-test 的安全、次数、双 ES 和 evidence 负例 | `0/2`～`2/2` |
| U3 | 冻结 `2.0.4` 候选的双 Router/双 Marshaller/双 ES 真实矩阵 | 数值两次平均；确定性项 `0/2`～`2/2` |
| U4 | runtime contract、版本、分支和差异检查 | 各命令 `0/2`～`2/2` |

U3 两次依次覆盖正常交替消息、Router 扩缩容、Marshaller rebalance、Kafka 短故障、搜索 ES
故障、观测 ES 故障、VictoriaMetrics 故障、恢复和最终 lag/offset/目标存储闭合。

## 5. 完成条件

1. 文件清单逐行有状态且不存在未预先批准的清单外修改。
2. 正式 runner 只调用一次且参数固定为 `--repetitions 2`；U1～U4 在两个 run 中各出现一次，
   候选和条件完全一致，逐次证据与 summary 完整。
3. 报告两次原值、算术平均和确定性结果计数，不运行第三次。
4. 结果归为 `target_met`、`boundary_found` 或 `execution_failed`。
5. 无论结果类型，创建实施记录、同步 `VERSION=2.0.4`、提交并停止。

版本更新不等于双 ES 隔离或多副本处理已经通过；实际能力只按 evidence 声明。
