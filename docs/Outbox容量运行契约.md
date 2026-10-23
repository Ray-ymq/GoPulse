# Outbox 与容量运行契约

API 事件写入继续使用业务事务传入的 executor。dispatcher、cleanup 和 outbox 采样共用独立 MySQL 池，默认最大连接 4；探针池最大连接 1。`MYSQL_TOTAL_MAX_OPEN_CONNS` 校验计入 API、outbox 和探针池，再乘当前角色副本数。通用 API 池默认 10，压测显式设 `MYSQL_MAX_OPEN_CONNS=8`。新增连接均由 role assembly 负责启动失败清理与退出关闭。

## 派发与清理

| 配置 | 默认 | 边界 |
| --- | --- | --- |
| OUTBOX_CLAIM_BATCH | 50 | 1–50 |
| OUTBOX_PUBLISH_CONCURRENCY | 8 | 1–32 |
| OUTBOX_MYSQL_MAX_OPEN_CONNS | 4 | 1–32 |
| OUTBOX_DATABASE_TIMEOUT | 2s | 10ms–30s |
| OUTBOX_PUBLISH_TIMEOUT | 5s | 10ms–30s |
| OUTBOX_LEASE_DURATION | 5m | 1s–10m，并满足批次预算 |
| OUTBOX_CLEANUP_BATCH | 500 | 1–500 |

claim 在同一 READ COMMITTED 事务内分别扫描 `(status,available_at,id)` 与 `(status,lease_expires_at,id)`。跨批次交替首选分支，首分支不足时补齐，随后一次 UPDATE 租约。每个 publisher 独占连接、channel、confirm 和 return 状态；一个 publisher 同时只有一条发布在途。整批发布结束后，通过 `BatchStore.MarkPublishedBatch` 批量提交成功确认的记录；按输入顺序返回每项租约丢失结果。失败发布走原有 ReleaseFailed。批量落库失败保留租约，允许租约到期后按 at-least-once 语义重投。

租约校验保守覆盖 `ceil(batch / concurrency) * publish_timeout + (batch + 2) * database_timeout + 1s`：包括 claim、成功确认和最坏的失败释放数据库预算。不要通过调小租约绕过长批次预算。消费者依赖 event_id 去重；搜索写入通过 revision 防止旧事件覆盖新内容。集成测试保留重复投递和旧版本写入检查。

cleanup 启动等待 60 秒，按 `(published_at,id)` 游标每批最多扫描 500 个候选，再对这些 ID 检查 requirement/command 保留资格并加锁删除。全批受保护也推进游标；候选耗尽后从头开始下一轮。保护条件继续沿用 retention cutoff，避免过早释放证据。删除与 `ReleaseRetentionTx` 在同一事务，迁移 17 为两张关联表增加 event_id 前导索引。进程重启会从头检查。

候选批次之间至少等待 1 秒，一轮结束后等待原 cleanup interval。后台 SQL 操作默认设置 2 秒 deadline；存储错误按 1/2/4/8/16/30 秒上限加抖动退避，成功复位。超时与取消保留 `errors.Is` 语义。cleanup 通过候选耗尽判断轮次结束，不以删除数量判断。

## 指标和可用性

backend 指标契约位于 `backend/internal/instrumentation/definitions.json`。现有缓存请求、回源、重建在途、waiter、资源拒绝、全局准入拒绝和限流决策继续全量计数，另补：

- `outbox_operation_duration_seconds_{bucket,count,sum}{operation="claim|cleanup",result="success|failure"}`：耗时分布；
- `outbox_cleanup_rows_total{result="candidate|deleted"}`：候选与实际删除数；
- `mysql_pool_{open_connections,in_use,max_connections,wait_total,wait_duration_seconds_total}{pool="api|outbox|probe"}`：每池占用和等待，1Hz 采样；
- `cache_writes_total{result="success|failure"}`：缓存写入结果；
- `freshness_events_total{stage="publish",result="success|failure"}`：真实发布确认成功/失败速率。

所有名称前缀为 `gopulse_backend_`。发布确认率与最终 outbox published 增量分别记录；不要把 broker 确认成功等同于落库成功。数据库锁等待应同步读取 MySQL exporter 或 performance_schema，而非从失败条数推断。

`/ready` 使用专用池执行 MySQL、schema、business state 和 bootstrap 检查，仍保留探针缓存和单飞；真实依赖故障仍返回不就绪。`/live` 只用于判断进程是否存活，不应以就绪失败直接触发存活重启。

限流故障维持 503，Prometheus `BackendRateLimiterUnavailable` 规则连续一分钟触发；规则见 `deploy/selfmonitor/alerts.yaml`，通知发送由运行环境的告警接收器负责。预期过载的 503（backend_busy、rate_limit_unavailable、post_read_unavailable）按 `(route,error_code)` 每秒保留前 5 条，其余在该秒结束时汇总。panic 与未预期内部错误完整记录，指标不采样。启动日志 `backend capacity configuration` 输出非敏感生效配置，可与采样元数据核对。

## 验证入口

执行 `go -C backend test ./...` 运行单元测试。执行 `make integration SCOPE=business`，在 Testcontainers 创建的隔离依赖中验证数据库迁移、消息派发和搜索链路。

集成用例 `TestIntegrationRabbitMQDispatchAtLeast300PerSecond` 包含 claim、真实 RabbitMQ mandatory/confirm/return 与批量落库，要求端到端速率至少达到 300 条/秒。另有 550k 行历史的索引检查，以及首批候选全部受保护时的游标推进检查。使用 `GOFLAGS=-v make integration SCOPE=business` 可保存具体确认率与 SQL 计划。

Redis 帖子详情缓存 TTL 默认保持 5m。正式 5m/30m TTL 对照测试尚未完成；压测采样工具和对照流程未包含在本次分支同步中。
