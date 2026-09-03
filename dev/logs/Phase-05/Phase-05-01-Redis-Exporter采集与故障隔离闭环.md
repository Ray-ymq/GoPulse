# Phase 5-01：Redis Exporter 采集与故障隔离闭环实施记录

## 1. 执行基线

- 执行日期：2026-09-03。
- 权威远程：`origin`。
- 已执行 `git fetch --prune origin`；从最新 `origin/main` 的 `1d24246`（根版本 `1.1.3`）创建 `develop/1.2.1`。
- 开始时旧分支工作区显示 `dev/imple/Phase-03/Phase-03-03-集成验收与里程碑收口.md` 有修改；切换到最新 `origin/main` 后该内容与新基线一致。本批未编辑、暂存或提交该计划文件。
- 开始前 `.run` 中仅观察到 `.run/bin/gopulse-backend` 和 `.run/dev-wsl-validation.sh`，没有 Redis Exporter 记录。
- 开始前 Docker Compose project 包含日常 `gopulse` 和已有 `gopulse-phase0203-integration`；相关监听快照只观察到 `127.0.0.1:5672`。隔离 Exporter 验收前后对日常 `gopulse` 容器、volume 和 `.run/*.json` 快照进行了比较，未发生变化。

## 2. 实际完成工作

### 2.1 独立 Redis Exporter module

- 新建独立 module `github.com/Ray-ymq/GoPulse/exporters/redis`，使用 Go 1.26；没有创建根 `go.work`，也没有导入 Backend internal package。
- 使用 `go-redis/v9` 执行请求时 `INFO server clients memory stats cpu keyspace`，使用 Prometheus `client_model` DTO 与 `common/expfmt` 编码 text exposition 0.0.4。
- 配置 loader 读取既有 Redis target 变量和四个 `REDIS_EXPORTER_*` 变量，校验 host、端口、非负 DB、`100ms..10s` scrape timeout 与 `1s..30s` shutdown timeout；配置错误只输出字段和稳定 reason code。
- collector 为每次调用构建新的完整 `Snapshot`，严格拒绝缺失/重复字段、负计数器、非有限或负 CPU、整数溢出和无效 keyspace；配置 DB 不存在时只将该 DB 的 keys/expires 置零。

### 2.2 HTTP、故障隔离与运行时

- `GET /health` 固定返回 Redis Exporter 进程健康 JSON，不调用 collector。
- `GET /metrics` 成功时先完成一次完整采集，再一次性编码固定十个 metric family（CPU family 含两个 mode sample）和 `up 1`；响应设置固定 Prometheus Content-Type 与 `Cache-Control: no-store`。
- Redis 不可用、认证失败、超时或响应解析失败时返回 `503`，正文只含 `gopulse_redis_up` 的 HELP、TYPE 和 `0`，不保留上次成功值。
- query/body 被拒绝，非 GET 返回 `405`，未知路径返回 `404`。
- 常驻入口不在启动时探测 Redis；配置固定 HTTP timeouts，捕获 `SIGINT`/`SIGTERM`，有界关闭 HTTP server 和 Redis client。
- 日志沿用 Schema v1 JSON 格式，固定 `service=redis-exporter`，使用 `config`、`http`、`collector`、`runtime` 模块和有限 reason code；真实认证验收确认日志不含错误密码或完整 target 地址。

### 2.3 Bash 生命周期、隔离验收和 CI

- `scripts/dev.sh` 增加 Exporter 配置合并/校验、端口冲突检测、独立构建、Redis 健康后启动、进程监测和 `.run/redis-exporter.json` 记录。
- Exporter 记录绑定 `exporters/redis` cwd、绝对 executable、start ticks 和 command marker；启动失败清理路径纳入本次已启动进程集合。
- `scripts/verify.sh` 只读检查 Exporter 进程身份、固定 `/health` JSON，以及正常日常 Redis 下 `/metrics` 的 HTTP 200、Content-Type 和 `up 1`。
- `scripts/down.sh` 按 Frontend、Redis Exporter、Search Indexer、Business Worker、Backend 顺序验证身份后停止进程；只有确认进程已不存在时才删除 stale record，身份失配或记录损坏时保留记录并拒绝发信号。`scripts/dev.sh` 启动前采用相同的“可证明 stale”边界。
- 新增 `scripts/verify-exporter.sh`：`--self-test` 在不访问 Docker 的情况下证明错误 marker/项目名被拒绝；默认模式创建随机白名单 Compose project、Redis 7.2.5 container/network/volume、临时 binary/record/ports，并在每个破坏或删除动作前验证归属。
- 默认隔离矩阵实际覆盖实时 INFO 对值、全部 metric family/type/有限值、Redis 停止、错误密码、TCP 超时、进程存活、目标恢复、SIGTERM、端口释放和日常资源快照不变。
- Reusable Quality Gates 新增独立 `Redis Exporter` job，运行 formatting、unit、vet、race 和真实隔离 Redis acceptance；Scripts and Compose job 增加 Exporter Go/Bash LF、syntax 与 self-test。
- 更新 CI 治理单元测试，使 planning-only `update` 对 5 个产品 job 的条件门控进行断言。

### 2.4 文档与版本

- `.env.example` 增加回环 `127.0.0.1:9121`、`2s` scrape timeout 和 `5s` shutdown timeout；未覆盖现有 `.env`。
- 更新根 README、Exporter 索引和 Redis Exporter README，记录运行方式、端点、指标、失败语义、被动采集边界和 Phase 6 交接边界。
- 根 `VERSION`、Frontend `package.json` 和 lockfile 根版本同步为 `1.2.1`。

## 3. 实际变更文件

- `.env.example`
- `.github/workflows/quality-gates.yml`
- `README.md`
- `VERSION`
- `exporters/README.md`
- `exporters/redis/README.md`
- `exporters/redis/go.mod`
- `exporters/redis/go.sum`
- `exporters/redis/cmd/redis-exporter/main.go`
- `exporters/redis/internal/collector/collector.go`
- `exporters/redis/internal/collector/collector_test.go`
- `exporters/redis/internal/config/config.go`
- `exporters/redis/internal/config/config_test.go`
- `exporters/redis/internal/httpapi/handler.go`
- `exporters/redis/internal/httpapi/handler_test.go`
- `exporters/redis/internal/logging/logging.go`
- `frontend/package.json`
- `frontend/package-lock.json`
- `scripts/ci/test_auto_pr_workflow.py`
- `scripts/dev.sh`
- `scripts/down.sh`
- `scripts/verify.sh`
- `scripts/verify-exporter.sh`
- `dev/logs/Phase-05/Phase-05-01-Redis-Exporter采集与故障隔离闭环.md`

## 4. 实际验证与结果

### 4.1 Redis Exporter 固定 Go 门禁

```bash
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
```

结果：全部通过；race detector 未报告数据竞争，gofmt 无未格式化文件。

实施早期第一次 `go test ./...` 曾因错误调用 `redislogging.SetLogger` 编译失败；已改为 go-redis 公共入口 `goredis.SetLogger`，受影响 Go 门禁随后通过。

### 4.2 Bash、Compose 与资源安全

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
```

结果：全部通过。

- Exporter self-test：错误进程 marker 被拒绝且测试 sleep 进程未被误杀；合法随机项目名通过，非白名单 `gopulse` 项目名被拒绝；未访问 Docker。
- 既有 business safety self-test：1 个安全目标通过，6 个不安全目标在 Docker 访问前被拒绝。因本批收紧了共享 Bash stale-record 处理，修复后重新执行该 self-test 并通过。
- Compose 仍保持既有 5 个回环发布；本批没有把 Exporter 增加为 Compose 服务。

### 4.3 真实 Redis 定向验收

```bash
scripts/verify-exporter.sh
```

结果：通过。

- 隔离 project：`gopulse-exporter-c9e88e50de67`（本次随机值，已清理）。
- 使用密码保护的真实 `redis:7.2.5-alpine`，配置 DB 5 写入普通 key 和过期 key，并制造一次 hit 与 miss。
- 首次 `/metrics` 返回 `200`，固定 Content-Type、全部 metric family/type、有限值、DB keys/expires 和 `up 1` 通过；hits、misses、keys、expires 与同一 Redis 的实际 `INFO` 一致。
- Redis 停止后 `/metrics` 在边界内返回 `503` 和唯一 `up 0`，`/health` 保持 `200`，Exporter PID/start identity 不变。
- Redis 恢复后未重启 Exporter即恢复 `200` 和 `up 1`。
- 错误密码返回相同公共 `503/up 0`，日志 reason 为 `redis_auth_failed`，未出现错误密码或完整目标地址。
- 挂起 TCP target 在 `300ms` scrape timeout 下返回 `503/up 0`，日志 reason 为 `redis_timeout`。
- `SIGTERM` 后 Exporter 在 shutdown timeout 内退出，监听端口释放。
- 隔离 container、network、volume、进程和临时目录均清理；日常 `gopulse` 快照前后一致。

### 4.4 治理和版本

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.2.1 --base-ref origin/main
git diff --check
```

结果：全部通过。

- Python CI：24 项测试通过。
- 第一次 Python CI 执行发现 `test_auto_pr_workflow.py` 仍断言 4 个产品 job；更新为新增 Exporter 后的 5 个，随后全量通过。
- 根和 Frontend 版本一致为 `1.2.1`。
- `develop/1.2.1` 与 Phase-05-01 分配一致，分支治理通过。
- 计划示例使用 `upstream/main`，本仓库配置的权威远程名为 `origin`，因此实际使用等价的 `origin/main`。
- Git whitespace 检查通过。

## 5. 与方案的偏差

- 预计目录名写作 `internal/httpserver`，实际使用 `internal/httpapi`；职责和公开 HTTP 契约不变，没有范围缩减。
- 隔离验收的 Redis project、端口和 token 每次随机；记录中的 project 名仅对应本次实际成功运行。
- 未执行完整 `scripts/verify-business.sh` 或日常 `dev.sh → verify.sh → down.sh`。原因是本批没有改变业务事实、数据库、AMQP、Elasticsearch 或 Frontend 产品行为，且当前机器开始时已有来自其他 checkout/批次的日常与集成 Compose project；直接复用固定 `gopulse` 项目会违反资源隔离要求。按计划以 `verify-business.sh --self-test` 保护共享安全边界，并由不依赖其他应用的真实 `verify-exporter.sh` 完成本批闭环。跨组件日常栈矩阵留给 Phase-05-02。
- 没有执行尚未发生的远程 CI、PR 或合并，也未将其记录为通过。

## 6. 已知限制与后续项

- Exporter 只支持单一静态 Redis target，不提供 TLS、HTTP 鉴权、动态配置、多目标、后台采集、缓存或派生 rate/ratio。
- Phase 5 不定义 MetricsMonitor Envelope、Plugin Manager 所有权或持久化；这些属于 Phase 6。Phase 6 改变启动所有者时必须保留本批 executable、环境变量、端点、信号关闭和进程身份契约。
- Phase-05-02 仍需在同一最终构建上执行阶段级跨组件验收、必要业务回归、远程门禁和阶段收口。

## 7. 完成结论

Phase-05-01 的独立 module、配置边界、严格 INFO snapshot、固定 Prometheus 契约、目标故障隔离、无需重启恢复、结构化脱敏日志、Bash 生命周期、真实隔离验收、独立 CI job、文档和 `1.2.1` 版本元数据均已完成。固定本地门禁没有阻断失败，本批达到提交条件。
