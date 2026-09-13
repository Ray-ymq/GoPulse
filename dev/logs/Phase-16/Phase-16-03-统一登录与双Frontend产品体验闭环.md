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

## 首轮真实运行结果与 Bundle runner

- 早期 `162a64b` 快照的完整 Compose 业务、依赖故障/恢复、替换、signal、保留卷以及管理安装/启停和真实 Metrics/Events 均通过；新增桌面 browser 在越权断言失败：测试误用不存在的 `/api/v1/admin/users`（404），实际公开合同是 `/api/v1/admin/users/:userId`。修正为具体用户路径，未放松 403 断言。首轮未完成，不记为通过；输出 `/tmp/gopulse-p1603-compose.log`。
- `bd40c37` 候选已构建至 `dist/phase16-03-v1/`，版本 1.13.3，未外部发布。第一次 builder 因历史双架构 schema 的 arm64 runtime 执行失败（exit 255），随后复用本机已有 binfmt 镜像临时注册 arm64，完成候选后立即撤销。此兼容步骤只服务已有构建格式，不是本批运行验收或 arm64 支持声明；全部实际产品和浏览器运行仍为真实 Linux amd64。输出 `/tmp/gopulse-p1603-candidate.log`、`/tmp/gopulse-p1603-candidate-retry.log`、`/tmp/gopulse-p1603-build-compat-cleanup.log`。
- 为避免用开发 Compose 冒充产品 Bundle，给现有 lifecycle 验证脚本增加可选 `--acceptance-image`，严格接收本机 Linux amd64 不可变 image ID。使用临时私有 Compose acceptance profile，在同一真实 Bundle edge 上运行两种 viewport 和三源规则创建，建立本次专属角色 fixture；不改产品安装 state，不使用宿主 Node。
- 新 runner 使用 Bundle 已有 bootstrap Redis 插件产生的真实事件，不再人为重复安装已有插件。临时 browser Compose 文件为 0600，包含的测试密码不写入成功 receipt；完成后删除，整个产品仍由 lifecycle 归属清理。
- `py_compile` 与修正 spec 的 TypeScript 检查通过。该 runner 尚待真实执行完成。

Bundle runner 初次调试在候选身份校验处失败（`KeyError: version`）：调试过程中传参从 secrets 改为 manifest 后与已运行 verifier 的旧调用不一致。未进入浏览器，产品由 finally purge 清理；没有将该轮记为成功。已统一为从实际 Backend 容器 OCI version/revision 对照 runner 标签，保持调用参数一致，再执行同一候选。输出 `/tmp/gopulse-p1603-bundle-browser.log`。补充 `go test ./internal/auth` 通过，覆盖直接关联的 cookie/auth 合同（已有有效 Go cache，未强制重跑）。

第二轮 Bundle 调试安装、唯一 edge、角色 bootstrap、Redis 插件 running 均已实际达到，但等待启动时的 best-effort Event 超时（120 秒），尚未进入 browser。修正 fixture 为完整 Bundle ready 后执行一次真实 Redis 插件 stop/start，再等待可观测事件；不伪造记录，不延长等待上限。该改动仅修复 browser fixture 的时序，未改生产采集链。第二轮由 lifecycle purge 完成清理；输出 `/tmp/gopulse-p1603-bundle-browser-v2.log`。

对 Event 等待失败进一步核对直接公开调用点，发现 fixture 的实际错误是使用了 Metrics 的 `range=15m` 参数：Events API 要求 `from` / `to` / `limit`，与 Frontend 的 `pageQuery` 一致。前述 bootstrap best-effort 时序只是初始假设，不能当作确认的根因。现已改为 UTC from/to/limit，并在轮询前先执行一次请求，使 HTTP 合同错误立即失败，不再被通用 wait_until 吞掉到超时。ready 后真实 stop/start 保留为明确的事件 fixture。

## 最终候选及已通过 release 门禁

最终产品候选：`dist/phase16-03-v2/`；产品与最终 browser 镜像均由已提交的 `03cd7b05d9c01f5067241668453aa367d111dfb6` Git archive 构建，版本 `1.13.3`。此前 v1 是调试候选，不与最终证据混用。最终 builder 结束后已再次撤销临时 qemu-aarch64 注册，实际验收均在原 Linux amd64 Docker daemon。

- Manifest SHA256：`50c39e1008d5f33bc8575409cd1473c1f77401b9480c2a3847851402af8ac9d7`。
- `scripts/verify-release-artifacts.sh --manifest /home/ray/GoPulse/dist/phase16-03-v2/release-manifest.json --platform linux/amd64 --runtime` **通过**，在干净 worktree `/tmp/gopulse-p1603-verify` 运行，输出 `/tmp/gopulse-p1603-release-runtime-final.log`。
- Release receipt：`dist/phase16-03-v2/verification-amd64.json`，状态 `amd64-runtime-and-compose-passed`，绑定上述 manifest/revision。
- 此命令内嵌的 `scripts/verify-compose.sh` 已通过完整业务、权限、依赖故障恢复、服务替换、signal、保留卷、管理安装/启停以及六插件真实采集/局部故障隔离回归；不另外重复该固定门禁。
- 容器内 `frontend-product.spec.ts` 的 desktop/narrow 两组各 2 项通过，时区固定 Asia/Shanghai；覆盖同源登录回跳、普通用户管理 API 403、401/登出/恢复、跨源登出 403 且原会话保留、cookie/CSP/cache、键盘焦点、审计 UTC 筛选和本地显示，以及真实事件的 loading/stale/unavailable/empty/恢复。
- `phase15-closure.spec.ts --grep 'create exact three-source'` 的三源规则创建直接回归通过。没有将其称为重新验收整个三源告警引擎历史矩阵；本批直接改变的是来源安全边界和 Frontend。
- 前两组 browser 使用容器内 origin；最后的 Bundle browser 使用真实安装发布的 loopback edge 含动态端口 origin。二者路由/端口及 Compose 布局不同，后者是验证 `$http_host` 来源保护与唯一产品 edge 的必要环境检查，不是对相同环境重复追求覆盖率。

第三轮 v1 调试仍由已加载旧 fixture 的进程执行旧 range 参数，并以同一 Event 等待超时结束；输出 `/tmp/gopulse-p1603-bundle-browser-v3.log`，该轮不记通过。所有已失败调试安装和第一轮 Compose 项目均已按自身归属清理，保留原有 Docker 项目。

## 最终 Bundle fixture 的 UTC 修正

同一 v2 候选第一次 Bundle 门禁在 fixture 的首次 Events 请求明确返回 400，已不再吞错等待。原因是 Python `isoformat()` 生成 `+00:00`，而现有 Events 参数合同检查 `time.UTC`，浏览器统一发送 `Z`。按 Backend `ParseOptions` 与 Frontend `toISOString()` 的直接合同，将 fixture 的两个时间都改为 canonical `Z`；不改 API、不放宽生产校验。该轮输出 `/tmp/gopulse-p1603-bundle-final.log`，安装已 purge。

这次仅修改宿主验收 fixture 的序列化，不修改任何产品/browser 镜像输入、配置、依赖或运行平台。最终十个产品镜像及 browser 镜像继续使用已通过 release gate 的 v2 manifest/revision；不因验收脚本修正或补写日志重建候选、重跑已通过的完整 Compose 门禁。后续只重跑尚未通过的 Bundle 生命周期/browser 项。

## Bundle 浏览器组合 fixture 的隔离

canonical UTC 修正后，Bundle 已进入真实 browser。desktop 的事件 loading/stale/unavailable/empty/恢复通过；登录/角色/跨源拒绝/登出/本地审计以及服务恢复均到达预期，最后组合步骤“恢复后立即清 Cookie 并 reload”落到了 auth-recovery 而非 login，导致该轮失败（`/tmp/gopulse-p1603-bundle-final-v2.log`）。不把本轮或最后断言记为通过，也不据此臆断 Backend 故障。

将两个独立 fixture 隔离：完成恢复后先结束原文档（about:blank），再清 Cookie 并重新访问受保护 `/posts`。这避免把前一恢复文档尚在进行的请求/导航与下一次失效注入交叠；仍严格要求真正无 Cookie 的页面跳到统一 login，未接受 auth-recovery 作为通过。生产代码不变。该改动仅改变 browser 测试输入的时序，已有产品 candidate 与完整 Compose/六插件结果继续有效；最终 Bundle 使用更新后的测试 runner，再执行尚未通过的两视口矩阵。
