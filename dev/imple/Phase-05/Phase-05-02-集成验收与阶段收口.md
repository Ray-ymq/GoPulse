# Phase 5-02：集成验收与阶段收口实施方案

> 执行序号：2 / 2
>
> 前置批次：Phase-05-01 已完成并通过验收
>
> 总方案来源：[Phase-05-总实施方案.md](Phase-05-总实施方案.md)

## 1. 批次目标

在同一最终构建上完成 Phase 0～5 必要回归、Redis Exporter 成功/故障/恢复矩阵、日常生命周期与隔离资源安全验收，形成 Phase 5 的阶段收口证据，并向 Phase 6 交付稳定的拉取和进程管理边界。

本批是固定的集成验收与阶段收口批次，不增加指标、配置、接口或插件功能。只允许对实际复现且直接阻断总方案验收的问题实施最小修复；Phase 5 完成版本固定为 `1.2.2`。

## 2. 前置条件

- Phase-05-01 已通过 `develop/1.2.1` 完成并合入主远程最新 `main`，根版本为 `1.2.1`，其实施记录与远程门禁证据齐全。
- Phase 4 及以前阶段保持完成状态，Phase 4 结构化日志、`scripts/verify-business.sh` 和其他固定验收入口可用。
- 已 fetch 主远程并从包含 Phase-05-01 的最新 `main` 创建 `develop/1.2.2`，未沿用前一批分支。
- WSL2 Linux filesystem 具备隔离 Redis、Exporter 和现有全栈验收所需资源；使用一个明确的 Docker daemon。
- 开始前保存日常 Compose project、volume、端口、`.run` 进程和 Git 状态；所有破坏性验收资源均带随机且可验证的归属标识。

## 3. 实施范围

### 3.1 封闭集成验收

- 在同一最终构建执行总方案第 11.3 节十项矩阵一次，覆盖真实数值、Prometheus 契约、被动采集、Redis 停止、认证失败、恢复、信号关闭、日常生命周期和隔离清理。
- 正常路径以 `/metrics` 的值与同一 Redis `INFO` 对照为最终证据；不得只检查端口、进程、HTTP `200` 或静态 metric name。
- 故障路径同时断言 `/metrics=503`、唯一 `up 0`、`/health=200`、Exporter PID/启动身份不变和响应时间上限。
- 恢复路径必须复用同一 Exporter 进程；重启 Exporter、注入静态值或人工清除状态不能替代自动恢复证明。
- 日常 `dev.sh → verify.sh → down.sh` 与隔离 `verify-exporter.sh` 分别验证，确认两者不会共享或误删进程记录、端口、project、network 或 volume。
- 执行 Phase 0～4 必要业务验收，证明 Exporter 纳入共享脚本后不破坏 Backend、Frontend、Worker、Search Indexer、Redis 缓存和 Phase 4 日志能力。

### 3.2 验收失败的最小修复

- 只修复固定矩阵中已复现、会使 Phase 5 验收不成立的问题；修复前保留复现命令和有限诊断。
- 修复后只重跑受影响的 unit/integration/脚本场景，再在最终 diff 上执行固定完成门禁一次。
- 新指标、新目标、新 HTTP 接口、性能优化、通用插件抽象或 Phase 6 管理能力不属于“最小修复”。
- 如失败来自 Phase 4 已发布公共契约与本方案矛盾，先更新总方案并明确兼容决策，不静默修改上游契约。

### 3.3 文档、版本与远程状态收口

- 更新根 README、Exporter README 与总方案状态，使实际命令、配置、端点、指标、限制和故障语义一致。
- 核对总方案、两份拆分方案、两份实施记录、Git 历史、版本和权威分支分配；不得把计划命令写成已通过。
- 将根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 更新为 `1.2.2`。
- 本地门禁通过时只记录本地结果；只有 Pull Request 合入和远程门禁实际成功后，才把总方案标记为 Phase 5 完成。

### 3.4 Phase 6 交接确认

- 确认 Phase 6 可以把 Exporter executable、环境变量和 `/health` 用作安装/启动/停止/状态查询的基础，而无需修改 Phase 5 指标契约。
- 确认 MetricsMonitor 可按固定 URL 周期 GET `/metrics`，以 HTTP 状态识别 scrape 成败并解析 Prometheus 0.0.4；Phase 6 再封装 GoPulse 标准 metrics 消息。
- 明确 `scripts/dev.sh` 在 Phase 5 对 Exporter 的直接所有权是过渡态；Phase 6 Plugin Manager 接管后应避免双重启动，但仍复用 PID、信号和端点边界。
- 不在本批预先创建 Plugin Manager manifest、Backend API、Monitor scheduler 或标准 Envelope。

## 4. 实施边界与非目标

- 不新增、删除或重命名总方案第 7 节的指标，不改变 Prometheus 0.0.4、HTTP `200/503` 或 health 语义。
- 不新增配置、CLI、`/ready`、metrics query 参数、鉴权、TLS 或远程暴露能力。
- 不实现 MySQL Exporter、多 Redis target、后台采集、主动推送、缓存、聚合或持久化。
- 不实现 Monitor、Plugin Manager、Backend/Frontend 管理、Kafka、Marshaller 或 VictoriaMetrics。
- 不进行高并发/长时间压测、Redis 多版本矩阵、网络故障全排列或生产容量规划。
- 不建立应用容器镜像，不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。
- 不因剩余时间执行一般性代码审查、依赖审计、覆盖率扩充或机会性重构。

## 5. 预计文件与交付物

```text
dev/imple/Phase-05/Phase-05-总实施方案.md
dev/logs/Phase-05/Phase-05-02-集成验收与阶段收口.md
README.md
exporters/README.md
exporters/redis/README.md
scripts/verify-exporter.sh（仅验收编排或阻断修复）
scripts/dev.sh（仅阻断修复）
scripts/down.sh（仅阻断修复）
scripts/verify.sh（仅阻断修复）
exporters/redis/**（仅阻断修复）
.github/workflows/**（仅远程门禁阻断修复）
scripts/ci/**（仅治理阻断修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义修改。如果固定验收没有暴露产品问题，本批应以验收证据、文档、版本和实施记录收口。

## 6. 详细实施步骤

1. 核对 Phase-05-01 实施记录、合入提交、远程门禁、已知限制和日常资源快照。
2. 在最终构建上执行 Redis Exporter formatting、unit、vet、race 与真实 integration，确认指标和失败契约没有漂移。
3. 执行 `scripts/verify-exporter.sh --self-test`，确认错误 token、名称、端口、label、container、PID record 和路径均被拒绝。
4. 执行真实隔离 Exporter 矩阵：启动、写入代表性数据、对照 `INFO`、被动采集、停止 Redis、错误认证、恢复、SIGTERM 与中断清理。
5. 执行日常 `dev.sh → verify.sh → down.sh`，确认 Exporter 与 Backend、两个 Worker、Frontend 和四项基础设施共同运行且进程归属正确。
6. 执行 Phase 0～4 固定业务验收一次，重点确认共享脚本、Redis 故障恢复和 Phase 4 结构化日志无回归。
7. 对失败执行最小修复和定向重验，不扩大为新指标、插件抽象或一般性清理。
8. 最终 diff 稳定后执行第 8 节固定完成门禁一次，保存实际输出摘要。
9. 更新 README、总方案状态、本批实施记录和 `1.2.2` 版本元数据，核对所有文档与实际行为一致。
10. 提交并创建 Pull Request；查询并记录实际远程检查和合入状态，未合入或失败时保持 Phase 5 未完成。
11. 合入与远程门禁通过后停止 Phase 5 扩展，将稳定端点、进程和失败契约交给 Phase 6。

## 7. 风险与控制

- **静态指标假通过**：执行真实命令和 key 操作，再把 Exporter sample 与同一 Redis `INFO` 对值。
- **故障只验证 HTTP**：同时验证 PID/start ticks、health、metrics、超时、日志脱敏和恢复后的同进程身份。
- **恢复依赖重启**：固定记录 Exporter PID，恢复前后必须一致；重启即该场景失败。
- **后台采集难以证明**：由 handler/collector spy 单元测试固定零调用，并确认运行架构不存在 ticker；不以不可靠的命令数时间差单独判定。
- **验收误伤日常资源**：随机 project/port/path/volume 加 label、container ID、PID 与前后快照多重验证。
- **共享脚本引入业务回归**：在同一最终 diff 上执行日常生命周期和 Phase 0～4 固定验收，不重复无关故障排列。
- **收口批次范围膨胀**：只处理阻断验收的复现问题，其他改进进入后续事项。
- **虚构远程完成**：本地、Pull Request、远程 checks 和合入状态分别记录，未观察到的结果保持未完成。

## 8. 固定完成门禁

最终 diff 上每项执行一次：

```bash
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.2.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
scripts/verify-business.sh
git diff --check
```

`scripts/verify-exporter.sh` 是 Phase 5 的封闭真实基础设施入口；`scripts/verify-business.sh` 是共享 Bash 生命周期和 Phase 0～4 的必要回归。若 Phase 4 最终采用额外的独立固定验收入口，也必须按其已合入总方案执行一次；Phase 4 前置条件不完整时不得开始本批。

完整验收只在 WSL2 Linux filesystem 和可确认归属的隔离资源执行。环境缺失时不得标记完成，也不得以 mock、fixture、源码阅读或新增单元测试替代真实 Redis、故障和恢复证据。

远程 Pull Request 必须通过仓库实际配置的 Branch governance、Backend、Frontend、Redis Exporter、Scripts and Compose、Integration 与自动 PR/合并相关门禁；只能记录实际观察到的检查名称和结果。

## 9. 验收标准

- Phase-05-01 的独立 Redis Exporter、HTTP/Prometheus 契约、Bash 生命周期和 CI 门禁可以共同运行。
- 真实 Redis 指标与 `INFO` 对值一致；全部 metric family、类型、标签与成功 `up 1` 符合总方案。
- `/health` 不采集目标；Exporter 空闲时不轮询、不推送，且不保存历史或上次成功 snapshot。
- Redis 停止、错误认证和采集超时均在上限内返回 `503`/唯一 `up 0`，Exporter 保持同一进程且 health 为 `200`。
- Redis 恢复后同一 Exporter 进程无需管理操作即恢复 `200` 与当前指标，没有陈旧数据。
- `SIGINT`/`SIGTERM` 有界退出；日常与隔离脚本不会误杀、误删或遗留进程、端口、container、network、volume 和临时文件。
- 结构化日志与 HTTP/metrics 输出不泄漏凭据、完整地址、业务 key、原始 `INFO` 或原始目标错误。
- Phase 0～4 必要业务、缓存、搜索、异步处理、结构化日志与 Phase 5 Exporter 同时运行且无回归。
- PowerShell 仍为 `0.2.1` 历史基线；Monitor、Plugin Manager、Kafka、VictoriaMetrics 和应用容器化未提前实现。
- 两份实施记录、总方案、Git 历史和 `VERSION=1.2.2` 一致，Pull Request 已合入且远程门禁通过。

## 10. Phase 5 完成与停止条件

只有第 9 节全部满足、Phase-05-02 合入主远程 `main`、远程门禁成功且实施记录完整，Phase 5 才完成。任一真实 Redis 场景、资源归属、远程检查或实施记录缺失时不得写成完成。

阶段验收通过后立即停止。MySQL Exporter、多目标、指标派生、HTTP 安全、生产部署和其他插件能力记录为后续事项，不继续占用 Phase 5。

## 11. Phase 6 交接

- 可独立启动和有界停止的 `gopulse-redis-exporter`，以及固定环境变量和默认 `127.0.0.1:9121` 监听契约。
- `GET /health` 的进程存活语义与 `GET /metrics` 的 Prometheus 0.0.4、`200/503`、`up 1/0` 语义。
- 固定 metric family、有限标签、真实 Redis 来源及无历史/无主动推送边界。
- 日常 Bash PID 归属与隔离验收模式，供 Phase 6 Plugin Manager 接管进程生命周期时复用。
- 明确保留给 Phase 6 的工作：Backend 管理链路、插件安装/启动/停止/更新/状态查询、MetricsMonitor 周期拉取、基础校验与 GoPulse 标准 metrics 消息封装。
