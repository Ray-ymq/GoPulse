# Phase 7-01：Message Router 与 Kafka 传输闭环开发记录

## 1. 执行信息

- 批次：`Phase-07-01`
- 目标版本：`1.4.1`
- 开发分支：`develop/1.4.1`
- 基线：`origin/main` at `b2fe9f005d389ff2855dd8a2af87c7df2058a92c`
- 执行环境：WSL2 Linux filesystem `/home/ray/GoPulse`，Go `1.26.7`，Docker Engine `29.7.2`，Docker Compose `v5.5.0`
- 本地状态：实现与固定本地验收已完成；远程 push gates、自动 Pull Request 和合入状态在提交/推送后补充。

开始前工作区存在用户未跟踪文件 `使用指南.md`。本批未读取、修改、暂存或提交该文件。

## 2. 实际完成工作

### 2.1 Message Router

- 创建独立 Go 1.26 module `github.com/Ray-ymq/GoPulse/router`，固定 `github.com/twmb/franz-go v1.21.0`。
- 实现严格配置加载和范围校验：IP 监听地址、端口、至少 32 bytes 且无 CR/LF 的服务 token、请求/生产/关闭时限、消息/缓冲上限、去重 broker 列表和固定 Topic。
- 实现 `GET /health`、受 Bearer 保护的 `GET /ready` 和 `POST /internal/v1/messages`。
- Bearer token 先做 SHA-256 再常量时间比较；Cookie、query token 和 JWT 不能替代 Router 服务身份。
- 实现 1 MiB 有界正文读取、严格 `application/json`/无 Content-Encoding、唯一顶层 JSON object、UTF-8、重复/未知/缺失字段、尾随 token、message ID、UTC timestamp、payload object 和唯一 `Idempotency-Key` 校验。
- 只允许 Envelope v1 的 `metrics/redis`，并通过只读路由表写入 `gopulse-observability-v1`。
- Kafka record key 固定为 `message_id`，value 直接使用原始 HTTP body bytes，不重新 marshal，不改写业务 timestamp。
- Producer 使用 `acks=all`、franz-go 默认幂等协议写、显式有界幂等取消、256 records/8 MiB 缓冲和默认 3s 生产窗口；HTTP 仅在 delivery callback 成功后返回 `202`。
- 生产窗口取消时立即放弃本地缓冲并刷新 Topic 客户端状态，使 HTTP 有界失败且 Kafka 恢复后无需重启 Router。该路径保留“可能已经写入、调用方未获确认”的潜在重复语义。
- 实现 JSON 结构化生命周期日志、信号处理、HTTP 在途请求 shutdown 和 Kafka client 有界关闭。

### 2.2 Kafka 与验证 Consumer

- `deploy/compose.yaml` 增加 `apache/kafka:4.3.1` 单节点 KRaft、controller/internal/external listeners、loopback external port、健康检查和 `kafka_data` volume。
- 禁用 broker 自动建 Topic；`kafka-init` 经 internal listener 使用 `--if-not-exists` 创建并 describe `gopulse-observability-v1`，参数为 1 partition / replication factor 1。
- 增加 `cmd/verify-consumer`，要求显式 partition 与 `[start,end)` offset 范围，使用唯一验证 client ID，输出包含 key、base64 value、partition 和 offset 的 JSON Lines 证据，不使用 consumer group、不提交 offset。

### 2.3 生命周期、验收和 CI

- `.env.example` 增加 Kafka/Router 全部配置，并把 Monitor 默认正式接到 Router；示例 token 明确仅用于本地开发。
- `scripts/dev.sh` 增加 Kafka/Router 配置和端口安全检查，等待 Kafka 健康、执行 Topic 初始化、构建 Router、以强 PID 记录启动并通过 authenticated readiness 后才启动 Monitor。
- `scripts/down.sh` 在 Monitor/Exporter 后停止 Router，再清理本项目 Compose；日常 Kafka volume 保留。
- `scripts/verify.sh` 只读检查 Kafka container、initializer、volume labels、Topic 参数、Router PID、health/readiness 和认证，不消费业务 record。
- 新增 `scripts/verify-router.sh`：随机隔离 Compose project、Kafka/Redis/Router/Monitor/Exporter 端口、Kafka volume、plugin root、进程和 Consumer identity；覆盖直接请求原始 bytes、非法请求不入 Topic、真实 Monitor success/target unavailable、Kafka stop/recovery、健康/就绪分离和完整清理。
- CI 增加 Router format/unit/vet/race/真实传输验收 job；脚本与 Compose job 增加 Router LF、Bash syntax、自检、Kafka 4.3.1、loopback、禁用 auto-create 和固定 Topic 检查。

### 2.4 文档与版本

- 更新根 README、Router README 和 Monitor README，记录职责边界、配置、错误/重复语义、生命周期和验收入口。
- 根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 同步为 `1.4.1`。

## 3. 变更文件

- `.env.example`
- `.github/workflows/quality-gates.yml`
- `README.md`
- `VERSION`
- `deploy/compose.yaml`
- `frontend/package.json`
- `frontend/package-lock.json`
- `monitor/README.md`
- `router/README.md`
- `router/go.mod`
- `router/go.sum`
- `router/cmd/router/main.go`
- `router/cmd/verify-consumer/main.go`
- `router/internal/config/config.go`
- `router/internal/config/config_test.go`
- `router/internal/envelope/envelope.go`
- `router/internal/envelope/envelope_test.go`
- `router/internal/httpserver/server.go`
- `router/internal/httpserver/server_test.go`
- `router/internal/kafka/producer.go`
- `router/internal/routing/routing.go`
- `scripts/dev.sh`
- `scripts/down.sh`
- `scripts/verify.sh`
- `scripts/verify-router.sh`
- `scripts/ci/test_auto_pr_workflow.py`
- `scripts/ci/test_verify_business.py`
- `dev/imple/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md`
- `dev/imple/Phase-07/Phase-07-总实施方案.md`
- `dev/logs/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md`

## 4. 验证记录

实施期间已执行并通过：

- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- `scripts/verify-router.sh --self-test`
- `scripts/verify-router.sh`
  - 真实读取 direct byte-integrity record；
  - 真实读取 MetricsMonitor `success` record；
  - 真实读取 Redis 停止后的 `target_unavailable` record；
  - Kafka 停止时 Router `/health=200`、`/ready=503`、发布 `503`；
  - Kafka/Redis 恢复后不重启 Router/Monitor 即再次读取 `success` record；
  - 隔离 container/network/volume/process/listening-port 清理检查通过。
- Router 定向 `go test -count=1 ./...`（实现过程中按受影响修改重跑）
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-router.sh`
- `git diff --check`

稳定最终代码/配置 diff 上执行的固定门禁：

- `(cd router && test -z "$(gofmt -l .)")`：通过。
- `(cd router && go test -count=1 ./...)`：通过。
- `(cd router && go vet ./...)`：通过。
- `(cd router && go test -race -count=1 ./...)`：通过。
- `(cd monitor && test -z "$(gofmt -l .)")`：通过。
- `(cd monitor && go test -count=1 ./...)`：通过。
- `(cd monitor && go vet ./...)`：通过。
- `(cd monitor && go test -race -count=1 ./...)`：通过。
- `(cd exporters/redis && go test -count=1 ./...)`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：24 项通过。首次运行发现 CI product-job 计数和 `down.sh` 默认值断言仍是 Phase 6 预期；更新直接受影响测试后通过。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/1.4.1 --base-ref upstream/main`：通过。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- 随机 Compose project 的日常 `kafka` + `kafka-init` 启动验证：initializer 在 Kafka healthy 后成功退出为 `exited|0`，通过。
- `scripts/verify-router.sh --self-test`：通过。
- `scripts/verify-router.sh`：最终稳定代码上通过。
- `scripts/verify-monitor.sh --self-test`：通过。
- `scripts/verify-exporter.sh --self-test`：通过。
- `git diff --check`：通过。
- Transport dependency boundary grep：Backend/Monitor 无 Kafka SDK 或 Kafka Go 引用，Router 无 RabbitMQ/AMQP 引用，通过。

按照批次方案，本批未修改 Backend 业务代码，因此没有重复运行完整 `verify-business.sh`；跨域业务回归留给 Phase-07-02。

## 5. 与方案的偏差

- 验收 Consumer 采用无 group 的显式 partition assignment，而不是临时 consumer group。它仍使用随机唯一 `client.id`，只读取明确 `[start,end)`，且完全不提交 offset；这样更直接满足“不污染正式 offset”的验收边界。
- franz-go 在 broker 已停止且幂等请求可能在途时，默认会为了保留 sequence window 拒绝快速取消。为满足方案的固定有界 HTTP 语义，本批启用 `AllowIdempotentProduceCancellation`，并在调用窗口到期时立即中止本地缓冲和清除客户端 Topic 状态。此选择明确牺牲取消路径的客户端去重保证，与方案要求记录的“不确定写入/潜在重复”语义一致。
- 默认隔离验收没有启动未参与本批链路的 MySQL/Backend/RabbitMQ/Elasticsearch，以保持验证范围只覆盖 Router 直接依赖和真实 Monitor/Exporter 链路；Phase-07-02 负责最终业务隔离回归。

## 6. 已知限制与后续事项

- 单节点、PLAINTEXT、1 partition / replication factor 1 仅用于本地开发和验收，不代表生产 Kafka 拓扑。
- 没有 SASL/TLS、多 broker、Schema Registry、多 Topic、持久去重、磁盘 spool、重放、transaction、Marshaller 或 VictoriaMetrics。
- 相同 `message_id` 可能存在多条 Kafka record；Phase 8 正式 Consumer 必须按交接契约处理潜在重复。
- 本记录的“本批完成”状态仍受远程 push gates、Pull Request 合入 `main` 和远程版本状态约束。
