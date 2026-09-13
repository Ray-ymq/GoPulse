# Phase-16-03 实施记录

## 状态

目标 `1.13.3`，分支 `develop/1.13.3`，从更新后的 `origin/main` (`5499a4f`) 创建；origin 与计划中的 upstream 指向同一仓库。**实施中，未完成验收，不宣称本批完成。** 根 VERSION 暂保留完成基线 `1.13.2`。预先提交实现快照仅用于现有 Compose 门禁的 committed-source 输入要求。

## 实际变更

- `frontend-shared/`：共享全局样式、focus/窄屏基础、时间格式函数、带原始 RFC3339 提示的时间组件和六类状态组件；两个 Frontend 的 Docker 构建均显式复制共享输入。
- 两个 Frontend 的样式、Vite/TypeScript 模块解析和日期工具引用共享基础；Events/Logs 使用共同 loading/empty/stale/unavailable 状态，审计和记录时间统一按浏览器时区展示。
- 用户登录回跳使用已实现页面 allowlist；认证刷新失败清理陈旧身份及关联缓存；401 回登录保留目标。管理会话刷新失败清理身份，管理壳新增登出和键盘跳至内容。
- 唯一 edge 增加 CSP、frame、content-type、Referrer-Policy 和 no-store；没有更改 Backend 的 cookie/session/CSRF 合同。
- `frontend-product.spec.ts` 增加同源真实角色、桌面/窄屏、键盘、审计时区筛选、登出、失效及恢复矩阵；复用现有 Compose acceptance profile，在完整回归管理 fixture 建立后运行。

## 已执行检查

- 首次最小 `frontend` redirect/auth Vitest 检查通过。
- `scripts/test-frontends.sh` 两轮均通过（第二轮对应新增时间及页面状态代码）：用户侧 18 文件/64 测试，管理侧 8 文件/35 测试；两端类型检查和生产构建通过。第二轮输出 `/tmp/gopulse-p1603-frontends-final.log`。
- 新增 Playwright spec 通过 `npx tsc --ignoreConfig --noEmit --skipLibCheck --moduleResolution bundler --module esnext --target es2022 --types node e2e/frontend-product.spec.ts`。
- `git diff --check` 通过。
- 实际 Docker server 为 Linux x86_64；没有触碰其他 Docker 项目和未跟踪用户文件 `~`。

## 验收入口差异与待完成事项

- 仓库没有 `frontend-contract` / `frontend-browser` 子命令；实际 acceptance ENTRYPOINT 是 Playwright。等价入口为 `acceptance e2e/frontend-product.spec.ts`，通过 `GOPULSE_VIEWPORT=desktop|narrow` 选择 viewport，固定 `Asia/Shanghai`；已嵌入 `scripts/verify-compose.sh` 的管理场景。不以 spec 加载代替浏览器执行。
- `verify-product-lifecycle.sh` 没有 `--reuse-install`；现有真实入口为 `--clean-install`。后续记录实际采用的候选和结果。
- 尚待真实容器浏览器、完整 Compose（含六插件）、生命周期直接回归及三源告警结果；通过后才更新受管版本并标记完成。
- 按仓库事实，用户 Frontend 是社交业务，指标/目标/事件/日志/告警/审计在管理 Frontend；不新增一套普通用户可观测页面或越权 API。
