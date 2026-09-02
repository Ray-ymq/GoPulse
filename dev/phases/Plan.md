# GoPulse 项目实施计划

## 1. 项目目标

GoPulse 是一个基于 Go 与 Kubernetes 构建的云原生社交内容平台。

项目最终由两部分组成：

- **基础业务系统**：提供用户、帖子、评论、点赞等社交业务能力。
- **可观测系统**：负责指标、日志、事件的采集、传输、清洗、存储与查询。

项目不从一开始追求完整微服务化、复杂高可用或大规模生产能力，而是采用阶段式实现：

```text
基础业务可运行
    ↓
业务异步能力
    ↓
搜索与日志能力
    ↓
可观测采集链路
    ↓
统一数据处理链路
    ↓
Kubernetes 部署
    ↓
完整 GoPulse
```

每个阶段必须满足两个条件：

1. 当前阶段本身能够独立运行和验证。
2. 当前阶段产物能够直接作为下一阶段的基础，不推倒重来。

## 1.1 执行平台与兼容边界

- Phase 0 与 Phase-01-01 已按原跨平台策略完成，原生 Windows PowerShell 与 Bash 开发入口的共同能力基线截至产品版本 `0.2.1`。
- 从 Phase-01-02 到 Phase 16，项目在 Windows 宿主机的 WSL2 Linux 环境中实施、测试和验收，活动仓库放在 WSL Linux 文件系统中，日常生命周期与验收入口只维护 Bash 版本。
- 此期间不新增或同步更新原生 PowerShell 脚本，不把 PowerShell/Bash 语义一致、Windows runner 或原生 Windows 验收作为阶段完成条件。现有 `scripts/*.ps1` 保留为 `0.2.1` 历史能力快照。
- Phase 16 完成并通过里程碑验收后，再建立不占用 Phase 0–16 编号的 Windows PowerShell 兼容任务，以最终 Bash 行为、配置契约、容器拓扑和验收流程为基线集中实现与回归。
- 延后原生 Windows 兼容不降低当前阶段的业务、数据、安全、故障恢复、Linux CI、Docker 或 Kubernetes 验收标准。

---

# 2. 总体技术栈

## 前端

- TypeScript
- Vue 3

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

## 部署环境

- Docker
- Kubernetes
- 1 Master + 3 Worker

规划节点：

```text
Master
└── Kubernetes Control Plane

Worker-1
├── Frontend
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

## 3.1 先完成业务，再建设可观测系统

GoPulse 首先必须是一个能够使用的社交平台。

如果没有真实业务流量、业务日志和基础组件，可观测系统本身没有实际观测对象。

因此实现顺序固定为：

```text
业务系统
→ 基础组件
→ 业务数据
→ 可观测数据
→ Kubernetes
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

---

## 3.6 以里程碑 MVP 驱动实施与切分

GoPulse 的四个里程碑分别交付一次可独立运行、验证和使用的递进式 MVP。后一个 MVP 继承前一个 MVP 的能力并增加新的完整闭环，不为追求最终架构而扩大当前里程碑范围：

- Milestone 1 交付业务系统 MVP，对应 Phase 0～Phase 3。
- Milestone 2 交付指标采集 MVP，对应 Phase 4～Phase 8。
- Milestone 3 交付完整可观测 MVP，对应 Phase 9～Phase 11。
- Milestone 4 交付云原生自观测 MVP，对应 Phase 12～Phase 15。
- Phase 16 不定义新的 MVP，只作为四个 MVP 既有能力的最终工程质量门槛。

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

Backend 可以：

```text
查看 Exporter 状态
启动 Exporter
停止 Exporter
```

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

让已经完成的后端数据链路真正成为一个“可观测系统”。

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

# 17. Phase 13：Kubernetes 基础部署

## 目标

把 Docker 环境迁移到 Kubernetes。

初期不追求复杂高可用。

集群：

```text
1 Master
3 Worker
```

## 第一阶段部署对象

无状态组件使用：

```text
Deployment
Service
```

例如：

```text
Frontend
Backend
Monitor
Router
Marshaller
Exporter
```

有状态基础设施根据项目学习目标逐步使用：

```text
StatefulSet
PVC
Service
```

不要求一次性全部改成复杂 StatefulSet。

## 节点规划

### Worker-1

```text
Frontend
Backend
Business Worker
```

### Worker-2

```text
MySQL
Redis
RabbitMQ
```

### Worker-3

```text
Kafka
VictoriaMetrics
Elasticsearch
Monitor
Message Router
Marshaller
Exporter
```

通过：

```text
nodeSelector
```

或节点标签控制调度。

## 验收标准

完整 GoPulse 可以脱离 Docker Compose，在 Kubernetes 中运行。

所有组件通过 Service 名称通信，不使用固定 Pod IP。

---

# 18. Phase 14：Ingress 与统一入口

## 目标

提供统一 HTTP 入口。

访问结构：

```text
Browser
   ↓
Ingress Controller
   ↓
Ingress
   ↓
Frontend / Backend Service
```

例如：

```text
gopulse.local/
gopulse.local/api/
```

原则：

- 外部只暴露必要入口。
- MySQL、Redis、Kafka、ES、VM 等基础组件不得直接暴露公网。
- 内部组件通过 ClusterIP Service 通信。

## 验收标准

用户只需要一个访问地址即可使用 GoPulse。

无需直接访问 NodePort。

---

# 19. Phase 15：Kubernetes 可观测闭环

## 目标

让 GoPulse 的可观测系统开始观测运行 GoPulse 自身的 Kubernetes 环境。

此时真正形成：

```text
GoPulse
   ↓
运行在 Kubernetes
   ↓
GoPulse Monitor
   ↓
观测 GoPulse 自己
```

新增采集对象可以包括：

```text
MySQL
Redis
RabbitMQ
Kafka
Elasticsearch
VictoriaMetrics
Backend
Kubernetes Node
Kubernetes Pod
```

插件仍集中部署在 Worker-3。

插件通过 Kubernetes Service 或目标地址连接其他节点上的组件。

## 验收标准

GoPulse 页面可以同时看到：

```text
业务运行状态
组件指标
业务日志
系统事件
```

完整形成项目闭环。

---

# 20. Phase 16：稳定性与工程化

## 目标

在四个里程碑 MVP 的主要架构闭环完成之后，统一强化既有组件的工程质量。

本阶段是最终质量门槛，不定义新的 MVP，也不把各阶段正常运行所必需的基础正确性延后到这里。前序阶段已经实现的配置、退出、健康检查、Migration 和消息可靠性能力在此统一契约、补齐差异并完成跨组件验证。

优先处理：

### 配置管理

统一环境变量和配置结构。

### Graceful Shutdown

所有 Go 服务支持：

```text
SIGTERM
SIGINT
```

优雅退出。

### Health Check

统一：

```text
/health
/ready
```

### Kubernetes Probe

配置：

```text
startupProbe
readinessProbe
livenessProbe
```

### 日志规范

统一结构化日志。

### Request ID

实现请求链路标识。

### 错误码

Backend API 建立统一错误响应。

### 数据库 Migration

建立明确 Schema Migration。

### Kafka Consumer

处理：

```text
offset
retry
异常消息
```

### RabbitMQ Consumer

处理：

```text
ack
retry
幂等
```

## 验收标准

组件发生正常重启时不会造成明显的数据混乱。

错误能够通过日志和状态接口定位。

---

# 21. 最终完整架构

业务链路：

```text
Frontend
   ↓
Go Backend
   ├────────────→ Redis
   ├────────────→ MySQL
   ├────────────→ Elasticsearch Search
   │
   └→ RabbitMQ
        ↓
   Business Worker
        ↓
      MySQL
```

Metrics 链路：

```text
Component
   ↓
Exporter
   ↑ Pull
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

Logs 链路：

```text
Go Backend / Components
   ↓ Push
LogMonitor
   ↓
Message Router
   ↓
Kafka
   ↓
Marshaller
   ↓
Elasticsearch
```

Events 链路：

```text
Components / GoPulse
   ↓ Push
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

查询链路：

```text
Frontend
   ↓
Go Backend
   ├── VictoriaMetrics
   └── Elasticsearch
```

管理链路：

```text
Frontend
   ↓
Go Backend
   ↓
Monitor
   ↓
Exporter Plugins
```

---

# 22. 阶段依赖关系

```text
Phase 0  工程骨架
   ↓
Phase 1  最小业务闭环
   ↓
Phase 2  RabbitMQ 业务异步
   ↓
Phase 3  Elasticsearch 搜索
   ↓
Phase 4  结构化业务日志
   ↓
Phase 5  Exporter
   ↓
Phase 6  Monitor
   ↓
Phase 7  Message Router + Kafka
   ↓
Phase 8  Marshaller + VictoriaMetrics
   ↓
Phase 9  LogMonitor
   ↓
Phase 10 EventMonitor
   ↓
Phase 11 可观测前端
   ↓
Phase 12 Docker
   ↓
Phase 13 Kubernetes
   ↓
Phase 14 Ingress
   ↓
Phase 15 Kubernetes 可观测闭环
   ↓
Phase 16 工程化与稳定性
```

---

# 23. 关键里程碑

## Milestone 1：业务系统 MVP

完成：

```text
Phase 0 ~ Phase 3
```

此时项目具备：

- 用户
- 帖子
- 评论
- 点赞
- RabbitMQ 异步任务
- Elasticsearch 全文搜索

该 MVP 证明 GoPulse 已是一个能够独立使用的社交业务系统。Phase 3 完成本里程碑验收后发布 `1.0.0`。

---

## Milestone 2：指标采集 MVP

完成：

```text
Phase 4 ~ Phase 8
```

此时：

```text
Component
→ Exporter
→ Monitor
→ Router
→ Kafka
→ Marshaller
→ VictoriaMetrics
```

完整运行。

该 MVP 证明 GoPulse 能够采集真实组件指标，并通过自研传输与处理链路完成存储和查询。这是整个项目最重要的技术里程碑。

---

## Milestone 3：完整可观测 MVP

完成：

```text
Phase 9 ~ Phase 11
```

此时具有：

```text
Metrics
Logs
Events
```

三条链路。

并能够通过前端进行统一查询和管理。

该 MVP 证明 Metrics、Logs、Events 三类数据及插件管理能力已形成面向用户的完整可观测体验。

---

## Milestone 4：云原生自观测 MVP

完成：

```text
Phase 12 ~ Phase 15
```

整个项目运行在 Kubernetes。

并由自己的 Monitor、Exporter、Kafka、Marshaller、VM、ES 观测自身。

最终形成：

```text
业务系统
   ↓
运行
   ↓
产生指标 / 日志 / 事件
   ↓
GoPulse 可观测系统
   ↓
采集与分析
   ↓
GoPulse 前端
```

这才是 GoPulse 的最终闭环。

该 MVP 证明完整系统可在 Kubernetes 中运行，并能通过 GoPulse 自身的可观测链路观察业务、组件和集群对象。Phase 16 在此基础上执行最终工程质量收口，不扩展新的 MVP 范围。

---

# 24. 当前执行起点

当前正式开发从：

```text
Phase 0
```

开始。

第一条主线不是 Kubernetes，也不是 Monitor。

而是：

```text
Frontend
   ↓
Go Backend
   ↓
MySQL
```

首先完成：

```text
用户
帖子
评论
点赞
```

然后依次加入：

```text
Redis
→ RabbitMQ
→ Elasticsearch
```

在业务系统具备真实运行数据后，再正式开始 Exporter、Monitor、Kafka、Marshaller 和 VictoriaMetrics。
