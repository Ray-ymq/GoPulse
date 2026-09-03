# Phase 7：Message Router与Kafka

> 本文档是阶段开发提纲，具体接口、数据模型、消息格式和部署参数将在本阶段进入详细设计时补充。

## 版本规划

- 版本线：`1.4.x`；阶段基线：`1.4.0`。
- 执行批次从 `1.4.1` 起编号，具体目标版本和开发分支由 Phase 7 总实施方案统一分配。

## 阶段目标

建立统一可观测消息入口，使 MetricsMonitor 通过 Message Router 将消息写入 Kafka。

本阶段完成后，应形成可独立运行和验证的结果，并能够直接支撑 Phase 8。

## 里程碑定位与 MVP 贡献

- 所属里程碑：Milestone 2，指标采集 MVP。
- 本阶段为统一 metrics 消息建立与 Kafka 解耦的传输通道，使 Monitor 不依赖 Kafka SDK，并为下游 Marshaller 提供可消费输入。
- metrics 消息的完整传输和职责边界是当前里程碑的必需增量；清洗、存储和长期 Topic 治理按`本阶段不做`延后。

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

## 用户态与访问边界

- 本阶段是可观测内部数据通道，不新增普通用户或管理员直接调用的 Router/Kafka 公共接口。
- Monitor 使用独立服务身份向 Message Router 发布；Router 接收端必须具有服务间鉴权、请求上限和受控监听/网络边界，不能复用用户 Cookie 作为内部服务凭据。
- 普通用户和管理员浏览器都不得直连 Router 或 Kafka；未来可观测页面只能通过 Backend 的管理员 API 间接访问数据能力。
- Router 或 Kafka 不可用不得改变普通用户社交 API 的认证与可用性；发布失败只按可观测链路契约处理。

## 阶段产物

- 可独立运行的 Message Router。
- Monitor 到 Router 再到 Kafka 的链路。
- 可供下游消费的完整 metrics 消息。

## 最小端到端闭环

`MetricsMonitor 标准消息 → Message Router 类型识别与路由 → Kafka → 验证 Consumer 读取完整消息`

该闭环必须证明 Monitor 与 Kafka SDK 解耦且 Router 不改写业务字段，不能只验证 Kafka 或 Router 的孤立可用性。

## 验收标准

- MetricsMonitor 不直接依赖 Kafka SDK。
- Kafka Consumer 可读取完整 metrics 消息。
- Message Router 不修改业务字段。
- Kafka 仅用于 Metrics、Logs、Events 等可观测数据。
- 未经内部服务鉴权的 Router 请求被拒绝，Router/Kafka 不形成普通用户或浏览器可访问入口。
- Router/Kafka 故障不破坏既有社交业务闭环或放宽任何用户角色权限。

## 阶段完成与停止条件

- 最小端到端闭环及全部阶段验收标准通过，标准 metrics 消息能够稳定成为下游消费输入。
- 对应实施记录已如实记录改动、验证、偏差和后续事项，固定完成门槛通过且没有阻断验收的失败。
- 达到上述条件后停止扩展；清洗、存储和长期 Topic 规划留给后续 Phase，阶段产物作为 Phase 8 的输入。

## 本阶段不做

- 字段转换、数据清洗和聚合。
- 存储逻辑。
- RabbitMQ 业务任务。
- 提前确定长期 Topic 拆分策略。

## 总实施方案生成与批次切分约束

- 默认规划 1～2 个实现批次；需要跨批验证时，再安排 1 个集成验收与阶段收口批次，总计尽量控制在 2～3 个批次。
- 超过 3 个批次时，总实施方案必须记录具体的风险隔离、依赖关系或独立交付理由。
- 实现批次按 Monitor 到 Kafka Consumer 的传输闭环切分，不按 Router、Kafka、Producer、Consumer 和测试等技术层机械拆分。
- 测试、文档和实施记录随对应能力完成；如安排收口批次，只执行跨批集成和固定验收，不加入新的功能范围。
- Router 接口、消息 Envelope、Topic 和运行参数由总实施方案根据实施前的真实代码基线确定，阶段提纲不提前冻结。
- 总实施方案必须保留本阶段内部服务身份、非浏览器入口和社交业务故障隔离边界。

## 后续待细化事项

- 具体代码目录与模块边界。
- 对外及内部接口定义。
- 数据结构、消息结构或存储结构。
- 配置项、运行参数与异常处理细节。
- 本阶段任务拆分、测试用例与实施顺序。
