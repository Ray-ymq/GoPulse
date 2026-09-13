# Phase-16-03：统一登录与双 Frontend 产品体验闭环实施方案

> 目标版本：`1.13.3`  
> 开发分支：`develop/1.13.3`  
> 运行与验收平台：真实 Linux `amd64`

## 1. 批次目标

在 Phase-16-02 的稳定生命周期和唯一 edge 上，将用户 Frontend、管理 Frontend 与 Backend API 收口为同一 origin 的完整产品体验：

- 一个共享登录入口、统一 session/cookie/CSRF/登出语义和明确角色分流。
- 两个 Frontend 共用设计 token、基础组件和错误状态，不复制漂移实现。
- 核心用户与管理流程覆盖 loading、empty、partial、stale、permission denied 和 backend unavailable。
- Linux 浏览器验收覆盖桌面与窄屏、键盘访问、焦点和非 UTC 时间展示。

## 2. 前置条件

- 从 Phase-16-02 合入后的最新 `upstream/main` 创建 `develop/1.13.3`。
- 产品 Bundle 可在 Linux `amd64` 通过唯一 edge 启动；Backend 不发布独立宿主端口。
- 登录、刷新、登出、当前用户、角色和 CSRF 的公开 API 合同可核对。
- 浏览器 acceptance runner 可通过产品 Compose profile 调用，不要求宿主 Node.js。
- 用于角色、业务、告警、审计和错误状态的最小固定 fixture 可重复建立。

## 3. 实施范围

### 3.1 共享前端基础

- 建立两个 Frontend 共同使用的颜色、排版、间距、层级、断点、focus、loading、empty、error 和 stale token。
- 抽取确有共同语义的按钮、表单、提示、导航、状态标签和时间展示；保留业务页面边界。
- 构建产物继续分别进入两个产品镜像，不要求用户在宿主构建。

### 3.2 统一登录与角色分流

- 同一 edge origin 提供登录、session 恢复、刷新、登出和 CSRF。
- 未登录访问受保护页面跳转统一登录；登录后按角色回到安全目标。
- 普通用户不能进入管理 Frontend 或管理 API；管理员可访问管理能力。
- 重定向目标必须同源且经过 allowlist，防止开放跳转。
- session 失效、权限变化和后端不可用时清理陈旧身份并提供可恢复状态。

### 3.3 页面与交互状态

- 用户侧覆盖首页/目标详情/指标/事件/日志/告警/操作历史等核心路径。
- 管理侧覆盖用户、角色、插件、目标和必要运维状态。
- 对 loading、empty、partial、stale、permission denied 和 backend unavailable 建立一致呈现。
- 危险操作显示明确对象、影响和结果；不在通知或错误中暴露 Secret。

### 3.4 响应式、键盘与时间

- 桌面与窄屏下导航、表格/卡片、筛选、详情和对话框可用，无关键控件被遮挡。
- 所有核心操作可通过键盘完成；焦点顺序、可见 focus、label、heading 和 live region 符合基础可访问要求。
- API 保持 UTC/RFC3339 事实；Frontend 统一按用户或浏览器时区展示，并保留必要的原始时间提示。
- 固定至少一个非 UTC 代表场景，验证跨日边界、筛选和审计时间一致。

### 3.5 Edge 与安全 headers

- Edge 正确路由用户 Frontend、管理 Frontend、API、静态资源和 fallback。
- 校验 cookie Path/SameSite/Secure、CSRF、CSP、frame、content-type 和 cache headers。
- 前端不保存明文凭据，不把内部服务地址或敏感配置写入 bundle。

## 4. 不在本批范围

- backup/restore、`1.9.4` upgrade、持久层迁移或最终阶段收口。
- macOS、Windows、`linux/arm64` 浏览器和运行验收。
- 全站视觉重设计、国际化平台、离线 PWA 或任意浏览器全排列。

## 5. 建议实施顺序

1. 锁定同源路由、会话、角色和错误合同。
2. 建立共享 token 与最小公共组件。
3. 实现统一登录、恢复、刷新、登出和安全重定向。
4. 收口用户与管理核心页面状态。
5. 完成 responsive、keyboard、timezone 和 headers。
6. 在 Linux 产品 Bundle 中运行角色/业务/管理浏览器矩阵。
7. 运行固定门禁，更新实施记录和 `VERSION`，提交后停止。

## 6. 预计直接影响文件

- 两个 Frontend 的共享包、页面、状态和测试
- Edge 路由与安全 header 配置
- Backend 身份/session/CSRF 接口的直接必要调整
- acceptance browser runner 与固定 fixture
- 产品使用文档
- `dev/logs/Phase-16/Phase-16-03-统一登录与双Frontend产品体验闭环.md`
- `VERSION`

## 7. 批次验收标准

1. 两个 Frontend 从共享基础构建，Bundle 不包含源码工具或开发 server。
2. 未登录、普通用户、管理员、session 失效、登出和安全回跳结果正确。
3. 用户对管理页面和 API 的越权请求稳定拒绝，且不泄露内部事实。
4. 两个 Frontend 的核心页面覆盖规定状态，并可在后端不可用后恢复。
5. 桌面与窄屏无关键阻断；键盘路径、焦点、语义标签通过。
6. 非 UTC 场景的列表、详情、筛选和审计时间一致。
7. 唯一 edge、cookie/CSRF/CSP/cache 等直接安全合同通过。
8. 六插件、三源告警和 Phase-16-02 生命周期的直接回归通过。
9. 根与受管版本更新为 `1.13.3`，同名实施记录完整且无阻断问题。

## 8. 固定验证命令与回归范围

```bash
scripts/test-frontends.sh
scripts/verify-product-lifecycle.sh --platform linux/amd64 --reuse-install
docker compose --profile acceptance run --rm acceptance frontend-contract
docker compose --profile acceptance run --rm acceptance frontend-browser --viewport desktop --timezone Asia/Shanghai
docker compose --profile acceptance run --rm acceptance frontend-browser --viewport narrow --timezone Asia/Shanghai
scripts/verify-compose.sh
```

命令可按实际仓库入口等价调整并记录。浏览器矩阵只覆盖明确角色、关键页面、两个 viewport 和一个非 UTC 代表场景，不扩张为全页面/全浏览器覆盖活动。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-16/Phase-16-03-统一登录与双Frontend产品体验闭环.md`，记录真实浏览器/runner、角色 fixture、viewport、timezone、edge、headers、失败轮次、验证结果和偏差。

交给 Phase-16-04 的固定输入是唯一 edge、稳定身份与角色合同、两个 Frontend 的关键事实，以及 lifecycle 可读取的安装 state。

