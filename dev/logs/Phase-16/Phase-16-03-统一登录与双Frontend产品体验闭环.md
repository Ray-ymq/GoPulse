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

## 安全合同直接缺口修正

实施中核对 Backend 路由发现没有显式 CSRF 来源校验，因此新增 `middleware.SameOrigin`，覆盖包含登录/登出的非只读请求。edge 与 Vite proxy 保留原始 Host（含端口），防止同源浏览器请求被反向代理改写后误拒。没有 Origin/Fetch Metadata 的自动化客户端保持原合同；拒绝 cross-site/same-site 非同源浏览器写入和不匹配 Origin。错误不包含内部服务事实。

用户侧登出原先 finally 清身份，会在服务端未确认退出时丢失重试上下文；改为仅在服务端成功后清空，与管理端一致。新增代表性失败后重试成功测试，`src/composables/useAuth.test.ts` 9 项通过。新增跨源请求测试以及 `go test ./internal/http/middleware ./internal/http` 实际通过（输出 `/tmp/gopulse-p1603-csrf.log`）。

因为此次修改的是共享请求安全边界，最终 Compose 门禁必须使用新 Backend/edge 重跑，而不能用正在进行的第一轮旧快照验收替代。这是重跑的具体风险依据，不是扩大通用审计范围。三源告警直接回归复用现有 `phase15-closure.spec.ts --grep 'create exact three-source'`，只验证受影响的浏览器创建和管理链，不重跑未修改的整个历史阶段。

安全修正后的 `scripts/test-frontends.sh` 全部通过（18 文件/65 用户侧测试、8 文件/35 管理侧测试，以及两端 typecheck/build），输出 `/tmp/gopulse-p1603-frontends-security.log`。新增 browser spec 再次通过单文件 TypeScript 检查；Bash 语法与 `git diff --check` 通过。

## 共享基础与最终候选准备

继续将两端完全相同的 `.button` 实现移入共享 CSS，避免按钮样式复制漂移。新增事件代表性 browser 状态检查覆盖 loading、旧结果保留、不可用、empty 和恢复。共享样式修改后的 `scripts/test-frontends.sh` 再次通过（65 + 35 测试及双生产构建），单文件 Playwright 类型检查通过，输出 `/tmp/gopulse-p1603-frontends-shared.log`。

为现有 release builder 的 Git archive + 版本标签输入准备 `1.13.3` 候选；候选提交中的版本号不是验收通过声明，本记录在所有门禁成功前保持“实施中”。第一轮早期快照尚在完整 Compose 回归，其结果不代替新 CSRF/edge 候选。
