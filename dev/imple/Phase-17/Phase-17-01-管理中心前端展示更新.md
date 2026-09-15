# Phase-17-01：管理中心前端展示更新实施方案

> 目标版本：`1.14.1`
> 开发分支：`develop/1.14.1`
> 运行与验收平台：WSL2/Linux + Bash；真实浏览器验收沿用 Linux `amd64` Compose 产品入口
> 视觉基准：`dev/imple/Phase-17/assets/Management_Center/` 下四张 `1440 × 1000` PNG

## 1. 批次目标

本批作为 Phase 17 临时插入的首个开发批次，只更新独立 Admin Frontend 的管理中心展示，使既有管理大屏、Metrics、Logs 和 Exporter 页面在不改变业务/API 合同的前提下，对齐用户提供的视觉基准，并让其余管理页面共享一致的导航壳层、状态语义和响应式体验。

```text
existing administrator APIs + role boundary
                    │
                    ▼
       shared admin layout and visual tokens
                    │
        ┌───────────┼───────────┐
        ▼           ▼           ▼
    dashboard     metrics      logs + exporter
        └───────────┼───────────┘
                    ▼
 accessible responsive management center
```

本批是前端展示批次，不新增管理能力、不伪造展示数据，也不提前实现 Phase-17-02 的运行时合同或 Phase-17-03 的持久状态可靠性工作。

## 2. 前置条件与视觉输入

- 从根完成版本 `1.13.6` 已合入的最新 `upstream/main` 创建 `develop/1.14.1`，不得从 `update` 或旧开发分支起步。
- 视觉输入固定为：
  - `dev/imple/Phase-17/assets/Management_Center/管理大屏.png`
  - `dev/imple/Phase-17/assets/Management_Center/Metrics.png`
  - `dev/imple/Phase-17/assets/Management_Center/Logs 页面.png`
  - `dev/imple/Phase-17/assets/Management_Center/Exporter 管理.png`
- 四张图片用于确定信息层级、侧栏/顶栏、色彩、间距、卡片、筛选区、状态标签和桌面布局，不要求逐像素复制图片中的示例值、内部地址或静态图表数据。
- 开工时只读取 `admin-frontend/` 的布局、路由、四个目标页面、直接服务/type 和现有相关测试；不得把临时视觉更新扩展为全仓前端重构。
- 沿用 Phase 16 的独立 Admin Frontend、同源 edge、统一登录和服务端 `super_admin` 授权边界；浏览器不得直连内部服务。

## 3. 实施范围

### 3.1 共享管理壳层

- 将现有顶部堆叠导航调整为视觉基准中的桌面侧栏 + 顶部工具栏结构，保留跳转主要内容、当前路由高亮、返回社交、当前管理员标识和退出登录。
- 导航继续覆盖管理大屏、Metrics、Logs、Events、告警、Exporter、用户角色和审计；不得隐藏既有页面或改变路由兼容重定向。
- 提取仅供 Admin Frontend 使用的布局/视觉 primitive，统一品牌区、分组标题、图标占位、按钮、卡片、状态 pill、表单和表格语义；不复制第二套业务状态管理。
- 桌面 `1440 × 1000` 以参考图为主要基准；窄屏下侧栏可折叠或转为可访问导航，内容不得水平溢出或遮挡操作。

### 3.2 管理大屏

- 依据 `管理大屏.png` 重组标题、刷新操作、摘要卡片、关键组件健康、告警概览、指标趋势和最近事件的信息层级。
- 只消费现有 overview DTO；服务端未提供的值显示明确的 unknown/暂无/partial 状态，不根据参考图硬编码 `6/6`、`12.8k`、告警数量或趋势点。
- 六个既有 overview section 必须仍可访问；无数据、分区无效、部分失败、加载和重试状态保持可辨识且不把 unknown 显示为 0。
- 若现有 DTO 足以绘制轻量趋势/环形展示，可使用无额外运行时依赖的 SVG/CSS；若事实不足，使用真实数据的等价卡片表达，不伪造序列。

### 3.3 Metrics 与 Logs

- Metrics 页面按参考图形成页面标题、查询条件、摘要卡片、主趋势区和最近采样表格；保留固定指标目录、范围和数据源合同。
- Logs 页面按参考图形成筛选栏、级别摘要和日志流卡片；保留现有分页、精确 Request ID/Error Code、级别/服务/模块筛选和安全 metadata 展示。
- 图表、标签和列表必须由现有 API 数据派生；空结果、后端不可用、invalid response、加载和加载更多状态有稳定文案。
- 长 request ID、错误码、实例、job 和 metadata 不得破坏布局；原始敏感 payload 仍不展示。

### 3.4 Exporter 管理

- 按 `Exporter 管理.png` 重组六插件目录、选中项、目标配置、运行状态、安装/更新和启动/停止操作区。
- 继续使用现有 Exporter DTO、签名安装包、安全字段、状态机和操作确认；不得仅为匹配图片增加 host/port/password 等后端不存在或不允许编辑的字段。
- running/stopped/installing/updating/failed 等状态保持可访问文本，不只依赖颜色；失败原因继续使用安全错误码和时间。
- 六个官方插件、刷新、选择、配置、安装/更新、启动/停止及指标跳转的既有行为不得回退。

### 3.5 其余管理页面的一致性

- Events、告警、用户角色和审计页面只适配新壳层及共享 token，修复直接由布局变化引起的溢出、标题和操作区问题。
- 不在本批重新设计告警规则、角色管理或审计业务流程，不新增页面、API、权限、Schema 或可观测来源。

### 3.6 加载、错误、可访问性与响应式

- 所有目标页面保留可见 focus、键盘导航、语义 heading/landmark、表单 label、按钮 disabled/busy 和 `role=status/alert`。
- 颜色对比、状态文本、触控目标和窄屏阅读顺序满足现有产品可用性基线；`prefers-reduced-motion` 下不依赖动画传达状态。
- 刷新和筛选防止重复提交；失败后保留安全重试入口，不泄漏内部 URL、堆栈、token、cookie 或原始响应。
- 视觉样式优先使用 CSS/SVG 和现有字体栈，不新增图表/UI 依赖，除非现有实现无法满足明确验收项且实施记录说明原因。

## 4. 明确不做

- 不修改 Backend、Monitor、Router、Marshaller、Exporter 进程、数据库、消息合同或 Compose 拓扑。
- 不把参考图中的示例数值、主机名、端口、账号或状态硬编码为产品事实。
- 不改变 `user/super_admin` 权限、统一登录、cookie、同源代理或管理 API 公共合同。
- 不重新设计普通用户 Frontend，不新增主题系统、国际化框架、图表框架或通用组件库。
- 不开展独立代码审查、覆盖率活动、依赖升级或与四个目标页面无关的机会性重构。

## 5. 建议实施顺序

1. 对照四张图片和现有 DTO，列出可直接实现、需等价表达和明确禁止伪造的展示项。
2. 完成共享壳层、设计 token、桌面/窄屏导航及直接组件测试。
3. 依次更新管理大屏、Metrics、Logs、Exporter，并在每页完成最小受影响测试。
4. 适配 Events、告警、用户角色、审计到新壳层，不改变业务流程。
5. 使用真实 Compose 管理入口运行管理员/普通用户、四页功能与桌面/窄屏浏览器验收，并保存必要截图供人工对照。
6. 固定门禁通过后更新同名实施记录和版本元数据，提交并停止。

## 6. 预计直接影响文件

- `admin-frontend/src/components/AdminLayout.vue` 及必要的 Admin Frontend 展示组件
- `admin-frontend/src/styles.css`、Admin Frontend 局部视觉 token/图标实现
- `admin-frontend/src/views/DashboardView.vue`
- `admin-frontend/src/views/ObservabilityMetricsView.vue`
- `admin-frontend/src/views/ObservabilityLogsView.vue`
- `admin-frontend/src/views/ObservabilityExportersView.vue`
- 直接受新壳层影响的 Events/Alerts/Users/Audit 页面和最小测试
- `frontend/e2e/admin-frontend.spec.ts`、dashboard/partial 相关浏览器测试或等价 Admin Frontend 验收
- 管理中心使用说明或前端产品体验文档
- `dev/logs/Phase-17/Phase-17-01-管理中心前端展示更新.md`
- `VERSION`、`.env.example` 和双 Frontend 版本元数据

视觉基准 PNG 是本批的设计输入，可随规划提交保留；生产 Frontend 不得把这些整页截图作为页面背景或运行时资源。

## 7. 批次验收标准

### 7.1 视觉与信息架构

1. `1440 × 1000` 下的共享侧栏、顶栏、内容背景、标题、卡片、筛选区和状态标签与四张视觉基准保持一致的信息层级与视觉语言。
2. 管理大屏、Metrics、Logs、Exporter 四页均由真实 DTO 渲染；参考图示例数据未被硬编码，事实不足时采用明确的等价表达。
3. Events、告警、用户角色和审计可在新壳层中正常使用，无明显布局断裂或被遮挡操作。
4. 桌面截图逐页人工对照四张基准并记录差异；差异只允许来自真实数据、现有 API 能力、可访问性或响应式约束。

### 7.2 功能与安全回归

1. 四个目标页面的加载、刷新、筛选、分页/加载更多、插件选择及生命周期操作通过既有或直接测试。
2. loading/empty/partial/error/unauthorized 状态有稳定安全展示；长字段不溢出，unknown 不伪装为 0 或 healthy。
3. 普通用户不能挂载管理页面或发出管理 API 请求；超级管理员统一登录、返回社交、退出和角色撤销分流不回退。
4. 浏览器请求仍保持同源，页面与生产 bundle 不包含 Secret、内部凭据或参考图中的示例内部值。

### 7.3 响应式与可访问性

1. `1440 × 1000`、代表性平板宽度和 `390 × 844` 下无不可控水平滚动，导航、筛选、卡片、表格和操作可完成。
2. 键盘可到达导航与主要操作，当前路由、focus、heading、label、status/error 和状态文本可被辅助技术识别。
3. 状态不只依赖颜色；减少动态偏好下无必要动画，刷新/提交期间不会重复触发操作。

完成条件：以上全部通过、无阻断问题、同名实施记录如实完成、版本元数据为 `1.14.1`，本批提交已创建。

## 8. 固定验证命令与回归范围

```bash
(cd admin-frontend && npm test && npm run build)
./scripts/test-frontends.sh
(cd frontend && npx playwright test e2e/admin-frontend.spec.ts e2e/dashboard.spec.ts e2e/dashboard-partial.spec.ts)
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.14.1 --base-ref upstream/main
git diff --check
```

Playwright 门禁使用真实 Compose 产品与超级管理员/普通用户账号。若实现新增专用视觉验收 spec，可将其加入同一条命令；桌面四页截图及窄屏截图只采集一次并由实施记录对照视觉基准，不引入全页像素快照作为跨环境阻断门禁。

本批未修改 Backend/API/数据库/消息/Compose 合同时，不运行 Phase 16 backup/recovery、Migration、Rabbit/Kafka、全产品韧性或 Kubernetes 门禁。若实际修改共享 edge、安全边界或 Compose，必须记录风险原因并追加直接相关的现有产品门禁。

## 9. 实施记录与交接

完成前创建 `dev/logs/Phase-17/Phase-17-01-管理中心前端展示更新.md`，至少记录：

- 四张视觉输入的实际使用方式、真实 DTO 到展示区域的映射和未采用示例项的原因；
- 实际修改文件、共享壳层/组件/token 和响应式策略；
- 每条验证命令、真实浏览器尺寸、截图路径、结果和最小修复；
- 管理员/普通用户、同源、错误/partial、插件生命周期和可访问性结果；
- 与参考图差异、计划偏差、已知限制和非阻断后续项。

交给 Phase-17-02 的固定输入是已合入主线的 `1.14.1` Admin Frontend 展示、共享管理壳层、四页真实数据映射和直接通过证据。后续批次修改错误映射、运行时状态或最终候选时必须保持该管理体验，不以运行时收口回退为旧布局。
