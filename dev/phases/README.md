# GoPulse 阶段性开发文档

本目录将 [`Plan.md`](Plan.md) 中的总体实施计划拆分为 Phase 0～Phase 16 的阶段开发提纲。第一轮文档只固定各阶段的目标、边界、依赖、产物和验收方向；具体接口、数据模型、消息格式、目录细节及部署参数将在后续逐阶段讨论后补充。

## 阶段索引

| 阶段 | 版本线 | 文档 | 主要结果 | 里程碑 |
| --- | --- | --- | --- | --- |
| Phase 0 | `0.1.x` | [工程骨架](Phase-00/Phase-00-工程骨架.md) | 建立可长期演进的项目结构、本地开发环境以及前后端最小可运行链路 | Milestone 1：可用业务系统 |
| Phase 1 | `0.2.x` | [最小业务闭环](Phase-01-最小业务闭环.md) | 将 GoPulse 建设为具备用户、帖子、评论和点赞能力的最小可用社交平台 | Milestone 1：可用业务系统 |
| Phase 2 | `0.3.x` | [业务异步化](Phase-02-业务异步化.md) | 引入 RabbitMQ，将核心同步业务与业务完成后的异步动作分离 | Milestone 1：可用业务系统 |
| Phase 3 | `0.4.x` | [Elasticsearch与业务搜索](Phase-03-Elasticsearch与业务搜索.md) | 让 Elasticsearch 首先服务真实业务搜索，提供帖子标题与正文的全文检索能力 | Milestone 1：可用业务系统；完成后发布 `1.0.0` |
| Phase 4 | `1.1.x` | [业务日志基础](Phase-04-业务日志基础.md) | 统一 Backend 业务日志，为后续 LogMonitor 与日志处理链路提供稳定数据源 | Milestone 2：指标采集链路 |
| Phase 5 | `1.2.x` | [Exporter Plugin原型](Phase-05-Exporter-Plugin原型.md) | 实现首个独立指标采集插件，验证常驻、被动拉取的 Exporter 工作模式 | Milestone 2：指标采集链路 |
| Phase 6 | `1.3.x` | [Monitor](Phase-06-Monitor.md) | 实现 Monitor 的 MetricsMonitor 与基础 Plugin Manager，建立指标采集和插件管理能力 | Milestone 2：指标采集链路 |
| Phase 7 | `1.4.x` | [Message Router与Kafka](Phase-07-Message-Router与Kafka.md) | 建立统一可观测消息入口，使 MetricsMonitor 通过 Message Router 将消息写入 Kafka | Milestone 2：指标采集链路 |
| Phase 8 | `1.5.x` | [Marshaller与VictoriaMetrics](Phase-08-Marshaller与VictoriaMetrics.md) | 完成从 Exporter 到 VictoriaMetrics 的第一条完整指标采集、传输、转换和存储链路 | Milestone 2：指标采集链路 |
| Phase 9 | `1.6.x` | [LogMonitor与日志链路](Phase-09-LogMonitor与日志链路.md) | 接入业务日志，完成从 Backend 到 Elasticsearch 的日志采集、传输、转换和查询链路 | Milestone 3：完整可观测平台 |
| Phase 10 | `1.7.x` | [EventMonitor与事件链路](Phase-10-EventMonitor与事件链路.md) | 补齐 Events 数据类型，记录插件生命周期、采集失败和系统运行中的离散事件 | Milestone 3：完整可观测平台 |
| Phase 11 | `1.8.x` | [可观测前端](Phase-11-可观测前端.md) | 将 Metrics、Logs、Events 和 Exporter 管理能力通过统一的 GoPulse 页面提供给用户 | Milestone 3：完整可观测平台 |
| Phase 12 | `1.9.x` | [Docker化](Phase-12-Docker化.md) | 为所有自研组件建立标准容器运行方式，并通过 Docker Compose 启动完整 GoPulse | Milestone 4：云原生化 |
| Phase 13 | `1.10.x` | [Kubernetes基础部署](Phase-13-Kubernetes基础部署.md) | 将完整 Docker Compose 环境迁移到 1 Master、3 Worker 的 Kubernetes 集群 | Milestone 4：云原生化 |
| Phase 14 | `1.11.x` | [Ingress与统一入口](Phase-14-Ingress与统一入口.md) | 为 Kubernetes 中的 GoPulse 提供统一 HTTP 访问入口，并收敛外部暴露面 | Milestone 4：云原生化 |
| Phase 15 | `1.12.x` | [Kubernetes可观测闭环](Phase-15-Kubernetes可观测闭环.md) | 让运行在 Kubernetes 中的 GoPulse 通过自身可观测系统观测业务、基础组件和集群对象 | Milestone 4：云原生化 |
| Phase 16 | `1.13.x` | [稳定性与工程化](Phase-16-稳定性与工程化.md) | 在主要架构闭环后统一补齐配置、退出、健康检查、错误处理和消息消费可靠性 | 工程化收尾 |

## 推荐阅读顺序

按照 Phase 0 → Phase 16 顺序阅读和实施。每个阶段均以前序阶段的可运行产物为基础，并应在进入下一阶段前完成本阶段验收。

总体演进顺序为：

```text
工程骨架
→ 最小业务闭环
→ 业务异步与搜索
→ 指标、日志、事件可观测链路
→ Docker 与 Kubernetes
→ Kubernetes 自观测闭环
→ 稳定性与工程化
```

## 关键职责边界

- MySQL 是核心业务事实来源；Redis 仅承担明确的缓存用途；Elasticsearch 的业务索引用于搜索且应可重建。
- RabbitMQ 只用于点赞、评论、通知等业务异步任务；Kafka 只用于 Metrics、Logs、Events 等可观测数据传输。
- Exporter 负责目标组件的定制化指标采集，不保存历史数据，也不主动向 Monitor 推送指标。
- Monitor 负责采集或接收、基础校验、基础结构化和标准消息封装。
- Message Router 只负责接收、识别类型、路由和写入 Kafka，不承担清洗、转换、聚合或存储。
- Marshaller 负责第二次处理，将 Kafka 消息校验、清洗并转换为目标存储格式。
- Frontend 只负责页面、交互和状态展示；查询、采集控制及插件管理等核心逻辑由 Go Backend 负责。

## 文档维护约定

- `Plan.md` 是总体阶段划分与架构边界的上层依据；阶段文档不得与其核心原则冲突。
- 每个 Phase 文档声明本阶段版本线；具体执行批次、目标版本和开发分支只在该 Phase 的总实施方案中统一规划，不要求各实施文件重复声明。
- Phase 的执行批次数量或顺序调整时，必须先同步更新总实施方案中的版本与分支分配，再创建尚未开始的开发分支。
- 第一轮不确定公开 API、数据库 Schema、Kafka Topic、RabbitMQ Queue 或 Kubernetes 资源规格。
- 后续讨论某个 Phase 时，只细化对应文档，并同步检查其与前后阶段的依赖关系。
- 新增技术细节时应同时更新验收标准和“不做事项”，避免范围无意扩张。
- 阶段完成后，应在对应文档中记录最终决策或链接到独立详细设计文档。
