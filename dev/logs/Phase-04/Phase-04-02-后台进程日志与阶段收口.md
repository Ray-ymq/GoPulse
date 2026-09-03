# Phase-04-02：后台进程日志与阶段收口开发记录

## 1. 执行信息

- 执行日期：2026-09-03
- 开发分支：`develop/1.1.2`
- 起点：已 fetch `origin`，从最新 `origin/main` 的 `fa7cdab`（`feat: complete HTTP request logging loop (#55)`）创建分支
- 起始版本：`1.1.1`
- 完成版本：`1.1.2`
- 初始工作树：干净
- 初始应用进程：当前工作区没有已运行的 Backend、Business Worker、Search Indexer 或 Frontend 进程
- 初始基础设施：保留既有 `gopulse` 与 `gopulse-phase0203-integration` Compose project，各为 3 个运行中容器；本批验收与 integration 使用独立随机 project，结束后均清理

## 2. 实际完成工作

### 2.1 Backend lifecycle 与 Outbox

- `cmd/server` 继续复用 `service=backend` 根 logger，并显式注入 `module=lifecycle` 与 `module=outbox` child logger；没有设置全局默认 `slog` logger。
- Backend 监听、停止、关闭失败和资源关闭失败改为固定 message、有限 `reason`/`resource` 的 Schema v1 JSON，不再记录监听地址、配置值或原始错误。
- `DispatcherOptions` 新增显式 `*slog.Logger`；未注入时使用 `service=backend,module=outbox` discard logger，生产装配显式传入 child logger。
- Outbox 在 confirm 后成功标记 published 才记录 `outbox event published`，字段为数值 `outbox_id`、`event_id` 和 `event_type`。
- claim、publish、mark、release、cleanup 与 envelope 失败改为固定 message 和有限 reason；空 claim、空 cleanup 与正常 poll tick 保持静默，事务、租约预算、confirm、释放、retention 和 cancellation 顺序未改变。

### 2.2 Business Worker 与 Search Indexer

- `RuntimeOptions.Logger`、`HandlerOptions.Logger` 和内部字段由格式化函数迁移为显式 `*slog.Logger`。
- 固定 profile 增加不可由环境覆盖的 service/module：Business Worker 使用 `business-worker/worker`，Search Indexer 使用 `search-indexer/search`。
- Handler 只在状态完成后记录一次 `event processed`、`event retry scheduled`、`event dead lettered` 或 `event ignored`；共同字段为 `event_id`、`event_type`、数值 `attempt` 和有限 `reason`，Search 成功记录额外包含安全的数值 `post_id`。
- self-event 在成功 ack 后记录 `reason=self_event`，没有通知副作用，也没有记录 recipient 或 Envelope/Payload。
- secondary publish、ack/nack、delivery stop、session close、连接/通道/delivery stream 中断和 shutdown timeout 使用固定日志，不输出 AMQP URL 或底层错误。
- 重连按状态转换记录首次 `connection unavailable`、后续 `connection restored` 或 `session interrupted`，保留原指数退避、jitter、confirm-before-ack、requeue、有限 retry/dead 和在途 handler join 语义。
- 两个命令入口在读取配置前创建固定 service logger；启动、停止、初始化阶段和 MySQL close 均输出 JSON。

### 2.3 search-reindex

- `cmd/search-reindex` 改为 `service=search-reindex,module=search` 的结构化开始、无操作、完成和失败日志，并关闭 `flag` 默认文本输出。
- `Reindexer.Run` 返回内部 `ReindexResult`，向唯一命令调用点提供 `Changed` 与安全 `DocumentCount`，用于记录 `result`、`document_count` 和 `batch_size`。
- 参数、退出码、MySQL 高水位复制、物理索引创建、alias 原子切换、尾部补偿和旧索引清理语义保持不变；日志不包含 generation、物理索引名、文档、DSL、连接地址或原始响应。

### 2.4 JSON 验收与敏感信息边界

- `wait_log_contains` 已替换为带 timeout 的逐行 JSON 字段 matcher；失败仅输出归属已确认临时目录中的有限 tail。
- search-reindex 输出写入独立应用日志文件，Backend、Business Worker、Search Indexer 和 search-reindex 分文件解析，脚本/Compose/Frontend 输出不冒充应用 Schema。
- `--logging-live` 增加 Outbox publish、Business Worker processed/self-ignore、Search Indexer processed 与 reindex lifecycle 的精确字段断言，并验证 HTTP request ID 与异步 event ID 各自职责。
- 默认完整业务矩阵结束时逐行验证四类应用日志的 Schema、service/module、时间、等级、事件字段、Outbox/Worker/Indexer event ID 交集、reindex 生命周期、重连日志上界和敏感值负面扫描。
- 完整矩阵首次运行发现 go-sql-driver/mysql 在故障路径直接向 stderr 输出包含 TCP 地址的文本。随后仅依据该依赖公开 `Config.Logger`/`Logger` API，在 `platform/mysql.go` 为应用连接配置 discard logger；没有读取第三方依赖源码，也没有改变数据库业务错误传播。

### 2.5 文档与版本

- README 更新为四类 Go 应用统一 Schema v1 JSON 日志契约、request/event ID 边界和验收入口。
- 根 `VERSION`、Frontend `package.json` 与 `package-lock.json` 同步为 `1.1.2`。

## 3. 变更文件

- Backend 命令：
  - `backend/cmd/server/main.go`
  - `backend/cmd/business-worker/main.go`
  - `backend/cmd/search-indexer/main.go`
  - `backend/cmd/search-reindex/main.go`
- Outbox：
  - `backend/internal/outbox/dispatcher.go`
  - `backend/internal/outbox/dispatcher_test.go`
- Worker：
  - `backend/internal/worker/profile.go`
  - `backend/internal/worker/runtime.go`
  - `backend/internal/worker/handler.go`
  - `backend/internal/worker/handler_test.go`
  - `backend/internal/worker/integration_test.go`
- Search 与平台：
  - `backend/internal/search/reindex.go`
  - `backend/internal/platform/mysql.go`
- 验收：`scripts/verify-business.sh`
- 文档与版本：`README.md`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`
- 本记录：`dev/logs/Phase-04/Phase-04-02-后台进程日志与阶段收口.md`

## 4. 实际验证

### 4.1 Backend 与 Frontend 固定门禁

以下命令在最终生产代码上执行并通过：

```bash
(cd backend && go test ./...)
(cd backend && go vet ./...)
(cd backend && go test -race ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
```

结果摘要：

- Backend 全量测试通过，race detector 未报告数据竞争，`go vet ./...` 通过。
- Frontend 9 个测试文件、46 项测试通过；typecheck 与 Vite production build 通过。

### 4.2 真实 integration

固定 tagged integration 命令未带安全开关时按既有设计失败并明确报告 `INTEGRATION_TESTS=1 is required`，未连接或修改任何数据库。随后创建随机 `gopulse-phase0402-integration-*` Compose project，使用白名单数据库 `gopulse_integration`、用户 `gopulse_integration`、Redis DB 15、动态 loopback 端口和独立 MySQL/Redis/RabbitMQ/Elasticsearch，执行迁移后运行：

```bash
INTEGRATION_TESTS=1 APP_ENV=test \
  go test -count=1 -tags=integration \
  ./internal/outbox ./internal/worker ./internal/notification ./internal/search
```

最终结果：四个 package 全部通过；随机 project 的容器、网络和 volumes 已清理。MySQL driver 日志抑制改动后重新执行同一隔离 integration，结果仍全部通过。

### 4.3 脚本、治理与 Compose

以下命令通过：

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.1.2 --base-ref upstream/main
git diff --check
```

结果摘要：

- safety self-test 接受 1 个安全目标，并在访问 Docker 前拒绝 6 个不安全目标。
- Python CI 单测 24 项通过。
- 版本元数据一致，`develop/1.1.2` 分支治理通过，Compose 配置与 Git whitespace 检查通过。

### 4.4 真实日志与完整业务矩阵

```bash
scripts/verify-business.sh --logging-live
scripts/verify-business.sh
```

最终结果：

- `--logging-live` 通过：解析 39 条 Backend JSON、关联 20 个真实 HTTP 请求；四进程解析结果为 Backend 39、Business Worker 4、Search Indexer 2、search-reindex 2。
- 完整 Phase 0～3 业务、通知、搜索、故障、重启和 Chromium 矩阵通过；四进程最终解析结果为 Backend 298、Business Worker 27、Search Indexer 38、search-reindex 8。
- 完整矩阵包含 Business Worker retry/dead、RabbitMQ 停止恢复、Worker/Indexer 重启、Elasticsearch 暂停恢复、search-reindex、Redis fallback 和最终业务事实检查。
- 随机 acceptance project 全部清理；脚本的开发状态快照比较通过，开始前既有 `gopulse` 与 `gopulse-phase0203-integration` 资源未被修改。

执行中出现并修复的两项验收失败：

1. 初版 focused flow 试图从业务 API 产生 self-event，但现有业务层会避免该无效通知事件；改为向隔离 RabbitMQ 发布合法、actor/recipient 相同的测试 Envelope，验证 Worker ack 后的 `event ignored`，不改变产品业务逻辑。
2. 完整故障矩阵捕获 go-sql-driver/mysql 的非 JSON TCP timeout stderr；通过公开 logger 配置抑制依赖文本后，完整矩阵和 focused logging 均重新执行并通过。

## 5. 与方案的偏差

- 没有缩减方案范围。
- 预计文件之外额外修改 `backend/internal/platform/mysql.go`。原因是完整真实故障验收观察到 MySQL driver 输出包含连接地址的非 JSON 文本，直接违反本批 Schema 和敏感信息边界；改动仅设置公开的 discard logger，是阻断验收所需的最小共享基础设施修复。
- 没有修改 `cmd/migrate`、数据库 migration、Frontend 产品源码、Compose 拓扑或冻结的 PowerShell 文件。
- 本地已完成全部固定门禁；尚未创建 Pull Request 或获得远程门禁结果，因此本记录不把远程检查或合入 `main` 写成已完成事实。

## 6. 已知限制与后续项

- Phase 4 的本地实现与验收已收口，但总方案定义的阶段完成仍要求本分支通过远程门禁并合入最新 `main`；该外部状态尚未发生。
- request ID 不进入 Outbox、Envelope、AMQP headers、notifications 或 Elasticsearch；异步关联继续使用 event ID。
- 动态日志级别、采样、日志文件 sink/轮转、采集、传输、存储、索引、查询、OpenTelemetry、LogMonitor 和 Kafka 均未实现，留给后续既定 Phase。
