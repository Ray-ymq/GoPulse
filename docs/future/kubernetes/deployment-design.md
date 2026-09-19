# Kubernetes 基础部署设计

> 状态：未来设计，暂未排期。
>
> 本文不属于当前 Phase、目标版本、实施批次或验收承诺。Kubernetes 只作为可选部署目标；当前受支持的产品环境仍为 Linux `amd64` Docker Compose。

## 设计目标

评估将当前已经完成并验收的 Compose 产品迁移到 Kubernetes 集群，同时保持全部产品行为不变。具体集群规模、拓扑和资源规格必须在未来立项时根据真实环境重新确定。

## 候选设计范围

- 为用户 Frontend、管理 Frontend、Backend、Worker、Indexer、Monitor、Router、Marshaller、六类插件及基础设施建立必要工作负载与 Service。
- 使用 Service DNS、配置与 Secret 引用替代 Compose 地址，不使用固定 Pod IP。
- 为有状态数据建立与阶段风险相称的 PVC、恢复和重建契约。
- 接入 Phase 17 已提供的启动、存活、就绪和优雅退出契约。
- 保留 `user`/`super_admin` 权限、内部服务边界和可观测故障不阻断社交业务的产品行为。

## 最小端到端闭环

`已验收制品 → Kubernetes 部署 → 两个 Frontend 与 Backend 运行 → 业务、插件、告警链路共同工作 → 工作负载重建后恢复`

## 未来验收方向

- 完整 GoPulse 脱离 Compose 在目标集群共同运行，不在集群内编译源码。
- 所有组件使用 Service 名称通信，持久状态在代表性 Pod 重建后满足保留或可重建契约。
- 普通用户和超级管理员的代表性流程与 Phase 17 一致。
- 浏览器不能直达内部组件，Secret 不进入 Frontend、日志或公共 API。
- Kubernetes 资源中不存在用于弥补产品功能缺失的临时旁路实现。

## 设计边界

- 新业务功能、新插件、告警产品扩展或前端重构。
- Ingress 统一入口、全面集群自观测或复杂生产级高可用。

## 未来实施约束

- 若未来立项，实施方案必须列出产品能力迁移矩阵、Service/Secret/持久化边界和 Compose 对等验收。
- 批次按可运行系统闭环切分，不按 Kubernetes 资源类型机械切分。
