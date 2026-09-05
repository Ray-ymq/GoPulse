# Phase-11-01：三类数据查询与双使用态前端闭环实施方案

> 权威目标版本与开发分支以 \`Phase-11-总实施方案.md\` 第 3.2 节为准：本批对应 \`1.8.1\` / \`develop/1.8.1\`。
>
> 当前状态：未开始。

## 1. 批次目标

以“管理员通过独立管理域查询三类真实可观测数据”为第一条产品纵向切片，一次性交付：

\`\`\`text
管理员现有会话
  → Frontend /admin/observability 管理壳层与 role 守卫
  → Backend Authentication + 实时 RequireAdmin
  ├─ 固定 Metrics API → VictoriaMetrics
  ├─ 现有 Logs API → Elasticsearch Logs alias
  └─ 现有 Events API → Elasticsearch Events alias
  → Frontend 白名单展示、筛选、分页与明确失败反馈
\`\`\`

本批完成后，管理员必须能在真实浏览器中查看 Redis Metrics、筛选/分页 Logs 与 Events并返回社交域；普通用户必须没有管理导航，直接输入任一管理 URL 时不触发可观测请求，并且直接调用全部查询 API 获得 \`403 permission_denied\`。

本批不交付 Exporter 管理页面与四区域总览，它们属于 Phase-11-02；但必须建立可扩展的管理壳层、API/runtime validator 和真实浏览器验收入口，不能只做页面骨架。

## 2. 前置条件

- 开工前 fetch 最新主远程，确认 Phase 10 最终版本 \`1.7.4\`、三类后端数据链路、Logs/Events API 与实施记录均已合入并通过远程门禁。
- 从最新 \`upstream/main\` 创建 \`develop/1.8.1\`；不沿用 \`update\`、Phase 10 或其他已完成分支。
- 核对 \`/users/me\` role、Authentication/RequireAdmin 次序、Logs/Events 参数与 DTO、10 个 Metrics family、VM Basic Auth/范围查询和 Frontend 认证恢复行为。
- 在 WSL2 Linux filesystem 与唯一 Docker daemon 中实施；保存 Git、日常进程/端口、Compose/volume、数据库、plugin root、Kafka group/offset、ES alias/PIT 和 VM 时间窗快照。
- 首次探索只读 Frontend router/auth/API/view/style、Backend http/config/logquery/eventquery、VM client模式和直接测试；不开展通用设计系统重写或后端链路审计。

## 3. 实施范围

### 3.1 Backend 固定 Metrics 查询

- 新建 \`backend/internal/metricquery\`，包含固定 catalog、参数 parser、query builder、VictoriaMetrics client、严格响应 validator、service 与 handler。
- 新增 \`GET /api/v1/observability/metrics\`，挂载到与 Logs/Events 相同的 Authentication → RequireAdmin 路由组；未登录/普通用户在创建 VM 请求前终止。
- \`metric\` 必填并只允许总方案 10 个 family；\`range\` 仅允许 \`15m|1h|6h|24h\`，默认 \`15m\`，Backend 固定对应 \`15s|1m|5m|15m\` step。
- query 只由 server catalog 构造固定 \`source=redis,target_id=redis-exporter-local\` selector；拒绝任意 query、label、from/to、step、重复/未知/空参数。
- 通过 \`POST /prometheus/api/v1/query_range\` 使用内部 Basic Auth；禁止 redirect，限制 timeout 与 2 MiB body，不记录 URL 凭据、query 或上游 body。
- 只接受 matrix 范围响应和总方案固定 label；规范化 UTC timestamp/有限 number，稳定排序，限制 32 series/4096 points，空结果返回空 series。
- 新增 \`metrics_unavailable\` 并将网络、timeout、认证、非成功、超限或不可信响应映射为安全 \`503\`。
- 增加 Backend VM URL/username/password/query timeout 与固定 range 配置；校验与日常 VM 身份一致，但不把 VM 加入 Backend readiness。

### 3.2 管理路由、壳层与双使用态

- 建立 \`/admin/observability\` 父级空间和 Metrics、Logs、Events 子路由；本批父路径可重定向 Metrics，Phase-11-02 再替换为总览页面。
- 增加独立 AdminLayout/AdminNav 或等价组件，提供三类查询导航、预留 Exporter 入口位置和“返回社交”；普通 AppNav 只对 admin 显示“可观测”入口。
- 扩展 router meta 为 \`requiresAdmin\`，守卫在组件挂载前等待/刷新 \`/users/me\`；匿名进入登录页，普通用户进入 \`/forbidden\`。
- 增加明确无权限页，允许返回帖子页；不得在该页包含管理数据、底层信息或自动重试可观测 API。
- admin 被运行中降权时，Backend \`403\` 清理当前管理数据并转入无权限状态，不清除普通登录或破坏社交能力。
- 保护全局与管理域 catch-all，确保未知管理 URL 不能绕过 admin guard。

### 3.3 Frontend API 与运行时验证

- 扩展 \`ApiErrorCode\` 与 HTTP 安全映射，加入 \`metrics_unavailable\`、\`logs_unavailable\`、\`events_unavailable\` 及 Phase 6 插件错误，为 Phase-11-02 复用。
- 建立 Metrics/Logs/Events DTO、filter/options 和严格 runtime validators；响应字段、时间、数值、枚举、metadata/结构化字段与 Backend 当前合同一致。
- query builder 使用安全编码；Logs/Events cursor 保持 opaque，续页只携带 cursor，不解析、不记录、不写入 URL 以外的长期存储。
- 支持取消或丢弃过期请求，避免筛选变化、离页或 role 变化后旧响应覆盖新状态。
- 不使用 \`v-html\`；未知错误只显示通用安全文案，不向用户展示 Backend 未识别 message 或响应 body。

### 3.4 Metrics 页面

- 默认查询 \`gopulse_redis_up\` 最近 \`15m\`，提供固定 family 与时间范围选择。
- 展示 family、kind、unit、实际 from/to/step、series labels、最后样本和轻量趋势；提供语义化的最新值/数据表作为可访问替代。
- 明确区分无数据和 VM 不可用；刷新失败时保留上次成功数据并显示更新时间。
- Counter 只显示原始累计序列，不实现 rate、聚合或任意表达式。

### 3.5 Logs 页面

- 支持当前 API 的 from/to 与 \`service,module,level,message,request_id,event_id,error_code\` 精确筛选，筛选目录必须与 Backend 固定词汇一致。
- 初次查询使用最近 15 分钟和固定合理 limit；按稳定顺序展示允许的日志 DTO 字段。
- 实现初次 loading、empty、error、refresh、load more 与 cursor 失效后重新查询；新筛选必须清空旧 cursor。
- 不展示 ES index/id/score、原始 JSON、未知字段、任意 DSL 或全文搜索框。

### 3.6 Events 页面

- 支持 from/to 与 \`source,event_name,severity,plugin_id,operation,error_code\` 精确筛选；前端目录阻止明显不可能组合，Backend 仍最终验证。
- 为固定 event name/severity/operation 和 metadata 提供稳定可读文案；只渲染已知 metadata 字段，不使用通用 JSON viewer。
- 明示可观测 Events 的 best-effort/at-least-once 边界，不把无事件解释为操作未发生。
- 与 Logs 一致实现 loading、empty、refresh、error、分页和 cursor 失效恢复。

### 3.7 浏览器验收入口与文档

- 新增 \`frontend/e2e/observability.spec.ts\` 或等价真实浏览器用例，覆盖普通用户和管理员两种会话。
- 新增 \`scripts/verify-observability-ui.sh\` 与 \`--self-test\`；隔离模式启动产生真实 Metrics/Logs/Events 所需的现有组件、Backend 和 Frontend。
- 管理员由真实注册 + \`admin-role promote\` 获得角色；数据通过真实 Redis/Exporter/Monitor/Router/Kafka/Marshaller/Backend 行为产生，不直接写 VM/ES。
- 更新 \`.env.example\`、Bash config 传递、Backend/根 README、CI 和页面使用说明；不修改冻结 PowerShell。
- 完成本批同名实施记录并同步版本 \`1.8.1\`。

## 4. 实施边界与非目标

- 不实现 Exporter 管理页、插件上传交互、总览四区域或局部 Monitor 故障矩阵；由 Phase-11-02 完成。
- 不修改 Metrics/Logs/Events 写入、Envelope、Kafka Topic/group、ES mapping/alias 或 Monitor 事件状态机。
- 不提供任意 MetricsQL/PromQL/DSL、日志全文检索、指标聚合/rate、告警、自动轮询、WebSocket/SSE 或复杂图表库。
- 不重构全部社交页面、替换全局视觉系统、引入第二套身份或创建应用容器镜像。
- 不修改冻结 PowerShell，不执行原生 Windows 验收。

## 5. 预计文件与交付物

\`\`\`text
backend/internal/metricquery/**
backend/internal/apperror/**
backend/internal/config/**
backend/internal/http/**
backend/cmd/server/**
frontend/src/components/Admin*.vue
frontend/src/components/AppNav.vue
frontend/src/composables/useAuth.ts
frontend/src/router/**
frontend/src/services/**
frontend/src/types/**
frontend/src/views/ForbiddenView.vue
frontend/src/views/ObservabilityMetricsView.vue
frontend/src/views/ObservabilityLogsView.vue
frontend/src/views/ObservabilityEventsView.vue
frontend/src/styles.css
frontend/e2e/observability.spec.ts
.env.example
README.md
backend/README.md
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-observability-ui.sh
scripts/ci/**
.github/workflows/quality-gates.yml
dev/logs/Phase-11/Phase-11-01-三类数据查询与双使用态前端闭环.md
dev/imple/Phase-11/Phase-11-总实施方案.md（仅状态/真实偏差）
VERSION
frontend/package.json
frontend/package-lock.json
\`\`\`

预计文件是边界，不要求制造无意义修改。若实现采用不同但同等清晰的目录，应在实施记录说明，不得突破总方案安全和验收合同。

## 6. 详细实施步骤

1. fetch 最新 \`main\`，核对 Phase 10 合入/门禁/版本与公共 DTO，创建 \`develop/1.8.1\` 并保存资源快照。
2. 先实现 metric catalog、options/query builder 与 VM client，使用 fake server 通过固定成功、空、畸形、超限和 unavailable 测试。
3. 将 Metrics handler 接入现有 observability admin group，证明 \`401/403/admin\` 与拒绝时 client 零调用，不增加 readiness 依赖。
4. 扩展 Frontend auth/router/nav，先通过匿名、普通用户、admin、直接 URL 和运行中降权的守卫测试。
5. 建立三类 API runtime validator 与页面公共加载/刷新/分页状态，避免把页面复制成三套不一致的错误逻辑。
6. 完成 Metrics、Logs、Events 页面及直接组件测试；只对本批新行为增加代表性用例。
7. 建立隔离浏览器验收入口，用真实链路产生三类数据并完成管理员查询与普通用户三层隔离。
8. 对齐配置、Bash 生命周期、README 和 CI；执行最小受影响检查后只修复真实失败。
9. 最终 diff 上完成第 8 节门禁，同步版本 \`1.8.1\`，如实编写同名实施记录。
10. 只暂存本批文件并提交；push、创建 PR，查询真实远程 checks 与合入状态。

## 7. 风险与控制

- **任意查询代理扩大攻击面**：metric 与 range 只从 server catalog 映射，永不拼接客户端表达式或 label matcher。
- **VM 响应穿透**：完整验证 result type、labels、timestamp、finite value、顺序和容量，再构造 DTO；失败不返回部分原始内容。
- **前端守卫抢跑**：admin guard 在 route component mount 前完成，测试断言普通用户 fetch 仅有 \`/users/me\`。
- **role 缓存过期**：进入管理域确认当前 role，API \`403\` 触发管理数据清理和明确拒绝；Backend 始终实时授权。
- **分页结果混用**：filter/range 变化使 cursor、在途请求和旧结果失效，cursor 不解析、不跨条件复用。
- **旧存储数据假通过**：隔离资源、唯一操作/请求标识和窄 UTC 时间窗，禁止直接写存储。
- **范围膨胀**：本批止于三类查询闭环；Exporter 操作、总览和跨依赖局部故障属于下一批。

## 8. 固定验证命令与必要回归

最终 diff 稳定后执行：

\`\`\`bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/metricquery ./internal/http/...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.8.1 --base-ref upstream/main
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
scripts/verify-business.sh --self-test
git diff --check
\`\`\`

- 真实浏览器验收必须证明当前隔离运行产生的 Metrics、Logs、Events，不能用 mocked fetch 或旧 volume 数据替代。
- 只因 Backend 路由共享边界而执行必要 Logs/Events/社交 unit 回归；Phase 8～10 的写入故障矩阵在实现未变时引用既有记录。
- 若 WSL2、Docker 或浏览器依赖缺失，实施记录必须标记主闭环未验证，不得用 macOS/mock 结果标记完成。

## 9. 批次验收标准

- Metrics API 只接受固定 family/range，使用内部 VM 身份完成固定范围查询，并返回有界、稳定、严格白名单 DTO。
- 未登录/普通用户/admin 对 Metrics、Logs、Events 分别获得 \`401/403/200\`；拒绝请求不触达 VM/ES。
- admin 看见独立管理导航并可返回社交域；普通用户无入口，直接任一管理 URL 不加载页面数据且进入明确无权限状态。
- 真实浏览器能查看当前运行产生的 Redis Metrics、Backend Logs 和 Monitor Events；筛选、刷新、空结果、错误及 Logs/Events 代表分页可用。
- Frontend runtime validator 拒绝不可信响应，不渲染 raw JSON/HTML、底层 URL/凭据、query/alias/index/PIT 或未知字段。
- VM 不可用不改变 Backend readiness 新依赖、Logs/Events 查询、Exporter API 或非搜索社交 API。
- 日常/隔离脚本、self-test、浏览器闭环、CI、版本/分支治理通过；实施记录真实完整，根与 Frontend 版本均为 \`1.8.1\`。

## 10. 明确完成条件

只有第 9 节全部满足、Phase-11-01 Pull Request 已合入主远程 \`main\`、远程固定门禁成功，且 \`dev/logs/Phase-11/Phase-11-01-三类数据查询与双使用态前端闭环.md\` 与真实提交一致，本批才完成。

任一真实三类查询、Backend admin 授权、普通用户直接路由无请求、VM response trust boundary 或内部访问隔离证据缺失时不得标记完成。达到条件后停止，不提前实现 Phase-11-02 总览或插件管理。

## 11. Phase-11-02 交接

- 独立管理布局、admin 守卫、运行中 \`403\` 处理、无权限页和返回社交域路径。
- Metrics 固定查询 API 以及 Metrics/Logs/Events 的严格 Frontend types、validators、clients 和页面状态模型。
- 可扩展的 \`verify-observability-ui.sh\`、真实 admin/普通用户浏览器夹具、随机资源归属和三类真实数据生成能力。
- 明确查询故障隔离：VM 只影响 Metrics，ES 只影响 Logs/Events，管理失败不改变普通社交业务。
