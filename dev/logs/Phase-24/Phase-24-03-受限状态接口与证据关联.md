# Phase-24-03 实施日志：受限状态接口与证据关联

## 执行身份与阶段状态

- 执行分支：`develop/2.6.3`；从 `upstream/main` 基线 `75a83f4348f5accdad00b0ff1840e39439a99960` 创建。
- 目标版本：`2.6.3`；沿用 Phase-24-02 的 migration 15 和 v2 activation，没有新增 migration。
- 收口状态：本批实现、真实 business/observe 验证和固定门禁已完成；Phase-24-04 管理端页面与 Phase-24-05 完整 V1 冻结矩阵未交付。

## 实际交付

- 新增 business 私网 `GET /internal/v1/business-stages` 及 platform 公共 `GET /api/v1/observability/business-stages`。两者严格要求 epoch 和 command/event/post 三选一；公共接口增加有限 Logs/Trace 来源和不超过 24 小时窗口。
- business 使用每实例 2 个查证槽、500ms 数据库期限；platform 使用最多两个受信私网 origin、4 个 RPC 槽、单次不重试，遵守 2 秒业务 RPC 和 3 秒公共总期限。combined 直接装配领域服务，standalone platform 只使用窄 HTTP client。
- 私网接口同时要求专用服务凭证和真实管理会话；business 重新从 Cookie/session 与数据库读取角色，服务凭证恒时比较，平台不信任用户可控身份 header。错误角色、缺凭证、普通用户、未知参数和多对象选择均有拒绝覆盖。
- 阶段响应只返回 command/Outbox/consumer/completion/failure 的有界摘要、当前/请求 epoch、预算水位和逐来源质量；搜索可见性按当前 alias/物理 generation、epoch、MySQL 当前帖子事实和一次 typed ES 查询查证，不返回正文或任意 DSL。
- Logs 关联使用固定 read alias、确切身份、epoch、最多 100 条和 24 小时窗口；缺少 epoch 的旧日志只标记为历史线索。新增 command/data epoch、projection generation、routing revision、consumer 字段经过 Monitor/Marshaller strict validation、template、现有 mapping update 和 retention 检查；旧日志仍可读。
- 业务 post、Outbox、worker、search consumer 的日志/Span 保留既有 trace/span/attempt 身份并补充适用关联字段。Trace 不增加查询后端或伪造 ID；未配置查询能力按合同返回质量说明。
- 原生 observe 集成环境补齐 business-worker 和 search-indexer，使验收覆盖真实 outbox→broker→consumer→搜索/Logs 链路；业务/平台配置、Compose、lifecycle 凭据保护和运行合同同步更新。

## 固定验证

| 门禁 | 实际结果 |
| --- | --- |
| `rtk proxy make verify MODULE=backend BASE_REF=upstream/main` | 通过；vet、全量测试、race、build 通过，lint 存量 107 条，新增 0 条 |
| `rtk proxy make verify MODULE=monitor BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 51 条，新增 0 条 |
| `rtk proxy make verify MODULE=marshaller BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 68 条，新增 0 条 |
| `rtk proxy make verify MODULE=lifecycle BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 46 条，新增 0 条 |
| `rtk proxy make verify MODULE=devtools BASE_REF=upstream/main` | 通过；测试、race 通过，lint 存量 24 条，新增 0 条 |
| `rtk proxy make integration SCOPE=business` | 通过；真实 MySQL/RabbitMQ/Redis/Elasticsearch、migration 15、角色装配和业务回归通过 |
| `rtk proxy make integration SCOPE=observe` | 通过；真实 MySQL/RabbitMQ/Redis/Elasticsearch/Kafka/VictoriaMetrics/Monitor，受限阶段关联与既有 Metrics/Logs/Events 流程通过 |
| `rtk proxy make check-compose` | 通过；Compose 静态检查通过 |
| `rtk proxy make docs-check BASE_REF=upstream/main` | 通过；合同、链接和引用检查通过 |
| 版本、分支与前端回归 | 通过；版本同步校验、`develop/2.6.3` 分支治理校验，以及 frontend 76/admin-frontend 54 个测试、typecheck、lint、build 均通过；`npm ci` 仅报告既有依赖弃用和 3 个 high severity audit 告警，未升级依赖 |

## 偏差与处理

- 首次 observe 超时是因为原生 observe 集成环境只启动 API/观测传输进程，没有启动 business-worker/search-indexer；补齐两个受管进程及 readiness 后，search consumer 和当前索引代际可见性收敛。
- 第二次 observe 暴露 Logs 适配器把已解析的 command/event/post 三个身份同时传给“单 selector”查询；改为沿用调用方原始 selector，并用 businessstate 单元测试覆盖三种选择。修正后的 observe 最终运行通过。
- 期间一次独立 migration 集成出现基础设施连接失败，随后单独 migration 检查和 business 集成均通过；未将瞬时失败写成能力结论。

## 清理与限制

- business/observe 使用单一 owned Compose 项目；每次集成结束后仅清理本批创建的容器、网络和依赖资源，未执行全局 prune。临时诊断输出和 live 查询测试已从工作树移除。
- 本批没有管理端阶段页面、正式 V1 冻结矩阵、全站 Trace 查询后端、长期 Trace 存储或容量/HA 结论；Trace 仅保留已有 OTLP ID、未配置/采样质量表达。
- 当前接口是逐来源、非原子快照；Logs/ES/业务一侧失败时保留其他已授权来源，不把失败折算成业务成功、零值或 false。
