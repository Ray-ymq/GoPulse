# Phase 9：LogMonitor与日志链路

> 本文档是阶段开发提纲，具体接口、数据模型、消息格式和部署参数将在本阶段进入详细设计时补充。

## 版本规划

- 版本线：`1.6.x`；阶段基线：`1.6.0`。
- 执行批次从 `1.6.1` 起编号，具体目标版本和开发分支由 Phase 9 总实施方案统一分配。

## 阶段目标

接入业务日志，完成从 Backend 到 Elasticsearch 的日志采集、传输、转换和查询链路。

本阶段完成后，应形成可独立运行和验证的结果，并能够直接支撑 Phase 10。

## 开发范围

- LogMonitor 被动接收。
- 日志第一次清洗与标准消息封装。
- Router 与 Kafka 日志传输。
- Marshaller 日志转换。
- Elasticsearch 日志存储与 Backend 查询。

## 核心任务

- 由 Backend 主动向 LogMonitor 推送日志。
- 优先使用简单 HTTP 接收方式。
- 复用 Message Router 与 Kafka 传输统一日志消息。
- 由 Marshaller 转换后写入独立日志索引。
- 通过 Backend 提供日志查询能力。

## 前置依赖

- Phase 8 Metrics 链路闭环完成。
- Phase 4 已提供结构化业务日志。
- Elasticsearch 可用。

## 阶段产物

- 可运行的 LogMonitor。
- 完整 Logs 可观测链路。
- 独立的日志索引。
- Backend 日志查询入口。

## 验收标准

- 调用 Backend API 产生的日志可最终进入 Elasticsearch。
- 日志可通过 Backend 查询。
- 帖子搜索索引与日志索引相互隔离。
- 日志源采用 Push，Metrics 继续采用 Pull。

## 本阶段不做

- EventMonitor。
- 前端日志分析页面的完整体验。
- 复杂日志采集协议与日志分析能力。

## 后续待细化事项

- 具体代码目录与模块边界。
- 对外及内部接口定义。
- 数据结构、消息结构或存储结构。
- 配置项、运行参数与异常处理细节。
- 本阶段任务拆分、测试用例与实施顺序。
