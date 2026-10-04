# GoPulse Excalidraw 风格 AI 图 v2

- 生成方式：按用户指定的 imagegen 技能，调用内置 `image_gen`。
- 当前实现基线：`VERSION=2.2.4`。
- 最终图片：[gopulse-modules-ai-v2.png](../../../docs/diagrams/gopulse-modules-ai-v2.png)。
- 输出为 AI 生成的位图，分辨率 1536 × 1024。
- 实际处理：全新生成一次；随后对存储访问、事务 Outbox 与 Worker 回写连接进行局部修正和残线清理。
- 已目视核对：五个模块分区、主要中文名称、五类双副本服务、六类 Exporter、两条消息链路、两套独立 Elasticsearch 及当前单链路 Trace 范围。
- 目视局限：Worker 左侧仍有一段无箭头折线残笔；关键回写方向以指向主 MySQL 的外侧箭头及卡片文字为准。该位图用于模块概览，精确接口合同以代码和实现文档为准。

## 初始生成提示词

```text
Use case: infographic-diagram.
Asset type: a high-resolution Chinese modular architecture diagram of the current GoPulse software project, generated as an image.
Primary request: Redraw the entire diagram from scratch in unmistakable EXCALIDRAW style.

STYLE IS CRITICAL: match the clean visual language of Excalidraw precisely. Pure white canvas. Thin, slightly irregular charcoal pencil outlines, occasionally subtly doubled. Simple rounded rectangles with extremely pale FLAT pastel fills and some light sparse hatching. Excalidraw Virgil-style English hand lettering and clear Xiaolai-like Chinese hand lettering. Thin clean hand-drawn arrows with sharp arrowheads and sensible right-angle routing. Plenty of white space. Restrained strokes, equal card spacing, large readable labels. This should look like an engineer's carefully organized Excalidraw architecture whiteboard.
AVOID thick marker lettering, calligraphy, watercolor, gradients, brush texture, glow, 3D, realistic materials, shadows, illustration flourishes, dense tiny writing, arrows across text, clipart, mascots, logos, UI chrome, or watermarks.

Composition: generous wide landscape canvas. Title at the upper left and small version at upper right. Three modular panels across the upper half: blue frontend, green business core, pale amber business data and background execution. A large lavender observability pipeline in the lower middle. Four neutral supporting cards along the bottom. Each module name is clearly larger than its concise role description. Do not squeeze long text into narrow boxes.

Exact title: "GoPulse · 当前项目模块架构"
Exact version: "v2.2.4"
Exact section titles:
"01 前端与同源入口"
"02 业务与管理核心"
"03 业务状态与异步执行"
"04 可观测数据管道"
"05 运行、交付与验证"

CONTENT:
01:
"用户 / 管理员" → "Nginx · 同源入口".
Two frontend cards below Nginx:
"社交端" with "内容 / 互动 / 关系 / 搜索 / 通知" and "frontend/".
"管理端" with "概览 / 告警 / 指标 / 日志 / 事件 / 插件" and "admin-frontend/".
Small shared card: "共享展示与样式" / "frontend-shared/".
Note: "共享身份体系 · HttpOnly Cookie".
Nginx → Backend arrow labeled "/api/v1"; Nginx to social page labeled "/"; Nginx to management page labeled "/admin/".

02:
One boundary containing "Backend ×2 · Go / Gin" with "认证 / 角色校验 / 有界请求".
Six small module cards within the same Backend boundary:
"身份与关系" / "auth · user"
"内容与互动" / "post · comment · like · bookmark"
"搜索与通知" / "search · notification"
"可靠投递" / "事务 Outbox · 租约 / 重试 / 确认"
"观测查询" / "Metrics / Logs / Events"
"管理与告警" / "概览 / 角色 / 审计 / 插件 / 告警"
Keep the management module on the lower left and observability queries in the lower middle so their outbound dashed connectors are clear.
Small note: "同一业务模型 · 不同运行进程".

03:
Three storage cards:
"MySQL" / "唯一业务事实源" / "业务 / Outbox / 通知 / 权限 / 审计"
"Redis" / "Cache-Aside 缓存" / "失败回退 MySQL"
"业务 Elasticsearch" / "可重建搜索投影" / "独立服务与持久卷"
Backend connects to all three stores.
Below them the business asynchronous execution chain:
"MySQL 事务 + Outbox" → "Outbox Dispatcher" (very clear subtitle: "Backend 内部运行") → "RabbitMQ".
RabbitMQ subtitle: "业务事件 · 通知 / 搜索队列" / "ACK / 重试 / 死信".
RabbitMQ forks into two worker cards:
"Business Worker ×2" / "幂等消费 · 通知写回 MySQL"
"Search Indexer ×2" / "根据 MySQL 事实收敛搜索投影".
Draw a continuous output connector FROM Business Worker TO MySQL.
Draw a continuous output connector FROM Search Indexer TO business Elasticsearch, routed through free space to the right of RabbitMQ. This connector MUST visibly start at Search Indexer and end at business Elasticsearch. RabbitMQ must have NO direct arrow to Elasticsearch.
The Outbox Dispatcher belongs to Backend, not an additional independently deployed service.

04:
One clean left-to-right data pipeline with short module labels:
A stacked group titled "采集来源" at the left:
"Exporter ×6" and "Redis / MySQL / RabbitMQ" plus "Kafka / Elasticsearch / VictoriaMetrics".
"组件运行指标" and "Backend / Worker / Indexer" plus "Monitor / Router / Marshaller".
"应用 Logs + 插件 Events".
All these sources → "Monitor" → "Message Router ×2" → "Kafka" → "Marshaller ×2".
Monitor: "插件唯一所有者" / "进程 / 配置 / Secret" / "周期采集 / Logs / Events".
Message Router: "鉴权 / Envelope 校验" / "等待 Kafka ACK".
Kafka: "可观测数据总线" / "1 Topic / 4 分区".
Marshaller: "消费组 / 手动 Offset" / "二次校验 / 转换 / 保留清理".
Marshaller forks into two separate stores on the right:
"VictoriaMetrics" / "Metrics 时序存储" / "原生 30d 保留策略"
"观测 Elasticsearch" / "Logs / Events" / "UTC 日索引保留 7 日" / "独立服务与持久卷".
Important cross-panel dashed connectors:
Backend's "管理与告警" card → Monitor, labeled "插件管理".
Backend's "观测查询" card → both observability stores, labeled "授权查询 / 告警评估".
Keep these dashed connectors in whitespace between the upper and lower panels, with no crossing through text.

05, four modest horizontal support cards:
"deploy/" / "Docker Compose / OCI 镜像 / 持久卷" / "edge / business / observability 网络"
"lifecycle/" / "Bundle 安装 / 启停 / 验证 / 备份恢复"
"scripts/ + loadtest/" / "单元 / 集成 / 浏览器验收" / "容量 / 故障 / 原始证据"
"单业务链路 Trace" / "发帖 → Outbox → RabbitMQ → Indexer" / "OpenTelemetry / OTLP → 验收文件 Collector"

Small footer legend: "实线：数据流    虚线：管理 / 授权查询    ×2：当前计算层副本数"
Scope note: "当前实现基线：v2.2.4 · Docker Compose"

Correctness constraints: Business facts belong to MySQL. Redis is replaceable cache. Business Elasticsearch is a rebuildable projection and is separate from observability Elasticsearch. RabbitMQ transports business events; Kafka transports Metrics, Logs, and Events. Five computing services have two replicas: Backend, Business Worker, Search Indexer, Message Router, Marshaller. Monitor exclusively owns exporter lifecycles. There are exactly six official exporter types. State stores are not shown as highly available. No Kubernetes, Helm, KEDA, Jaeger, Grafana, Prometheus server, or production Trace query store. Include only the stated implemented modules. Verbatim spellings, clear readable Chinese, correct continuous directional connectors, no cropped elements.
Output: one complete, polished, clean Excalidraw-style image.
```

## 存储访问与回写连线修正

```text
Use case: precise-object-edit.
Input image: edit target, the full GoPulse Excalidraw-style architecture diagram.
Preserve the thin Excalidraw pencil style, all boxes, all text, all colors, all panels, and all existing correct connectors.

Correct ONLY the database access connectors in the upper-right business panel, as follows:
1. The curved horizontal access bus ABOVE the MySQL, Redis, and business Elasticsearch boxes currently has arrowheads into all three stores, but its source is not visibly connected to Backend. Add a thin clean connector FROM the right edge of the main "Backend ×2 · Go / Gin" box TO that existing access bus. Route it through the narrow empty gap between the green and amber panels and the whitespace just under the amber panel heading. All three existing bus arrowheads must continue pointing INTO the three data stores. The common source must visibly be Backend.
2. Between the blue MySQL database box and the small lavender "MySQL 事务 + Outbox" box directly below it, reverse the vertical arrow so it points DOWN from MySQL INTO "MySQL 事务 + Outbox", then preserve the existing rightward chain from that box to Outbox Dispatcher and RabbitMQ.
3. The black return line from the left edge of "Business Worker ×2" currently ends at the bottom of "MySQL 事务 + Outbox". Reroute that return line so it points directly INTO the main blue "MySQL" database box instead. Route the line through empty space along the far left inside the amber business panel, avoiding every box and text. The output must clearly be Business Worker → MySQL, with the arrowhead touching the main MySQL database box. It must not connect to the transaction/Outbox operation box.

Keep the correct RabbitMQ → Business Worker, RabbitMQ → Search Indexer, and Search Indexer → business Elasticsearch arrows unchanged. Keep all lower observability connections and all labels unchanged. Do not add nodes. Do not change wording. Do not crop or alter the canvas. This is a connector-only correction.
```

## 重复回写线清理

```text
Use case: precise-object-edit.
Input image: edit target, full GoPulse architecture image, native size 1536 x 1024.

DELETE EXACTLY ONE SHORT CONNECTOR. No redrawing of the diagram.
In upper-right panel 03, the "Business Worker x2" box currently has TWO black return lines leaving its left side.
KEEP the larger OUTER return line: it leaves Worker around (1072,483), runs left to roughly (975,483), then upward to the left edge of the MAIN blue MySQL box around (975,225). This is the correct direct Business Worker → MySQL connection.
ERASE only the smaller INNER L-shaped line: it leaves Worker around (1072,469), runs left to roughly (1022,469), then upward to roughly (1022,384), with an arrowhead touching the bottom of the small lavender "MySQL 事务 + Outbox" box. Erase the entire inner L line and its arrowhead, restoring the plain white background beneath it.
After the edit Business Worker must have only ONE outgoing return connection, the outer one to the main MySQL storage box. It must not point to the small MySQL transaction/Outbox operation box.
Keep the MAIN MySQL → "MySQL 事务 + Outbox" downward arrow ABOVE the lavender operation box unchanged.
Preserve every other pixel as closely as possible: all text, all other lines, all boxes, all colors, the Excalidraw pencil style, and the canvas. Do not add or shift anything.
```
