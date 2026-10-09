# 观测数据保留与生命周期

Phase-20-04 为 Logs、Events、VictoriaMetrics 和 Trace 验收工件定义了有限、可诊断的生命周期边界。产品默认只声明当前冻结候选中的行为，不把短时 fixture 或配置值扩大成长期容量承诺。

## Logs 与 Events

Logs 和 Events 默认各保留 7 个 UTC 日历日，合法配置范围为 1～90 日。评估时刻为 `T` 时，保留边界为：

```text
C = floor_UTC_day(T) - (retention_days - 1) days
```

日期 `D < C` 的索引才允许清理；`D = C`、未来日期和无效日期都保留。迟到消息使用其原始事件时间判断，过期消息不会重新创建旧索引，而是以 `expired_log_retention` 或 `expired_event_retention` 永久处理并提交有效 lease。

清理器每 60 秒运行一轮，单轮最多处理 16 个索引，单请求超时 3 秒，轮次预算 15 秒，最多 3 次重试（250ms～5s），追赶截止时间为 60 秒。它只绑定启动时读取的观测 Elasticsearch `cluster_uuid`，并且要求：

- 索引精确匹配 `gopulse-logs-v1-YYYY.MM.DD` 或 `gopulse-events-v1-YYYY.MM.DD`；
- 日期有效、对应 strict mapping 的 `_meta` 归属标记和固定 read alias 同时成立；
- 删除前先设置 `index.blocks.write=true`，再重新检查归属并删除；
- 业务搜索索引、未知同前缀索引和不能证明归属的索引永远不会进入删除请求。

写入端在准备写入前按 UTC 边界检查一次，在实际存储返回后再次检查。若清理和在途写入发生竞态，旧索引会被阻断/删除，写入端将过期结果作为永久失败返回；Marshaller 不会无限重试，也不会让过期索引复活。查询继续只使用 `gopulse-logs-v1-read` 和 `gopulse-events-v1-read`，已删除历史窗口返回空命中，当前和边界日期仍可读。

可诊断指标使用固定低基数标签：

- `gopulse_marshaller_retention_cleanup_total` 与对应 duration counter：`stream=logs|events`，结果为 `deleted|not_found|not_owned|invalid_date|not_expired|transient|permission_denied|budget_exhausted|unknown`；
- `gopulse_marshaller_retention_late_records_total`：结果为 `accepted|expired|invalid_date`；
- `gopulse_marshaller_retention_retries_total`：结果为 `transient|permission_denied|unknown`；
- `gopulse_marshaller_retention_last_success_timestamp_seconds` 和 `gopulse_marshaller_retention_blocked`：仅使用 `stream` 标签。

## VictoriaMetrics 与 Trace 工件

VictoriaMetrics 固定使用 `victoriametrics/victoria-metrics:v1.151.0` 的原生 `-retentionPeriod=30d`。验收证明运行参数、当前样本可查询以及窗口内/窗口外样本提交行为；原生物理回收未被短时测试加速，因此不声明配置后立即回收。

Trace 只作为本批验收接收器，固定写入 Collector 归属 volume 的 `/var/lib/gopulse/trace/spans.jsonl`。文件上限为 16 MiB，总量上限为 64 MiB，最多保留 3 个备份文件；轮转和清理只作用于该归属目录，不代表产品提供长期 Trace 存储。

## 验证入口

组件指标目录使用 `make verify-plugins` 的原生专项验收校验。R01～R08 的完整生命周期执行器已随 Phase 18–20 正式矩阵退役删除；本节合同继续有效，但不再由该历史入口证明，源码与原始回执见 Git 历史及 [Phase-20 实施日志](../logs/Phase-20/)。固定 Go、Backend、Monitor、Frontend、runtime contract、版本、分支和 `git diff --check` 门禁按 Phase-20-04 方案执行。
