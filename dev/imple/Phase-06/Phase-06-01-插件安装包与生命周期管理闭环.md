# Phase 6-01：插件安装包与生命周期管理闭环实施方案

> 执行序号：1 / 3
>
> 前置阶段：Phase 5 已完成并通过验收
>
> 总方案来源：[Phase-06-总实施方案.md](Phase-06-总实施方案.md)

## 1. 批次目标

从 `monitor/` 只有占位说明的基线，纵向交付“已登录管理员 → Backend 管理代理 → Monitor Plugin Manager → Redis Exporter 状态变化”闭环。本批同时完成持久化管理员角色、服务器运维授权 CLI、`tar.gz` 安装包、受限解包、原子版本切换、进程所有权、更新回滚、Monitor 重启恢复和安全状态查询。

本批完成后，合法 Redis Exporter 包必须能经 Backend 上传，被 Monitor 解压到固定版本目录，自动启动并通过 `/health`；普通用户不得查看或改变任何插件状态。Phase-06-02 只在本批稳定所有权和状态事实上接入 MetricsMonitor，不重新设计安装和运行时。

## 2. 前置条件

- Phase-05-01 与 Phase-05-02 已合入主远程 `main`，根与 Frontend 版本均为 `1.2.2`，实施记录和远程门禁齐全。
- Redis Exporter 可独立构建，`/health`、`/metrics`、回环监听、环境变量、信号退出和 PID 归属契约与 Phase 5 最终方案一致。
- 已 fetch 主远程并从包含 Phase 5 全部结果的最新 `main` 创建 `develop/1.3.1`，没有沿用 `update` 或 Phase 5 分支。
- 在 WSL2 Linux filesystem 实施，具备 Go、Bash、Docker、MySQL 和真实 Redis Exporter 验收资源。
- 开始前记录日常 Compose project、端口、`.run` 进程、插件根和 Git 快照；不更改或清理不属于本批的资源。

## 3. 实施范围

### 3.1 管理员角色与授权

- 新增可逆数据库迁移，将 `users.role` 定义为非空 `user|admin`，默认与已有行均为 `user`。
- 用户领域模型保存角色，但业务作者摘要仍只暴露 `id/username`，不把管理员身份散布到帖子、评论和通知响应。
- 新增 `cmd/admin-role`，命令契约固定为 `go run ./cmd/admin-role promote --username <username>`；用规范化 username 查找用户，用户不存在失败，已是管理员幂等成功。
- JWT 及 Cookie 格式不变。管理员中间件从已验证用户 ID 查询当前数据库角色，避免角色变更后旧 token 继续使用过期权限。
- 注册、登录和 `/users/me` 的当前用户响应增加 `role`；Frontend `PublicUser` 与认证状态适配新字段，不增加管理页面或导航。
- 新增 `permission_denied`，只在已登录但非 admin 的插件路由返回 `403`；未登录继续返回 `401 authentication_required`。

### 3.2 Monitor 基础运行时

- 建立独立 `monitor` Go module、配置 loader、结构化 logger、`cmd/monitor`、HTTP server、`/health`、`/ready` 和信号关闭。
- Monitor 默认只监听 `127.0.0.1:9090`；内部 API 使用至少 32 bytes Bearer token，健康接口不返回配置、插件路径或进程信息。
- 实现线程安全的插件操作串行化、进程归属校验、健康探测、超时退出和可注入的 clock/process/health 边界，以便在不启动真实子进程时单元验证状态转换。
- 运行 Redis Exporter 时只传递 Phase 5 固定 Redis/Exporter 环境变量白名单，cwd 固定为已验证当前 release，直接调用 executable 而不经 shell。

### 3.3 安装包、布局与 Registry

- 新增 Redis Exporter 打包入口，产生根 `plugin.json` 和 `bin/gopulse-redis-exporter` 的确定性 `tar.gz`，Manifest 字段、版本、Linux/arch、入口路径与 SHA-256 符合总方案第 7 节。
- Backend/Monitor 同时强制 64 MiB 上传限制；Monitor 再强制 32 个 entry、128 MiB 总解压量、96 MiB 单文件、64 KiB Manifest 和 240 bytes 路径上限，拒绝路径穿越、重复 entry、链接、device/FIFO/socket、错误摘要、平台不符和非法 Manifest。
- 安装根必须是受信绝对路径，本地运行固定为 `$REPO_ROOT/.run/plugins`；使用 `.staging`、`releases/<version>`、相对 `current` symlink、`registry.json` 和 `runtime/process.json` 布局。
- release、`current` 和 Registry 分别使用同文件系统 rename 与 fsync 建立原子边界，失败不暴露临时路径且不留半安装状态。
- Registry 只持久化公开 Manifest 元数据、当前版本、安装/更新时间与 `desired_state`，不写入 PID、密码、token、绝对 executable 或原始错误。

### 3.4 生命周期语义

- install 对未安装 ID 完成安全解包、release 登记、`current` 切换和自动启动；只在 Phase 5 `/health` 契约通过后返回 `201` 与 `desired_state=running, observed_state=running`。
- 首次启动/health 失败撤销 Registry、current、release、PID 记录和已启动进程，保证 GET 查询仍为 not found。
- start/stop 幂等；start 先持久化 desired running，启动唯一进程并等待 health，失败时保留 desired running 且标记 observed failed；stop 先持久化 desired stopped，再对归属匹配进程发 `SIGTERM`，超时时只对同一已验证进程强制退出。
- Linux 插件进程使用独立进程组和 parent-death `SIGTERM`；Monitor 重启发现合法遗留进程时先有界停止，再依 desired state 恢复，保证唯一运行实例。
- update 只接受同 ID 更高 SemVer。running 插件在新包完整验证后停止旧版、切换并启动新版；新版失败时原子恢复旧版并重启。stopped 插件更新后仍 stopped。
- Monitor 重启时校验 Registry/current/release/PID 事实，只重启 desired state 为 running 的插件。单项恢复失败记为 `failed`，Monitor 仍就绪并允许管理员修复。

### 3.5 Backend 代理与稳定 API

- 增加总方案第 9 节六个 `/api/v1/exporter-plugins` 路由，全部按“登录中间件 → admin 中间件 → handler”顺序注册。
- install/update 严格接受唯一 multipart `package`，Backend 使用有界 reader 流式转发，不先读取完整包，不把用户文件名当作安装路径。
- Monitor 对应 `/internal/v1/exporter-plugins` 路由只接受有效 Bearer token；Backend 把内部错误映射为稳定公共错误，不透传原始 body 或底层错误。
- 状态 DTO 包含总方案第 8.2 节字段；本批 `last_scrape_at/last_success_at` 为 `null`，由 Phase-06-02 接入后填充，不伪造采集记录。

## 4. 实施边界与非目标

- 不实现 MetricsMonitor 定时器、Prometheus parser、metrics Envelope 或 HTTP Publisher。
- 不实现插件管理 Frontend、独立管理员登录、用户列表/禁用/删除、前端角色管理或通用 RBAC。
- 不自动把第一个用户提升为管理员，不通过环境变量隐式赋权，不把角色放入长寿命 token claim。
- 不支持 ZIP、远程 URL、数字签名、任意 Manifest hook、第三方插件 ID、多 target 或多实例。
- 不调用 shell 运行插件，不将 archive 解压到仓库源码目录，不使用 OS mount/容器卷。
- 不删除旧 release 以实现空间回收；首版保留已成功安装的旧版用于回滚，清理策略留待后续。
- 不修改 Redis Exporter 指标语义、被动拉取、无历史、目标故障或自动恢复契约。

## 5. 预计文件与交付物

```text
backend/migrations/**
backend/internal/user/**
backend/internal/auth/**
backend/internal/http/**
backend/internal/apperror/**
backend/internal/config/**
backend/cmd/admin-role/**
backend/cmd/server/**
frontend/src/types/api.ts
frontend/src/services/api.ts
frontend/src/composables/useAuth.ts
monitor/go.mod
monitor/go.sum
monitor/cmd/monitor/**
monitor/internal/config/**
monitor/internal/httpserver/**
monitor/internal/plugin/**
monitor/README.md
.env.example
scripts/package-redis-exporter.sh
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-monitor.sh
scripts/ci/**
.github/workflows/quality-gates.yml
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-06/Phase-06-01-插件安装包与生命周期管理闭环.md
```

该列表是允许触达的预计边界，不要求制造无意义修改。实际未变更文件不得写入实施记录；超出边界的需求必须先判断是否直接阻断本批验收。

## 6. 详细实施步骤

1. 核对 Phase 5 实施记录、Exporter 最终端点/环境/信号/PID 契约、最新数据库迁移序号和 Backend 认证中间件。
2. 增加 `users.role` 迁移、领域字段、Repository 读/更新和 admin 查询，保证业务作者 DTO 不增加角色。
3. 实现 `admin-role promote`、幂等与脱敏失败，然后建立 admin 中间件和 `permission_denied` 错误映射。
4. 更新注册/登录/当前用户 DTO 与 Frontend `PublicUser`，定向验证旧页面、作者摘要和会话恢复无回归。
5. 建立 Monitor module、配置、日志、HTTP 生命周期、health/readiness 和 internal Bearer 认证。
6. 实现 Manifest 严格解析、archive 有界安全检查、staging 解包、入口 digest/平台校验和原子 release/current 布局。
7. 实现 Registry 原子持久化、desired/observed state、单插件操作串行化和公共状态 DTO。
8. 实现直接进程启动、环境白名单、health 等待、PID 归属、幂等启停、超时退出和 Monitor 重启恢复。
9. 实现 install 自动启动、update 保态、原子回滚和全路径失败清理，以可控测试进程验证所有状态转换。
10. 增加 Monitor 内部路由和 Backend 管理代理，实现 multipart 限流转发、超时、取消和安全错误映射。
11. 实现确定性打包脚本和 `verify-monitor.sh`，用真实 Redis Exporter 完成管理员安装、启停、更新、回滚和重启恢复。
12. 把 Monitor 纳入 Bash 日常生命周期与 CI，移除 `dev.sh` 对 Exporter 的直接启动所有权，保留安全下线与验证。
13. 更新 README、配置示例、`1.3.1` 版本元数据和同名实施记录，只写入真实结果。

## 7. 风险与控制

- **第一个注册用户抢占管理员**：所有用户默认 `user`，只允许拥有服务器和数据库配置权限的运维 CLI 显式提升。
- **角色变更后旧 token 越权**：管理路由以数据库当前角色授权，不使用客户端或 token 中的角色副本。
- **archive 穿越/炸弹**：上传、entry 类型、路径、单文件、文件数和总解压量多重限制，所有内容只落入随机 staging。
- **安装成功但不可运行**：install 仅在真实进程启动和 health 通过后提交，失败撤销全部新状态。
- **更新导致长时间停机**：新包先完整校验，切换窗口只包含停旧、原子切换、启新；失败立即恢复旧版和 desired state。
- **进程记录复用 PID 误杀**：发信号前重新校验 cwd、绝对 executable、start ticks 和 marker，任一不匹配均拒绝。
- **Monitor 与脚本双重所有权**：Phase 6 后 Exporter 只由 Plugin Manager 启动，`dev.sh/down.sh` 通过 Monitor 完成编排。
- **共享配置泄漏**：Monitor 只传递 Exporter 白名单，API/状态/日志只使用有限错误码，不回显路径、命令行和原始错误。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败修复后只重跑受影响的命令或场景：

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-monitor.sh --self-test
scripts/verify-monitor.sh
scripts/verify-exporter.sh
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.3.1 --base-ref upstream/main
git diff --check
```

`scripts/verify-monitor.sh` 在本批只执行角色授权、安装包、管理 API、生命周期、回滚和重启恢复矩阵；Phase-06-02 再扩展同一入口覆盖周期采集和 Publisher。`verify-exporter.sh` 保证安装包内的 Phase 5 Exporter 本身契约没有被接管逻辑改变。

如本批实际修改共享 Compose 资源、数据库初始化或 Bash 进程归属基础函数，补跑 `scripts/verify-business.sh --self-test`；只有已观察的跨组件回归风险才执行完整 `scripts/verify-business.sh`，并在实施记录中说明原因。

## 9. 验收标准

- 迁移 up/down 成功，新旧用户默认均为 `user`，只有运维 CLI 显式提升后才成为 `admin`。
- 管理员使用既有登录/Cookie 成功访问六个插件管理路由；普通用户全部为 `403 permission_denied`，未登录仍为 `401`。
- 当前用户响应可区分 `user/admin`，业务作者响应不暴露 role，Frontend 已有登录、恢复和业务页面无回归。
- 合法 `tar.gz` 通过 Backend 限流转发，Monitor 安全解包、原子安装到固定布局、自动启动真实 Redis Exporter 并在 health 通过后返回 running。
- 总方案规定的 archive/Manifest/大小/平台/版本负向样例全部被拒绝，没有越界文件、半安装 release、错误 current 或残留 Registry。
- start/stop 幂等且只作用于归属匹配进程；错误/过期 PID 记录不得导致误杀。
- running 与 stopped 两种状态的更新符合保态规则，新版失败时 current/Registry/进程都恢复为旧版有效状态。
- Monitor 重启后恢复 desired running，保留 desired stopped，单插件恢复失败不拖垮 Monitor API。
- Bash 日常生命周期不再双重启动 Exporter，隔离验收成功/失败/中断不误伤日常资源。
- 第 8 节固定验证和远程门禁通过，版本元数据为 `1.3.1`，实施记录真实完整。

## 10. 明确完成条件

只有管理员授权、公共/内部 API、安全安装包、原子布局、自动安装启动、幂等启停、保态更新、失败回滚、重启恢复和进程归属保护全部通过，且没有阻断验收的失败，才可标记 Phase-06-01 完成。只有 mock 进程、直接调用 Monitor 或手工复制 executable 不足以完成本批。

## 11. 下一批交接

- 已合入的 `users.role`、admin 中间件、运维提升 CLI 和当前用户 `role` 契约。
- 可独立运行的 Monitor，以及稳定的 health/readiness、internal token、Plugin Manager 和 Backend 公共代理 API。
- 经过真实验收的 Redis Exporter 安装包、Manifest v1、固定布局、原子 current、Registry 和 PID 归属记录。
- 可订阅的插件状态转换与唯一所有权；Phase-06-02 根据 running/stopped 启用或停止采集 target。
- 明确留给 Phase-06-02 的工作：周期调度、Prometheus 解析、指标基础校验、Envelope v1、HTTP Publisher 与真实捕获验收。
