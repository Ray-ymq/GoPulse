# Phase-18-03：业务计算层多副本与异步闭合实施记录

## 实际完成

- 在 `develop/2.0.3`（基于 `origin/main`）完成 Backend、Business Worker、Search Indexer 双副本 Compose 拓扑；Frontend 使用两个 Backend 的私有 upstream，Monitor 使用显式副本端点列表。
- 增加有界实例身份、HTTP 并发上限、MySQL 连接池与整套副本预算校验；Worker/Indexer 使用带实例身份的唯一 consumer tag；Backend Outbox owner 使用实例身份和进程号；结构化日志携带 `instance_id`。
- 将连接池配置贯通 Backend、Worker、Indexer 和搜索重建路径；迁移容器只接收数据库连接必需项，保留既有 lease、ack/requeue、告警租约语义。
- 增加 Phase 18-03 固定两轮 runner、自测和证据汇总；runner 对 Worker、Indexer、RabbitMQ、Elasticsearch 故障组在同一 token 下先初始化 owner/actor 账号，再执行故障场景。
- 修复 acceptance `worker-seed` 的异步等待：评论提交和点赞请求均确认完成后才退出；将 `worker-verify` 等待窗口调整为 75 秒，覆盖已配置的 30 秒 Outbox/RabbitMQ 发布重试预算。
- 修复 PR 门禁发现的 Compose 环境泄漏：运行进程使用连接池配置锚点，迁移容器保持最小数据库环境；Compose 发布门禁同步为单一 Frontend loopback 入口。
- 更新 Monitor 日志接收 schema：接受公共结构化 logger 发出的有界 `instance_id`，并复用共享身份校验器拒绝非法值。
- 修复 PR #180 暴露的日志身份链路缺口：Marshaller 索引契约校验及旧索引升级接受 `instance_id`，Backend 查询 DTO 保留该字段，管理前端严格 DTO 接受该字段且继续拒绝未知字段；Compose 管理日志场景验证详情展示。
- 更新 runtime contract、运行时文档、README、版本元数据和 Phase 18-03 记录；`VERSION`、`.env.example`、两个前端 package 与两个 lockfile 均为 `2.0.3`。

实际变更文件：

- `.env.example`、`VERSION`、`README.md`、`backend/README.md`。
- `admin-frontend/package.json`、`admin-frontend/package-lock.json`、`frontend/package.json`、`frontend/package-lock.json`、`frontend/e2e/compose-business.spec.ts`。
- `backend/cmd/server/main.go`、`backend/cmd/server/main_test.go`、`backend/internal/config/config.go`、`backend/internal/config/config_test.go`、`backend/internal/config/worker.go`、`backend/internal/config/search_indexer.go`、`backend/internal/platform/mysql.go`、`backend/internal/platform/platform_test.go`、`backend/internal/worker/runtime.go`、`backend/internal/worker/runtime_test.go`。
- `componentmetrics/config.go`、`componentmetrics/logging.go`、`componentmetrics/runtime.go`、`componentmetrics/runtime_test.go`、`monitor/internal/metrics/collector/components.go`。
- `.github/workflows/quality-gates.yml`、`deploy/compose.yaml`、`deploy/docker/frontend/nginx.conf`、`deploy/runtime-contracts.json`、`docs/runtime-contracts.md`。
- `scripts/ci/phase18_business_scale.py`、`scripts/ci/test_phase18_business_scale.py`、`scripts/verify-phase18-business-scale.sh`。
- `scripts/verify-compose-observability.sh`。
- `monitor/internal/logs/logs.go`、`monitor/internal/logs/logs_test.go`。
- PR #180 日志身份修复：`marshaller/internal/logs/validation.go`、`marshaller/internal/logs/transform_test.go`、`marshaller/internal/elasticsearch/client.go`、`marshaller/internal/elasticsearch/client_test.go`、`backend/internal/logquery/logquery.go`、`backend/internal/logquery/logquery_test.go`、`admin-frontend/src/types/observability.ts`、`admin-frontend/src/services/observability.ts`、`admin-frontend/src/services/observability.test.ts`、`frontend/e2e/compose-observability.spec.ts`。
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
- PR #180 首轮 GitHub 门禁：Branch governance 因迁移容器环境白名单缺少四个 MySQL pool key 而失败；Scripts and Compose 因仍要求两个 `127.0.0.1` 宿主绑定而失败。其余已完成的 Backend、Router、Marshaller、Monitor、Redis Exporter、前端和 Integration 检查通过；Full-stack Compose acceptance 当时仍在运行。
- 在候选树首次执行 `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：108 项中 1 项失败，失败项为 `test_compose_environments_and_probe_argv_are_role_minimal`，原因是 pool key 随共享 MySQL anchor 进入迁移容器。
- Compose 环境锚点拆分后再次执行 `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：当前 Phase-18-03 树 100 项全部通过。
- 按 GitHub Scripts and Compose 门禁重放 Compose 配置、单一 loopback 绑定、镜像版本、Kafka/VictoriaMetrics 镜像、迁移依赖和内部网络断言：通过。
- `bash -n scripts/verify-compose.sh scripts/verify-compose-observability.sh`：通过；`git diff --check`：通过。
- 在 `monitor/` 执行 `gofmt -w internal/logs/logs.go internal/logs/logs_test.go` 与 `go test -count=1 ./internal/logs ./internal/httpserver`：通过。
- `git diff --check`：通过。
- 范围清单先在 `update` 修订并由 PR #181 以 merge commit 合入 `main`；随后把更新合入 `develop/2.0.3`，才修改已登记的 `.github/workflows/quality-gates.yml`。
- `(cd backend && go test -count=1 ./internal/logquery)`：通过；`(cd marshaller && go test -count=1 ./internal/logs ./internal/elasticsearch)`：通过。
- 在修正 Marshaller `instance_id` 索引契约及旧索引映射后，`(cd marshaller && go test -count=1 ./internal/elasticsearch)`：通过；`git diff --check`：通过。
- 在管理前端登记范围后，`(cd admin-frontend && npm test -- --run src/services/observability.test.ts)`：8 项通过；`git diff --check`：通过。
- PR #180 Actions run `36320699044`：所有其他门禁通过；Full-stack Compose acceptance 的 admin `waitForLogs` 45 秒后仍为 0 条。根因是 strict index verification 的字段集合漏了 `instance_id`；template 已有该 mapping，导致字段计数与索引契约不一致并触发存储重试。
- PR #180 Actions run `36321387651`：加入 ES 索引契约字段后，Full-stack Compose 仍在 admin `waitForLogs` 失败；Backend 日志查询已返回非空记录（响应体 15,884 字节），管理前端 `LogEntry` 严格白名单未接受新增的 `instance_id`，因此未渲染记录。
- PR #180 Actions run `36322235076`：Branch governance、Backend、Router、Marshaller、Monitor、Redis Exporter、两个前端、Scripts and Compose、Integration 和 Full-stack Compose acceptance 全部通过；PR #180 于 2026-09-27 合入，merge commit `ec481f1dbf8a91e411e3cd53240468550b0cf00e`。
- `scripts/verify-phase18-business-scale.sh --repetitions 2` 在 `7b54ef3` 上的本地后续尝试未产生 summary；不作为通过或失败证据。对其遗留的专属 Compose 项目执行 `docker compose --project-name gopulse-p1803-5100853359f1-r2 --env-file .run/phase18-business-scale-2.0.3-7b54ef3d6560-cf541f2adafc/run-2/U3/compose.env --file deploy/compose.yaml down --volumes --remove-orphans`，退出码 `0`。
- 前端 DTO 清单遗漏通过 `update` 的 PR #188 修订并先于代码变更合入 `main`。

为遵守候选证据不可复用规则，正式验收在每次相关 runner/acceptance 修订后创建新候选；每个候选均只调用一次固定命令 `scripts/verify-phase18-business-scale.sh --repetitions 2`，没有执行第三轮：

1. `b35220a0374d418fa0c357a8527263c28a87857b`：`.run/phase18-business-scale-2.0.3-b35220a0374d-b294dab761a5/`，`boundary_found`；U1/U2/U4 为 `2/2`，U3 为 `0/2`。Worker 故障场景使用新 token 但未初始化 owner/actor 账号，浏览器停留在 `/login`。
2. `4afa609f70af74136ce4cc03ea3f57c82cb244e4`：`.run/phase18-business-scale-2.0.3-4afa609f70af-b43a52a67ada/`，`boundary_found`；四个故障组的账号初始化均通过，Worker/Indexer 故障恢复也通过，但 RabbitMQ 恢复场景两轮均收到 3 条而期望 4 条通知。
3. `e07f65d28a8bd5e1356c6b6c29e34e59755e8ae4`：`.run/phase18-business-scale-2.0.3-e07f65d28a8b-05eb189a8b14/`，`boundary_found`；run-1 U3 `target_met`，run-2 在 RabbitMQ 恢复场景因 30 秒等待窗口收到 3 条而超时；run-1 清理退出码为 `0`。
4. 最终候选 `74e00417f9d4ceb141d9df3dd0f30651352fdf23`：`.run/phase18-business-scale-2.0.3-74e00417f9d4-112e3b2e1e84/`，固定两轮均 `target_met`。
5. PR 门禁修正候选 `8316922895c4ff31aa733b6047afd5badb49d76b`：`.run/phase18-business-scale-2.0.3-8316922895c4-d5e751e5da45/`，固定两轮均 `target_met`；U1～U4 均为 `2/2`。binding 固定 `VERSION=2.0.3`、Compose digest `sha256:b63e4fb77b7c2800ed00e6dc3ec542fb525e4fe5d52c171f531cf7f064399f60` 与 runtime contract digest `sha256:9d1de93ce883c7309bbd8af7d502abcbb8710958e6eb71e1b35d7bb58acae20f`。

## 候选 4（74e0041）正式两轮结果

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

## PR 门禁修正候选（8316922）正式两轮结果

| 单元 | run-1 | run-2 | 结果 |
| --- | --- | --- | --- |
| U1 | 通过 | 通过 | `2/2` |
| U2 | 通过 | 通过 | `2/2` |
| U3 | `target_met` | `target_met` | `2/2` |
| U4 | 通过 | 通过 | `2/2` |

两轮均完成 Backend、Worker、Indexer 双副本健康检查、正常业务、三类计算副本故障、RabbitMQ 与搜索 Elasticsearch 短故障、恢复和最终闭合；Compose 清理退出码均为 `0`。闭合快照两轮一致：`outbox_pending_or_leased=0`、`notifications=16`、`search_alias=true`，MySQL/RabbitMQ/搜索闭合命令均退出 `0`。

该候选 U3 场景耗时（秒，`run-1 / run-2 / 平均`）：

| 场景 | run-1 | run-2 | 平均 |
| --- | ---: | ---: | ---: |
| normal | 5.421 | 4.829 | 5.125 |
| backend_failover | 5.043 | 4.770 | 4.906 |
| backend_failover_stop/start | 8.403 / 2.913 | 5.468 / 2.954 | 6.936 / 2.933 |
| worker_failover_account_init | 4.805 | 4.484 | 4.644 |
| worker_failover_stop/start | 10.369 / 1.859 | 10.366 / 1.869 | 10.367 / 1.864 |
| worker_failover | 2.793 | 2.730 | 2.761 |
| worker_recovery | 2.685 | 2.659 | 2.672 |
| indexer_failover_account_init | 4.713 | 4.193 | 4.453 |
| indexer_failover_stop/start | 10.341 / 2.670 | 10.324 / 2.712 | 10.332 / 2.691 |
| indexer_failover | 2.360 | 2.384 | 2.372 |
| indexer_recovery | 2.760 | 2.758 | 2.759 |
| rabbit_fault_seed_account_init | 4.707 | 4.646 | 4.676 |
| rabbit_fault_seed_stop/start | 1.471 / 0.430 | 1.464 / 0.438 | 1.468 / 0.434 |
| rabbit_fault_seed | 2.785 | 2.792 | 2.788 |
| rabbit_recovery | 23.132 | 24.464 | 23.798 |
| elasticsearch_fault_seed_account_init | 4.898 | 4.288 | 4.593 |
| elasticsearch_fault_seed_stop/start | 2.612 / 0.512 | 2.431 / 0.476 | 2.522 / 0.494 |
| elasticsearch_fault_seed | 2.341 | 2.488 | 2.415 |
| elasticsearch_recovery | 15.298 | 20.581 | 17.939 |

## 偏差、限制与后续项

- 初始候选的 U3 边界由验收账号初始化缺失触发；修复后又发现 acceptance 写入未等待和 RabbitMQ 30 秒重试窗口未被等待条件覆盖。每次修订均创建了新候选并重新完成固定两轮，未修改或复用旧 evidence。
- 首轮 PR 检查发现共享数据库环境锚点给迁移容器传递了其不使用的连接池预算，且通用 Compose 门禁仍按旧的双宿主端口拓扑断言。先在 `update` 补登记 `.github/workflows/quality-gates.yml` 和单一入口条件，再拆分 Compose pool anchor、把门禁计数改为一个；随后冻结新 revision 并重新执行唯一的两轮候选验收。
- PR #180 的 Actions run `36315775113` 中，Scripts and Compose 通过而 Full-stack Compose acceptance 失败，原因为仍断言 Backend 必须映射宿主 loopback。追踪调用后确认 `scripts/verify-compose.sh --full` 将执行委托给 `scripts/verify-compose-observability.sh`；先前对 wrapper 分支的试改已恢复，随后在 `update` 的 PR #185 登记实际委托脚本，并只在该全栈断言中保留 Frontend loopback、拒绝 Backend 宿主端口。修正后对两个脚本执行 Bash 语法检查和差异空白检查，均通过。
- Actions run `36316456345` 的端口拓扑、Monitor 初始化、安全边界、业务迁移及 Worker/Indexer 恢复均通过；唯一失败为 admin E2E 的 `waitForLogs` 在 45 秒内未读到记录。容器日志显示 Backend/Worker/Indexer 发送的新结构化日志均收到 `permanent_rejection`；Monitor `allowedFields` 不含公共 logger 新增的 `instance_id`。该实际 schema 不兼容已在 `update` 的 PR #186 预先登记并修复；`monitor/internal/logs` 与 `monitor/internal/httpserver` 定向测试通过。
- PR 门禁修复在 `8316922` 正式两轮候选后继续进行；该候选的 U1～U4 evidence 仍只绑定 `8316922895c4ff31aa733b6047afd5badb49d76b`。其后的 PR 修复由定向包测试与最终 GitHub Full-stack Compose acceptance 验证；`7b54ef3` 上未完成的本地 Phase 18-03 runner 没有 summary，未发布或复用其 evidence，也没有把该旧候选矩阵结果表述为对 `b8cadaf` 的新矩阵结果。
- Phase 18-03 清单外的 `componentmetrics/logging.go`、`backend/internal/config/worker.go`、`backend/internal/config/search_indexer.go` 已在 `update` 的 `9693fe3` 登记；`frontend/e2e/compose-business.spec.ts` 已在 `update` 的 `461da86` 登记后才修改。
- 本批固定验收矩阵在候选 `8316922895c4ff31aa733b6047afd5badb49d76b` 的结果为 `target_met`，该结论只绑定该 revision 与其 evidence；`VERSION=2.0.3` 表示本批版本元数据已同步，不扩大为对未覆盖生产环境的泛化保证。
