# Phase-18-03：双 Elasticsearch 隔离与端到端背压实施方案

> 目标版本：`2.0.3`  
> 开发分支：`develop/2.0.3`  
> 直接前序：`2.0.2`；历史升级输入：`1.14.5`

## 1. 批次目标

将当前共享 Elasticsearch 拆为业务搜索和 Logs/Events 两个故障域，并为 HTTP、连接池、
Outbox、RabbitMQ、Kafka、Marshaller 和 sink 写入建立一致的有界背压，并验证每层在解除压力后
能够有界恢复。本批只修复 Phase-18-01/02 证据支持的瓶颈和无界路径。完整三轮重复性、跨系统
最终收敛和 150/300 RPS SLO 不在本批伪造通过，留给 Phase-18-05。

## 2. 前置条件

- Phase-18-01/02 已合入主线；Phase-18-01 可以是已接受的失败基线，单/多副本基线、第一瓶颈和所有权证据可复核。
- 从最新 `upstream/main` 创建 `develop/2.0.3`，准备独立 `1.14.5`、`2.0.2` 和 clean project。
- 历史单 ES 数据在任何迁移前先使用正式 lifecycle 备份，不就地破坏原卷。

## 3. 实施范围

### 3.1 双 Elasticsearch 拓扑与配置

- Compose 增加 `search-elasticsearch` 和 `observability-elasticsearch`，使用独立 named volume、健康检查、CPU/内存上限和内部网络连接。
- 两者启用身份验证，使用不同最小权限账户/密码；凭据从私有 secret 路径注入，不出现在 URL、日志或 evidence。
- Backend/Search Indexer/search-init 只使用 `SEARCH_ELASTICSEARCH_*`；Backend Logs/Events 查询和 Marshaller 只使用 `OBSERVABILITY_ELASTICSEARCH_*`。
- 旧 `ELASTICSEARCH_URL` 或相同凭据同时用于两个目标时启动失败，不做静默别名。runtime contract、`.env.example`、Bundle init 和 doctor 同步新键。
- 六插件目录不增加实例；Elasticsearch Exporter 固定指向业务搜索 ES，两个 ES 由 lifecycle 分别直接诊断。

### 3.2 单 ES 数据迁移与备份

- 迁移命令只识别固定业务索引、Logs/Events template、alias 和 index prefix，遇到未知对象停止并报安全 reason code。
- 保留原单 ES 作为搜索输入，将 Logs/Events 复制到观测 ES；每个 index 校验 count、strict mapping、alias 和确定性抽样 ID。
- 迁移状态原子记录 completed index，中断后仅重试未完成项；验证前不删除任何旧文档，切换后只由显式 cleanup 移除旧 Logs/Events。
- backup format v2 分别封装 MySQL/Redis/Rabbit/Kafka/VM/搜索 ES/观测 ES/插件状态，保持认证、checksum、强归属和原子 restore。
- restore 接受 v2 和 Phase 16/17 format v1；v1 输入经同一拆分流程导入，失败时不产生伪完成目标。

### 3.3 HTTP 与数据库背压

- Frontend 限制 worker connections、keepalive、request body 和 upstream timeout；Backend 使用全局有界在途 semaphore 及有限的路由例外。
- 饱和时返回 HTTP `503`、`server_overloaded` 和 `Retry-After: 1`，请求不进入 handler；该 code 进入统一错误目录和双 Frontend 安全提示。
- MySQL max-open/max-idle/lifetime/wait timeout 改为强类型配置，多 Backend 的连接总上限必须小于 MySQL 容量且保留 migration/doctor 预算。
- Outbox 定期取样 pending+leased 和 oldest age，到高水位时拒绝会产生新事件的业务写入，低于低水位后恢复。
  最大超出量必须能由 Backend 副本数×在途写请求×单请求最大事件数推导。

### 3.4 RabbitMQ、Kafka 与 sink 背压

- RabbitMQ main/retry/dead queue 同时配置 max-length/max-length-bytes 与 `overflow=reject-publish`；publisher confirm nack/return 必须使 Outbox 释放 lease 或 consumer requeue。
- 禁止 drop-head 和未记录的 TTL 丢失；满队列、retry 满和 dead 满都有独立指标和安全日志。
- Kafka 保留 Router records/bytes 双上限，增加 topic retention/segment 和 broker 磁盘预检；缓冲满、ack timeout 或磁盘压力时 Router 不返回 `202`。
- Marshaller 每 partition 只处理一条未 commit 记录，VM/观测 ES client 的连接、body、timeout 和 retry backoff 有硬上限；恢复后从未提交 offset 追赶。
- log/event 本地队列在 Router 接受前可有界丢弃，但必须暴露 dropped counter、queue full/recovered 状态和运维诊断；Router 接受后不得静默丢失。

## 4. 故障与超载矩阵

1. 分别停止搜索 ES 和观测 ES，验证另一数据面继续写入/查询，社交 MySQL 代表流程不被阻断。
2. 分别压满 HTTP in-flight、MySQL pool、Outbox、Rabbit main/retry/dead、Router buffer/Kafka 和两个 sink。
3. 验证每一层只出现规定的拒绝或积压，无 OOM、无无界 goroutine/队列/重试/日志增长、无伪成功。
4. 故障解除后 10 分钟内清空本轮积压，搜索/通知和 Metrics/Logs/Events 满足 30/60 秒的新数据收敛目标。

## 5. 不在本批范围

- Elasticsearch 集群 HA、跨宿主副本、新 Exporter 或新存储。
- 对没有 Phase-18-01/02 瓶颈或本批超载证据的索引/SQL/缓存调优。
- 独立健康 canary 和合同代码生成。

## 6. 验收与完成条件

- clean、`1.14.5` 和 `2.0.2` 输入均完成双 ES 收敛，历史数据与恢复后新写入可查。
- 双 ES 使用独立凭据/卷/资源，交叉停机不拖垮另一数据面，format v1/v2 restore 通过。
- 七类背压点的正常、饱和、拒绝、恢复和指标场景通过，已接受业务无静默丢失。
- 本批验证各层有界背压、单层恢复、无 OOM/无界增长和无伪成功；完整 150/300 RPS SLO、
  三轮重复性及跨系统最终收敛由 Phase-18-05 统一判定。

## 7. 固定验证命令

```bash
scripts/verify-phase18-backpressure.sh --self-test
scripts/verify-phase18-backpressure.sh --manifest dist/release-manifest.json --from-manifest "$GOPULSE_PHASE17_MANIFEST" --work "$GOPULSE_PHASE18_WORK"
scripts/verify-backup-restore.sh --manifest dist/release-manifest.json --format-v1 "$GOPULSE_PHASE17_BACKUP" --work "$GOPULSE_PHASE18_WORK/restore"
python3 scripts/verify-phase18-evidence.py --backpressure "$GOPULSE_PHASE18_WORK/evidence/backpressure.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.3 --base-ref upstream/main
git diff --check
```

创建同名实施记录，更新 `VERSION=2.0.3`，只提交本批文件后停止。
