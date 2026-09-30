# Phase-20-04：观测数据保留与生命周期

> 目标版本：2.2.4；开发分支：develop/2.2.4；当前状态：未开始，依赖 03 完成。

## 1. 目标与范围

让 Logs/Events/时序数据具有明确保留与删除边界，证明清理不会误删业务数据或破坏当前观测查询。
沿用现有按 UTC 日期的观测索引，优先实现受控过期索引清理，不为本批改造整个写入拓扑。

- Logs、Events 分别配置保留期、清理周期/批量/超时和失败重试，启动时校验上下界。
- 只操作明确归属的 gopulse-logs-v1-* / gopulse-events-v1-* 日期索引；禁止任意索引表达式、
  集群级删除、Docker prune 和误删业务 Elasticsearch。
- 多 Marshaller 的重复清理幂等，不引入新的 HA 控制面；并发写入/清理行为必须明确。
- 冻结迟到数据合同：超过保留边界的消息不能重新创建已过期索引；有计数及明确永久处理理由，
  不无限重试。当前与未过期索引保持可读，删除失败可诊断并在依赖恢复后继续。
- VictoriaMetrics 使用原生 retentionPeriod，锁定当前受支持版本的公共配置与实际删除/拒绝
  边界，不自建时序数据清理器，也不承诺配置后立即物理回收。
- Trace Collector 仅作为验收工件接收器，文件轮转/总量/归属清理有界；本批不声明长期 Trace 存储。

保留期、日期边界、清理预算和配置默认值在本批创建分支前，依据 01/03 的增长基线在 update
补入本方案并合入 main。不能为了在短测试中达标改小产品保留期或修改宿主时钟。

## 2. 允许变更文件与验收

| 文件 | 验收要求 |
| --- | --- |
| marshaller/internal/retention/policy.go、policy_test.go、runner.go、runner_test.go、elasticsearch.go、elasticsearch_test.go（均在 retention 目录） | 所有权/日期/边界校验、幂等、有界清理与失败恢复 |
| marshaller/internal/config/config.go、config_test.go、marshaller/cmd/marshaller/main.go | 校验策略、生命周期初始化/关闭，多副本行为确定 |
| marshaller/internal/logs/validation.go、transform.go、transform_test.go、marshaller/internal/events/events.go、events_test.go | 迟到数据按合同处理，不重建过期索引 |
| marshaller/internal/elasticsearch/client.go、client_test.go、events_client.go、events_client_test.go（均在 elasticsearch 目录） | 当前/未过期读写与 alias 不退化，业务索引不可触及 |
| componentmetrics/catalog.go、registry_test.go、validation.go | 清理/迟到/失败指标低基数且属于固定目录 |
| monitor/internal/metrics/collector/components.go、components_test.go、marshaller/internal/envelope/envelope.go、components_test.go（对应目录）、backend/internal/metricquery/metricquery.go、metricquery_test.go | 生命周期诊断可通过既有指标链路查询 |
| backend/internal/logquery/logquery.go、logquery_test.go、backend/internal/eventquery/eventquery.go、eventquery_test.go | 保留窗口和已删除数据查询语义明确，不破坏授权/分页 |
| deploy/compose.yaml、deploy/runtime-contracts.json、deploy/runtime-contracts.schema.json、deploy/otel/phase20-collector.yaml | VM 原生保留配置、清理权限/预算、Trace 工件轮转及归属 |
| scripts/ci/phase20_retention.py、test_phase20_retention.py、scripts/verify-phase20-retention.sh | 真实归属 fixture、过期/未过期/迟到/删除失败与安全清理 |
| docs/observability-retention.md、docs/component-metrics.md | 保留/删除/查询/回收延迟、告警/限制与容量影响 |

新增固定指标涉及生成客户端时，实施前补入具体生成文件与验证入口；不把配置声明当作真实删除证据。
日志、状态与版本元数据规则遵循总方案。

## 3. 验收、命令与回归

最小代表性集合：归属正确的过期索引被删除；未过期与业务/外部索引保留；过期迟到消息不复活；
删除失败被记录且恢复后清理成功；两个 Marshaller 重复清理没有越权或不可恢复错误。
使用历史 UTC 日期 fixture 验证判定，真实依赖操作验证执行；模拟时钟只用于最低层边界测试。

VM 必须核对运行进程实际配置，并按该版本公共合同验证历史数据边界和当前数据可查询。
物理回收受原生周期影响，记录观察到的回收事实和未覆盖时间范围；短窗口不冒充多日保留验收。

直接检查：go -C marshaller test ./internal/retention ./internal/config ./internal/logs
./internal/events ./internal/elasticsearch；go -C backend test ./internal/logquery ./internal/eventquery；
受影响 componentmetrics/Envelope 与查询合同检查。
待实现固定入口：scripts/verify-phase20-retention.sh；
python3 scripts/verify-phase20-evidence.py --retention <目录>。
最终固定回归为观测日志/事件写查、业务搜索保留、双副本清理和 runtime contract 验证。

## 4. 完成条件

确定性安全门禁、真实删除/失败恢复/迟到行为及旧查询兼容全部通过，VM 与 Trace 工件的实际
保留范围和未覆盖项如实记录，创建同名日志、更新 2.2.4 并提交。
误删、索引复活、无限重试或把配置当作执行证据属于阻断问题。

## 5. 公共参考

[VictoriaMetrics 配置与保留](https://docs.victoriametrics.com/victoriametrics/)
和 [Collector file exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/exporter/fileexporter/README.md)。
实施按锁定版本文档核对，不用最新文档假设现有镜像支持所有配置。
