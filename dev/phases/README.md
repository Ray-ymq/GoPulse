# GoPulse 阶段索引

目标架构由[高并发设计](<../../docs/GoPulse 高并发架构设计.md>)与
[可观测设计](<../../docs/GoPulse 可观测架构设计.md>)共同定义。
完成产品版本读取 [VERSION](../../VERSION)；当前能力、执行状态与证据边界见
[能力状态](../status/capability-status.md)。本页只维护阶段入口与历史导航。

## 实施入口

- Phase 21：[用户业务与管理服务分离总实施方案](../imple/Phase-21/Phase-21-总实施方案.md) / [阶段导航](Phase-21-用户业务与管理服务分离.md)。
- 批次顺序、目标版本、分支、进入条件与验收由总方案和分方案负责；实际交付与执行状态查询能力状态及同名日志。

## 历史阶段

下表保留 Phase 0～20 的历史分配与阶段主题。候选、实际结果及未交付范围以
相应总实施方案、[能力状态](../status/capability-status.md)和[实施日志](../logs/README.md)为准。

<details>
<summary>展开历史阶段索引</summary>

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
| Phase 20 | `2.2.1`～`2.2.4` | [端到端性能闭环与可观测治理](Phase-20-端到端性能闭环与可观测治理.md) | 可信诊断、单链路关联、无需优化复验与生命周期 | Milestone 7 按调整范围收口 |

</details>

[Plan.md](Plan.md)保留 2026-10-05 的历史规划快照；后续阶段与当前状态通过上述入口查询。
已完成材料保持原路径，索引和提纲的维护约定见[开发文档](../README.md)。
