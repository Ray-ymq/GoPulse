# Phase 8-01：Marshaller 与 VictoriaMetrics 指标闭环开发记录

## 1. 实施结果

- 实施日期：2026-09-04
- 目标版本：`1.5.1`
- 开发分支：`develop/1.5.1`
- 基线：`origin/main` / `f737ffa`
- 状态：已完成。PR #73 的 10 项远程 checks 全部通过，并于 2026-09-04 以 squash commit `ea3b910` 合入 `main`；远程 `develop/1.5.1` 已按普通开发分支生命周期删除。

本批建立了真实 Redis → Redis Exporter → MetricsMonitor → Message Router → Kafka → Marshaller → VictoriaMetrics 的最小指标闭环。Marshaller 使用正式 group `gopulse-marshaller-metrics-v1`、earliest 初始位置和手动 offset，独立执行完整 Envelope v1/Redis metrics 二次校验，将合法 record 确定性转换为 Prometheus text，并在 VictoriaMetrics 明确返回空 `204 No Content` 且 partition generation ownership 仍有效后提交。

## 2. 实际完成内容

### 2.1 Marshaller module 与运行时

- 新建独立 Go module `github.com/Ray-ymq/GoPulse/marshaller`，Go 与 franz-go 版本和 Router 基线一致。
- 新增 `cmd/marshaller`、严格配置、Schema v1 单行 JSON logging、信号处理、HTTP lifecycle 和 Kafka 有界关闭。
- `/health` 仅返回进程存活；Bearer 保护的 `/ready` 在统一 deadline 内检查 Kafka、固定 Topic 和带 Basic Auth 的 VictoriaMetrics。
- 配置在创建 Kafka client和 HTTP listener 前完成验证；依赖暂不可用时进程保留 health 并通过 readiness 返回 `503`。

### 2.2 严格 Envelope 与 metrics 二次校验

- record value 限制为 1 MiB，并要求有效 UTF-8、唯一 JSON object、无尾随 token。
- 先用 token scanner 递归拒绝任意层 object 重复 key，再用 `DisallowUnknownFields` typed decoder 拒绝固定 schema object 的未知字段。
- 固定验证 schema/type/source、key/message ID、UTC RFC3339Nano、未来 5 分钟偏差、插件/目标身份和稳定三段 SemVer。
- 完整验证 `success` 的 10 family/11 sample 和 `target_unavailable` 的唯一 `up=0`，包括 family/kind/labels、CPU mode、共享 DB label、有限数值、counter 非负和 canonical sample 唯一性。

### 2.3 确定性转换与 VictoriaMetrics client

- 每个 Envelope 形成一个最大 2 MiB 的 Prometheus import body；sample 与 labels 稳定排序，label text 转义，value 使用 `FormatFloat('g', -1, 64)`。
- 固定增加 `source=redis` 和 `target_id=redis-exporter-local`，不增加 message/plugin/status/Kafka 等高基数标签。
- 所有 sample 使用 Envelope Unix 毫秒，RFC3339Nano 的纳秒部分按 Go `UnixMilli` 规则截断。
- VictoriaMetrics client 使用专用 timeout、Basic Auth、禁止 redirect、有限响应读取；写入只接受空 body 的 `204`，错误不回显凭据或响应正文。

### 2.4 Consumer offset 与 ownership 状态机

- franz-go Consumer 固定 Topic/group、earliest 和 `DisableAutoCommit`，不启用 Topic 自动创建。
- `OnPartitionsAssigned` 建立 generation lease；revoke/lost 立即取消并移除旧 lease；shutdown 取消全部 lease。
- 合法 record 只有在 transform、VM transport acceptance 和 lease 复验后提交；永久无效 record 不调用 VM，在 lease 有效时提交并继续。
- 暂时存储失败保持当前 record 未提交并执行有界指数退避；退避、write 和 commit 都能被 ownership/shutdown 取消。
- commit 失败将 partition 标记为 halted 并停止 Consumer 推进；进程保留 `/health`，而 `/ready` 返回 `503`，不处理后续 record；延迟成功但 lease 已失效时不调用 commit。
- 单元测试通过注入 Decoder、Transformer、Writer、Committer 和 Ownership 覆盖成功、永久失败、暂时失败、commit 失败、revoke/lost、延迟响应和退避取消。

### 2.5 Compose、Bash lifecycle、验收与 CI

- `deploy/compose.yaml` 新增固定 `victoriametrics/victoria-metrics:v1.151.0`、loopback 端口、Basic Auth、独立 volume、`1ms` dedup 和带认证健康检查。
- `.env.example` 新增 Marshaller/VM 开发配置；`dev.sh` 在 Kafka/Topic/VM 后依次启动 Router、Marshaller、Monitor，使用 `.run/marshaller.json` 记录强进程身份。
- `down.sh` 按 Monitor/Exporter → Marshaller → Router 顺序停止应用并保留日常 volumes；`verify.sh` 只读检查 VM container/volume、Marshaller PID、health/readiness 和固定指标查询。
- 新增 `verify-marshaller.sh`：self-test 无 Docker；默认模式创建随机隔离 Compose project，运行真实 success、target unavailable/recovery、代表性永久坏 record 后继续、VM outage offset 不推进、恢复后提交、instant/range query、`vm_rows_invalid_total` 和资源清理。
- CI 新增 Marshaller 独立 job，并扩展 LF/Bash、self-test 和 Compose 固定镜像/loopback/auth/dedup/volume 契约。

### 2.6 文档与版本

- 更新根 README、Marshaller README、Router/Monitor 交接说明。
- 根 `VERSION`、Frontend package metadata 同步到 `1.5.1`。

## 3. 变更文件

- Marshaller：`marshaller/go.mod`、`marshaller/go.sum`、`marshaller/cmd/marshaller/main.go`、`marshaller/internal/**`（含独立 logging handler）、`marshaller/README.md`
- 基础设施与配置：`deploy/compose.yaml`、`.env.example`
- 生命周期与验收：`scripts/dev.sh`、`scripts/down.sh`、`scripts/verify.sh`、`scripts/verify-marshaller.sh`
- CI：`.github/workflows/quality-gates.yml`、`scripts/ci/test_auto_pr_workflow.py`、`scripts/ci/test_verify_business.py`
- 文档与版本：`README.md`、`router/README.md`、`monitor/README.md`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`
- 开发记录：本文件

未修改冻结的 `scripts/*.ps1`。未修改用户已有的未跟踪文件 `使用指南.md`。

## 4. 验证记录

### 4.1 实施期间定向验证

- `(cd marshaller && go test -count=1 ./...)`：通过。
- `(cd marshaller && go vet ./...)`：通过。
- `(cd marshaller && go test -race -count=1 ./...)`：通过。
- `scripts/verify-marshaller.sh --self-test`：通过，9 个配置/project/query/port 负向 case 被拒绝。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：更新 CI job 数与 lifecycle defaults 契约后，25 项通过。
- `scripts/verify-router.sh --self-test`、`scripts/verify-monitor.sh --self-test`、`scripts/verify-exporter.sh --self-test`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过；rendered Compose 有 7 个 loopback published ports。

### 4.2 真实隔离链路

`scripts/verify-marshaller.sh` 最终通过，实际证明：

- 真实 Redis success 产生并查询到 up、connected clients、commands、CPU、memory 和 keyspace 指标；
- fixture 仅注入一个坏 key record，Marshaller 记录 `message_id_mismatch`，提交该永久无效 record 并继续处理后续真实消息；
- Redis stop/start 产生并查询到 `up=0` 和恢复后的 `up=1`；
- VictoriaMetrics stop 后，先等待 Marshaller 进入真实 `write_retry`，再固定 committed offset；Kafka end offset 继续增长时 committed offset 不推进；VM 恢复后该 offset 推进；
- instant query、query_range 和 `vm_rows_invalid_total` 检查通过；
- 正常退出后无本批隔离 container、network、volume、进程或监听端口残留。

真实验收前两次运行暴露并修复了验收实现问题：第一次发现 VictoriaMetrics 镜像 BusyBox `wget` 不支持 `--user/--password`，改为明确 Basic Authorization header；第二次发现 consumer-group 表格解析列错误以及 VM stop 前可能已有一个已接受的在途 record，分别修正列解析，并在断言 offset 稳定前等待真实 `write_retry`。这些失败没有被记录为产品能力通过；修复后的完整默认模式重新执行并通过。

### 4.3 最终固定门禁

最终 diff 上执行并通过：

- `(cd marshaller && test -z "$(gofmt -l .)")`
- `(cd marshaller && go test -count=1 ./...)`
- `(cd marshaller && go vet ./...)`
- `(cd marshaller && go test -race -count=1 ./...)`
- `(cd router && test -z "$(gofmt -l .)" && go test -count=1 ./...)`
- `(cd monitor && test -z "$(gofmt -l .)" && go test -count=1 ./...)`
- `(cd exporters/redis && go test -count=1 ./...)`
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：25 项通过。
- `python3 scripts/ci/validate_versions.py`
- `python3 scripts/ci/validate_branch.py --branch develop/1.5.1 --base-ref upstream/main`
- 第 8 节列出的全部 Bash syntax 检查。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- `scripts/verify-marshaller.sh --self-test`
- `scripts/verify-marshaller.sh`（在版本同步到 `1.5.1` 后重新执行完整隔离链路并通过）
- `scripts/verify-router.sh --self-test`
- `scripts/verify-monitor.sh --self-test`
- `scripts/verify-exporter.sh --self-test`
- `git diff --check`
- GitHub push workflow run `33860357343` 完成权威质量门禁；自动创建 PR #73，PR 页面记录 10 项 checks 通过，随后 squash merge 为 `ea3b910`。

## 5. 与计划偏差

- 计划预计可能拆分更多内部 package；实际保持 `config`、`envelope`、`consumer`、`metrics`、`victoriametrics`、`httpserver` 六个明确边界，没有建立无业务价值目录。
- 仓库当前只有 `.github/workflows/quality-gates.yml` 作为 reusable gates 定义，因此 Marshaller job 直接加入该文件，没有创建不存在的第二份 workflow。
- 本批没有声明真实 broker rebalance/restart、Marshaller 进程恢复、完整 VM/Kafka 故障矩阵或 exactly-once；这些仍属于 Phase-08-02。

## 6. 已知限制与后续项

- Topic 当前固定单 partition；真实多 generation broker rebalance 和进程重启恢复尚未执行，留给 Phase-08-02。
- VictoriaMetrics `204` 仅作为 HTTP transport acceptance；本批通过封闭 transformer、实际查询和 `vm_rows_invalid_total` 补充验证，但不宣称逐 sample 事务确认。
- Marshaller 暂不提供产品 Metrics Query API、Frontend 页面、Dashboard、告警、任意 MetricsQL 代理或多租户能力。
- 本批远程门禁和合入已完成。Phase-08-02 继续承担真实 broker rebalance/restart、Marshaller 进程恢复和完整 VM/Kafka 故障矩阵。
