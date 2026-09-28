# Phase-18-05 实施记录

## 完成内容

- 在 `develop/2.0.5` 上从 `origin/main` 的最新提交创建候选，冻结版本 `2.0.5`。
- 扩展 `deploy/runtime-contracts.json` 与 Schema，固化 12 个长运行进程的角色、Compose 服务、副本身份、独立私有探针、监听器和连接/队列/in-flight/关停预算。
- 为 Compose 计算层服务增加机器合同标签和私有诊断监听器声明；没有新增宿主端口。
- 增加代码进程目录输出、统一身份环境常量、合同漂移/负例验证和合同 wrapper。
- 增加 Phase-18-05 固定两轮 runner、不可覆盖的命令回执、文件账本、候选绑定、证据验证器及自测。
- 将六处产品版本元数据更新为 `2.0.5`，补充运行时合同和 Phase-18-05 验收语义文档。
- 正式 runner 只调用了一次，生成且仅生成 `run-1`、`run-2`；没有启动第三轮。

## 候选绑定

- 分支：`develop/2.0.5`
- 候选提交：`3884aecce7dc57b053ce4ccc249d872ffc383ef4`
- 运行时合同摘要：`sha256:a0780c448a6f5147a5a9ba020144d68fe8b3bf57f5d058b68c52b96430ded9fd`
- 证据目录：`.run/phase18-scale-closure-2.0.5-3884aecce7dc-bcee7f366ec0`
- 候选绑定要求：直接私有探针、四个探针路径、两次固定矩阵、Kafka 四分区、无新增宿主端口。

## 正式矩阵结果

入口 `scripts/verify-phase18-scale-closure.sh --repetitions 2` 返回退出码 `0`，摘要结果为
`boundary_found`。两次运行均为 `boundary_found`，确定性计数如下：

| 单元 | 结果 |
| --- | --- |
| U1 合同 verifier 与负例 | `2/2` |
| U2 runner/evidence 自测 | `2/2` |
| U3 完整扩缩容与故障矩阵 | `0/2` |
| U4 版本、分支、diff-check | `2/2` |

U3 两次项目清理的退出码均为 `0`。保留的数值原值和算术平均为：

| 数值 | run-1 | run-2 | 平均 |
| --- | ---: | ---: | ---: |
| `diagnostic_checked` | 28 | 28 | 28 |
| `diagnostic_total` | 102 | 102 | 102 |
| `elapsed_seconds` | 476.908 | 114.744 | 295.826 |

两轮均完成了正常业务场景、业务/观测扩缩、RabbitMQ/Kafka/两个 Elasticsearch/VictoriaMetrics
故障、单实例 SIGTERM、服务重建和终态命令；部分场景的验收断言未通过，因此没有将 U3 标为
`target_met`。正常业务验收退出码为 `0`；正常观测及五个故障场景的验收命令退出码为 `1`，
相关 stop/start 命令和依赖健康恢复命令均保留在各自回执中。单实例 SIGTERM 的 backend-2
健康恢复等待超时，随后仍完成了重建、终态检查和清理。

## 实际变更文件

候选提交实际变更了：

`.env.example`、`VERSION`、`admin-frontend/package-lock.json`、`admin-frontend/package.json`、
`componentmetrics/catalog.go`、`componentmetrics/cmd/catalog/main.go`、`componentmetrics/config.go`、
`deploy/compose.yaml`、`deploy/runtime-contracts.json`、`deploy/runtime-contracts.schema.json`、
`docs/runtime-contracts.md`、`frontend/package-lock.json`、`frontend/package.json`、
`scripts/ci/phase18_scale_closure.py`、`scripts/ci/phase18_scale_evidence.py`、
`scripts/ci/test_phase18_scale_closure.py`、`scripts/ci/test_phase18_scale_evidence.py`、
`scripts/ci/test_runtime_contracts.py`、`scripts/ci/verify_runtime_contracts.py`、
`scripts/verify-phase18-evidence.py`、`scripts/verify-phase18-scale-closure.sh`、
`scripts/verify-runtime-contracts.sh`。

正式结果确定后更新了 `README.md`、`dev/phases/Plan.md`、`dev/phases/README.md`、
`dev/phases/Phase-18-高并发与可观测架构收敛.md`，并创建本记录文件。

## 实际执行命令与结果

- `git fetch origin`：成功；候选从最新 `origin/main` 创建。
- `python3 scripts/ci/verify_runtime_contracts.py ... --candidate 2.0.5`：通过，输出 12 个进程和合同摘要。
- `python3 -m unittest discover -s scripts/ci -p test_runtime_contracts.py`：通过，2 个测试。
- `python3 -m unittest discover -s scripts/ci -p test_phase18_scale_closure.py`：通过，8 个测试。
- `python3 -m unittest discover -s scripts/ci -p test_phase18_scale_evidence.py`：通过，6 个测试。
- `python3 -m unittest discover -s scripts/ci -p 'test_phase18*.py'`：通过，66 个测试。
- `go test ./...`（`componentmetrics`）：通过。
- `go run ./cmd/catalog --processes`：通过，输出 12 个确定性进程 ID。
- `python3 scripts/ci/validate_versions.py`：通过。
- `docker compose --env-file .env.example -f deploy/compose.yaml config --quiet`：通过。
- `python3 -m py_compile ...`、`git diff --check`：通过。
- `scripts/verify-phase18-scale-closure.sh --repetitions 2`：退出码 `0`，结果 `boundary_found`，两轮证据完整。
- `PYTHONPATH=scripts/ci python3 scripts/verify-phase18-evidence.py --closure .run/phase18-scale-closure-2.0.5-3884aecce7dc-bcee7f366ec0`：通过。
- 修正 evidence wrapper 的仓库根目录模块路径后，`python3 scripts/verify-phase18-evidence.py --closure ...`：通过，同一证据仍为 `boundary_found`。

## 偏差与已知限制

- 最初从仓库根目录直接运行 Python 自测时得到模块导入错误；按既有 `scripts/ci` discovery 入口重跑通过。一次附加的 `--schema` 参数不被合同 verifier 接受，随后按其公开参数重跑通过；仓库中不存在尝试调用的 `scripts/ci/verify-diff-scope.py`，未将其伪装为通过。
- 正式 runner 的直接容量诊断对六类自研组件指标监听器请求了 `/metrics`，而现有组件指标合同实际路径为 `/internal/v1/metrics`；回执保留了 backend、router、marshaller、monitor 共 7 个实例的 404 边界。五个由 Monitor 管理且没有独立 Compose 服务的 Exporter 记录为 `not_started`。因此本批不宣称这些容量信号达标。
- evidence wrapper 的导入路径修正在正式矩阵后完成；未修改任何既有回执，也未追加矩阵运行。若要校正直接指标路径，必须创建新的冻结候选并重新执行两轮，不能把本次证据改写成通过。
- 本结果不声明 `150 RPS` 稳定容量、状态层 HA、生产 SLO 或 Kubernetes 支持；这些属于后续另行规划的输入。
