# Phase-09-01：Backend 日志端到端查询闭环实施记录

- 实施日期：2026-09-04
- 目标版本：`1.6.1`
- 开发分支：`develop/1.6.1`
- 基线：`upstream/main` at `ef8e4b46eec61d4012dcf7b577294f0d21ce8299`，基线版本 `1.5.4`
- 本地状态：实现与固定本地门禁通过；推送、Pull Request、远程 checks 和合入状态在提交后执行并在最终交付说明中记录

## 1. 实际完成内容

### 1.1 Backend stdout 与非阻塞日志投递

- 新增 `backend/internal/observability/logship` 有界异步 shipper：固定容量、单 worker/单队首在途、32 位小写十六进制 message ID、同项重试复用 ID、HTTP timeout、拒绝 redirect、有限响应读取、指数退避和有界关闭。
- 新增 logging shipping writer，保证 Schema v1 JSON bytes 先写 stdout，stdout 成功后才尝试 non-blocking enqueue；queue full 或远端失败只影响远程副本。
- shipper 状态使用独立 stdout-only logger，不把日志正文、token、URL、响应正文或底层错误写入状态日志，避免递归。
- Backend server 仅在 `LOG_MONITOR_URL` 配置时启用远程 sink；本批未接入 Business Worker、Search Indexer 或 search-reindex。
- 增加 loopback URL、专用 token、timeout、queue、retry、shutdown 以及固定 Backend 日志查询配置校验。

### 1.2 LogMonitor 接收与第一次清洗

- 新增 `POST /internal/v1/logs`，使用与 `MONITOR_API_TOKEN` 不同的 `LOG_MONITOR_INGEST_TOKEN`。
- 严格限制单条 `application/json`、无 `Content-Encoding`、最大 64 KiB、唯一且合法的 `Idempotency-Key`。
- 新增 Backend Schema v1 validator：递归重复 key、未知字段、嵌套/null、类型、时间、service/module/message 词汇、关联 ID、数值、route/token 和敏感哨兵均在进入 Router 前校验。
- 使用 header message ID 和原日志时间构造 `logs/backend` Envelope v1；只有 Router `202` 返回 ingest `202`，传输失败映射为安全 `503 transport_unavailable`。
- MetricsMonitor 和插件管理接口保持原有发布接口与生命周期。

### 1.3 Router logs 路由

- Envelope validator 新增 `logs/backend` 合法组合，同时保留 `metrics/redis` 和 unsupported 组合拒绝语义。
- `logs` 与 `metrics` 都路由到固定 `gopulse-observability-v1` Topic；Kafka key 仍为 message ID，value 仍为 HTTP 原始 bytes。

### 1.4 Marshaller typed handler 与 Elasticsearch

- 公共 Envelope decoder 与 metrics payload 校验解耦，保留 metrics typed payload，并为 logs 保留严格原始 payload。
- Processor 新增显式 `metrics/redis`、`logs/backend` target registry；永久无效 record 记录固定 reason 并提交，暂时存储失败保留既有重试/ownership/commit 语义。
- 新增 Marshaller 第二次 Backend Schema v1 校验与日志 document 转换；payload `timestamp` 映射为 `@timestamp`，transport message ID 不进入 `_source`。
- 新增固定 `gopulse-logs-v1-template`、`gopulse-logs-v1-YYYY.MM.DD` 和 `gopulse-logs-v1-read` Elasticsearch writer；mapping 为 `dynamic: strict`，Envelope message ID 固定作为文档 `_id`。
- Marshaller readiness 同时检查 Kafka、VictoriaMetrics 和 Elasticsearch；metrics 实际故障恢复/重放验收保持通过。

### 1.5 Backend admin 日志查询

- 新增 `GET /api/v1/observability/logs`，路由顺序为 authentication 后 admin authorization，再进入 handler/repository。
- 新增固定 read alias repository、PIT、`@timestamp desc` + `_shard_doc desc` 排序、`search_after`、2 分钟 HMAC 签名 cursor 和安全 DTO。
- 查询参数仅允许 `from,to,service,module,level,message,request_id,event_id,error_code,limit,cursor`；默认 15 分钟、最大 24 小时、limit `1..100`，cursor 不得混入其他参数。
- alias 尚不存在返回空页；Elasticsearch 不可用返回 `503 logs_unavailable`；PIT/cursor 无效返回 `400 validation_failed`。
- 定向路由测试证明未登录 `401` 与普通用户 `403` 均不会调用日志 application/repository。

### 1.6 生命周期、CI、文档与版本

- 新增 `scripts/verify-logs.sh` 无 Docker self-test 与随机隔离真实纵向验收，覆盖真实注册请求、request ID 关联、管理员查询、`401/403`、固定 template/alias 和相同 message ID 重放幂等。
- 扩展 `scripts/dev.sh`、`.env.example` 和现有 Monitor/Marshaller 验收配置；未修改冻结 PowerShell。
- 扩展 reusable quality gates，新增 Backend log pipeline job，并把新脚本纳入 Bash/self-test 门禁。
- 更新根、Monitor、Router、Marshaller README；根 `VERSION` 和 Frontend package metadata 更新为 `1.6.1`。

## 2. 主要变更文件

- Backend：`backend/cmd/server/main.go`、`backend/internal/config/config.go`、`backend/internal/observability/logging/**`、`backend/internal/observability/logship/**`、`backend/internal/logquery/**`、`backend/internal/http/api.go`、`backend/internal/apperror/error.go`、`backend/internal/http/response/response.go` 及直接测试。
- Monitor：`monitor/internal/logs/**`、`monitor/internal/httpserver/**`、`monitor/internal/config/**`、`monitor/internal/metrics/publisher/publisher.go`、`monitor/cmd/monitor/main.go`。
- Router：`router/internal/envelope/**`、`router/internal/routing/routing.go`、README。
- Marshaller：`marshaller/internal/envelope/**`、`marshaller/internal/consumer/**`、`marshaller/internal/logs/**`、`marshaller/internal/elasticsearch/**`、`marshaller/internal/config/**`、`marshaller/cmd/marshaller/main.go`、README。
- 生命周期/治理：`.env.example`、`scripts/dev.sh`、`scripts/verify-logs.sh`、`scripts/verify-marshaller.sh`、`scripts/verify-monitor.sh`、`scripts/verify-router.sh`、`.github/workflows/quality-gates.yml`、`scripts/ci/test_auto_pr_workflow.py`。
- 文档/版本：`README.md`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`、本实施方案状态和本记录。

## 3. 实际验证

### 3.1 基线与资源快照

- `git fetch upstream main --prune`：通过；`upstream/main` 为 `ef8e4b46eec61d4012dcf7b577294f0d21ce8299`，`VERSION=1.5.4`。
- 开工前 `git status --short --branch`：`main...origin/main`，仅存在用户未跟踪文件 `使用指南.md`；本批未读取、修改、暂存或提交该文件。
- Docker daemon：`Ray-ymq 29.7.2`；开工前 `docker compose ls` 无运行项目；监听快照仅见 WSL DNS `10.255.255.254:53`。

### 3.2 Go module 门禁

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
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
```

### 3.3 脚本、Compose 与治理门禁

以下命令均通过：

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.1 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/verify-logs.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-logs.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
```

### 3.4 真实隔离验收

- `scripts/verify-logs.sh`：最终通过。随机隔离 MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 和四个 Go 进程；真实注册请求 `X-Request-ID=fa16f889084c00ab4a41ab32314fa21c` 查询到 `user registered` 与 `http request completed`；未登录为 `401`，普通用户为 `403`；同一受控 message ID 重放两次后 Elasticsearch 仅有一个 `_id` 命中；固定 template/read alias 可读；资源完成强归属清理。
- `scripts/verify-marshaller.sh`：通过。既有完整真实 metrics success/up0/recovery、三类永久无效 continuation、双成员 rebalance、Kafka/VM 故障恢复、offset 安全和 captured-real replay 均保持通过；本批新增 Elasticsearch readiness 未破坏原验收。
- 首次 `scripts/verify-logs.sh` 尝试因新增脚本中的 VictoriaMetrics healthcheck 使用了不兼容的 wget 认证参数而失败；改为固定 Basic Authorization header 后重新执行并通过。该失败属于验收脚本问题，不是产品链路失败。

## 4. 与方案的差异

- Monitor 与 Marshaller 分别保有职责独立的 Schema v1 validator 实现，而不是跨 Go module 共享内部包；这是为了保持 module/internal 边界和两次独立清洗语义。
- Backend 查询范围、alias 和 Marshaller template/index prefix 在代码中保持固定常量；对应环境变量只允许方案冻结值，避免形成任意索引或无界查询配置。
- 本地实施记录在首个实现提交中生成；Pull Request 编号、远程 checks 和合入结果无法在提交前真实记录，将在推送后的交付状态中说明，若需要落库则通过同批次后续文档提交补充。

## 5. 已知限制与后续项

- 本批仅把 Backend server 的 HTTP、业务、cache、Outbox 和 lifecycle logger 接入远程 sink；Business Worker、Search Indexer、search-reindex 接线属于 Phase-09-02。
- 内存队列在 LogMonitor `202` 前不具备 durable 语义；进程崩溃可能丢失未确认远程副本，stdout 仍保留原 Schema v1 行为。
- Elasticsearch 日志暂未启用 ILM/自动删除；容量、保留和完整故障恢复矩阵由后续批次处理。
- Frontend 日志页面、全文检索、聚合、告警、多 Topic 和 Windows PowerShell 更新均不在本批范围内。
