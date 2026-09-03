# Phase 6-03：MetricsMonitor 周期采集与标准消息闭环实施记录

> 实施日期：2026-09-03
>
> 实施分支：`develop/1.3.3`
>
> 目标版本：`1.3.3`

## 1. 实际完成工作

- 在 Monitor 中新增固定目标 `redis-exporter-local` 的 MetricsMonitor。已安装并处于 running 状态的 `redis-exporter` 会在安装、启动、更新和重启恢复后立即采集，并按配置周期继续采集。
- Plugin Manager 与 MetricsMonitor 通过内部生命周期接口协作。stop、update 和 shutdown 会先取消并等待当前采集退出；MetricsMonitor 不读取 Registry、不操作 PID，也不修改 desired state。
- 新增默认 `15s` 采集周期、`3s` 采集超时和 `3s` 发布超时配置，并校验采集超时严格小于周期。采集地址只允许由受信 Exporter 配置形成的回环 HTTP 地址。
- 使用 Prometheus 官方 text parser 解析响应，限制响应为 1 MiB，并校验 Phase 5 固定 family、类型、标签、有限数值、counter 非负、样本唯一性、文本时间戳和数量上限。
- 实现 `200/up=1` 的完整 `success` 语义和严格 `503/up=0` 的 `target_unavailable` 语义；网络、超时、内容类型、超限、解析和契约错误不生成消息，也不缓存或重放旧 payload。
- 新增 Envelope v1：安全随机 32 位小写十六进制 message ID、Monitor UTC 时间、固定 metrics/redis 类型来源、插件与目标元数据，以及稳定排序的结构化 samples。
- 新增 Publisher 接口。Router URL 为空时使用无历史内存丢弃实现；配置 Router 时使用专用有界 HTTP client，发送 Bearer token、`Idempotency-Key` 和 JSON，并仅接受 `202 Accepted`，不重试且不落盘。
- 将最近采集时间、最近成功时间和有限安全错误写回插件公共状态；Backend 现有代理 DTO 已包含这些字段，因此无需修改 Backend 契约或增加指标正文接口。
- 扩展 `scripts/verify-monitor.sh`：加入严格 HTTP 捕获端、真实 Redis key 数变化、立即/周期消息、stop 后无延迟消息、畸形 Exporter、Redis 停止/恢复、Publisher 停止/恢复、更新回滚和 Monitor 重启恢复证据。
- 更新运行配置示例、Bash 启动配置透传、Monitor/根 README，以及根和 Frontend 版本元数据到 `1.3.3`。

## 2. 实际变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `monitor/README.md`
- `monitor/cmd/monitor/main.go`
- `monitor/go.mod`
- `monitor/go.sum`
- `monitor/internal/config/config.go`
- `monitor/internal/config/config_test.go`
- `monitor/internal/metrics/collector/collector.go`
- `monitor/internal/metrics/collector/collector_test.go`
- `monitor/internal/metrics/envelope/envelope.go`
- `monitor/internal/metrics/publisher/publisher.go`
- `monitor/internal/metrics/publisher/publisher_test.go`
- `monitor/internal/plugin/manager.go`
- `monitor/internal/plugin/types.go`
- `scripts/dev.sh`
- `scripts/verify-monitor.sh`
- `dev/logs/Phase-06/Phase-06-03-MetricsMonitor周期采集与标准消息闭环.md`

## 3. 验证命令与结果

以下命令均在最终生产代码与脚本 diff 上实际执行并通过：

- `(cd monitor && test -z "$(gofmt -l .)")`
- `(cd monitor && go test -count=1 ./...)`
- `(cd monitor && go vet ./...)`
- `(cd monitor && go test -race -count=1 ./...)`
- `(cd backend && go test -count=1 ./...)`
- `(cd backend && go vet ./...)`
- `(cd frontend && npm test -- --run)`：9 个测试文件、48 个测试通过。
- `(cd frontend && npm run build)`
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/package-redis-exporter.sh`
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- `scripts/verify-monitor.sh --self-test`
- `scripts/verify-monitor.sh`：真实 Redis、真实 Exporter、Envelope 捕获、目标不可用/恢复、畸形数据拒绝、Publisher 故障恢复和插件生命周期矩阵通过。
- `scripts/verify-exporter.sh`：真实 Redis INFO 数值、目标停止、认证失败、超时、恢复、SIGTERM 和资源清理通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：24 个测试通过。
- `python3 scripts/ci/validate_versions.py`
- `python3 scripts/ci/validate_branch.py --branch develop/1.3.3 --base-ref upstream/main`
- `git diff --check`

远程 GitHub Actions 门禁未在本地执行；需在推送分支或创建 PR 后由远程工作流确认。

## 4. 相对方案偏差

- 调度器采用每个 target 单一串行 goroutine 执行“立即采集 + ticker 周期采集”，因此在途采集期间不会创建第二个采集 goroutine；ticker 的积压由 Go ticker 的有界通道自然合并。该实现满足单 target 最多一个在途采集和无无界队列要求，但没有额外维护独立的 `scrape_in_progress` 状态字段。
- `.github/workflows/quality-gates.yml` 未修改，因为现有 Monitor job 已固定执行格式、单元测试、vet、race 和 `scripts/verify-monitor.sh`，现有 scripts-and-compose job 也已执行 Bash 语法、self-test 和 Compose 校验。
- Backend 未修改，因为 Phase-06-02 的安全状态 DTO 已包含 `last_scrape_at`、`last_success_at` 和 `last_error`，本批只需在 Monitor 侧填充这些字段。
- 按实施方案验证边界，本批未重复执行完整 `scripts/verify-business.sh`；该跨阶段业务回归保留给 Phase-06-04。

## 5. 已知限制与跟进项

- Phase 6 仍只有固定的本地 Redis Exporter target，不提供动态目标 CRUD、服务发现或多插件采集。
- HTTP Publisher 不重试、不持久化、不提供可靠消息队列；正式 Router/Kafka、Marshaller 和 VictoriaMetrics 转换属于后续 Phase。
- 最近采集状态仅保存在 Monitor 内存中，Envelope 正文不写入 Registry 或状态 API。
- 远程门禁结果需在分支推送后补充确认。
