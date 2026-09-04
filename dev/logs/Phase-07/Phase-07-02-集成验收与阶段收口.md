# Phase 7-02：集成验收与阶段收口开发记录

## 1. 执行信息

- 批次：`Phase-07-02`
- 目标版本：`1.4.2`
- 开发分支：`develop/1.4.2`
- 基线：`origin/main` at `66c02e3762bd2fc95dd51d3460430c8a0f5064cd`
- 执行日期：2026-09-04
- 执行环境：WSL2 Linux filesystem `/home/ray/GoPulse`，Go `1.26.7`，Node.js `v24.20.0`，npm `11.19.0`，Docker Engine `29.7.2`，Docker Compose `v5.5.0`
- 完成状态：本地固定验收、远程 push gates、自动 Pull Request 和合入均完成；Phase 7 完成条件已满足。

开始前工作区存在用户未跟踪文件 `使用指南.md`、既有 `.run` 产物、五个 `gopulse_*` 日常 volume，以及已退出的 `gopulse-phase0203-integration` Compose 资源。本批未读取、修改、暂存、提交或清理 `使用指南.md`，未清理既有日常 volume/Compose 资源，并使用随机强归属资源执行验收。

Phase-07-01 的 PR #68 已于 2026-09-03 14:54:48 UTC 合入 `main`，merge commit 为本批基线 `66c02e3762bd2fc95dd51d3460430c8a0f5064cd`。

## 2. 实际完成工作

### 2.1 最终契约与传输闭环

- 核对 Router 仍固定使用 `github.com/twmb/franz-go v1.21.0`，Compose 固定 `apache/kafka:4.3.1`，Topic 为 `gopulse-observability-v1`。
- 核对 `monitor/go.mod` 不含 Kafka SDK，Monitor 仍只通过 HTTP Bearer、`Idempotency-Key` 和 `202 Accepted` Publisher 契约发布。
- 核对 Backend 仍使用 RabbitMQ AMQP，Router 不含 RabbitMQ 路由、Marshaller、存储、logs/events Topic 或字段转换。
- 在最终 `1.4.2` 构建上运行真实 Redis Exporter → Monitor → Router → Kafka → Consumer 验收，验证原始 bytes、record key、成功/不可用 Envelope、Kafka 故障和原进程恢复。
- 为 `verify-router.sh` 增加有界、安全的 JSON 证据行，保留 offset 范围、message ID、key、SHA-256、scrape 状态、故障 HTTP 状态和稳定 PID；不输出 token、原始 body、broker 凭据或内部错误。

最终 Router 验收保留的代表性证据：

- direct：offset `[0,1)`，message/key `2436e23cd195e8175494c384ee0f3bfc`，body/value SHA-256 均为 `6621186eee0369fb52a432c81b4d121d992b4f5aeedfd3405e315322c16a9ce4`，`byte_equal=true`。
- 拒绝不写：非法客户端 `topic` 字段请求前后 offset 均为 `1`。
- Monitor success：offset `[2,3)`，record offset `2`，message/key `35679d92724a5200c8a94ab5d98cdb89`，scrape status `success`。
- Monitor target unavailable：offset `[5,6)`，record offset `5`，message/key `8c7f0a370178bd5b08175d7078ca0771`，scrape status `target_unavailable`。
- Kafka 恢复：offset `[7,9)` 内 record offset `8`，message/key `e3ccdd3be2a9c66d25f7049fe20ae804`，scrape status `success`。
- Kafka 停止期间 Router `/health=200`、鉴权 `/ready=503`、发布 `503`；恢复前后 Router PID `69302`、Monitor PID `71759` 保持不变。

### 2.2 业务隔离验收阻断修复

首次执行 `scripts/verify-business.sh` 时，Compose 因新增的必填 `KAFKA_PORT` 未写入临时验收环境而在创建资源前失败。按固定矩阵做最小修复：

- 为业务验收分配、校验并写入随机非默认 `KAFKA_PORT`，将它纳入端口唯一性和默认端口拒绝规则。
- 将业务验收 Compose 启动范围收窄为 MySQL、Redis、RabbitMQ 和 Elasticsearch，并显式断言本项目没有 Kafka container。
- 因此业务矩阵在 Kafka/Router 未运行的可观测故障条件下执行，真实证明普通用户/admin 授权、社交操作、RabbitMQ 通知、搜索和日志不依赖 Kafka。
- 扩展 `scripts/ci/test_verify_business.py`，固定检查随机 Kafka 配置、显式服务启动集合和 Kafka 缺席断言。

修复后的完整业务验收通过：真实 Chromium 用户流程、管理员权限边界、RabbitMQ/Outbox/Worker 十项可靠性矩阵、搜索重建与增量索引、Redis 故障恢复及 Schema v1 安全日志均保持原契约，验收项目和 volume 完整清理。

### 2.3 生命周期、文档与版本收口

- 在独立 WSL Linux 临时目录和随机 Compose project `gopulse-lifecycle-6e641cc58d97` 上执行最终 `dev.sh → verify.sh → down.sh`。
- `verify.sh` 只读通过：五个长期 Compose 服务、Kafka initializer/Topic/volume、Router/Monitor/Exporter、Backend、Worker、Indexer、Frontend 和 `1.4.2` 插件版本均通过。
- 前台应用按生命周期清理后，`down.sh` 删除随机项目 containers/network并保留日常语义下的 volumes；验收 harness 随后只删除该随机项目的强归属 volumes。随机项目无进程、端口、container、network 或 volume 残留，开始前的用户日常 volumes 保持不变。
- 更新根 README 和 Router README，记录 Phase 7 最终链路、业务隔离条件和安全证据输出。
- 根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 同步为 `1.4.2`。
- 更新 Phase 7 总方案和本批方案状态为“本地实现与固定验收完成，待远程门禁和合入”；未提前标记 Phase 7 完成。

## 3. 文件变更

- `README.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `router/README.md`
- `scripts/verify-router.sh`
- `scripts/verify-business.sh`
- `scripts/ci/test_verify_business.py`
- `dev/imple/Phase-07/Phase-07-总实施方案.md`
- `dev/imple/Phase-07/Phase-07-02-集成验收与阶段收口.md`
- `dev/logs/Phase-07/Phase-07-02-集成验收与阶段收口.md`

## 4. 验证记录

### 4.1 Go、Frontend 与治理门禁

- `(cd router && test -z "$(gofmt -l .)")`：通过。
- `(cd router && go test -count=1 ./...)`：通过。
- `(cd router && go vet ./...)`：通过。
- `(cd router && go test -race -count=1 ./...)`：通过。
- `(cd monitor && test -z "$(gofmt -l .)")`：通过。
- `(cd monitor && go test -count=1 ./...)`：通过。
- `(cd monitor && go vet ./...)`：通过。
- `(cd monitor && go test -race -count=1 ./...)`：通过。
- `(cd exporters/redis && test -z "$(gofmt -l .)")`：通过。
- `(cd exporters/redis && go test -count=1 ./...)`：通过。
- `(cd backend && test -z "$(gofmt -l .)")`：通过。
- `(cd backend && go test -count=1 ./...)`：通过。
- `(cd frontend && npm test -- --run)`：最终 `9` 个 test files、`48` 项测试通过。
- `(cd frontend && npm run build)`：`vue-tsc --noEmit` 与 Vite production build 通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：`24` 项通过。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/1.4.2 --base-ref upstream/main`：通过。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。

### 4.2 固定集成与安全门禁

- `scripts/verify-router.sh --self-test`：通过。
- `scripts/verify-router.sh`：最终证据增强后的真实 Kafka/Redis/Monitor/Exporter 验收通过；第 2.1 节记录了 offset、ID、hash、故障状态和 PID 证据，随机资源清理通过。
- `scripts/verify-monitor.sh --self-test`：通过。
- `scripts/verify-exporter.sh --self-test`：通过。
- `scripts/verify-business.sh --self-test`：通过，1 个安全目标接受、6 个不安全目标拒绝且不访问 Docker。
- `scripts/verify-business.sh`：修复后完整通过；业务验收 project `gopulse-acceptance-9e23c2327a48` 未启动 Kafka，真实 Chromium、RabbitMQ/Outbox、搜索、Redis/restart 和日志矩阵通过并完成资源清理。
- 隔离日常生命周期：随机 project `gopulse-lifecycle-6e641cc58d97` 的 `dev.sh → verify.sh → down.sh` 通过，随后只清理该 project 的 volumes，开始前资源快照保持不变。
- `git diff --check`：通过。

## 5. 与方案的偏差和实际失败

- 首次 `scripts/verify-business.sh` 在 Compose 插值阶段失败：`KAFKA_PORT is required`。未创建容器；最小修复后只重跑直接受影响的脚本/测试和最终业务验收，结果通过。
- 隔离日常生命周期必须避开工作区已有的用户 `gopulse_*` volumes，因此在临时仓库副本中只把三份生命周期脚本的固定 Compose project 名替换为随机强归属 project；产品脚本的其他内容与最终 diff 一致。非交互后台 harness 的 SIGINT 被 shell 忽略后改用 SIGTERM触发现有同一清理 trap，应用清理、`down.sh` 和资源核对均通过。这是验收编排差异，不是产品生命周期变更。
- 临时生命周期副本最初将 Frontend `node_modules` 链接到主工作区，副本中的 `npm ci` 清空了主工作区生成目录。该目录未被 Git 跟踪；随后在主工作区重新执行 `npm ci`，并在最终 `1.4.2` 元数据上重新完成 Frontend tests/build。项目文件和用户未跟踪文档未受影响。
- Router 默认验收在证据输出修改前已通过一次；因脚本发生相关变化，按规则在最终脚本上重跑一次并记录最终证据。其余已通过的 Go 检查在 Go 代码/依赖未变化时未机械重复。

## 6. 已知限制与后续事项

- 单节点、PLAINTEXT、1 partition / replication factor 1 仅用于本地开发与验收，不代表生产 Kafka 拓扑。
- 没有 SASL/TLS、多 broker、Schema Registry、多 Topic、持久去重、重放、Marshaller、VictoriaMetrics 或可观测前端；这些按 Phase 8 及后续方案处理。
- Router 超时仍是“不确定写入”语义，相同 `message_id` 可能重复；Phase 8 Consumer 必须保留 ID 并实现相应幂等/重复处理。
- 本批开发提交 `608b6b3bb236196dbbeb40dfc1efe3ee06a931fc` 的 Auto PR and Merge workflow run `33845369264` 已成功完成；Backend、Router、Monitor、Redis Exporter、Frontend、Scripts and Compose、Integration、Branch governance 全部远程门禁通过。
- 自动创建的 PR #69 已于 2026-09-04 06:46:13 UTC squash merge 到 `main`，merge commit 为 `fd7f4bdb239be73d6729e42b600d19f02a15fbe0`；远程根版本为 `1.4.2`。
- Phase 7 已按总方案完成并停止；后续只按 Phase 8 计划处理正式 Consumer、Marshaller、VictoriaMetrics 和重复语义。
