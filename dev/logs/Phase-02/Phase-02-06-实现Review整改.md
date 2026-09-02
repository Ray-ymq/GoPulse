# Phase-02-06：实现 Review 整改开发记录

## 1. 执行基线

- 执行日期：2026-09-02。
- 权威开发分支：`develop/0.3.6`。
- 目标版本：`0.3.6`。
- 起始提交：`b901c96`；开始前已 fetch `origin`，当时 `origin/main` 为 `a7f4a64`。两者均以 Phase 2 完成提交 `efff938` 为父提交，工作树内容一致；按用户明确要求以 `develop/0.3.6` 为权威分支继续整改。
- 执行环境：WSL2/Linux 文件系统中的 `/home/ray/GoPulse`，使用 Bash 与单一 Docker 环境。
- 冻结的 `scripts/*.ps1` 未修改。

## 2. 实际完成内容

### 2.1 Published Outbox 有界清理

- 将 Repository 已有的 `CleanupPublished` 能力纳入 Dispatcher 的生产 `Store` 边界和运行生命周期。
- Dispatcher 启动时立即执行一次清理，之后按 `OUTBOX_CLEANUP_INTERVAL` 周期运行；主投递循环结束时会取消并等待清理 goroutine。
- 每次清理固定一次 cutoff，按 `OUTBOX_CLEANUP_BATCH` 循环删除超过 `OUTBOX_PUBLISHED_RETENTION` 的 `published` 行；满批后退让 100ms，pending/leased 行不符合 Repository 删除条件。
- 新增默认值和边界：清理周期 `1h`（`1m..24h`）、保留期 `168h`（`1h..8760h`）、batch `500`（`1..1000`）；同步更新生产入口、`.env.example` 和 Bash 配置传播。
- 清理错误只输出收敛日志，不停止正常 Outbox 投递。

### 2.2 整批租约预算

- 在环境配置加载与 `NewDispatcher` 构造边界统一要求 `claim batch × publish timeout + 1s <= lease duration`。
- 默认租约由 `30s` 调整为 `1m`，满足默认 batch 10、单条 publish timeout 5 秒的最坏串行预算。
- 隔离业务验收继续使用 `3s` 租约和 `2s` timeout，并将验收 claim batch 调整为 1，保持其快速故障注入节奏且满足同一约束。

### 2.3 Worker cancellation 与 goroutine ownership

- 每个在途 handler 使用独立、可取消的 processing context。
- 正常 shutdown 先停止新投递，并在 `BUSINESS_WORKER_SHUTDOWN_TIMEOUT` 内允许当前处理完成；超时后取消 context，等待 handler 完成 Nack/requeue 和退出后才让 runtime 返回。
- RabbitMQ connection/channel 中断同样取消并 join 当前 handler。
- Processor 接口补充必须及时响应 context cancellation 的契约说明；新增单元测试，并加强已有 integration processor 的停止断言。

### 2.4 文档、治理与版本

- Phase 2 总方案补充 `Phase-02-06` → `0.3.6` → `develop/0.3.6` 权威分配，记录 PR #39 已合入和远程门禁已通过，并在本批固定门禁通过后标记为本地完成。
- README 更新为 `0.3.6`，说明 cleanup、整批租约预算和 Worker shutdown 语义。
- Review 报告追加整改执行结果，保留原始 finding 和 Review 基线作为历史事实。
- 自动 PR workflow 在创建 PR 前执行相对 `main` 的 tree diff；无文件变化时输出 summary 并跳过 PR 创建和自动合并。新增静态治理测试防止 guard 被移除。
- 根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 统一更新为 `0.3.6`。

## 3. 变更文件

- `.env.example`
- `.github/workflows/auto-pr-merge.yml`
- `README.md`
- `VERSION`
- `backend/cmd/server/main.go`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/internal/outbox/dispatcher.go`
- `backend/internal/outbox/dispatcher_test.go`
- `backend/internal/worker/handler.go`
- `backend/internal/worker/integration_test.go`
- `backend/internal/worker/runtime.go`
- `backend/internal/worker/runtime_test.go`
- `frontend/package.json`
- `frontend/package-lock.json`
- `scripts/dev.sh`
- `scripts/verify-business.sh`
- `scripts/ci/test_auto_pr_workflow.py`
- `dev/imple/Phase-02/Phase-02-总实施方案.md`
- `dev/imple/Phase-02/Phase-02-06-实现Review整改.md`
- `dev/logs/Phase-02/Phase-02-06-实现Review整改.md`
- `dev/review/2026-09-02-Phase-2实现Review报告.md`

## 4. 实际验证

### 4.1 实现期间定向检查

- `go test -count=1 ./internal/config ./internal/outbox ./internal/worker ./cmd/server`（`backend`）：通过。
- `go test -count=1 ./internal/worker`（`backend`）：通过。
- `python3 scripts/ci/test_auto_pr_workflow.py`：通过，1 项测试。
- `bash -n scripts/dev.sh scripts/verify-business.sh`：通过。

### 4.2 最终固定完成门禁

- `test -z "$(gofmt -l .)"`（`backend`）：通过。
- `go test -count=1 ./...`（`backend`）：通过。
- `go vet ./...`（`backend`）：通过。
- `go test -race -count=1 ./...`（`backend`）：通过，未发现数据竞争。
- `npm test -- --run`（`frontend`）：通过，8 个测试文件、42 项测试。
- `npm run build`（`frontend`）：通过，包含 `vue-tsc --noEmit` 和 Vite production build。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，18 项治理测试。
- `python3 scripts/ci/validate_versions.py`：通过，根版本和 Frontend npm 元数据均为 `0.3.6`。
- `python3 scripts/ci/validate_branch.py --branch develop/0.3.6 --base-ref origin/main`：通过。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh`：通过。
- `bash scripts/verify-business.sh --self-test`：通过；接受 1 个合法目标并拒绝 6 个不安全目标，未访问 Docker。
- `docker compose --env-file .env.example --file deploy/compose.yaml config`：通过；渲染结果恰有 4 个 `host_ip: 127.0.0.1`。
- `git diff --check`：通过。

## 5. 与方案的偏差

- 没有重写 P3-01 指出的既有 no-op 历史提交；按方案在自动 PR workflow 增加预防性 guard。
- 没有新建通用定时任务进程；published cleanup 直接归属于现有 Dispatcher 生命周期，减少新的部署单元。
- 没有重复执行完整 Chromium E2E 和十项可靠性故障矩阵。本批未改变用户通知链路、RabbitMQ 拓扑或完整验收场景，且定向与固定门禁未暴露跨组件回归，符合方案明确的停止条件。
- 未修改 Phase-02-05 实施记录中的事后状态；其内容继续反映该批执行当时尚待 PR 合入的历史事实。

## 6. 已知限制与后续项

- Worker runtime 的有界回收依赖 `Processor` 遵守 context cancellation 契约；当前通知 Processor 使用可取消的数据库调用，后续新 Processor 也必须满足该接口约束。
- Outbox 与 RabbitMQ 仍保持至少一次语义，不声明 exactly-once；清理只控制已发布历史行的保留，不改变重复投递收敛边界。
- 当前仍是单节点本地 RabbitMQ 和无规模压测结论；生产集群 HA、网络分区和容量规划不属于本整改批次。
- 本地整改和固定门禁已完成；远程 PR、合并及其远程质量门禁尚未执行，不能预先记录为通过。
