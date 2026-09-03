# Phase 6-01：管理员身份与双用户态授权边界实施方案

> 执行序号：1 / 4
>
> 前置阶段：Phase 5 已完成并通过验收
>
> 总方案来源：[Phase-06-总实施方案.md](Phase-06-总实施方案.md)

## 1. 批次目标

在不建立第二套账号、密码或会话协议的前提下，为 GoPulse 建立“普通用户社交业务态”和“管理员可观测管理态”的持久化身份与服务端授权边界。

本批纵向交付“普通用户注册/登录 → 当前用户稳定返回 `role=user` → 服务器运维 CLI 显式提升 → 同一账号和会话被识别为 `role=admin`”闭环，并提供后续管理与查询接口统一复用的 admin 授权中间件。管理员是普通用户能力的权限超集：可以继续使用社交业务；普通用户不得获得任何可观测查询或管理权限。

## 2. 前置条件

- Phase-05-01 与 Phase-05-02 已合入主远程 `main`，根与 Frontend 版本均为 `1.2.2`，实施记录和远程门禁齐全。
- Backend 现有注册、登录、JWT/Cookie、`/api/v1/users/me`、用户 Repository 与数据库迁移契约已重新核对。
- 已 fetch 主远程并从包含 Phase 5 全部结果的最新 `main` 创建 `develop/1.3.1`，没有沿用 `update` 或 Phase 5 分支。
- 在 WSL2 Linux filesystem 实施，具备 Go、Bash、MySQL 和 Frontend 测试环境。
- 开始前记录数据库、端口、进程和 Git 快照；不更改或清理不属于本批的资源。

## 3. 实施范围

### 3.1 持久化角色与迁移

- 新增可逆顺序迁移，将 `users.role` 定义为非空枚举语义字段，有效值固定为 `user|admin`，默认值为 `user`。
- 迁移后的全部既有用户仍为 `user`；注册用户始终为 `user`，不自动提升首个、最早或指定用户名用户。
- 用户领域模型和 Repository 保存并读取角色；角色非法或数据库读取失败时拒绝授权，不降级为 admin。
- 业务作者摘要继续只包含 `id` 和 `username`，帖子、评论、通知、搜索文档与公开缓存不得暴露 `role`。

### 3.2 管理员引导 CLI

- 新增服务器运维命令 `go run ./cmd/admin-role promote --username <username>`，只允许把已注册用户从 `user` 显式提升为 `admin`。
- CLI 使用与登录一致的 username 规范化规则；用户不存在时非零退出，重复提升幂等成功，并且不打印密码、token、数据库 DSN 或用户隐私字段。
- Phase 6 不提供网页赋权、公开用户管理 API、环境变量自动赋权或默认管理员账号；需要多个管理员时由运维人员逐个执行 CLI。
- 本批不增加降级、禁用、删除和角色列表能力；这些能力必须在后续独立设计审计与恢复语义后才能进入范围。

### 3.3 当前用户契约与会话语义

- 注册、登录和 `/api/v1/users/me` 的当前用户 DTO 增加 `role`，固定返回 `user` 或 `admin`。
- JWT 与 Cookie 格式保持不变，JWT 继续只承载稳定用户 ID；Backend 每次需要角色时读取数据库当前值，不信任客户端字段或可能过期的 token role claim。
- 用户被提升后无需建立第二套管理员会话；现有有效 Cookie 的后续 `/users/me` 与授权判断应反映数据库当前角色。
- Frontend `PublicUser`、响应校验、认证状态恢复与持久化适配 `role`，但本批不增加管理导航、管理路由或可观测页面。

### 3.4 双用户态权限模型

后续 Phase 必须复用下列权限矩阵，不得在具体页面或接口中重新解释：

| 能力域 | 未登录 | `user` | `admin` | 内部服务身份 |
| --- | --- | --- | --- | --- |
| 社交公开能力 | 按既有契约 | 按既有契约 | 按既有契约 | 不适用 |
| 发帖、评论、点赞、通知等登录业务 | `401` | 允许 | 允许 | 不适用 |
| Metrics、Logs、Events 查询 | `401` | `403 permission_denied` | 允许 | 按内部契约 |
| Exporter 安装、查询与生命周期管理 | `401` | `403 permission_denied` | 允许 | Monitor token |
| Monitor、Router、Marshaller 与存储内部接口 | 拒绝 | 拒绝 | 浏览器不直连 | 独立服务鉴权与受控网络 |

- 新增可复用 admin 授权中间件与 `permission_denied` 安全错误；未登录固定为 `401 authentication_required`，已登录但非 admin 固定为 `403 permission_denied`。
- 中间件必须在认证完成后根据数据库当前角色授权，失败时不得调用下游 handler、Monitor 或存储服务。
- Frontend 未来根据 `role` 控制导航和路由只是体验层；Backend 服务端授权始终是安全边界。
- Phase 6 后续管理 API、Phase 8 指标查询、Phase 9 日志查询、Phase 10 事件查询和 Phase 11 管理控制台都必须继承本契约。

## 4. 实施边界与非目标

- 不实现插件包、Monitor、Exporter 生命周期、MetricsMonitor、消息 Envelope 或 Publisher；这些分别由 Phase-06-02/03 完成。
- 不实现独立管理员登录、第二套用户表、第二种 Cookie、通用 RBAC、细粒度权限、组织/租户或用户管理页面。
- 不改变社交业务 API 的既有可见性、作者 DTO、缓存键、搜索文档或通知消息格式。
- 不把角色作为前端自行决定权限的依据，不新增只能靠隐藏按钮保护的敏感能力。
- 不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 预计文件与交付物

```text
backend/migrations/**
backend/internal/user/**
backend/internal/auth/**
backend/internal/http/**
backend/internal/apperror/**
backend/cmd/admin-role/**
frontend/src/types/api.ts
frontend/src/services/api.ts
frontend/src/composables/useAuth.ts
scripts/ci/**
.github/workflows/quality-gates.yml
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-06/Phase-06-01-管理员身份与双用户态授权边界.md
```

预计文件是允许边界，不要求制造无意义修改。实际未修改文件不得写入实施记录；超出边界的需求必须先判断是否直接阻断本批验收。

## 6. 详细实施步骤

1. 核对 Phase 5 最终主线、用户迁移序号、Repository、认证中间件、当前用户 DTO 与 Frontend 会话恢复路径。
2. 增加 `users.role` up/down 迁移、领域字段、Repository 读取与受限提升写入，验证既有行与新注册用户默认均为 `user`。
3. 实现 `admin-role promote` 的参数校验、username 规范化、用户查找、幂等提升和脱敏输出。
4. 增加 `permission_denied` 和可复用 admin 中间件，使用代表性 handler 测试证明未登录、普通用户、管理员三种结果及下游调用边界。
5. 更新注册、登录和 `/users/me` DTO，确保角色变化由数据库即时反映，公开作者 DTO 保持不变。
6. 更新 Frontend 当前用户类型、响应校验和认证恢复，定向回归注册、登录及既有社交页面。
7. 更新 README 中的两种使用态、管理员提升方式和权限矩阵，不把未实现的管理页面写为已交付。
8. 执行第 8 节固定门禁，将根与 Frontend 版本更新为 `1.3.1`，创建同名实施记录并只记录真实结果。

## 7. 风险与控制

- **首个用户隐式成为管理员**：数据库和注册路径统一默认 `user`，只接受服务器运维 CLI 显式提升。
- **角色变更后旧 token 越权**：JWT 不承载授权事实，敏感请求按用户 ID 查询数据库当前角色。
- **只做前端隐藏**：Backend 中间件是唯一授权裁决点，前端导航仅改善体验。
- **管理员身份公开泄漏**：当前用户 DTO 可见 role，所有作者摘要、搜索文档、缓存和通知继续使用公开用户模型。
- **角色功能扩张成用户管理系统**：本批 CLI 只支持 promote，不增加列表、降级、禁用、删除或网页操作。
- **身份改造破坏社交业务**：回归既有认证、作者响应和代表性社交闭环；只在观察到共享边界风险时扩大检查。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败修复后只重跑受影响的命令或场景：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.3.1 --base-ref upstream/main
git diff --check
```

迁移测试至少覆盖 up/down、既有用户默认值、新注册用户默认值和非法角色约束；授权测试用一个代表性受保护 handler 覆盖 `401/403/允许` 三种结果，不为每个未来路由复制相同用例。

若角色字段实际进入共享缓存、搜索或异步消息路径，补跑受影响的既有业务脚本并在实施记录中写明扩展原因；否则不把完整可观测或插件验收提前到本批。

## 9. 验收标准

- 迁移 up/down 成功，既有用户和新注册用户默认均为 `user`，非法角色无法持久化。
- 指定已注册用户只能通过运维 CLI 显式提升为 `admin`；用户不存在失败，重复提升幂等，多管理员可逐个建立。
- 注册、登录和 `/users/me` 稳定返回 role；提升后同一账号和会话可读取数据库最新角色。
- 未登录访问代表性 admin handler 返回 `401`，普通用户返回 `403 permission_denied`，管理员通过，且拒绝路径不调用下游能力。
- 普通用户与管理员都可继续按既有规则使用社交业务；公开作者摘要、搜索和通知不暴露 role。
- Frontend 既有注册、登录、会话恢复和社交页面适配新字段且无回归，本批没有伪造未实现的管理入口。
- 第 8 节固定验证与远程门禁通过，版本元数据为 `1.3.1`，同名实施记录真实完整。

## 10. 明确完成条件

只有角色迁移、运维提升、当前用户契约、数据库实时授权、`401/403/允许` 边界、公开身份脱敏和既有社交回归全部通过，且没有阻断验收的失败，才可标记 Phase-06-01 完成。只有数据库新增字段、前端隐藏元素或在测试中手工构造 admin 不足以完成本批。

## 11. 下一批交接

- 已合入的 `users.role`、admin 中间件、`permission_denied`、运维提升 CLI 和当前用户 `role` 契约。
- 固定的双用户态权限矩阵：社交能力按既有规则共享，可观测查询与管理仅 admin，内部服务不对浏览器开放。
- Phase-06-02 必须在全部插件公共管理路由复用 admin 中间件，并验证普通用户请求不会到达 Monitor。
- Phase-06-03 只消费稳定插件状态形成采集目标，不扩大用户授权模型。
