# Phase-10-02：采集故障事件与可靠性闭环实施记录

## 1. 实施信息

- 实施日期：2026-09-05。
- 目标版本：`1.7.2`。
- 开发分支：`develop/1.7.2`。
- 基线：`origin/main` / `1c8b55b`（包含 Phase-10-01）。
- 实施环境：WSL2 Linux filesystem `/home/ray/GoPulse-1.7.2`，使用独立 Git worktree，未改动原 `/home/ray/GoPulse` 工作区中的用户未提交文件。
- 状态：本地实现与固定门禁完成；Pull Request 合入主远程和远程门禁仍需在提交推送后完成，因此尚未宣称满足实施方案第 10 节的远程完成条件。

## 2. 实际完成内容

### 2.1 固定 Events 词汇与三端合同

- Monitor 与 Marshaller 同步开放并严格校验：
  - `exporter_plugin_failed`、`exporter_plugin_exited`；
  - `metrics_collection_failed`、`metrics_collection_recovered`；
  - `metrics_target_unavailable`、`metrics_target_recovered`。
- 固定每个事件的英文 message、severity、operation、error code、state 与 scrape status 组合；未知字段、不可能组合、自由文本和敏感片段继续拒绝。
- Events metadata 新增可选的 `error_code`、`scrape_status`，既有生命周期 payload 保持兼容。
- Backend 查询过滤器允许新增固定词汇，并拒绝 event/severity/operation/error_code 的不可能组合；返回文档按相同合同复核。

### 2.2 Plugin Manager 终态失败与异常退出

- Manager 构造配置可直接注入 Event recorder，恢复期的 `recovery_invalid` / `recovery_failed` 可在恢复流程中记录，不再依赖恢复完成后的附加顺序。
- start 终态失败记录 `exporter_plugin_failed` / `start_failed`，update rollback 结果按最终安全错误码和最终状态记录失败事件。
- 非预期进程退出在 process record 清理、MetricsMonitor disable、runtime ownership 复核和 observed state=`failed` 提交后记录 `exporter_plugin_exited` / `process_exited`。
- 预期 stop/update/shutdown 继续使用 intentional 标记抑制异常退出事件；旧 runtime 不得覆盖替换后的状态。
- Event recorder 拒绝不会回滚插件状态或改变操作结果。

### 2.3 Metrics collection/target episode

- MetricsMonitor 将每次 scrape 收敛为最终结果；只有 metrics Envelope 成功发布后才提交成功/target status 和恢复事件。
- collection failure 按 generation 去抖：连续 scrape/parse/message/publish 失败只记录一次，完整发布成功后记录一次 recovered。
- Redis target unavailable 与 collection failure 使用独立状态：只有成功发布的 `target_unavailable` 才进入 unavailable episode，成功发布 `success` 后记录 target recovered。
- disable 后重新 enable 会清空 episode，不为停机窗口生成假失败或假恢复。
- EventMonitor `Record` 返回 false 不回滚 episode，不阻塞 scrape，也不改变 Plugin Status。

### 2.4 Elasticsearch、Kafka 与三类消息可靠性

- Events template 和现有 Events v1 index mapping 增加 strict keyword 字段 `metadata.error_code`、`metadata.scrape_status`。
- Marshaller 写入前对已存在的日索引执行兼容 mapping 扩展；新索引仍由 template 创建，写后继续核对 strict mapping 与 read alias。
- `scripts/verify-events.sh` 扩展为真实可靠性验收：
  - Router 短时中断产生 collection failure/recovery；
  - Redis 停止/恢复产生 target unavailable/recovered；
  - 不可执行插件产生真实 start terminal failure，恢复权限后可再次启动；
  - kill 真实 exporter PID 产生 unexpected exit；
  - 注入永久非法 Kafka record 后，后续合法事件仍可查询；
  - Elasticsearch 停止期间正式 group committed offset 保持不变，恢复后继续推进；
  - 从 Kafka 捕获合法 Events key/value 原样重放，Events alias 文档数量不增加；
  - 同一正式 Topic/group 中的 Redis metrics、Backend logs 和 Events 分别可在 VictoriaMetrics、Logs alias 和 Events alias 观察。
- admin 查询、未登录 `401`、普通用户 `403`、strict mapping 和敏感哨兵检查继续保留。

### 2.5 文档与版本

- `docs/events-v1.md` 更新为 1.7.2 failure/recovery、episode、mapping 迁移和可靠性交付语义。
- README Phase 10 能力描述更新。
- 根 `VERSION`、Frontend package 与 lockfile 版本同步为 `1.7.2`。

## 3. 变更文件

- Monitor：
  - `monitor/cmd/monitor/main.go`
  - `monitor/internal/events/contract.go`
  - `monitor/internal/events/monitor_test.go`
  - `monitor/internal/metrics/collector/collector.go`
  - `monitor/internal/metrics/collector/collector_test.go`
  - `monitor/internal/plugin/manager.go`
  - `monitor/internal/plugin/manager_test.go`
- Marshaller：
  - `marshaller/internal/events/events.go`
  - `marshaller/internal/elasticsearch/events_client.go`
  - 对应 Events/Elasticsearch 测试。
- Backend：
  - `backend/internal/eventquery/eventquery.go`
  - `backend/internal/eventquery/eventquery_test.go`
- 验收、文档与版本：
  - `scripts/verify-events.sh`
  - `docs/events-v1.md`
  - `README.md`
  - `VERSION`
  - `frontend/package.json`
  - `frontend/package-lock.json`
  - 本实施记录。

## 4. 实际验证与结果

### 4.1 Monitor

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin ./internal/metrics/collector)
```

结果：全部通过。race 验证覆盖 EventMonitor、Plugin Manager 与 Metrics collector；新增测试证明 collection/target 去抖、终态 start failure 和 unexpected exit 状态提交顺序。

### 4.2 Marshaller

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./internal/events ./internal/elasticsearch ./internal/consumer)
```

结果：全部通过。覆盖新增 Events contract、strict mapping 扩展、永久错误继续、暂时错误重试和 ownership 丢失不提交。

### 4.3 Backend 与仓库门禁

```bash
(cd backend && go test -count=1 ./internal/eventquery ./internal/http/...)
(cd backend && go vet ./internal/eventquery ./internal/http/...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-events.sh \
  scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh \
  scripts/verify-logs.sh scripts/verify-business.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
git diff --check
```

结果：全部通过；CI Python 共 25 项通过，版本与分支治理通过。

### 4.4 自检与真实集成验收

```bash
scripts/verify-events.sh --self-test
scripts/verify-events.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-logs.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
```

结果：全部最终通过。

- `verify-events.sh` 最终输出：failure、recovery、replay、offset 和 mixed Events 闭环通过，物理索引为随机隔离项目中的 `gopulse-events-v1-2026.09.05`。
- 验收实际观察到固定生命周期、start failure、unexpected exit、collection failure/recovery、target unavailable/recovered；同 ID 重放 count 不增加；ES 故障期间 offset 不推进，恢复后推进；Metrics/Logs/Events 三类存储均可查。
- `verify-business.sh` 完整业务验收通过：首轮 Playwright 2 passed / 2 skipped（按脚本矩阵选择），targeted search-rebuild 与 search-live 各 1 passed，Phase 2 可靠性矩阵和 Phase 4 process log 校验通过。
- 全部真实验收使用随机强归属 Compose project、端口、数据库、plugin root 和临时目录；脚本结束后仅清理所属资源。

### 4.5 验收中发现并修正的问题

- 首次扩展 Events 验收使用了“collection failure 总数必须等于 1”的过强断言；Redis 故障前后可形成独立的合法 collection episode。改为验证 failure/recovery 成对、每个连续窗口不洪泛。
- 一次真实运行中 Monitor scrape timeout 早于 exporter Redis timeout，导致只能观察 collection network failure，不能稳定观察合法 `target_unavailable`。验收环境改为 exporter timeout `100ms`、Monitor timeout `750ms`，确保 target 状态由 exporter 合同返回并成功发布后再产生事件。
- 上述修正后重新完整执行 `scripts/verify-events.sh`，最终通过；没有把失败运行记录为完成证据。

## 5. 与计划的偏差

- 计划描述 Events Store mapping 字段“已预留”，实际 Phase-10-01 mapping 只有六个 metadata 字段。为保持现有 `gopulse-events-v1-*` alias/index，不新建版本或动态字段，本批增加受控的 existing-index `_mapping` 扩展，并在每次写入前确认 template/mapping，随后严格验证八字段合同。
- collection failure 的真实数量不固定为全流程一个；Router 故障和 Redis 停止过程中的独立传输/网络窗口可形成两个已恢复 episode。实现保持“一个连续 episode 只发一次”的合同，验收按 episode 配对而非全流程绝对计数。
- 未执行一般代码审查、依赖审计或覆盖率扩张；测试与验收仅围绕新增词汇、episode、进程退出、mapping、offset、重放和三类共存。

## 6. 已知限制与后续事项

- EventMonitor 仍是有界内存 best-effort source queue；进程崩溃或容量耗尽时，尚未进入 Kafka 的远程 Events 副本可能丢失，这是既定产品边界。
- 本地实现不能替代实施方案要求的 PR 合入和远程固定门禁。提交推送后需创建 Pull Request，确认远程门禁通过并合入 `main`，再由 planning/status 更新记录正式完成状态。
- Phase-10-03 应基于最终合入构建执行阶段级交叉验收与状态收口，不在本批继续扩展事件类型。
