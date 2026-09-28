# Kubernetes 集群可观测设计

> 状态：未来设计，暂未排期。
>
> 本文不属于当前 Phase、目标版本、实施批次或验收承诺。它讨论如何让 GoPulse 观测可选的 Kubernetes 运行环境，不把 Kubernetes 当作业务与可观测产品闭环的成立条件。

## 设计目标

评估将既有组件采集、管理大屏与内部告警适配到 Kubernetes 运行目标，并补充代表性集群对象数据。

## 候选设计范围

- 六类官方单实例插件连接集群内 Redis、MySQL、RabbitMQ、Kafka、Elasticsearch 和 VictoriaMetrics Service。
- Backend、Worker、Indexer、Monitor、Router 和 Marshaller 的既有指标在 Kubernetes 形态下保持可采集。
- 采集代表性 Node、Pod、Workload 状态与事件，使用最小只读 RBAC。
- 将 Kubernetes 指标、日志、事件和告警接入既有存储、Backend 和独立管理 Frontend。
- 在管理大屏区分业务、组件、采集链路和 Kubernetes 运行状态。

## 最小端到端闭环

`Kubernetes 中的业务、组件和代表性集群对象 → 既有采集处理链路 → Backend → 管理大屏与内部告警`

## 未来验收方向

- 六类组件、自研服务和代表性 Kubernetes 对象均产生真实可查询数据。
- 代表性集群异常可触发并恢复内部告警，且不引入外部通知渠道。
- Kubernetes API 权限仅覆盖已验收的只读对象，不读取或返回 Secret。
- 普通用户不能访问任何集群数据；超级管理员经统一入口查看大屏和告警。
- 自观测链路退化不阻断代表性社交闭环，也不改变 Phase 17 已验收的产品语义。

## 设计边界

- 重做插件框架、同类多实例、全面集群监控、自动修复或跨集群采集。
- 新业务功能、前端体系重构、生产级 HA、完整 SRE 或容量承诺。

## 未来验收方向

- 若未来立项，完整产品应在 Kubernetes 中保持原有能力并能够观察其运行环境。
- 未实现的生产化能力应进入独立后续规划，不扩大首次部署适配范围。

## 未来实施约束

- 必须使用真实集群数据，不能用静态演示或集群外链路代替。
- 若未来立项，实施方案必须固定最小 RBAC、敏感字段过滤、普通用户负向验收和社交业务故障隔离。
