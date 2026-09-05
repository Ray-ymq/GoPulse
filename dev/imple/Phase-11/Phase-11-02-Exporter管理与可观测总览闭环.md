# Phase-11-02：Exporter 管理与可观测总览闭环实施方案

> 权威目标版本与开发分支以 \`Phase-11-总实施方案.md\` 第 3.2 节为准：本批对应 \`1.8.2\` / \`develop/1.8.2\`。
>
> 当前状态：已完成。Pull Request #98 已于 2026-09-05 合入主远程 `main`，远程固定门禁全部成功。

## 1. 批次目标

在 Phase-11-01 已交付三类查询和双使用态管理壳层的基础上，完成管理员的“查看整体状态 → 管理 Exporter → 观察结果”闭环：

\`\`\`text
管理员可观测总览
  ├─ Metrics 最近状态
  ├─ Logs 最近记录
  ├─ Events 最近记录
  └─ Exporter 当前事实
       → install / start / stop / update
       → Backend 实时 admin 授权与严格 DTO
       → Monitor Plugin Manager
       → 返回当前状态并可在 Events/Metrics 页面继续核对
\`\`\`

本批完成后，管理员必须能通过真实浏览器管理 Redis Exporter，看到 desired/observed state、最近采集、最近成功和安全错误，并在总览同时使用四类能力。任一 VM、ES 或 Monitor 代表故障只能使对应区域失败，其他已可用结果、普通社交域和管理员社交能力应保持可用。

本批不重新实现 Phase 6 插件状态机，也不将 Events 查询误作插件当前状态事实源；Backend 返回的 Monitor 数据必须先经过严格 trust boundary。

## 2. 前置条件

- Phase-11-01 已合入最新主远程 \`main\`，远程门禁成功，根与 Frontend 版本为 \`1.8.1\`，同名实施记录与真实提交一致。
- 从包含 Phase-11-01 的最新 \`upstream/main\` 创建 \`develop/1.8.2\`，不沿用前批分支或 \`update\`。
- 核对管理布局、admin guard、Metrics/Logs/Events client 与状态模型、\`verify-observability-ui.sh\`，以及 Backend/Monitor Exporter API 的真实成功和错误 body。
- 核对 Phase 6/10 对 install/start/stop/update、desired/observed state、last scrape/success/error 和 Events best-effort 的既有语义；不从页面需求反向改写状态机。
- 在 WSL2 Linux filesystem、唯一 Docker daemon和随机强归属隔离资源中实施；保存 Git、日常栈、端口、plugin root、registry/process、VM/ES/Kafka 与临时包快照。

## 3. 实施范围

### 3.1 Backend Exporter DTO trust boundary

- 将 \`backend/internal/exporterplugin\` 的成功响应从宽松 decode 改为严格 JSON object：递归拒绝重复 key、未知字段、尾随 token、错误 JSON 类型、控制字符、超限 body 与多余内容。
- \`Status\` 只接受固定 \`id=redis-exporter\`、受限 name、稳定 SemVer、已知 kind/source、合法 desired/observed state 和 UTC RFC3339Nano 时间。
- \`last_error\` 改为显式 \`SafeError {code,message,at}\`，code/message 使用 Phase 6/10 已知安全词汇/长度边界；禁止任意 object 透传。
- 校验时间与状态组合：未安装不伪造 Status；running 必须有 started time，last success 不晚于 last scrape，错误时间和更新时间必须可解释；只冻结当前公共合同，不推导进程内部事实。
- List 按 ID 稳定，当前最多一个固定插件；Get/action/upload 都从已验证内部对象显式构造 Backend DTO。
- Monitor 畸形、未知、超限、redirect、网络、timeout 或非预期成功状态统一映射 \`503 monitor_unavailable\`；已知业务错误仍保持固定状态码。
- \`401/403\` 必须在 Monitor client 前发生；上传仍由 Backend/Monitor 各自执行大小、multipart 与包内容安全边界。

### 3.2 Frontend Exporter API 与页面

- 增加 ExporterStatus、SafeError、desired/observed state、错误 code 的 TypeScript 类型与严格 runtime validator。
- API client 支持 list/get/start/stop，以及 install/update 的单一 multipart \`package\`；不得手工设置 multipart boundary，cursor/token/内部地址不进入请求。
- 文件选择阶段给出 \`.tar.gz\`、非空和 64 MiB 上限提示；客户端检查只改善体验，服务端仍是最终安全边界。
- 未安装/空列表展示安装卡片；已安装展示 ID/name/version、状态、安装/更新时间、启动时间、最近采集、最近成功和 last error。
- 根据当前 DTO 禁用明显不适用操作和重复点击；stop/update 显示明确影响确认，进行中状态使用按钮与 \`aria-busy\` 或等价语义。
- 成功后用返回 DTO 原子更新页面并允许刷新；失败保留旧状态，区分 package invalid、not found、conflict、operation in progress、operation failed 和 monitor unavailable。
- 页面不得把 operation HTTP 成功等价为 Events 已写入，也不得根据 Events 缺失回滚或改写当前插件状态。

### 3.3 可观测总览

- 将 \`/admin/observability\` 从临时重定向替换为总览页面；管理导航正式加入总览与 Exporter。
- 并行发起四个独立请求：固定 \`gopulse_redis_up&range=15m\`、Logs 最近有限页、Events 最近有限页、Exporter list/status。
- 每个区域维护独立 loading、empty、error、last updated 和 retry；总览级刷新触发所有区域，但一个失败不取消或清空其他成功区域。
- Metrics 卡片显示最后 Redis up 样本及采样时间；Exporter 卡片显示 observed state/版本/最近成功；Logs/Events 只显示最新有限条目并链接详情页。
- 不合成跨数据源事务快照，不计算全局健康分、不将旧指标当当前事实、不把零日志/事件判定为健康。
- 总览与各详情页共享有限的 formatter/状态组件，避免错误文案、time/number 格式和 loading 语义分叉；不建设通用 dashboard framework。

### 3.4 局部故障、role 变化与操作恢复

- VM 故障时 Metrics 卡片和页面显示 \`metrics_unavailable\`，Logs/Events/Exporter 和非搜索社交能力保持可用。
- Monitor 故障时 Exporter 卡片/页面显示 \`monitor_unavailable\`，历史 Metrics/Logs/Events 仍可查看；Backend readiness 不新增 Monitor 依赖。
- ES 故障时 Logs/Events 分别显示安全不可用，Metrics/Exporter 与非搜索社交 API 可用；保留搜索/readiness 对 ES 的既有行为，不虚构完全健康。
- 管理员操作途中若被降权，Backend \`403\` 保持最终决定；Frontend 清除选择文件和管理数据、停止后续刷新并进入无权限状态。
- 网络失败、刷新失败或 Monitor action 失败不乐观伪造状态；恢复后由显式刷新重新读取 Plugin Manager 事实。

### 3.5 真实浏览器验收、生命周期与文档

- 扩展 \`frontend/e2e/observability.spec.ts\`，从真实管理页面完成至少一个状态转换；阶段最终矩阵覆盖 install/start/stop/update，但本批可按最短真实路径选择代表操作并在 Phase-11-03 完整收口。
- 扩展 \`scripts/verify-observability-ui.sh\` 生成确定性合法插件包，使用浏览器上传/启停/更新，并核对页面状态、后续 Events 与 Metrics。
- 在隔离栈依次制造 VM 和 Monitor 代表故障，验证总览局部失败、恢复、管理页面状态和社交必要回归。
- \`--self-test\` 增加 package path/size、plugin root、PID/port/project/container/volume 与清理目标负向检查；不读取或删除用户包。
- 更新 README、Frontend 使用说明、CI 与脚本说明；\`verify.sh\` 继续只读，不执行页面操作或创建验收数据。
- 完成本批同名实施记录并同步版本 \`1.8.2\`。

## 4. 实施边界与非目标

- 不改变 Monitor 插件安装、更新、回滚、进程 ownership、重启恢复或 MetricsMonitor 状态机；只有固定验收复现阻断问题时做最小修复。
- 不增加多插件、插件市场、远程 package URL、拖拽上传、批量操作、自动更新或任意插件 ID。
- 不增加 Backend 总览聚合 endpoint；Frontend 组合现有四个独立产品接口，以保留局部失败语义。
- 不实现全局健康分、告警、自动轮询、复杂图表、跨 Logs/Events 关联或管理员审计日志。
- 不重跑 Phase 6/8/9/10 全部可靠性矩阵，不创建应用镜像，不修改冻结 PowerShell。

## 5. 预计文件与交付物

\`\`\`text
backend/internal/exporterplugin/**
backend/internal/http/**
frontend/src/components/Admin*.vue
frontend/src/components/Observability*.vue
frontend/src/router/**
frontend/src/services/**
frontend/src/types/**
frontend/src/views/ObservabilityOverviewView.vue
frontend/src/views/ObservabilityExportersView.vue
frontend/src/styles.css
frontend/e2e/observability.spec.ts
README.md
backend/README.md
scripts/verify-observability-ui.sh
scripts/verify.sh（仅只读检查）
scripts/dev.sh（仅配置/输出）
scripts/down.sh（仅本批资源阻断修复）
scripts/ci/**
.github/workflows/quality-gates.yml
dev/logs/Phase-11/Phase-11-02-Exporter管理与可观测总览闭环.md
dev/imple/Phase-11/Phase-11-总实施方案.md（仅状态/真实偏差）
VERSION
frontend/package.json
frontend/package-lock.json
\`\`\`

预计文件是边界，不要求修改无直接需求的后端组件。若 Monitor/Router/Marshaller production code 被触及，实施记录必须写明具体阻断失败和扩展验证理由。

## 6. 详细实施步骤

1. fetch 最新 \`main\`，核对 11-01 合入/门禁/版本和验收入口，创建 \`develop/1.8.2\` 并保存资源快照。
2. 以当前真实 Monitor 响应冻结 Backend Status/SafeError DTO，先通过合法、畸形、未知字段、超限和已知错误映射测试。
3. 实现 Frontend Exporter types/validator/client，确保 multipart boundary、文件清理和运行中 \`403\` 行为正确。
4. 完成未安装、已安装、过渡、失败、Monitor 不可用及 install/start/stop/update 页面状态；只增加代表性组件测试。
5. 建立四区域总览和独立 loading/error/retry，复用而不泛化三类查询页的有限组件。
6. 扩展真实浏览器脚本和 E2E，完成插件代表操作、状态核对、Events/Metrics 观察与普通用户隔离。
7. 在隔离资源中验证 VM 与 Monitor 局部故障/恢复和非搜索社交必要回归；不重复下层全部故障排列。
8. 对齐 README、CI、脚本 self-test、版本和实施记录，最终 diff 上执行第 8 节门禁。
9. 只暂存本批文件并提交；push、创建 PR，查询真实远程 checks 与合入状态。

## 7. 风险与控制

- **Monitor 成功响应被直接信任**：Backend 先做递归/typed/semantic/size 校验，再显式构造公共 DTO；不使用 \`any\` 透传 last error。
- **操作按钮与真实状态竞争**：Frontend 只做重复提交抑制，最终状态取操作响应或刷新结果，不自己模拟 Plugin Manager 转换。
- **multipart 内存或 boundary 错误**：浏览器使用 FormData，Backend 保持流式/上限；页面不读取完整 package 到 JS 字符串。
- **总览单点失败**：四请求独立 settlement 与重试，不使用 all-or-nothing Promise 或 Backend 聚合。
- **历史 Metrics 与插件当前状态混淆**：总览分别标注 sample timestamp 和当前状态更新时间，不推导强一致健康。
- **故障验收误伤日常栈**：停止容器/进程前验证随机 project、port、PID、volume 和 plugin root 强归属，结束后对比快照。
- **范围扩张**：本批止于单一 Redis Exporter 和四区域总览；多插件、告警、自动轮询与大屏留后。

## 8. 固定验证命令与必要回归

最终 diff 稳定后执行：

\`\`\`bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/exporterplugin ./internal/http/...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.8.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/verify-events.sh \
  scripts/verify-observability-ui.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-observability-ui.sh --self-test
scripts/verify-observability-ui.sh
scripts/verify-monitor.sh --self-test
scripts/verify-events.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
\`\`\`

- \`verify-observability-ui.sh\` 必须使用真实浏览器完成插件代表操作，不得通过 curl 操作后只截图页面冒充交互证据。
- Phase-11-01 的已通过三类查询 unit/浏览器证据在相关代码未变时不机械重跑额外排列；本批主脚本自然覆盖的查询路径作为必要回归。
- WSL2/Docker/Chromium 缺失时必须记录主验收未通过，不能用组件 test 或 mocked fetch 代替。

## 9. 批次验收标准

- Backend 对 Exporter list/get/action/upload 成功 body 执行严格字段、类型、状态、时间、SemVer、SafeError 和容量验证；畸形响应不穿透浏览器。
- 普通用户对全部 Exporter API 为 \`403\` 且 Monitor client 零调用；admin 使用现有会话成功操作，运行中降权安全失败。
- 浏览器正确呈现未安装、running/stopped/failed/过渡、最近 scrape/success/error，并完成至少一个真实 install/start/stop/update 代表转换。
- 总览同时显示真实 Metrics、Logs、Events 和 Exporter 结果；各区域拥有独立 loading/empty/error/retry 和详情入口。
- VM、Monitor 代表故障分别局部降级并可恢复；其他管理区域、非搜索社交 API 和管理员社交能力不新增失败。
- 页面不直连内部组件、不回显原始 Monitor/VM/ES 内容，不把操作成功等价为事件必达或把历史样本等价为当前状态。
- 浏览器主闭环、脚本 self-test、资源清理、CI、版本/分支治理通过；实施记录真实完整，根与 Frontend 版本均为 \`1.8.2\`。

## 10. 明确完成条件

只有第 9 节全部满足、Phase-11-02 Pull Request 已合入主远程 \`main\`、远程固定门禁成功，且 \`dev/logs/Phase-11/Phase-11-02-Exporter管理与可观测总览闭环.md\` 与真实提交一致，本批才完成。

缺少真实浏览器插件操作、Backend Monitor response trust boundary、四区域局部失败或普通用户服务端拒绝任一证据时不得标记完成。完成后停止功能扩展，将最终能力交给 Phase-11-03 做跨批收口。

## 11. Phase-11-03 交接

- 可直接运行的管理域：总览、Metrics、Logs、Events、Exporter，以及返回社交域与无权限路径。
- Backend 固定 Metrics query 和严格 Exporter DTO，四类 API 的 Authentication → 实时 RequireAdmin 边界。
- 真实浏览器脚本中的 admin/普通用户、真实三类数据、插件包/状态转换、VM/Monitor 局部故障和强资源归属能力。
- 已知限制：VM 单节点共享内部 Basic 身份、总览非强一致快照、Events 不作为插件事实源、无告警/自动轮询/任意查询。
