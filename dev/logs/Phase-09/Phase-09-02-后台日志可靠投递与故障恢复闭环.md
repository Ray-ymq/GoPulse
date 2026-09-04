# Phase-09-02：后台日志、可靠投递与故障恢复闭环实施记录

- 实施日期：2026-09-04
- 目标版本：`1.6.2`
- 开发分支：`develop/1.6.2`
- 基线：`upstream/main` at `ee6055ba2a99d0e85e38cc61d4b8a55534c04198`，基线版本 `1.6.1`
- 完成状态：本地实现、固定门禁与隔离真实验收通过；尚未 push、创建 Pull Request、执行远程门禁或合入 `main`

## 1. 实际完成内容

### 1.1 四类应用日志源统一接线

- Business Worker、Search Indexer 和 search-reindex 的专用配置加载均复用既有 `LOG_MONITOR_URL`、`LOG_MONITOR_INGEST_TOKEN` 与 `LOG_SHIP_*` 严格校验；未启用时继续保持 stdout-only。
- 新增进程日志运行时适配层，四类应用均使用 Phase-09-01 的同一有界异步 `logship.Shipper`，没有复制 HTTP client、队列或重试状态机。
- Worker/Indexer 在业务配置成功后建立远程 sink，RabbitMQ runtime、ack/nack、retry/dead 和 reconnect 控制流不接收 shipper 返回值；定向命令测试证明远程失败不改变业务退出结果。
- search-reindex 在参数和业务配置通过后才建立 sink；参数错误与配置错误仍只写 stdout。成功或失败结果先确定，再进行有界 drain，drain 超时仅输出 stdout-only 状态告警，不改变原退出码。
- `scripts/dev.sh` 向 Worker、Indexer 和一次性 reindex 传递同一日志环境，并在 Monitor ready 后执行初始化 reindex；`scripts/dev.sh` 信号清理和 `scripts/down.sh` 均先停止应用日志源，再停止 Monitor、Marshaller 和 Router。

### 1.2 源端可靠发送边界

- 临时网络、timeout、认证和非永久 HTTP 状态继续保留当前队首及固定 message ID，按既有有界退避重试，不能越过后续记录。
- `400`、`413`、`422` 被识别为永久输入拒绝，只丢当前远程副本并继续下一条；`401` 等状态仍维持降级重试。
- transport unavailable 状态只在一次故障状态转换时输出，恢复后输出一次 recovered 状态，避免每次退避循环刷屏；状态 logger 始终 stdout-only，不递归进入 shipper。
- queue full、同 ID 重试、永久拒绝继续、shutdown 取消和 stdout/remote exact-byte 行为均由确定性测试覆盖。

### 1.3 Monitor、Router、Marshaller 与查询扩展

- Monitor 的两次清洗入口扩展到固定 `backend`、`business-worker`、`search-indexer`、`search-reindex` service/message/module 词汇，并从已验证 payload service 生成匹配的 `logs/<service>` Envelope source。
- Router 与 Marshaller 公共 Envelope registry 显式接受四个 logs source，继续使用单一 `gopulse-observability-v1` Topic、原始 Envelope bytes 和 message ID key。
- Marshaller 为四类 logs source 注册同一日志 transformer/writer；transformer 再次验证 payload service 必须等于 Envelope source，永久无效记录安全提交并继续，ES 暂时失败仍不提交当前 offset。
- Backend 管理员日志查询 service 白名单扩展到四类生产 source，保留 event ID、时间、filter、PIT/cursor 与 DTO 省略字段合同。

### 1.4 隔离验收、生命周期、文档与版本

- `scripts/verify-logs.sh` 扩展为随机资源的真实后台链路验收：启动 Worker/Indexer，先执行 reindex，真实发帖和评论，按 Outbox event ID 查询对应 Indexer/Worker 日志，并查询 reindex start/result。
- 同一脚本注入一个 Kafka 永久坏日志并验证后继合法日志可写；停止 Elasticsearch 后接受日志，再启动 ES 并验证同一正式 group 恢复写入；保留同 message ID 重放唯一文档与 `401/403` 权限结果。
- `scripts/verify.sh` 增加只读的 Monitor POST-only route、固定日志 template 和 read alias 检查，不注入测试记录或移动 offset。
- 根 README、Monitor/Router/Marshaller README 已同步四类 source、best-effort/at-least-once 边界、单 Topic 顺序 backpressure 和停止顺序。
- 根 `VERSION`、Frontend `package.json` 与 lockfile 已同步为 `1.6.2`。

## 2. 主要变更文件

- Backend 命令与配置：`backend/cmd/business-worker/**`、`backend/cmd/search-indexer/**`、`backend/cmd/search-reindex/**`、`backend/internal/config/**`。
- Backend 日志与查询：`backend/internal/observability/logship/**`、`backend/internal/observability/processlog/**`、`backend/internal/logquery/**`。
- Pipeline：`monitor/internal/logs/**`、`router/internal/envelope/**`、`marshaller/internal/envelope/**`、`marshaller/internal/logs/**`、`marshaller/cmd/marshaller/main.go`。
- 生命周期与验收：`scripts/dev.sh`、`scripts/down.sh`、`scripts/verify.sh`、`scripts/verify-logs.sh`。
- 文档与版本：`README.md`、`monitor/README.md`、`router/README.md`、`marshaller/README.md`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`、本实施方案状态和本记录。

## 3. 实际验证

### 3.1 开工与资源基线

- `git fetch upstream main && git fetch origin main`：通过；`upstream/main` 与 `origin/main` 均为 `ee6055ba2a99d0e85e38cc61d4b8a55534c04198`。
- 从 `upstream/main` 创建 `develop/1.6.2`；基线 `VERSION=1.6.1`。
- 开工前工作区只有用户未跟踪文件 `使用指南.md`；本批未读取、修改、暂存或提交该文件。
- 开工前与隔离验收后 `docker compose ls` 均无运行项目；所有 `gopulse-logs-*`、`gopulse-marshaller-*` 与 `gopulse-acceptance-*` 随机项目均由各自 trap 清理。

### 3.2 定向与固定代码门禁

以下最终命令均通过：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
```

### 3.3 脚本、治理与配置门禁

以下最终命令均通过：

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/verify-logs.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-logs.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
```

### 3.4 真实隔离验收

- `scripts/verify-logs.sh`：最终通过。随机隔离 MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics、Router、Marshaller、Monitor、Backend、Business Worker、Search Indexer 和 search-reindex。真实 post/comment 的 Outbox event ID 分别关联到 `search-indexer`/`business-worker` processed 日志；reindex start 与 completed/skipped 可由 admin 查询；永久坏 Kafka 日志未写 ES 且后继继续；ES 停止期间接受的日志在恢复后写入；同 ID 重放保持一个文档；未登录/普通用户分别保持 `401/403`。
- `scripts/verify-marshaller.sh`：通过。真实 metrics success/up0/recovery、三类永久异常 continuation、双成员 rebalance、VM 与 Kafka 故障恢复、正式 group offset 安全和 captured-real replay 均保持通过，证明新增 logs source 未破坏 metrics handler。
- `scripts/verify-business.sh`：通过。Phase 0～3 浏览器、通知、搜索、Outbox、Worker/Indexer、retry/dead、broker/ES/reindex 和 Phase 4 四进程结构化日志矩阵无回归。

### 3.5 实施中发现并关闭的阻断

- 首次扩展验收把 Worker/Indexer reconnect minimum 设为低于既有配置下限的 `20ms`，命令按合同拒绝启动；fixture 改为合法的 `100ms`。
- 第二次扩展验收触发 Bash `set -u` 的同一行局部变量展开；拆分局部变量声明与赋值后通过 self-test。
- 第三次扩展验收发现 Backend 日志查询仍只允许 `service=backend`；扩展固定四类 service 白名单并增加定向测试后，跨 service event ID 查询通过。
- 第四次扩展验收在搜索 alias 初始化前发送 `post.created`，Indexer 正确进入 retry；把 reindex 前置到业务流量前后，真实 processed 关联通过。
- 最终 Backend race 门禁发现业务 logger 与 shipper 状态 logger 并发写同一个非并发安全 stdout writer 的竞态；共享运行时改为以同一互斥 writer 串行化完整 JSON 行，并在超时取消后等待 worker 确认退出，再由命令输出 shutdown 告警。

## 4. 与方案的差异

- 未修改 `.env.example`：Phase-09-01 已包含完整共享 `LOG_MONITOR_*` / `LOG_SHIP_*` 配置，本批只需让三个后台命令和脚本继承这些既有变量。
- 未修改 CI workflow：既有 Logs pipeline job 已执行 `scripts/verify-logs.sh`，脚本扩展后会自动覆盖本批代表性矩阵；Backend/Monitor/Router/Marshaller 既有 jobs 继续执行各自 unit/vet/race。
- 未改 Monitor/Marshaller module 边界：两者继续保有职责独立的严格日志 validator，避免跨 Go module 共享 internal 实现。
- 未新增专用 logs Topic 或 consumer group；按计划保留单 Topic、单 partition、正式 group 的有序 backpressure。
- 未执行 push、Pull Request、远程 checks 或合入；这些结果不能在本地实施记录中标记完成。

## 5. 已知限制与后续项

- 内存队列只在 LogMonitor 返回 `202` 后进入 Kafka 可恢复边界；进程崩溃、超过队列容量、永久输入错误或 drain 超时仍可能丢失远程副本，stdout 不受影响。
- 单 Topic/单 partition 意味着 Elasticsearch 暂时故障会延迟后续 metrics 与 logs；Topic 拆分、独立 group、磁盘 spool 和生产 SLA 不在本批范围。
- 日志索引仍未启用 ILM 或自动删除；容量与保留策略继续作为后续事项。
- Frontend 日志页面、全文检索、聚合、告警、EventMonitor 和冻结 PowerShell 更新均不在本批范围。
- 本批只有在 `develop/1.6.2` 推送、Pull Request 远程门禁通过并合入主远程 `main` 后，才能按实施方案第 11 节标记为完整完成。
