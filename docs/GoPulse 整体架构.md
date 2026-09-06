GoPulse 是一个基于 **Go 与云原生思想** 搭建的社交与可观测平台，由基础业务系统与可观测系统组成。两套系统必须先在 Compose 环境形成完整产品，随后再把同一产品部署到 Kubernetes。
- **基础业务系统：** 负责用户、资料、关注、Following 信息流、帖子、评论、点赞、收藏、通知和搜索。
- **可观测系统：** 负责 Metrics、Logs、Events 的采集、传输、处理、存储、查询和管理后台内部告警。
- **产品界面：** 用户端与超级管理员端是两个独立 Frontend，共用 Backend、用户数据库、统一登录和同源会话。

# 一、技术选型
GoPulse 主要采用以下技术：

| 类型      | 技术               |
| ------- | ---------------- |
| 前端      | TypeScript、Vue3  |
| 后端      | Go、Gin           |
| 核心数据存储  | MySQL            |
| 缓存      | Redis            |
| 业务异步任务  | RabbitMQ         |
| 可观测数据传输 | Kafka            |
| 时序数据存储  | VictoriaMetrics  |
| 日志与事件存储 | Elasticsearch    |
| 数据采集    | Exporter、Monitor |
| 数据传输    | Message Router   |
| 数据清洗    | Marshaller       |
| 本地与产品运行 | Docker Compose    |
| 最终部署    | Kubernetes       |

---

# 二、整体架构
![GoPulse 整体架构](<photo/Pasted image 20260825165420.png>)
# 三、业务系统

基础业务系统负责 GoPulse 的社交业务能力，包括：

- 显示名称、个人介绍与用户搜索。
- 关注/取消关注、关注通知与 Following 信息流；关注和粉丝列表仅本人可见。
- 帖子发布、查询、编辑和带二次确认的永久删除。
- 评论、点赞、收藏、通知和全文搜索。
- 本轮不支持帖子图片、私信、转发、推荐算法等 X 扩展能力。

![GoPulse 业务系统](<photo/Pasted image 20260824173131.png>)

在这一部分，相关联的组件不多，但都非常重要

- Mysql 用于存储核心数据，比如用户账号密码，点赞关注等信息
- Redis 用于缓存，目的是存储一些临时数据，避免频繁查询 Mysql 对其造成压力
- RabbitMQ 用于异步执行任务，目的是为了保证系统高并发


# 四、可观测系统
可观测系统负责对组件和应用产生的 Metrics、Logs、Events 进行数据采集、传输、转换、存储、查询
![GoPulse 可观测系统](<photo/Pasted image 20260824174615.png>)

可观测系统使用 Redis、MySQL、RabbitMQ、Kafka、Elasticsearch 与 VictoriaMetrics 六类官方插件，每类严格限制为一个实例和一个目标。管理端默认展示可观测大屏、系统状态和内部告警记录；告警不接入邮件、短信或 Webhook。

最终角色只有 `user` 与 `super_admin`。系统保留一个不可删除、不可降级的引导超级管理员，并允许超级管理员按用户 ID 调整其他账号的角色。

# 五、部署边界

Phase 17 前必须完成业务、可观测、插件、告警、双前端、跨平台交付和工程稳定性。Phase 18～Phase 20 才处理 Kubernetes 基础部署、Ingress 与集群可观测；Kubernetes 不承接未完成的产品功能。
