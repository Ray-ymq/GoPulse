GoPulse 是一个基于 **Go 与 云原生思想** 搭建的社交平台，由 基础业务系统 与 可观测系统 组成，最终统一运行在 Kubernetes 集群中
- **基础业务系统:** 负责用户、内容、评论、点赞等正常业务
- **可观测系统:** 负责 Metrics、Logs、Events 的采集、传输、处理、存储和查询
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
| 部署      | Kubernetes       |

---

# 二、整体架构
![GoPulse 整体架构](<photo/Pasted image 20260825165420.png>)
# 三、业务系统
基础业务系统负责 GoPulse 的社交业务能力，包括
- 用户、点赞、关注、收藏、评论等
![GoPulse 业务系统](<photo/Pasted image 20260824173131.png>)
在这一部分，相关联的组件不多，但都非常重要
- Mysql 用于存储核心数据，比如用户账号密码，点赞关注等信息
- Redis 用于缓存，目的是存储一些临时数据，避免频繁查询 Mysql 对其造成压力
- RabbitMQ 用于异步执行任务，目的是为了保证系统高并发


# 四、可观测系统
可观测系统负责对组件和应用产生的 Metrics、Logs、Events 进行数据采集、传输、转换、存储、查询
![GoPulse 可观测系统](<photo/Pasted image 20260824174615.png>)

其实从整体来看，就是堆组件而已，把有用的没用的，都给他堆上去，然后 AI 一顿搓就成了

但，这个项目其实我想了很久，起码有一个月，虽然是一个玩具项目，不具有生产的条件，但正因为如此，所以有很多的优化方案，比如只有 3 个 Worker 节点，如果 一个 Worker 节点挂了，正好是 Mysql，怎么办？

这就涉及到了实例部署嘛，你可以委婉的表示，我本来想多实例部署的，但是主机资源不够......然后吹nb就行了 hh，当然这个是我的幻想
