# GoPulse 阶段性开发文档

本目录将 [`Plan.md`](Plan.md) 拆分为各阶段提纲。Phase 17 已在 `1.14.5` 完成并收口 Milestone 4；已完成阶段的提纲、实施方案和记录保持历史原貌。Phase 18 已由总实施方案分配为 `2.0.x` 的五个执行批次，当前完成产品版本仍为 `1.14.5`。

## 阶段索引

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
| Phase 18 | `2.0.x` | [高并发与可观测架构收敛](Phase-18-高并发与可观测架构收敛.md) | 容量基线、多副本、背压与故障隔离 | Milestone 5 |

## 已完成执行顺序

```text
Phase 12 已完成的 Compose 基线
→ Phase 13 业务与用户端
→ Phase 14 插件与组件指标
→ Phase 15 告警与管理端
→ Phase 16 Linux 产品化与双前端交付
→ Phase 17 完整产品工程验收
```

Phase 17 完成后，GoPulse 已能在当前 Linux `amd64` Compose 环境中完成业务、插件、Metrics/Logs/Events、内部告警和两个 Frontend 的代表性流程。Phase 18 将优先证明容量、多副本、背压和故障隔离，不以部署平台变更替代产品自身验证。

Kubernetes 基础部署、统一入口和集群观测已移至 [`docs/future/kubernetes`](../../docs/future/kubernetes/README.md)，仅作为未排期设计，不属于当前 Phase、版本或里程碑。

## 核心边界

- MySQL 是业务事实源；Redis 是缓存；Elasticsearch 业务索引可由 MySQL 重建。
- RabbitMQ 只承载业务异步任务；Kafka 只传输 Metrics、Logs 和 Events。
- 六类官方插件均为一种插件、一个实例、一个目标；多实例保留为独立未来设计。
- 用户 Frontend 与管理 Frontend 是两个独立应用，但共用 Backend、用户数据库、统一登录和同源会话。
- 最终角色只有 `user` 与 `super_admin`；引导超级管理员不可删除或降级，其他账号可由超级管理员按用户 ID 调整角色。
- 告警只在管理后台内部展示，不接入外部通知渠道。
- Frontend 不直连 Monitor、插件或数据基础设施。
- 当前受支持的产品环境是 Linux `amd64` Docker Compose；其他部署环境需要独立立项和验收。

## 文档维护约定

- `Plan.md` 是阶段划分、里程碑和架构边界的上层依据。
- 每个 Phase 的精确批次、目标版本和 `develop/x.x.x` 分支只在总实施方案中分配。
- 批次按可运行、可验证的端到端能力切分，不按技术层机械拆分。
- 阶段提纲不提前冻结 API、Schema、消息契约、告警表达式或未来部署资源规格。
- 实施完成后必须按仓库规则记录真实改动、命令、结果、偏差和后续事项。
