# MySQL Exporter

Phase-14-02 的 server/global 状态插件。只接受结构化配置，不接受 DSN、任意 SQL、业务表或 label。

- container mode 固定 `mysql:3306`，host mode 固定 `127.0.0.1:3306` / `[::1]:3306`。
- 环境：`MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_DATABASE`、`MYSQL_USERNAME`、`MYSQL_PASSWORD`、
  `MYSQL_EXPORTER_CONNECT_TIMEOUT`、`MYSQL_EXPORTER_SCRAPE_TIMEOUT`。
- `database` 是必须校验的产品配置字段，但连接不选择默认 schema；全局状态不需要业务库权限。
- 专用账号只需 `GRANT USAGE ON *.*`。不授予 PROCESS、REPLICATION CLIENT 或业务 SELECT/写权限。
- 回环 `127.0.0.1:9122`：`GET /health` 仅报告进程状态；`GET /metrics` 为完整 Prometheus 文本。
- `--check` 读取相同完整快照，输出固定安全 JSON，成功 exit 0、失败 exit 1。
- 任意连接/认证/超时/字段错误：HTTP 503 + 唯一 `gopulse_mysql_up 0`（带 TYPE 行），不缓存旧值。
- SIGTERM/SIGINT 有界退出；日志不记录账号、密码、DSN、SQL 或上游错误。

## 指标

10 families / 11 samples；前缀统一为 `gopulse_mysql_`。

| 后缀 | 上游 | kind / 单位 |
| --- | --- | --- |
| up | 完整采集成功 | gauge / boolean |
| uptime_seconds | Uptime | gauge / seconds |
| connections | Threads_connected | gauge / count |
| max_connections | @@GLOBAL.max_connections | gauge / count |
| threads_running | Threads_running | gauge / count |
| queries_total | Queries | counter / count |
| slow_queries_total | Slow_queries | counter / count |
| transactions_total | Com_commit、Com_rollback | counter / count，`result=commit\|rollback` |
| buffer_pool_data_bytes | Innodb_buffer_pool_bytes_data | gauge / bytes |
| buffer_pool_dirty_bytes | Innodb_buffer_pool_bytes_dirty | gauge / bytes |

transactions 仅表示显式命令数，不代表 autocommit 或所有引擎事务。字段缺失不补零。
上游文档：MySQL 8.4 Reference Manual 的 SHOW STATUS、Server Status Variables、Server System Variables。

```bash
(cd exporters/mysql && go test ./...)
bash scripts/package-redis-exporter.sh --source mysql --version 1.11.2
```

包构建入口保留历史文件名，`--source` 只放行 redis/mysql/rabbitmq；新插件仅支持 Manifest v2。
账号交付入口与新卷/旧卷流程见 `deploy/plugins/README.md`。
