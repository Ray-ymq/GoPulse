# Phase-19-01 实施记录

## 已完成工作

- 按总实施方案获取 `upstream/main`，从 `upstream/main` 创建 `develop/2.1.1`，目标版本为 `2.1.1`。
- 将 Backend 并发准入移动到 `/api/v1` Gin 路由边界；`/startup`、`/live`、`/ready`、`/health` 不占用业务槽位，仍保留依赖、启动和停止语义；饱和 API 返回 `503 backend_busy`。
- 在 Backend 共享指标中保留旧请求数和 duration total，并增加固定累计桶、`_count`、`_sum`、当前并发、并发上限和拒绝计数；请求状态以单一不可变原子快照发布。
- 扩展共享 catalog、严格分布校验、Monitor 解析、Monitor Envelope、Marshaller 第二次校验/转换、Backend 固定查询目录和管理端生成合同。
- Compose 两个 Backend healthcheck 改为容器内直接访问 `127.0.0.1:8080/ready` 的私有探针。
- 同步 `2.1.1` 版本元数据、runtime contract、README、能力状态和组件指标文档。

固定桶及名称是本计划未逐项指定时采用的实现约定：桶为 `0.005`、`0.01`、`0.025`、`0.05`、`0.1`、`0.25`、`0.5`、`1`、`2`、`5`、`10`、`+Inf` 秒；分布族为 `gopulse_backend_http_request_duration_seconds_{bucket,count,sum}`；容量族为 `gopulse_backend_http_requests_in_flight`、`gopulse_backend_http_concurrency_limit`、`gopulse_backend_http_rejected_total`。

## 实际变更文件

- `.env.example`、`README.md`、`VERSION`。
- `admin-frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/src/services/componentMetrics.ts`、`admin-frontend/src/services/management.test.ts`、`admin-frontend/src/services/management.ts`、`admin-frontend/src/views/ObservabilityMetricsView.vue`。
- `backend/cmd/server/main.go`、`backend/cmd/server/main_test.go`、`backend/internal/http/router.go`、`backend/internal/http/router_test.go`、`backend/internal/metricquery/components_test.go`、`backend/internal/metricquery/metricquery.go`。
- `componentmetrics/backend.go`、`componentmetrics/backend_test.go`、`componentmetrics/catalog.go`、`componentmetrics/cmd/catalog/main.go`、`componentmetrics/registry.go`、`componentmetrics/registry_test.go`、`componentmetrics/validation.go`。
- `deploy/compose.yaml`、`deploy/runtime-contracts.json`、`deploy/runtime-contracts.schema.json`。
- `docs/capability-status.md`、`docs/component-metrics.md`、`docs/runtime-contracts.md`。
- `frontend/e2e/phase15-closure.spec.ts`、`frontend/package-lock.json`、`frontend/package.json`。
- `marshaller/internal/envelope/components_test.go`、`marshaller/internal/envelope/envelope.go`、`marshaller/internal/metrics/transform_test.go`。
- `monitor/internal/metrics/collector/components_test.go`、`monitor/internal/metrics/envelope/envelope.go`。
- `scripts/ci/verify_component_metrics.py`、`scripts/verify-plugin-metrics.sh`。

## 已执行命令及结果

- `git fetch upstream`：通过；基线为 `upstream/main=ff8823d2409bc2e810b6c7905960e4d98c48b50e`。
- `go -C backend test -count=1 ./cmd/server ./internal/http ./internal/metricquery`：通过。
- `go -C componentmetrics test -count=1 ./...`、`go -C componentmetrics test -race -count=1 ./...`：通过。
- `go -C monitor test -count=1 ./internal/metrics/collector ./internal/metrics/envelope`：通过。
- `go -C marshaller test -count=1 ./internal/envelope ./internal/metrics`：通过。
- `npm run typecheck` 和 `npm test -- --run`：Frontend 通过（18 个测试文件、67 个测试）；Admin Frontend 通过（12 个测试文件、47 个测试）。
- `python3 scripts/ci/verify_component_metrics.py --self-test`、`scripts/verify-component-metrics.sh --self-test`、`scripts/verify-plugin-metrics.sh --self-test`：通过，无 Docker 访问。
- 无参数 `python3 scripts/ci/verify_component_metrics.py`、无参数 `scripts/verify-plugin-metrics.sh`：通过，包含生产多副本合同、真实请求、指标恢复和存储恢复检查。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.1.1`：通过。
- `python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/2.1.1 --base-ref upstream/main`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- `scripts/verify-compose.sh`：最终候选完整 Compose 验收通过；包含构建、拓扑/隔离、迁移幂等、业务与可观测性浏览器场景、Redis/Worker/Indexer/VictoriaMetrics/Monitor/Router 故障恢复、Kafka 替换及持久化、Redis Exporter 矩阵和 Phase 15 closure。
- `git diff --check`：通过。

## 偏差、失败原因与修复

- 首次完整 Compose 验收在 `frontend/e2e/phase15-closure.spec.ts` 失败：点击“创建规则”时按钮仍处于 disabled，第一次等待 30 秒超时；加入异步目录加载等待后，第二次仍在 90 秒后保持 disabled。
- 根因是新增延迟桶指标使用 Prometheus 标准 `le` 标签，而 `admin-frontend/src/services/management.ts` 的严格 catalog 校验白名单遗漏了 `le`。后端 catalog 返回 200，但管理端将其判为无效，`AlertsView` 的 catalog 保持为空，故按钮不会启用。
- 在 `management.ts` 加入 `le`，并增加对应管理端单元测试；最终完整 Compose 验收通过。
- 生成的组件指标合同最初使用默认单副本标签，生产多副本验收不匹配；改为从生产 endpoint inventory 生成合同，并让恢复检查覆盖实际副本请求。
- Phase 14 组件验收缺少已有的 `.series-card` 测试钩子；在现有指标表行补齐该 class。
- 多副本 Redis 检查和存储恢复检查曾分别受固定副本值假设、旧投影消息已被消费影响；改为检查任一有效副本，并用新建 post/comment 触发新的恢复投影。
- 一次完整组件验收因 `reconcile_plugin_accounts.py` 的 MySQL/RabbitMQ 验收基础设施瞬时失败，原实现未改动，重跑后通过。
- 上述兼容性修改属于用户明确授权的直接实现调整，均已纳入本批次文件和测试范围。

## 限制和后续项

- 本批次提供诊断、容量和尾延迟指标，不据此宣称已完成容量规划或 SLO 目标设定。
- `http_rejected_total` 已由单元、路由和完整 Compose 指标链路验证；后续若调整固定桶、catalog 或生产副本 inventory，需要同步更新 runtime contract、生成合同和对应验收证据。
