# Phase 7-03：Review 整改与阶段再验收开发记录

## 1. 执行信息

- 批次：`Phase-07-03`
- 目标版本：`1.4.3`
- 开发分支：`develop/1.4.3`
- 基线：`origin/main` at `ff2fc20a16df3158751962d5d3227c69de53472d`
- Review 文档提交：`2fb7105`（`docs: add Phase 7 implementation review`）
- 执行日期：2026-09-04
- 执行环境：WSL2 Linux filesystem `/home/ray/GoPulse`，Go `1.26.7`，Node.js `24.20.0`，npm `11.19.0`，Docker `29.7.2` / Compose `v5.5.0`
- 完成状态：本地实现和固定验收完成；源提交 `31ecc90` 通过 9 项远程 checks，PR #71 已以提交 `60f9aa8` 合入 `main`。

开始和结束时工作区均存在用户未跟踪文件 `使用指南.md`。本批未读取、修改、暂存或提交该文件。

## 2. 实际完成工作

### 2.1 严格 Envelope schema

- 在数值转换前检查 `schema_version` 原始 JSON token，只有数字 token 才进入 `json.Number.Int64()`；字符串 `"1"`、小数 `1.0` 和其他非整数表示返回 `message_invalid`。
- 增加 quoted/decimal schema 单元回归；合法 integer `1` 的原始 body bytes 保持不变。
- 真实 Kafka 拒绝矩阵包含 quoted schema，HTTP `400/message_invalid`，整组执行前后 Kafka end offset 均为 `1`。

### 2.2 Producer 有界并发与取消

- 删除容量为 1 且由 publish/readiness/close 共用的全局 slot。
- 使用 franz-go `TryProduce`，让配置的 `MaxBufferedRecords` 和 `MaxBufferedBytes` 成为真实非阻塞准入边界；缓冲满立即返回 `kgo.ErrMaxBuffered`，HTTP 映射为 `503 kafka_unavailable`，不在 Producer 外形成等待队列。
- record 使用独立固定 delivery timeout；HTTP request context 取消只结束该调用等待，不调用全客户端 abort，也不取消其他已接受 record。客户端级 `UnsafeAbortBufferedRecords` 只保留在 shutdown flush 失败路径。
- readiness 不再与 publish 共享锁，可在存在 pending record 时完成 broker/Topic 元数据检查。
- 新增 fake client 定向测试和真实 franz-go record/byte limit 测试，覆盖并发进入、records 满、bytes 满、单调用取消、readiness 与 shutdown。

### 2.3 Router 验收与资源安全

- `verify-router.sh --self-test` 移除 Docker Compose 调用；在不含 Docker 的 PATH 中成功执行。
- self-test 负向覆盖短 token、Consumer 非法 range、固定 Topic、project、相邻/非相邻端口冲突、PID start ticks/executable/cwd/marker、container service/config path、volume label/name、Compose 摘要篡改和清理上下文；伪造 PID record 被拒绝后测试进程仍存活，随后才显式终止。
- 五个随机端口改为在同一 Python 进程同时持有五个 socket，统一输出后关闭，并再次验证端口数量、范围和两两唯一。
- Router/Monitor 隔离进程增加 PID record，停止前校验 start ticks、executable、cwd 和 command marker。
- Compose 清理先校验 project、固定 Topic、Compose 文件 SHA-256/path，以及 container/volume project/service/path/name 归属；归属不一致时拒绝删除。
- 默认真实 Kafka 验收在单一 baseline 上执行 18 类拒绝：无/错/query token、普通用户/admin Cookie-only、错误 Content-Type、Content-Encoding、超限、重复 key、尾随 JSON、Idempotency-Key 缺失/重复/不匹配、未知字段、quoted schema 和 unsupported schema/type/source。每项同时校验 HTTP status 与安全 error code，输出不包含 token、Cookie 或原始 payload；整组 Kafka offset 不增长。

### 2.4 治理、文档与版本

- Phase 7 总实施方案权威分配 `Phase-07-03` → `1.4.3` / `develop/1.4.3`，补充本批范围、验证边界和 Phase 级关闭条件。
- 新增 Phase-07-03 拆分实施方案，并同步 Phase-07-01 方案/记录中的 PR #68、远程门禁和合入提交事实。
- 更新 Router README 的 non-blocking buffer、取消和立即拒绝语义。
- 根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 同步为 `1.4.3`。

## 3. 变更文件

- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `router/README.md`
- `router/internal/envelope/envelope.go`
- `router/internal/envelope/envelope_test.go`
- `router/internal/kafka/producer.go`
- `router/internal/kafka/producer_test.go`
- `scripts/verify-router.sh`
- `dev/imple/Phase-07/Phase-07-总实施方案.md`
- `dev/imple/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md`
- `dev/imple/Phase-07/Phase-07-03-Review整改与阶段再验收.md`
- `dev/logs/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md`
- `dev/logs/Phase-07/Phase-07-03-Review整改与阶段再验收.md`

## 4. 实际验证记录

以下命令在最终生产代码/脚本 diff 上执行并通过：

- `(cd router && test -z "$(gofmt -l .)")`
- `(cd router && go test -count=1 ./...)`
- `(cd router && go vet ./...)`
- `(cd router && go test -race -count=1 ./...)`
- `(cd monitor && test -z "$(gofmt -l .)")`
- `(cd monitor && go test -count=1 ./...)`
- `(cd monitor && go vet ./...)`
- `(cd monitor && go test -race -count=1 ./...)`
- `(cd exporters/redis && test -z "$(gofmt -l .)")`
- `(cd exporters/redis && go test -count=1 ./...)`
- `(cd backend && test -z "$(gofmt -l .)")`
- `(cd backend && go test -count=1 ./...)`
- `(cd frontend && npm test -- --run)`：9 个 test files、48 项测试通过。
- `(cd frontend && npm run build)`：typecheck 与 Vite production build 通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：25 项通过。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/1.4.3 --base-ref origin/main`：通过，唯一匹配 Phase-07-03。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh`：通过。
- 使用只包含 `env bash dirname readlink mktemp sha256sum awk python3 go setsid sleep rm` 的临时 PATH 执行 `scripts/verify-router.sh --self-test`：通过，明确输出不访问 Docker并拒绝 10 个不安全目标。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- `scripts/verify-monitor.sh --self-test`：通过。
- `scripts/verify-exporter.sh --self-test`：通过。
- `scripts/verify-business.sh --self-test`：通过。
- `scripts/verify-router.sh`：通过。隔离 project 为 `gopulse-router-626380fbba53`；direct record byte equality 为 true；18 类拒绝矩阵前后 offset 均为 `1`；真实 Monitor `success`、Redis 停止后的 `target_unavailable`、Kafka outage 下 health `200`/ready `503`/publish `503`、原 Router/Monitor PID 恢复和强归属清理均通过。完成后该 project 的 container 与 volume 查询均为空。
- `git diff --check`：通过。

## 5. 与方案的偏差

- 未执行 `scripts/verify-business.sh` 完整 Chromium/RabbitMQ/数据库验收。理由：本批未修改 Backend、Frontend 业务行为、业务 Compose 服务、数据库或 RabbitMQ；Backend 全量 unit、Frontend unit/build、业务安全 self-test 和使用同一 Redis/Monitor/Router/Kafka 基础设施的真实 Router 验收均通过，没有出现要求扩大到完整业务验收的具体回归证据。符合本批方案的比例化回归边界。
- 无 Docker self-test 使用临时 PATH 白名单而不是卸载/移动 Docker 二进制；实际 PATH 内不存在 `docker`，脚本返回 0。

## 6. 已知限制与后续事项

- Kafka 单节点 PLAINTEXT、1 partition / replication factor 1 仍仅用于本地开发与验收。
- HTTP 调用在确认前取消或超时时仍是“不确定写入”语义；record 可能最终写入，调用方不得把非 `202` 当作确定未写入，Phase 8 Consumer 仍需按 `message_id` 处理潜在重复。
- 本批无未完成的远程收口事项；Kafka 单节点和不确定写入等产品边界按上述后续事项交给 Phase 8。
