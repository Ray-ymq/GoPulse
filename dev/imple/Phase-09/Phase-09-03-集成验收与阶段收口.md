# Phase-09-03：集成验收与阶段收口实施方案

> 权威目标版本与开发分支以 `Phase-09-总实施方案.md` 第 3.2 节为准：本批对应 `1.6.3` / `develop/1.6.3`。
>
> 当前状态：本地验收完成（2026-09-04），待远程门禁与合入。

## 1. 批次目标

在 Phase-09-01 与 Phase-09-02 已合入的最终能力上执行 Phase 9 封闭集成矩阵，证明真实 API 与后台日志可以经 LogMonitor、Router、Kafka、Marshaller进入独立 Elasticsearch日志索引，并由Backend在实时admin授权后受限查询；同时证明 Metrics并存、帖子索引隔离、业务故障隔离和资源安全。

本批是验收与状态收口批次，不是第三个功能实现批次。除固定矩阵真实复现且阻断 Phase 9 验收的问题外，不修改公共API、Envelope、mapping、发送语义、Topic/group或查询能力，不增加Frontend、Events、告警、全文分析、ILM或生产加固。

## 2. 前置条件

- Phase-09-01 与 Phase-09-02 已合入最新主远程 `main`，各自远程门禁成功，根与 Frontend版本为 `1.6.2`，两份实施记录与真实提交一致。
- 从包含全部 Phase 9前置能力的最新 `upstream/main` 创建 `develop/1.6.3`，不沿用前两批分支。
- 已核对两批实施记录中的真实偏差、已通过检查、环境、message ID/offset/index证据和未完成限制；相关实现未变化时不机械重跑组件排列。
- 验收在WSL2 Linux filesystem、唯一Docker daemon和随机强归属资源上执行；开始前保存Git、日常进程、端口、Compose、volume、Kafka group、插件根、日志文件与Frontend配置快照。
- 如前置批次有未合入、远程失败、版本不一致或关键验收缺失，先回到对应批次处理，不用本收口批次掩盖。

## 3. 收口范围

### 3.1 最终静态与组件门禁

- 在最终提交上执行 Backend、Monitor、Router、Marshaller的gofmt/unit/vet/race以及直接受影响integration tests。
- 执行脚本CI unittest、版本与分支校验、Bash语法、LF、Compose渲染、固定loopback、token/index/alias/Topic/group和资源归属检查。
- 执行 `verify-logs.sh --self-test`，证明所有不安全token、URL、queue、body、query、index、port、PID、project/container/volume和清理目标被拒绝。
- 核对 Phase-09-01/02已通过且未变化的精确queue/commit/ownership/HTTP竞态测试；只有相关代码、配置、依赖或环境改变时重跑受影响场景。

### 3.2 真实请求日志闭环

- 在全新随机ES/VM/Kafka环境启动日常等价栈，记录日志alias初始状态、帖子alias、Kafka正式group起点与VM查询时间窗。
- 通过真实注册、admin提升/登录和状态变更API产生 `service=backend` 日志；以响应 `X-Request-ID` 查询唯一HTTP完成日志及对应业务成功日志。
- 核对 status、level、method、route template、duration/response bytes、user/resource ID和error_code的类型/省略规则；查询不得命中旧volume或fixture数据。
- 选一个真实4xx和一个真实5xx/安全故障的最低必要代表，证明日志等级和公共error_code正确，响应与ES均不含底层错误或请求内容。

### 3.3 真实后台日志闭环

- 真实发帖产生 `post.created`，通过event ID查询Backend Outbox发布与Search Indexer成功/恢复日志，帖子搜索最终事实正确。
- 真实评论或首次点赞产生通知事件，通过event ID查询Backend Outbox与Business Worker处理日志，通知最终事实正确。
- 执行一次search-reindex，查询start与completed/skipped日志，命令退出结果、帖子alias和document count相符。
- 核对request ID不被伪造为跨进程关联键，event ID不暴露AMQP payload或用户内容；各service/module/message命中固定词汇。

### 3.4 查询权限、范围与分页

- 对同一日志查询分别使用无Cookie、普通用户Cookie、admin Cookie：结果固定为 `401 authentication_required`、`403 permission_denied`、`200`；前两者repository调用为零。
- 逐类选择一个代表验证 `from/to`、service、module、level、message、request ID、event ID、error code与limit；未知、重复、空、超长、通配符和超24h范围返回 `400 validation_failed`。
- 产生超过一页的有限日志，证明排序固定、PIT内分页无重复/遗漏、next cursor固化首次请求的实际时间窗与filters；篡改、过期、与其他参数混用和PIT失效安全失败。
- 空alias/空时间窗返回空页；ES不可用返回 `503 logs_unavailable`，不回显URL、index、PIT、DSL或响应body。
- 响应只含canonical DTO；扫描禁止 `_index`、`_id`、`_score`、message ID、Kafka metadata、Envelope、token、路径和未知字段。

### 3.5 索引与存储隔离

- 日志template只匹配 `gopulse-logs-v1-*`，物理索引按Envelope UTC日期，固定read alias完整覆盖当前日志索引。
- 日志写入只使用message ID作为 `_id`；同一合法record原样重放后count不增加、内容确定，commit最终推进。
- 一个受控永久无效logs record不产生ES文档，offset越过后真实合法日志继续；不在端到端层枚举validator全集。
- 帖子alias `gopulse-post-search-v1`、物理索引、mapping、搜索结果和reindex行为保持不变；日志查询不能指定帖子alias，帖子repository不能访问日志prefix。
- strict mapping拒绝未知字段；验收不直接写合法日志代替Marshaller，也不删除日常索引以制造干净环境。

### 3.6 Metrics/Logs 并存与故障恢复

- 在同一有限Kafka offset窗口交替产生真实Redis metrics、Backend API日志和后台日志；最终VM与ES分别出现预期结果且无互写。
- 执行一个接收/传输故障窗口：停止Monitor或Router/Kafka，继续代表性业务，确认stdout与业务事实成立、源队列有界；恢复后容量内记录最终可查。
- 执行一个存储故障窗口：在logs record已进入Kafka后停止ES，确认正式group offset不越过、Marshaller health/ready符合合同；恢复后写入/提交并继续后继metrics。
- 引用或在变化时重跑 Phase-09-02 的Marshaller重启、同ID重放和永久异常继续证据；不得通过手工提交offset或直接写ES恢复。
- 执行 Phase 8真实Metrics代表回归：success、target_unavailable与恢复至少各一次，确认logs扩展没有放宽metrics validator、改变标签/时间或破坏VM查询。

### 3.7 业务、内部访问与敏感信息隔离

- 在可观测故障窗口执行代表性注册/登录、帖子、评论、点赞、通知与搜索必要流程；新增日志代码不改变MySQL/RabbitMQ事实、公共状态码或授权。
- 明确记录ES故障仍触发Phase 3既有Backend readiness/search退化；本批只要求非搜索社交操作不因日志链路增加新的失败。
- LogMonitor分别拒绝无/错token、Monitor admin token、普通/admin Cookie与JWT；Router/Marshaller同样只接受各自内部token。
- Elasticsearch、Kafka、Monitor、Router、Marshaller、VictoriaMetrics端口只loopback/受控网络；Frontend bundle和HTTP响应不含内部URL或凭据。
- 在用户名、密码、Cookie/JWT、帖子/评论、搜索词、内部URL、绝对路径和底层错误中放置唯一哨兵，扫描所有应用stdout、远程ES文档、Backend响应和保留验收制品均无泄漏。

### 3.8 生命周期、资源与远程状态

- 在隔离配置执行 `dev.sh → verify.sh → down.sh`，核对基础设施 → Router → Marshaller → Monitor →日志源的启动，以及日志源drain → Monitor → Marshaller → Router →基础设施的关闭。
- `verify.sh` 必须只读，不创建template/index、产生日志、写Kafka、提交offset、打开无法关闭的PIT、修改用户角色或修复资源。
- 正常、故障、验收失败、signal和脚本中断路径对比前后Git、PID、端口、project/container/network/volume、Kafka group、PIT、插件根、日志文件和临时token快照。
- 只清理本批随机强归属资源；unknown/mismatched资源安全拒绝。日常命名Elasticsearch/Kafka/VM volume必须保留。
- 更新README、HTTP契约、总/拆分方案状态与本批实施记录；本地通过、push、PR、远程checks和合入分别如实记录。

## 4. 阻断问题修复边界

只允许修复以下会直接使固定矩阵不成立的问题：

- 真实日志无法经过某一必需组件或无法由admin查询。
- offset在写入前推进、永久异常阻断、同ID产生重复文档或metrics契约回归。
- `401/403` 后仍访问ES、查询越界、索引混写或敏感数据泄漏。
- 日志故障改变业务事实、进程失控、生命周期误杀/误删或验收资源残留。
- 版本、分支、CI或文档与最终实现不一致。

修复前保存最小复现和有限诊断；修复后只重跑受影响package、脚本或场景。非阻断的性能、体验、容量、可维护性和未来功能记录为后续项，不延长本批。

## 5. 实施边界与非目标

- 不新增日志源、公共query参数、响应字段、HTTP endpoint、Envelope字段、Topic/group、index或mapping能力。
- 不实现Frontend日志页、EventMonitor、告警、聚合、全文检索、ILM、日志删除、磁盘spool或零丢失。
- 不开展一般代码Review、依赖审计、覆盖率活动、长时压力或容量测试；只有用户另行明确请求时进行。
- 不修改冻结PowerShell，不增加原生Windows/Windows runner，不创建应用容器镜像。

## 6. 预计文件与交付物

```text
dev/imple/Phase-09/Phase-09-总实施方案.md（仅最终状态/真实偏差同步）
dev/logs/Phase-09/Phase-09-03-集成验收与阶段收口.md
README.md
monitor/README.md
router/README.md
marshaller/README.md
.env.example（仅文档/阻断修复）
scripts/verify-logs.sh（验收编排或阻断修复）
scripts/verify-business.sh（仅交叉故障窗口）
scripts/verify-marshaller.sh（仅Metrics并存阻断修复）
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

预计文件是允许边界，不要求制造无意义修改。若固定验收未暴露产品问题，本批应主要包含验收编排/证据、文档、版本和实施记录。

## 7. 详细实施步骤

1. fetch最新main，核对09-01/02实施记录、合入提交、远程checks、当前版本与已知限制，创建 `develop/1.6.3` 并保存资源快照。
2. 在最终构建上执行直接模块、脚本、Compose和self-test静态门禁；记录可引用的前批成功检查及其未变化依据。
3. 执行第3.2节真实API request ID日志链路和字段/错误代表检查。
4. 执行第3.3节发帖、评论/点赞、Outbox、Worker/Indexer和reindex的event ID后台链路。
5. 执行第3.4节用户态、filters、PIT/cursor、空结果和存储错误查询矩阵。
6. 执行第3.5节template/index/alias、严格mapping、同ID重放、永久异常与帖子搜索隔离。
7. 执行第3.6节Metrics/Logs交替、代表性接收/传输故障、ES故障恢复和Metrics真实回归。
8. 在故障窗口执行第3.7节业务/内部访问/敏感哨兵检查，不额外重复所有业务排列。
9. 执行日常等价生命周期、verify只读性及正常/失败/中断资源前后快照。
10. 只对真实阻断失败做有限修复，相关代码或环境改变后只重跑受影响项。
11. 最终diff稳定后完成第9节尚未通过的固定门禁，更新README、总方案状态、版本 `1.6.3`和本批实施记录。
12. 仅暂存本批文件并提交；push、创建PR，查询真实远程checks与合入状态。
13. 合入且远程门禁成功后标记Phase 9完成并立即停止，把稳定契约交给Phase 10/11。

## 8. 风险与控制

- **旧ES/VM数据形成假通过**：使用随机volume、当前offset、message/event/request ID和窄UTC时间窗，禁止直接写存储充当源证据。
- **收口机械重跑扩大成本**：前批未变化成功项引用实施记录；只实际运行阶段交叉事实和最终固定门禁。
- **故障窗口误伤业务或日常栈**：停止前校验随机project、container ID、PID、port、volume和group，结束后对比快照。
- **日志查询本身产生新日志干扰分页**：PIT冻结第一页视图，验收使用绑定filter与已知时间窗，不用总count猜测。
- **ES故障被误写为新增业务失败**：区分Phase 3既有readiness/search依赖与Phase 9新增日志sink；只比较非搜索业务行为。
- **同Topic阻塞被误判丢失**：观察committed offset和恢复后顺序，不要求ES故障时后继metrics越过当前logs record。
- **admin权限掩盖敏感返回**：管理员响应仍按DTO白名单扫描，内部字段和哨兵对admin同样禁止。
- **虚构远程完成**：本地结果、push、PR、checks和merge分开记录；未观察即不写完成。
- **范围扩张**：固定矩阵通过后停止，Frontend/Events/ILM/spool/Topic拆分均转后续。

## 9. 固定验证命令与必要回归

最终diff上按影响执行；阶段交叉矩阵、治理和远程门禁必须实际完成：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-logs.sh --self-test
scripts/verify-logs.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

完整验收只在WSL2 Linux filesystem、真实Kafka/Elasticsearch/VictoriaMetrics和随机强归属资源执行。环境缺失时不得标记完成；mock、静态Envelope、直接Kafka produce或直接ES index只能用于最低层异常，不替代真实主链路。

## 10. 阶段收口验收标准

- 真实API request ID可查询到HTTP与业务日志，真实event ID可查询到Outbox与Worker/Indexer日志，reindex日志与命令结果一致。
- 四类日志源保留stdout且非阻塞Push；容量内短时故障恢复，长故障/崩溃的best-effort限制被准确记录。
- LogMonitor、Router、Kafka、Marshaller、Elasticsearch全链路有真实证据，合法写入后才提交，坏消息继续，同ID重放无重复文档。
- `gopulse-logs-v1-*`、read alias和strict mapping与帖子搜索完全隔离；搜索与reindex必要回归通过。
- 未登录 `401`、普通用户 `403`、admin受限查询成功；拒绝请求不触达ES，filter/cursor/范围/空结果/503均符合合同。
- Metrics/Logs在同Topic/Marshaller并存且只写各自存储；Phase 8真实metrics代表链路无回归。
- 可观测故障不改变非搜索社交API、RabbitMQ必要事实或权限；现有ES readiness/search退化被准确区分。
- 内部身份不可互换，端口保持loopback，stdout、ES、Backend响应、Frontend bundle和验收制品无敏感哨兵或内部字段。
- dev/verify/down和失败/中断清理不误杀、不误删、不遗留进程、端口、container/network/volume、group、PIT、插件根、日志或token。
- 总/拆分方案、README、三份实施记录、Git历史和远程状态一致；固定门禁通过，根与Frontend版本均为 `1.6.3`。

## 11. 明确完成条件

只有第10节全部满足、Phase-09-03 Pull Request已合入主远程 `main`、远程固定门禁成功且三份Phase 9实施记录真实完整，Phase 9才完成。任一真实源、完整传输、offset、查询授权、索引隔离、重放、Metrics并存、业务/敏感/资源安全或远程证据缺失时不得标记完成。

达到条件后立即停止，不在本批追加Review、Frontend、Events、告警、ILM、spool、Topic拆分或容量优化。后续独立Review只有在用户明确请求时进行，并按真实finding另行规划整改批次。

## 12. Phase 10 / Phase 11 交接

向Phase 10交付：

- Monitor被动接收与专用服务身份模式、Router多类型单Topic路由、Marshaller公共Envelope与typed handler registry。
- Elasticsearch固定template/index/alias和幂等 `_id` writer模式；Events必须使用独立payload/mapping/alias，不与logs混写。
- 正式group offset、永久/暂时错误、ownership与有序backpressure的真实证据。

向Phase 11交付：

- `GET /api/v1/observability/logs` 的实时admin授权、有限filters、PIT签名cursor、安全DTO和错误合同。
- 日志索引隔离、内部访问限制和Frontend只能经Backend读取的边界。
- stdout-before-`202` best-effort、Kafka-after-`202` at-least-once与同ID幂等的准确产品语义。

Phase 9完成后Milestone 3仍未完成；只有Phase 10 Events链路与Phase 11统一管理员前端均通过各自验收，才能宣称完整可观测MVP完成。
