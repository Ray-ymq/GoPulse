# GoPulse 整体架构

> 当前完成产品版本：`2.0.5`。当前支持环境是 Linux `amd64` Docker Compose；Kubernetes 是未排期设计，
> 不是既有能力。已验证、边界和未验证能力见 [`capability-status.md`](capability-status.md)。

## 1. 项目定位

GoPulse 由真实社交业务和自研可观测链路组成。社交业务不是为了扩展功能清单，而是提供包含事务、缓存、
异步消息、搜索投影和权限的真实负载；可观测系统负责解释这些链路在正常、过载和故障时的行为。

后续目标是形成一个可验证的系统工程闭环：

```text
业务负载与故障
→ 业务系统和状态服务
→ Metrics / Logs / Events
→ SLI、容量与恢复证据
→ 定位第一瓶颈
→ 有证据的最小优化
```

## 2. 当前技术栈

| 领域 | 当前实现 |
| --- | --- |
| 用户端 / 管理端 | TypeScript、Vue 3，两套独立 Frontend |
| Backend 与后台进程 | Go、Gin、Business Worker、Search Indexer |
| 业务事实 | MySQL |
| 可丢弃缓存 | Redis |
| 业务异步传输 | RabbitMQ、事务 Outbox |
| 业务搜索投影 | Elasticsearch |
| 可观测采集与管理 | 六类 Exporter、Monitor |
| 可观测传输 | Message Router、Kafka |
| 可观测清洗与写入 | Marshaller |
| 指标存储 | VictoriaMetrics |
| 日志与事件存储 | 独立 Observability Elasticsearch |
| 交付 | Linux `amd64` OCI、Docker Compose、Bundle |

## 3. 业务系统

```text
Browser
  ↓
Frontend / Admin Frontend
  ↓ same-origin
Backend × 2
  ├── MySQL：用户、帖子、评论、点赞、关注、收藏、通知、角色、告警
  ├── Redis：帖子详情缓存，可丢弃、可回源
  ├── Outbox → RabbitMQ → Business Worker × 2 → 通知投影
  └── Outbox → RabbitMQ → Search Indexer × 2 → Business Elasticsearch
```

MySQL 是业务事实源。Redis 不决定业务事实；业务 Elasticsearch 是可从 MySQL 重建的搜索投影。
RabbitMQ 只承载业务异步任务，Kafka 不进入业务事实链路。

Backend、Worker 和 Indexer 已支持两个具名副本。会话不依赖粘性路由；Outbox owner、consumer tag、
lease、ack/requeue 和最终闭合均有固定验收。当前 MySQL、Redis、RabbitMQ 和业务 Elasticsearch
仍可为单节点，计算层多副本不等于状态层高可用。

## 4. 可观测系统

```text
Exporter / GoPulse 组件
  ↓ scrape / ingest
Monitor
  ↓ Envelope
Router × 2
  ↓ Kafka（4 partitions）
Marshaller × 2
  ├── metrics → VictoriaMetrics
  └── logs / events → Observability Elasticsearch
                         ↓
                      Backend
                         ↓
                 管理 Frontend / 内部告警
```

Monitor 负责插件期望状态、本地插件进程和采集；Router 只认证、校验、路由并写入 Kafka；Marshaller
按 partition 所有权执行第二次严格校验、转换、写入和手动 offset 提交。浏览器不直连任何数据或
可观测基础设施。

Router、Marshaller 已支持双副本、有限 buffer/in-flight/retry 和目标级故障隔离。业务搜索 ES 与
可观测 ES 使用独立服务、卷和网络边界。Monitor 仍是单实例和插件生命周期唯一所有者。

## 5. 运行与安全边界

- 默认只发布一个 Frontend 回环端口；Backend 和所有基础设施位于私有网络。
- 所有长运行 Go 进程具有 startup、live、ready、health、有限超时和有界关停合同。
- 普通用户与超级管理员共用身份和同源会话；所有管理授权由 Backend 执行。
- 日志、指标和事件失败不能回滚或阻断已经成立的业务事实。
- 运行时合同、Compose、版本、制品和验收 evidence 均绑定候选 revision。

## 6. 当前边界

- Phase 18 没有证明稳定 `150 RPS`，也没有生产容量 SLO。
- 当前组件 HTTP 指标主要提供 count/duration total，尾延迟分布将在 Phase 19 补齐。
- 尚未实现分布式 Trace、观测数据长期保留合同、Monitor HA 和状态层 HA。
- Kubernetes、HPA、跨地域、多活、生产 TLS/SASL 均不是当前产品能力。

后续发展顺序见
[`GoPulse 高并发与可观测后续路线图`](../dev/phases/GoPulse-高并发与可观测后续路线图.md)。
