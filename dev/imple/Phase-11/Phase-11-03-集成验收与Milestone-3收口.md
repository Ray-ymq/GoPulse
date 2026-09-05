# Phase-11-03：集成验收与 Milestone 3 收口实施方案

> 权威目标版本与开发分支以 \`Phase-11-总实施方案.md\` 第 3.2 节为准：本批对应 \`1.8.3\` / \`develop/1.8.3\`。
>
> 当前状态：未开始。

## 1. 批次目标

在 Phase-11-01 与 Phase-11-02 已合入的最终实现上执行封闭阶段矩阵，证明普通社交域与管理员可观测域可在同一产品、同一身份系统中安全并存；管理员通过真实浏览器共同使用 Metrics、Logs、Events 和 Exporter 管理，普通用户在导航、路由与 Backend API 三层保持隔离。

本批同时完成 Milestone 3“完整可观测 MVP”收口。它是集成验收与状态收口批次，不是第三个功能批次；除最终矩阵真实复现且阻断 Phase 11 验收的问题外，不新增 endpoint、查询参数、页面、图表、插件能力、告警或基础设施架构。

## 2. 前置条件

- Phase-11-01 与 Phase-11-02 已合入最新主远程 \`main\`，各自远程门禁成功，根与 Frontend 版本为 \`1.8.2\`，两份实施记录和真实提交一致。
- 从包含两批能力的最新 \`upstream/main\` 创建 \`develop/1.8.3\`，不沿用前两批分支或 \`update\`。
- 已核对两批实施记录中的实际偏差、已通过检查、浏览器证据、故障窗口、资源归属和已知限制；未变化的成功检查不机械重跑。
- 验收在 WSL2 Linux filesystem、唯一 Docker daemon、随机 Compose project/数据库/loopback 端口/凭据/plugin root/volume/Kafka group 与可强归属临时资源中执行。
- 开始前保存 Git、日常进程/端口、Compose project/container/network/volume、数据库、插件 registry/process、Kafka offset、ES index/alias/PIT、VM 时间窗与浏览器制品快照。
- 若任一前批未合入、远程失败、版本不一致或关键验收缺失，先回到对应批次处理，不使用收口批次掩盖。

## 3. 收口范围

### 3.1 最终静态、组件与治理门禁

- 在最终提交运行 Backend gofmt/unit/vet 与 metricquery/exporterplugin/http 直接 race；运行 Frontend Vitest、typecheck/build。
- 运行脚本 CI unittest、版本/分支治理、Bash 语法、LF、Compose 渲染、固定 loopback、VM/ES/Monitor 配置和资源归属检查。
- 运行 \`verify-observability-ui.sh --self-test\`，覆盖 project/port/PID/container/volume/plugin root/package/token/清理目标的拒绝条件。
- 核对前批已通过且未变化的 query builder、VM response validator、Exporter DTO validator、route guard、page state 和分页单元证据；只有相关实现或环境变化时重跑额外定向检查。

### 3.2 双使用态身份、导航与路由

- 注册普通用户和待提升管理员，确认二者使用同一登录/Cookie；通过运维 CLI 提升管理员，不提供页面提权入口。
- 普通用户社交导航不显示可观测入口；逐一直接打开总览、Metrics、Logs、Events、Exporter URL，确认在页面组件请求前进入明确无权限状态。
- 对普通用户直接调用 Metrics/Logs/Events 与 Exporter list/get/install/start/stop/update 代表接口，均为 \`403 permission_denied\`，对应 VM/ES/Monitor 调用为零。
- 管理员能在社交域完成代表操作，并从社交导航进入管理域、在管理子页切换、返回社交域和退出登录。
- 管理员进入管理域后，仅在隔离数据库中用受控测试 SQL 将该账号降为普通用户，确认下一次 role 确认或 API \`403\` 清除管理数据/文件/cursor 并进入无权限状态，但社交登录保持有效；不为验收新增产品降权 API 或 CLI。
- 未登录直达管理 URL 进入登录，登录 admin 后返回原管理目标；登录普通用户不得被带入管理组件。

### 3.3 Metrics 真实浏览器闭环

- 在当前隔离栈由真实 Redis Exporter、Monitor、Router、Kafka、Marshaller 产生 \`gopulse_redis_up\` 和代表性 gauge/counter/label family。
- 管理员在 Metrics 页切换至少一个无标签 family和一个 \`mode\` 或 \`db\` family，核对时间范围、step、series label、有限值、最后样本和趋势/表格。
- 对固定 \`15m|1h|6h|24h\` 选一个非默认范围，确认 Backend 固定 query/step，浏览器没有任意表达式或 label matcher入口。
- 空时间窗或新资源尚无样本时显示 empty；停止 VM 后显示 \`metrics_unavailable\`，响应不含 URL/Basic identity/query/body；恢复原 VM 后无需重启浏览器/Backend即可刷新成功。
- VM 故障期间 Logs/Events/Exporter 既有可用数据仍可查看，Backend 不因新增 VM 依赖失去 readiness。

### 3.4 Logs 与 Events 真实浏览器闭环

- 使用真实 Backend admin/社交请求产生唯一 request ID 日志，通过 Logs 页按 service/module/level 或 request ID 代表筛选查询。
- 通过真实浏览器插件操作或采集状态变化产生唯一生命周期/故障恢复事件，通过 Events 页按 event name/operation/severity 或 error code 代表筛选查询。
- 为 Logs 与 Events 各产生有限超过一页的数据，验证 load more 顺序、无重复/遗漏、新筛选重置 cursor，以及 cursor 失效后的明确恢复。
- 检查页面只展示 DTO 白名单；不显示 ES index/id/score、PIT/cursor 内容、message ID、Kafka metadata、Envelope、原始 JSON 或未知字段。
- 停止 ES 后 Logs/Events 显示各自安全 unavailable，Metrics/Exporter 和非搜索社交 API 仍可用；准确记录帖子 search/readiness 对 ES 的既有退化。
- 恢复原 ES 后页面刷新成功；不直接写 ES 或修改 alias/PIT 来制造通过。

### 3.5 Exporter 真实管理闭环

- 从未安装状态通过浏览器选择本批生成的合法 \`.tar.gz\`，完成 install 并看到 \`running\`、版本、安装/更新时间和最近启动。
- 等待真实 scrape 后刷新，核对 last scrape/last success 与 Metrics 最近样本时间可解释但不要求强一致。
- 通过浏览器依次执行 stop、幂等状态下的按钮限制与 start，确认操作进行中禁用、返回 DTO 原子更新和后续真实 Events 可查询。
- 使用更高稳定 SemVer package 执行 update，确认页面版本与 desired/observed state正确；更新失败代表场景保持旧页面事实并显示固定安全错误。
- 制造一个安全可控的 plugin/collection failure，使 last error 可见且无原始进程/网络详情；恢复后状态由刷新读取 Monitor 事实。
- Monitor 停止时 Exporter 页/总览局部 \`monitor_unavailable\`；历史 Metrics/Logs/Events 和社交域可用，恢复 Monitor 后可重新查询。
- 浏览器不得上传多 part、远程 URL 或超过客户端提示上限的文件；服务端 package 安全由 Phase 6 既有验证保护，不在本批穷举 archive 攻击矩阵。

### 3.6 总览、局部故障与恢复

- 正常状态下四区域分别显示真实 Redis 最近样本、Exporter 当前状态、最新 Logs 与 Events，并可导航到详情。
- 对 VM、Monitor、ES 三类依赖至少分别执行一个有归属的故障窗口；确认受影响区域、保留数据、错误文案、独立重试和恢复行为符合总方案。
- 总览不因一项 rejected promise 进入全页失败，不把历史样本或无记录合成为“系统健康”，不把四请求称为同一事务快照。
- 刷新过程中返回社交页、退出或降权时取消/废弃在途响应；旧响应不得重新填充管理数据。
- 故障窗口内执行代表性登录、帖子读取/发布与评论或点赞，确认 Phase 11 没有给非搜索业务增加 VM/Monitor/ES 依赖。

### 3.7 内部访问、输出与前端安全

- 记录真实浏览器 network，业务/管理请求只能访问 Frontend origin 下的 Backend \`/api/v1\`；不得访问 9090/9091/9092/9093/9200/8428 或其他内部地址。
- 扫描 Frontend bundle，不得含 VM/ES/Monitor URL、Basic/Bearer token、ES alias/index、Kafka Topic/group、query/PIT 或服务器绝对路径。
- 对日志允许字段、事件 metadata、插件 safe error 与文件名放置唯一 HTML/script 哨兵，页面必须以文本显示且不执行；不得使用 raw HTML renderer。
- 未知 Backend error code、畸形成功 body和超限字段在定向测试中转为安全通用错误，原始 body/message 不显示。
- 退出、降权、离页和失败路径清理上传文件引用、cursor、管理数据与在途请求，不在 localStorage/sessionStorage 持久化内部响应。

### 3.8 可访问性、响应式与体验一致性

- 所有筛选、导航、文件上传、确认与操作按钮可用键盘完成，form control 有明确 label，错误区域具有可感知语义。
- loading/disabled/busy 状态可由文本或 ARIA 识别，running/stopped/failed 与 level/severity 不只依赖颜色。
- 在桌面与窄屏 viewport 验证管理导航、筛选器、指标展示、日志/事件长字段和 Exporter 操作不产生不可用横向溢出。
- 刷新、筛选、加载更多和操作反馈在五个管理页面保持一致；不因收口新增独立设计系统或视觉重做。

### 3.9 生命周期、资源与远程状态

- 在隔离配置执行 \`dev.sh → verify.sh → down.sh\`，确认 Backend VM 配置、Frontend 路由、全部后台组件启动/关闭顺序和有界退出。
- \`verify.sh\` 必须只读：不创建用户/角色、不操作插件、不写 VM/ES/Kafka、不修改 offset、不打开无法关闭的 PIT。
- 正常、失败、signal 和中断路径前后对比 Git、PID、port、project/container/network/volume、数据库、Kafka group/offset、ES PIT/index、VM volume、plugin root/package 和临时凭据。
- 只清理本批随机强归属资源；unknown/mismatch 安全拒绝，日常命名资源和用户工作区改动必须保持。
- 更新 README、总/拆分方案状态与本批实施记录；本地验证、push、PR、远程 checks、合入和 Milestone 状态分别如实记录。

## 4. 阻断问题修复边界

本批只允许修复下列直接阻断固定矩阵的问题：

- 普通用户可见管理入口、直接路由触发数据请求、任一 API 未返回 \`403\`，或拒绝后仍调用内部服务。
- admin 无法在真实浏览器查询 Metrics/Logs/Events 或操作 Exporter，且问题属于已规划合同实现。
- VM query 或 Monitor response 未严格验证、内部地址/凭据/原始响应/HTML 可穿透到浏览器。
- cursor/过期请求混用导致跨筛选数据、role 变化后管理数据残留、重复操作或错误当前状态。
- 总览一项失败造成全页失败，或 VM/Monitor 新增依赖破坏 Backend readiness/非搜索社交能力。
- 页面关键键盘/label/非颜色状态或窄屏问题使固定用户闭环不可完成。
- 生命周期误杀/误删、资源遗留、verify 发生写操作，或版本/分支/CI/文档/实施记录不一致。

修复前记录最小复现和风险依据，修复后只重跑受影响 package、页面或场景。非阻断的视觉优化、性能、容量、可维护性和未来功能进入后续事项。

## 5. 实施边界与非目标

- 不新增公共 API、metric family/range、Logs/Events filter、Exporter operation、响应字段或管理路由。
- 不实现告警、自动轮询、WebSocket/SSE、任意查询、复杂大屏、跨数据关联、多 Exporter 或插件市场。
- 不修改 Metrics/Logs/Events 写入可靠性、Kafka/ES/VM 存储、Monitor Plugin Manager，除非真实阻断复现明确指向这些共享边界。
- 不做一般代码/架构 Review、依赖审计、覆盖率活动、长时压力或容量测试；只有用户另行明确请求时执行。
- 不创建应用容器镜像、Kubernetes/Ingress 资源，不修改冻结 PowerShell。

## 6. 预计文件与交付物

\`\`\`text
dev/imple/Phase-11/Phase-11-总实施方案.md（仅最终状态/真实偏差）
dev/imple/Phase-11/Phase-11-01-三类数据查询与双使用态前端闭环.md（仅状态/真实偏差）
dev/imple/Phase-11/Phase-11-02-Exporter管理与可观测总览闭环.md（仅状态/真实偏差）
dev/logs/Phase-11/Phase-11-03-集成验收与Milestone-3收口.md
README.md
backend/README.md
frontend/README.md（若已存在/本阶段创建）
frontend/e2e/observability.spec.ts（验收编排或阻断修复）
scripts/verify-observability-ui.sh
scripts/verify.sh（仅只读收口或阻断修复）
scripts/dev.sh（仅阻断修复）
scripts/down.sh（仅阻断修复）
scripts/ci/**
.github/workflows/quality-gates.yml
backend/**（仅阻断修复）
frontend/src/**（仅阻断修复）
VERSION
frontend/package.json
frontend/package-lock.json
\`\`\`

预计文件是允许边界，不要求制造无意义改动。如果固定验收没有暴露产品问题，本批应主要提交验收编排/证据、文档、版本和实施记录。

## 7. 详细实施步骤

1. fetch 最新 \`main\`，核对 11-01/02 实施记录、合入提交、远程 checks、版本、实际偏差与已知限制，创建 \`develop/1.8.3\` 并保存资源快照。
2. 执行第 3.1 节最终静态、直接 package、Frontend、脚本、Compose 和 self-test 门禁；记录可引用的未变化前批成功证据。
3. 在隔离栈完成第 3.2 节双用户态导航、直接路由、API 权限、管理员社交与运行中降权矩阵。
4. 执行第 3.3/3.4 节真实 Metrics、Logs、Events 浏览器查询、筛选、分页、空结果和 VM/ES 恢复。
5. 执行第 3.5 节浏览器 install/stop/start/update、失败反馈、状态时间与后续 Events/Metrics 核对。
6. 执行第 3.6 节总览四区域及 VM/Monitor/ES 局部故障/恢复，并在窗口内完成必要社交回归。
7. 执行第 3.7/3.8 节 browser network、bundle/响应/哨兵扫描、清理、键盘、语义与窄屏检查。
8. 执行日常等价生命周期、verify 只读和成功/失败/中断资源快照；只对真实阻断问题做有限修复。
9. 最终 diff 稳定后完成第 9 节尚未通过门禁，更新 README、方案状态、版本 \`1.8.3\` 和本批实施记录。
10. 只暂存本批文件并提交；push、创建 PR，查询真实远程 checks 与合入状态。
11. 合入且远程门禁成功后标记 Phase 11 与 Milestone 3 完成并立即停止，向 Phase 12 交付容器化输入。

## 8. 风险与控制

- **收口演变为新功能批次**：只修复第 4 节阻断问题；新页面、查询、告警与视觉优化全部留后。
- **旧 VM/ES 数据假通过**：随机 volumes、唯一用户/请求/插件版本、窄 UTC window 和当前样本时间，不直接写存储。
- **插件管理破坏资源**：只对随机 plugin root 与验证 ownership 的进程操作，package 由本批生成并记录 digest/version。
- **故障矩阵破坏日常环境**：每次停止/恢复前验证 project label、container ID、port、PID、volume、database 和 plugin root，结束后对比快照。
- **共享 ES 故障语义被误述**：明确 Logs/Events 与帖子 search/readiness 的既有共同退化，只要求非搜索社交 API 不新增失败。
- **前批检查机械重复**：未变化证据引用实施记录；阶段脚本自然覆盖的路径不再另跑等价全排列。
- **浏览器 UI 与 API 证据割裂**：关键查询和插件操作都由 Playwright 在真实页面完成，同时核对 Backend/存储事实。
- **虚构远程完成**：本地结果、push、PR、checks、merge 与 Milestone 状态分开记录，未观察即不写完成。

## 9. 固定验证命令与必要回归

最终 diff 上执行：

\`\`\`bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/metricquery ./internal/exporterplugin ./internal/http/...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.8.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/verify-events.sh \
  scripts/verify-observability-ui.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-observability-ui.sh --self-test
scripts/verify-observability-ui.sh
scripts/verify-events.sh --self-test
scripts/verify-logs.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
\`\`\`

- 阶段主闭环必须在 WSL2 Linux filesystem、真实依赖、真实自研进程和强归属隔离资源中通过；mock/fake server 仅用于定向异常，不代替主证据。
- \`verify-observability-ui.sh\` 是本阶段唯一完整浏览器主入口；其自然覆盖的下层健康路径不再通过调用全部旧真实脚本重复。
- 只有观察到共享写入、授权、生命周期或业务回归时才扩展运行 \`verify-events.sh\`、\`verify-logs.sh\`、\`verify-marshaller.sh\`、\`verify-monitor.sh\` 或 \`verify-business.sh\` 的真实模式，并在实施记录说明原因。
- 环境缺失或任一固定矩阵失败时不得标记 Phase/Milestone 完成。

## 10. 阶段收口验收标准

- 同一身份系统下，admin 同时使用社交与独立管理域；普通用户无管理导航、直接 URL 无数据请求、全部查询/管理 API 为 \`403\` 且内部调用为零。
- 管理员真实浏览器可查看固定 Redis Metrics，查询/筛选/分页真实 Logs 与 Events，页面只展示白名单 DTO 并准确表达空结果与交付边界。
- 管理员真实浏览器完成 Exporter install/start/stop/update 阶段矩阵，当前状态/最近采集/错误来自严格 Backend DTO，后续 Events/Metrics 可核对但不被误作强一致事务。
- 总览四区域同时可用且各自 loading/empty/error/retry；VM、Monitor、ES 代表故障只造成准确局部退化并可恢复。
- Backend Metrics query 不接受任意表达式，VM/Monitor 响应均经严格 trust boundary；浏览器只访问同源 Backend且无内部地址、凭据、query/body、alias/index/PIT/Topic 泄漏。
- role 变化、筛选变化、离页、退出与请求竞态不会残留或重新注入管理数据、上传文件、cursor 或旧结果。
- VM/Monitor 不加入 Backend readiness；可观测故障不破坏非搜索社交 API，admin 仍可使用社交域，ES 对 search/readiness 的既有影响被准确记录。
- 管理域关键闭环具备键盘可操作、明确 label/focus/busy、非仅颜色状态与窄屏可用性。
- 日常/隔离生命周期、verify 只读、失败/中断清理、CI、版本/分支治理与远程门禁通过，三份实施记录真实完整，根与 Frontend 版本均为 \`1.8.3\`。
- Metrics、Logs、Events 与 Exporter 管理已在一个管理员体验中共同闭环，Milestone 3“完整可观测 MVP”成立。

## 11. 明确完成条件

只有第 10 节全部满足、Phase-11-03 Pull Request 已合入主远程 \`main\`、远程固定门禁成功，且三份 Phase 11 实施记录与真实提交一致，Phase 11 和 Milestone 3 才完成。

任一真实浏览器查询/插件操作、普通用户导航/路由/API 隔离、Backend 最终授权、DTO/内部访问安全、局部故障恢复、社交回归、可访问性关键路径、资源清理或远程证据缺失时不得标记完成。

完成后立即停止，不追加 Review、告警、大屏、任意查询、自动轮询、跨数据关联、多插件或容器化。独立实现 Review 只有用户明确请求时另行执行。

## 12. Phase 12 交接

- 普通社交域与 \`/admin/observability\` 管理域的完整 Frontend 构建、路由和浏览器验收。
- Backend Metrics/Logs/Events/Exporter 四类产品 API、最终 admin 授权、VM/ES/Monitor 配置及局部故障语义。
- 明确用户访问面仅为 Frontend/Backend；Monitor、Router、Marshaller、Kafka、VictoriaMetrics、Elasticsearch 是 Phase 12 必须转入内部容器网络的服务面。
- \`verify-observability-ui.sh\` 的真实数据、浏览器、权限、故障和强资源归属矩阵，供应用容器化后复用。
- VM/Monitor 不进入 Backend readiness、ES 对 search/readiness 的既有依赖、总览非强一致快照和单节点 VM 共享内部 Basic 身份等已知边界。

Phase 12 必须保证容器化后浏览器仍只访问 Backend、普通用户仍为 \`403\`、内部身份不注入 Frontend，并在完整 Compose 中重跑本阶段代表浏览器闭环。
