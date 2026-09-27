# Phase-18-03：业务计算层多副本与异步闭合实施记录

## 实际完成

- 在 `develop/2.0.3`（基于 `origin/main`）完成 Backend、Business Worker、Search Indexer 双副本 Compose 拓扑；Frontend 使用两个 Backend 的私有 upstream，Monitor 使用显式副本端点列表。
- 增加有界实例身份、HTTP 并发上限、MySQL 连接池与整套副本预算校验；Worker/Indexer 使用带实例身份的唯一 consumer tag；Backend Outbox owner 使用实例身份和进程号；结构化日志携带 `instance_id`。
- 将连接池配置贯通 Backend、Worker、Indexer、迁移和搜索重建路径，并保留既有 lease、ack/requeue、告警租约语义。
- 增加 Phase 18-03 固定两轮 runner、自测和证据汇总；runner 对 Worker、Indexer、RabbitMQ、Elasticsearch 故障组在同一 token 下先初始化 owner/actor 账号，再执行故障场景。
- 修复 acceptance `worker-seed` 的异步等待：评论提交和点赞请求均确认完成后才退出；将 `worker-verify` 等待窗口调整为 75 秒，覆盖已配置的 30 秒 Outbox/RabbitMQ 发布重试预算。
- 更新 runtime contract、运行时文档、README、版本元数据和 Phase 18-03 记录；`VERSION`、`.env.example`、两个前端 package 与两个 lockfile 均为 `2.0.3`。

实际变更文件：

- `.env.example`、`VERSION`、`README.md`、`backend/README.md`。
- `admin-frontend/package.json`、`admin-frontend/package-lock.json`、`frontend/package.json`、`frontend/package-lock.json`、`frontend/e2e/compose-business.spec.ts`。
- `backend/cmd/server/main.go`、`backend/cmd/server/main_test.go`、`backend/internal/config/config.go`、`backend/internal/config/config_test.go`、`backend/internal/config/worker.go`、`backend/internal/config/search_indexer.go`、`backend/internal/platform/mysql.go`、`backend/internal/platform/platform_test.go`、`backend/internal/worker/runtime.go`、`backend/internal/worker/runtime_test.go`。
- `componentmetrics/config.go`、`componentmetrics/logging.go`、`componentmetrics/runtime.go`、`componentmetrics/runtime_test.go`、`monitor/internal/metrics/collector/components.go`。
- `deploy/compose.yaml`、`deploy/docker/frontend/nginx.conf`、`deploy/runtime-contracts.json`、`docs/runtime-contracts.md`。
- `scripts/ci/phase18_business_scale.py`、`scripts/ci/test_phase18_business_scale.py`、`scripts/verify-phase18-business-scale.sh`。
- 本实施记录文件。

## 实际执行的检查与结果

- `git fetch origin --prune`：成功；确认 `origin/main` 为候选基线并创建 `develop/2.0.3`。
- Backend 直接测试：`go test -count=1 ./internal/config ./internal/platform ./internal/outbox ./internal/alert ./internal/worker ./cmd/server`：通过。
- Componentmetrics：`go test -count=1 ./...`：通过。
- Monitor collector：`go test -count=1 ./internal/metrics/collector`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_phase18_business_scale.py'`：初始 runner 5 项通过；账号初始化修复后 6 项通过。
- `python3 -m py_compile scripts/ci/phase18_business_scale.py`：通过。
- `python3 scripts/ci/sync_version_metadata.py --version 2.0.3`：成功；`python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example`：通过。
- `npm test`：前端 67/67 通过；`npm run typecheck`：通过；两次 acceptance 修复后均执行过。
- `gofmt`、`git diff --check`：通过。
- `scripts/verify-phase18-business-scale.sh --repetitions 3`：按固定次数规则以退出码 `2` 拒绝。

为遵守候选证据不可复用规则，正式验收在每次相关 runner/acceptance 修订后创建新候选；每个候选均只调用一次固定命令 `scripts/verify-phase18-business-scale.sh --repetitions 2`，没有执行第三轮：

1. `b35220a0374d418fa0c357a8527263c28a87857b`：`.run/phase18-business-scale-2.0.3-b35220a0374d-b294dab761a5/`，`boundary_found`；U1/U2/U4 为 `2/2`，U3 为 `0/2`。Worker 故障场景使用新 token 但未初始化 owner/actor 账号，浏览器停留在 `/login`。
2. `4afa609f70af74136ce4cc03ea3f57c82cb244e4`：`.run/phase18-business-scale-2.0.3-4afa609f70af-b43a52a67ada/`，`boundary_found`；四个故障组的账号初始化均通过，Worker/Indexer 故障恢复也通过，但 RabbitMQ 恢复场景两轮均收到 3 条而期望 4 条通知。
3. `e07f65d28a8bd5e1356c6b6c29e34e59755e8ae4`：`.run/phase18-business-scale-2.0.3-e07f65d28a8b-05eb189a8b14/`，`boundary_found`；run-1 U3 `target_met`，run-2 在 RabbitMQ 恢复场景因 30 秒等待窗口收到 3 条而超时；run-1 清理退出码为 `0`。
4. 最终候选 `74e00417f9d4ceb141d9df3dd0f30651352fdf23`：`.run/phase18-business-scale-2.0.3-74e00417f9d4-112e3b2e1e84/`，固定两轮均 `target_met`。

## 最终候选正式两轮结果

| 单元 | run-1 | run-2 | 结果 |
| --- | --- | --- | --- |
| U1 | 通过 | 通过 | `2/2` |
| U2 | 通过 | 通过 | `2/2` |
| U3 | `target_met` | `target_met` | `2/2` |
| U4 | 通过 | 通过 | `2/2` |

两轮均完成六个业务副本健康检查、正常业务、Backend 故障、Worker 故障、Indexer 故障、RabbitMQ 短故障、Elasticsearch 短故障及恢复；两轮 Compose 清理退出码均为 `0`。最终闭合快照两轮一致：`outbox_pending_or_leased=0`、`notifications=16`、`search_alias=true`、RabbitMQ 队列快照命令退出码为 `0`。

最终 U3 场景耗时（秒，`run-1 / run-2 / 平均`）：

| 场景 | run-1 | run-2 | 平均 |
| --- | ---: | ---: | ---: |
| normal | 4.868 | 4.960 | 4.914 |
| backend_failover | 5.077 | 4.959 | 5.018 |
| backend_failover_stop/start | 5.486 / 2.922 | 5.525 / 2.896 | 5.505 / 2.909 |
| worker_failover_account_init | 4.447 | 4.481 | 4.464 |
| worker_failover_stop/start | 10.330 / 1.839 | 10.344 / 1.854 | 10.337 / 1.847 |
| worker_failover | 2.841 | 2.779 | 2.810 |
| worker_recovery | 2.770 | 2.638 | 2.704 |
| indexer_failover_account_init | 4.273 | 4.181 | 4.227 |
| indexer_failover_stop/start | 10.363 / 2.681 | 10.327 / 2.704 | 10.345 / 2.692 |
| indexer_failover | 2.419 | 2.369 | 2.394 |
| indexer_recovery | 2.705 | 2.705 | 2.705 |
| rabbit_fault_seed_account_init | 4.230 | 4.136 | 4.183 |
| rabbit_fault_seed_stop/start | 1.455 / 0.455 | 1.454 / 0.424 | 1.454 / 0.440 |
| rabbit_fault_seed | 2.689 | 2.749 | 2.719 |
| rabbit_recovery | 39.262 | 51.237 | 45.249 |
| elasticsearch_fault_seed_account_init | 4.359 | 4.367 | 4.363 |
| elasticsearch_fault_seed_stop/start | 2.922 / 0.463 | 2.843 / 0.470 | 2.883 / 0.467 |
| elasticsearch_fault_seed | 2.279 | 2.417 | 2.348 |
| elasticsearch_recovery | 15.453 | 15.194 | 15.323 |

## 偏差、限制与后续项

- 初始候选的 U3 边界由验收账号初始化缺失触发；修复后又发现 acceptance 写入未等待和 RabbitMQ 30 秒重试窗口未被等待条件覆盖。每次修订均创建了新候选并重新完成固定两轮，未修改或复用旧 evidence。
- Phase 18-03 清单外的 `componentmetrics/logging.go`、`backend/internal/config/worker.go`、`backend/internal/config/search_indexer.go` 已在 `update` 的 `9693fe3` 登记；`frontend/e2e/compose-business.spec.ts` 已在 `update` 的 `461da86` 登记后才修改。
- 最终结果是本批固定验收矩阵的 `target_met`；`VERSION=2.0.3` 表示本批版本元数据已同步，不扩大为对未覆盖生产环境的泛化保证。
