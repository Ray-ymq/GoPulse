# Phase 13：Kubernetes基础部署

> 本文档是阶段开发提纲，具体接口、数据模型、消息格式和部署参数将在本阶段进入详细设计时补充。

## 版本规划

- 版本线：`1.10.x`；阶段基线：`1.10.0`。
- 执行批次从 `1.10.1` 起编号，具体目标版本和开发分支由 Phase 13 总实施方案统一分配。

## 阶段目标

将完整 Docker Compose 环境迁移到 1 Master、3 Worker 的 Kubernetes 集群。

本阶段完成后，应形成可独立运行和验证的结果，并能够直接支撑 Phase 14。

## 开发范围

- 无状态组件 Deployment 与 Service。
- 有状态基础设施的基础 Kubernetes 部署。
- 必要持久化资源。
- 节点标签与调度约束。
- 集群内服务发现。

## 核心任务

- 部署 Frontend、Backend、Worker 和自研可观测组件。
- 逐步部署 MySQL、Redis、RabbitMQ、Kafka、VictoriaMetrics 与 Elasticsearch。
- 按节点规划设置标签及调度约束。
- 使用 Service 名称替代固定 Pod IP。
- 验证完整系统在集群内运行。

## 前置依赖

- Phase 12 Docker 化完成。
- Kubernetes 集群与容器镜像可用。

## 阶段产物

- GoPulse Kubernetes 基础资源。
- 集群内服务通信。
- 必要的数据持久化配置。
- 符合规划的节点调度结果。

## 验收标准

- 完整 GoPulse 可脱离 Docker Compose 在 Kubernetes 中运行。
- 所有组件通过 Service 名称通信。
- 工作负载按预期分布到三个 Worker。
- 必要数据在 Pod 重建后仍可使用。

## 本阶段不做

- 复杂生产级高可用。
- 一次性将全部有状态组件完善为复杂 StatefulSet。
- Ingress 统一外部入口。
- Kubernetes 自观测闭环。

## 后续待细化事项

- 具体代码目录与模块边界。
- 对外及内部接口定义。
- 数据结构、消息结构或存储结构。
- 配置项、运行参数与异常处理细节。
- 本阶段任务拆分、测试用例与实施顺序。
