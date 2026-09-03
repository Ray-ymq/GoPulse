# Phase 6-04：集成验收与阶段收口实施记录

> 实施日期：2026-09-03
>
> 实施分支：`develop/1.3.4`
>
> 目标版本：`1.3.4`
>
> 整改依据：`dev/review/2026-09-03-Phase-6实现Review报告.md`

## 1. 实际完成工作

- 关闭 P1-01：为 `scripts/verify-business.sh` 的隔离环境文件、迁移/运维命令环境和 Backend 启动环境补齐随机隔离的 `MONITOR_API_TOKEN`、明确 `MONITOR_URL` 与权威 `30s` 请求超时。完整 Phase 0～5 业务、可靠性、搜索和日志回归已执行到结束。
- 关闭 P1-02：重构 Plugin Manager 更新回滚事务。更新前保存 Registry 快照；新 Registry 持久化失败或新版本启动失败时统一恢复旧 `current`、磁盘 Registry 和内存 Registry；旧版本无法重启时公共状态进入 `failed/rollback_failed`，不再虚报 `running`，也不保留不存在的 runtime。新增 Registry 写入失败和新旧版本双重启动失败定向测试。
- 关闭 P1-03：在创建任何插件目录前拒绝 `/`、当前用户 home、仓库根和含 symlink 的路径；记录插件根 device/inode，在每次变更操作和关键阻塞点后重新校验。Registry、`.staging`、插件目录、releases、runtime、process record 与 release 节点均校验预期文件类型；清理动作在边界校验失败时拒绝执行。新增危险根、root/internal symlink 和运行中 root 替换负向测试。
- 关闭 P2-01：MetricsMonitor 在 generation 真正结束前保留 cancel/done join 句柄，调用方取消后后续 Disable 仍会等待同一 generation。Plugin Manager 的 stop/update 使用自身有界 context 停止采集，不再直接受 HTTP 请求取消影响。新增延迟退出 Publisher 的定向并发测试。
- 关闭 P2-02：`scripts/dev.sh` 在启动 Monitor 前读取当前包 Manifest 和持久化 Registry，按版本执行 install/update；同版本 digest 漂移、版本倒退或损坏 Registry 使用安全重建策略，并在检测到仍存活的归属 Exporter 时拒绝删除。`scripts/verify.sh` 通过 Monitor 公共状态确认运行版本与根 `VERSION` 一致。隔离日常生命周期验证了 `1.3.3 → 1.3.4` 自动更新和最终 `1.3.4` 运行事实。
- 关闭 P2-03：Backend、Monitor、`.env.example` 与 `dev.sh` 统一 `MONITOR_REQUEST_TIMEOUT=30s`、允许 `1s..60s`；插件 start/stop 为 `1s..30s`；scrape interval 为 `1s..5m`；scrape timeout 为 `100ms..30s` 且小于 interval。新增上下界配置测试。
- 将根与 Frontend 版本元数据更新为 `1.3.4`，更新根 README 和 Phase 6 总方案状态表。

## 2. 实际变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `monitor/internal/config/config.go`
- `monitor/internal/config/config_test.go`
- `monitor/internal/metrics/collector/collector.go`
- `monitor/internal/metrics/collector/collector_test.go`
- `monitor/internal/plugin/manager.go`
- `monitor/internal/plugin/manager_test.go`
- `monitor/internal/plugin/storage.go`
- `scripts/dev.sh`
- `scripts/verify.sh`
- `scripts/verify-business.sh`
- `dev/imple/Phase-06/Phase-06-总实施方案.md`
- `dev/logs/Phase-06/Phase-06-04-集成验收与阶段收口.md`

工作区原有未跟踪文件 `使用指南.md` 未读取、未修改、未暂存，也未纳入验收资源清理。

## 3. 验证命令与结果

以下语言、脚本、版本和 Compose 门禁在最终代码/配置 diff 上实际执行并通过：

- `(cd monitor && test -z "$(gofmt -l .)")`
- `(cd monitor && go test -count=1 ./...)`
- `(cd monitor && go vet ./...)`
- `(cd monitor && go test -race -count=1 ./...)`
- `(cd backend && test -z "$(gofmt -l .)")`
- `(cd backend && go test -count=1 ./...)`
- `(cd backend && go vet ./...)`
- `(cd backend && go test -race -count=1 ./...)`
- `(cd exporters/redis && test -z "$(gofmt -l .)")`
- `(cd exporters/redis && go test -count=1 ./...)`
- `(cd exporters/redis && go vet ./...)`
- `(cd exporters/redis && go test -race -count=1 ./...)`
- `(cd frontend && npm test -- --run)`：9 个测试文件、48 个测试通过。
- `(cd frontend && npm run build)`
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：24 个测试通过。
- `python3 scripts/ci/validate_versions.py`
- `python3 scripts/ci/validate_branch.py --branch develop/1.3.4 --base-ref origin/main`
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/package-redis-exporter.sh`
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- `git diff --check`

以下固定自检与真实隔离验收实际执行并通过：

- `scripts/verify-monitor.sh --self-test`
- `scripts/verify-monitor.sh`：最终 Plugin Manager/Storage diff 上真实完成管理员链路、安装/启停/更新、失败回滚、Monitor 重启、真实 Redis 数值、目标停止/恢复、畸形指标拒绝和 Publisher 恢复。
- `scripts/verify-exporter.sh --self-test`
- `scripts/verify-exporter.sh`：真实 Redis INFO、目标停止、认证失败、超时、恢复、SIGTERM 和归属清理通过。
- `scripts/verify-business.sh --self-test`
- `scripts/verify-business.sh`：数据库迁移、真实 Chromium、通知可靠性 10 项矩阵、搜索重建/增量收敛和 Schema v1 日志验证完整通过；隔离 container、network、volume 和进程随后清理。

日常生命周期在 `/tmp` 下的隔离仓库副本执行，以随机端口和唯一 Compose project 避免触碰当前工作区既有资源：

- 首轮以 `VERSION=1.3.3` 执行 `scripts/dev.sh → scripts/verify.sh`，所有 Compose、进程归属、Monitor、Redis Exporter、Backend readiness 和 Frontend 检查通过。
- 保留隔离插件根，将副本 `VERSION` 改为 `1.3.4` 后再次执行 `scripts/dev.sh → scripts/verify.sh`；Registry、`current` 和 Monitor 公共状态均为运行中的 `1.3.4`，证明日常路径实际调用 update 而非继续复用旧二进制。
- 最终 Storage 安全调整后再次从干净隔离副本执行 `scripts/dev.sh → scripts/verify.sh → Ctrl+C → scripts/down.sh`，运行版本为 `1.3.4`，全部检查通过。
- 两次隔离 project 的 container、network、volume、临时副本和应用进程均已清理；原工作区 PID `814165` 的用户既有 Redis Exporter 全程保持运行。

## 4. 相对方案偏差与实际失败

- 原工作区存在用户拥有且已记录的 Redis Exporter 进程，因此没有直接在原 `.run` 上执行破坏性的日常生命周期。改用完整隔离仓库副本、随机端口和唯一 Compose project 验证同一 Bash 逻辑，并在结束后确认原 PID 仍存活。
- 第一次搭建隔离日常生命周期时，临时 `.env` 只替换了 `ELASTICSEARCH_PORT`/`RABBITMQ_PORT`，未同步临时 `ELASTICSEARCH_URL`/`RABBITMQ_URL`，导致 search reindex 或 readiness 失败。修正仅用于 `/tmp` 验收副本的 URL 后重新从启动路径执行并通过；该失败不是项目代码回归，相关失败 project 已清理。
- P1-01 采用 Review 建议中的最小关闭路径：为保留的完整业务验收入口补齐安全 Monitor 配置；本批未进一步拆分 migration/worker/indexer 配置加载器，因为固定门禁已恢复且没有观察到其他命令被 Monitor 配置阻断。
- 文件系统边界采用规范化路径、危险根拒绝、symlink/类型检查和 device/inode 身份复核；没有引入 Linux `openat` 目录 fd 重写。现有变更操作在开始和关键阻塞点后重新验证，清理在验证失败时拒绝执行，定向替换测试未发生允许根外写入或删除。

## 5. 已知限制与跟进项

- 远程 GitHub Actions 结果需在本分支推送后由远程工作流确认；本记录在提交前不预写未观察到的远程成功结果。
- Phase 6 仍只管理固定 Redis Exporter 和单一 target；多插件、多 target、签名包、远程市场和容器化插件不在本批范围。
- HTTP Publisher 仍按 Phase 6 契约不重试、不持久化；正式 Router/Kafka 可靠链路属于 Phase 7。
- Phase 6 只有在 `develop/1.3.4` 远程门禁成功并合入主远程 `main` 后才按总方案标记为阶段完成。
