# Phase 11：可观测前端总实施方案

> 当前状态：未开始。本方案于 2026-09-05 以最新 \`upstream/main\` 提交 \`1baea93\` 与产品版本 \`1.7.4\` 为规划基线。Phase 11 使用 \`1.8.x\` 版本线，共拆分为 3 个执行批次。

## 1. 实施目标

在 Phase 8、Phase 9 和 Phase 10 已交付 Metrics、Logs、Events 三类真实数据链路及 Phase 6 已交付 Exporter 管理 API 的基础上，建立独立于普通社交信息架构的管理员可观测域：

\`\`\`text
同一用户注册、登录与 Cookie
  ├─ 普通用户 / 管理员 → 社交业务域
  └─ 管理员 → /admin/observability
       → Backend 实时 admin 授权
       ├─ 固定 Metrics 查询 → VictoriaMetrics
       ├─ 受限 Logs 查询 → Elasticsearch Logs alias
       ├─ 受限 Events 查询 → Elasticsearch Events alias
       └─ Exporter 查询与操作 → Monitor Plugin Manager
\`\`\`

阶段完成必须同时证明：

- 管理员通过浏览器查看真实 Redis 指标、查询真实业务日志和真实可观测事件，并完成真实 Redis Exporter 安装、启动、停止或更新代表操作。
- 普通用户不显示管理入口，直接输入管理 URL 时在组件挂载和请求发起前被阻止，对全部可观测查询与管理 API 固定获得 \`403 permission_denied\`。
- Frontend 只访问同源 Backend \`/api/v1\`，不持有或接触 Monitor、Router、Kafka、Marshaller、VictoriaMetrics、Elasticsearch 的地址和凭据。
- 总览与各功能页对加载、空结果、部分依赖失败、请求失败、操作冲突和恢复提供明确且有限的状态，不因某一可观测依赖失败破坏其他管理能力或普通社交业务。

只增加导航链接、只交付静态页面、让浏览器直连底层组件、代理任意 MetricsQL/Elasticsearch DSL、只隐藏按钮而不做 Backend 授权，或用 mock 数据代替阶段浏览器主闭环，均不构成 Phase 11 完成。

## 2. 当前真实基线与规划输入

本方案编写前已 fetch 主远程。规划基线具备：

- Frontend 使用 Vue 3、Vue Router、TypeScript、Vite、Vitest 和 Playwright；当前只有社交业务路由及独立 \`/dev/status\` 诊断页，没有管理员布局、可观测 API client、管理页或 admin 路由守卫。
- \`/api/v1/users/me\` 已返回数据库权威的 \`user|admin\` role，Frontend 的认证恢复在首次受保护导航前完成；当前路由只区分 \`requiresAuth\`、\`guestOnly\` 与诊断页。
- Backend 已使用同一 Authentication → 数据库实时 RequireAdmin 链保护 \`GET /api/v1/observability/logs\`、\`GET /api/v1/observability/events\` 和全部 \`/api/v1/exporter-plugins\` 路由。
- Logs API 支持最大 24 小时时间范围、固定词汇过滤、PIT + 签名 cursor，并只返回 Schema v1 安全 DTO；Events API提供等价的最大范围、固定事件/metadata 词汇、独立 PIT cursor 与安全 DTO。
- Backend 尚无 Metrics 产品查询 API，也没有 VictoriaMetrics 查询 client；VictoriaMetrics 仅通过内部 Basic Auth 暴露 loopback 查询入口，现有验收脚本会执行固定即时/范围查询。
- Metrics 存储目前只有 10 个固定 Redis family，时序带固定 \`source=redis\`、\`target_id=redis-exporter-local\`，仅 \`mode=user|system\` 和十进制 \`db\` 是允许的业务标签。
- Backend 已代理 Exporter 列表、详情、install/start/stop/update；Monitor 状态含 desired/observed state、安装/更新时间、最近启动、最近采集、最近成功和安全 last error。当前 Backend client 对 Monitor 成功 body 与 \`last_error\` 的校验仍不足以作为浏览器最终信任边界，Phase-11-02 必须收紧。
- 可观测写入或查询依赖不会参与 MySQL 业务事实事务。Backend 既有 Elasticsearch readiness 同时服务帖子搜索，但 Phase 11 不得再把 VictoriaMetrics 或 Monitor 纳入 Backend 业务 readiness。
- Bash 生命周期、隔离验收和 CI 已覆盖各后端链路及普通社交浏览器闭环；当前没有统一的管理员浏览器可观测验收入口。

Phase-11-01 开工前必须重新 fetch 最新主远程并核对上述路由、DTO、指标 family、Monitor 状态和脚本入口。如果已合入变更影响公共契约、安全边界或批次依赖，应先更新本方案与尚未开始的拆分方案。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 10 最终版本 \`1.7.4\` 已位于最新主远程 \`main\`，Phase 8～10 的实施记录、远程门禁以及 Metrics/Logs/Events 真实链路证据可核对。
- 每批实施、应用测试和集成验收在 Windows 宿主的 WSL2 Linux filesystem 与唯一 Docker daemon 中执行；Bash 是唯一维护的本地生命周期和验收入口。
- 每批开始前保存 Git、日常进程/端口、Compose project/container/network/volume、数据库、插件根、Kafka group/offset、ES index/alias/PIT 和 VM 查询窗口快照。
- 每批只读取直接涉及的 Frontend、Backend 查询/插件代理、配置、脚本和测试，不把可观测前端实施扩展为全仓 Review、依赖审计、覆盖率活动或基础设施调优。

### 3.2 权威批次、版本与开发分支

Phase 11 使用 \`1.8.x\` 版本线，\`1.8.0\` 只作为阶段基线，不创建空批次。下表是本阶段执行顺序、目标版本与开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-11-01 | \`1.8.1\` | \`develop/1.8.1\` | 未开始 |
| Phase-11-02 | \`1.8.2\` | \`develop/1.8.2\` | 未开始 |
| Phase-11-03 | \`1.8.3\` | \`develop/1.8.3\` | 未开始 |

执行规则：

- 每批从包含全部前置批次的最新 \`upstream/main\` 创建独立分支，不在 \`update\`、Phase 10 分支或已完成开发分支实施。
- 同一批次全部提交共享目标版本；批次完成时同步根 \`VERSION\`、\`frontend/package.json\` 和 \`frontend/package-lock.json\`。
- 每批完成前创建与拆分方案同名的 \`dev/logs/Phase-11/Phase-11-XX-*.md\`，只记录实际改动、验证、偏差、失败和限制。
- Phase-11-01 交付三类数据查询、独立管理壳层和双使用态访问边界的完整浏览器纵向闭环。
- Phase-11-02 交付 Exporter 浏览器管理、可观测总览、跨页面状态反馈和代表性降级闭环。
- Phase-11-03 只在前两批已合入能力上执行跨批集成、Milestone 3 验收、文档和状态收口；除真实复现的阻断问题外不新增功能。
- 已推送分支不得静默改名或重新编号。实施前批次顺序变化时，先修改本表并重算尚未创建的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 独立 \`/admin/observability\` 路由空间、管理员布局、管理导航、返回社交域入口、普通用户拒绝页和 admin 路由守卫。
- Backend 固定 Redis Metrics 查询 API、严格 VictoriaMetrics client、受限参数、确定性查询构造、响应重验证和安全 DTO。
- Frontend Metrics、Logs、Events 页面，覆盖固定选择项、受控时间范围、分页、刷新、空结果、错误和安全字段呈现。
- Frontend Exporter 状态与管理页面，覆盖未安装、运行、停止、过渡、失败、Monitor 不可用，以及 install/start/stop/update 操作。
- 由 Frontend 组合 Metrics、最新 Logs、最新 Events 与 Exporter 状态的可观测总览；每个区域独立加载、失败和重试。
- Backend Exporter 成功响应 trust boundary 加固，确保 Monitor 即使返回畸形或扩展字段也不会穿透到浏览器。
- 管理员浏览器真实闭环、普通用户导航/路由/API 三层隔离、可观测依赖降级和社交域必要回归。

### 4.2 明确不做

- 复杂大屏、拖拽布局、用户自定义面板、图表设计器、任意 MetricsQL、PromQL 或 Elasticsearch DSL。
- 告警规则、告警中心、通知推送、日志全文分析、跨 Logs/Events 关联追踪或自动根因分析。
- 自动轮询形成的准实时控制台、WebSocket/SSE、无限滚动、后台导出、批量插件操作或插件市场。
- 多 Exporter 通用化、多 target 聚合、任意插件 ID、任意 metric/label、任意 metadata 或原始 JSON 查看器。
- Frontend 直连 Monitor、Router、Kafka、Marshaller、VictoriaMetrics 或 Elasticsearch。
- 新管理员身份、第二套登录、前端自助提权、通用 RBAC 或仅依赖前端隐藏的权限方案。
- 应用镜像、完整 Compose 应用容器化、Kubernetes、Ingress、生产高可用、容量压测或生产级凭据治理；它们属于 Phase 12 及以后。
- 修改冻结的 \`scripts/*.ps1\`、新增原生 Windows 验收或 Windows runner。

## 5. 双使用态信息架构与访问矩阵

### 5.1 路由与导航

固定管理路由：

| 路由 | 页面 | 访问条件 |
| --- | --- | --- |
| \`/admin/observability\` | 可观测总览 | 登录且当前 role 为 admin |
| \`/admin/observability/metrics\` | Redis 指标 | 登录且当前 role 为 admin |
| \`/admin/observability/logs\` | 应用日志 | 登录且当前 role 为 admin |
| \`/admin/observability/events\` | 可观测事件 | 登录且当前 role 为 admin |
| \`/admin/observability/exporters\` | Exporter 管理 | 登录且当前 role 为 admin |
| \`/forbidden\` | 明确无权限状态 | 已登录用户 |

- 普通社交导航保持帖子、搜索、通知、发布和退出；只有 admin 在该导航看到一个“可观测”入口，不把指标卡片或插件控件嵌入普通用户页面。
- 管理域使用独立 header/侧栏或等价导航，显示总览、指标、日志、事件、Exporter 和“返回社交”；不复制登录状态或建立第二套会话。
- 管理路由使用 \`requiresAuth + requiresAdmin\`。守卫等待 \`/users/me\` 恢复；未登录进入登录页，非 admin 在目标组件挂载前进入 \`/forbidden\`，不得触发任何可观测请求。
- 首次进入管理域前应刷新或确认当前用户 role；运行中 Backend \`403 permission_denied\` 表示服务端 role 已变化时，Frontend 清除管理页敏感内存状态并转入无权限状态，但不把已登录用户误判为登出。
- 管理域未知子路由只在通过 admin 守卫后重定向总览；全局未知路由不得成为绕过守卫的入口。

### 5.2 权限矩阵

| 主体 | 社交域 | 管理导航/路由 | Metrics/Logs/Events API | Exporter API |
| --- | --- | --- | --- | --- |
| 未登录 | 登录/注册页 | 重定向登录 | \`401 authentication_required\` | \`401 authentication_required\` |
| 普通用户 | 完整使用 | 不显示；直接访问为明确拒绝 | \`403 permission_denied\`，内部 client 零调用 | \`403 permission_denied\`，Monitor client 零调用 |
| 管理员 | 完整使用 | 允许 | 受限查询 | 受限查询与管理 |

Frontend 守卫只改善体验，Backend 实时数据库授权始终是最终安全边界。作者摘要、帖子、评论、搜索和通知不得暴露用户是否为 admin。

## 6. Backend Metrics 查询契约

### 6.1 HTTP API 与参数

新增：

| Method | Path | 授权 | 成功响应 |
| --- | --- | --- | --- |
| \`GET\` | \`/api/v1/observability/metrics\` | Authentication → 数据库实时 RequireAdmin | \`200\` 单一 metric family 范围结果 |

请求参数固定为：

- \`metric\` 必填且只能出现一次，只允许第 6.2 节的 10 个 family。
- \`range\` 可选且只能出现一次，默认 \`15m\`，仅允许 \`15m|1h|6h|24h\`。
- 不接受 \`query\`、任意表达式、from/to、step、label matcher、重复 key、未知/空参数、控制字符、通配、正则或超长值；统一返回 \`400 validation_failed\`。
- 时间窗由 Backend 当前 UTC 时钟锚定；固定 step 分别为 \`15s|1m|5m|15m\`，使单条时序最多约 100 个点。不得把客户端输入拼接为查询表达式。

### 6.2 固定指标目录

| metric family | kind | unit | 允许的公共标签 |
| --- | --- | --- | --- |
| \`gopulse_redis_up\` | gauge | boolean | 无 |
| \`gopulse_redis_uptime_seconds\` | gauge | seconds | 无 |
| \`gopulse_redis_connected_clients\` | gauge | count | 无 |
| \`gopulse_redis_used_memory_bytes\` | gauge | bytes | 无 |
| \`gopulse_redis_commands_processed_total\` | counter | count | 无 |
| \`gopulse_redis_keyspace_hits_total\` | counter | count | 无 |
| \`gopulse_redis_keyspace_misses_total\` | counter | count | 无 |
| \`gopulse_redis_cpu_seconds_total\` | counter | seconds | \`mode=user|system\` |
| \`gopulse_redis_db_keys\` | gauge | count | \`db=<uint32 decimal>\` |
| \`gopulse_redis_db_expiring_keys\` | gauge | count | \`db=<uint32 decimal>\` |

Backend 只构造：

\`\`\`text
<allowlisted_metric>{source="redis",target_id="redis-exporter-local"}
\`\`\`

并调用 VictoriaMetrics \`POST /prometheus/api/v1/query_range\`。浏览器不能选择 provenance 标签，不能请求 VM 自身指标，也不能查询其他 target。

### 6.3 安全响应 DTO

\`\`\`json
{
  "data": {
    "metric": "gopulse_redis_cpu_seconds_total",
    "kind": "counter",
    "unit": "seconds",
    "range": "15m",
    "from": "2026-09-05T08:00:00Z",
    "to": "2026-09-05T08:15:00Z",
    "step_seconds": 15,
    "series": [
      {
        "labels": {"mode": "user"},
        "points": [
          {"timestamp": "2026-09-05T08:15:00Z", "value": 12.5}
        ]
      }
    ]
  }
}
\`\`\`

- \`labels\` 是强类型白名单对象，只能包含该 family 允许的 \`mode\` 或 \`db\`；不返回 \`__name__\`、\`source\`、\`target_id\` 或任意未知标签。
- timestamp 规范化为 UTC RFC3339，value 必须是有限 JSON number；系列与点按标签、timestamp 稳定排序。
- 最多 32 个 series、4096 个总点、2 MiB 上游 body；超限、重复时序、倒序/重复时间点、无效标签、NaN/Inf、错误 result type 或未知字段作为不可信上游响应处理，不返回部分结果。
- 无匹配数据返回 \`200\` 与空 \`series\`；VictoriaMetrics 网络、超时、认证、redirect、非成功、畸形或超限响应统一返回 \`503 metrics_unavailable\`。
- 不返回 VM URL、认证信息、原始 MetricsQL、原始响应、内部错误或下游 status text。

### 6.4 Client、配置与 readiness

- 在 \`backend/internal/metricquery\` 建立 options/catalog/query builder、VictoriaMetrics client、response validator、service 和 handler；不复用日志/事件 PIT cursor，也不把查询实现放入 Frontend。
- Client 禁止 redirect，使用有界 timeout/header/body；Basic Auth 只从 Backend 配置读取，不写日志、不进入错误或 API DTO。
- Phase 8 单节点 VM 只有一套 HTTP Basic Auth，本阶段 Backend 查询暂时复用同一内部身份；这是本地 MVP 的已知最小权限限制，浏览器仍不可获得该身份，Phase 12 网络收口不得将其变为公开入口。
- 新增并严格校验：

\`\`\`text
BACKEND_VICTORIAMETRICS_URL=http://127.0.0.1:8428
BACKEND_VICTORIAMETRICS_USERNAME=gopulse-marshaller
BACKEND_VICTORIAMETRICS_PASSWORD=<at-least-32-bytes>
BACKEND_VICTORIAMETRICS_QUERY_TIMEOUT=3s
BACKEND_METRIC_QUERY_DEFAULT_RANGE=15m
BACKEND_METRIC_QUERY_MAX_RANGE=24h
\`\`\`

- 日常 Bash 配置必须使 Backend 查询身份与当前 VM 容器身份一致；URL 只接受无 userinfo/query/fragment 的 HTTP(S) base URL，本地生命周期固定 loopback。
- VictoriaMetrics 不加入 Backend \`/ready\`；VM 不可用只影响 Metrics API，不改变社交 API、Logs/Events 查询或 Exporter 管理响应。

## 7. Frontend 三类数据查询域

### 7.1 API 边界与公共状态

- 为 Metrics、Logs、Events 和后续 Exporter DTO 建立显式 TypeScript 类型与运行时 validator；拒绝缺字段、未知字段、错误类型、非法时间、非有限数值、未知词汇和不可能组合。
- API query builder 使用 \`URLSearchParams\` 或等价安全编码；cursor 保持 opaque，续页只发送 cursor；筛选变化后丢弃旧 cursor 和旧请求结果。
- 页面至少区分初次加载、空结果、带旧数据刷新失败、完全失败、无权限和恢复成功。一次失败不清空仍可安全展示的上次结果，页面明确标记更新时间。
- 使用请求序号、AbortController 或等价机制阻止过期响应覆盖新筛选；禁用重复提交和重复“加载更多”。
- \`401\` 继续触发现有登出导航；\`403\` 进入无权限状态；\`metrics_unavailable|logs_unavailable|events_unavailable|monitor_unavailable\` 使用各自有限文案，不显示底层错误。

### 7.2 Metrics 页面

- 以固定指标目录和 \`15m|1h|6h|24h\` 时间范围选择器发起 Backend 请求；默认展示 \`gopulse_redis_up\` 最近 15 分钟。
- 显示指标名称、kind、unit、实际时间窗、最后样本和值；允许 family 自带的 mode/db 多 series，但不把标签扩展成任意过滤器。
- 使用轻量、响应式趋势展示与可访问的最新值/数据表；颜色不是 up/down 或错误的唯一表达方式，空数据与依赖不可用明确区分。
- Counter 只显示存储中的原始累计值，不在浏览器擅自推导 rate；复杂聚合与派生查询留后。

### 7.3 Logs 页面

- 复用当前 Backend 合同提供时间范围和 \`service,module,level,message,request_id,event_id,error_code\` 精确筛选；选择项来自受版本控制的前端目录并与 Backend 词汇保持一致。
- 首页默认最近 15 分钟，limit 固定使用合理页面大小；按 Backend 顺序展示 timestamp、level、service/module、固定 message 和允许的结构化字段。
- 不提供自由全文搜索、不显示 ES index/id/score、原始 JSON、任意字段或底层错误。
- “加载更多”只使用 next cursor；新筛选、刷新或时间范围变化重新开启首页查询。

### 7.4 Events 页面

- 提供时间范围与 \`source,event_name,severity,plugin_id,operation,error_code\` 精确筛选，主动阻止已知不可能组合。
- 对 10 个固定 event name 使用稳定中文显示名，同时保留可核对的技术标识；按 event 类型将 metadata 渲染为已知键值，不使用任意 JSON renderer。
- 明确说明 Events 是有界 best-effort/at-least-once 可观测记录，查询不到不代表操作绝对未发生；不将事件作为插件当前状态事实源。
- 分页、刷新、空结果、PIT/cursor 失效和 ES 不可用分别给出可操作反馈；cursor 失效重新查询首页，不静默拼接不一致结果。

## 8. Exporter 管理与可观测总览

### 8.1 Backend Exporter trust boundary

- 将 Monitor 成功响应解码为严格 DTO：只接受固定 \`redis-exporter\`、已知 kind/source/state、稳定 SemVer、UTC 时间和 \`last_error {code,message,at}\`。
- 对响应 body 设置上限，拒绝重复/未知字段、尾随 token、null/错误类型、控制字符、未知状态、无效时间及不可能的 desired/observed 组合。
- Backend 只返回显式构造的公共 DTO；不得原样转发 Monitor JSON、PID、路径、命令行、环境、内部 URL/token 或未知错误。
- 畸形/超限/不可达 Monitor 响应统一 \`503 monitor_unavailable\`；已知业务错误继续保持 Phase 6 的稳定 code/status。
- 全部插件读写路由继续位于 Authentication → RequireAdmin 之后，拒绝时 Monitor client 调用为零。

### 8.2 Exporter 页面

- 未安装时展示安装入口；安装与更新仅接受一个 \`.tar.gz\` 文件，客户端先做非空、扩展名和 64 MiB 上限提示，Backend/Monitor 仍执行最终安全校验。
- 已安装时展示 ID/name/version、desired/observed state、安装/更新时间、最近启动、最近采集、最近成功和安全 last error。
- start/stop/update 根据当前状态和 operation-in-progress 禁用不适用操作；防止重复点击，但不在前端重新实现 Plugin Manager 状态机。
- stop/update 等有中断影响的动作提供明确确认；操作成功使用返回 DTO 原子替换状态，失败保留旧状态并显示固定错误，随后可手动刷新。
- 页面不宣称 Events 已投递成功；插件操作成功与后续 Events 是否可查保持 Phase 10 的准确边界。

### 8.3 总览页面

- 总览通过四个独立 Backend 请求组合：\`gopulse_redis_up\` 最近 15 分钟、最近 Logs、最近 Events、Exporter 列表/状态。
- 不新增 all-or-nothing 聚合 endpoint。四个区域分别拥有 loading/empty/error/retry，VM 失败不能清空 Events/Logs/Exporter，Monitor 失败不能遮蔽历史查询。
- 展示当前可核对事实：Redis 最近样本、Exporter observed state 与最近成功时间、最新有限日志和事件；不把页面抓取时点合成为强一致快照。
- 每个摘要提供进入对应详情页的链接与总览级手动刷新；不做自动告警、健康评分或无依据的“系统正常”结论。

## 9. 安全、故障与兼容性边界

- 浏览器网络请求必须保持同源 Frontend → Backend；构建产物扫描不得出现底层端口、URL、Basic/Bearer token、ES alias/index、Kafka Topic/group 或绝对路径。
- Backend 对 Metrics/Logs/Events/Exporter 均先认证再实时 admin 授权；普通用户拒绝必须发生在创建 VM/ES/Monitor 请求之前。
- 对用户可见的结构化字段执行文本插值，不使用 \`v-html\` 或等价 raw HTML；长字段截断/换行不得破坏布局或形成脚本执行。
- 页面不可用状态不得回显 Backend 未知 message、下游 body、query、PIT、cursor 内容、token 或网络详情。
- VM 不可用只使 Metrics 区域失败；Monitor 不可用只使 Exporter 查询/操作失败；ES 不可用使 Logs/Events 失败并保留 Phase 3 既有 search/readiness 语义；非搜索社交 API 仍须可用。
- 管理页面刷新、返回社交域和退出登录不能遗留上传文件、旧 cursor、敏感数据或在途操作回调。
- Phase 11 不改变 Metrics/Logs/Events 写入、Kafka offset、ES mapping/alias、Monitor 插件事实或业务数据库；UI 只能读取/调用现有受控产品接口。

## 10. 生命周期、验收入口与 CI

- \`scripts/dev.sh\` 继续先启动基础设施与后台链路，再启动 Backend/Frontend；只扩展 Backend VM 查询配置和用户可见地址说明，不新建 Frontend 直连代理。
- \`scripts/verify.sh\` 保持只读，可检查 Metrics route、管理前端可达与固定配置，但不得创建用户、提升角色、操作插件、写 Kafka/ES/VM 或打开无法关闭的 PIT。
- 新增 \`scripts/verify-observability-ui.sh\` 作为 Phase 11 唯一真实浏览器主验收入口，支持 \`--self-test\` 和隔离真实模式。
- 真实模式使用随机 Compose project、数据库、loopback 端口、凭据、plugin root、Kafka group/offset、ES/VM volume 和进程目录；通过真实 API 注册普通用户与管理员并使用运维 CLI 提升管理员。
- 验收数据由真实 Redis/Exporter/Monitor/Router/Kafka/Marshaller/Backend 行为产生；允许为可控页面数量调用真实产品 API，不允许直接 VM import、ES index 或静态响应替代主证据。
- 正常、失败、signal 和中断路径只清理本批强归属资源；对 unknown/mismatch 安全拒绝，并核对日常栈与用户工作区前后快照。
- Frontend CI 运行 Vitest、typecheck/build；Backend CI覆盖 metricquery 与 Exporter trust boundary；脚本/Compose job 纳入新脚本语法/self-test；独立 Phase 11 浏览器 job 或现有合适 job 运行真实主闭环。

## 11. 测试与验收策略

### 11.1 最低有效测试层

- Backend metricquery 测试覆盖参数 allowlist、固定 query 构造、时间窗/step、合法 VM matrix、空结果、超限/畸形响应、timeout/redirect/非成功与 \`metrics_unavailable\`。
- Backend HTTP 测试对 Metrics、Logs、Events、Exporter 各选代表请求证明 \`401/403/admin\`，重点断言拒绝时相应内部 client/repository 零调用。
- Exporter client 测试覆盖一个合法状态、一个畸形/未知字段响应和已知错误映射；不枚举所有 JSON 排列。
- Frontend router/nav 测试覆盖匿名、普通用户、admin、role 刷新失败、运行中 \`403\` 和直接管理 URL 无请求。
- Frontend 各页用代表性成功、空、失败与一次 stale-response/重复操作测试证明状态机；Logs/Events 分页各保留一个代表场景，不重复 Backend 全部 query contract。
- 浏览器真实验收覆盖普通用户隔离、管理员三类查询、Exporter 代表管理操作、总览部分失败和返回社交域；不把每个过滤排列复制到 E2E。

### 11.2 阶段封闭端到端矩阵

1. 同一浏览器会话验证 admin 进入社交域与管理域；普通用户无导航、直接 URL 无管理请求，并对全部 API 获得 \`403\`。
2. 真实 Redis → Exporter → Monitor → Router → Kafka → Marshaller → VM 数据可由 admin Metrics 页查询；页面无数据与 VM \`503\` 可区分。
3. 真实 Backend 请求日志与真实插件/采集事件经现有链路进入各自 ES alias，并由 admin Logs/Events 页面筛选、分页与刷新。
4. 通过浏览器完成至少一个真实插件生命周期操作；状态 DTO、按钮状态、最近采集/成功/错误与后续可观测事件一致。
5. 总览四区域显示各自真实结果；依次制造一个 VM 故障和一个 Monitor 故障，确认局部失败、可恢复和其他区域保持可用。
6. 可观测故障窗口执行代表性登录、帖子读取/发布或评论/点赞，证明非搜索社交行为不新增失败。
7. 浏览器请求、构建产物、Backend 响应、保留日志与验收制品扫描不得包含内部 URL/凭据、原始查询/响应或敏感哨兵。
8. \`dev.sh → verify.sh → down.sh\` 与隔离脚本在成功、失败和中断路径不误杀、不误删、不遗留资源。

已在前批通过且相关代码、配置、依赖和环境未变化的 Metrics/Logs/Events 写入可靠性、offset、mapping、幂等和 episode 去抖证据可引用实施记录，不在 Phase 11 重跑全套后端故障排列。

## 12. 批次拆分与交付关系

### 12.1 Phase-11-01：三类数据查询与双使用态前端闭环

- 新增 Backend 固定 Metrics API 与安全 VM query client。
- 建立管理路由/导航/守卫、无权限状态和 Metrics/Logs/Events 页面。
- 交付管理员浏览器三类查询及普通用户导航/路由/API 隔离的第一条纵向闭环。
- 详见 [Phase-11-01-三类数据查询与双使用态前端闭环.md](Phase-11-01-三类数据查询与双使用态前端闭环.md)。

### 12.2 Phase-11-02：Exporter 管理与可观测总览闭环

- 加固 Backend Exporter DTO trust boundary。
- 交付 Exporter 状态/安装/启停/更新页面与四区域总览。
- 扩展真实浏览器验收到插件操作、局部故障、恢复和社交隔离。
- 详见 [Phase-11-02-Exporter管理与可观测总览闭环.md](Phase-11-02-Exporter管理与可观测总览闭环.md)。

### 12.3 Phase-11-03：集成验收与Milestone-3收口

- 在最终合入实现上执行阶段封闭矩阵、必要回归、生命周期/资源检查、文档和远程状态收口。
- 除真实阻断问题外不新增产品功能。
- 详见 [Phase-11-03-集成验收与Milestone-3收口.md](Phase-11-03-集成验收与Milestone-3收口.md)。

三个批次符合阶段提纲的上限：前两个分别形成“管理员查询”和“管理员操作/总览”两个可独立验收的用户闭环，第三个只做跨批集成与里程碑收口；没有按 Frontend、Backend、测试或数据源机械拆分。

## 13. 预计变更边界

\`\`\`text
backend/internal/metricquery/**
backend/internal/exporterplugin/**
backend/internal/apperror/**
backend/internal/config/**
backend/internal/http/**
backend/cmd/server/**
backend/**/*_test.go
frontend/src/components/**
frontend/src/composables/**
frontend/src/router/**
frontend/src/services/**
frontend/src/types/**
frontend/src/views/**
frontend/src/utils/**
frontend/src/styles.css
frontend/e2e/**
frontend/package.json
frontend/package-lock.json
.env.example
README.md
backend/README.md
frontend/README.md（若创建）
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-observability-ui.sh
scripts/ci/**
.github/workflows/quality-gates.yml
dev/imple/Phase-11/**
dev/logs/Phase-11/**
VERSION
\`\`\`

预计文件是允许边界而非强制修改清单。无需变更 Monitor/Router/Marshaller/Exporter production code；只有 Phase 11 固定验收复现其公共契约的阻断问题时，才能做最小修复并记录风险依据。

## 14. 固定完成门禁

各功能批次只执行自身拆分方案规定的直接检查；Phase-11-03 在最终 diff 上完成以下阶段门禁：

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

- 完整浏览器主闭环只能在 WSL2 Linux filesystem、真实 MySQL/Redis/RabbitMQ/Elasticsearch/Kafka/VictoriaMetrics、真实自研进程和强归属隔离资源中标记通过。
- Backend/Frontend unit test 或 mock 浏览器响应不能替代三类真实查询和插件管理主证据。
- 只有具体共享边界变化或观察到回归时才追加对应后端真实验收，并在实施记录说明扩展原因；默认不重跑 Phase 8～10 全部矩阵。
- 每批只运行一次最终固定门禁；成功后若相关代码、配置、依赖和环境未变，不因上下文切换机械重跑。

## 15. Phase 级验收、完成与交接

### 15.1 Phase 级验收标准

- 管理员沿用现有会话同时使用社交域和独立可观测域；普通用户不显示入口、直接路由不请求数据、全部可观测 API 返回 \`403\`。
- Backend Metrics API 只接受固定 family/range，构造固定 selector，严格验证 VM 响应并返回有界 DTO；不暴露任意查询能力、内部身份或地址。
- 管理员可在浏览器查看真实 Metrics、按现有契约查询/分页 Logs 与 Events；页面只渲染白名单字段，并正确区分 loading、empty、invalid、unavailable 和 refresh failure。
- 管理员可通过浏览器查看 Exporter 当前状态与最近采集/错误，并完成真实 install/start/stop/update 中至少一个代表生命周期；Backend 不原样转发 Monitor 数据。
- 可观测总览同时组合真实 Metrics、最新 Logs、最新 Events 和 Exporter 状态，单一区域失败不遮蔽其他结果或形成错误的全局健康结论。
- 浏览器只调用同源 Backend；构建产物、请求、响应、日志和验收制品不含底层 URL/凭据、原始查询/响应、索引/Topic/PIT 或敏感哨兵。
- VM、Monitor 或 ES 代表故障有明确局部反馈并可恢复；非搜索社交 API、管理员社交能力和现有身份会话不因 Phase 11 新增依赖而失败。
- 管理页面具备键盘可操作、明确 label/focus、非仅颜色状态和窄屏可用的最低可访问性；不要求复杂可视化。
- 日常/隔离生命周期、verify 只读性、成功/失败/中断清理与远程门禁通过；三份实施记录真实完整，根与 Frontend 版本均为 \`1.8.3\`。
- Milestone 3 的三类数据与插件管理在同一管理员产品体验内共同可用，普通用户社交域保持隔离和可用。

### 15.2 完成与停止条件

只有第 15.1 节全部满足、Phase-11-01/02/03 Pull Request 均已合入主远程 \`main\`、远程固定门禁成功，且三份 Phase 11 实施记录与真实提交一致，Phase 11 与 Milestone 3 才完成。

任一真实浏览器三类查询、插件代表操作、Backend 最终授权、普通用户直接路由无请求、DTO/内部访问隔离、局部故障恢复、社交回归、资源安全或远程证据缺失时，不得标记完成。

达到条件后立即停止。复杂大屏、告警、任意查询、跨数据关联、自动轮询、更多 Exporter、多租户和生产安全加固记录为后续，不继续占用 Phase 11。独立实现 Review 只在用户明确请求时执行，不作为默认阶段门禁。

### 15.3 Phase 12 交接

向 Phase 12 交付：

- 完整 Frontend 用户访问面：普通社交域与 \`/admin/observability\` 管理域，二者共享 Backend 会话并由 Backend 执行最终授权。
- Backend 四类可观测产品入口及其配置：固定 Metrics query、Logs/Events 查询、Exporter 查询与管理。
- 浏览器只访问 Frontend/Backend 的明确拓扑；Monitor、Router、Marshaller、Kafka、VictoriaMetrics、Elasticsearch 均是容器化时必须保持内部网络隔离的服务面。
- 管理域对 VM/ES/Monitor 局部故障的既有行为、Backend readiness 不新增 VM/Monitor 依赖的边界，以及非搜索社交业务的代表回归。
- 可复用的真实浏览器验收入口、随机资源归属/清理规则和 Milestone 3 验收矩阵，供完整 Compose 容器化后复跑。

Phase 12 不得因容器网络可达而让 Frontend 直连内部组件，也不得把 Backend VM/Monitor 配置或内部身份注入浏览器构建产物。
