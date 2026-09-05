# Phase-10-03：集成验收与阶段收口实施方案

> 权威目标版本与开发分支以 `Phase-10-总实施方案.md` 第 3.2 节为准：本批对应 `1.7.3` / `develop/1.7.3`。
>
> 当前状态：待实施。

## 1. 批次目标

在 Phase-10-01 与 Phase-10-02 已合入的最终能力上执行 Phase 10 封闭集成矩阵，证明真实插件生命周期、运行失败、异常退出、Metrics 采集失败/恢复与 Redis target unavailable/recovered 事件能经 EventMonitor、Router、Kafka、Marshaller 进入独立 Elasticsearch 索引，并由 Backend 在实时 admin 授权后受限查询；同时证明 episode 去抖、Metrics/Logs/Events 并存、索引隔离、故障恢复、业务隔离和资源安全。

本批是验收与状态收口批次，不是第三个功能实现批次。除固定矩阵真实复现且阻断 Phase 10 验收的问题外，不修改 Events API、payload/Envelope、event 词汇、mapping、Topic/group、去抖或发送语义，不增加 Frontend、告警、Kubernetes 事件、聚合或生产加固。

## 2. 前置条件

- Phase-10-01 与 Phase-10-02 已合入最新主远程 `main`，各自远程门禁成功，根与 Frontend 版本为 `1.7.2`，两份实施记录与真实提交一致。
- 从包含全部 Phase 10 前置能力的最新 `upstream/main` 创建 `develop/1.7.3`，不沿用前两批分支或 `update`。
- 已核对两批实施记录中的真实偏差、已通过检查、环境、event/message ID、offset/index 证据与已知限制；相关实现未变化时不机械重跑组件排列。
- 验收在 WSL2 Linux filesystem、唯一 Docker daemon、随机 Compose project/loopback 端口/凭据/plugin root/volume 与可强归属临时资源中运行。
- 开始前保存 Git、日常进程/端口、Compose project/container/network/volume、Kafka group/offset、PIT、插件根、Logs/Events index/alias 与 VM 时间窗口快照。
- 如前置批次未合入、远程失败、版本不一致或关键验收缺失，先回到对应批次处理，不用本收口批次掩盖。

## 3. 收口范围

### 3.1 最终静态与组件门禁

- 在最终提交上执行 Backend、Monitor、Router、Marshaller 的 gofmt/unit/vet 与直接受影响 package race。
- 执行脚本 CI unittest、版本/分支治理、Bash 语法、LF、Compose 渲染、固定 loopback、token/index/alias/Topic/group 和资源归属检查。
- 执行 `verify-events.sh --self-test`，证明不安全配置、端口、token、index/alias、plugin root、PID、Compose project/container/volume、Kafka group 和清理目标被拒绝。
- 核对 Phase-10-01/02 已通过且未变化的 Record/Close 线性化、queue/jitter、validator、cursor 和 ownership 单元证据；只有相关代码、配置、依赖或环境变化时重跑对应项。

### 3.2 真实插件生命周期事件闭环

- 在全新随机 ES/Kafka 资源中启动日常等价栈，记录 Events/Logs/帖子 alias 初始状态、Kafka 正式 group 起点与 VM 查询时间窗。
- 通过真实注册、admin 提升/登录和 Backend 插件 API 安装可验证 Redis Exporter package，由 Backend Events API 查询唯一 `exporter_plugin_installed`。
- 依次执行 stop 和 start，查询对应事件，核对真实 observed state、时间顺序、plugin ID/version、operation、from/to state 与固定 severity/message。
- 使用更高 SemVer 的真实 package 执行 update，确认只有一个 `exporter_plugin_updated`，metadata 中新旧版本正确，内部 stop/start 不另产生事件。
- 对已 running start no-op、一个被拒绝无效 package/operation 和 Monitor shutdown 分别检查事件窗，证明不制造假生命周期 Events。
- 所有成功事件都必须由真实操作经全链产生；禁止直接 ES index、修改 registry 或伪造 EventMonitor callback 代替源证据。

### 3.3 真实失败、异常与恢复事件闭环

- 选择一个可控且不损坏日常资源的 Plugin Manager 终态失败，核对 `exporter_plugin_failed` 中 operation/error/to state 与 API/Plugin Status 真实一致，不使用 invalid request 充当运行失败。
- 对当前 ownership 匹配的 Exporter 执行受控非预期退出，查询唯一 `exporter_plugin_exited`，同时核对 observed state=`failed`、MetricsMonitor disable 和进程记录清理。
- 通过真实断开 Redis/Exporter 目标或等价受控方式产生 `metrics_target_unavailable`，保持多个 scrape 周期后仍只有一条；恢复真实 target 后只有一条 `metrics_target_recovered`。
- 产生一个 scrape/parse 或 publish 失败 episode，持续至少两个观察周期，确认只有一条 `metrics_collection_failed`；完整 scrape + metrics publish 成功后只有一条 `metrics_collection_recovered`。
- 对“采集成功但 metrics publish 失败”执行一个代表窗口，确认窗口内不先出现 recovered；EventMonitor 使用同一 Router 时的重试/延迟边界如实记录。

### 3.4 查询权限、范围与分页

- 对同一 Events 查询分别使用无 Cookie、普通用户 Cookie、admin Cookie，结果固定为 `401 authentication_required`、`403 permission_denied`、`200`；前两者 repository/ES 调用为零。
- 每类选择一个代表验证 from/to、source、event name、severity、plugin ID、operation、error code 与 limit；未知、重复、空、超长、通配、不可能组合和超 24h 范围返回 `400 validation_failed`。
- 产生超过一页的有限事件，证明 PIT 内排序稳定、续页无重复/遗漏、next cursor 固化首页时间窗与 filters；篡改、过期、与其他参数混用和 PIT 失效安全失败。
- 空 alias/空时间窗返回空页；ES 不可用返回 `503 events_unavailable`，不回显 URL、index/alias、PIT、DSL、响应 body 或底层错误。
- 响应只含 canonical DTO；扫描禁止 `_index`、`_id`、`_score`、message ID、Kafka metadata、Envelope、PID、token、路径、原始错误和未知 metadata。

### 3.5 索引、严格映射与幂等

- Events template 只匹配 `gopulse-events-v1-*`，物理索引按 Envelope UTC 日期，固定 read alias 完整覆盖当前 Events 索引。
- 根文档和 metadata 对象使用 strict mapping，字段/类型与总方案一致；未知字段被拒绝。
- 同一合法 Kafka key/value 重放后 Events alias count 不增加，文档内容确定，offset 最终推进。
- 一个受控永久无效 events record 不产生 ES 文档，安全越过后真实合法事件继续；不在端到端层穷举 validator 全集。
- Logs alias/index/template/mapping、帖子 alias/索引/搜索/reindex 保持不变；Events query 不能指定他类 alias，他类 repository 不能访问 Events prefix。

### 3.6 三类数据并存与故障恢复

- 在同一有限 Kafka offset 窗口交替产生真实 Redis metrics、Backend API log、生命周期 event 和 failure/recovery event，最终 VM、Logs alias、Events alias 分别出现预期结果且无互写。
- 执行一个源端/传输故障窗口：停止 Router/Kafka 或受控阻塞 publisher，继续代表性 plugin/metrics 操作，确认主结果与队列有界；恢复后容量内 Events 最终可查。
- 执行一个存储故障窗口：Events record 已进 Kafka 后停止/替换 ES，确认正式 group offset 不越过、Marshaller health/ready 符合契约；恢复后重建/confirm template/mapping/alias，写入、提交并继续后续 Metrics/Logs。
- 引用或在变化时重跑 Phase-10-02 的 Marshaller 重启/ownership、同 ID 重放与永久异常证据；不通过手工提交 offset 或直接写 ES 恢复。
- 执行 Phase 8 Metrics 真实代表回归和 Phase 9 Backend request log 真实代表回归，确认 Events 扩展不改变两类 validator、writer、alias/mapping、查询或恢复语义。

### 3.7 业务、内部访问与敏感信息隔离

- 在 Events 源端/传输/存储故障窗口执行代表性注册/登录、帖子、评论或首次点赞、通知与搜索必要流程；新增 Events 不改变 MySQL/RabbitMQ 事实、公共状态码或授权。
- 明确 ES 故障仍会触发 Phase 3 既有 Backend readiness/search 退化；本批只要求非搜索社交操作不因 Events 新增额外失败。
- Router 只接受正确内部 Bearer 服务身份；Monitor 管理 token、Log ingest token、普通/admin Cookie 与 JWT 都不可互换。
- Elasticsearch、Kafka、Monitor、Router、Marshaller、VictoriaMetrics 端口只在 loopback/受控网络；Frontend bundle 和 HTTP 响应不含内部 URL 或凭据。
- 在用户名、密码、Cookie/JWT、帖子/评论、搜索词、内部 URL、绝对路径和底层错误中放置唯一哨兵，扫描 stdout、Kafka/ES、Backend 响应、Frontend bundle 和保留验收制品均无泄漏。

### 3.8 生命周期、资源与远程状态

- 在隔离配置中执行 `dev.sh → verify.sh → down.sh`，核对基础设施 → Router → Marshaller → Monitor/EventMonitor → Backend 的启动顺序和反向有界关闭。
- `verify.sh` 必须只读：不制造 Events、不安装/启停插件、不创建 template/index、不写 Kafka/ES、不提交 offset、不打开无法关闭的 PIT、不修改用户角色。
- 正常、故障、验收失败、signal 和脚本中断路径前后对比 Git、PID、端口、project/container/network/volume、Kafka group/offset、PIT、plugin root、日志文件与临时 token。
- 只清理本批随机强归属资源；unknown/mismatched 资源安全拒绝，日常命名 ES/Kafka/VM volume 必须保留。
- 更新 README、契约文档、总/拆分方案状态与本批实施记录；本地通过、push、PR、远程 checks 和合入分别如实记录。

## 4. 阻断问题修复边界

只允许修复以下会直接使固定矩阵不成立的问题：

- 真实生命周期/失败/恢复事件无法经必需组件或无法由 admin 查询。
- episode 去抖错误、恢复误报、预期退出误报、queue 无界或事件失败改变插件/metrics 主结果。
- offset 在写入/合同验证前推进、永久坏 Events 阻塞、同 ID 重复文档、ownership 失效后错提，或 Metrics/Logs 契约回归。
- `401/403` 后仍访问 ES、查询越界、索引混写、任意 metadata 渗入或敏感信息泄漏。
- Events 故障改变业务事实、进程失控、生命周期误杀/误删或验收资源遗留。
- 版本、分支、CI、README、总/拆分方案或实施记录与最终实现不一致。

修复前保存最小复现与有限诊断；修复后只重跑受影响 package、脚本或场景。非阻断的性能、体验、容量、可维护性和未来功能记录为后续项，不延长本批。

## 5. 实施边界与非目标

- 不新增事件源、event name、公共 query 参数、响应字段、HTTP endpoint、Envelope 字段、Topic/group、index 或 mapping 能力。
- 不实现 Frontend Events 页、告警/通知、聚合、全文检索、复杂关联、Kubernetes 事件、ILM、事件删除、spool 或 exactly-once。
- 不开展一般代码 Review、依赖审计、覆盖率活动、长时压力或容量测试；只有用户另行明确请求时执行。
- 不修改冻结 PowerShell，不增加原生 Windows/Windows runner，不创建应用容器镜像。

## 6. 预计文件与交付物

```text
dev/imple/Phase-10/Phase-10-总实施方案.md（仅最终状态/真实偏差）
dev/logs/Phase-10/Phase-10-03-集成验收与阶段收口.md
README.md
monitor/README.md
router/README.md
marshaller/README.md
.env.example（仅文档/阻断修复）
scripts/verify-events.sh（验收编排或阻断修复）
scripts/verify-business.sh（仅交叉故障窗口）
scripts/verify-marshaller.sh（仅三类并存阻断修复）
scripts/verify-logs.sh（仅 Logs 回归阻断修复）
scripts/dev.sh（仅阻断修复）
scripts/down.sh（仅阻断修复）
scripts/verify.sh（仅阻断修复）
backend/**（仅阻断修复）
monitor/**（仅阻断修复）
router/**（仅阻断修复）
marshaller/**（仅阻断修复）
deploy/compose.yaml（仅阻断修复）
.github/workflows/quality-gates.yml（仅门禁阻断修复）
scripts/ci/**（仅治理阻断修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义修改。如果固定验收未暴露产品问题，本批应主要包含验收编排/证据、文档、版本和实施记录。

## 7. 详细实施步骤

1. fetch 最新 `main`，核对 10-01/02 实施记录、合入提交、远程 checks、当前版本与已知限制，创建 `develop/1.7.3` 并保存资源快照。
2. 在最终构建上执行第 3.1 节静态、直接模块、脚本、Compose 和 self-test 门禁，记录可引用的前批成功检查与未变化依据。
3. 执行第 3.2 节真实 install/start/stop/update 事件闭环与 no-op/拒绝/shutdown 负向检查。
4. 执行第 3.3 节真实终态失败、unexpected exit、collection failure/recovery 和 target unavailable/recovered 去抖矩阵。
5. 执行第 3.4 节用户态、filters、PIT/cursor、空结果和存储错误查询矩阵。
6. 执行第 3.5 节 template/index/alias、strict root/metadata mapping、同 ID 重放、永久异常与 Logs/帖子索引隔离。
7. 执行第 3.6 节三类消息交替、代表性源端/传输/存储故障恢复、Metrics 和 Logs 真实代表回归。
8. 在故障窗口执行第 3.7 节业务/内部访问/敏感哨兵检查，不额外重复所有业务排列。
9. 执行日常等价生命周期、verify 只读性与正常/失败/中断资源前后快照。
10. 只对真实阻断失败做有限修复，相关代码或环境改变后只重跑受影响项。
11. 最终 diff 稳定后完成第 9 节尚未通过的固定门禁，更新 README、总/拆分方案状态、版本 `1.7.3` 和本批实施记录。
12. 仅暂存本批文件并提交；push、创建 PR，查询真实远程 checks 与合入状态。
13. 合入且远程门禁成功后标记 Phase 10 完成并立即停止，把稳定契约交给 Phase 11。

## 8. 风险与控制

- **旧 ES/VM 数据形成假通过**：使用随机 volume、当前 offset、真实插件版本、唯一 event/message ID 和窄 UTC 窗口，不直接写存储充当源证据。
- **持续故障观察时间无界**：使用可配置的最小合法 scrape interval 与固定有限超时，只观察证明一个失败和一个恢复所需的代表窗口。
- **可观测同路故障让失败事件延迟**：区分事件发生时间与可查时间，核对恢复后队列顺序；不要求 Router/Kafka 不可用时立即查到同路失败事件。
- **收口机械重跑扩大成本**：前批未变化成功项引用实施记录，只实际运行阶段交叉事实和最终固定门禁。
- **故障窗口误伤业务/日常栈**：停止或替换前验证随机 project、container ID、PID、port、volume、group 和 plugin root，结束后对比快照。
- **同 Topic 阻塞被误判丢失**：观察 committed offset 和恢复后顺序，不要求 ES 故障时后继类型越过当前 Events record。
- **admin 权限掩盖敏感返回**：admin 响应仍执行 DTO/metadata 白名单与哨兵扫描，内部字段对 admin 同样禁止。
- **虚构远程完成**：本地结果、push、PR、checks 和 merge 分开记录，未观察即不写完成。
- **范围扩张**：固定矩阵通过后停止，Frontend、告警、Kubernetes Events、ILM、spool、Topic 拆分转后续。

## 9. 固定验证命令与必要回归

最终 diff 上按影响执行；阶段交叉矩阵、治理与远程门禁必须实际完成：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/eventquery ./internal/http/...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin ./internal/metrics/collector)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./internal/events ./internal/elasticsearch ./internal/consumer)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/verify-events.sh \
  scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-events.sh --self-test
scripts/verify-events.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-logs.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

完整验收只在 WSL2 Linux filesystem、真实 Kafka/Elasticsearch/VictoriaMetrics 和随机强归属资源中标记通过。环境缺失时不得标记完成；mock、静态 Envelope、直接 Kafka produce 或直接 ES index 仅能用于最低层异常，不代替真实主链路。

## 10. 阶段收口验收标准

- 真实 install/start/stop/update 成功转换可查，no-op/拒绝/shutdown 不产生假事件；真实终态失败、unexpected exit、collection failure/recovery 和 target unavailable/recovered 符合固定词汇。
- 持续故障按 episode 去抖，恢复只在完整成功后发生；EventMonitor 非阻塞且有界，短时故障容量内恢复，长故障/best-effort 限制准确记录。
- EventMonitor、Router、Kafka、Marshaller、Elasticsearch 全链有真实证据；合法写入与索引合同成立后才提交，坏消息继续，同 ID 重放无重复文档。
- `gopulse-events-v1-*`/read alias/root+metadata strict mapping 与 Logs/帖子索引完全隔离，空集群替换/存储故障恢复时不提前 commit。
- 未登录 `401`、普通用户 `403`、admin 受限查询成功；拒绝请求不访问 ES，filter/cursor/范围/空结果/`503` 均符合契约。
- Metrics/Logs/Events 在同 Topic/正式 group/Marshaller 中并存并只写各自存储；Phase 8/9 真实代表链路、validator、mapping/alias、查询和故障恢复无回归。
- Events 故障不改变插件管理、Metrics 主结果、非搜索社交业务、RabbitMQ 必要事实或既有权限；ES 对 search/readiness 的既有影响被准确区分。
- 内部身份不可互换，端口保持 loopback，stdout、Kafka/ES、Backend 响应、Frontend bundle 和验收制品无敏感哨兵或内部字段。
- dev/verify/down、verify 只读性和失败/中断清理不误杀、不误删、不遗留进程、端口、container/network/volume、group、PIT、plugin root、日志或 token。
- 总/拆分方案、README、三份实施记录、Git 历史与远程状态一致；固定门禁通过，根与 Frontend 版本均为 `1.7.3`。

## 11. 明确完成条件

只有第 10 节全部满足、Phase-10-03 Pull Request 已合入主远程 `main`、远程固定门禁成功，且三份 Phase 10 实施记录真实完整，Phase 10 才完成。任一真实事件源、完整传输、episode 去抖、offset/ownership、查询授权、索引隔离、重放、三类并存、业务/敏感/资源安全或远程证据缺失时不得标记完成。

达到条件后立即停止，不在本批追加 Review、Frontend、告警、聚合、Kubernetes Events、ILM、spool、Topic 拆分或容量优化。后续独立 Review 只有在用户明确请求时进行，并按真实 finding 另行规划整改批次。

## 12. Phase 11 交接

- `GET /api/v1/observability/events` 的实时 admin 授权、有限 filters、PIT 签名 cursor、安全 DTO、空结果和 `events_unavailable` 契约。
- event name/source/severity/message/metadata 的固定 v1 词汇，以及生命周期、失败/恢复、unexpected exit 和 episode 去抖语义。
- Events 索引隔离、内部访问限制和 Frontend 只能经 Backend 读取的安全边界。
- Router `202` 前源端有界 best-effort、Kafka 接受后 at-least-once、同 ID 幂等与同 Topic 有序 backpressure 的准确产品语义；UI 不得将缺失事件解释为系统行为绝对未发生。

Phase 10 完成时三类后端可观测链路已齐备，但 Milestone 3 仍等待 Phase 11 统一管理员前端；本批不单独宣称“完整可观测 MVP”完成。
