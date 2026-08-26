# Phase 10：EventMonitor与事件链路

> 本文档是阶段开发提纲，具体接口、数据模型、消息格式和部署参数将在本阶段进入详细设计时补充。

## 阶段目标

补齐 Events 数据类型，记录插件生命周期、采集失败和系统运行中的离散事件。

本阶段完成后，应形成可独立运行和验证的结果，并能够直接支撑 Phase 11。

## 开发范围

- EventMonitor 被动接收。
- 统一事件模型。
- Router 与 Kafka 事件传输。
- Transformer 事件转换。
- Elasticsearch 事件存储。

## 核心任务

- 选择插件启停、采集失败等真实事件源。
- 接收并基础校验事件。
- 封装统一 events 消息并经 Router 写入 Kafka。
- 由 Transformer 转换并写入 Elasticsearch。
- 提供事件查询基础能力。

## 前置依赖

- Phase 9 日志链路完成。
- Message Router、Kafka、Transformer 与 Elasticsearch 可复用。

## 阶段产物

- 可运行的 EventMonitor。
- 统一事件数据。
- 完整 Events 可观测链路。
- 事件查询能力。

## 验收标准

- 插件启动、停止或采集失败可产生事件。
- 事件可通过完整链路进入 Elasticsearch。
- 事件可被查询并表达来源、严重程度、时间和描述。
- EventMonitor 保持被动接收模式。

## 本阶段不做

- 指标告警系统。
- 复杂事件关联分析。
- Kubernetes 原生事件的全面采集。
- 可观测前端。

## 后续待细化事项

- 具体代码目录与模块边界。
- 对外及内部接口定义。
- 数据结构、消息结构或存储结构。
- 配置项、运行参数与异常处理细节。
- 本阶段任务拆分、测试用例与实施顺序。
