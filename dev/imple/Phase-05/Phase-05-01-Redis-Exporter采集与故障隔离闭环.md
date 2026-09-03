# Phase 5-01：Redis Exporter 采集与故障隔离闭环实施方案

> 执行序号：1 / 2
>
> 总方案来源：[Phase-05-总实施方案.md](Phase-05-总实施方案.md)

## 1. 批次目标

从 `exporters/` 仅有占位说明的基线，纵向交付一个独立、常驻、被动拉取的 Redis Exporter：真实 Redis 7.2.x → 按 `/metrics` 请求即时执行 `INFO` → Prometheus text exposition 0.0.4 → 成功指标或明确的目标失败状态。

本批必须同时证明正常采集、Redis 停止/认证失败时的进程存活、无陈旧数据和 Redis 恢复后的自动恢复，并将 Exporter 纳入 Bash 生命周期、进程归属保护和独立 CI 门禁。本批完成后，核心产品能力已经可独立运行；Phase-05-02 只做阶段级集成验收与收口。

## 2. 前置条件

- Milestone 1 `1.0.0` 已发布，Phase 4 全部批次已合入主远程并通过其固定验收；根版本为 Phase 4 的最终 `1.1.x`。
- 已核对 Phase 4 最终结构化日志和 Bash 生命周期契约；若与总方案冲突，先更新 Phase 5 方案而不是在实现中静默偏离。
- 已 fetch 主远程并从包含 Phase 4 的最新 `main` 创建 `develop/1.2.1`，未沿用 `update`、旧 release 分支或已完成开发分支。
- 在 WSL2 Linux filesystem 中实施，Docker daemon、Go、Bash、Python、curl 和现有质量门禁可用。
- 开始前记录 Git 状态、日常 Compose project/volume、`.run` 进程和端口状态，不覆盖用户改动。

## 3. 实施范围

### 3.1 独立 Go module 与配置

- 在 `exporters/redis` 建立独立 module `github.com/Ray-ymq/GoPulse/exporters/redis`，Go 版本与仓库实施基线一致，不新增根 `go.work`。
- 使用 `go-redis/v9` 访问 Redis，使用 Prometheus 官方 Go 库的 DTO/编码能力；不复用 `backend/internal`，不注册默认 runtime/process collectors。
- 独立配置 loader 复用 `REDIS_HOST`、`REDIS_PORT`、`REDIS_PASSWORD`、`REDIS_DB`，并读取 `REDIS_EXPORTER_HTTP_HOST`、`REDIS_EXPORTER_HTTP_PORT`、`REDIS_EXPORTER_SCRAPE_TIMEOUT` 和 `REDIS_EXPORTER_SHUTDOWN_TIMEOUT`。
- 严格校验 host、端口、DB 和 duration；scrape timeout 为 `100ms..10s`，shutdown timeout 为 `1s..30s`。无效配置在监听前脱敏报错并非零退出。
- `.env.example` 增加默认回环监听 `127.0.0.1:9121`、`2s` scrape timeout 和 `5s` shutdown timeout；既有 `.env` 不自动覆盖。

### 3.2 Redis `INFO` 采集与指标快照

- collector 接口以 context 返回一个完整、不可变的 scrape snapshot；HTTP 层只依赖该接口，便于证明 `/health` 不触发采集。
- 每次 `/metrics` 执行一次 `INFO server clients memory stats cpu keyspace`，解析总方案第 7 节的固定字段。
- 必填字段缺失、格式错误、负计数器、非有限浮点、整数溢出或 Redis 命令失败使整个 snapshot 失败。
- 配置 `dbN` 不存在时输出 keys/expiring keys 为 `0`；存在时严格解析 `keys` 与 `expires`，忽略 `avg_ttl` 和未配置数据库。
- 不在 collector 中保存上一次 snapshot、命中率、速率或采集累计值；Redis client pool 只管理连接。

### 3.3 HTTP 与 Prometheus 契约

- `GET /health` 固定返回 `200` 与 `{"status":"ok","service":"redis-exporter"}`，不调用 collector。
- `GET /metrics` 成功返回 `200`、`Cache-Control: no-store`、Prometheus 0.0.4 Content-Type、`up 1` 和全部固定指标。
- 目标连接、认证、超时或解析失败返回 `503`，正文只编码 `gopulse_redis_up` 的 `HELP`、`TYPE` 与值 `0`；不得输出部分或陈旧样本。
- 接口不接受 query 或 body；非 `GET` 返回 `405`，未知路径返回 `404`。
- 每次响应先完成采集与内存快照，再编码一次；不得边读 Redis 边向客户端写入，避免失败时出现半个成功响应。

### 3.4 常驻运行与结构化日志

- 新增 `cmd/redis-exporter`，启动时不主动探测 Redis；Redis 当时不可用不影响 HTTP server 启动。
- HTTP server 设置固定 read-header、read、write 和 idle timeout，write 边界大于 scrape timeout 且小于无界等待。
- 捕获 `SIGINT`/`SIGTERM`，在 shutdown timeout 内停止接收、等待在途请求、关闭 Redis client 并退出；关闭超时或 server 异常返回非零。
- 使用 Phase 4 JSON 日志约定，`service=redis-exporter`；日志只记录稳定 reason code 与必要运行状态，不输出密码、完整连接串、原始 `INFO` 或业务 key。

### 3.5 Bash 生命周期与定向验收

- `scripts/dev.sh` 校验 Exporter 配置/端口，构建 `.run/bin/gopulse-redis-exporter`，在 Redis 健康后启动并写入 `.run/redis-exporter.json`。
- PID 记录固定 cwd=`exporters/redis`、绝对 executable、start ticks 与 command marker；启动失败时只清理本次已启动的仓库应用。
- `scripts/verify.sh` 只读验证 Exporter 进程身份、`/health` JSON、正常日常 Redis 下 `/metrics` 的 `200`、Content-Type 和 `up 1`。
- `scripts/down.sh` 经身份校验停止 Exporter；记录失配时拒绝发信号，只清理可证明已 stale 的记录。
- 新增 `scripts/verify-exporter.sh` 与 `--self-test`，复用随机 token、白名单名称、端口、label、container、volume 与 PID 多重归属模式，执行总方案第 11.3 节的定向矩阵。
- 默认验收只启动本批需要的隔离 Redis 和 Exporter，不要求启动 Backend、Frontend、RabbitMQ 或 Elasticsearch。

### 3.6 CI、文档与版本

- Reusable Quality Gates 新增 `Redis Exporter` job，使用 `exporters/redis/go.mod`，执行 gofmt、unit、vet、race 与受密码保护的真实 Redis integration。
- Scripts and Compose job 纳入 `scripts/verify-exporter.sh` 的 LF、Bash syntax 和 `--self-test`。
- 更新根 README 与 `exporters/README.md`，说明启动、端点、配置、指标、失败语义、被动模式和 Phase 6 交接边界。
- 将根和 Frontend 版本更新为 `1.2.1`，创建本批同名实施记录，如实记录实际结果。

## 4. 实施边界与非目标

- 不实现 MySQL Exporter、多 Redis target、动态配置热更新、服务发现或可插拔 collector 注册表。
- 不实现后台 ticker、主动推送、历史缓存、rate/ratio 聚合或本地持久化。
- 不实现 `/ready`、自定义 query、指标筛选、调试配置接口、HTTP 鉴权或 TLS。
- 不实现 Backend/Frontend 管理接口、Monitor、Plugin Manager、统一 metrics Envelope、Kafka 或 VictoriaMetrics。
- 不把 Exporter 增加为 Docker Compose 应用服务，不创建应用镜像；隔离验收中的 Redis 仍使用现有固定镜像。
- 不修改 Backend Redis Repository、业务缓存语义、Phase 4 日志业务字段或冻结 PowerShell。
- 不因本批新增独立 module 而将全部 Go 组件合并到一个 module/workspace。

## 5. 预计文件与交付物

```text
exporters/README.md
exporters/redis/go.mod
exporters/redis/go.sum
exporters/redis/cmd/redis-exporter/
exporters/redis/internal/config/
exporters/redis/internal/collector/
exporters/redis/internal/httpserver/
exporters/redis/README.md
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-exporter.sh
scripts/ci/
.github/workflows/quality-gates.yml
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-05/Phase-05-01-Redis-Exporter采集与故障隔离闭环.md
```

预计文件只表示允许触达的边界；实际未修改文件不得写入实施记录。遇到跨边界需求时，先判断是否直接服务本批验收，否则记录为后续事项。

## 6. 详细实施步骤

1. 从总方案提取新增行为、固定指标和门禁，核对最新 `main`、Phase 4 日志格式、现有 Redis 配置与 Bash PID 归属实现。
2. 建立独立 Go module、配置结构和校验测试，确认模块不导入 Backend internal package 且根目录不增加 `go.work`。
3. 封装 Redis client 与 collector 接口，实现有界 `INFO` 调用和严格、一次性 snapshot 解析。
4. 固定十一组 metric samples 的名称、类型、HELP、来源和有限标签，使用 Prometheus 官方编码能力生成 0.0.4 文本。
5. 实现 HTTP handler，先完成采集再写响应；用 fake collector 验证 health 不采集、成功 `200`、失败 `503`、无部分/陈旧值及 method/path 边界。
6. 实现 `cmd/redis-exporter`、结构化脱敏日志、HTTP timeouts 与信号有界关闭；启动路径不探测 Redis。
7. 扩展 `.env.example` 和 Bash 配置解析、端口冲突、构建、启动、PID 记录、只读验证与安全停止。
8. 建立隔离 `verify-exporter.sh`，先完成无 Docker 的 target/record 负向测试，再接入真实密码 Redis。
9. 验证真实 `INFO` 对值、Redis 停止、认证失败、scrape timeout、进程存活、目标恢复、SIGTERM 和资源清理。
10. 新增 CI Redis Exporter job 与脚本检查，保持 Backend/Frontend 既有 job 不变。
11. 更新 README、目标版本和实施记录，如实记录实际文件、命令、偏差和已知限制。

## 7. 风险与控制

- **采集失败返回部分指标**：先构造完整 snapshot，成功后一次编码；任一字段失败只返回 `up 0`。
- **误把上次值当当前值**：collector 和 handler 不持有成功 snapshot，不提供缓存或 fallback。
- **`INFO` 解析脆弱**：只解析固定 Redis 7.2.x 字段，按 section/field 严格转换；配置 DB 不存在是唯一明确的零值例外。
- **标签泄漏或基数增长**：只有固定 `mode` 和数字 `db` 标签，不使用地址、错误文本、key 或命令名。
- **目标故障拖垮 HTTP**：每次 scrape 使用 context deadline，HTTP write timeout 明确大于 scrape timeout；失败不终止进程。
- **启动顺序错误**：进程启动不探测目标，日常 `dev.sh` 仍在 Redis 健康后启动；真实故障由请求时状态表达。
- **脚本误杀进程或删除资源**：继续使用 cwd/executable/start ticks/marker 与 Compose label/container/port 多重验证。
- **独立 module 漏过 CI**：增加专属 job 和 cache dependency path，不依赖 Backend job 的工作目录。
- **范围膨胀**：只实现一个目标和固定字段，不加入 Monitor、Plugin Manager、多目标或生产安全平台。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败修复后只重跑可能受影响的命令或场景：

```bash
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.2.1 --base-ref upstream/main
git diff --check
```

真实 Redis integration 可以由 `scripts/verify-exporter.sh` 和 CI Redis Exporter job 共同承载，不再另建重复的全栈验收。若实际修改共享 Bash 进程身份函数或 Compose 核心配置，补跑 `scripts/verify-business.sh --self-test`；只有真实回归风险需要时才扩展到完整 `scripts/verify-business.sh`，并在实施记录中写明依据。

## 9. 验收标准

- `exporters/redis` 可独立构建、测试和运行，不依赖 Backend module、根 `go.work` 或其他应用进程。
- 配置默认值、范围和错误脱敏符合总方案；Redis 启动时不可用不阻止 Exporter 监听。
- `/health` 在正常和 Redis 故障期间都返回固定 `200` JSON，且没有调用 collector。
- `/metrics` 正常返回真实 Redis 当前值、正确 Prometheus 类型和 `up 1`；配置 DB 不存在时两个 DB gauge 为零。
- Redis 停止、错误密码、超时或解析失败时返回有界 `503` 和唯一的 `up 0` 样本，无部分或陈旧数据。
- Redis 恢复后无需重启 Exporter 即恢复 `200`；Exporter 不存在后台采集、主动推送或历史存储。
- 结构化日志、HTTP body 和指标不包含密码、完整连接地址、业务 key、原始 `INFO` 或原始目标错误。
- `dev.sh`、`verify.sh`、`down.sh` 与隔离验收安全管理 Exporter，失败/中断不误伤日常资源。
- CI Redis Exporter 门禁及第 8 节固定验证通过，版本元数据为 `1.2.1`，实施记录真实完整。

## 10. 明确完成条件

只有独立 module、固定 HTTP/指标契约、真实 Redis 成功采集、目标故障隔离、无需重启恢复、Bash 生命周期与定向隔离验收全部通过，且没有阻断验收的失败，才可标记 Phase-05-01 完成。只通过 unit test、静态 fixture 或正常路径不足以完成本批。

## 11. 下一批交接

- 已运行验证的 `gopulse-redis-exporter`、环境变量、信号关闭和进程身份契约。
- 稳定的 `/health`、`/metrics`、Prometheus metric family 与 HTTP 失败语义。
- 真实成功、Redis 停止/认证失败、恢复和资源清理的定向证据。
- Phase-05-02 只需在同一最终构建执行跨组件矩阵和必要回归，不得重新打开指标范围或提前实现 Phase 6。
