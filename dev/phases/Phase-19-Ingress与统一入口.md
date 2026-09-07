# Phase 19：Ingress与统一入口

> 本阶段只收敛 Kubernetes 外部入口，不改变 Phase 16 已形成的同域登录和双前端产品模型。

## 版本规划

- 版本线：`1.16.x`；阶段基线：`1.16.0`。
- 执行批次从 `1.16.1` 起编号，由 Phase 19 总实施方案统一分配版本与分支。

## 阶段目标

为 Kubernetes 中的 GoPulse 提供单一 HTTP 入口，使统一登录、角色分流、用户 Frontend、管理 Frontend 和 Backend API 在同一域名下工作，并关闭不必要的外部暴露。

## 开发范围

- 对接 Ingress Controller，路由统一登录入口、两个 Frontend 和 Backend API。
- 保持同源 Cookie、退出、会话恢复和角色变化后的重新分流。
- 内部基础设施、自研服务和插件仅通过集群内部 Service 通信。
- 统一处理上游故障，不向浏览器泄漏 Service 名称、集群地址或原始错误。

## 最小端到端闭环

`单一地址登录 → user 进入用户端 / super_admin 进入管理端 → 两端只经 Backend 使用内部能力 → 退出或角色变化后正确重新分流`

## 验收标准

- 用户只需一个域名，不需要直接访问 NodePort。
- 普通用户不能加载管理数据；超级管理员可完成大屏、插件、告警和权限管理流程。
- 两个独立 Frontend 的资源和路由互不混淆，但共享同源登录会话。
- MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics、Monitor、Router、Marshaller 和插件均不直接对外暴露。
- 可观测上游故障不阻断代表性社交路径，错误响应不泄漏内部拓扑。

## 本阶段不做

- 新的身份体系、细粒度 RBAC、外部告警渠道或复杂网关治理。
- 公网生产域名、完整证书运营、多集群流量或服务网格。

## 总实施方案约束

- 总方案必须提供完整路由/暴露矩阵、同源会话测试、身份 header 负向验证和两个 Frontend 的独立回退规则。
- 验收必须经单一入口完成，不能依赖 NodePort 旁路。
