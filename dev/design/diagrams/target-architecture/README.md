# GoPulse 目标架构图

绘制日期：2026-10-05。三张图是以下两份提案的配套图解，均标明“目标设计”，不表示已经实现，也不改变 Phase、版本或实施验收合同。

- [可观测架构设计](<../../../../docs/GoPulse 可观测架构设计.md>)：M1–M9、共享身份和环境资源合同。
- [高并发架构设计](<../../../../docs/GoPulse 高并发架构设计.md>)：B1–B10、进程职责和业务一致性边界。

| 图 | 高清 PNG | SVG | 可编辑源文件 |
| --- | --- | --- | --- |
| 目标整体架构 | [PNG](gopulse-target-overall.png) | [SVG](gopulse-target-overall.svg) | [Excalidraw](gopulse-target-overall.excalidraw) |
| 高并发业务目标架构 | [PNG](gopulse-target-business.png) | [SVG](gopulse-target-business.svg) | [Excalidraw](gopulse-target-business.excalidraw) |
| 可观测目标架构 | [PNG](gopulse-target-observability.png) | [SVG](gopulse-target-observability.svg) | [Excalidraw](gopulse-target-observability.excalidraw) |

使用原生矢量元素绘制，沿用已有图中的浅色分区、圆角框、手绘字体与彩色箭头；SVG 内嵌已有架构图使用的字体，PNG 从实际 SVG 渲染导出。两张 PNG 为 4600 × 3600，可观测图为 4600 × 3720。Excalidraw 文件中的文字、框和箭头均可编辑。

图例：蓝色实线表示业务调用或数据传输；绿色实线表示受限查询；紫色虚线表示配置、状态或资源预算。分区表示职责与运行边界，不表示固定 Kubernetes 节点、固定副本数或已经具备高可用。

三图共同遵守以下边界：

- M1 的业务 API 与平台 API 分开运行，Outbox dispatcher 独立扩容与排空。
- 业务事实与 Outbox 同事务提交；发布确认、消费者完成分别表达。
- 业务搜索 ES 与观测 ES 分开，缓存和搜索是派生结果。
- M2 维护期望配置与目录，M3 回报实际生效状态，M4 按来源策略接入。
- 指标、日志、观测事件通过 Router、Kafka 和 Marshaller；Span 走独立标准 OTLP 路径。
- M6 通过业务接口查证只读持久状态，不借由 Router 查询业务事实。
- M8 汇总整体环境资源合同；B10 提交业务预算，M2–M7 执行各自预算。
- M9 复用 M1/B2 的身份与角色事实，不另建账号库。

图中的主箭头用于说明关系，省略响应线和部分内部调用；它们不代表跨存储原子快照，也不代表所有遥测都会完整保存。架构细节与条件扩展仍以两份设计文档为准。
