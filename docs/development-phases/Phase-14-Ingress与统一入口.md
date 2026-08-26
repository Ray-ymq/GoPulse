# Phase 14：Ingress与统一入口

> 本文档是阶段开发提纲，具体接口、数据模型、消息格式和部署参数将在本阶段进入详细设计时补充。

## 阶段目标

为 Kubernetes 中的 GoPulse 提供统一 HTTP 访问入口，并收敛外部暴露面。

本阶段完成后，应形成可独立运行和验证的结果，并能够直接支撑 Phase 15。

## 开发范围

- Ingress Controller 对接。
- Frontend 与 Backend 路由。
- 统一域名或访问地址。
- 内部基础设施访问边界。

## 核心任务

- 配置统一入口并将页面与 API 请求路由到对应 Service。
- 确保内部组件继续通过 ClusterIP 通信。
- 关闭不必要的直接外部暴露。
- 验证用户端完整访问流程。

## 前置依赖

- Phase 13 Kubernetes 基础部署完成。
- Frontend 与 Backend Service 可用。
- 集群已安装或可使用 Ingress Controller。

## 阶段产物

- GoPulse 统一访问入口。
- Frontend 与 API 的 Ingress 路由。
- 受控的集群外部暴露策略。

## 验收标准

- 用户只需一个地址即可访问页面和业务 API。
- 用户不需要直接访问 NodePort。
- MySQL、Redis、Kafka、Elasticsearch、VictoriaMetrics 等基础设施不直接暴露公网。
- 内部组件通信保持正常。

## 本阶段不做

- 公网生产域名和证书体系。
- 基础设施外部管理入口。
- 复杂网关治理和多集群流量策略。

## 后续待细化事项

- 具体代码目录与模块边界。
- 对外及内部接口定义。
- 数据结构、消息结构或存储结构。
- 配置项、运行参数与异常处理细节。
- 本阶段任务拆分、测试用例与实施顺序。
