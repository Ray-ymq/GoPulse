# Phase 0-03：Frontend 连通性页面实施记录

- 完成日期：2026-08-31（Asia/Shanghai）
- 分支：`develop/0.1.3`
- 目标版本：`0.1.3`
- 前置批次：Phase 0-01（`0.1.1`）、Phase 0-02（`0.1.2`）
- 执行范围：Phase-00-03；未实现 Phase-00-04 及后续批次

## 1. 实际完成内容

1. 在 `frontend/` 建立独立的 Vue 3 + TypeScript + Vite 工程，固定 Vue、Vite、TypeScript、Vitest、Vue Test Utils、jsdom 与 `vue-tsc` 版本，并提交与 `package.json` 一致的 `package-lock.json`。
2. 提供 `dev`、`test`、`test:watch`、`typecheck` 和 `build` npm scripts；Vite 固定监听 `localhost:5173`，端口被占用时直接失败，避免静默切换端口。
3. 定义 `/health` 与 `/ready` 的 TypeScript 契约和运行时校验。两个请求均使用同源相对路径、`Accept: application/json`、禁用缓存和 3 秒客户端超时，不向页面透传底层网络错误。
4. `/health` 仅在 HTTP 200 且响应为 `{"status":"ok","service":"backend"}` 时将 Backend 标记为 `up`；网络失败或超时映射为 `unreachable`，意外状态码、非法 JSON 或错误契约映射为 `invalid`。
5. `/ready` 接受 HTTP 200 和 503，并校验状态码、`ready`/`not_ready`、服务名和完整三项 `up`/`down` checks 的一致性。Backend 存活但 readiness 不可达或无效时，三项依赖统一清空为 `unknown`，不保留旧状态。
6. 实现 GoPulse 连通性首页，展示 Backend、MySQL、Redis、RabbitMQ 的 `loading`、`up`、`down`、`unreachable`、`invalid`、`unknown` 状态，提供诊断信息、最近刷新时间和手动刷新按钮。
7. 页面首次挂载自动刷新；刷新期间四项状态进入 `loading`、按钮禁用并显示进行中状态。实现请求序号保护，并在组件卸载时使未完成请求失效，防止旧请求覆盖新页面状态。
8. 配置 Vite 将 `/health` 和 `/ready` 代理到 `http://localhost:8080`，Frontend 未引入 CORS、路由、Pinia、业务页面、业务 API 或 Dockerfile。
9. 使用 Vitest + Vue Test Utils 编写 12 项自动化测试，覆盖初始 loading、全部正常、503 单项/多项失败、Backend 不可达、readiness 网络失败/意外状态码/错误契约、health 错误契约、手动刷新、重复提交保护、3 秒超时与状态码/响应体一致性校验。
10. 将根 `VERSION` 从 `0.1.2` 更新为 `0.1.3`。

## 2. 本批变更文件

- `frontend/package.json`
- `frontend/package-lock.json`
- `frontend/index.html`
- `frontend/tsconfig.json`
- `frontend/tsconfig.app.json`
- `frontend/tsconfig.node.json`
- `frontend/vite.config.ts`
- `frontend/src/env.d.ts`
- `frontend/src/main.ts`
- `frontend/src/App.vue`
- `frontend/src/App.test.ts`
- `frontend/src/styles.css`
- `frontend/src/components/StatusCard.vue`
- `frontend/src/services/connectivity.ts`
- `frontend/src/services/connectivity.test.ts`
- `frontend/src/types/connectivity.ts`
- `dev/logs/Phase-00/Phase-00-03-Frontend连通性页面.md`
- `VERSION`

用户已有且未跟踪的 `VSRSION` 未修改、未暂存、未提交。Frontend 的 `node_modules/` 和 `dist/` 受 `.gitignore` 保护，未纳入版本控制。

## 3. 自动化验证与结果

执行环境：Windows，Node.js `v24.14.1`，npm `11.11.0`。

### 3.1 依赖、类型、测试、构建与 Backend 回归

执行：

```powershell
cd frontend
npm ci --ignore-scripts
npm run typecheck
npm test
npm run build
npm ls --depth=0
npm audit --omit=dev

cd ../backend
go test ./...
```

结果：

- npm 安装成功，最终直接依赖版本与 `package.json` 一致；生产依赖审计返回 0 个漏洞。
- `vue-tsc --noEmit` 通过。
- Vitest 共执行 2 个测试文件、12 项测试，全部通过。
- Vite 生产构建成功，生成 `dist/index.html`、CSS 和 JavaScript 产物；`dist/` 未提交。`go test ./...` 通过，确认 Frontend 批次未破坏现有 Backend 测试。
- 初始选用的 TypeScript 7 与当前 `vue-tsc` 不兼容，实际调整为 TypeScript `6.0.3`；初始 jsdom 30 和 Vue Test Utils 2.5 的传递依赖对本机 Node `24.14.1` 提示更高 patch 版本要求，最终固定为 jsdom `29.0.1` 与 Vue Test Utils `2.4.6`，消除 engine 警告并保持测试能力。

### 3.2 真实 Backend 与 Vite 代理联调

使用已存在且 healthy 的 `gopulse` Compose 项目、本地 `.env`、Phase 0-02 Backend 和 Vite 开发服务器执行：

1. 三项依赖正常时，Backend 与 Vite 代理的 `/health` 均返回 HTTP 200；`/ready` 返回 HTTP 200，MySQL、Redis、RabbitMQ 均为 `up`；Frontend 首页返回 HTTP 200。
2. 停止 Redis 后，通过 Vite 代理请求 `/health` 仍返回 HTTP 200；`/ready` 返回 HTTP 503，只有 Redis 为 `down`，MySQL 与 RabbitMQ 保持 `up`。
3. 重新启动 Redis 并等待容器恢复 healthy 后，无需重启 Backend，Vite 代理 `/ready` 恢复 HTTP 200，三项重新为 `up`。
4. 停止 Backend 后，Frontend 首页仍返回 HTTP 200，Vite 代理 `/health` 返回 HTTP 502，验证 Frontend 与 Backend 进程边界及不可达路径。
5. 联调结束后恢复 Redis 为 healthy，并终止本批手动启动的 Backend 与 Frontend 进程；未停止原有 MySQL、Redis、RabbitMQ 容器，未删除其具名卷。

页面状态渲染和刷新按钮交互通过 jsdom 中的组件 DOM 断言完成；本批没有执行人工浏览器视觉走查或截图对比。

### 3.3 仓库检查

执行：

```powershell
git diff --check
git status --short
git branch --show-current
```

结果：`git diff --check` 通过；当前分支为 `develop/0.1.3`。提交时只暂存本批 Frontend、实施记录和 `VERSION`，不包含未跟踪的 `VSRSION`。

## 4. 偏差、限制与后续项

- 计划指定 Node.js 24 和 npm 11；本机满足主版本要求，但安装初选的最新依赖时出现 Node patch 级 engine 警告，因此按实际兼容性固定测试依赖版本，未要求升级本机 Node。
- 本批完成自动化 DOM 测试和真实 Vite 代理联调，但未执行人工浏览器视觉走查；后续若调整样式，应补充桌面与窄屏浏览器截图检查。
- 本批只提供 Frontend 自身命令。根目录统一 `dev`、`down`、`verify` 脚本和从零启动体验属于 Phase 0-04。
- 本批未实现登录、业务页面、Vue Router、Pinia、Frontend Dockerfile、CI、E2E 浏览器框架或 Backend CORS，符合 Phase 0-03 边界。
- Phase 0-04 可依赖以下确定入口：Backend 工作目录 `backend/`、启动命令 `go run ./cmd/server`、默认端口 `8080`；Frontend 工作目录 `frontend/`、依赖命令 `npm ci`、启动命令 `npm run dev`、构建命令 `npm run build`、默认端口 `5173`。两者前台运行时均可通过终端中断信号退出；本次 Windows PTY 中断由宿主命令会话报告非零退出码，应用进程与监听端口均已结束。
