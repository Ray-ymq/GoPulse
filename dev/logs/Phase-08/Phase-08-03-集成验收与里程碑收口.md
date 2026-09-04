# Phase 8-03：集成验收与 Milestone 2 收口开发记录

## 1. 实施结果

- 实施日期：2026-09-04
- 目标版本：`1.5.3`
- 开发分支：`develop/1.5.3`
- 基线：`origin/main` / `9809bfd`
- 状态：已完成。PR #77 的 10 项权威 push jobs 全部通过，并于 2026-09-04 20:16:52（Asia/Shanghai）以 squash commit `058ff4d` 合入 `main`；远程 `develop/1.5.3` 已按普通开发分支生命周期删除。

本批在 Phase-08-01/02 已合入实现上完成最终集成矩阵，没有修改 Marshaller、Router、Monitor 或 Redis Exporter 的生产 Go 代码。增量集中在真实验收证据、业务隔离脚本兼容、文档和 `1.5.3` 版本收口。

## 2. 实际完成内容

### 2.1 最终真实指标矩阵

- 扩展 `scripts/verify-marshaller.sh` 的查询白名单，覆盖固定 10 个 family、11 个 success sample，而不是只检查原先的代表性子集。
- 在随机隔离 Redis 中写入普通 key、TTL key、命中、未命中和代表性命令，再从真实 Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics 链路查询全部 family。
- 对每条时序核对固定 `source=redis`、`target_id=redis-exporter-local`，以及仅允许的 `mode`/`db` 标签；拒绝 message ID、offset、status 等额外标签。
- 将 VictoriaMetrics 最新值与同一 Redis `INFO` 和 Exporter Prometheus 响应做有界采集时差比较；核对 keyspace、CPU、内存、连接、命令、hit/miss 和 uptime 事实。
- 记录本次验收窗口，并通过已有 bounded `router/cmd/verify-consumer` 从明确 partition/offset 捕获一条真实 Router record，保存有限 message ID、offset 和 Envelope Unix 毫秒时间戳用于后续原样重放。
- 停止 Redis 后查询到新的 `gopulse_redis_up=0`，恢复 Redis 后不重启 Router、Marshaller 或 Monitor 即再次查询到 `up=1` 和完整 family。

### 2.2 永久异常、offset 与继续消费

- 停止真实 Monitor/Exporter 后，只通过 fixture producer 注入三个计划规定的代表：结构错误、Kafka key/Envelope ID 不符、success sample 集合不完整。
- 每类异常均证明：固定 reason code 出现、正式 group committed offset 越过该 record、`vm_rows_inserted_total` 不增加。
- 三类异常完成后恢复同一 Monitor 插件根，真实 Redis record 再次进入同一 partition、被正式 group 提交并在 VictoriaMetrics 查询成功。
- Marshaller 日志仍只记录有限 reason code、Topic、partition 和 offset；没有增加 record body、标签全集、内部 URL 或凭据输出。

### 2.3 内部访问与确定性重放

- Marshaller `/ready` 对无 token、错 Bearer、普通/admin Cookie、Backend 风格 JWT 和 query token 均返回 `401`；只有正确内部 Bearer 成功。
- Marshaller 的 `/metrics`、`/query`、`/offsets`、`/replay` 和 `/admin` 均不存在，未形成消息接收、查询、offset 或重放管理 API。
- VictoriaMetrics 受保护查询对无/错 Basic 和普通/admin Cookie 返回 `401`，正确内部 Basic 成功；容器端口和 Marshaller listener 均核对为 `127.0.0.1`。
- 对最初从真实上游捕获的原始 Kafka key/value 进行一次原样重放。原始投递加重放使用相同 series/value/Unix 毫秒 timestamp，`1ms` dedup 下窄窗口只有一个有效点；结论仍明确为 at-least-once，而非 exactly-once。

### 2.4 故障恢复、业务隔离与生命周期

- 最终 `verify-marshaller.sh` 继续通过 Phase-08-02 已建立的 VictoriaMetrics 同进程恢复、明确未提交 record 的 Marshaller 重启重取、Kafka broker 重启和正式 group rejoin 场景。
- `scripts/verify-business.sh` 原先在 Compose 解析阶段缺少 Phase 8 VictoriaMetrics 必填变量。本批为隔离环境新增随机 VictoriaMetrics 端口与临时凭据，但仍只启动 MySQL、Redis、RabbitMQ 和 Elasticsearch，并显式断言 Kafka 与 VictoriaMetrics 容器都不存在。
- 完整 business/browser 矩阵在 Kafka/VM 均未启动时通过：登录、普通/admin 授权、帖子、评论、点赞、通知、搜索、Outbox、RabbitMQ 故障与恢复均保持原契约，Backend readiness 不依赖 VictoriaMetrics。
- 日常生命周期在临时仓库副本中执行。由于工作区已有用户 `gopulse` volumes，只在临时副本把三个脚本的 Compose project 常量替换为随机 `gopulse-lifecycle-*`，其余最终源码、配置和版本保持一致；实际完成 `dev.sh → verify.sh → 信号退出 → down.sh`。
- `verify.sh` 全部项目通过且未改变运行环境；`down.sh` 移除随机 project 的 container/network 并保留命名 volume。随后依据前后快照删除该随机 project 的命名 volume，以及本次新产生且快照可唯一归属的 5 个 anonymous volume；用户容器、volume、`.run` 和未跟踪 `使用指南.md` 保持不变。

## 3. 文件变更

- `scripts/verify-marshaller.sh`
- `scripts/verify-business.sh`
- `README.md`
- `marshaller/README.md`
- `router/README.md`
- `monitor/README.md`
- `dev/imple/Phase-08/Phase-08-总实施方案.md`
- `dev/imple/Phase-08/Phase-08-03-集成验收与里程碑收口.md`
- `dev/logs/Phase-08/Phase-08-03-集成验收与里程碑收口.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`

未修改用户未跟踪文件 `使用指南.md`。

## 4. 验证命令与结果

以下命令在最终 `1.5.3` diff 上执行并通过：

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

结果摘要：

- `scripts/ci`：25 项 unittest 通过；branch governance 确认 `develop/1.5.3` 与 Phase 权威分配一致。
- Frontend：9 个测试文件、48 项测试通过；typecheck 和 Vite production build 通过。
- Marshaller、Router、Monitor 的普通、vet、race 门禁通过；Redis Exporter 和 Backend 固定 package 回归通过。
- 最终 Marshaller acceptance 在 `1.5.3` 包版本上通过完整 success/up0/recovery、三类永久异常、内部访问、VM/Kafka/进程恢复、真实 record 重放、invalid-row 稳定和随机资源清理。
- Business acceptance 在 Kafka/VM 均未启动的随机项目中通过真实 Chromium、搜索、权限、RabbitMQ/Outbox/Worker 和资源快照矩阵。
- 临时日常生命周期的 `verify.sh` 全部 PASS，`down.sh` 保留命名 volume；隔离后续清理完成，最终无 `gopulse-marshaller-*`、`gopulse-acceptance-*` 或 `gopulse-lifecycle-*` container/network 残留。
- Frontend/Backend 产品代码扫描未发现 VictoriaMetrics/Marshaller 内部凭据、正式 group 或 loopback VM URL 泄漏。

## 5. 实施中的失败与最小修复

- 首次内部访问负向使用 VictoriaMetrics `/health`，实际该 liveness 路径公开返回 `200`。验收改为受保护的 Prometheus query 路径，准确验证查询/写入身份边界，没有改变生产配置。
- 首次完整 family 对比观察到 VM 查询结果相对当前 Exporter uptime 约 31 秒。保持 family、label、keyspace 和有限值硬约束，仅将 uptime 采集可见性容差设为 45 秒；没有放宽 schema 或存储接受规则。
- 首次尝试用 Kafka console consumer 捕获真实 record 时，控制台格式无法稳定证明 key/value 对应。改用仓库现有 bounded `cmd/verify-consumer`，读取明确 `[offset, end)` 且不提交 offset。
- 首次 `verify-business.sh` 在启动任何容器前因 Compose 全文件插值缺少 `VICTORIAMETRICS_*` 失败。新增第 9 个随机端口和临时 VM 凭据，并显式断言 VM 不启动；第二次完整 business acceptance 通过。
- 临时日常生命周期首次最终快照发现 5 个只带 `com.docker.volume.anonymous` 的新 volume。根据开始前快照确认它们只属于本次随机验收后单独删除，再次核对随机 project 无 container/network/volume 残留；未修改用户资源。

## 6. 偏差、限制与后续

- 未新增 Backend Metrics Query API、Frontend 指标页面、Dashboard、告警、聚合、DLQ、offset/replay 管理或 logs/events 能力。
- 没有重复实现或修改 Phase-08-01/02 的 Consumer、Writer、ownership/commit 状态机；其精确 commit failure、revoke/lost 与延迟 HTTP 竞态继续由已通过的注入式测试证明。
- 本批完整 shell 矩阵仍在当前单 partition、单节点 Kafka/VictoriaMetrics 开发拓扑上执行；不形成容量、HA、TLS、多租户或 exactly-once 结论。
- 本地固定验收、权威远程 push jobs、PR 合入和主远程版本均已实际观察；Phase 8 与 Milestone 2 的完成条件已经满足。

## 7. 远程门禁与合入状态

- 本地提交：`c3f7dbd test: close Phase 8 milestone acceptance`。
- 权威 GitHub Actions push run：`33871557834`，于 2026-09-04 20:16:58（Asia/Shanghai）完成并成功。
- 成功 jobs：Branch governance、Backend、Frontend、Message Router、Redis Exporter、Integration、Marshaller、Scripts and Compose、Monitor，以及 Open PR and enable auto-merge。
- 自动创建 PR #77，标题为 `test: close Phase 8 milestone acceptance`。
- PR #77 于 2026-09-04 20:16:52（Asia/Shanghai）使用 squash 策略合入 `main`，远程提交为 `058ff4dd2c786db04fc7df82bdf83ac58e4cb590`。
- 合入后已执行 `git fetch --prune origin`：`origin/main` 指向 `058ff4d`，远程 `develop/1.5.3` 不再存在，`origin/main:VERSION` 为 `1.5.3`。
- 至此实施方案第 9 节验收标准、第 10 节完成条件和总方案第 18.1～18.2 节全部满足；Phase 8 与 Milestone 2 已完成。
