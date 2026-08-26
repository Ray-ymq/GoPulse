# Phase 8：Transformer与VictoriaMetrics

> 本文档是阶段开发提纲，具体接口、数据模型、消息格式和部署参数将在本阶段进入详细设计时补充。

## 阶段目标

完成从 Exporter 到 VictoriaMetrics 的第一条完整指标采集、传输、转换和存储链路。

本阶段完成后，应形成可独立运行和验证的结果，并能够直接支撑 Phase 9。

## 开发范围

- Kafka metrics 消费。
- Transformer 字段校验与映射。
- 指标格式转换与异常过滤。
- VictoriaMetrics 指标写入与查询。

## 核心任务

- 消费 Router 写入 Kafka 的 metrics 消息。
- 识别消息类型并执行第二次处理。
- 转换为 VictoriaMetrics 可接受的格式。
- 写入并查询实际组件指标。

## 前置依赖

- Phase 7 Message Router 与 Kafka 完成。
- 统一 metrics 消息可从 Kafka 消费。
- VictoriaMetrics 可用。

## 阶段产物

- 可运行的 Transformer。
- VictoriaMetrics 写入链路。
- 完整 Metrics 可观测闭环。

## 验收标准

- Exporter 指标最终可写入 VictoriaMetrics。
- 可查询到连接、请求、状态等实际采集指标。
- 异常数据不会破坏正常消费链路。
- Monitor 与 Transformer 的两层处理职责保持分离。

## 本阶段不做

- 日志和事件处理链路。
- 可观测前端。
- 复杂指标聚合、告警和长期容量设计。

## 后续待细化事项

- 具体代码目录与模块边界。
- 对外及内部接口定义。
- 数据结构、消息结构或存储结构。
- 配置项、运行参数与异常处理细节。
- 本阶段任务拆分、测试用例与实施顺序。
