# Phase 7：Message Router与Kafka

> 本文档是阶段开发提纲，具体接口、数据模型、消息格式和部署参数将在本阶段进入详细设计时补充。

## 版本规划

- 版本线：`1.4.x`；阶段基线：`1.4.0`。
- 执行批次从 `1.4.1` 起编号，具体目标版本和开发分支由 Phase 7 总实施方案统一分配。

## 阶段目标

建立统一可观测消息入口，使 MetricsMonitor 通过 Message Router 将消息写入 Kafka。

本阶段完成后，应形成可独立运行和验证的结果，并能够直接支撑 Phase 8。

## 开发范围

- Message Router 服务。
- Kafka 可观测数据通道。
- 消息类型识别与路由。
- metrics 消息端到端传输。

## 核心任务

- 让 Monitor 将统一消息发送到 Message Router。
- 由 Router 识别消息类别并选择 Kafka 目标。
- 将消息原样写入 Kafka。
- 通过 Consumer 验证消息完整性。

## 前置依赖

- Phase 6 Monitor 完成。
- MetricsMonitor 可产生统一消息。
- Kafka 可用。

## 阶段产物

- 可独立运行的 Message Router。
- Monitor 到 Router 再到 Kafka 的链路。
- 可供下游消费的完整 metrics 消息。

## 验收标准

- MetricsMonitor 不直接依赖 Kafka SDK。
- Kafka Consumer 可读取完整 metrics 消息。
- Message Router 不修改业务字段。
- Kafka 仅用于 Metrics、Logs、Events 等可观测数据。

## 本阶段不做

- 字段转换、数据清洗和聚合。
- 存储逻辑。
- RabbitMQ 业务任务。
- 提前确定长期 Topic 拆分策略。

## 后续待细化事项

- 具体代码目录与模块边界。
- 对外及内部接口定义。
- 数据结构、消息结构或存储结构。
- 配置项、运行参数与异常处理细节。
- 本阶段任务拆分、测试用例与实施顺序。
