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

## 正式采样与 TTL A/B

`perflab/tools/sample_capacity.py` 独立以 1Hz 抓取 backend 指标，每次 HTTP 请求超时 500ms。Docker 资源流和 Redis exporter 采样独立运行，指标失败不会阻塞资源记录。输出记录 UTC、窗口、采样槽位缺口、抓取失败、资源数据年龄和配置白名单；token 只从环境读取，不写入元数据。

先保存并校验相同数据库快照与访问序列，记录二者 SHA-256；分别用 5m/30m 启动实例，固定 API 池 8、executor 8，并使用完全相同的请求序列和到达率。快照恢复、访问序列回放由现有 perflab recipe/load 流程完成。采样环境中的配置须与启动日志核对，不应将 shell 中未生效的配置作为证据。

```bash
export MYSQL_MAX_OPEN_CONNS=8 BACKEND_RESOURCE_REBUILD=8 REDIS_POST_DETAIL_TTL=5m
python3 perflab/tools/sample_capacity.py \
  --metrics-url http://127.0.0.1:29131/internal/v1/metrics \
  --redis-metrics-url http://127.0.0.1:29121/metrics \
  --snapshot-sha256 "$SNAPSHOT_SHA256" --sequence-sha256 "$SEQUENCE_SHA256" \
  --container backend --container mysql --container redis \
  --stage ttl-5m --warmup 1800 --measure 1800 --recovery 120 \
  --output .run/ttl-5m
# 恢复相同快照和访问序列，以 30m 重跑，输出 .run/ttl-30m。
python3 perflab/tools/compare_ttl.py .run/ttl-5m .run/ttl-30m
```

30m 只有在命中率增加至少 10 个百分点、回源减少至少 20%、Redis 实际内存低于 maxmemory 75%、没有新增缓存写失败时才采用。证据缺失、采样失败或不足 30 分钟则报告 inconclusive 并维持 5m。当前默认仍为 5m；短测不代表 TTL A/B 验收。

验证入口：`go -C backend test ./...`、`make integration SCOPE=business`。集成用例 `TestIntegrationRabbitMQDispatchAtLeast300PerSecond` 包含 claim、真实 RabbitMQ mandatory/confirm/return 与批量落库，硬门槛 ≥300 条/秒；另有 550k 行历史的索引和全候选受保护情况下的游标推进检查。使用 `GOFLAGS=-v make integration SCOPE=business` 可保存具体确认率与 SQL 计划。不得用 mock 吞吐替代该门槛。


## 本轮验证记录（2026-10-19 UTC）

真实 RabbitMQ 连续两轮分别发布并落库 5,000 条，10.9507s / 11.0088s，端到端速率为 456.59 / 454.18 条/秒（8 publisher、batch 50、outbox 池 4）。两轮均超过 300 条/秒硬门槛。550k 行历史回归确认 pending、lease recovery、published cleanup 三个扫描使用各自索引；首批 500 条全部受保护时，下一批仍删除 500 条。API 的 8 条连接全部占用时 `/ready` 仍返回 200。迁移状态、消费者去重和搜索旧 revision 回放测试通过。

`go -C backend test ./...`、`make check MODULE=backend`、关键模块 race 测试、devtools 全部单元测试、Python TTL 门槛测试和 promtool 告警规则校验通过。采样工具还以不可用指标端点进行 3 秒检查：指标和资源各保留 3 个采样槽位。

完整 business 集成测试未全通过：`TestIntegrationRedisReadProtectionR09` 的相对吞吐门槛连续三轮失败（2026-10-19 22:25–22:35 +0800 的三轮 business 集成测试）。protected hotspot 为 3074.3–3117.2/s，对照 3639.2–3706.1/s（83.4%–85.1%）；uniform 为 3040.9–3076.6/s，对照 3423.3–3714.6/s（82.8%–88.8%）。六次比较均值 84.9%，全部低于原 90% 门槛。按 16 并发把倒吞吐差换算成每请求服务时间，六组 A/B 相差 0.59–0.89ms（均值 0.78ms），与同窗口 p95 的上移一致；A、B 的 `db_ops` 均为每请求 2.00 次，B 的数据库耗时和连接池等待在三轮中均不高于 A，而两个变体唯一的运行时差异是 `BACKEND_RATE_LIMIT_ENABLED`，因此差距来自限流中间件在请求路径上新增的固定延迟，而不是数据库工作量。本轮把暖读 SQL 从历史 v1 的 6.002 次/成功降到 2.00 次/请求，请求处理变快后该固定延迟的占比反而升高，相对吞吐降至 84.9%。不能因此放宽相对吞吐门槛，也不能将本轮标记为全部验收通过；后续应定位限流路径的固定成本（例如批处理等待和脚本往返）并在不放宽门槛的前提下降低它，再单独重跑 R09 复核。

正式 5m/30m TTL A/B 的两组 30 分钟预热和 30 分钟测量尚未执行：`.run/ttl-5m` 与 `.run/ttl-30m` 均无采样输出，默认仍保持 5m。`sample_capacity.py` 与 `compare_ttl.py` 已按上文门槛实现（命中率提高至少 10 个百分点、回源减少至少 20%、Redis 峰值内存低于 maxmemory 75%、候选零缓存写失败、快照与访问序列哈希及非 TTL 配置一致；证据不足时输出 inconclusive 并维持 5m），但工具测试不替代正式实验。`--snapshot-sha256`/`--sequence-sha256` 需由操作者计算后传入，工具只校验两次运行取值一致；采样记录的有效配置取自采样进程环境，仍须与 backend 启动日志逐项核对，Redis 未配置 maxmemory 时会因缺少内存证据判为 inconclusive。P1–P2 的长窗口验收仍待完成。

后续测试按用户要求单独进行；本轮不继续启动性能或 TTL 实验。以上结果为交接前已执行的验证，R09 和 TTL 待验收项保持如实记录。
