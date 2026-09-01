# Phase 1-06：Frontend 业务闭环与阶段收口实施方案

> 执行序号：6 / 6
> 前置批次：Phase 1-01 至 Phase 1-05 已完成并通过验收
> 总方案来源：[Phase-01-总实施方案.md](Phase-01-总实施方案.md)

## 1. 批次目标

建立 Vue Router 业务路由、认证状态恢复、注册/登录、帖子列表/发布/详情、评论和点赞页面，将前五批 Backend 能力组成可从浏览器完整操作的最小业务闭环。

本批同时扩展跨平台验收入口，执行真实 MySQL/Redis、Backend 重启、Redis 故障与恢复的完整验收，更新阶段文档、实施记录和根 `VERSION`，完成 Phase 1 收口。

## 2. 前置条件

- Phase 1-05 已合并到配置的主远程 `main`，根 `VERSION` 与前一批目标版本一致。
- 已从该远程最新 `main` 创建总方案为本批分配的开发分支。
- Phase 1 全部 Backend API、状态码、错误码和 Cookie 语义已稳定。
- Backend 单元测试、Repository 真实 MySQL 测试和 Redis 降级验收已通过。
- Phase 0 Frontend 测试、类型检查、构建、Vite 代理和开发脚本已可用。
- Phase 1-01 至 Phase 1-05 实施记录已完成。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 Router 与认证状态

引入与现有 Vue 版本兼容的 Vue Router，建立：

```text
/register
/login
/posts
/posts/new
/posts/:postId
```

- 根路径根据认证状态导航到 `/posts` 或 `/login`。
- `/register` 和 `/login` 为匿名页，已登录用户访问时导航到 `/posts`。
- 业务页为受保护页，未登录时导航到 `/login`。
- 首次导航必须等待 `/api/v1/users/me` 恢复完成，区分“未初始化”、“已登录”和“未登录”，避免刷新跳转闪烁。
- 使用轻量 `useAuth` composable 共享当前用户与初始化 promise，不引入 Pinia。

### 3.2 HTTP Service

- 统一使用同源 `/api/v1` 相对路径和 Cookie 凭据。
- 建立通用成功/错误响应解析，为所有 API 定义 TypeScript DTO，不使用无约束 `any`。
- HTTP 204 不尝试解析 JSON。
- HTTP 401 清除内存认证状态并导航到登录页，不解析、保存或自行续期 JWT。
- 业务错误保留稳定 `code` 供页面映射，未知错误使用安全通用提示。

### 3.3 页面与交互

#### 注册与登录

- 显示用户名、密码字段及最小前端校验。
- 提交期间禁用重复提交，显示字段错误、用户名冲突、凭据错误和网络错误。
- 注册或登录成功后更新 `useAuth` 并导航到 `/posts`。
- 退出从共享导航入口触发，成功后清除用户并导航到 `/login`。

#### 帖子列表与发布

- 列表显示标题、内容摘要、作者、时间、评论数和点赞数。
- 支持首页加载与基于 `next_cursor` 的加载更多，防止并发加载相同游标。
- 明确显示加载、空列表、加载失败和已无更多数据状态。
- 发布页校验标题与正文，成功后导航到新帖子详情。

#### 帖子详情、评论和点赞

- 详情显示帖子全文、作者、时间、评论数、点赞数和当前用户点赞状态。
- 评论列表按 Backend 顺序显示，支持游标加载更多。
- 评论提交期间禁用重复提交，成功后清空输入并重新获取详情与评论首页。
- 点赞按钮根据 `liked_by_me` 调用明确的 `PUT` 或 `DELETE`，请求期间禁用二次操作。
- 点赞成功后重新获取详情，不将未被服务器确认的本地计数作为长期状态。

### 3.4 Phase 0 连通性页面

保留 `/health` 和 `/ready` Backend 契约。Phase 0 连通性 UI 按总方案作出明确决策：

- 优先移到不出现在业务主导航的 `/dev/status` 路由，用于本地诊断。
- 如现有组件不适合保留，可移除 UI，但必须保留 Backend 契约、组件状态映射测试中仍有价值的通用部分，并在实施记录说明决策。

### 3.5 集成验收与文档收口

- 保持 PowerShell 与 Bash 默认 `verify` 为只读运行状态检查，可通过未认证访问受保护 API 应返回 401 验证业务路由和认证中间件可达，但不创建业务记录。
- 为 PowerShell 与 Bash 提供独立的完整业务验收入口。`verify-business` 不复用日常 `gopulse` Compose project、`.run` 记录、Backend/Frontend 端口、数据库或 Redis DB，而是创建名称含随机令牌且匹配严格白名单的独立验收资源。
- 验收令牌使用 12 位小写十六进制；Compose project 必须匹配 `^gopulse-acceptance-[a-f0-9]{12}$`，数据库必须匹配 `^gopulse_acceptance_[a-f0-9]{12}$`。所有端口只绑定回环并与日常开发端口分离，Backend/Frontend 使用临时进程目录和显式环境覆盖，不修改用户 `.env`。
- Redis 使用验收 Compose project 自有实例和数据卷；脚本只允许清空该实例的验收 Redis DB，禁止对未验证归属的地址执行 `FLUSHDB`，并无条件禁止 `FLUSHALL`。
- 需要停止 Redis、重启 Backend 或操作容器的破坏性故障矩阵只作用于经过 project label、容器 ID 和端口三重校验的验收资源，不放入默认 `verify`。
- `verify-business` 在成功、失败和用户中断时都恢复/停止自己启动的进程，并只对已验证的验收 project 执行 `down --volumes`；启动前拒绝默认开发数据库名、默认 Redis 目标、空令牌、非法 project 名和任何非回环发布地址。
- 通过负向测试验证错误目标会在执行任何删除、清空、停止或重启前失败；在日常开发栈并行运行时执行验收，确认其容器、进程记录、数据卷和 `.env` 前后不变。
- 完善 README 的当前功能、迁移、配置、页面、开发命令、验收命令和已知限制。
- 核对六份实施记录都存在且与实际工作相符。
- 在全部验收通过后，将根 `VERSION` 从前一批版本更新为总方案为本批分配的目标版本。

## 4. 明确不做的内容

- 不引入 Pinia、大型 UI 组件库、完整设计系统或复杂动画。
- 不实现服务端渲染、PWA、离线缓存或 Frontend 容器化。
- 不实现用户资料、头像、关注、收藏、搜索或通知页面。
- 不在 Frontend 保存或解析 JWT。
- 不为了 UI 便利改变已稳定 Backend 事实与错误契约；发现 Backend 缺陷时回到所属批次修复并重新验收。
- 不引入 RabbitMQ 业务消息或 Phase 2 能力。

## 5. 目标文件和目录

```text
frontend/package.json
frontend/package-lock.json
frontend/src/router/
frontend/src/views/
frontend/src/components/
frontend/src/composables/
frontend/src/services/
frontend/src/types/
frontend/src/**/*.test.ts
frontend/vite.config.ts
scripts/verify.ps1
scripts/verify.sh
scripts/verify-business.ps1
scripts/verify-business.sh
.github/workflows/quality-gates.yml
scripts/ci/
README.md
VERSION
dev/logs/Phase-01/Phase-01-06-Frontend业务闭环与阶段收口.md
```

如前置批次验收暴露 Backend 缺陷，可修改对应 Backend 文件，但必须在 Phase 1-06 记录中说明修复归属并重新执行受影响批次验收。

## 6. 详细实施步骤

1. 检查前五批交接物、实施记录、Backend API 契约和 Git 状态。
2. 安装并固定 Vue Router，更新 lockfile。
3. 建立业务 DTO、通用 HTTP 客户端、API Service 和错误映射。
4. 实现 `useAuth` 的单次初始化、注册、登录、退出和 401 处理。
5. 实现 Router 与匿名/受保护导航规则，覆盖页面刷新恢复。
6. 实现注册、登录、共享导航和退出交互。
7. 实现帖子列表、加载更多、发布帖子和详情页。
8. 实现评论列表/发布和点赞/取消点赞交互。
9. 处理 Phase 0 连通性 UI，确保 Backend 健康契约与有效测试不丢失。
10. 使用 mock HTTP 覆盖认证、路由、分页、表单、评论、点赞和错误状态。
11. 扩展 PowerShell/Bash 默认 `verify` 的只读业务路由检查。
12. 实现使用独立 Compose project、回环端口、数据库、Redis 和临时进程目录的 `verify-business`，保持两平台退出码、故障恢复和清理语义一致。
13. 为验收目标白名单、归属校验、非回环地址、默认开发资源、清理范围和中断恢复增加自动化负向测试。
14. 更新 GitHub Actions 质量门禁，将 `verify-business.sh` 纳入 LF/Bash 语法检查，将 `verify-business.ps1` 纳入 PowerShell AST 检查，并运行不需要 Docker 的安全负向测试。
15. 运行 Backend 单元/integration 测试与 vet、Frontend 测试/类型检查/生产构建和开发脚本回归。
16. 在日常开发栈并行存在时，从空验收业务库执行完整业务流程、Backend 重启、验收 Redis 清空、故障与恢复验收，比较前后开发栈状态。
17. 在浏览器中验证真实页面渲染和交互，不以 HTML 200 替代浏览器验收。
18. 更新 README，创建 Phase 1-06 实施记录，核对前五份记录。
19. 将根 `VERSION` 更新为总方案为本批分配的目标版本，检查暂存范围并完成阶段提交。

## 7. 测试与验收标准

### 7.1 Frontend 自动化测试

- 首次认证恢复期间不发生误导航，恢复成功/失败进入正确页面。
- 注册、登录、退出、用户名冲突和错误凭据状态正确。
- 列表加载、空状态、错误、加载更多和无更多数据正确。
- 发布帖子成功与失败状态正确。
- 详情 404、评论加载/发布、点赞/取消和重复提交防护正确。
- HTTP 204 不解析 JSON，401 清除认证并导航。
- 类型检查、Frontend 测试和 Vite 生产构建全部通过。

### 7.2 完整业务流程

1. 从空测试业务库向上迁移并启动 Frontend、Backend 和基础设施。
2. 从浏览器注册用户，验证自动登录和刷新后状态恢复。
3. 退出后受保护页不可访问，重新登录后恢复。
4. 发布多条帖子，验证列表顺序、加载更多和详情。
5. 发表多条评论，验证评论顺序、分页与计数。
6. 重复点赞与取消点赞，验证幂等、计数和按钮状态。
7. 重启 Backend，验证用户、帖子、评论和点赞事实仍存在。
8. 清空 Redis，验证业务数据不丢失并能重建缓存。
9. 停止 Redis，验证 `/ready` 报告故障，但浏览器业务闭环仍可操作。
10. 恢复 Redis，无需重启 Backend 即恢复 readiness 和缓存能力。

### 7.3 工程与文档

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go test -count=1 -tags=integration ./...` 在隔离 MySQL/Redis 环境通过，且真实依赖缺失不得静默 skip。
- Frontend 测试、类型检查和生产构建通过。
- PowerShell 与 Bash `dev`、`down`、`verify`、`verify-business` 语义一致，默认 `verify` 不改变业务数据，完整业务验收只使用专用验收数据库；已执行的平台和未能执行的限制如实记录。
- `verify-business` 的 project、容器、数据库、Redis、端口和进程目录均与日常开发栈隔离；成功、失败和中断后无遗留验收进程、容器、网络或数据卷。
- 默认开发数据库、默认 Redis、非法/空 project 名、非回环发布地址和归属不匹配目标全部在任何破坏性动作前被拒绝。
- 日常开发栈并行存在时，验收前后容器 ID/状态、`.run` 记录、数据库事实、Redis 数据和具名卷保持不变。
- GitHub Actions 在 Linux/Windows 分别检查新增 Bash/PowerShell 脚本，并执行跨平台共享的安全目标负向测试。
- README 中的命令、端口、路由、配置和缓存限制与实现一致。
- 六份实施记录存在，不将未执行验收写为已通过。
- `VERSION` 等于总方案为 Phase 1 最终批次分配的目标版本。

## 8. 完成定义

- 用户可在 Frontend 完成注册、登录、发帖、查询、评论、点赞和取消点赞。
- Backend 重启后核心数据仍存在且来自 MySQL。
- Redis 清空、宕机和恢复不造成业务事实丢失。
- 故障注入和清理只作用于脚本本次创建且归属已验证的验收资源，不影响并行运行的日常开发环境。
- Backend、Frontend、迁移、脚本和真实基础设施验收均已完成并如实记录。
- `/health` 和 `/ready` 保持 Phase 0 契约。
- 六份实施记录、README 和 `VERSION` 已收口。
- 未引入 Phase 2 或后续阶段能力。
- 仅提交 Phase 1 实际变更，不包含用户或其他任务改动。

## 9. Phase 2 交接条件

交付给 Phase 2 前必须提供：

- 评论、点赞和取消点赞的明确 Service 成功边界。
- 以 MySQL 成功为准的同步事实写入与稳定业务 ID。
- 可映射为 `comment.created`、`post.liked`、`post.unliked` 的已提交业务操作。
- RabbitMQ 故障不得影响已提交 MySQL 事实的明确边界约束。

Phase 2 只能在核心事实写入之后增加异步动作，不得将 RabbitMQ 变成评论或点赞的最终事实来源。
