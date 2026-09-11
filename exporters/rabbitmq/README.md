# RabbitMQ Exporter

锁定 RabbitMQ 3.13.3 Management HTTP，严格聚合唯一 `/` vhost，不使用集群 overview。

- container mode 固定 `rabbitmq:15672`；host mode 只允许 `127.0.0.1` / `::1` 的 15672。
- 配置：`RABBITMQ_HOST`、`RABBITMQ_MANAGEMENT_PORT`、`RABBITMQ_USERNAME`、
  `RABBITMQ_PASSWORD`、`RABBITMQ_VHOST=/`、`RABBITMQ_EXPORTER_CONNECT_TIMEOUT`、
  `RABBITMQ_EXPORTER_SCRAPE_TIMEOUT`。不接受 URL、API path 或任意 vhost。
- 专用账号 tag=`monitoring`，仅 `/` permissions configure/write/read=`^$`。
- 回环端口 9123；`/health`、`/metrics`、`--check`、SIGTERM、完整/失败快照行为与 MySQL 插件一致。
- HTTP 禁止代理与重定向，单次响应最多 1 MiB，所有请求共享 scrape deadline。

## 指标与聚合

9 families / 10 samples；前缀 `gopulse_rabbitmq_`。

| 后缀 | 固定接口 / 字段 | kind |
| --- | --- | --- |
| up | 所有请求和字段验证成功 | gauge |
| connections | `/api/vhosts/%2F/connections` 长度，检查每项 vhost | gauge |
| channels | `/api/vhosts/%2F/channels` 长度，检查每项 vhost | gauge |
| queues | `/api/queues/%2F` 长度，检查每项 vhost | gauge |
| consumers | 同上 consumers 求和 | gauge |
| messages | 同上 messages_ready / messages_unacknowledged 求和，`state=ready\|unacked` | gauge |
| published_total | `/api/vhosts` 选择唯一 `/`，message_stats.publish | counter |
| delivered_total | 同上 message_stats.deliver_get | counter |
| acked_total | 同上 message_stats.ack | counter |

up 单位 boolean，其余 count。deliver_get 包含四种 delivery/get；redeliver 已是子集，不额外累加。
同一账号对单 vhost `/api/vhosts/%2F` 会收到需要 administrator 的 401，因此使用列表并严格选取，
不增权。账号看见的其他 vhost 不进入指标，也不输出任意对象名。

官方 `rabbitmq/rabbitmq-server` tag `v3.13.3` 的 `deps/rabbitmq_management/priv/www/api/index.html`
明确 message_stats 中仅出现有活动的字段：因此仅这三个累计 counter 的未出现字段按零。
空队列数组求和为零；已有队列缺少 consumers/ready/unacked、scope 不符或任一请求失败均整体失败，
不以冷启动猜测填零。不保留历史值；上游 counter 重置原样表达。

```bash
(cd exporters/rabbitmq && go test ./...)
bash scripts/package-redis-exporter.sh --source rabbitmq --version 1.11.2
```
