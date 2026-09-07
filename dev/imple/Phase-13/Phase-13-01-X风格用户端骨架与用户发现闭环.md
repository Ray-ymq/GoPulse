# Phase-13-01：X 风格用户端骨架与用户发现闭环实施方案

> 当前状态：待实施。本文档定义 Phase 13 首个执行批次的范围与验收合同；目标版本 `1.10.1`、开发分支 `develop/1.10.1` 和执行顺序以 `Phase-13-总实施方案.md` 的权威分配表为准。实际开工前必须 fetch 主远程并从最新 `origin/main` 创建本批分支，不从规划分支承载功能实现。

## 1. 批次目标

在不提前实现关注、收藏、编辑或删除的前提下，建立后续 Phase 13 功能共同依赖的用户资料、用户发现和独立用户端壳层：

```text
当前登录用户维护 display_name / bio
  → 公开资料 API 与用户帖子列表
  → username / display_name 用户搜索
  → 帖子作者摘要展示可变 display_name
  → UserAppShell 承载首页、搜索、通知、资料和发布入口
```

批次完成必须证明：

- 既有用户迁移后有可用显示名称，username 不可修改，资料校验和公开字段边界稳定。
- 用户可以按 username 或 display name 搜索并进入公开资料页，资料页能分页展示该用户仍存在的帖子。
- 帖子列表、详情和搜索结果中的作者显示名称不会因共享缓存而无限陈旧。
- 普通用户界面形成 X-inspired 的响应式导航与连续时间线基线，管理员页面不被新的壳层或 CSS 作用域改变。

## 2. 前置条件

- Phase 12 已合入并通过 Compose 产品验收；开工时根和 Frontend 版本以最新 `origin/main` 为准。
- 从 `origin/main` 创建 `develop/1.10.1`，确认分支只对应本批权威分配。
- 核对现有 `users`、`posts` migration，user/post/search/http package，Frontend router、认证守卫、`AppNav`、普通用户 views 和 `AdminLayout`。
- 保存现有 Desktop/Mobile 普通用户首页、搜索、通知和管理员代表性页面截图，作为壳层隔离回归基线。
- 若现有注册、登录、帖子列表/详情或帖子搜索已失败，先记录并处理前置阻断，不用本批 UI 重构掩盖。

## 3. 实施范围

### 3.1 用户资料 schema 与领域模型

- 新增顺序 migration，为 `users` 增加 `display_name` 和 `bio`。
- 已有用户以 `username` 回填 `display_name`；完成回填后显示名称为非空。`bio` 采用明确的空值策略，API 对外统一为空字符串或既定 nullable 形式，不混用。
- username 继续参与登录和公开资料路由，不新增修改 username 的 API。
- 在 user model/repository/service 中增加公开资料与本人资料 DTO，不把 password hash、内部凭据或非公开数据带入响应。
- 统一校验：display name trim 后 1–64 个 Unicode 字符；bio trim 后 0–160 个 Unicode 字符。保存和响应使用 trim 后值。

### 3.2 资料与用户发现 API

新增并接入认证路由：

- `PATCH /api/v1/users/me/profile`
- `GET /api/v1/users/:username`
- `GET /api/v1/users/:username/posts`
- `GET /api/v1/search/users?q=...`

具体合同：

- 资料更新只从当前会话取得 user id；缺字段、超长、空显示名称使用既有 validation error 结构。
- 公开资料响应至少包含稳定 user id、username、display name、bio、created time，以及当前 viewer 是否为本人；本批不伪造 follower/following 数和关注状态。
- 用户帖子列表只返回目标用户仍存在的帖子，排序沿用帖子时间线的稳定倒序游标。
- 用户搜索同时匹配 username 和 display name，使用 MySQL 有界查询即可；query 做 trim、长度限制和 escape，limit 有上下界，cursor 使用项目现有签名/稳定编码方式。
- 搜索排序采用可解释且稳定的规则：精确 username 优先，其次前缀/包含匹配，再以稳定 id 打破并列。不得引入未说明的推荐或热度语义。
- 未知 username 返回 not-found；空 query 不触发无界全表扫描，应返回 validation error 或明确空结果，实施时固定一种行为并测试。

### 3.3 作者公开资料水合

- 扩展帖子公开 DTO，使 author 至少包含 id、username 和 display name；现有依赖 username 的导航保持可用。
- `display_name` 是可变字段，不把它作为长期不可失效字段固化在共享帖子缓存。
- 优先在列表/搜索/详情读取后批量水合作者摘要，避免逐条 N+1；若采用用户投影缓存，必须有版本/失效测试并在实施记录说明选择。
- 资料更新后，所有新请求都应读取最新显示名称；不要求修改历史业务事件 payload。

### 3.4 独立 UserAppShell

- 新增独立普通用户壳层，按路由 meta 或明确 layout route 与 `AdminLayout` 分流；两者继续复用认证会话和 API client，但不共享会污染管理页的全局布局选择器。
- Desktop：约 240px 左导航、600–640px 中央时间线、约 320px 右上下文栏；Tablet 收缩图标栏并隐藏右栏；Mobile 单栏、底部导航和发布 FAB。
- 左侧/底部导航提供：首页、搜索、通知、收藏、个人资料；Desktop 提供发布按钮。收藏在本批可以进入明确的“功能将在后续批次开放”或禁用占位，但不得伪造已实现数据。
- 管理员可观测入口从普通用户主导航移入账户菜单；现有管理员守卫、路由和页面内容不改变。
- 建立局部 CSS tokens：
  - 主文字 `#0F1419`
  - 弱化文字 `#536471`
  - 页面背景 `#FFFFFF`
  - 次级背景 `#F7F9F9`
  - 边框 `#EFF3F4`
  - 强调色 `#2563EB`
  - 危险色 `#F4212E`
- 明亮主题为本批验收主题。可以定义暗色变量名，但不实现主题切换或把暗色视觉列入完成条件。

### 3.5 首页、搜索和资料页面

- 首页改为连续时间线行和 1px 分隔线，移除普通用户主路径的大面积渐变、浮空阴影和独立大卡片感。
- 首页 sticky tabs 展示 `全部 / Following`；本批 `Following` 可呈受控未开放状态并指向 Phase-13-02，不得显示“为你推荐”。
- Post row 保留 GoPulse 标题/正文：标题为克制粗体首行，正文随后；动作行只显示已有评论、点赞，以及后续收藏的预留位置，不显示转发/分享/媒体伪功能。
- 使用稳定字母头像，不提供头像上传或编辑入口。
- 搜索页增加 `帖子 / 用户` tabs，保留现有帖子搜索行为并接入用户搜索；两类结果都有 loading、empty、error、cursor 继续加载状态。
- 公开资料页展示字母头像、display name、`@username`、bio、加入时间和该用户帖子；本人资料页提供编辑资料入口。
- 编辑资料使用页面或可访问 modal，具备初始值、字符计数、提交中、字段错误、服务器错误和成功反馈。

### 3.6 可访问性与路由行为

- 导航、tabs、菜单和 icon button 使用语义 label、键盘 focus 和至少 44px 触控尺寸。
- sticky header 不遮挡浏览器焦点；Mobile bottom nav 不覆盖时间线末尾内容。
- 刷新公开资料、用户搜索和本人资料路由时，Frontend container 的 SPA fallback 与认证行为保持正确。
- 未登录访问受保护资料编辑时沿用现有登录重定向；公开资料 API 是否要求登录以总方案现有 protected API 边界为准，本阶段不扩大匿名访问面。

## 4. 不在本批范围

- 真实关注/取消关注、Following 数据、关注/粉丝数或关系列表。
- 真实收藏状态和收藏列表。
- 帖子编辑、删除、历史版本或删除确认。
- 用户 Elasticsearch 索引、用户推荐、热搜、头像媒体、背景图。
- 管理员页面重构或 X 品牌资产复制。

## 5. 建议实施顺序

1. 新增 user profile migration 与 repository/model/service，先用直接 user package 测试固定回填和校验。
2. 实现本人资料更新、公开资料、用户帖子和用户搜索 handler/route，固定游标与错误合同。
3. 扩展帖子 author DTO 和批量资料水合，确认缓存命中/搜索水合仍返回最新 display name。
4. 重构 Frontend route/layout 分流并建立 `UserAppShell`、tokens 和 responsive navigation。
5. 接入搜索 users tab、公开/本人资料页、编辑资料交互和连续时间线 post row。
6. 增加与验收项一一对应的 Backend/Frontend 代表性测试，最后运行本批固定门禁。
7. 更新根与 Frontend 版本为 `1.10.1`，创建对应实施记录并提交。

## 6. 预计直接影响文件

实施时以实际 diff 为准，预计集中在：

- `backend/migrations/*`
- `backend/internal/user/*`
- `backend/internal/post/*`
- `backend/internal/search/*`
- `backend/internal/http/*`
- `backend/internal/platform/redis/*`（仅当作者水合/缓存合同需要）
- `frontend/src/router/*`
- `frontend/src/components/*`
- `frontend/src/views/*`
- `frontend/src/services/api.ts`
- `frontend/src/types/api.ts`
- `frontend/src/styles.css` 或新增用户端作用域样式
- `frontend/e2e/*`（仅本批代表性用户发现闭环）
- `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 及既有受管版本元数据
- `dev/logs/Phase-13/Phase-13-01-X风格用户端骨架与用户发现闭环.md`

不得借机格式化或重写无关管理页、Compose、可观测服务或后续批次文件。

## 7. 批次验收标准

### 7.1 数据与 API

- migration up 后每个既有用户都有以 username 回填的 display name；down/up 在空测试库可重复执行。
- 本人可更新合法 display name/bio；空显示名称和超长字段被拒绝，username 未改变。
- 用户 B 可以按用户 A 的精确 username、新 display name 和合理部分匹配找到 A；结果顺序稳定且分页无重复/遗漏代表性问题。
- 公开资料只包含允许字段；未知用户 not-found；用户帖子列表不混入其他作者帖子。
- 资料更新后，从帖子列表、详情、帖子搜索和用户资料页发起的新请求均显示最新 display name，包括已有 Redis 帖子缓存命中的代表性路径。

### 7.2 Frontend

- Desktop、Tablet 和 Mobile 的 UserAppShell 结构符合总方案；首页为连续时间线，只有 `全部 / Following`，不存在“为你推荐”、转发或分享伪功能。
- 搜索页的帖子/用户 tabs 可键盘操作，用户结果可进入资料页；资料编辑具有校验、失败恢复和成功状态。
- 本人与他人资料视觉动作不同，本批不展示虚假的关注数或公开关系列表入口。
- 管理员代表性可观测页面仍使用原 AdminLayout，导航、路由守卫和主要样式未被用户端 CSS 覆盖。

### 7.3 完成条件

本节全部通过、无阻断问题，实施记录真实完整，版本元数据为 `1.10.1`，且最终提交只包含本批文件时，Phase-13-01 才可标记完成。非阻断暗色主题、更多动画、用户排序优化和头像能力记录为后续并停止扩展本批。

## 8. 固定验证命令与回归范围

实施中按最小范围运行；下列完成门禁只对最终 diff 运行一次：

```bash
cd backend
go test ./internal/user ./internal/post ./internal/search ./internal/http

go test -tags=integration ./internal/user ./internal/post ./internal/search

cd ../frontend
npm test
npm run typecheck
npm run build
npm run test:e2e -- --grep "profile|user search|user shell"

cd ..
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.10.1 --base-ref origin/main
git diff --check origin/main...HEAD
```

- 集成命令在仓库规定的隔离测试环境中设置 `INTEGRATION_TESTS=1` 与白名单 MySQL/Redis 配置；若测试按 package/name 分布调整，实施记录必须写出实际等价命令，不能把未运行命令记为成功。
- 回归范围固定为注册/登录、帖子列表/详情、帖子搜索、普通用户路由和一条管理员页面访问。
- 只有作者水合修改共享 Redis 结构、认证路由或全局 CSS 导致具体风险时，才扩展对应缓存/全栈/更多管理员检查，并先在实施记录写明原因。
- 已成功检查在相关代码、配置、依赖和环境未变化时不重复运行。

## 9. 实施记录要求

完成前创建：

`dev/logs/Phase-13/Phase-13-01-X风格用户端骨架与用户发现闭环.md`

记录必须包含实际 migration/API/UI 选择、实际文件、实际验证命令和结果、与本方案偏差、已知限制，以及交给 Phase-13-02 的用户 id、资料 DTO、布局和路由合同。
