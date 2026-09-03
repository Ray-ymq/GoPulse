# Phase 5-02：集成验收与阶段收口实施记录

## 1. 执行基线

- 执行日期：2026-09-03。
- 权威远程：`origin`。
- 已执行 `git fetch --prune origin`；从最新 `origin/main` 的 `59fe5ce`（根版本 `1.2.1`）创建 `develop/1.2.2`。
- Phase-05-01 已由 PR #58 于 2026-09-03 合入 `main`；通过 GitHub API 实际核对其 head `31c0912` 的 Branch governance、Backend、Frontend、Redis Exporter、Scripts and Compose、Integration 与自动 PR/合并 job 均成功。
- 开始时 Docker 中存在日常 `gopulse` 和既有 `gopulse-phase0203-integration` project；`.run` 中只有历史二进制和 `dev-wsl-validation.sh`，没有应用进程记录。开始时另有未跟踪用户文件 `使用指南.md`，本批未读取、修改、暂存或提交该文件。
- 活动仓库位于 WSL Linux filesystem `/home/ray/GoPulse`，Docker client/server 均为 `29.7.2`，使用 `default` context 的单一 daemon。

## 2. 实际完成工作

### 2.1 Phase 0～5 集成验收

- 在隔离 Redis 7.2.5 与临时 Exporter 上执行成功、停止目标、错误认证、TCP 超时、同进程恢复、SIGTERM 与归属清理矩阵。
- 成功路径将 Exporter 的 hits、misses、DB keys 和 expires 样本与同一 Redis 的实时 `INFO` 对值，并验证全部固定 metric family、类型、有限标签、Prometheus 0.0.4 Content-Type 与 `up 1`。
- 故障路径验证 `/metrics=503`、唯一 `gopulse_redis_up 0`、`/health=200`、进程身份不变、日志 reason code 脱敏和有界响应；恢复路径未重启 Exporter。
- 完整执行 `scripts/verify-business.sh`，覆盖真实 Chromium 业务闭环、历史与增量搜索、Phase 2 十项可靠性矩阵、Redis/Elasticsearch/RabbitMQ 故障恢复，以及 Phase 4 Schema v1 结构化日志。
- 隔离 Exporter 和业务验收均使用随机 project、端口、volume 与临时目录；成功退出后资源已清理，既有 `gopulse-phase0203-integration` project 保持运行且未被修改。

### 2.2 日常生命周期阻断修复

日常 `dev.sh → verify.sh → down.sh` 首次执行实际暴露三个阻断问题，均按本批边界实施最小修复：

1. 旧 `.env` 未声明 `ELASTICSEARCH_PORT` 时，`dev.sh` 虽定义默认值但未把该键纳入 `ALL_CONFIG_KEYS`，启动在 Compose 前失败。现已纳入配置解析，使默认 `9200` 正常生效。
2. `verify.sh` 在同一条 `local` 声明中先引用尚未完成赋值的 `key` 和 `binary`，分别触发 `invalid indirect expansion` 与 `unbound variable`。现已拆分依赖赋值，不改变验证契约。
3. 同一旧 `.env` 下，`down.sh` 未为 Compose 插值提供 `ELASTICSEARCH_PORT` 默认值，导致应用进程停止后基础设施无法关闭。现只为该非敏感端口增加与 `dev.sh` 一致的 `9200` fallback。

- `scripts/ci/test_verify_business.py` 增加针对上述配置键、fallback 和 Bash 赋值顺序的回归断言。
- 最终重新执行日常生命周期：四项基础设施健康，Backend、Business Worker、Search Indexer、Redis Exporter 和 Frontend 均启动；`verify.sh` 的进程身份、Exporter 两端点、Backend health/readiness、受保护 API 和 Frontend 检查全部通过；`down.sh` 随后清理应用记录和 `gopulse` container/network，并保留命名 volume。

### 2.3 文档、版本与 Phase 6 交接

- 根 README 更新为 `1.2.2`，补充 Exporter 启动步骤、旧 `.env` 默认值兼容、固定验收入口和 Phase 5 收口说明。
- Exporter 两级 README 明确隔离 Exporter 验收、Phase 0～4 业务回归、日常生命周期，以及 Phase 6 对 executable、环境变量、HTTP 状态、Prometheus 0.0.4、PID 与信号边界的复用要求。
- 总实施方案将 Phase-05-01 标记为已合入，并将 Phase-05-02 保持为“本地验收完成，待远程门禁与合入”；未把尚未发生的 Phase-05-02 远程结果写成通过。
- 根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 同步更新为 `1.2.2`。
- Phase 6 可以周期 GET 固定 `/metrics`，以 HTTP `200/503` 判定 scrape 成败并解析 Prometheus 0.0.4；GoPulse metrics envelope、MetricsMonitor scheduler 和 Plugin Manager 所有权仍留在 Phase 6。

## 3. 实际变更文件

- `README.md`
- `VERSION`
- `dev/imple/Phase-05/Phase-05-总实施方案.md`
- `dev/logs/Phase-05/Phase-05-02-集成验收与阶段收口.md`
- `exporters/README.md`
- `exporters/redis/README.md`
- `frontend/package.json`
- `frontend/package-lock.json`
- `scripts/dev.sh`
- `scripts/verify.sh`
- `scripts/down.sh`
- `scripts/ci/test_verify_business.py`

## 4. 实际验证与结果

### 4.1 Redis Exporter 固定门禁

```bash
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
```

结果：全部通过。

- formatting 无输出；unit、vet 与 race 均成功。
- self-test 拒绝错误 token、project、路径、端口、label、container 和 PID record，未触碰 Docker 或无关进程。
- 最终隔离 project 为 `gopulse-exporter-7cb6fdd80045`；实时 `INFO` 对值、固定指标、停止目标、认证失败、超时、同进程恢复、SIGTERM、端口释放和资源清理全部通过。

### 4.2 Backend、Frontend、脚本与治理

```bash
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.2.2 --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
git diff --check
```

结果：全部通过。

- Backend 全 module 测试与 vet 通过。
- Frontend 9 个测试文件、46 项测试通过；typecheck 与 Vite production build 通过。
- Python CI 24 项测试通过。
- 版本元数据一致为 `1.2.2`；分支治理确认 `develop/1.2.2` 与版本分配一致。
- 本仓库权威远程名为 `origin`，因此计划中的 `upstream/main` 实际使用等价的 `origin/main`。
- Bash syntax、Compose config 和 whitespace 检查通过。

### 4.3 Phase 0～4 必要业务回归

```bash
scripts/verify-business.sh
```

结果：通过。

- 最终隔离 project 为 `gopulse-acceptance-1dc39dc48d12`。
- Chromium 主流程 2 项通过、2 项按场景设计跳过；独立 `search-rebuild` 与 `search-live` 浏览器场景分别通过。
- Phase 2 十项可靠性矩阵全部通过，包括 Worker 停止/恢复、broker outage、Backend/Worker restart、重复事件、临时/永久失败和 RabbitMQ container restart。
- Phase 4 日志验证通过：实际观察到 backend 273 条、worker 27 条、indexer 38 条、reindex 8 条符合约束的记录。
- 验收 project、container、network、volume、进程和临时目录已清理。

### 4.4 日常共享生命周期

```bash
scripts/dev.sh
scripts/verify.sh
# 向前台 dev.sh 发送 Ctrl+C 后：
scripts/down.sh
```

结果：最终重跑通过。

- `dev.sh` 使用未补写 Phase 3/5 可选键的既有 `.env` 成功解析默认值，启动 MySQL、Redis、RabbitMQ、Elasticsearch、Backend、Business Worker、Search Indexer、Redis Exporter 与 Frontend。
- `verify.sh` 全部 13 项运行状态、进程身份与 HTTP 契约检查通过。
- `down.sh` 返回 0；没有遗留应用 PID record、监听端口或 `gopulse` container/network，`gopulse_*` 命名 volume 保留。
- 既有 `gopulse-phase0203-integration` 的 MySQL、Redis 与 RabbitMQ container 在验收后仍保持健康运行。

## 5. 与方案的偏差

- 计划中的分支校验示例使用 `upstream/main`，实际仓库只有权威远程 `origin`，因此使用 `origin/main`；校验语义不变。
- 本批原计划不修改产品功能；实际只修改了三个 Bash 生命周期阻断点及对应回归断言。修复来自固定日常验收的真实失败，没有新增 Exporter 指标、配置、HTTP 接口或 Phase 6 功能。
- `gh` CLI 在环境中不可用；Phase-05-01 的合入与远程 job 状态通过 GitHub REST API 核对。Phase-05-02 的 PR、远程 checks 和合入尚未发生，因此未记录为完成。

## 6. 已知限制与后续项

- Exporter 仍只支持一个静态 Redis target，不提供 TLS、HTTP 鉴权、多目标、主动推送、后台采集、缓存或历史数据。
- Plugin Manager、MetricsMonitor scheduler、GoPulse metrics envelope、Kafka 与 VictoriaMetrics 均未提前实现。
- Phase 5 已完成；后续只按 Phase 6 计划接入 Plugin Manager 与 MetricsMonitor，不在 `1.2.2` 上继续扩展 Exporter 能力。

## 7. 远程门禁跟进

- 本批提交 `0e3ec19` 推送至 `develop/1.2.2` 后触发 Auto PR and Merge workflow `33722403547`。
- 实际观察到 Quality gates before PR / Branch governance、Backend、Frontend、Redis Exporter、Scripts and Compose、Integration 全部成功；Open PR and enable auto-merge job 也成功。
- 自动创建的 PR #60 于 2026-09-03 06:20:56 UTC 以 squash 方式合入 `main`，主远程提交为 `b85afa8`；远程开发分支随后按规则删除。
- 已重新 fetch 并核对 `origin/main` 的 `VERSION=1.2.2`，因此 Phase-05-02 与 Phase 5 的远程收口条件均已满足。

## 8. 完成结论

Phase-05-02 的同构建 Redis Exporter 矩阵、Phase 0～4 必要业务回归、日常 Bash 生命周期、资源归属与清理、文档、Phase 6 交接和 `1.2.2` 版本元数据均已完成。固定本地门禁全部通过，实际暴露的三个生命周期阻断问题已最小修复并在最终 diff 上重验通过；远程 workflow `33722403547` 的全部配置门禁成功，PR #60 已合入主远程 `main`。Phase 5 验收与阶段收口完成，按计划停止扩展并进入 Phase 6 交接。
