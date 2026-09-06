# Phase 20：Kubernetes可观测集成

> 本阶段让完整 GoPulse 观测其 Kubernetes 运行环境，不把 Kubernetes 当作业务与可观测产品闭环的成立条件。

## 版本规划

- 版本线：`1.17.x`；阶段基线：`1.17.0`。
- 执行批次从 `1.17.1` 起编号，由 Phase 20 总实施方案统一分配版本与分支。

## 阶段目标

将 Phase 14 的组件采集、Phase 15 的管理大屏与内部告警适配到 Kubernetes 运行目标，并补充代表性集群对象数据，完成 Kubernetes 交付里程碑。

## 开发范围

- 六类官方单实例插件连接集群内 Redis、MySQL、RabbitMQ、Kafka、Elasticsearch 和 VictoriaMetrics Service。
- Backend、Worker、Indexer、Monitor、Router 和 Marshaller 的既有指标在 Kubernetes 形态下保持可采集。
- 采集代表性 Node、Pod、Workload 状态与事件，使用最小只读 RBAC。
- 将 Kubernetes 指标、日志、事件和告警接入既有存储、Backend 和独立管理 Frontend。
- 在管理大屏区分业务、组件、采集链路和 Kubernetes 运行状态。

## 最小端到端闭环

`Kubernetes 中的业务、组件和代表性集群对象 → 既有采集处理链路 → Backend → 管理大屏与内部告警`

## 验收标准

- 六类组件、自研服务和代表性 Kubernetes 对象均产生真实可查询数据。
- 代表性集群异常可触发并恢复内部告警，且不引入外部通知渠道。
- Kubernetes API 权限仅覆盖已验收的只读对象，不读取或返回 Secret。
- 普通用户不能访问任何集群数据；超级管理员经统一入口查看大屏和告警。
- 自观测链路退化不阻断代表性社交闭环，也不改变 Phase 17 已验收的产品语义。

## 本阶段不做

- 重做插件框架、同类多实例、全面集群监控、自动修复或跨集群采集。
- 新业务功能、前端体系重构、生产级 HA、完整 SRE 或容量承诺。

## 阶段完成条件

- 全部阶段验收标准通过，完整产品在 Kubernetes 中保持原有能力并能够观察其运行环境。
- 达到条件后完成 Milestone 5；未实现的生产化能力进入独立后续规划，不继续扩大当前阶段。

## 总实施方案约束

- 必须使用真实集群数据，不能用静态演示或集群外链路代替。
- 总方案必须固定最小 RBAC、敏感字段过滤、普通用户负向验收和社交业务故障隔离。
