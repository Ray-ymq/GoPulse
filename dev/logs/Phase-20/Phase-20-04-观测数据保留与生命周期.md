# Phase-20-04：观测数据保留与生命周期实施日志

> 目标版本：`2.2.4`；分支：`develop/2.2.4`；最终候选：`3bc6cdf1c3790f048432a59429d0d7b833c1807b`。
> 执行状态：`complete`；记录日期：2026-10-01。

## 已完成工作

- 实现 Logs/Events 的 UTC 日历保留策略、1～90 日配置校验、7 日默认值、60 秒清理周期、16 索引批量、15 秒轮次预算、3 秒请求超时、有限重试和 60 秒追赶合同。
- 实现观测 Elasticsearch 的固定 cluster UUID、精确日期索引、strict mapping、`_meta` 归属标记和固定 read alias 校验；清理前阻断写入并二次检查归属，业务索引和未知同前缀索引不会进入删除请求。
- 实现写入前/实际写入后的 UTC 保留复核、过期 Logs/Events 的永久错误码和 Processor 提交语义，覆盖清理与在途写入竞态，避免过期索引复活。
- 增加清理、迟到数据、重试、阻塞和最近成功时间的固定低基数 Marshaller 指标，并同步 Go catalog、生成的管理端指标目录和浏览器标签目录。
- 增加真实 Elasticsearch、双 Runner 幂等、暂时删除失败、权限失败、当前 alias 查询和 Trace/VM 真实依赖的 R01～R08 验收入口与证据 verifier。
- 将 VictoriaMetrics Compose 参数固定为 `-retentionPeriod=30d`；将 Collector 文件工件固定在 `/var/lib/gopulse/trace`，单文件 16 MiB、总量 64 MiB、最多 3 个备份。
- 同步运行时合同、环境示例、前后端版本元数据、Phase 状态文档、能力状态和保留生命周期文档；根 `VERSION` 更新为 `2.2.4`。

## 实际变更文件

- 根与文档：`.env.example`、`README.md`、`VERSION`、`dev/imple/Phase-20/Phase-20-04-观测数据保留与生命周期.md`、`dev/phases/Plan.md`、`dev/phases/README.md`、`docs/capability-status.md`、`docs/component-metrics.md`、`docs/observability-retention.md`。
- 前端与目录：`admin-frontend/package.json`、`admin-frontend/package-lock.json`、`admin-frontend/src/services/componentMetrics.ts`、`admin-frontend/src/services/management.ts`、`frontend/package.json`、`frontend/package-lock.json`、`componentmetrics/catalog.go`、`componentmetrics/registry_test.go`。
- 部署合同：`deploy/compose.yaml`、`deploy/otel/phase20-collector.yaml`、`deploy/runtime-contracts.json`。
- Backend：`backend/internal/eventquery/eventquery.go`、`backend/internal/eventquery/eventquery_test.go`、`backend/internal/logquery/logquery.go`、`backend/internal/logquery/logquery_test.go`。
- Marshaller：`marshaller/cmd/marshaller/main.go`、`marshaller/internal/config/config.go`、`marshaller/internal/config/config_test.go`、`marshaller/internal/consumer/processor.go`、`marshaller/internal/consumer/processor_test.go`、`marshaller/internal/elasticsearch/client.go`、`marshaller/internal/elasticsearch/client_test.go`、`marshaller/internal/elasticsearch/events_client.go`、`marshaller/internal/elasticsearch/events_client_test.go`、`marshaller/internal/events/events.go`、`marshaller/internal/events/events_test.go`、`marshaller/internal/logs/transform.go`、`marshaller/internal/logs/transform_test.go`、`marshaller/internal/retention/{policy.go,policy_test.go,runner.go,runner_test.go,elasticsearch.go,elasticsearch_test.go}`。
- 验收工具：`scripts/ci/phase20_evidence.py`、`scripts/ci/phase20_retention.py`、`scripts/ci/test_phase20_retention.py`、`scripts/ci/verify_component_metrics.py`、`scripts/verify-phase20-evidence.py`、`scripts/verify-phase20-retention.sh`。

## 实际执行命令与结果

- `git fetch origin --prune`：完成；在 `update` 冻结 04 合同并经 PR #205 以 merge commit `221ae2e` 合入 `main`，随后从最新 `origin/main` 创建 `develop/2.2.4`。
- `gofmt`：所有变更 Go 文件格式化完成；`git diff --check`：通过。
- Marshaller 定向测试：`go test -count=1 ./internal/retention ./internal/config ./internal/logs ./internal/events ./internal/elasticsearch ./internal/consumer ./internal/envelope ./cmd/marshaller`，通过。
- Backend 定向测试：`go test -count=1 ./internal/logquery ./internal/eventquery ./internal/metricquery`，通过。
- Monitor 定向测试：`go test -count=1 ./internal/metrics/collector`，通过。
- componentmetrics：`go test -count=1 ./...`，在同步 Marshaller 新预算 `362` 后通过。
- 管理端：`npm run test -- src/services/management.test.ts`（13 tests）和 `npm run typecheck`，均通过。
- `python3 scripts/ci/verify_component_metrics.py --self-test`：通过；`python3 -m unittest scripts.ci.test_phase20_retention scripts.ci.test_phase20_evidence`：9 tests 通过；Python 编译检查与 `bash -n scripts/verify-phase20-retention.sh`：通过。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.4`：通过。
- `python3 scripts/ci/validate_versions.py`：通过；`python3 scripts/ci/validate_branch.py --branch develop/2.2.4 --base-ref origin/main`：通过。
- `scripts/verify-phase20-retention.sh --manifest /var/tmp/gopulse-phase20-04-candidate.json --work /var/tmp/gopulse-phase20-04-evidence-3bc6cdf`：输出 `execution_status=complete`，R01～R08 全部通过；随后 `python3 scripts/verify-phase20-evidence.py --retention /var/tmp/gopulse-phase20-04-evidence-3bc6cdf`：输出 `execution_status=complete`，R01～R08 全部为 `pass`。
- 最终真实证据记录：Trace 3 个文件、总量 `34,456,978` 字节；Elasticsearch、VictoriaMetrics、Collector 镜像身份和候选 revision 均写入 `/var/tmp/gopulse-phase20-04-evidence-3bc6cdf/retention.json`；VM 物理回收观察值为 `false`。

## 偏差与修正

- 锁定的 VictoriaMetrics `v1.151.0` 在刚导入的短窗口中即时查询没有返回样本，但 `query_range`、series 和 export 可核对真实样本。验收入口改为固定时间窗口的真实 `query_range`，没有改变产品 retention 配置，也没有把该短窗口解释成物理回收证据。
- Collector `0.138.0` 是无 shell 的精简镜像，原先的 `docker exec sh/find` 无法读取归属目录。验收入口改用 Docker 文件复制接口读取同一 `/var/lib/gopulse/trace` 路径，并按 File Exporter 实际生成的时间戳轮转文件名校验；未扩大清理路径。
- 真实验收中还修正了 OTLP fixture 的 Go 代码生成、Collector 端口就绪等待和 retention evidence R04 原始测试引用；修正后均重新绑定候选并重跑最终 R01～R08，未复用失败候选证据。

## 已知限制与后续项

- Logs/Events 的 7 日默认值和本批删除证据不构成宿主磁盘容量、长期成本或多日稳定性承诺；资源预算和持续运行属于后续 Phase-20-05/06。
- VictoriaMetrics 只验证了原生 `30d` 进程参数、当前/窗口查询和提交行为；短时运行没有等待 30 日物理回收，因此不声明立即物理删除。
- Trace 只验收固定 Collector 归属目录内的有界文件工件和轮转，不声明长期 Trace 存储、查询或跨目录回收能力。
- 未登记归属标记的未知同前缀索引保持拒绝，不会被自动接管；业务索引与 alias 继续由独立业务 Elasticsearch 管理。
