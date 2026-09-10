# Phase-14-02 实施记录

## 状态

已完成（2026-09-10）。目标版本与完成版本为 `1.11.2`；开发分支 `develop/1.11.2`。
从 fetch 后的 `origin/main` 创建 `develop/1.11.2`（origin/upstream 指向同一仓库）。

## 前置确认

使用隔离临时容器 `gopulse-p1402-mysql`（MySQL 8.4.0）与
`gopulse-p1402-rabbit`（RabbitMQ 3.13.3-management-alpine）确认上游字段与最小权限，
不修改已有业务卷。凭据仅在探测进程内生成，不记录原始响应或密码。

### MySQL 映射（已运行真实最小权限探测）

专用账号仅 `GRANT USAGE ON *.*`，不选择默认业务数据库，不授予 SELECT/PROCESS/REPLICATION CLIENT。
`database` 仍按 Schema 校验，但 server/global 查询不执行 USE、不查询业务表。
`SHOW GLOBAL STATUS WHERE Variable_name IN (...)` 和 `SELECT @@GLOBAL.max_connections` 成功，
隔离账号 `CREATE DATABASE gopulse_probe_denied` 失败。

| family 后缀 | 上游字段 | kind / 单位 | sample 数 |
| --- | --- | --- | --- |
| up | 完整采集成功 | gauge / boolean | 1 |
| uptime_seconds | Uptime | gauge / seconds | 1 |
| connections | Threads_connected | gauge / count | 1 |
| max_connections | @@GLOBAL.max_connections | gauge / count | 1 |
| threads_running | Threads_running | gauge / count | 1 |
| queries_total | Queries | counter / count | 1 |
| slow_queries_total | Slow_queries | counter / count | 1 |
| transactions_total | Com_commit / Com_rollback | counter / count，result=commit/rollback | 2 |
| buffer_pool_data_bytes | Innodb_buffer_pool_bytes_data | gauge / bytes | 1 |
| buffer_pool_dirty_bytes | Innodb_buffer_pool_bytes_dirty | gauge / bytes | 1 |

共 10 families / 11 samples。任一字段缺失、解析失败或目标不可用均不返回部分成功值。

### RabbitMQ API 探测

锁定版本官方 API 资料：`rabbitmq/rabbitmq-server` tag `v3.13.3` 的
`deps/rabbitmq_management/priv/www/api/index.html`（通过 raw.githubusercontent.com 读取）。
官方站点在当前环境返回 HTTP 403，改用官方仓库对应 tag 文档，无第三方源码审计。
文档明确 `message_stats` 仅出现已发生活动的字段；`deliver_get` 是四种 delivery/get 的总和，
redeliver 是其子集，不额外相加。

专用账号 `monitoring`，仅 `/` 的 configure/write/read=`^$`。
真实探测发现单个 `/api/vhosts/%2F` 返回 401（需要 administrator），不可采用该路径或增权。
`GET /api/vhosts`、`/api/vhosts/%2F/connections`、`/api/vhosts/%2F/channels`、
`/api/queues/%2F` 可读；应从 vhosts 数组严格选择唯一 `/`，不得使用 `/api/overview` 集群统计。
隔离受控队列发布一条消息后，vhost 的 publish=1、messages_ready=1、messages_unacknowledged=0。

## 验证范围补充

接入新 source 时发现既有 Events 的 producer 固定为 Redis；若不修改，新插件事件会误标或被拒绝。
因此将直接影响的 Monitor/Marshaller/Backend Events ID 校验扩展到本批三种已交付 ID，
并纳入其已有包测试；不扩大到其他业务或依赖审计。

## 实现与已解决的运行问题

- 新建两个独立 Go 模块，固定 loopback HTTP/health/check、安全错误、SIGTERM 与上游 deadline。
- 通用 Manager 新增 cluster config adapter；仅向子进程传入该 source 环境，独立端口/health/collector。
- Manifest v2 受限 packager 支持 `--source mysql|rabbitmq`，镜像编译进受信 release catalog。
- Monitor/Router/Marshaller/Backend 目录注册新 source；保留 Redis v1/v2 存储标签，
  MySQL/RabbitMQ 增加固定 producer provenance；Frontend 配置/管理与查询支持切换 source。
- 新增显式幂等账号部署入口；管理员文件仅部署进程读取，私有候选状态、归属冲突、锁、
  分 source 重试、完整 connection-test 后激活，均不替换原业务账号。
- 真实构建发现 archive 校验仍强制 Redis executable；改为按 manifest source 校验，
  并拒绝夹带其他官方 executable，新增对应安全边界测试。
- 真实链路发现 Marshaller consumer target map 未注册新 source；补齐两条生产 dispatch，
  不是仅放开 envelope 校验。
- 原验收 nc 在 stdin EOF 时提前关闭请求，取消 MySQL context，造成假 503；
  探测保持连接直到响应结束，未放宽 Exporter 失败条件。
- 错凭据 connection-test 按项目已有错误合同返回 HTTP 422，验收已修正原先猜测的 502。
- Frontend 目录测试旧 fixture 把所有 source/kind/unit 硬编码为 Redis/gauge/count；
  更新 fixture 以验证新增闭合目录，不放松生产校验。

### 制品验证

从实际 `gopulse/monitor:1.11.2` 镜像取包，用相同 executable 重新调用 packager，
两种包均 `cmp` 字节一致。归档 SHA-256：

- MySQL：`f87c99bfc17936655af90b1d0f5f619211d4d082e555a7f322c1b552e1fefdfb`
- RabbitMQ：`2afc72620e2cb7341c60dbee1bb5a284889dcb4fe4aebb55afdad1ba8df9ca24`

构建使用项目锁定 Go/Alpine Docker stages；Frontend 在宿主 npm build 后复制到既有项目
nginx runtime 镜像运行验收。Dockerfile 的 syntax frontend 拉取被省略（临时副本位于 `.run`），
其余 production build stages 未绕过。临时输出与凭据均不提交。

### RabbitMQ 完整映射

| family 后缀 | 上游 API / 字段或公式 | kind / 单位 | samples |
| --- | --- | --- | --- |
| up | 所有请求和必要字段验证成功 | gauge / boolean | 1 |
| connections | `/api/vhosts/%2F/connections` 长度，逐项核对 vhost | gauge / count | 1 |
| channels | `/api/vhosts/%2F/channels` 长度，逐项核对 vhost | gauge / count | 1 |
| queues | `/api/queues/%2F` 长度，逐项核对 vhost | gauge / count | 1 |
| consumers | 同上 consumers 求和 | gauge / count | 1 |
| messages | 同上 messages_ready / messages_unacknowledged 求和 | gauge / count，state=ready/unacked | 2 |
| published_total | `/api/vhosts` 唯一 `/` 的 message_stats.publish | counter / count | 1 |
| delivered_total | 同上 message_stats.deliver_get | counter / count | 1 |
| acked_total | 同上 message_stats.ack | counter / count | 1 |

共 9 families / 10 samples。只有官方资料明确零省略的三个 message counter 可补零；
空数组求和为零，已有队列缺少必要字段、null、解析失败或认证/请求错误不得按零。
不采用 `/api/overview`，不输出 queue/vhost/user/channel 名，不本地累计计数。

共享 MySQL 实际停止时，Backend 管理 API 的身份/权限查询依赖 MySQL，实际返回现有 HTTP 500；
验收据总方案 §12 改为检查该既有降级、Monitor 内部插件进程存活、安全 up=0 与重启恢复，
不要求停机期间 Backend 管理 API 正常，不扩展原业务容错。

MySQL 官方文档补充核对：Oracle 托管的 MySQL 8.4 Reference Manual，
`mysql-8.4-en/show-status.html` 与 `mysql-8.4-en/server-status-variables.html`：
SHOW STATUS 仅要求能够连接，不要求额外授权；Threads_connected 为当前连接，
buffer_pool_bytes_* 为字节而非 pages，Com_* 为语句命令计数。与已运行的 8.4.0 最小权限快照一致。

## 完成验证

下列检查实际执行并通过。命令输出保留在 `.run/p1402-build/checks/`；成功后没有因记录整理或
版本元数据更新重复执行未受影响的 Go/Frontend 测试。

| 命令 | 结果 |
| --- | --- |
| `(cd exporters/mysql && go test ./...)` | 通过 |
| `(cd exporters/rabbitmq && go test ./...)` | 通过 |
| `(cd monitor && go test ./...)` | 通过 |
| `(cd router && go test ./...)` | 通过 |
| `(cd marshaller && go test ./...)` | 通过 |
| `(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/http)` | 通过；同次增加直接受影响的 `./internal/eventquery`，原因见上文 |
| `(cd frontend && npm test)` | 17 个测试文件、70 项测试通过 |
| `(cd frontend && npm run typecheck)` | 通过 |
| `(cd frontend && npm run build)` | 通过 |
| `bash scripts/verify-plugin-metrics.sh --self-test` | 通过 |
| `bash scripts/verify-plugin-metrics.sh --sources mysql,rabbitmq` | 完整真实门禁通过，含 Playwright 1 项双插件浏览器流程 |
| `python3 scripts/ci/validate_versions.py` | 通过，VERSION / Frontend / .env.example 为 1.11.2 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.2 --base-ref upstream/main` | 通过 |
| `git diff --check`、`git diff --check upstream/main...HEAD` | 通过 |
| `bash -n scripts/{package-redis-exporter,reconcile-plugin-accounts,verify-plugin-metrics}.sh` | 通过 |
| `python3 -m py_compile scripts/ci/{reconcile_plugin_accounts,verify_plugin_clusters,verify_plugin_metrics}.py` | 通过 |

真实门禁最终证据目录：`.run/gopulse-p1401-37d4509b3c75/`（沿用既有强归属 harness 命名），
结果 `results.json` 与脱敏 `browser.log`；最终总输出 `.run/p1402-build/acceptance.log`。
之前的验收失败及修正原因已在上文记录，不将那些失败运行计为通过。

实际证明：

1. 新卷创建 USAGE / monitoring 专用账号并通过完整 connection-test 和 install/start。
2. 仅在本次归属资源内移除采集账号、换用空插件卷，保留已建立的 Phase 13 兼容业务数据，
   证明既有卷补建；重复调和不换密码、不改 grants，不改变业务帖子/搜索事实。
3. 账号已建立但激活中断时保留同一候选；重试核对私有配置与 Secret 后完成。
4. 状态查询成功，MySQL 代表性对象创建/业务写入返回精确权限拒绝，RabbitMQ 建队列被拒绝。
5. 两类全部 19 families / 21 samples 经 Backend 查询；MySQL max_connections 与实际 server 配置一致。
   RabbitMQ 受控发布后 vhost publish 累计量增加并可查询。
6. 错凭据 connection-test 不改变已运行实例；仅修改归属采集账号凭据的故障产生安全 up=0，
   不影响 Redis/原业务；恢复密码后仍由原进程采集。
7. 暂停目标模拟超时，以及实际停止 MySQL/RabbitMQ，均只有安全失败快照，无旧值/部分值；
   恢复后不重装、不清业务卷，进程记录不变。
8. Monitor 替换保留 stopped/running desired state；损坏 MySQL active 记录不阻止 Redis/RabbitMQ，
   恢复归属记录后可正常恢复。坏记录在单 ID API 表达为安全错误，不要求伪造 running 状态。
9. Redis 全链路、一条 Logs/Events、普通用户管理拒绝、帖子/搜索主路通过。
10. Browser 对 MySQL/RabbitMQ 分别连接测试、保留 Secret 配置替换、停止/启动、切换 source 和查询；
    Secret 提交后清空、不出现在 DOM。API/registry/events/logs 扫描通过，secret.json 为 0600。

## 文件变更范围

- `exporters/mysql/{go.mod,go.sum,README.md,cmd/mysql-exporter/main.go,internal/collector/*,internal/runtime/*}`
- `exporters/rabbitmq/{go.mod,README.md,cmd/rabbitmq-exporter/main.go,internal/collector/*,internal/runtime/*}`
- `monitor/internal/plugin/{adapter,cluster_config,catalog,manager,process,runtime,archive}.go` 及直接测试；
  `monitor/cmd/{monitor,plugin-package-metadata}/main.go`；`monitor/internal/metrics/{collector,envelope}/*`；
  `monitor/internal/events/contract.go`
- `router/internal/envelope/envelope.go`；`marshaller/cmd/marshaller/main.go`；
  `marshaller/internal/{envelope,metrics,events}/*` 的直接注册/校验和测试
- `backend/internal/exporterplugin/{catalog,configuration,cluster_config}.go`；
  `backend/internal/metricquery/{metricquery.go,cluster_test.go}`；`backend/internal/eventquery/eventquery.go`
- Frontend Exporters/Metrics 两个 view，exporters/observability service、指标类型、直接目录测试，
  `frontend/e2e/phase14-clusters.spec.ts`
- `scripts/package-redis-exporter.sh`、`scripts/reconcile-plugin-accounts.sh`、
  `scripts/ci/{reconcile_plugin_accounts,verify_plugin_clusters,verify_plugin_metrics}.py`
- `deploy/{compose.yaml,docker/observability.Dockerfile,plugins/README.md}`，README、Exporter/Monitor 文档，
  `.env.example`、`VERSION`、Frontend 两个版本元数据文件，以及本批计划状态/实施记录。

## 偏差、限制与 Phase-14-03 交接

- 不变更指标语义或验收合同；RabbitMQ 改用允许的列表 API 严格选择 `/`，未扩大采集权限。
- 账号调和为显式 Linux/Bash 部署步骤（Python 3 标准库），新旧卷共用，业务启动不依赖其成功。
  管理员需保管并备份受控候选目录；自动接管非归属账号、自动权限修复和密码轮换不在本批范围。
- 目标全停时遵循既有业务降级，不将 MySQL 停机期 Backend HTTP 500 改造为新容错功能。
- 真实环境为 Linux amd64 / Compose；没有宣称 macOS、原生 Windows、其他架构或 Kubernetes 已验收。
- Redis 历史迁移入口保持原 Phase-14-01 锁定镜像验收参数，本批没有重跑完整迁移矩阵；
  Redis 历史标签契约由现有测试保留，新链路与业务回归由本批真实门禁验证。
- Phase-14-03 可复用 adapter 接口、独立 revision/Secret 事务、受限 packager、per-ID process/collector。
  新增 source 必须同时注册 Monitor 契约、Router allowlist、Marshaller envelope **与 consumer target map**、
  provenance 和 Backend/Frontend 目录；不可只放行 envelope 后漏掉实际存储分发。
- 真实验收入口仍只允许 `--sources redis --migration` 或 `--sources mysql,rabbitmq`；
  下一批明确加入自己的有界 source 参数，不提前放开全部未交付插件。
