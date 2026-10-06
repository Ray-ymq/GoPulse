# Phase-18-05：合同单一来源、独立诊断与完整矩阵收口

> 目标版本：`2.0.5`
>
> 开发分支：`develop/2.0.5`
>
> 最终验收次数：每个验收单元固定 `2` 次（首次 + 唯一一次重复）。

## 1. 目标

把副本角色、依赖、探针、指标、所有权、背压和关闭语义固化为一个机器合同；使用冻结候选
正好运行两次完整扩缩容与故障矩阵，生成逐次事实和平均摘要后关闭 Phase 18。

## 2. 实施范围

- 权威机器合同覆盖进程 ID、可扩展/单写角色、监听器、startup/live/ready、硬/软依赖、
  实例身份、关键连接/队列/in-flight 预算和关停预算。
- Compose、代码目录、文档和验收脚本通过生成或漂移校验与合同一致；合同不是运行时配置服务。
- 验收容器逐实例检查探针、身份和容量信号，不经过可观测数据面且不新增宿主端口。
- 固定候选、数据、实例数、partition、资源、故障顺序和 evidence schema 后只执行两次完整矩阵。
- 报告区分已达到、未达到、未证明和执行失败；版本完成不替代能力声明。

## 3. 允许变更文件与逐文件验收

| 文件 | 文件级验收条件 |
| --- | --- |
| `deploy/runtime-contracts.json` | 成为完整机器事实源，覆盖全部长运行 Go 进程、副本角色、依赖和预算 |
| `deploy/runtime-contracts.schema.json` | 严格验证新增字段、枚举、范围、唯一性和向后兼容边界 |
| `deploy/compose.yaml` | 所有进程、探针、网络、服务和预算均能与机器合同一一对应 |
| `dev/contracts/runtime-contracts.md` | 解释合同权威性、独立诊断、两次验收和版本/能力声明区别 |
| `componentmetrics/catalog.go`、`componentmetrics/config.go` | 进程/指标目录不再另建冲突事实源，漂移可被机器检查发现 |
| `componentmetrics/cmd/catalog/main.go` | 若保留生成入口，其输出确定且只来自权威合同/受管目录 |
| `scripts/ci/verify_runtime_contracts.py` | 校验合同与 Compose、进程目录、探针、依赖、预算和实例角色一致 |
| `scripts/ci/test_runtime_contracts.py` | 覆盖缺项、重复角色、错误副本能力、预算漂移和非独立探针负例 |
| `scripts/verify-runtime-contracts.sh` | 接受冻结 candidate，输出可绑定的合同 digest 和逐实例结果 |
| `scripts/verify-phase18-scale-closure.sh` | 正式模式只接受 `--repetitions 2`；run-1 失败仍保存/清理并继续 run-2，禁止第三次 |
| `scripts/ci/phase18_scale_closure.py` | 编排两个完整 run，持久化原值/均值/确定性计数/失败阶段/清理结果 |
| `scripts/ci/phase18_scale_evidence.py` | 严格验证 binding、两个且仅两个 run、平均算法、文件账本和结果类型 |
| `scripts/ci/test_phase18_scale_closure.py` | 覆盖次数、candidate 漂移、顺序、失败继续记录和清理 |
| `scripts/ci/test_phase18_scale_evidence.py` | 覆盖篡改、缺 run、第三 run、错误平均、缺失败输出和文件清单外修改 |
| `scripts/verify-phase18-evidence.py` | 明确区分 Phase-18-01 历史 evidence 与 18-05 新 closure evidence |
| `dev/phases/Plan.md`、`dev/phases/README.md`、`dev/phases/Phase-18-高并发与可观测架构收敛.md` | 只在批次最终结果确定后更新当前状态，不改写历史运行事实 |
| `README.md` | 只声明两次 evidence 实际支持的能力；未达到项和单节点状态层边界清楚 |
| `dev/logs/Phase-18/Phase-18-05-合同单一来源独立诊断与完整矩阵收口.md` | 完整列出实际文件、两次矩阵、均值、结果类型、偏差和后续输入 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 六处产品版本一致为 `2.0.5` |

## 4. 固定最终验收单元

正式 runner 以一次 `--repetitions 2` 调用创建 `run-1` 和 `run-2`；下列单元在两个 run
中各执行一次：

| 单元 | 验证内容 | 合并方式 |
| --- | --- | --- |
| U1 | 机器合同 verifier 与负例测试 | `0/2`～`2/2` |
| U2 | closure/evidence runner 自测 | `0/2`～`2/2` |
| U3 | 冻结 `2.0.5` 候选完整扩缩容与故障矩阵 | 数值两次平均；确定性项 `0/2`～`2/2` |
| U4 | 版本、分支、文件账本和 `git diff --check` | 各命令 `0/2`～`2/2` |

U3 的两次运行顺序固定：正常并发流量 → 业务计算副本扩/缩 → 可观测计算副本扩/缩 →
RabbitMQ/Kafka 短故障 → 搜索 ES/观测 ES/VictoriaMetrics 分别故障 → 单实例 SIGTERM →
服务重建 → Outbox、RabbitMQ、Kafka lag/offset、搜索投影和可观测写入终态检查。

## 5. 完成条件

1. 文件清单逐行有状态，合同、Compose、代码和验收不存在未记录漂移。
2. 正式 runner 只调用一次且参数固定为 `--repetitions 2`；U1～U4 在两个 run 中各出现一次；
   两个 run 的 candidate 和条件一致且证据不可覆盖。
3. `summary.json` 保留两次原值、算术平均、确定性计数、失败阶段和清理结果。
4. 最终结果为 `target_met`、`boundary_found` 或 `execution_failed`，不启动第三次运行。
5. 无论结果类型，创建实施记录、同步 `VERSION=2.0.5`、提交并关闭 Phase 18。
6. 后续只根据事实另行规划容量认证、状态层 HA、Kubernetes 或 SRE，不创建 Phase-18-06。

`2.0.5` 只表示 Phase 18 的固定实施和两次验收已经完成，不表示所有目标已经通过。
