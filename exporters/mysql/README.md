# MySQL 诊断指标

插件使用固定只读查询。基础采集继续提供原有指标。新增诊断采集器默认全部启用；插件不会修改 MySQL 的采集配置。

文件配置的 `config.collectors` 或环境变量 `MYSQL_EXPORTER_COLLECTORS` 可以指定以逗号分隔的诊断采集器名称。省略或设为空字符串时全部启用，设为 `none` 时全部禁用。列表不得包含空项、空格、重复项或未知名称。文件配置示例：

```json
"collectors": "transactions,lock_waits,statement_digest,statement_events,errors,performance_schema,consumers,instruments,digest_lost"
```

基础采集器 `global_status` 始终启用。可选名称及指标如下。表中指标均带 `gopulse_mysql_` 前缀，时间单位均为秒。

| 采集器 | 指标与维度 |
|---|---|
| `innodb_metrics` | 保留死锁、undo 历史长度、undo 槽位、redo LSN 与 checkpoint age 指标；仅输出 MySQL 中处于 enabled 状态的条目 |
| `transactions` | `transactions_by_state`、`transaction_state_oldest_age_seconds`、`transaction_state_longest_lock_wait_seconds`、`transaction_state_rows_locked`、`transaction_state_rows_modified`，标签为 `state`；保留事务总数、整体最老年龄、锁行数和修改行数，并新增整体 `transaction_longest_lock_wait_seconds` |
| `redo_capacity` | 保留 `redo_log_capacity_bytes` |
| `buffer_pool` | `buffer_pool_capacity_bytes`、`buffer_pool_wait_free_total`；原有 Buffer Pool 使用量、脏页、页数与命中相关计数继续保留 |
| `lock_waits` | `row_lock_waiting_transactions`、`row_lock_blocking_transactions`、`row_lock_longest_wait_seconds`，标签为 `schema`、`table`、`index` |
| `statement_digest` | `statement_digest_executions_total`、`statement_digest_seconds_total`、`statement_digest_errors_total`、`statement_digest_rows_examined_total`、`statement_digest_rows_sent_total`、`statement_digest_temporary_disk_tables_total`、`statement_digest_no_index_used_total`，标签为 `schema`、`digest` |
| `statement_events` | `statement_event_executions_total`、`statement_event_seconds_total`、`statement_event_errors_total`，标签为 `event`；涵盖二进制协议 Prepare、Execute、Close stmt，SQL PREPARE、EXECUTE 和 COMMIT 事件，具体输出取决于服务器提供的事件 |
| `errors` | `errors_total{code,error}`，按错误码及名称统计 SUM_ERROR_RAISED；优先保留 1205（锁等待超时）、1213（死锁）、1317（查询中断），即使计数为零也输出；其他错误仅在计数大于零时输出 |
| `file_io` | `file_reads_total`、`file_read_seconds_total`、`file_read_bytes_total`、`file_writes_total`、`file_write_seconds_total`、`file_write_bytes_total`、`file_misc_operations_total`、`file_misc_seconds_total`，标签为 `file_type`、`event`；覆盖 InnoDB 和 binlog 文件事件，类型分为 redo、binlog、data、other |
| `wait_events` | `wait_events_total`、`wait_event_seconds_total`，标签为 `event`；覆盖 mutex、condition、rwlock、sxlock |
| `table_io` | `table_io_operations_total`、`table_io_seconds_total`，标签为 `schema`、`table`、`operation`；操作为 fetch、insert、update、delete |
| `index_io` | `index_io_operations_total`、`index_io_seconds_total`，标签为 `schema`、`table`、`index`、`operation`；操作与表统计相同 |
| `performance_schema` | `performance_schema_enabled` |
| `consumers` | `performance_schema_consumer_enabled{consumer}`，报告各 consumer 的 YES/NO 配置 |
| `instruments` | `performance_schema_instruments`、`performance_schema_instruments_enabled`、`performance_schema_instruments_timed`，标签为 `instrument_group`；按名称的前三段分组，覆盖 statement、文件和表 I/O、内部同步等待 |
| `digest_lost` | `performance_schema_digest_lost_total` |

事务状态保留服务器原值，并为缺失的 RUNNING、LOCK WAIT、COMMITTING、ROLLING BACK 输出零。事务为空时，总数、最老年龄和最长锁等待时间均为零。年龄由 MySQL 的 NOW(6) 与事务时间计算，不依赖 exporter 主机的时区。热点锁只统计 RECORD 锁；等待与阻塞事务分别使用 DISTINCT 去重。每个库、表、索引组独立计数，不应将组计数相加当作全局唯一事务数。

`file_misc_operations_total` 包含 MySQL 文件事件中的其他操作，其含义不能直接等同于 fsync 次数。索引为空字符串表示 NULL INDEX_NAME；MySQL 将未使用索引的访问及 INSERT 记入 NULL 索引组。详见 [MySQL 索引 I/O 摘要表说明](https://dev.mysql.com/doc/mysql-perfschema-excerpt/8.0/en/performance-schema-table-io-waits-summary-by-index-usage-table.html)。

文件、内部等待、表和索引采集只输出 `COUNT_STAR>0` 的条目。性能模式会为未启用或未发生的事件预置零值行，过滤这些行后，`collector_truncated=1` 才表示确实因 128 组或 64 KiB 上限丢弃了有活动的条目。内部等待仍要求对应 `wait/synch/*` instrument 已启用并计时；测试准备入口只启用回答日志、锁和事务竞争所需的同步对象。

## 采集状态与边界

每个采集器输出 `collector_enabled`、`collector_success`、`collector_duration_seconds`、`collector_failures_total`、`collector_truncated`，标签为 `collector`。失败次数在同一个 Client 生命周期内累计，插件重启后归零。禁用时 enabled、success、duration 均为零，不增加失败次数。

`up=1` 表示基础采集成功。诊断查询失败时，该采集器 success 为零，失败次数加一，本轮不输出该采集器的业务指标。读取中断、NULL 数值、负数、非有限数和重复序列也按失败处理，不输出部分结果。success 为一只表示查询与转换成功，不表示所有 instruments 和 consumers 都已启用。检查采集配置时，还需读取对应配置指标。

基础采集失败、整体超时或输出违反 HTTP 指标契约时，仍按原有协议返回 HTTP 503 和 `up=0`，该响应不附带分项状态。基础采集失败次数会在后续成功响应中显示。诊断采集共享整体 scrape_timeout；若某个查询耗尽总超时，本轮无法交付分项状态。

热点锁、digest、内部等待、表和索引访问各最多输出 128 组；文件 I/O 最多 64 组；错误最多 256 组。各有界采集器的正文达到 64 KiB 后，不再追加后续组，最后一组可能使正文略超 64 KiB。热点锁按最长等待排序；digest、文件、内部等待、表和索引按累计耗时排序；错误优先保留上述三个错误码，其余按次数排序。查询多读取一组以识别行数截断。达到行数或正文上限时，`collector_truncated=1`，success 仍为一。组排名变化可能导致时间序列离开当前输出，窗口计算须检查截断状态。

插件不输出 SQL 原文、参数、事务 ID、线程 ID、锁值或文件路径。digest 使用 Performance Schema 的摘要哈希；NULL schema/digest 使用空字符串，NULL 错误码使用 `unknown`。digest 空字符串代表未分配独立摘要的汇总桶，并不代表一条 SQL。

摘要表、错误表和事件表具有各自的覆盖范围。Prepare/Execute 事件统计用于补充参数化 SQL 的观察范围，不能保证为所有执行恢复 digest，也不能据此将数据库错误与 HTTP 500 一一对应。SUM_ERRORS=0 只说明所观察摘要中没有记录错误。表和事件的累计值会因 MySQL 重启、摘要清理或相应统计重置而归零，应使用 counter 的窗口增量或 rate。COMMIT 平均耗时可用同一事件的 `rate(statement_event_seconds_total) / rate(statement_event_executions_total)` 计算，次数为零时不计算平均值。

## 权限与版本

新增查询以 MySQL 8.0/8.4 为目标，使用 `performance_schema.data_locks`、`data_lock_waits` 和错误摘要表。低版本、MariaDB、权限不足或表不存在时，对应采集器会报告失败，其余采集器继续执行。

监控账号需能读取相关 Performance Schema 表；读取全局 InnoDB 事务与指标还需要相应 PROCESS 权限。按部署环境授予最小权限，无需授予业务表写权限。插件不会自动开启 Performance Schema、修改 setup_consumers 或 setup_instruments。配置指标可用于检查 global_instrumentation、thread_instrumentation、statements_digest、statement 和等待事件的 ENABLED/TIMED 状态。

## 验证

```sh
make ci-check MODULE=exporters/mysql
make ci-test MODULE=exporters/mysql
make ci-build MODULE=exporters/mysql
```

可选的本地集成测试只读取指定容器中的统计信息。容器须提供 `mysql` 客户端和 MYSQL_ROOT_PASSWORD 环境变量，测试不会打印密码或更改服务器配置：

```sh
GOPULSE_MYSQL_TEST_CONTAINER=gopulse-mysql-1 make ci-test MODULE=exporters/mysql
```
