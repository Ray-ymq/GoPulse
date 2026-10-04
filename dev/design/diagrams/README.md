# GoPulse 当前项目模块架构图

基于当前工作区的已完成产品版本 `2.2.4`，绘制日期为 2026-10-04。
使用 Excalidraw 原生元素和导出器生成，中文与英文采用手绘字体。

![GoPulse 当前项目模块架构](gopulse-modules.png)

- [高清 PNG](gopulse-modules.png)：4608 × 3424，可用于阅读与分享。
- [矢量 SVG](gopulse-modules.svg)：可放大查看模块文字和连线。
- [可编辑 Excalidraw 源文件](gopulse-modules.excalidraw)：可在 Excalidraw 中打开并调整。

图按前端与入口、业务与管理核心、业务状态与异步执行、可观测数据管道、运行交付与验证分组。
实线表示业务或观测数据流，虚线表示管理控制或授权查询，`×2` 表示当前计算层副本数。
图中 Backend 内部的业务卡片是模块，Outbox Dispatcher 也在 Backend 内运行；
Business Worker 和 Search Indexer 是同一业务代码库的独立运行进程。
Search Indexer 根据 MySQL 事实收敛业务 Elasticsearch 投影，该数据库读取关系写在卡片内。

实现事实核对来源：

- [当前版本](../../../VERSION)与[能力状态](../../status/capability-status.md)。
- [当前部署拓扑](../../../deploy/compose.yaml)与[同源入口配置](../../../deploy/docker/frontend/nginx.conf)。
- [Backend API 模块](../../../backend/internal/http/api.go)、[组件指标库](../../../componentmetrics)、[Exporter 模块](../../../exporters)。
- [Monitor](../../../monitor)、[Router](../../../router)、[Marshaller](../../../marshaller)、[生命周期工具](../../../lifecycle)。
- [顶层架构总纲](../../phases/GoPulse-高并发与可观测后续路线图.md)中的当前实现基础与目标差异。

此图描述当前 Compose 实现。Trace 卡片限定为已实现的单业务链路与验收文件 Collector。
总纲中的 Jaeger 产品查询、Helm、Kubernetes 与 KEDA 属于后续目标，未列为当前模块。
