# Phase 19：容量认证与性能可观测总实施方案

> 初始规划基线：2026-09-28，`upstream/main=9730c57`，根 `VERSION=2.0.5`。
>
> 修订基线：2026-09-29，`upstream/main=911b13e`，根 `VERSION=2.1.2`。原 Phase-19-03
> deterministic preflight 为 `incomplete`，正式容量入口未调用；容量认证顺延到 Phase-19-04。
>
> Phase 19 使用新版本线 `2.1.x`；patch `0` 为阶段基线，不创建 `2.1.0` 实现批次。

## 1. 阶段目的

Phase 19 在既有多副本和故障矩阵之上，交付指定环境中的容量曲线，而不是一个脱离资源、
数据和延迟目标的 RPS 宣传数字。执行顺序固定为：

```text
探针/准入与诊断信号可信
→ 容量 profile、统计和 evidence 合同冻结
→ 正式验收路径修订并以非正式 preflight 证明
→ 同一冻结候选完成容量认证并发布事实
```

本阶段不预先承诺 `150 RPS` 达标。它要求结果可复核、边界可解释、第一瓶颈有同窗证据，
并严格区分“验收执行是否完整”和“容量目标是否达到”。

## 2. 结果语义

### 2.1 执行状态

| 状态 | 含义 | 是否可完成批次 |
| --- | --- | --- |
| `complete` | 候选、环境、profile、全部重复、原始证据和安全清理完整 | 是 |
| `incomplete` | 候选漂移、runner/evidence 错误、缺失重复、证据丢失或清理不安全 | 否 |

`incomplete` 是验收流程未完成，不能伪装成产品容量边界，不能更新批次完成版本。

### 2.2 能力状态

执行状态为 `complete` 后，容量结论只能是：

| 状态 | 含义 |
| --- | --- |
| `target_met` | 冻结 profile 定义的目标阶梯和同步/异步/观测门禁均达到 |
| `boundary_found` | 执行完整，但一个或多个目标未达到，且原始事实和第一边界完整保留 |

版本号表示计划内实施和验证完成，不单独表达容量通过；README 与能力清单必须同时展示能力状态。

## 3. 批次、版本与分支

| 批次 | 目标版本 | 分支 | 交付结果 |
| --- | --- | --- | --- |
| Phase-19-01 | `2.1.1` | `develop/2.1.1` | 探针/业务准入隔离，尾延迟与饱和度指标合同 |
| Phase-19-02 | `2.1.2` | `develop/2.1.2` | 容量 profile、runner、采样、统计和 evidence 冻结 |
| Phase-19-03 | `2.1.3` | `develop/2.1.3` | 修订正式验收路径并完成真实有界 preflight |
| Phase-19-04 | `2.1.4` | `develop/2.1.4` | 认证冻结 `2.1.3` 候选、发布第一瓶颈并收口阶段 |

每个批次开始前获取 primary remote，从当时最新的 `upstream/main` 创建表中分支。前一批必须完成、
合并且更新根 `VERSION` 后，才能创建下一批分支。不得提前创建后续分支。

原预检期间本地创建的 `develop/2.1.3` 未推送且没有实施提交；本修订合入主线后，开始新
Phase-19-03 前必须从最新 primary remote `main` 重建该分支。不得将旧基线上的空分支视为已满足创建规则。

## 4. Phase-19-01：探针隔离与性能诊断信号

目标：在业务并发槽饱和时，探针仍表达进程和依赖的真实状态；为 HTTP 请求提供固定、低基数、
可在 VictoriaMetrics 查询 P50/P95/P99 的分布指标，并保留现有 count/duration 合同兼容性。

必须证明：

- `/startup`、`/live`、`/ready`、`/health` 不被业务准入信号量错误拒绝；
- Compose healthcheck 使用不经过边缘和业务准入的直接私有探针；
- 业务并发上限耗尽时，普通 API 明确返回 `503 backend_busy`，且拒绝本身有计数；
- 延迟桶只使用 method、route template、status class 和固定 bucket，不接受 URL、ID 或任意标签；
- Monitor、Envelope、Marshaller、VictoriaMetrics 和 Backend 查询对新指标合同端到端一致；
- 旧 count/duration 查询、管理页面和固定组件指标目录不退化。

详细文件清单和验证见 [`Phase-19-01`](Phase-19-01-探针隔离与尾延迟指标.md)。

## 5. Phase-19-02：容量合同与验收工具

目标：把 Phase-18 历史负载能力迁移为独立的 Phase 19 容量 profile，并在正式运行前冻结资源、
数据、阶梯、重复、门禁、停止条件、统计和 evidence schema。

必须证明：

- profile 是机器可读的单一来源，精确绑定数据规模、混合比例、虚拟用户、阶梯、窗口和阈值；
- 负载生成器进程与 SUT 的 CPU、RSS、调度滞后和饱和信号分别记录；
- 正式执行固定为三次独立重复，每次包含 `50/100/150/200 RPS` 阶梯和有界恢复；
- 每阶梯保留三次原值，汇总 median、min/max 和变异系数，不把三次样本混成伪分位数；
- 安全停止只允许在 OOM、归属丢失、清理不安全或 profile 定义的硬错误上触发，并保留未执行阶梯；
- runner/evidence 自测在任何正式候选冻结前完成。

Phase-19-02 完成后，`capacity-profile.json` 的 RPS、窗口、业务比例、重复次数和能力阈值保持冻结；
Phase-19-03 只允许修订宿主兼容、正式编排和真实 evidence 合同，并产生供 Phase-19-04 绑定的新 digest。
详细文件清单和验证见
[`Phase-19-02`](Phase-19-02-容量合同与验收工具.md)。

## 6. Phase-19-03：容量验收工具修订

目标：修复原正式预检确认的验收基础设施问题，在不改变负载和能力阈值的前提下，使 runner 能够：

- 从计划固定的 `--manifest/--work` 接口自行读取 profile、Compose 和 runtime contract；
- 为每轮空项目物化并复核确定性配方，使用该轮实际 endpoint；
- 保存逐阶梯进度、资源、异步恢复和 Metrics/Logs/Events 真实新鲜度原始证据；
- 按产品支持边界验证 Compose 最低版本，不以固定 major 字符串拒绝兼容环境；
- 由 verifier 从原始文件重算门禁，拒绝合成布尔值、整轮最大值冒充恢复事实和 summary 替换；
- 用 `formal=false` 的有界真实 preflight 完成启动、短负载、恢复、验证和清理。

本批不调用正式三重复入口，不产生能力结论。完成后根版本为 `2.1.3`；Phase-19-04 只能从包含本批
完成提交的最新主线构建被测 `2.1.3` 候选。

详细文件清单和验证见 [`Phase-19-03`](Phase-19-03-容量验收工具修订.md)。

## 7. Phase-19-04：正式容量认证与阶段收口

目标：对同一冻结 `2.1.3` 候选调用一次正式入口，由 runner 完成 profile 规定的三次独立重复，
形成容量曲线、能力状态和第一瓶颈结论，并以完成版本 `2.1.4` 收口 Phase 19。

正式结果至少包含：

- 每阶梯 achieved RPS、明确拒绝、超时、非预期错误、P50/P95/P99/max；
- Backend/MySQL/Redis/RabbitMQ/两个 ES/Kafka/VM 和自研进程 CPU、RSS、I/O、连接、队列与 lag；
- Outbox、RabbitMQ、通知、搜索和可观测数据的恢复与最终闭合；
- 候选、Compose、runtime contract、profile、数据配方、宿主、Docker 和清理回执；
- `complete + target_met` 或 `complete + boundary_found`；不得产生“执行失败但批次完成”。

本批不允许根据结果修改产品代码。若发现瓶颈，只记录可复核证据，由 Phase 19 之后的新总方案决定
是否优化；不得为获得更好数字在同批修改候选并筛选重跑。

被测 candidate version/revision/digest 与收口版本必须分别展示；`2.1.4` 不得被描述成另一个已测试
候选。详细文件清单和验证见 [`Phase-19-04`](Phase-19-04-容量认证与阶段收口.md)。

## 8. 阶段完成条件

1. Phase-19-01、19-02、19-03、19-04 按顺序完成，四个分支和版本与本方案一致。
2. 探针不受业务准入饱和影响，尾延迟与饱和度指标可通过完整观测链路查询。
3. Phase-19-03 真实有界 preflight 通过且明确 `formal=false`，未生成或复用正式容量结论。
4. Phase-19-04 正式容量入口只调用一次，由同一 profile 完成三次独立重复；没有候选或阈值漂移。
5. evidence verifier 对 binding、逐阶梯原值、真实恢复/新鲜度、聚合、失败、未执行阶梯和清理严格校验。
6. Phase-19-04 执行状态为 `complete`，能力状态为 `target_met` 或 `boundary_found`。
7. 实施记录只记录实际文件、命令、结果、偏差与限制；根 `VERSION` 最终为 `2.1.4`。
8. 更新能力清单，停止 Phase 19；Trace、生命周期、Monitor HA、状态层 HA 和 Kubernetes 另行立项。

## 9. 非目标

- 分布式 Trace、OTLP 接入或 Trace 存储。
- Logs/Events/VictoriaMetrics 长期保留实现。
- Monitor 多副本、同类插件多实例或目标自动发现。
- MySQL/RabbitMQ/Kafka/Elasticsearch/VictoriaMetrics 集群化。
- Kubernetes、HPA、Service Mesh、跨地域、多活或生产 TLS/SASL。
- 未经正式容量证据支持的 SQL、缓存、GC、连接池或消息参数优化。
