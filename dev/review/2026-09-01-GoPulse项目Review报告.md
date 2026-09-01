# GoPulse 项目 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-01 |
| Review 基线 | `798927b`（`origin/main`） |
| 当前分支 | `develop/0.1.6` |
| 当前完成版本 | `0.1.5` |
| 当前里程碑 | Phase 0 已完成，Phase 1 尚未实施 |
| Review 范围 | Backend、Frontend、Compose、本地开发脚本、GitHub Actions、版本/分支规则、实施计划与开发记录 |
| 结论 | **有条件通过（Conditional Pass）** |

本次 Review 以仓库当前实际实现为准，重点判断两个问题：

1. Phase 0 的工程骨架是否仍能按文档完成启动、检查和停止闭环。
2. 当前仓库是否具备安全、可重复、受门禁保护地进入 Phase 1 的条件。

## 2. 总体结论

当前项目的 Phase 0 主功能链路在 Windows 环境中可用：Backend 测试与竞态检测通过，Frontend 测试、类型检查和生产构建通过，Compose 配置可解析，实际执行 `dev.ps1` 后三项基础设施健康，`/health`、`/ready` 和 Frontend 均通过 `verify.ps1` 验证，最后可由 `down.ps1` 完整停止且保留数据卷。

代码层面的优点包括：健康检查契约清晰、依赖错误不会直接泄漏给客户端、配置加载具有较高测试覆盖率、Frontend 对 HTTP 状态与 JSON 契约进行了运行时校验、开发脚本对进程身份和重复启动做了较细致的保护。

但是，当前状态还不适合无条件开始或自动合并 Phase 1：

- Windows Git 的默认换行转换会让仓库中的 Bash 脚本在当前 checkout 直接语法失败。
- 唯一的 GitHub Actions 工作流负责自动建 PR 和合并，却没有执行测试，也没有落实仓库声明的分支名称与 `update` 范围约束。
- Compose 和 Backend 默认把本地开发服务暴露到全部宿主机网络接口，而示例配置使用固定开发凭据。
- 当前分支 `develop/0.1.6` 不在任何权威实施计划的版本分配中；Phase 0 已在 `0.1.5` 收口，下一批按计划应为 `develop/0.2.1`。

建议先完成第 4 节的 P1 项，再开始 Phase-01-01。

## 3. 风险分级

| 等级 | 定义 |
| --- | --- |
| P0 | 已造成数据丢失、严重安全事件或核心链路完全不可用，需要立即停止发布 |
| P1 | 会阻断受支持平台、破坏仓库治理或形成明显安全暴露，应在进入下一开发阶段前修复 |
| P2 | 当前基线可运行，但会放大后续阶段的稳定性、可维护性或运维风险 |
| P3 | 低风险工程卫生或文档质量问题，可随近邻批次处理 |

本次未发现 P0 问题，共记录 **4 项 P1、4 项 P2、1 项 P3**。

## 4. Review Findings

### P1-01：缺少 `.gitattributes`，Windows checkout 会破坏 Bash 脚本

**证据**

- 仓库存在 `.editorconfig`，但没有 `.gitattributes`；Git 不会根据 `.editorconfig` 固定 checkout 换行符。
- 当前 Git for Windows 配置为 `core.autocrlf=true`。
- `git ls-files --eol` 显示三个 Bash 脚本均为 `i/lf w/crlf`：索引中是 LF，工作区被转换为 CRLF。
- 对当前工作区执行以下命令，三个文件全部语法失败：

```text
bash -n scripts/dev.sh
bash -n scripts/down.sh
bash -n scripts/verify.sh
```

典型错误为：

```text
syntax error near unexpected token `done'
syntax error near unexpected token `$'{\r''
```

- 对 Git 索引中的同一内容执行 `git show HEAD:<path> | bash -n`，三个脚本全部通过，证明问题来自 checkout 换行转换而不是脚本逻辑本身。
- 同一换行问题还导致 `gofmt -l .` 在当前 Windows checkout 中列出全部 Go 文件，降低格式检查的可信度。
- `README.md` 将 `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh` 列为正式检查命令，Phase 0 总实施方案也要求脚本语法检查通过，因此当前行为与已声明验收标准不一致。

**影响**

- 在 Windows 仓库目录中使用 Git Bash 或 WSL 调用 Bash 脚本时，入口可能在执行任何业务逻辑前直接失败。
- Windows 与 Unix 开发者看到的工作区字节内容不一致，容易产生无意义的全文件换行 diff 或格式检查误报。
- Phase 0 宣称的跨平台开发入口不能稳定复现。

**建议**

1. 在仓库根目录增加 `.gitattributes`，至少强制以下文件使用 LF：

```gitattributes
*.sh text eol=lf
*.go text eol=lf
```

2. 根据团队策略决定是否将其他文本文件统一为 LF；如统一处理，应先评估现有文件并在独立格式化提交中完成，避免与功能修改混合。
3. 在 Windows CI 或 Windows 开发验收中加入：

```text
git ls-files --eol scripts/*.sh
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh
gofmt -l backend
```

**完成条件**

- 在 `core.autocrlf=true` 的新 Windows clone 中，三个 Bash 脚本工作区均保持 LF，`bash -n` 全部通过。
- Go 格式检查不再因为 checkout 换行策略列出全部文件。

---

### P1-02：自动合并工作流没有测试、分支名称和变更范围门禁

**位置**

- `.github/workflows/auto-pr-merge.yml:7-10`
- `.github/workflows/auto-pr-merge.yml:23-30`
- `.github/workflows/auto-pr-merge.yml:93-130`
- `AGENTS.md:27-41`

**问题**

当前仓库仅有一个 GitHub Actions 工作流。它在所有非 `main` 分支发生 push 时触发，仅通过 `github.actor == 'Ray-ymq'` 进行限制，然后创建 PR，并立即尝试 `gh pr merge`；如果立即合并被仓库要求阻止，才启用 auto-merge。

工作流本身没有：

- checkout 代码；
- Backend 测试、`go vet` 或竞态检测；
- Frontend 测试、类型检查或构建；
- PowerShell/Bash 语法检查；
- Compose 配置检查；
- `develop/x.x.x` 或 `update` 分支名称校验；
- `update` 分支只能修改规划、架构、文档和仓库规则的 diff 范围校验；
- 版本分支是否存在于权威 Phase 总实施方案中的校验。

因此，如果 GitHub 仓库没有另行配置强制检查，工作流可以合并未经任何自动验证的提交。即使外部存在分支保护，仓库本身也没有提供会产生这些 required check 的 CI 工作流。

**影响**

- 测试失败、构建失败或 Bash 脚本被 CRLF 破坏的提交仍可能自动进入 `main`。
- `feature/*`、拼写错误的开发分支或未经分配的版本分支也会被自动建 PR 和合并。
- `update` 分支可以携带应用代码，工作流仍会使用 merge commit 合并，无法落实 `AGENTS.md` 的规划分支边界。
- 自动合并速度高于质量门禁，放大单次误推送的影响。

**建议**

1. 新增独立 CI 工作流，至少执行：
   - Backend：`go test ./...`、`go vet ./...`；关键共享代码变更时执行 `go test -race ./...`。
   - Frontend：`npm ci`、`npm test`、`npm run build`。
   - 脚本：PowerShell AST 解析、`bash -n`，并逐步引入 PSScriptAnalyzer/ShellCheck。
   - 配置：`docker compose ... config --quiet`。
2. 在自动 PR 工作流最前面校验分支名称：仅允许 `^develop/[0-9]+\.[0-9]+\.[0-9]+$` 或精确的 `update`。
3. 对 `update` 分支执行 changed-files 范围检查，发现应用代码、应用测试或普通开发变更时拒绝自动合并。
4. 对 `develop/x.x.x` 校验目标版本已在对应 Phase 总实施方案中分配；批次完成时再校验 `VERSION`。
5. 将自动合并建立在 required checks 全部成功之后，不把“立即尝试合并”作为默认路径。

**完成条件**

- 人为制造一个失败测试时，PR 不可合并。
- 推送非法分支名时，工作流明确失败且不创建/合并 PR。
- 在 `update` 中修改 `backend/` 或 `frontend/` 时，范围门禁明确失败。

---

### P1-03：本地基础设施和 Backend 默认监听全部宿主机接口

**位置**

- `.env.example:5`
- `.env.example:8-23`
- `deploy/compose.yaml:10-11`
- `deploy/compose.yaml:32-33`
- `deploy/compose.yaml:51-53`
- `backend/internal/config/config.go:14-16`

**证据**

Compose 端口只声明 `published:target`，没有指定宿主机 IP。实际启动后，`docker ps` 显示：

```text
0.0.0.0:5672->5672/tcp, [::]:5672->5672/tcp
0.0.0.0:15672->15672/tcp, [::]:15672->15672/tcp
0.0.0.0:16379->6379/tcp, [::]:16379->6379/tcp
0.0.0.0:13306->3306/tcp, [::]:13306->3306/tcp
```

Backend 默认 `HTTP_HOST=0.0.0.0`，真实启动时监听 `0.0.0.0:8080`。与此同时，`.env.example` 提供的是公开、固定、仅供开发使用的 MySQL、Redis 和 RabbitMQ 凭据。

**影响**

- 在宿主机防火墙或 Docker 网络规则允许时，同一局域网中的其他设备可能直接访问 MySQL、Redis、RabbitMQ AMQP、RabbitMQ 管理台和 Backend。
- 固定开发凭据一旦与全接口监听组合使用，就不再只是“本机开发便利性”问题。
- 开发者可能误以为 README 中的 `localhost` 地址意味着服务只绑定回环接口。

**建议**

1. 本地默认配置改为回环绑定：

```yaml
ports:
  - "127.0.0.1:${MYSQL_PORT}:3306"
```

Redis、RabbitMQ AMQP 和管理端口同理。
2. 将 `.env.example` 的 `HTTP_HOST` 默认值改为 `127.0.0.1`。
3. 如果确实需要局域网或容器外远程访问，使用显式、带风险说明的可选配置覆盖，而不是默认开放。
4. 在 README 中说明端口绑定范围，而不只列出访问 URL。

**完成条件**

- 默认启动后，Docker 端口和 Backend 仅监听 `127.0.0.1`/`::1`。
- 需要远程访问时必须由开发者显式选择非回环地址。

---

### P1-04：当前 `develop/0.1.6` 不在权威版本与批次分配中

**位置**

- `VERSION`
- `dev/imple/Phase-00/Phase-00-总实施方案.md:13-24`
- `dev/imple/Phase-01/Phase-01-总实施方案.md:32-51`
- `AGENTS.md:10-25`

**证据**

- 根 `VERSION` 为 `0.1.5`。
- Phase 0 总实施方案明确 Phase-00-01 至 Phase-00-05 已完成，Phase 0 没有剩余批次，后续开发从 Phase 1 的 `0.2.x` 版本线继续。
- Phase 1 的首个权威分配为 Phase-01-01 → `0.2.1` → `develop/0.2.1`。
- 当前分支是 `develop/0.1.6`，但仓库中没有 Phase-00-06，也没有任何总实施方案将 `0.1.6` 分配给可执行开发批次。
- 本次工作是 Review 文档，不是可执行产品开发批次；按仓库规则，纯规划/文档工作通常应留在 `update`，且不更新 `VERSION`。

**影响**

- 如果该分支被推送，自动 PR 工作流会接受其名称并尝试合并，但仓库历史会出现无法映射到实施批次的版本分支。
- 后续人员可能误认为 Phase 0 仍有第六批，或错误地在该分支上继续产品实现。
- 若最终把 `VERSION` 更新为 `0.1.6`，将直接与 Phase 0 已收口、Phase 1 从 `0.2.1` 开始的权威计划冲突。

**建议**

- 如果该分支只承载本次 Review 文档，不要更新 `VERSION`，并在是否推送/合并前明确其例外性质；更符合现有规则的做法是把纯 Review 文档放到 `update`。
- Phase 1 实现应从最新 `origin/main` 创建 `develop/0.2.1`，不要在 `develop/0.1.6` 上开始 Phase-01-01。
- 如果确实需要新增 Phase-00-06，必须先更新 Phase 0 总实施方案，定义批次范围、验收标准以及 `0.1.6` 分配，再开展实现。
- 在 P1-02 的工作流门禁中增加“版本必须存在于权威计划”的机器校验。

**完成条件**

- 所有被推送的 `develop/x.x.x` 都能唯一映射到一个未完成或正在执行的实施批次。
- Review/规划类变更不会导致产品 `VERSION` 变化。

---

### P2-01：readiness 的超时兜底可能积累无法回收的 goroutine

**位置**

- `backend/internal/http/router.go:78-123`
- `backend/internal/http/router.go:144-165`
- `backend/internal/http/router_test.go:114-138`

**问题**

`readinessHandler` 已经为每个 checker 启动一个 worker goroutine；`runChecker` 内部又启动一个 goroutine 调用 `item.checker.Check(checkContext)`，然后由外层 select 在 context 超时时提前返回。

这种方式能让单次 `/ready` 请求按时返回，但如果 checker 完全忽略 context 且长期不返回，内层 goroutine 仍会一直存活。测试 `TestReadyBoundsCheckerThatIgnoresContext` 通过测试结束时关闭 `release` 才释放该 goroutine，因此验证的是“HTTP 响应不会被阻塞”，不是“后台工作能够被回收”。

另外，checker goroutine 内没有 panic recovery；任意 checker 的 panic 都会导致整个 Go 进程退出，而 Gin 的 `Recovery` 中间件无法恢复其他 goroutine 中的 panic。

**影响**

- 在频繁 readiness 探测和异常 checker 的组合下，goroutine 数量可能持续增长。
- 后续接入更多外部系统或自定义 checker 后，错误实现会被该抽象放大。
- readiness 本应是低风险诊断入口，但 checker panic 可能反向终止 Backend。

**建议**

- 明确 `Checker.Check` 必须遵守 context，并优先直接同步调用，让具体驱动负责取消；当前 MySQL、Redis 和 RabbitMQ 适配器本身都已接收 context。
- 如果必须隔离不可信 checker，使用有界 worker、并发上限或 singleflight，而不是每个请求创建无法强制终止的 goroutine。
- 在 checker 隔离层增加 panic recovery，将 panic 记录为内部错误并把该依赖标记为 `down`，日志中仍需避免泄漏凭据。
- 增加压力测试：模拟永久阻塞 checker，连续请求 `/ready` 后验证 goroutine 数量不会线性增长。

---

### P2-02：HTTP Server 只设置了 `ReadHeaderTimeout`

**位置**

- `backend/cmd/server/main.go:53-57`

**问题**

当前 `http.Server` 仅设置 `ReadHeaderTimeout: 5s`，没有配置 `ReadTimeout`、`WriteTimeout`、`IdleTimeout` 和 `MaxHeaderBytes`。

Phase 0 只有两个短响应接口，风险暂时有限；进入 Phase 1 后，注册、登录、发帖、评论和分页查询都会复用该 Server。如果客户端缓慢发送 body、Handler/依赖异常阻塞，或空闲 keep-alive 连接过多，Server 缺少统一资源边界。

**建议**

- 在 Phase-01-01 的 HTTP 基础契约批次中明确并实现 Server 超时基线。
- 至少评估 `ReadTimeout`、`WriteTimeout`、`IdleTimeout`、`MaxHeaderBytes`；对未来可能存在的流式接口单独处理，不应因此让普通 API 全局无限等待。
- 增加超时配置测试或 Server 构造测试，避免只在 `main` 中硬编码而无法验证。

---

### P2-03：配置项没有形成端到端一致契约

**位置**

- `frontend/vite.config.ts:6-19`
- `backend/internal/config/config.go:28-35`
- `backend/cmd/server/main.go:28-62`
- `README.md:112-115`

**问题**

1. `HTTP_PORT` 在 `.env`、Backend、开发脚本和 `verify` 中均被视为可配置，但 Vite 代理始终固定到 `http://localhost:8080`。README 已提示“标准 Phase 0 流程保持 8080”，说明这是已知限制，但配置模型仍不一致。
2. `APP_ENV` 被加载并测试，却没有参与 Gin mode、日志策略或任何运行时行为。真实启动输出仍显示 Gin debug mode 警告。

**影响**

- 修改 `HTTP_PORT` 后，脚本会在新端口启动并验证 Backend，但 Frontend 页面仍请求 8080，形成部分成功、页面失败的状态。
- `APP_ENV=production` 容易给出错误的安全感，因为它目前不会让应用进入 release mode。
- 无效配置项增加运维和测试认知成本。

**建议**

- 使用 Vite `loadEnv` 或由开发脚本显式注入 Backend proxy target，使 Frontend 与 `HTTP_PORT` 使用同一来源。
- 明确 `APP_ENV` 的合法值和行为；若当前不需要，应删除该配置，若保留则至少映射 Gin mode 和环境相关日志策略。
- 增加非默认 `HTTP_PORT` 的端到端测试。

---

### P2-04：近 2,000 行跨平台脚本缺少自动化测试和静态分析门禁

**位置**

- `scripts/dev.ps1`
- `scripts/down.ps1`
- `scripts/verify.ps1`
- `scripts/dev.sh`
- `scripts/down.sh`
- `scripts/verify.sh`
- `.github/workflows/auto-pr-merge.yml`

**问题**

六个脚本承担 dotenv 解析、端口检查、Docker Compose 生命周期、进程身份校验、锁、依赖安装、构建、启动、验证与清理，合计接近 2,000 行，但仓库中没有 Pester、Bats 或其他脚本测试，也没有 CI 中的 PowerShell/Bash 静态检查。

Phase 0 开发记录包含大量人工验收证据，但本次发现的 CRLF 回归说明：人工验收记录不能替代对 checkout 行为和每次提交的自动回归。

**建议**

- 把纯逻辑逐步拆为可测试函数/模块，为 dotenv、配置优先级、端口冲突、陈旧记录、路径身份和退出码增加 Pester/Bats 测试。
- CI 至少建立 Windows PowerShell 与 Linux Bash 双平台矩阵。
- 引入 PSScriptAnalyzer、ShellCheck 和 actionlint；当前环境未安装这些工具，本次未执行对应检查。
- 为真实 Docker 集成测试设置较低频率或按路径触发，避免每次文档变更都启动完整环境。

---

### P3-01：`.DS_Store` 已被 Git 跟踪

**位置**

- `.DS_Store`
- `.gitignore:20-26`

**问题**

`.gitignore` 已忽略 `.DS_Store`，但根目录中的 `.DS_Store` 仍在 Git 索引中。`git ls-files -ci --exclude-standard` 能直接列出该文件。

**影响**

- macOS Finder 元数据会继续出现在仓库历史和无关 diff 中。
- 与现有忽略规则不一致。

**建议**

在独立工程卫生提交中从索引删除该文件，并保留 `.gitignore` 规则：

```text
git rm --cached .DS_Store
```

## 5. 正向评价

### 5.1 Backend

- `/health` 不访问外部依赖，`/ready` 精确返回三项依赖状态，契约边界清晰。
- readiness 检查并发执行，单项和整体请求均有超时边界。
- 返回体不会暴露底层连接错误或 RabbitMQ URL 凭据，测试对此有明确断言。
- MySQL、Redis、RabbitMQ 客户端均配置了连接/读写相关超时。
- HTTP Server 支持信号驱动的 graceful shutdown，并验证了端口释放。
- 配置加载通过可注入 `LookupFunc` 实现确定性测试，非法端口、缺失变量和敏感 URL 错误均有覆盖。

### 5.2 Frontend

- 不只依赖 TypeScript 静态类型，还对 `/health` 和 `/ready` JSON 做运行时结构校验。
- 对 HTTP 200/503 与 readiness body 的一致性进行了校验，避免把矛盾响应误判为成功。
- 请求设置 3 秒客户端超时，区分不可达与契约无效。
- UI 覆盖 loading、up、down、unreachable、invalid、unknown 状态，并避免在 `/health` 不可信时采用 `/ready` 结果。
- 12 项测试覆盖主要状态转换、手动刷新和重复提交保护。

### 5.3 开发体验与文档

- PowerShell 和 Bash 脚本均从自身路径解析仓库根目录，不依赖调用者当前目录。
- `.env` 解析采用受限语法，没有直接 `source` 用户文件。
- 进程记录包含 PID、启动时间、可执行文件和项目上下文，降低误杀无关进程的风险。
- `verify` 为只读验证，`down` 默认保留具名卷，职责划分清晰。
- Phase 0 的计划、批次记录和阶段收口文档较完整，实际完成内容与 `VERSION=0.1.5` 一致。

## 6. 本次实际验证

### 6.1 通过项

| 检查 | 结果 |
| --- | --- |
| `go test -count=1 ./...` | 通过，4 个 Backend package 全部成功 |
| `go vet ./...` | 通过 |
| `go test -race -count=1 ./...` | 通过，未检测到数据竞争 |
| `go test -count=1 -cover ./...` | 通过；`cmd/server` 32.4%，`config` 94.0%，`http` 95.8%，`platform` 80.5% |
| `npm test -- --run` | 通过，2 个测试文件、12 项测试 |
| `npm run build` | 通过，包含 `vue-tsc --noEmit` 和 Vite production build |
| `npm audit --omit=dev --audit-level=moderate` | 通过，0 个已报告漏洞 |
| `npm audit --audit-level=moderate` | 通过，0 个已报告漏洞 |
| PowerShell AST 解析 | `dev.ps1`、`down.ps1`、`verify.ps1` 全部通过 |
| `docker compose ... config --quiet` | 通过 |
| Windows `scripts/dev.ps1` | 通过，基础设施、Backend、Frontend 均成功启动 |
| Windows `scripts/verify.ps1` | 通过，3 个 Compose 服务、2 个 Backend 接口和 Frontend 共 6 项通过 |
| Windows `scripts/down.ps1` | 通过，容器和网络停止，具名卷保留 |
| 索引内容 `git show HEAD:<script> \| bash -n` | 3 个 Bash 脚本全部通过 |

### 6.2 失败或受阻项

| 检查 | 结果 | 说明 |
| --- | --- | --- |
| 当前工作区 `bash -n scripts/*.sh` | 失败 | `core.autocrlf=true` 将 LF 转为 CRLF；详见 P1-01 |
| 当前工作区 `gofmt -l .` | 列出全部 Go 文件 | 同一 checkout 换行问题导致格式检查失真 |
| Bash 真实集成启动 | 未继续执行 | 当前工作区脚本已在语法阶段失败，继续运行没有意义 |
| `govulncheck ./...` | 未执行 | 当前环境未安装 `govulncheck` |
| actionlint / ShellCheck / PSScriptAnalyzer | 未执行 | 当前环境未安装对应工具 |

说明：本次 `npm audit` 结果只代表 2026-09-01 执行时 npm registry 返回的状态；Go 依赖漏洞扫描因工具不可用不能视为已完成。

## 7. 建议整改顺序

### 第一组：进入 Phase 1 前的阻塞项

1. 增加 `.gitattributes`，修复 Bash/Go checkout 换行策略，并在全新 Windows clone 验证。
2. 建立 CI required checks，再调整自动 PR/merge 工作流依赖这些检查。
3. 为分支名称、权威版本分配和 `update` 变更范围增加自动门禁。
4. 将本地 Compose 端口和 Backend 默认绑定改为回环接口。
5. 明确处理 `develop/0.1.6`：仅作为例外文档分支，或将 Review 迁回 `update`；Phase 1 实现使用 `develop/0.2.1`。

### 第二组：建议纳入 Phase-01-01

1. 完整配置 HTTP Server 超时和资源边界。
2. 修正 readiness checker 的 goroutine 生命周期与 panic 隔离。
3. 让 `HTTP_PORT`、Vite proxy 和验证脚本形成同一配置契约。
4. 明确 `APP_ENV` 行为和 Gin mode。

### 第三组：工程化持续改进

1. 为 PowerShell/Bash 脚本增加自动化测试与双平台 CI。
2. 引入 `govulncheck`、actionlint、ShellCheck、PSScriptAnalyzer。
3. 删除已跟踪的 `.DS_Store`。
4. 后续业务代码增加覆盖率门槛时，应按 package 风险分层，不建议只设置单一全仓百分比。

## 8. 最终判定

- **Phase 0 功能状态：通过。** Windows 实际启动、健康检查、Frontend 响应和停止链路均已复验成功。
- **跨平台可复现性：不通过。** 当前 Windows checkout 中 Bash 脚本因 CRLF 无法通过语法检查。
- **自动合并安全性：不通过。** 仓库内没有支撑自动合并的测试和治理门禁。
- **本地默认网络安全：不通过。** 开发服务默认发布到全部宿主机接口。
- **Phase 1 开工建议：有条件允许。** 先关闭 P1-01 至 P1-04，或至少为这些问题建立明确、已分配版本的修复批次，再开始 Phase-01-01 业务实现。
## 9. Phase-00-06 整改执行结果

2026-09-01，用户指定 `develop/0.1.6` 为 Phase 0 权威收尾分支，并在 Phase 0 总实施方案中新增 `Phase-00-06 → 0.1.6 → develop/0.1.6` 的唯一分配。原 Review 的证据与初始“有条件通过”结论保留不变，以下为整改后的增量判定。

| Finding | 结果 | 关闭证据 |
| --- | --- | --- |
| P1-01 | 已关闭 | `.gitattributes` 固定 Bash/Go 为 LF；Windows 工作区 `bash -n` 通过，`gofmt -l backend` 无输出 |
| P1-02 | 已关闭 | 新增可复用质量门禁、PR CI、分支治理脚本与 7 项负向/正向测试；自动 PR 只在门禁成功后创建并启用 auto-merge |
| P1-03 | 已关闭 | Compose 四个端口与 Backend 默认绑定 `127.0.0.1`；Windows 实际启动和 `docker ps` 已验证 |
| P1-04 | 已关闭 | Phase 0 总实施方案新增 Phase-00-06 权威分配，根 `VERSION` 更新为 `0.1.6` |
| P3-01 | 已关闭 | 已从 Git 跟踪中删除根 `.DS_Store`，保留忽略规则 |

P2-01、P2-02、P2-03 仍按原建议进入 Phase-01-01；P2-04 已完成基础双平台语法/质量门禁和治理测试，但细粒度 Pester/Bats、ShellCheck、PSScriptAnalyzer、actionlint 与 govulncheck 继续作为后续工程化项。

**整改后最终判定：通过。** Phase 0 在 `VERSION=0.1.6` 完成最终收口；进入 Phase 1 的仓库内阻塞项已关闭。远程仓库仍应在 `main` 分支保护中将 CI 检查配置为 required checks，作为工作流门禁之外的防御层。
