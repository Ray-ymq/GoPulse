# Phase-10-02：采集故障事件与可靠性闭环实施方案

> 权威目标版本与开发分支以 `Phase-10-总实施方案.md` 第 3.2 节为准：本批对应 `1.7.2` / `develop/1.7.2`。
>
> 当前状态：待实施。

## 1. 批次目标

在 Phase-10-01 已合入的插件生命周期 Events 纵向闭环上，接入具有真实运行价值的失败、异常与恢复转换：

```text
Plugin Manager 终态操作失败 / Exporter 非预期退出
MetricsMonitor 采集失败↔恢复 / Redis target unavailable↔recovered
  → episode 去抖与固定安全事件
  → EventMonitor → Router → Kafka → Marshaller
  → Events Elasticsearch → Backend admin 查询
```

本批同时完成 Events 链路的可靠性闭环：持续故障不产生每 scrape 一条的事件洪泛，恢复只在真实完整成功后发生；EventMonitor queue/Router/Kafka 短时故障在容量内可恢复；永久坏 Events 不阻塞后续；Events Store 故障时 offset 不越过，恢复后写入并继续；Metrics、Logs、Events 三类数据能在同 Topic/正式 group/Marshaller 中交替处理。

本批不重做 Phase-10-01 已通过的完整 API、存储和运维基础，只在新增失败词汇和真实故障暴露直接问题时修改对应共享边界。

## 2. 前置条件

- Phase-10-01 已合入最新主远程 `main`，远程门禁成功，根与 Frontend 版本为 `1.7.1`，同名实施记录与真实提交一致。
- 从包含 Phase-10-01 的最新 `upstream/main` 创建 `develop/1.7.2`，不沿用 `develop/1.7.1` 或 `update`。
- 核对 Phase-10-01 实施记录中的真实偏差、已通过检查、message ID/offset/index 证据与限制；相关实现未变化时不机械重跑全部成功排列。
- 在 WSL2 Linux filesystem、唯一 Docker daemon 和随机强归属资源中实施；开始前保存 Git、日常进程/端口、Compose、volume、plugin root、Kafka group/offset、Logs/Events alias 与 VM 时间窗口快照。
- 仅阅读 Plugin Manager 失败/watch/recovery、MetricsMonitor scrape/publish 结果、EventMonitor episode 状态、直接验收脚本和必要回归；不进行一般 Review 或覆盖率扩张。

## 3. 实施范围

### 3.1 插件终态失败与异常退出

- 开放 `exporter_plugin_failed`，只覆盖已进入真实系统转换后的 start/stop/update/recover 终态运行失败；metadata 必须包含 `plugin_id`、可知时的 `plugin_version`、`operation`、`error_code` 和最终 `to_state`。
- 可保存的 Plugin Manager 错误码只能来自总方案固定安全词汇：`start_failed`、`stop_failed`、`update_failed`、`rollback_failed`、`recovery_failed`、`recovery_invalid` 和 `process_exited`；不临时从 wrapped error 推导新码，不保存底层消息。
- invalid package/not found/conflict/in-progress/未授权继续视为请求拒绝而非系统运行事件；它们不进入 Events 索引，防止由客户端反复无效请求造成事件洪泛。
- 非预期插件进程退出在 ownership 确认、process record 清理、MetricsMonitor disable 和 observed state=`failed` 提交后记录 `exporter_plugin_exited`，使用 `error_code=process_exited`。
- 预期 stop/update/shutdown 必须通过现有 intentional 标志或等价机制抑制 exited 事件；老 watcher、ownership mismatch 或已替换 runtime 不得写新状态或事件。
- 恢复期失败如发生在 EventMonitor worker 完全启动之前，通过在 Manager 构造时注入 recorder 或等价初始化顺序保证真实终态可记录；初始化中途失败不留下 goroutine/进程。

### 3.2 Metrics 采集 episode 状态机

- 为 Events 单独建立每 plugin/target 的低基数 episode 状态，不根据 Plugin Status `LastError` 每次赋值直接发送。当前只有 `redis-exporter` / `redis-exporter-local`，不建立通用多租户 key。
- 将每次 scrape 对事件状态机的输入收敛为一个最终结果：采集/解析失败、metrics message 构造失败、publish 失败、已成功发布的 `target_unavailable`，或已成功发布的 `success`。
- 当从无 active failure 进入首个 scrape/parse/message/publish 失败时记录一个 `metrics_collection_failed`；持续失败即使 safe error code 变化，也不在未恢复期间重复发送。
- 只有后续一次完整采集并成功发布 metrics 时记录一个 `metrics_collection_recovered` 并清除 failure episode。采集成功但 publish 失败不先恢复后失败。
- 已成功发布的 `scrape_status=target_unavailable` 从非 unavailable 状态只记录一个 `metrics_target_unavailable`；继续 unavailable 不重复。后续已发布 `success` 只记录一个 `metrics_target_recovered`。
- collection failure 与 target unavailable 是两个独立状态维度；当一次发布失败时，不得根据未成功传输的 target status 发 unavailable/recovered。
- plugin stop/update/disable 取消 scrape 不是 collection failure/recovery；新一次显式 start 后从空 episode 开始，不为停机窗口产生假恢复。
- EventMonitor Record 失败不得回滚 episode 状态、阻塞 scrape 或改变 Plugin Status；事件系统不是 metrics 成功的前置事务。

### 3.3 事件词汇、Marshaller 与查询扩展

- 在 Monitor 与 Marshaller 的单一受版本控制 contract table 中对齐新 event name、severity/message 一对一关系与 metadata 必填/禁止组合。
- `exporter_plugin_failed`、`exporter_plugin_exited` 使用 `severity=error`；`metrics_collection_failed`、`metrics_target_unavailable` 使用 `severity=warn`；两个 recovered 使用 `severity=info`。
- `message` 使用与 event name 固定绑定的英文短句，不拼接原始错误、PID、地址、路径、包名、用户输入或变动文本。
- `error_code`、`operation`、`from_state/to_state` 和 `scrape_status` 仅接受已知词汇；Backend query filters 同步允许可能组合，拒绝不可能的 event/operation/error 组合。
- Events Store mapping 字段集不变；新事件只使用已预留的 strict metadata 字段。如真实实施需要新字段，先更新总方案、template、Monitor/Marshaller validator、Backend DTO/filter 和验收，不改为 dynamic map。

### 3.4 EventMonitor 源端可靠性与恢复

- 使用可控小容量队列证明 queue full 时 Record 立即返回、只丢远程副本、主操作与 metrics 结果不变；持续 full 只记录一个状态，恢复 available 后可再报。
- 在 Router/Kafka 短时不可用时，队首使用同一 message ID 和内容退避重试；容量内后续事件保序，恢复后最终可查。
- 确定性 Router `4xx` 作为当前记录永久失败并继续后续；网络/超时/`429`/`5xx` 不得被误判为永久失败。
- shutdown 与 Record 的线性化和 drain 超时在 race 下可确定验证；超时不泄漏 goroutine，不留下可继续访问已关闭 publisher 的 worker。
- 状态日志不反向进入 EventMonitor，防止 Router/Kafka 故障递归放大。

### 3.5 Kafka/Marshaller/Elasticsearch 可靠性

- 在当前正式 group 与受控 offset 窗口注入一个永久无效 events record，证明 Events Store 零调用、reason code 有限、offset 安全越过，且紧随其后的真实合法事件最终可查。
- 对一个合法事件以相同 key/value 重放，证明目标索引、`_id`、文档 body 确定且 alias count 不增加；不通过修改 ID 制造伪重放。
- 在 events record 已进 Kafka 后停止或替换 Elasticsearch，确认 Marshaller 不越过当前 offset、health/ready 符合契约；恢复后重建/confirm Events template、strict index/read alias，写入并继续后续类型。
- 当 partition ownership 在 Events Store 写入或退避期间丢失，旧 generation 不提交；新 owner 通过同 ID 幂等收敛并推进。
- 不为事件新增 Topic/group/partition，不通过跳过当前暂时失败事件让后续 Metrics/Logs 先行。

### 3.6 Metrics/Logs/Events 三类并存

- 在同一有限 Kafka offset 窗口中，依次产生真实 Redis metrics、Backend API log、插件生命周期 event、target failure/recovery event，最终分别在 VM、Logs alias 和 Events alias 观察。
- 确认三类 handler 仅调用各自 writer，events payload 字段不放宽 metrics/logs validator，Events template 不匹配 Logs/帖子索引。
- 执行 Phase 8 Metrics 代表回归：`success`、`target_unavailable` 与恢复各至少一次，确认新 episode 事件不改变 metrics samples、timestamp、labels、VM write 或 Plugin Status。
- 执行 Phase 9 Logs 代表回归：一个真实 Backend request ID 可查、Logs alias/mapping 保持、日志查询 admin 授权不变；不重复全部后台日志组合。

### 3.7 查询、敏感和业务隔离

- 对新事件词汇更新 Backend filters/DTO 与不可能组合校验，保持既有 PIT/cursor、固定 alias、空结果和 `events_unavailable` 契约。
- 使用无 Cookie、普通用户 Cookie 和 admin Cookie 查询同一故障 episode，结果分别为 `401`、`403`、`200`；前两者 repository 零调用。
- 对 plugin ID/version、safe error code、source/severity/message 执行响应白名单检查；不返回 PID、内部 URL、token、路径、原始 error、堆栈、Envelope 或 Kafka/ES 元数据。
- 在 EventMonitor/Router/Kafka/Events Store 故障窗口执行最低必要的插件 start/stop 和社交业务代表流程；新增 Events 不得改变主响应、registry/state、MySQL/RabbitMQ 事实或 Backend readiness。
- 对用户名、token/Cookie/JWT、内部 URL、服务器路径、底层错误和用户内容放置唯一哨兵，扫描 Monitor stdout、Kafka/ES、Backend 响应和保留验收制品均不得泄漏。

### 3.8 生命周期、验收与文档

- 扩展 `scripts/verify-events.sh --self-test` 的 episode、故障注入、offset/index 和清理参数安全检查，保持无 Docker 环境下可执行自检。
- 扩展真实 `verify-events.sh` 以受控方式产生持续失败/恢复、unexpected exit、queue/transport 故障、Events Store 故障、永久坏 record、同 ID 重放与三类消息交错证据。
- 验收脚本不直接写合法 ES 文档代替真实源；手工 Kafka record 仅用于永久坏输入和同 ID 重放边界。
- 对齐 Monitor/Marshaller/Backend/Events README、词汇、episode 语义、best-effort/at-least-once 限制、单 Topic 有序阻塞和 Phase 11 使用注意。
- 更新 CI Events pipeline，不放宽 Metrics/Logs 门禁；完成同名实施记录，同步根与 Frontend 版本为 `1.7.2`。

## 4. 实施边界与非目标

- 不新增成功生命周期 event name、公共 endpoint、query 参数、index/alias、Topic/group 或第三方事件源。
- 不将每次 scrape、每次 retry、每个 invalid admin 请求或 EventMonitor 自身传输失败记录为 Events。
- 不实现 Frontend、告警、通知、聚合、复杂关联、Kubernetes 事件、ILM、spool、优先级队列或 exactly-once。
- 不开展全仓 Review、通用稳定性加固、长时压测或容量评估；只修复固定矩阵复现的直接阻断问题。
- 不修改冻结 PowerShell，不增加原生 Windows/Windows runner 验收，不创建应用镜像。

## 5. 预计文件与交付物

```text
monitor/internal/events/**
monitor/internal/plugin/**
monitor/internal/metrics/collector/**
monitor/internal/config/**
monitor/cmd/monitor/**
monitor/README.md
marshaller/internal/events/**（词汇扩展）
backend/internal/eventquery/**（词汇/过滤扩展）
scripts/verify-events.sh
scripts/verify-monitor.sh（仅必要回归）
scripts/verify-marshaller.sh（仅三类并存或故障边界）
scripts/verify-logs.sh（仅 Logs 代表回归）
scripts/verify-business.sh（仅故障窗口业务回归）
.github/workflows/quality-gates.yml（仅 Events job）
README.md
marshaller/README.md
dev/logs/Phase-10/Phase-10-02-采集故障事件与可靠性闭环.md
dev/imple/Phase-10/Phase-10-总实施方案.md（仅状态/真实偏差）
VERSION
frontend/package.json
frontend/package-lock.json
```

若 Phase-10-01 已将相同契约文档化且无改变，本批不为形式完整重复修改。预计文件之外的修改必须由固定验收失败直接触发，并在实施记录中写明原因。

## 6. 详细实施步骤

1. fetch 最新 `main`，确认 Phase-10-01 合入、门禁、版本、实施记录和已知限制，创建 `develop/1.7.2` 并保存环境快照。
2. 先在 Plugin Manager 接入终态失败与 unexpected exit，用代表性成功/失败/预期退出测试固定时序、词汇与主结果隔离。
3. 将 MetricsMonitor 事件输入收敛为每 scrape 一个最终结果，实现 collection 与 target 两个 episode 维度，通过持续失败、发布失败、恢复和 stop/restart 测试。
4. 对齐 Monitor/Marshaller/Backend 事件词汇与 metadata/query 组合，不增加任意字段。
5. 先执行受影响 package 的最小单元/race 测试，修复直接失败；不为了覆盖率穷举错误码排列。
6. 扩展 `verify-events.sh` self-test 与真实故障矩阵，执行失败/恢复、去抖、queue/transport、永久坏 record、重放、ES 恢复和三类消息并存。
7. 在故障窗口执行最低必要插件与社交业务回归，扫描敏感哨兵并对比资源快照。
8. 更新直接 README/CI/验收契约，不重写未变更的 Phase-10-01 内容。
9. 在最终 diff 上完成第 8 节固定门禁，同步版本 `1.7.2`，如实完成同名实施记录。
10. 仅暂存本批文件并提交；push、创建 PR，查询真实远程 checks 和合入状态。

## 7. 风险与控制

- **状态回调产生 recovered/failed 震荡**：只向 episode 状态机报告一个最终 scrape 结果，publish 失败前的中间成功不触发恢复。
- **持续故障形成事件洪泛**：每个 episode 只产生首次失败与首次真实恢复，不按错误码跳变重新开 episode。
- **预期停止被误记为异常退出**：固定 intentional/ownership/runtime identity 检查顺序，以受控 stop/update/shutdown 回归证明。
- **事件链路故障影响主链**：Record 与 episode 更新不等待网络，记录失败不改变 plugin status、metrics result 或 API 响应。
- **事件报告 Router 失败形成递归**：EventMonitor 运输日志永远 stdout-only，不调用 recorder。
- **同 Topic 阻塞被误判为丢失**：观察 committed offset 与恢复后顺序，不要求 ES 故障时后继 Metrics/Logs 越过当前 Events record。
- **故障注入误伤日常资源**：每次停止/替换前验证随机 project、container ID、PID、port、volume、plugin root 和 group 归属，结束后对比快照。

## 8. 固定验证命令与必要回归

最终 diff 稳定后执行：

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin ./internal/metrics/collector)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./internal/events ./internal/elasticsearch ./internal/consumer)
(cd backend && go test -count=1 ./internal/eventquery ./internal/http/...)
(cd backend && go vet ./internal/eventquery ./internal/http/...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-events.sh \
  scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh \
  scripts/verify-logs.sh scripts/verify-business.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-events.sh --self-test
scripts/verify-events.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-logs.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

- 本批真实 Events 验收必须覆盖至少一个采集失败/恢复 episode、一个 target unavailable/recovered episode 和一个 unexpected exit，且事件均由真实系统行为产生。
- 为证明三类并存，在受控有限窗口执行 Metrics 成功/unavailable/恢复、一个 Backend request log 和 Events 查询；不扩展为全部 Phase 8/9 矩阵。
- 已成功且未变化的 Phase-10-01 详细 API/cursor 排列可引用实施记录；新增词汇、故障、去抖和三类并存项必须实际运行。

## 9. 批次验收标准

- 插件终态操作失败与非预期退出在状态提交后产生固定安全事件，预期 stop/update/shutdown 不生成 exited。
- 持续 collection failure 只有一个 failed，完整发布成功后只有一个 recovered；publish 失败不产生中间 recovered。
- Redis target unavailable/recovered 只在对应 status metrics 成功发布后记录，与 collection failure episode 职责清晰，持续 scrape 不洪泛。
- EventMonitor queue full/短时 Router/Kafka 故障保持非阻塞与有界，容量内恢复；状态日志受节流且不递归生成 Events。
- 永久坏 event 不写 ES 且后续真实事件可查；同 ID 重放只有一个文档；Events Store 故障/集群替换时 offset 不越过，恢复后合同与顺序正确。
- Metrics/Logs/Events 交替处理并只写各自存储，既有 Metrics 与 Logs 代表性主链、validator、alias/mapping 和 admin 授权不回归。
- 新事件可按已知词汇由 admin 查询，未登录/普通用户保持 `401/403` 与 repository 零调用，响应及存储不泄漏敏感哨兵或内部字段。
- Events 故障窗口不改变插件操作、Plugin Status、Metrics 结果、社交业务事实或 Backend readiness；资源前后快照无误伤/遗留。
- 固定门禁通过，同名实施记录真实完整，根与 Frontend 版本均为 `1.7.2`。

## 10. 明确完成条件

只有第 9 节全部满足、Phase-10-02 Pull Request 已合入主远程 `main`、远程固定门禁成功，且 `dev/logs/Phase-10/Phase-10-02-采集故障事件与可靠性闭环.md` 与真实提交一致，本批才完成。任一 failure/recovery 真实源、episode 去抖、故障恢复、offset/ownership、三类并存、权限、敏感或资源证据缺失时不得标记完成。

完成后立即停止功能扩展。Phase-10-03 只在本批最终构建上做阶段级交叉验收和状态收口，不补计划外功能。

## 11. Phase-10-03 交接

- 已合入的全部生命周期成功、运行失败、unexpected exit、collection failure/recovery 和 target unavailable/recovered 事件词汇。
- episode 去抖、最终 scrape 结果、非阻塞源端、queue/transport 恢复与 best-effort 边界的真实证据。
- Events 永久异常继续、同 ID 幂等、ES 故障恢复、group ownership/offset 和三类消息并存证据。
- 有限 admin 查询、脱敏、业务隔离、资源安全和日常 Bash 生命周期入口。
