# Phase-18-05 实施记录

## 完成内容

- 在 `develop/2.0.5` 上从 `origin/main` 的最新提交创建候选，冻结版本 `2.0.5`。
- 扩展 `deploy/runtime-contracts.json` 与 Schema，固化 12 个长运行进程的角色、Compose 服务、副本身份、独立私有探针、监听器和连接/队列/in-flight/关停预算。
- 为 Compose 计算层服务增加机器合同标签和私有诊断监听器声明；没有新增宿主端口。
- 增加代码进程目录输出、统一身份环境常量、合同漂移/负例验证和合同 wrapper。
- 增加 Phase-18-05 固定两轮 runner、不可覆盖的命令回执、文件账本、候选绑定、证据验证器及自测。
- 将六处产品版本元数据更新为 `2.0.5`，补充运行时合同和 Phase-18-05 验收语义文档。
- 初始候选、一次修正候选和最终修正候选均各自通过一次正式入口，并且每个候选只生成
  `run-1`、`run-2`；没有为任何候选启动第三轮。旧候选证据均保留且未覆盖。

## 初始候选绑定（首次正式矩阵）

- 分支：`develop/2.0.5`
- 候选提交：`3884aecce7dc57b053ce4ccc249d872ffc383ef4`
- 运行时合同摘要：`sha256:a0780c448a6f5147a5a9ba020144d68fe8b3bf57f5d058b68c52b96430ded9fd`
- 证据目录：`.run/phase18-scale-closure-2.0.5-3884aecce7dc-bcee7f366ec0`
- 候选绑定要求：直接私有探针、四个探针路径、两次固定矩阵、Kafka 四分区、无新增宿主端口。

## 初始候选正式矩阵结果（保留的历史事实）

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
`scripts/verify-runtime-contracts.sh`、`dev/logs/Phase-18/Phase-18-05-合同单一来源独立诊断与完整矩阵收口.md`。

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

## 修正候选与最终候选

用户要求修复 `U3: 0/2` 后，保留初始候选并创建了两个新的、可独立绑定的候选；这属于本批次的实际偏差，
不是对初始回执追加第三轮：

| 候选 | 正式证据 | 结果 | U1/U2/U3/U4 |
| --- | --- | --- | --- |
| `7d834a2` | `.run/phase18-scale-closure-2.0.5-7d834a234f4b-68f3df8150ac` | `boundary_found` | `2/2`、`2/2`、`0/2`、`2/2` |
| `2a64bf3`（最终） | `.run/phase18-scale-closure-2.0.5-2a64bf3adfee-cb7f86577f95` | `boundary_found` | `2/2`、`2/2`、`0/2`、`2/2` |

两个修正候选的严格证据校验均通过。修正内容为：

- 组件自研进程指标诊断使用合同规定的 `/internal/v1/metrics`，插件自身仍使用 `/metrics`。
- 观测验收先执行 `setup`，再查询用户 ID 并运行 `admin-role bootstrap`，随后执行管理员场景。
- RabbitMQ 和业务 ES 故障使用业务 spec 支持的 `business` 场景；每次 acceptance 命令使用唯一 token。
- SIGTERM 后使用 `--force-recreate --no-deps` 重建 `backend-2`，两轮健康恢复均通过。
- 业务副本缩容诊断只检查仍运行的副本，不再探测刚停止的三个实例。

最终候选 `2a64bf3` 的完整矩阵结果如下：

| 项目 | run-1 | run-2 | 平均/计数 |
| --- | ---: | ---: | ---: |
| `diagnostic_checked` | 63 | 63 | 63 |
| `diagnostic_total` | 88 | 88 | 88 |
| `elapsed_seconds` | 200.536 | 186.490 | 193.513 |
| 项目清理退出码 | 0 | 0 | `2/2` |

最终 U3 仍为 `0/2`，但边界已收敛为真实结果：

- 每轮 5 条 Monitor 托管、当前未安装且没有独立 Compose 服务的 Exporter 记录为 `not_started`；因此
  `normal-concurrency` 和 `terminal-closure` 保持 `boundary_found`。
- RabbitMQ 短故障的业务验收因通知异步物化延迟未满足浏览器断言；业务 ES 短故障的业务验收因搜索索引
  不可用未满足搜索断言。两者均为退出码 `1`，原始 Playwright 输出已保留。
- 其余正常验收、观测初始化、业务/观测扩缩、Kafka、观测 ES、VictoriaMetrics、SIGTERM、服务重建和
  终态命令均通过；业务缩容、指标路径、观测管理员初始化和 SIGTERM 不再是失败边界。

最终候选实际执行的命令与结果：

- `python3 -m unittest discover -s scripts/ci -p 'test_phase18_scale_closure.py'`：通过，10 个测试。
- `python3 -m unittest discover -s scripts/ci -p 'test_phase18_scale_evidence.py'`：通过，6 个测试。
- `python3 -m py_compile scripts/ci/phase18_scale_closure.py scripts/ci/test_phase18_scale_closure.py`：通过。
- `git diff --check`：通过。
- `scripts/verify-phase18-scale-closure.sh --repetitions 2`（最终候选 `2a64bf3`）：退出码 `0`，两轮证据完整，结果 `boundary_found`。
- `python3 scripts/verify-phase18-evidence.py --closure .run/phase18-scale-closure-2.0.5-2a64bf3adfee-cb7f86577f95`：通过。

最终证据绑定到提交 `2a64bf3adfee8b826d45e94e705d90ff0d10624b`；后续仅更新本记录和状态说明，
没有修改 runner、产品代码或任何最终回执。
