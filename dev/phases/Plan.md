# GoPulse 项目实施计划

## 1. 项目目标

GoPulse 是一个以 Go 为核心、可独立运行并可部署到 Kubernetes 的社交内容与可观测平台。Kubernetes 是最终部署环境，不是业务系统或可观测系统成立的前提。

项目最终由两部分组成：

- **基础业务系统**：提供用户资料、关注、Following 信息流、帖子、评论、点赞、收藏、通知与搜索。
- **可观测系统**：负责指标、日志、事件的采集、传输、清洗、存储、查询与内部告警。

项目不从一开始追求完整微服务化、复杂高可用或大规模生产能力，而是采用阶段式实现：

```text
基础业务可运行
    ↓
Metrics / Logs / Events 可观测链路
    ↓
业务系统、插件、告警与双前端产品闭环
    ↓
跨平台交付与工程质量验收
    ↓
Kubernetes 部署、统一入口与集群观测
```

每个阶段必须满足两个条件：

1. 当前阶段本身能够独立运行和验证。
2. 当前阶段产物能够直接作为下一阶段的基础，不推倒重来。

## 1.1 执行平台与兼容边界

- Phase 0 与 Phase-01-01 已按原跨平台策略完成，原生 Windows PowerShell 与 Bash 开发入口的共同能力基线截至产品版本 `0.2.1`；Phase-01-02 至 Phase 12 已在 WSL2/Linux 与 Bash 主路径完成。
- Phase 13～Phase 15 先在既有 Compose 基线上补齐业务、插件、告警和双前端产品能力；这些能力不得依赖 Kubernetes 才能运行或验收。
- Phase 16 是明确的跨平台产品化阶段。它必须以真实环境验证至少 Linux `amd64`、macOS `arm64` 与 Windows `amd64`，不能只靠交叉编译、Compose 静态解析或 WSL 内运行宣称 macOS/Windows 支持。
- macOS 与 Windows 的产品运行方式以 Linux 容器和共享生命周期实现为基础；不要求 MySQL、Kafka、Elasticsearch 或 GoPulse 自研组件成为原生 Windows Service 或 macOS LaunchDaemon。
- 用户应能从 macOS Terminal 与 Windows PowerShell/Terminal 调用受支持入口。现有 `scripts/*.ps1` 继续作为 `0.2.1` 历史快照，不扩展为与 Bash 重复的第二套编排实现；如有必要，可由 Phase 16 新增名称明确的薄启动器。
- Phase 17 在完整 Compose 产品上完成 Kubernetes 前的工程质量验收。
- Phase 18 至 Phase 20 才进入 Kubernetes 实施，使用 WSL2/Linux 作为主环境，并在直接影响镜像或用户入口时回归 Phase 16 已交付的多架构与跨平台契约；不要求 Kubernetes 集群本身原生运行在 macOS 或 Windows。
- 平台适配不降低业务、数据、安全、故障恢复、Linux CI、Docker 或 Kubernetes 验收标准。

## 1.2 用户态与访问边界

- GoPulse 只有一套用户、注册、登录和会话系统，但从 Phase 15 起提供两个独立 Frontend 应用：普通用户端与超级管理员端。
- 最终角色只有 `user` 与 `super_admin`。系统保留一个不可删除、不可降级的引导超级管理员，并允许多个超级管理员；任一超级管理员可按用户 ID 调整非引导账号角色。
- 同一域名提供统一登录入口，Backend 根据数据库当前角色决定进入哪个 Frontend；作者摘要、帖子、评论、通知、搜索和公开缓存不得暴露角色。
- Metrics、Logs、Events、Alerts 查询及 Exporter 管理全部属于超级管理员能力，必须由 Backend 授权；未登录返回 `401`，普通用户返回 `403 permission_denied`。
- 两个 Frontend 的分流、导航和路由守卫只负责体验，不能替代 Backend 授权。
- Monitor、Message Router、Marshaller、Kafka、VictoriaMetrics、Elasticsearch、数据库和 Kubernetes 内部接口不面向浏览器，只接受独立服务身份并保持受控网络边界。
- 可观测链路故障不得不必要地阻断普通用户社交业务；Phase 6～Phase 20 的总实施方案、验收和部署必须持续验证身份隔离、内部服务不暴露和安全错误响应。

---

# 2. 总体技术栈

## 前端

- TypeScript
- Vue 3
- 独立用户 Frontend
- 独立管理 Frontend

## 后端

- Go
- Gin

## 业务基础设施

- MySQL
- Redis
- RabbitMQ
- Elasticsearch

## 可观测基础设施

- Kafka
- VictoriaMetrics
- Elasticsearch

## 自研组件

- Go Backend
- Monitor
  - MetricsMonitor
  - LogMonitor
  - EventMonitor
- Message Router
- Marshaller
- Exporter Plugins

## 运行与部署环境

- Docker
- Kubernetes
- 1 Master + 3 Worker

规划节点：

```text
Master
└── Kubernetes Control Plane

Worker-1
├── 用户 Frontend
├── 管理 Frontend
└── Go Backend

Worker-2
├── MySQL
├── Redis
└── RabbitMQ

Worker-3
├── Kafka
├── VictoriaMetrics
├── Elasticsearch
├── Monitor
├── Message Router
├── Marshaller
└── Exporter Plugins
```

节点分布属于项目最终部署目标，不作为前期开发的强制约束。

---

# 3. 实施原则

## 3.1 先完成产品闭环，再迁移部署环境

GoPulse 首先必须是一个能够使用的社交平台。

如果没有真实业务流量、业务日志和基础组件，可观测系统本身没有实际观测对象。

既有可观测链路建立后，必须继续补齐业务、插件、告警和用户界面，先证明整个产品在 Compose 中能够独立运转。Kubernetes 只负责把同一产品迁移到新的部署环境，不允许承接尚未完成的产品功能。

因此后续顺序固定为：

```text
既有业务与可观测链路
→ 业务基础系统闭环
→ 插件与组件可观测闭环
→ 内部告警与独立管理端
→ 跨平台双前端产品
→ 工程质量验收
→ Kubernetes 部署与集群观测
```

---

## 3.2 RabbitMQ 与 Kafka 严格分工

RabbitMQ 只用于业务异步任务，例如：

```text
点赞
评论
通知
部分异步写入
```

Kafka 只承担可观测数据传输：

```text
Metrics
Logs
Events
```

禁止把 RabbitMQ 和 Kafka 混成统一消息系统。

---

## 3.3 Message Router 不承担数据清洗

Message Router 的职责只有：

```text
接收数据
→ 判断消息类别
→ 路由
→ 写入 Kafka
```

禁止承担：

- 指标计算
- 字段转换
- 数据聚合
- 数据清洗
- 存储逻辑

---

## 3.4 Monitor 与 Marshaller 分层处理

Monitor 完成第一次处理：

```text
采集 / 接收
→ 基础校验
→ 基础结构化
→ 标准消息封装
```

Marshaller 完成第二次处理：

```text
Kafka 消息
→ 类型识别
→ 字段转换
→ 数据清洗
→ 输出目标格式
```

最终：

```text
metrics → VictoriaMetrics
logs    → Elasticsearch
events  → Elasticsearch
```

---

## 3.5 前端不承担核心业务逻辑

前端只负责：

- 页面展示
- 用户交互
- 请求 Backend
- 展示执行状态

组件管理、插件下发、采集控制、数据查询等逻辑全部由 Go Backend 负责。

用户 Frontend 与管理 Frontend 是两个独立应用，但共用 Backend、身份数据和同源会话。可观测页面只向超级管理员开放；登录分流、导航和路由守卫不能替代 Backend 授权。

---

## 3.6 以里程碑 MVP 驱动实施与切分

GoPulse 的五个里程碑分别交付一次可独立运行、验证和使用的递进式 MVP。后一个 MVP 继承前一个 MVP 的能力并增加新的完整闭环，不为追求最终架构而扩大当前里程碑范围：

- Milestone 1 交付业务系统 MVP，对应 Phase 0～Phase 3。
- Milestone 2 交付指标采集 MVP，对应 Phase 4～Phase 8。
- Milestone 3 交付完整可观测 MVP，对应 Phase 9～Phase 11。
- Milestone 4 交付完整可交付产品 MVP，对应 Phase 12～Phase 17：业务、可观测、插件、告警、双前端、跨平台和工程质量全部在 Kubernetes 之前闭环。
- Milestone 5 交付 Kubernetes 部署与自观测 MVP，对应 Phase 18～Phase 20，只迁移和观察已经完成的产品。

每个 Phase 的总实施方案必须优先形成当前阶段对所属里程碑的最小端到端贡献，并遵循以下切分规则：

- 默认安排 1～2 个实现批次；需要跨批验证时，再安排 1 个集成验收与阶段收口批次，总批次数尽量控制在 2～3 个。
- 超过 3 个批次时，必须在总实施方案中记录具体的风险隔离、依赖关系或独立交付理由。
- 实现批次按可运行、可验证的端到端能力切分，不按数据库、Backend、Frontend、测试等技术层机械拆分。
- 测试、文档和实施记录随对应能力完成；如安排收口批次，只执行跨批集成和固定验收门槛，不引入新的功能范围。
- 阶段验收通过且没有阻断验收的失败后立即停止；非阻塞优化和非当前 MVP 必需内容进入后续事项。

阶段提纲只固定 MVP 贡献、能力边界、最小闭环和验收方向。公开 API、数据库 Schema、消息契约和部署参数由对应总实施方案根据实施前的真实代码基线确定。

---

# 4. Phase 0：工程骨架

## 目标

建立可以长期演进的项目代码结构和本地开发环境。

此阶段不实现完整业务。

## 实现范围

建立顶层项目结构，例如：

```text
gopulse/
├── frontend/
├── backend/
├── monitor/
├── router/
├── marshaller/
├── exporters/
├── deploy/
├── docs/
└── scripts/
```

Backend 建立最小 Gin 服务：

```text
GET /health
```

Frontend 建立最小 Vue 页面。

建立本地基础设施：

```text
MySQL
Redis
RabbitMQ
```

优先通过 Docker Compose 启动。

## 阶段产物

```text
Frontend
   ↓
Backend
   ↓
MySQL / Redis / RabbitMQ
```

全部能够启动。

## 验收标准

- 前端可以访问。
- Backend `/health` 正常返回。
- Backend 可以连接 MySQL。
- Backend 可以连接 Redis。
- Backend 可以连接 RabbitMQ。
- 项目具有统一配置文件或环境变量方案。
- 所有组件能够通过一条明确的开发命令启动。

## 此阶段不做

- Kafka
- VictoriaMetrics
- Elasticsearch
- Monitor
- Marshaller
- Kubernetes
- Exporter

---

# 5. Phase 1：最小业务闭环

## 目标

先把 GoPulse 做成真正可使用的社交平台。

形成最小业务闭环：

```text
用户
→ 发布帖子
→ 查看帖子
→ 评论
→ 点赞
```

## 后端模块

优先实现：

```text
User
Post
Comment
Like
```

建议先保持一个 Backend，不进行微服务拆分。

逻辑上可以模块化：

```text
backend/
├── user/
├── post/
├── comment/
├── like/
├── repository/
├── service/
└── api/
```

## MySQL

保存核心业务事实。

示例：

```text
users
posts
comments
likes
```

原则：

> MySQL 是业务数据的最终事实来源。

## Redis

第一阶段只承担明确的缓存用途。

例如：

```text
帖子详情缓存
用户信息缓存
热点帖子缓存
```

禁止为了“用了 Redis”而设计复杂缓存。

## 前端

实现最小页面：

```text
登录 / 用户
帖子列表
帖子详情
发布帖子
评论
点赞
```

## 验收标准

用户可以完成完整流程：

```text
创建用户
→ 发布帖子
→ 查询帖子
→ 评论
→ 点赞
```

重新启动 Backend 后：

- 核心业务数据仍然存在。
- 数据来自 MySQL。
- Redis 缓存失效不会造成业务数据丢失。

## 此阶段不做

- RabbitMQ 业务异步化
- ES 全文搜索
- 可观测系统
- Kubernetes

---

# 6. Phase 2：业务异步化

## 目标

引入 RabbitMQ，让业务系统真正出现同步链路与异步链路。

## 使用范围

优先选择适合异步处理的业务：

```text
点赞后的通知
评论后的通知
异步消息记录
```

示例：

```text
用户 A 评论帖子
      ↓
Go Backend
      ↓
MySQL 写入评论
      ↓
RabbitMQ
      ↓
Worker Consumer
      ↓
生成通知
      ↓
MySQL
```

## 关键边界

RabbitMQ 消息不是最终业务数据。

例如：

```text
评论
```

仍然先写 MySQL。

RabbitMQ 处理的是：

```text
评论成功后需要发生的后续动作
```

而不是代替数据库保存评论。

## 实现内容

Backend：

- RabbitMQ Producer
- 消息结构定义
- 重试基础机制

新增：

```text
Business Worker
```

负责消费 RabbitMQ。

初期 Business Worker 可以仍然放在 Backend 仓库中，以独立进程启动。

## 验收标准

- 评论写入后即使通知消费者暂时停止，评论仍然成功。
- Consumer 恢复后能够继续处理消息。
- RabbitMQ 故障不会造成已经写入 MySQL 的核心数据丢失。
- 可以明确观察同步业务与异步业务的区别。

---

# 7. Phase 3：Elasticsearch 与业务搜索

## 目标

首先让 Elasticsearch 在业务系统中产生真实用途，而不是只作为日志仓库存在。

实现帖子全文搜索。

## 链路

```text
用户发布帖子
      ↓
Go Backend
      ↓
MySQL
      ↓
同步 / 异步建立 ES 索引
      ↓
Elasticsearch
```

查询：

```text
Frontend
   ↓
Backend Search API
   ↓
Elasticsearch
```

## 搜索范围

第一版只实现：

```text
帖子标题
帖子正文
```

Backend 对外提供统一查询接口。

前端不得直接连接 Elasticsearch。

## 数据边界

```text
MySQL = 业务事实
ES    = 搜索索引
```

如果 ES 数据丢失，应能够从 MySQL 重建。

## 验收标准

能够通过关键词搜索帖子。

例如：

```text
搜索：
Kubernetes

返回：
标题或正文中包含相关内容的帖子
```

删除 Elasticsearch 索引后，可以通过重建流程恢复搜索数据。

---

# 8. Phase 4：业务日志基础

## 目标

为后面的可观测链路准备真实日志源。

此阶段先不建设完整 Monitor / Kafka / Marshaller。

## Backend 日志规范

Backend 统一输出结构化日志，例如：

```json
{
  "level": "info",
  "service": "backend",
  "module": "post",
  "message": "post created",
  "timestamp": "...",
  "request_id": "...",
  "user_id": 10001
}
```

日志至少包含：

```text
timestamp
level
service
module
message
request_id
```

## 验收标准

主要 API 都能生成统一格式日志。

通过 request_id 可以关联一次请求中的关键日志。

这一阶段的意义不是建立日志平台，而是保证后续 LogMonitor 有稳定的数据来源。

---

# 9. Phase 5：Exporter Plugin 原型

## 目标

实现 GoPulse 可观测系统的第一个独立能力：组件指标采集插件。

先只实现一个插件。

建议：

```text
mysql_exporter
```

或者：

```text
redis_exporter
```

## Exporter 工作模式

Exporter 作为常驻进程：

```text
启动 HTTP Server
        ↓
等待 /metrics 请求
        ↓
收到请求
        ↓
连接目标组件
        ↓
采集
        ↓
转换为统一指标格式
        ↓
HTTP Response
```

Exporter 不主动向 Monitor 推送数据。

Exporter 不保存历史数据。

## 第一版接口

```text
GET /metrics
GET /health
```

## 验收标准

直接访问：

```text
http://exporter:port/metrics
```

可以获得组件指标。

关闭目标组件后，Exporter 能够返回明确的异常指标或采集失败状态，而不是自身直接崩溃。

---

# 10. Phase 6：Monitor

## 目标

实现 GoPulse 自研可观测采集器。

Monitor 由三个逻辑模块组成：

```text
Monitor
├── MetricsMonitor
├── LogMonitor
└── EventMonitor
```

此阶段先完成 MetricsMonitor。

Phase 6 同时先建立两种产品使用态的身份与授权基础：普通用户使用社交业务域，管理员通过同一账号和会话获得可观测管理能力。全部插件管理接口由 Backend 根据数据库当前 `admin` 角色授权，普通用户固定返回 `403 permission_denied`。

本阶段按四个可执行批次实施：管理员身份与双用户态授权、插件安装包与生命周期、MetricsMonitor 周期采集与标准消息、集成验收与阶段收口；权威版本与分支以 Phase 6 总实施方案为准。

---

## 10.1 MetricsMonitor

工作方式：

```text
Exporter
   ↑
周期 HTTP Pull
   |
MetricsMonitor
```

MetricsMonitor 负责：

1. 管理采集目标。
2. 周期请求 Exporter。
3. 解析返回内容。
4. 做第一次基础清洗。
5. 生成 GoPulse 标准消息。

统一消息至少包含：

```text
type
source
timestamp
payload
```

例如：

```json
{
  "type": "metrics",
  "source": "mysql",
  "timestamp": "...",
  "payload": {}
}
```

## 10.2 Plugin Manager

Monitor 内实现简单插件生命周期能力：

```text
安装
启动
停止
更新
状态查询
```

它是节点执行器，不建设独立插件平台。

Go Backend：

```text
Backend
   ↓ 管理指令
Monitor
   ↓
Exporter
```

## 验收标准

管理员通过 Backend 可以：

```text
查看 Exporter 状态
安装 Exporter
启动 Exporter
停止 Exporter
更新 Exporter
```

普通用户对以上接口均为 `403 permission_denied`，且被拒绝请求不得到达 Monitor；管理员和普通用户的既有社交业务均保持可用。

MetricsMonitor 可以周期采集 Exporter。

采集结果已经转换为 GoPulse 内部统一消息。

---

# 11. Phase 7：Message Router + Kafka

## 目标

正式建立统一可观测数据入口。

新增：

```text
Message Router
Kafka
```

链路：

```text
MetricsMonitor
      ↓
Message Router
      ↓
Kafka
```

## Message Router 职责

只负责：

```text
接收
识别消息类型
选择 Topic
写入 Kafka
```

例如：

```text
metrics → metrics topic
logs    → logs topic
events  → events topic
```

也可以先统一使用一个 Topic，通过消息中的 `type` 分类。

项目第一版推荐：

```text
一个 Kafka Topic
+
统一消息 Envelope
```

等数据规模与场景明确后再拆 Topic。

## 验收标准

MetricsMonitor 不直接依赖 Kafka SDK。

链路必须是：

```text
MetricsMonitor
→ Message Router
→ Kafka
```

Kafka Consumer 可以读取完整 metrics 消息。

Message Router 不修改业务字段。

---

# 12. Phase 8：Marshaller + VictoriaMetrics

## 目标

完成第一条真正完整的可观测写入链路。

链路：

```text
Exporter
   ↓
MetricsMonitor
   ↓
Message Router
   ↓
Kafka
   ↓
Marshaller
   ↓
VictoriaMetrics
```

## Marshaller 职责

消费 Kafka。

识别：

```text
type = metrics
```

执行：

```text
字段校验
字段映射
格式转换
异常数据过滤
```

最终转换成 VictoriaMetrics 可以写入的格式。

## 重要边界

MetricsMonitor：

```text
解决“采集到的数据是否能够进入 GoPulse”
```

Marshaller：

```text
解决“进入 GoPulse 的数据如何成为可存储数据”
```

## 验收标准

MySQL / Redis Exporter 指标最终能够写入 VictoriaMetrics。

可以直接查询 VictoriaMetrics，看到：

```text
CPU / connection / request / status
```

等实际采集指标。

至此第一条可观测闭环完成。

---

# 13. Phase 9：LogMonitor + 日志链路

## 目标

接入日志数据。

日志模式与 Metrics 不同。

Metrics：

```text
Monitor 主动 Pull
```

Logs：

```text
日志源主动 Push
```

## 链路

```text
Go Backend
    ↓
LogMonitor
    ↓
第一次清洗
    ↓
Message Router
    ↓
Kafka
    ↓
Marshaller
    ↓
Elasticsearch
```

LogMonitor 负责被动接收。

建议提供：

```text
POST /logs
```

或者内部 TCP / HTTP 接收接口。

第一阶段优先 HTTP，降低复杂度。

## Elasticsearch 数据区分

业务帖子搜索索引和日志索引必须分离。

例如：

```text
gopulse-posts-*
gopulse-logs-*
```

## 验收标准

调用 Backend API 后产生业务日志。

日志能够经过：

```text
Backend
→ LogMonitor
→ Router
→ Kafka
→ Marshaller
→ ES
```

最终通过 Backend 查询接口搜索日志。

---

# 14. Phase 10：EventMonitor + 事件链路

## 目标

补齐第三类可观测数据。

Event 用于描述系统发生的离散事件，例如：

```text
Exporter 启动
Exporter 停止
采集失败
组件连接失败
插件安装
插件升级
Pod 重启
```

链路：

```text
Event Source
    ↓
EventMonitor
    ↓
Message Router
    ↓
Kafka
    ↓
Marshaller
    ↓
Elasticsearch
```

EventMonitor 同样采用被动接收模式。

统一事件模型：

```text
event_name
source
severity
timestamp
message
metadata
```

## 验收标准

插件启停、采集失败等操作能够产生 Event。

Event 可以在 Elasticsearch 中查询。

---

# 15. Phase 11：可观测前端

## 目标

让已经完成的后端数据链路真正成为管理员可使用的“可观测系统”，同时保持普通用户社交平台独立、可用且不可见管理数据。

前端新增：

```text
可观测总览
指标查看
日志查询
事件查询
采集插件管理
```

## 数据读取方式

Frontend 不直接连接：

```text
VictoriaMetrics
Elasticsearch
Monitor
```

统一通过：

```text
Frontend
   ↓
Go Backend
```

普通用户与管理员继续共用现有登录和 Cookie；只有管理员可以进入可观测管理路由并调用以下 API，普通用户固定获得 `403 permission_denied`。

Backend 提供：

```text
Metrics Query API
Logs Query API
Events Query API
Monitor Management API
```

也就是此前定义的 all-query 能力直接归 Backend。

## 插件页面

至少显示：

```text
插件名称
目标组件
运行状态
最近采集时间
最近错误
```

操作：

```text
安装
启动
停止
更新
```

操作流程：

```text
Frontend
   ↓
Backend
   ↓
Monitor
   ↓
Exporter
```

前端定期查询执行状态。

失败时展示失败原因。

## 验收标准

不直接访问任何底层数据库，通过 GoPulse 页面完成：

```text
查看指标
搜索日志
查询事件
管理 Exporter
```

---

# 16. Phase 12：Docker 化

## 目标

在 Kubernetes 之前，先保证所有组件具备标准容器运行方式。

需要 Docker 化的自研组件：

```text
Frontend
Backend
Business Worker
Monitor
Message Router
Marshaller
Exporter
```

基础组件使用官方镜像：

```text
MySQL
Redis
RabbitMQ
Kafka
VictoriaMetrics
Elasticsearch
```

建立完整 Docker Compose 环境。

## 验收标准

本机不安装任何项目运行时基础组件，仅依赖：

```text
Docker
Docker Compose
```

即可启动完整 GoPulse。

完整链路均可验证。

---

# 17. Phase 13：业务基础系统与用户端闭环

## 目标

在既有用户、帖子、评论、点赞、通知和搜索基础上，补齐一个参考 X 但主动删减后的社交业务闭环。

本阶段只实现：

- 显示名称与个人介绍。
- 用户资料页展示基本资料和该用户发布的帖子，但不公开其关注与粉丝列表。
- 按登录用户名或显示名称搜索用户。
- 从作者、搜索结果或资料页关注/取消关注。
- 仅包含已关注用户帖子的 Following 信息流。
- 新增关注进入既有通知中心，取消关注不产生通知。
- 仅本人可见的关注、粉丝和收藏关系。
- 帖子收藏/取消收藏。
- 帖子编辑：只保存最新内容并记录最后编辑时间。
- 帖子永久删除：二次确认后清理评论、点赞、收藏、缓存和搜索投影，历史通知保留为“原内容已删除”。
- 独立用户 Frontend 的首页、Following、搜索、收藏、资料和帖子管理主路径。

## 验收标准

- 上述业务可在现有 Compose 系统独立运行，不依赖 Kubernetes。
- 作者权限、关系隐私、关系幂等、缓存/搜索一致性和永久删除边界均可验证。
- 既有注册、登录、评论、点赞、通知、搜索和管理员可观测能力不回归。

## 不做

图片、视频、私信、转发、引用、话题、趋势、推荐算法、广告、付费、私密账号、关注审批、拉黑、举报治理及公开关注/粉丝列表均不进入本阶段。

---

# 18. Phase 14：插件体系与组件可观测闭环

## 目标

把 Redis Exporter 原型扩展为六类官方单实例插件，并补齐 GoPulse 自研组件指标。

官方插件固定为：

```text
Redis
MySQL
RabbitMQ
Kafka
Elasticsearch
VictoriaMetrics
```

固定实例模型：

```text
一种插件
→ 一个运行实例
→ 一个采集目标
```

本阶段建立受限 Manifest、兼容与完整性校验、配置 Schema、Secret 隔离、安装/启停/更新/回滚和重启恢复，并让六类真实指标继续经过 Monitor、Router、Kafka、Marshaller、VictoriaMetrics 和 Backend。

Backend、Business Worker、Search Indexer、Monitor、Router 与 Marshaller 同时提供有限、稳定、低基数的运行指标。

## 验收标准

- 六类官方插件各自对一个真实目标产生可查询指标。
- 同类第二实例和第二目标在服务端被拒绝。
- 一种插件失败不影响其他插件、历史数据或社交业务。
- Secret、完整连接串和内部进程信息不进入公共 API、日志或事件。
- 自研组件指标可被统一查询，且不使用业务 ID 级高基数标签。

## 不做

同类多实例、单实例多目标、目标自动发现、第三方插件、插件市场、告警和 Kubernetes 不进入本阶段。

---

# 19. Phase 15：告警与管理端闭环

## 目标

建立独立管理 Frontend、内部告警闭环和最终双角色模型。

角色固定为：

```text
user
super_admin
```

系统保留一个不可删除、不可降级的引导超级管理员。可存在多个超级管理员；任一超级管理员可以按用户 ID 提升或降级非引导账号。

用户端与管理端共用 Backend、用户数据库、统一登录和同源会话，但使用两个独立 Frontend 应用。超级管理员进入管理端后默认看到可观测大屏。

## 管理端范围

- 业务与组件运行状态。
- Metrics、Logs、Events 查询。
- 六类插件状态与生命周期操作。
- 当前告警、近期告警记录和规则管理。
- 按用户 ID 调整角色。
- 权限与管理操作审计事件。

告警形成：

```text
Metrics / Logs / Events
→ 规则评估
→ 触发 / 持续 / 恢复
→ 管理大屏与历史记录
```

告警只在管理后台内部呈现，不发送邮件、短信、Webhook 或其他外部通知。

## 验收标准

- 普通用户只能进入用户端，管理页面和 API 由 Backend 拒绝。
- 超级管理员登录后默认进入管理大屏。
- 引导超级管理员受到保护，其他账号角色调整及时生效且可审计。
- 三类数据均能触发和恢复代表性告警，重复评估不会产生无界重复记录。
- 告警或可观测依赖故障不阻断社交业务。

---

# 20. Phase 16：跨平台产品化与双前端交付

## 目标

把完整业务与可观测能力提升为可在 Linux、macOS 与 Windows 宿主上安装、运行、升级、诊断和恢复的双前端 Compose 产品。

支持方式：

```text
统一域名与登录入口
        ↓
Backend 读取数据库当前角色
   ├── user        → 独立用户 Frontend
   └── super_admin → 独立管理 Frontend
```

最小真实矩阵固定包含 Linux `amd64`、macOS `arm64` 与 Windows `amd64`；自研容器、两个 Frontend 和六类插件形成 `linux/amd64`、`linux/arm64` 制品。

## 产品交付能力

- 共享的初始化、启动、停止、状态、日志、验证和脱敏诊断入口。
- 同一域名、同源会话和两个独立 Frontend 的安全分流。
- 两端统一视觉基础、桌面/窄屏、加载/空/错误/过期状态、键盘焦点和时区表现。
- 从 `1.9.4` 到本阶段的可重复升级路径。
- 业务、可观测、插件、角色、告警和配置的最小一致备份/恢复。
- 正常、失败与中断路径的强归属资源清理。

## 验收标准

- 三类真实宿主均可启动完整系统，不要求宿主安装 Go、Node.js 或基础设施。
- 同一域名与统一登录可按角色进入正确的独立 Frontend，越权由 Backend 拒绝。
- 用户端和管理端各自完成代表性闭环。
- 升级与隔离恢复后，业务事实和管理状态保持一致或可重建。
- 诊断、日志、API、Frontend bundle 和制品元数据不泄漏敏感信息。

本阶段仍不创建 Kubernetes 资源。

---

# 21. Phase 17：稳定性与工程化

## 目标

在完整 Compose 产品上统一配置、退出、健康检查、错误处理、Migration、消息消费与告警评估可靠性，形成 Kubernetes 前的最终产品验收。

重点包括：

- 统一环境变量与配置结构。
- 所有 Go 服务的 SIGTERM/SIGINT 优雅退出。
- 可直接供后续 Probe 使用的启动、存活和就绪契约。
- 结构化日志、Request ID 和统一 API 错误。
- Schema Migration。
- Kafka offset、重试和异常消息处理。
- RabbitMQ ack、重试和幂等。
- 告警评估的恢复、重复抑制和故障隔离。
- 两个 Frontend 与 `user`/`super_admin` 权限矩阵的最终回归。

## 验收标准

- 完整业务系统、可观测系统、插件、告警和两个 Frontend 在 Compose 中共同运行。
- 服务重启、依赖短暂故障和数据迁移不会造成明显数据混乱或权限降级。
- 引导超级管理员保护与角色调整在重启后保持正确。
- 错误可通过安全日志和状态接口定位。
- 达到验收标准后完成 Milestone 4；Kubernetes 不参与本里程碑成立条件。

---

# 22. Phase 18：Kubernetes 基础部署

## 目标

把 Phase 17 已完成的产品迁移到 `1 Master + 3 Worker` Kubernetes 集群。Kubernetes 只改变部署位置与编排方式，不增加或补做产品功能。

部署对象包含：

- 用户 Frontend 与管理 Frontend。
- Backend、Business Worker、Search Indexer。
- Monitor、Router、Marshaller 和六类单实例插件。
- MySQL、Redis、RabbitMQ、Kafka、VictoriaMetrics、Elasticsearch。

无状态组件使用 Deployment/Service；有状态组件使用与本阶段风险相称的持久化资源。所有组件通过 Service DNS 通信，不使用固定 Pod IP。

## 验收标准

- 完整 GoPulse 脱离 Compose 在目标集群共同运行，集群内不编译源码。
- 普通用户和超级管理员的代表性流程与 Phase 17 一致。
- 代表性 Pod 重建后，持久状态满足保留或可重建契约。
- 浏览器不能直达内部组件，Secret 不进入 Frontend、日志或公共 API。
- Kubernetes 资源中不存在补偿产品缺口的临时旁路。

---

# 23. Phase 19：Ingress 与统一入口

## 目标

为 Kubernetes 中的 GoPulse 提供单一 HTTP 入口，保持 Phase 16 已形成的同域登录和双前端产品模型。

访问结构：

```text
Browser
   ↓
Ingress
   ├── 统一登录
   ├── 用户 Frontend
   ├── 管理 Frontend
   └── Backend API
```

内部基础设施、自研服务和插件继续只通过集群内部 Service 通信。

## 验收标准

- 用户只需要一个域名，不直接访问 NodePort。
- 登录、会话恢复、退出和角色变化后的重新分流正确。
- 两个独立 Frontend 的资源和路由互不混淆。
- 普通用户不能加载管理数据；超级管理员可完成大屏、插件、告警和权限管理。
- 内部组件不对外暴露，可观测故障不阻断代表性社交路径。

---

# 24. Phase 20：Kubernetes 可观测集成

## 目标

让已经完整运行的 GoPulse 观测其 Kubernetes 部署环境，而不是依赖 Kubernetes 才形成业务或可观测产品闭环。

本阶段：

- 六类官方单实例插件连接集群内组件 Service。
- 自研组件既有指标在 Kubernetes 形态下继续采集。
- 增加代表性 Node、Pod、Workload 状态与事件，使用最小只读 RBAC。
- 将集群指标、日志、事件和告警接入既有 Backend 与独立管理 Frontend。
- 管理大屏区分业务、组件、采集链路和 Kubernetes 状态。

## 验收标准

- 六类组件、自研服务和代表性 Kubernetes 对象均产生真实可查询数据。
- 代表性集群异常可触发并恢复内部告警。
- Kubernetes API 权限仅覆盖已验收只读对象，不读取或返回 Secret。
- 普通用户不能访问集群数据；超级管理员经统一入口查看大屏和告警。
- 自观测退化不阻断社交业务，也不改变 Phase 17 已验收的产品语义。

达到条件后完成 Milestone 5。全面集群监控、自动修复、跨集群采集、生产级 HA 和完整 SRE 进入独立后续规划。

---

# 25. 最终完整架构

身份与用户入口：

```text
统一登录
   ↓
Go Backend
   ├── user        → 用户 Frontend
   └── super_admin → 管理 Frontend
```

业务链路：

```text
用户 Frontend
   ↓
Go Backend
   ├── MySQL：用户、关注、收藏、帖子、评论、点赞、通知
   ├── Redis：缓存
   ├── Elasticsearch：可重建搜索投影
   └── RabbitMQ → Business Worker
```

可观测链路：

```text
六类 Exporter / GoPulse 组件 / Kubernetes 对象
   ↓
Monitor
   ↓
Message Router
   ↓
Kafka
   ↓
Marshaller
   ├── metrics → VictoriaMetrics
   └── logs / events → Elasticsearch
```

查询与告警：

```text
VictoriaMetrics / Elasticsearch
   ↓
Go Backend
   ├── 告警规则评估与内部记录
   └── 管理 Frontend 大屏
```

Frontend 不直连 Monitor、插件、Kafka、VictoriaMetrics、Elasticsearch、数据库或 Kubernetes API。

---

# 26. 阶段依赖关系

```text
Phase 0～12  已完成的业务、可观测与 Compose 基线
   ↓
Phase 13 业务基础系统与用户端闭环
   ↓
Phase 14 插件体系与组件可观测闭环
   ↓
Phase 15 告警与管理端闭环
   ↓
Phase 16 跨平台产品化与双前端交付
   ↓
Phase 17 稳定性与工程化（完整产品验收）
   ↓
Phase 18 Kubernetes 基础部署
   ↓
Phase 19 Ingress 与统一入口
   ↓
Phase 20 Kubernetes 可观测集成
```

---

# 27. 关键里程碑

## Milestone 1：业务系统 MVP

Phase 0～Phase 3 完成用户、帖子、评论、点赞、通知和搜索的第一版可用业务系统，并以 `1.0.0` 收口。

## Milestone 2：指标采集 MVP

Phase 4～Phase 8 完成 Exporter、Monitor、Router、Kafka、Marshaller 与 VictoriaMetrics 指标链路。

## Milestone 3：完整可观测 MVP

Phase 9～Phase 11 完成 Metrics、Logs、Events 查询和第一版管理体验。

## Milestone 4：完整可交付产品 MVP

Phase 12～Phase 17 完成：

- 可运行的 Compose 基线。
- 关注、Following、收藏、帖子编辑/删除和独立用户端。
- 六类官方单实例插件与自研组件指标。
- 内部告警、双角色和独立管理端大屏。
- 同域登录、双前端跨平台交付、升级、备份和恢复。
- Kubernetes 前的统一工程质量验收。

该里程碑证明 GoPulse 自身已经完整运转。Kubernetes 不是其完成条件。

## Milestone 5：Kubernetes 部署与自观测 MVP

Phase 18～Phase 20 将 Milestone 4 的同一产品迁移到 Kubernetes，提供统一 Ingress，并接入代表性集群对象的可观测数据和内部告警。

该里程碑只证明部署与集群观测能力，不重新定义业务、插件、告警或前端产品范围。

---

# 28. 当前执行起点

Phase 12 及其 Review 整改已经完成，当前完成产品版本为：

```text
1.9.4
```

Phase 12 的实施范围、验收结论、实施方案和实施记录保持完成时状态，不追溯改写。

下一阶段是：

```text
Phase 13 业务基础系统与用户端闭环
```

Phase 13 实施前必须基于最新主远程和 `1.9.4` 真实代码生成总实施方案，确定批次顺序、目标版本和 `develop/x.x.x` 分支。规划文档完成不代表 Phase 13 实现已经开始或完成。
