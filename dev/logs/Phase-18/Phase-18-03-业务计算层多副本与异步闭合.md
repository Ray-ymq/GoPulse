# Phase-18-03：业务计算层多副本与异步闭合实施记录

## 实际完成

- 在 `develop/2.0.3`（基于 `origin/main`）完成 Backend、Business Worker、Search Indexer 的双副本 Compose 拓扑；Frontend 使用两个 Backend 的私有 upstream，Monitor 使用显式副本端点列表。
- 增加有界实例身份、HTTP 并发上限、MySQL 连接池与整套副本预算校验；Worker/Indexer 使用带实例身份的唯一 consumer tag；Backend Outbox owner 使用实例身份和进程号；结构化日志携带 `instance_id`。
- 将连接池配置贯通 Backend、Worker、Indexer、迁移和搜索重建路径，并保留既有 lease、ack/requeue、告警租约语义。
- 更新 runtime contract、运行时文档、README、版本元数据和 Phase 18-03 runner；`VERSION`、`.env.example`、两个前端 package 与两个 lockfile 均为 `2.0.3`。
- 在 `update` 先登记清单外但实际需要的 `componentmetrics/logging.go`、`backend/internal/config/worker.go`、`backend/internal/config/search_indexer.go`，提交 `9693fe3`；实现提交为 `b35220a`（`feat(phase18): close business replica runtime`）。

## 实际执行的检查与结果

- `git fetch origin --prune`：成功；确认 `origin/main` 后创建 `develop/2.0.3`。
- Backend 直接测试：`go test -count=1 ./internal/config ./internal/platform ./internal/outbox ./internal/alert ./internal/worker ./cmd/server`：通过。
- Componentmetrics：`go test -count=1 ./...`：通过。
- Monitor collector：`go test -count=1 ./internal/metrics/collector`：通过。
- Runner self-test：`python3 -m unittest discover -s scripts/ci -p test_phase18_business_scale.py`：5 个测试通过。
- `scripts/verify-phase18-business-scale.sh --repetitions 3`：按固定次数规则以退出码 `2` 拒绝。
- `python3 scripts/ci/sync_version_metadata.py --version 2.0.3`：同步六处版本元数据成功；`python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example`：通过。
- `gofmt`、`python3 -m py_compile scripts/ci/phase18_business_scale.py scripts/ci/test_phase18_business_scale.py`、`git diff --check`：通过。
- 正式 runner 仅调用一次：`scripts/verify-phase18-business-scale.sh --repetitions 2`。证据目录为 `.run/phase18-business-scale-2.0.3-b35220a0374d-b294dab761a5/`，候选 revision 为 `b35220a0374d418fa0c357a8527263c28a87857b`。

## 正式两轮结果

| 单元 | run-1 | run-2 | 结果 |
| --- | --- | --- | --- |
| U1 | 通过 | 通过 | `2/2` |
| U2 | 通过 | 通过 | `2/2` |
| U3 | `boundary_found` | `boundary_found` | `0/2` |
| U4 | 通过 | 通过 | `2/2` |

两轮均完成了新 Compose 项目构建、六个业务副本健康检查、正常业务场景、停一个 Backend 业务场景以及 Backend 副本恢复；两轮项目清理退出码均为 `0`。已记录的 U3 计时平均值包括：正常场景 `5.139s`、Backend failover `5.043s`、Backend 停止/恢复分别为 `8.955s`/`2.944s`。

正式汇总结果是 `boundary_found`，未执行第三轮。U3 在 Worker failover 的 acceptance seed 阶段停止：该场景使用了新的 token，却没有先注册对应 owner/actor 账号，浏览器登录停留在 `/login`。这是验收 harness 的账号准备边界，不足以证明 Worker 多副本故障恢复失败；因此 RabbitMQ、Elasticsearch 短故障和最终 Outbox/Rabbit/通知/搜索零积压闭合未在本次正式两轮中到达。

## 偏差、限制与后续项

- 结果按计划如实归类为 `boundary_found`；不能据此宣称 Phase-18-03 多副本业务能力已经完整通过。
- 后续应在 acceptance runner 中为每个 Worker/Indexer/RabbitMQ/Elasticsearch 故障子场景先完成同一 token 的业务账号准备，或安全复用已注册的账号；修订后必须创建新的候选 revision，并按批次规则重新执行完整固定两轮验收，不能修改或复用本次 evidence。
- 本次版本仍按 Phase-18-03 完成条件同步为 `2.0.3`；版本更新不代表多副本能力通过。
