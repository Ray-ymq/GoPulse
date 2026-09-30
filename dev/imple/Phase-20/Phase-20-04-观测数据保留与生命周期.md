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

保留期、清理预算和配置默认值在本批创建分支前，依据 01/03 的增长基线在 update
补入本方案并合入 main。不能为了在短测试中达标改小产品保留期或修改宿主时钟。

## 2. 保留边界与开工前冻结项

Logs/Events 的 retention_days=R 表示保留含当前 UTC 日在内的 R 个日历日，R 为正整数。
每轮以固定评估时刻 T 计算 C=floor_UTC_day(T)-(R-1) 天；索引日期 D<C 才过期，D=C 必须
保留。例如 R=3、T 为 9 月 30 日，则 9 月 28～30 日保留，9 月 27 日及以前过期。
迟到数据按原始合法事件时间判定，不重写为到达时间续命；时间恰好等于 C 可写，C 前 1ns
应永久过期处理。写入过程中跨过 UTC 边界时按实际执行时刻重新判定，不沿用旧检查结论。

可删除索引必须同时满足：精确产品前缀与真实有效日历日期、固定观测集群身份、产品 mapping/
alias/归属标记合同和 D<C。不得把前缀匹配作为唯一所有权证明；外部索引、业务索引、无效日期、
未来日期及无法证明归属的同前缀索引均不删除，并保存拒绝原因。既有无归属标记索引的兼容
办法只能使用实施前登记的有限清单与严格 mapping 校验，不自动接管未知索引。

| 冻结项 | 开工前必须填写的字段 |
| --- | --- |
| Logs/Events 策略 | 各自 R、最小/最大合法配置值、默认值、增长证据与所需磁盘估算 |
| 清理预算 | cycle_seconds、batch_indices、request_timeout_seconds、单轮时间/请求上限及单位 |
| 失败与追赶 | retry_min/max_seconds、单轮最大重试次数、失败分类、恢复后 catchup_deadline_seconds 与依据 |
| 所有权及并发防护 | 集群绑定方式、精确索引/标记合同、旧索引有限兼容清单、写入与清理的防竞态方案及证据入口 |
| 查询边界 | 已过期/当前数据、空 alias、删除时在途分页的预期响应；保持现有授权/分页公共合同，需变化则先登记 |
| VictoriaMetrics | 镜像版本/digest、retentionPeriod 原值与单位、该版本历史写入/查询/回收合同、可实测范围及依据 |
| Trace 工件 | 单文件/总字节/文件数上限、轮转/清理触发、路径归属、执行时限与失败行为 |

所有值必须有限、可机读且有来源；catchup_deadline 不得以“恢复后最终成功”替代。
文件/数据回收量按删除前后实际读数报告，与配置声明和预测分开。

## 3. 允许变更文件与验收

| 文件 | 验收要求 |
| --- | --- |
| marshaller/internal/retention/policy.go、policy_test.go、runner.go、runner_test.go、elasticsearch.go、elasticsearch_test.go（均在 retention 目录） | 所有权/日期/边界校验、幂等、有界清理与失败恢复 |
| marshaller/internal/config/config.go、config_test.go、marshaller/cmd/marshaller/main.go | 校验策略、生命周期初始化/关闭，多副本行为确定 |
| marshaller/internal/logs/validation.go、transform.go、transform_test.go、marshaller/internal/events/events.go、events_test.go | 迟到数据按合同处理，不重建过期索引 |
| marshaller/internal/consumer/processor.go、processor_test.go | 每次实际写入/重试遵循有效保留边界；过期为有计数的永久终态，offset/lease 语义不退化 |
| marshaller/internal/elasticsearch/client.go、client_test.go、events_client.go、events_client_test.go（均在 elasticsearch 目录） | 当前/未过期读写与 alias 不退化，业务索引不可触及 |
| componentmetrics/catalog.go、registry_test.go、validation.go | 清理/迟到/失败指标低基数且属于固定目录 |
| componentmetrics/cmd/catalog/main.go、admin-frontend/src/services/componentMetrics.ts、management.ts、management.test.ts（后三者同 services 目录）、scripts/ci/verify_component_metrics.py | 固定指标、生成目录和严格客户端同步，旧指标子集保留 |
| monitor/internal/metrics/collector/components.go、components_test.go、marshaller/internal/envelope/envelope.go、components_test.go（对应目录）、backend/internal/metricquery/metricquery.go、metricquery_test.go | 生命周期诊断可通过既有指标链路查询 |
| backend/internal/logquery/logquery.go、logquery_test.go、backend/internal/eventquery/eventquery.go、eventquery_test.go | 保留窗口和已删除数据查询语义明确，不破坏授权/分页 |
| deploy/compose.yaml、deploy/runtime-contracts.json、deploy/runtime-contracts.schema.json、deploy/otel/phase20-collector.yaml | VM 原生保留配置、清理权限/预算、Trace 工件轮转及归属 |
| scripts/ci/phase20_retention.py、test_phase20_retention.py、phase20_evidence.py、test_phase20_evidence.py（均在 scripts/ci）、scripts/verify-phase20-retention.sh、scripts/verify-phase20-evidence.py | 真实归属 fixture、过期/未过期/迟到/删除失败、并发及安全清理 |
| docs/observability-retention.md、docs/component-metrics.md | 保留/删除/查询/回收延迟、告警/限制与容量影响 |

新增固定指标名称、标签、预期目录增量和准确验证入口在开工前登记；不把配置声明当作真实
删除证据。若竞态方案需要上述清单之外的配置/代码，先细化清单再开工。日志、状态与版本
元数据规则遵循总方案。

## 4. 固定案例、命令与回归

| case_id | 操作与通过规则 |
| --- | --- |
| R01 日期与归属边界 | 覆盖 C 前 1ns、C、C 后 1ns、跨 UTC 日、无效/未来日期及同前缀外部索引；只删除已证明归属的 D<C 索引，D=C/未过期/业务/未知归属数据及查询均保留 |
| R02 真实删除与写查 | 在独占观测 ES 中建立历史日期 fixture，保存删除前后索引/文档/alias 读数；真实过期索引消失，当前 Logs/Events 经完整链路写入并由管理员查询命中 |
| R03 迟到与重试 | 删除后投递旧时间消息，以及写失败后等待到跨保留边界再重试；过期消息有永久原因/计数并按有效 lease 提交，不重建索引，不无限存储重试 |
| R04 校验后删除竞态 | 写请求通过前置保留检查后阻塞，推进最低层受控时钟使其过期，另一清理执行删除，再释放在途写入；旧索引/文档不得复活，终态可核对。覆盖写先完成和删除先完成两种顺序及双副本 |
| R05 删除失败与追赶 | 精确归属目标的删除注入暂时失败，单轮请求/重试/耗时不超预算且有原因；恢复后在冻结 catchup_deadline 内删除积压，当前读写继续，永久权限错误不假报成功 |
| R06 双副本幂等 | 两个 Marshaller 对同一过期索引并发清理；真实已不存在的 404 可作为幂等结果，认证/权限/其他失败不可吞掉；无越权、无索引复活 |
| R07 查询兼容 | 当前数据、已过期时间窗、空 alias、清理时在途分页及未授权查询符合冻结公共合同；独立业务搜索数据和 alias 保持不变 |
| R08 VM 与 Trace 预算 | 实际进程 retentionPeriod 与合同一致，按锁定版本验证可观测历史边界与当前可查询；Collector 达到冻结轮转阈值时总文件/字节不越界、只清理归属路径且可核对原始工件 |

R04 不能仅靠“再次校验”或串行单测判为通过：必须记录写入已获准、清理已完成、写入恢复
以及最后真实存储查询的顺序。最低层可用注入时钟和请求屏障稳定复现；同一实现的真实 ES
集成门禁须覆盖删除与在途写入冲突，可由 Go 集成测试驱动受控时钟，不能修改宿主/集群时钟。
R03/R04 保存 offset、永久理由/计数、索引存在性与最终文档查询；即使 writer 预检查通过，
存储在途请求、自动建索引和后续重试也不得让过期索引复活。

VM 必须核对运行进程实际配置，并按该版本公共合同验证历史数据边界和当前数据可查询。
物理回收受原生周期影响，记录观察到的回收事实和未覆盖时间范围；短窗口不冒充多日保留验收。
版本公共合同若允许旧时间数据在回收周期前短暂可见，报告该行为与周期上界，不能套用
Logs/Events 的立即过期判据；也不能只检查启动参数便认定真实生命周期通过。

```bash
go -C marshaller test -count=1 ./internal/retention ./internal/config ./internal/logs ./internal/events ./internal/elasticsearch ./internal/consumer ./internal/envelope
go -C backend test -count=1 ./internal/logquery ./internal/eventquery ./internal/metricquery
go -C monitor test -count=1 ./internal/metrics/collector
go -C componentmetrics test -count=1 ./...
npm --prefix admin-frontend run test -- src/services/management.test.ts
npm --prefix admin-frontend run typecheck
python3 scripts/ci/verify_component_metrics.py --self-test
python3 -m unittest scripts.ci.test_phase20_retention scripts.ci.test_phase20_evidence
scripts/verify-phase20-retention.sh --manifest <候选manifest> --work <新目录>
python3 scripts/verify-phase20-evidence.py --retention <同目录>
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.4
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.4 --base-ref origin/main
git diff --check
```

phase20 入口待实现；真实依赖案例、Go 集成调用参数及新增固定指标在开工前冻结。
该入口必须运行 R01～R08 并保留底层测试/集成原始结果；不能把 mock 删除当成真实删除。

## 5. 完成条件

R01～R08、生成目录及上述固定门禁全部通过，verifier 逐例重算，执行状态 complete；
VM 与 Trace 工件的实际保留范围和未覆盖项如实记录，创建同名日志、更新 2.2.4 并提交。
误删、索引复活、无限重试或把配置当作执行证据属于阻断问题。

## 6. 公共参考

[VictoriaMetrics 配置与保留](https://docs.victoriametrics.com/victoriametrics/)
和 [Collector file exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/exporter/fileexporter/README.md)。
实施按锁定版本文档核对，不用最新文档假设现有镜像支持所有配置。
