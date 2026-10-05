# GoPulse 阶段性开发文档

项目目标架构由[高并发设计](<../../docs/GoPulse 高并发架构设计.md>)与
[可观测设计](<../../docs/GoPulse 可观测架构设计.md>)共同定义。
本目录导航已分配阶段；[Plan.md](Plan.md)保存历史划分，不另立未来路线。
精确批次、版本、分支和门禁以各 Phase 总实施方案为准；实际能力见[能力状态](../status/capability-status.md)。

## 最近收口：Phase 20（截至 2.2.4）

当前已完成产品版本读取 [VERSION](../../VERSION)。2026-10-05 按用户要求，Phase 20
仅保留已完成的 01～04，在 `2.2.4` 结束；原 05、06 已取消，实施文件已移除。
原资源预算与最终持续运行目标未交付；本次范围收口不表示原六批验收通过。

- [总实施方案](../imple/Phase-20/Phase-20-总实施方案.md)：调整后范围、四批结果、历史分支与取消记录。
- [Phase 20 日志](../logs/Phase-20/)：实际执行与证据，文档整理不改变其结论或恢复执行。

| 阶段 | 版本线 | 文档 | 主要结果 | 里程碑 |
| --- | --- | --- | --- | --- |
| Phase 20 | `2.2.1`～`2.2.4` | [端到端性能闭环与可观测治理](Phase-20-端到端性能闭环与可观测治理.md) | 可信诊断、单链路关联、无需优化复验与生命周期 | Milestone 7 按调整范围收口 |

后续目标按两篇设计理解；进入执行前须通过正式总实施方案分配范围和验收合同。
当前没有待执行批次；后续以 `2.2.4` 为基线，不再依赖已取消的 05/06。
未分配的目标不预留 Phase、版本或分支，历史未完成结果不能作为已交付能力。

## 已完成阶段与历史导航

下表是 Phase 0～19 的阶段记录，文档保留原路径；阶段版本只表示其历史分配。

<details>
<summary>展开已完成阶段索引</summary>

| 阶段 | 版本线 | 文档 | 主要结果 | 里程碑 |
| --- | --- | --- | --- | --- |
| Phase 0 | `0.1.x` | [工程骨架](Phase-00/Phase-00-工程骨架.md) | 项目结构与最小运行链路 | Milestone 1 |
| Phase 1 | `0.2.x` | [最小业务闭环](Phase-01-最小业务闭环.md) | 用户、帖子、评论与点赞 | Milestone 1 |
| Phase 2 | `0.3.x` | [业务异步化](Phase-02-业务异步化.md) | RabbitMQ 与可靠异步通知 | Milestone 1 |
| Phase 3 | `0.4.x` | [Elasticsearch与业务搜索](Phase-03-Elasticsearch与业务搜索.md) | 可重建帖子搜索 | Milestone 1；`1.0.0` 收口 |
| Phase 4 | `1.1.x` | [业务日志基础](Phase-04-业务日志基础.md) | 结构化业务日志 | Milestone 2 |
| Phase 5 | `1.2.x` | [Exporter Plugin原型](Phase-05-Exporter-Plugin原型.md) | Redis Exporter 原型 | Milestone 2 |
| Phase 6 | `1.3.x` | [Monitor](Phase-06-Monitor.md) | MetricsMonitor 与插件管理基础 | Milestone 2 |
| Phase 7 | `1.4.x` | [Message Router与Kafka](Phase-07-Message-Router与Kafka.md) | 可观测消息入口 | Milestone 2 |
| Phase 8 | `1.5.x` | [Marshaller与VictoriaMetrics](Phase-08-Marshaller与VictoriaMetrics.md) | 指标存储闭环 | Milestone 2 |
| Phase 9 | `1.6.x` | [LogMonitor与日志链路](Phase-09-LogMonitor与日志链路.md) | 日志闭环 | Milestone 3 |
| Phase 10 | `1.7.x` | [EventMonitor与事件链路](Phase-10-EventMonitor与事件链路.md) | 事件闭环 | Milestone 3 |
| Phase 11 | `1.8.x` | [可观测前端](Phase-11-可观测前端.md) | 第一版可观测查询与管理界面 | Milestone 3 |
| Phase 12 | `1.9.x` | [Docker化](Phase-12-Docker化.md) | 完整 Compose 运行基线 | Milestone 4 |
| Phase 13 | `1.10.x` | [业务基础系统与用户端闭环](Phase-13-业务基础系统与用户端闭环.md) | 关注、Following、收藏、帖子编辑/删除与独立用户端 | Milestone 4 |
| Phase 14 | `1.11.x` | [插件体系与组件可观测闭环](Phase-14-插件体系与组件可观测闭环.md) | 六类官方单实例插件与自研组件指标 | Milestone 4 |
| Phase 15 | `1.12.x` | [告警与管理端闭环](Phase-15-告警与管理端闭环.md) | 内部告警、双角色与独立管理端大屏 | Milestone 4 |
| Phase 16 | `1.13.x` | [Linux 产品化与双前端交付](Phase-16-Linux产品化与双前端交付.md) | 同域双前端与 Linux `amd64` 产品交付 | Milestone 4 |
| Phase 17 | `1.14.x` | [稳定性与工程化](Phase-17-稳定性与工程化.md) | 完整产品质量验收 | Milestone 4 收口 |
| Phase 18 | `2.0.x` | [高并发与可观测架构收敛](Phase-18-高并发与可观测架构收敛.md) | 容量边界、计算层多副本、双 ES、背压与独立诊断 | Milestone 5 收口 |
| Phase 19 | `2.1.x` | [容量认证与性能可观测](Phase-19-容量认证与性能可观测.md) | 探针/准入隔离、尾延迟信号与指定环境容量认证 | Milestone 6 收口 |

</details>

Phase 17 已完成 Linux `amd64` Compose 中业务、插件、Metrics/Logs/Events、内部告警与双前端的代表性产品流程。
Phase 18 已完成计算层多副本、故障域隔离、背压和独立诊断；Phase-18-05 最终固定矩阵为 `target_met`，不替代独立容量认证。
Phase 19 在 `2.1.4` 完成，对冻结 `2.1.3` 候选的正式容量认证为 `complete / boundary_found`。
Phase 20 已完成可信诊断、单链路关联、B0 十二格复验和数据生命周期，并按调整范围在 `2.2.4` 结束；这些结果不改写 Phase 19 的历史边界。
各结果的候选、环境与测量范围以[能力状态](../status/capability-status.md)及对应日志为准。

## 当前实现边界

- MySQL 是业务事实源；Redis 是缓存；Elasticsearch 业务索引可由 MySQL 重建。
- RabbitMQ 只承载业务异步任务；Kafka 只传输 Metrics、Logs 和 Events。
- 六类官方插件均为一种插件、一个实例、一个目标；多实例属于目标设计，尚未成为当前能力。
- 用户 Frontend 与管理 Frontend 是两个独立应用，但共用 Backend、用户数据库、统一登录和同源会话。
- 当前角色只有 `user` 与 `super_admin`；引导超级管理员不可删除或降级，其他账号可由超级管理员按用户 ID 调整角色。
- 告警只在管理后台内部展示，不接入外部通知渠道。
- Frontend 不直连 Monitor、插件或数据基础设施。
- 当前受支持的产品环境是 Linux `amd64` Docker Compose；状态层 HA 与 Kubernetes 尚未分配实施合同。

## 文档维护约定

- 两篇目标设计共同负责架构；提纲、索引与历史规划不得另立未来主线。
- 每个 Phase 的精确批次、目标版本和 `develop/x.x.x` 分支只在总实施方案中分配。
- 批次按可运行、可验证的端到端能力切分，不按技术层机械拆分。
- 阶段提纲不提前冻结 API、Schema、消息契约、告警表达式或未来部署资源规格。
- 已完成计划、日志、评审与原始证据保留路径与历史上下文；只通过导航区分当前与历史。
- 实施完成后必须按仓库规则记录真实改动、命令、结果、偏差和后续事项。
