# Phase-18-04：可观测计算层多副本双 ES 与背压隔离实施记录

## 实际完成

- 在 `develop/2.0.4` 完成 Router、Marshaller 双副本拓扑；为副本增加可识别的 `instance_id`，Kafka topic 使用四个 partition，并将 Router buffer、Marshaller in-flight/retry 与目标阻塞状态纳入有界配置和指标。
- 完成 Marshaller generation ownership、revoke fencing、按 partition 并行且单 partition 保序、写入成功后提交 offset、永久坏记录继续处理和目标级重试/阻塞隔离。
- 完成业务搜索 ES 与 Logs/Events ES 的独立服务、卷、网络和地址；为业务搜索与可观测写入增加 purpose 约束，防止跨 ES 写入。
- 完成 Monitor 多端点轮询与故障转移；补齐 runtime contract、Compose、环境样例、README、前端版本元数据和 Phase 18-04 固定验收 runner。
- 当前产品版本已同步为 `2.0.4`。

实际变更文件：

- `.env.example`、`VERSION`、`README.md`、`docs/runtime-contracts.md`、`deploy/compose.yaml`、`deploy/runtime-contracts.json`。
- `frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`。
- `backend/README.md`、`backend/cmd/search-reindex/main.go`、`backend/cmd/search-reindex/main_test.go`、`backend/internal/config/config.go`、`backend/internal/config/config_test.go`、`backend/internal/platform/elasticsearch.go`、`backend/internal/platform/platform_test.go`、`backend/internal/search/elasticsearch.go`、`backend/internal/search/processor_test.go`。
- `componentmetrics/catalog.go`、`componentmetrics/validation.go`、`componentmetrics/registry_test.go`。
- `router/README.md`、`router/cmd/router/main.go`、`router/internal/config/config.go`、`router/internal/config/config_test.go`、`router/internal/kafka/producer.go`。
- `marshaller/README.md`、`marshaller/cmd/marshaller/main.go`、`marshaller/internal/config/config.go`、`marshaller/internal/config/config_test.go`、`marshaller/internal/consumer/kafka.go`、`marshaller/internal/consumer/ownership.go`、`marshaller/internal/consumer/processor.go`、`marshaller/internal/consumer/processor_test.go`、`marshaller/internal/elasticsearch/client.go`、`marshaller/internal/elasticsearch/client_test.go`、`marshaller/internal/elasticsearch/events_client.go`、`marshaller/internal/elasticsearch/events_client_test.go`。
- `monitor/README.md`、`monitor/cmd/monitor/main.go`、`monitor/internal/config/config.go`、`monitor/internal/config/config_test.go`、`monitor/internal/metrics/collector/components_test.go`、`monitor/internal/metrics/publisher/publisher.go`、`monitor/internal/metrics/publisher/publisher_test.go`。
- `scripts/verify-phase18-observability-scale.sh`、`scripts/ci/phase18_observability_scale.py`、`scripts/ci/test_phase18_observability_scale.py`。
- 本实施记录文件。

## 实际执行的检查与结果

- `git fetch origin --prune`：成功；从 `origin/main` 创建 `develop/2.0.4`，随后以 `git merge --ff-only develop/2.0.3` 保留已完成的前序 Phase 18-03 提交。
- 各 Go 模块执行 `go test -count=1 ./...` 或 Backend 受影响包测试：componentmetrics、Router、Marshaller、Monitor、Backend 均通过。
- 各 Go 模块执行 `go vet ./...`：通过。
- 受影响模块 race 测试：通过。
- `npm ci --ignore-scripts --dry-run`：frontend、admin-frontend 均通过。
- `python3 -m unittest discover -s scripts/ci -p test_phase18_observability_scale.py`：通过。
- `python3 -m py_compile scripts/ci/phase18_observability_scale.py`：通过。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- `docker info`：通过。
- `git diff --check`：通过。

正式 runner 只调用一次：

```text
scripts/verify-phase18-observability-scale.sh --repetitions 2
```

命令退出码为 `0`，创建了唯一的 `run-1` 和 `run-2`，未执行第三轮。证据目录为：
`.run/phase18-observability-scale-2.0.4-13e7a9da5566-47d164a903e2/`。

## 正式两轮结果

| 单元 | run-1 | run-2 | 结果 |
| --- | --- | --- | --- |
| U1 | 通过 | 通过 | `2/2` |
| U2 | 通过 | 通过 | `2/2` |
| U3 | `boundary_found` | `boundary_found` | `0/2 target_met` |
| U4 | 通过 | 通过 | `2/2` |

正式汇总结果为 `boundary_found`，`target_met_runs=0`。两轮 U3 均完成 Compose build/up、健康检查、管理员初始化、故障动作、恢复动作、业务 ES/可观测 ES/Kafka group 收尾检查和 Compose 清理；清理退出码均为 `0`。通过的 U3 场景为 setup、Router 副本故障转移、Marshaller rebalance、Kafka 短故障、业务搜索 ES 故障和可观测 ES 故障。

U3 acceptance 场景耗时（秒，run-1 / run-2 / 平均）：

| 场景 | run-1 | run-2 | 平均 |
| --- | ---: | ---: | ---: |
| normal | 58.006 | 63.640 | 60.823 |
| router replica failover | 2.771 | 2.818 | 2.795 |
| marshaller rebalance | 2.714 | 2.559 | 2.636 |
| Kafka short fault | 2.705 | 2.839 | 2.772 |
| business search ES fault | 2.789 | 2.577 | 2.683 |
| observability ES fault | 2.731 | 2.814 | 2.772 |
| VictoriaMetrics fault | 22.860 | 22.833 | 22.846 |
| recovery | 49.021 | 48.898 | 48.960 |

## 偏差、限制与后续项

- 初始本地分支为已完成但尚未合入远端 `main` 的 `develop/2.0.3`；按 Phase 顺序从 `origin/main` 创建 `develop/2.0.4` 后 fast-forward 保留其六个前序提交。这是为保留前序批次成果所做的基线偏差。
- Phase-18-04 清单未单列 `componentmetrics/registry_test.go`，但新增固定指标族改变了 registry 的最大样本数；该测试同步调整为新的固定预算，否则直接测试会失败。此项已在本记录中明确登记。
- 两轮 U3 的 `normal` 和 `recovery` 浏览器验收均在 `waitForLogs` 等待窗口内未看到记录；两轮 VictoriaMetrics 故障验收均未看到预期的不可用提示。现有证据只能确认这些 acceptance 断言失败，未在固定两轮之外继续定位。
- 两轮四个副本 metrics probe 均退出码 `1`。冻结证据中的 probe 命令使用了 `$$ROUTER_METRICS_TOKEN` / `$$MARSHALLER_METRICS_TOKEN` 形式，未提供有效副本指标证据；这是验收脚本的后续修复项。由于本批固定 runner 只能执行一次且不得追加第三轮，未在本批修改后重跑。
- 因 U3 在两轮均存在上述边界，本批结果不宣称 `target_met`；按计划要求仍同步 `VERSION=2.0.4`、创建本记录并提交。

## PR 门禁修复记录

实际修复：

- `deploy/compose.yaml` 将 MySQL 连接池参数从共享初始化环境移到 Backend、Business Worker 和 Search Indexer 运行时环境，恢复迁移、搜索初始化和管理初始化容器的角色最小环境集合。
- `scripts/verify-compose-observability.sh` 按当前 Phase-18 拓扑仅要求 Frontend 发布宿主回环端口；Backend 副本通过 Frontend 的内网 upstream 访问。
- `dev/imple/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md` 补登记完整 Compose 验收脚本。

实际执行的检查与结果：

- 远程运行 `36325432100`：治理测试因初始化容器多出四个 MySQL 连接池变量失败；完整 Compose 验收因 Backend 未绑定宿主回环端口失败；其余产品与集成 job 通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_verify_business.py'`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：108 项通过。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --format json`：通过，并确认仅 Frontend 发布宿主端口、Backend 无宿主端口。
- `bash scripts/verify-compose.sh --self-test`、`bash -n scripts/verify-compose-observability.sh`、`git diff --check`：通过。

后续：修复提交推送后等待 GitHub Actions 重新执行完整门禁；本地未重复 Phase-18-04 固定两轮正式验收。

远程复核补充：运行 `36329279305` 中 Branch governance 已通过；`Scripts and Compose` 暴露 `.github/workflows/quality-gates.yml` 仍要求两个宿主回环发布，而当前 Compose 只有 Frontend 一个发布。已将该门禁登记到实施计划并调整为单个发布，等待再次推送后的远程复核。

远程复核补充：运行 `36329500862` 中 Branch governance、Scripts and Compose 及全部单模块/集成 job 通过；Full-stack Compose acceptance 在 `scripts/verify-compose-observability.sh` 的旧单 ES 网络断言处失败，自动 PR job 因此跳过。已将完整拓扑断言更新为搜索 ES 仅连接 business、观测 ES 仅连接 observability，并纳入观测 ES 健康状态及持久卷检查；等待再次推送后的远程复核。

远程复核补充：运行 `36330185241` 中网络拓扑断言已通过，Branch governance、Scripts and Compose 及全部单模块/集成 job 通过；Full-stack Compose acceptance 在管理员场景的 `waitForLogs` 等待超时，后端查询持续返回空页，日志转发出现 `permanent_rejection`。对照共享日志构造器和 Monitor 校验器确认 `instance_id` 已由所有进程写入但未列入远程日志允许字段；本批将该字段纳入 Phase-18-04 文件范围，并限制为安全 token。

本次实际变更文件：

- `monitor/internal/logs/logs.go`：允许并校验副本 `instance_id` 字段。
- `monitor/internal/logs/logs_test.go`：增加合法副本身份通过和不安全身份拒绝测试。
- `dev/imple/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md`：登记远程日志校验文件范围。

本次实际执行的检查与结果：

- `go test ./internal/logs`（Monitor）：通过。
- `go test ./...`（Monitor）：通过。
- `gofmt -w internal/logs/logs.go internal/logs/logs_test.go`：通过。

远程复核补充：运行 `36331061451` 中其余门禁全部通过；Full-stack Compose closure 的管理员日志验收仍在 `waitForLogs` 超时。该运行的 Marshaller 输出明确出现 `invalid_log_payload`，确认上一修复只覆盖了 Monitor 入口，Marshaller 日志转换器和观测 ES 严格 mapping 仍未登记 `instance_id`。

本次继续实际变更文件：

- `marshaller/internal/logs/validation.go`、`marshaller/internal/logs/transform_test.go`：允许并校验副本 `instance_id`。
- `marshaller/internal/elasticsearch/client.go`：将 `instance_id` 加入观测日志索引模板、既有索引 mapping 扩展和 mapping 校验字段。
- `dev/imple/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md`：登记 Marshaller 日志校验文件范围。

本次继续实际执行的检查与结果：

- `gofmt -w internal/logs/validation.go internal/logs/transform_test.go internal/elasticsearch/client.go`（Marshaller）：通过。
- `go test ./internal/logs ./internal/elasticsearch`（Marshaller）：通过。
- `go test ./...`（Marshaller）：通过。
- `go test ./...`（Monitor）：通过。
- `gofmt -w internal/elasticsearch/client_test.go`（Marshaller）：通过。
- `go test ./internal/logs ./internal/elasticsearch`（Marshaller，mapping 测试补充后重跑）：通过。
- `git diff --check`：通过。

远程复核补充：运行 `36331840222` 中 Branch governance、Scripts and Compose 及全部单模块/集成 job 通过；Full-stack Compose closure 的管理员日志验收仍在 `waitForLogs` 超时。日志已由 Marshaller 接收并写入观测 ES，但 Backend 的日志/事件查询和统计仍复用了业务 ES 客户端，查询持续返回空页；该运行未生成 PR。

本次继续实际变更文件：

- `backend/internal/config/config.go`、`backend/internal/config/config_test.go`、`backend/internal/config/runtime_mode_test.go`：增加独立的 `OBSERVABILITY_ELASTICSEARCH_URL` 配置、主机模式默认端口和 container 服务 DNS 校验。
- `backend/internal/platform/elasticsearch.go`、`backend/internal/platform/platform_test.go`：增加用途绑定为 `observability` 的 ES client 构造路径，并保持业务搜索 client 拒绝观测用途。
- `backend/cmd/server/main.go`：业务搜索继续绑定业务 ES；日志、事件、统计和告警查询绑定观测 ES。
- `backend/internal/logquery/logquery.go`、`backend/internal/logquery/logquery_test.go`：允许严格读取持久化的 `instance_id` 私有元数据，同时保持公开日志 DTO 不变。
- `deploy/compose.yaml`、`.env.example`、`deploy/runtime-contracts.json`、`docs/runtime-contracts.md`：补齐 Backend 观测 ES 地址、健康依赖、运行时契约和拓扑文档。
- `scripts/verify-logs.sh`、`scripts/verify-events.sh`：宿主验收向 Backend 显式提供观测 ES 地址。
- `dev/imple/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md`：登记本批实际涉及的 Backend 查询、日志解码和独立验收文件。

本次实际执行的检查与结果：

- `gofmt -w backend/cmd/server/main.go backend/internal/config/config.go backend/internal/config/config_test.go backend/internal/config/runtime_mode_test.go backend/internal/platform/elasticsearch.go backend/internal/platform/platform_test.go backend/internal/logquery/logquery.go backend/internal/logquery/logquery_test.go`：通过。
- `go test -count=1 ./...`（Backend）：通过。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_runtime_contracts.py'`：2 项通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_verify_business.py'`：12 项通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- `bash -n scripts/verify-logs.sh scripts/verify-events.sh`、`bash scripts/verify-logs.sh --self-test`、`bash scripts/verify-events.sh --self-test`：通过。
- `python3 -m json.tool deploy/runtime-contracts.json`、`git diff --check`：通过。

限制与后续：本地未重复 Phase-18-04 固定两轮正式验收；本次提交推送后等待远程完整门禁重新执行并确认 PR 自动化结果。

远程复核补充：运行 `36333259282` 中除 Full-stack Compose acceptance 外的门禁均通过；Full-stack 的 Backend 已对 VictoriaMetrics 停止场景返回两次 `503 metrics_unavailable`，但管理端 Metrics 页面未显示预期不可用提示，自动 PR job 因此跳过。该失败发生在前次双 ES 查询修复之后，属于 Metrics 页面在 Frontend 代理 503 响应和请求并发取消边界的缺陷。

本次继续实际变更文件：

- `admin-frontend/src/views/ObservabilityMetricsView.vue`：按 Metrics 接口的 503 状态兜底显示 VictoriaMetrics 不可用提示；请求错误处理使用请求本地的 AbortController，避免被后续请求覆盖。
- `admin-frontend/src/views/ObservabilityMetricsView.test.ts`：增加代理仅保留 503 状态和请求被替代后的页面提示回归测试。
- `dev/imple/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md`：登记上述管理端文件范围。

本次实际执行的检查与结果：

- `npx vitest run src/views/ObservabilityMetricsView.test.ts`：2 项通过。
- `npm run typecheck`（Admin Frontend）：通过。
- `npm test -- --run`（Admin Frontend）：12 个测试文件、45 项通过。
- `npm run build`（Admin Frontend）：通过。
- `git diff --check`：通过。

限制与后续：本地未重复 Phase-18-04 固定两轮正式验收；本次修复提交推送后等待远程 Full-stack Compose acceptance 和 PR 自动化结果。

## 2026-09-28 PR 门禁复核：管理端指标目录同步

远程运行 `36334287639` 中除 Full-stack Compose acceptance 外的门禁均通过；Full-stack 的 `vm-down` 场景仍未找到 VictoriaMetrics 不可用提示，自动 PR job 因此跳过。保留本地 Compose 环境复现后，Frontend access log 显示页面只请求了 `/observability/metrics/catalog`，没有继续请求 `/observability/metrics`；浏览器页面显示的是通用 catalog 响应失败文案。`componentmetrics/cmd/catalog` 对照确认管理端生成目录缺少 Router/Marshaller 的 8 个固定指标，严格校验在指标查询前拒绝了后端的 105 条目录。

本次实际变更文件：

- `admin-frontend/src/services/componentMetrics.ts`：从当前 `componentmetrics/cmd/catalog` 重新生成组件指标类型和合同，补齐 Router buffer/backpressure 与 Marshaller partition/target 指标。
- `scripts/ci/verify_component_metrics.py`：同步 Router `120`、Marshaller `310` 的当前固定样本预算，使 self-test 与权威目录一致。
- `dev/imple/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md`：登记生成目录和 self-test 文件范围。
- `dev/logs/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md`：记录本次复现、修复和验证。

本次实际执行的检查与结果：

- `bash scripts/verify-compose-observability.sh --keep`：按原始候选复现 `vm-down` 失败，保留 Compose 环境供诊断；未重复 Phase-18-04 固定两轮 runner。
- `go run ./componentmetrics/cmd/catalog`：输出 6 个组件、48 个组件指标族，样本预算为 `713/37/24/112/120/310`。
- `python3 scripts/ci/verify_component_metrics.py --self-test`：修复前因 Router/Marshaller 预算过期失败；修复后通过。
- `npm test -- --run`（Admin Frontend）：12 个测试文件、45 项通过。
- `npm run typecheck`、`npm run build`（Admin Frontend）：通过。
- `docker compose ... build admin-frontend`：镜像构建内置 45 项测试和生产构建均通过。
- 更新本地 admin-frontend 镜像后运行同一 `vm-down` 浏览器流程：目录校验通过，实际发出 Metrics 请求并收到 `503 metrics_unavailable`，页面显示 VictoriaMetrics 不可用提示。
- `git diff --check`：通过。

限制与后续：修复后的正式远程 Compose acceptance 尚未运行；本地只执行了目标故障场景，未把它计为固定两轮正式验收结果。等待本次提交推送后的远程门禁和 PR 自动化结果。

## 2026-09-28 PR 门禁复核：持久化 ES 重启就绪条件

远程运行 `36335849997` 中其余 10 个门禁均通过；Full-stack Compose acceptance 在持久化卷重启的 `compose up --wait` 阶段发现 `search-init` 退出码为 `1`，自动 PR job 因此跳过。远程清理日志当时只输出 Backend、Worker、Indexer、Router、Marshaller 和 Monitor，没有包含 `search-init` 自身日志，无法从该次证据确认初始化器的具体错误文本。

本次实际变更文件：

- `deploy/compose.yaml`：业务 Elasticsearch 健康检查继续等待 `yellow`，并增加等待初始化 shard 和迁移 shard 清零的条件，避免持久化恢复尚未完成就启动 `search-init`。
- `scripts/verify-compose-observability.sh`：失败清理日志加入 `search-init`，保留初始化器的实际错误输出。
- `dev/logs/Phase-18/Phase-18-04-可观测计算层多副本双ES与背压隔离.md`：记录本次远程失败、修复和验证。

本次实际执行的检查与结果：

- `bash scripts/verify-compose-observability.sh --keep`：完整 Compose 验收中 Phase-12 的启动、网络、故障隔离、持久化重启和 Redis Exporter 场景通过；后续 Phase-15 管理闭环在“创建规则”按钮未启用时超时，保留环境用于本次就绪条件复核；未重复 Phase-18-04 固定两轮 runner。
- 在保留的隔离 Compose 环境中，对 `down --remove-orphans` 后的 `up --detach --wait --wait-timeout 420` 执行 3 轮持久化重启：每轮 `search-init` 退出码均为 `0`，日志均为 `search reindex skipped`。
- `docker exec ... curl .../_cluster/health?wait_for_status=yellow&wait_for_no_initializing_shards=true&wait_for_no_relocating_shards=true&timeout=5s`：返回成功，且 `initializing_shards=0`、`relocating_shards=0`。
- `bash scripts/verify-compose.sh --self-test`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config`：通过。
- `git diff --check`：通过。
- 通过 scoped `docker compose ... down --volumes --remove-orphans` 清理保留的隔离项目，并删除本次唯一 acceptance image tags；未执行全局 Docker prune。

限制与后续：新的远程 Full-stack acceptance 尚未运行；本地 Phase-12 相关路径和 3 轮持久化重启已通过，等待本次提交推送后的远程门禁和 PR 自动化结果。
