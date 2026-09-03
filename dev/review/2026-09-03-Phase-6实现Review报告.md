# GoPulse Phase 6 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-03 |
| 用户指定权威 Review 分支 | 用户写作 `develop1.3.4`；依据 Phase 6 权威分配表按规范使用 `develop/1.3.4` |
| Review 分支创建方式 | fetch 后远端不存在 `origin/develop/1.3.4`；从最新 `origin/main` 创建本地 `develop/1.3.4`，未推送 |
| Review 基线 | `4a3af9aa9c2718490de82a102eff51639be8a197`（`feat(monitor): add periodic metrics collection (#65)`，Review 开始时与 `origin/main` 一致） |
| Phase 6 实现起点 | `2c73bbc3aa24cd47859ae4b62111c93675073d54`（Phase 5 Review 整改合并提交） |
| Phase 6 已合入提交 | `d78abaa`（Phase-06-01 / PR #63）、`fd4b6f4`（Phase-06-02 / PR #64）、`4a3af9a`（Phase-06-03 / PR #65） |
| 当前完成版本 | 根 `VERSION` 与 Frontend npm 元数据均为 `1.3.3`；本次只新增 Review 文档，不修改版本 |
| 权威目标版本 | Phase-06-04 对应 `1.3.4` / `develop/1.3.4`；当前尚未达到批次完成条件 |
| 已有实施记录 | Phase-06-01、Phase-06-02、Phase-06-03 共 3 份；Phase-06-04 实施记录尚不存在 |
| 实际执行环境 | WSL2 Linux，Go 1.26.7，Node.js 24.20.0，npm 11.19.0，Python 3.12.3，Docker 29.7.2 / Compose v5.5.0 |
| Review 范围 | Phase 6 总方案与四个拆分方案、前三批实施记录、角色与授权、Backend/Monitor 管理代理、插件包/Registry/进程生命周期、MetricsMonitor、Envelope/Publisher、Bash 生命周期、隔离验收、版本与分支治理 |
| Phase 6 已实现规模 | 72 个文件，4483 行新增、117 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. `user|admin` 是否来自 MySQL 当前事实，并由 Backend 在服务端保护全部插件管理路由。
2. 插件包解包、版本切换、进程归属、启停、更新回滚和重启恢复是否在失败路径上仍保持一致。
3. MetricsMonitor 是否形成立即/周期、无重叠、严格 Prometheus 校验、Envelope v1 与有界 Publisher 闭环。
4. Phase 0～5 回归、日常 Bash 生命周期、隔离资源清理和固定质量门禁是否仍可执行。
5. Phase-06-04 / `develop/1.3.4` 是否已经具备阶段收口与版本发布条件。

Review 没有覆盖、暂存或提交工作区原有的未跟踪文件 `使用指南.md`。

## 2. 总体结论

Phase 6 前三个功能批次已经交付了方向正确且相当完整的主体能力：

- 用户角色持久化到 MySQL，注册默认 `user`，运维 CLI 可显式提升为 `admin`；现有 Cookie/JWT 不承载角色，管理员授权按数据库当前值判断。
- Backend 对插件列表、查询、安装、启动、停止和更新复用认证及管理员中间件；普通用户与未登录用户的隔离在真实 Backend → Monitor 链路中得到验证。
- Monitor 已成为独立 Go module，内部 API 使用 Bearer token；Redis Exporter 包具有严格 Manifest、SHA-256、平台、路径、条目数量和解压大小校验。
- Plugin Manager 已实现固定 Redis Exporter 的安装、自动启动、幂等启停、PID/start ticks/executable/cwd/command marker 归属校验、版本目录、`current` symlink 和 Registry。
- MetricsMonitor 已实现立即与周期拉取、单 target 串行、1 MiB 响应上限、压缩/内容类型/Prometheus 契约校验、成功与 `target_unavailable` 消息，以及可选 HTTP Publisher。
- 本次独立执行 `scripts/verify-monitor.sh` 与 `scripts/verify-exporter.sh` 均通过，证明正常安装、启停、更新、单次回滚、Monitor 重启、真实 Redis 数值、目标停止/恢复、畸形指标拒绝和 Publisher 恢复等主要路径可运行。

但是，Phase 6 当前不能按权威方案标记完成：

1. 固定回归门禁 `scripts/verify-business.sh` 在数据库迁移前即失败，Phase 0～5 必要回归没有通过。
2. Plugin Manager 的更新事务在“新版本启动失败且旧版本也无法恢复”时会把公共状态恢复成 `running`，即使没有任何归属进程；部分 Registry 持久化失败也会留下内存、`current` 与磁盘 Registry 不一致。
3. `MONITOR_PLUGIN_ROOT` 没有落实权威方案规定的危险根目录和 symlink 边界，文件安装与清理可落到 `/`、用户 home、仓库根或被替换的 symlink 目标。
4. 调用方取消 stop/update 请求时，MetricsMonitor 会先取消并丢失等待句柄，而 Manager 在错误返回后保留运行中的 Exporter 和原公共状态，形成“进程仍 running、采集已停止”的中间状态。
5. 日常 `dev.sh` 每次构建当前 Exporter 包，却在插件已安装时完全不比较版本或摘要，也不调用 update；持久化插件根可能长期运行旧二进制。
6. Monitor/Backend 的 timeout 默认值和允许范围与 Phase 6 权威配置契约不一致。

本次记录 **3 项 P1、3 项 P2**。其中 P1-01 已直接使固定完成门禁失败，P1-02 与 P1-03 分别破坏核心生命周期事实和文件系统安全边界。Phase-06-04 必须先关闭这些阻断项并完成全部固定验收，才能把版本更新到 `1.3.4` 并声明 Phase 6 完成。

## 3. 风险分级

| 等级 | 定义 |
| --- | --- |
| P0 | 已造成数据丢失、严重安全事件或核心链路完全不可用，需要立即停止发布 |
| P1 | 阻断固定验收、破坏关键安全/持久化边界，或使公开生命周期事实与真实进程明显不一致 |
| P2 | 主成功路径可运行，但调用取消、日常升级、配置契约或可维护性存在可复现风险 |
| P3 | 低风险工程卫生或文档问题，可在后续近邻任务处理 |

本次未发现 P0 或 P3；记录 **3 项 P1、3 项 P2**。

## 4. Review Findings

### P1-01：Phase 0～5 固定业务回归因缺少 `MONITOR_API_TOKEN` 在迁移阶段失败

**位置**

- `backend/internal/config/config.go:215-227`
- `backend/cmd/migrate/main.go:24-35`
- `scripts/verify-business.sh:240-291`
- `scripts/verify-business.sh:294-307`
- `scripts/verify-business.sh:314-328`
- `dev/imple/Phase-06/Phase-06-04-集成验收与阶段收口.md` 的固定回归要求

**实际证据**

本次在最终 `origin/main` 基线上执行：

```bash
scripts/verify-business.sh
```

隔离 MySQL、Redis、RabbitMQ 和 Elasticsearch 已创建并启动，但数据库迁移立即失败：

```text
2026/09/03 18:20:56 database migration failed: load configuration: MONITOR_API_TOKEN is required
exit status 1
```

原因是 Backend 共享 `config.Load()` 从 Phase-06-02 起无条件要求 `MONITOR_API_TOKEN`，而 `cmd/migrate` 同样加载完整 Backend 配置。`verify-business.sh` 生成的隔离环境和 `backend_environment`/`start_backend` 参数均未提供该变量。

失败后的隔离 container、network 和 volume 已由脚本清理；没有发现本次资源残留。

**影响**

- Phase-06-04 权威方案明确要求 `scripts/verify-business.sh` 作为 Phase 0～5 必要回归；当前固定完成门禁不能通过。
- 当前问题发生在迁移前，浏览器业务、通知可靠性、搜索、日志和管理员社交能力均未进入验证，不能以 focused Monitor 验收替代。
- 同类脚本或运维命令只要复用完整 Backend 配置，也可能被与自身职责无关的 Monitor 配置阻断。

**建议整改**

1. 至少为 `verify-business.sh` 的生成环境、迁移、Backend 启动和所有复用完整 Backend 配置的路径提供安全的隔离 `MONITOR_API_TOKEN` 与明确 `MONITOR_URL`。
2. 更稳妥地拆分 migration/worker/indexer 所需配置，避免 `cmd/migrate` 因未使用的 Monitor 客户端配置而失败；不得因此放松真正启动 Backend 时的 token 校验。
3. 增加一个脚本或最低层回归，证明 Phase 6 新增必填配置不会再次使保留的 Phase 0～5 验收入口在启动前失败。

**关闭条件**

- `scripts/verify-business.sh` 在干净隔离资源上完整通过，而不是只通过 `--self-test`。
- Phase 0～5 业务、可靠性、搜索和日志矩阵均执行到完成，并确认失败/成功清理无资源残留。

---

### P1-02：更新回滚的二次失败会报告 `running`，且持久化失败可留下三份状态不一致

**位置**

- `monitor/internal/plugin/manager.go:354-383`
- `monitor/internal/plugin/manager.go:393-425`
- `monitor/internal/plugin/storage.go:35-79`
- `dev/imple/Phase-06/Phase-06-总实施方案.md` 的“更新失败原子回滚”验收

**实际证据**

更新新版本启动失败后，代码先执行：

```go
m.registry.Plugins[id] = old
m.states[id] = oldState
```

随后才尝试重新启动旧版本。只有 `restartErr == nil` 时才写入恢复后的 runtime；当旧版本也启动失败时，没有失败分支修正状态，因此 `oldState` 可以继续是 `observed_state=running`，但 `m.runtimes[id]` 中不存在可归属进程。

本次创建并在执行后删除了临时定向测试，构造“旧版本初始 running → 新版本 `/bin/false` 启动失败 → 修改后的运行配置使旧版本恢复也失败”。执行：

```bash
(cd monitor && go test -run TestReviewFailedRollbackCanReportRunningWithoutProcess -count=1 -v ./internal/plugin)
```

测试成功复现：Update 返回错误，随后 `Get` 仍返回 `observed_state=running`，但没有存活的归属 runtime。

此外还有两个同一事务边界内的问题：

- 新 Registry 保存失败时，代码只尝试把 `current` 切回旧版本，没有恢复已经改写的内存 `m.registry` 和 `m.states`；defer 又会删除新 release，公共状态可能指向已不存在的版本。
- 新版本启动失败后的 `switchCurrent` 和旧 Registry `saveRegistry` 错误均被忽略；API 状态、`current` symlink 和磁盘 Registry 可能各自表达不同版本。

**影响**

- 管理员可能看到旧版本 `running`，实际 Exporter 已停止；后续指标采集和告警事实错误。
- Monitor 重启可能读取另一份磁盘事实，导致重启前后状态跳变，且无法确认哪一份状态是权威。
- 违反 Phase 6 的核心阶段验收：“更新保持 desired state，运行新版失败时旧版 current/Registry/进程完整恢复”。

**建议整改**

1. 把 update 实现为显式事务状态机：保留旧快照，所有 `current`、Registry、内存状态和进程步骤均检查错误并拥有对应补偿。
2. 如果旧版本恢复失败，保留旧版本/desired state 的持久化事实，但公共状态必须为 `failed`，写入有限 `rollback_failed`，清除 runtime 和无效 process record；不得报告 `running`。
3. Registry 保存失败时先恢复内存快照，并验证 `current` 与磁盘 Registry；补偿失败应进入明确 repair-required 状态，而不是忽略错误。
4. 增加代表性的“新版本失败、旧版本恢复成功”和“新旧版本均失败”测试；再增加 Registry 原子写失败测试，断言三份状态一致。

**关闭条件**

- 任意 update 失败后，内存状态、磁盘 Registry、`current`、release 和真实进程一致。
- 双重启动失败时状态为 `failed` 且无伪造 running；恢复成功时旧版本真实 health、PID 归属和 MetricsMonitor generation 均恢复。

---

### P1-03：`MONITOR_PLUGIN_ROOT` 未实现危险根目录和 symlink 安全边界

**位置**

- `monitor/internal/config/config.go:54-58`
- `monitor/internal/plugin/manager.go:32-45`
- `monitor/internal/plugin/storage.go:12-18`
- `monitor/internal/plugin/manager.go:160-200`
- `dev/imple/Phase-06/Phase-06-总实施方案.md:367`

**实际证据**

权威方案规定：

- 插件根必须是规范化绝对路径；
- 不得为 `/`、用户 home 或仓库根；
- 创建后不得跟随根目录替换或越界 symlink。

当前配置只检查 `filepath.IsAbs` 并执行 `filepath.Clean`。Manager 随后直接对该字符串执行 `MkdirAll`、创建 `.staging`、写 `registry.json`、创建 `redis-exporter/releases`、切换 symlink 和失败清理，没有：

- 拒绝 `/`、home、仓库根；
- `Lstat`/`EvalSymlinks` 后的允许根验证；
- 防止运行期间根目录或中间目录被替换为 symlink；
- 以打开的目录 fd 约束后续创建、rename 和删除。

因此合法格式的 `MONITOR_PLUGIN_ROOT=/`、用户 home 或仓库根会通过配置；指向其他位置的绝对 symlink 也会被跟随。

**影响**

- 以高权限运行时，安装/更新/清理可在系统根、home 或仓库中创建或删除 `registry.json`、`.staging`、`redis-exporter` 与 `current`。
- 根目录被替换后，受限 archive 的“不得越过插件根”只约束字符串拼接，不能约束真实文件系统目标。
- 这是 Phase 6 安装包与原子清理的关键文件系统安全边界，不是普通配置可用性问题。

**建议整改**

1. 在配置或 Manager 初始化时拒绝 `/`、当前用户 home、解析后的仓库根以及其他明确危险目录。
2. 对 root 和既有父级使用 `Lstat`，拒绝 symlink；记录并校验规范化真实路径、device/inode，或使用目录 fd + `openat`/`renameat` 风格操作约束所有文件写入。
3. 对 `.staging`、插件目录、releases、runtime 和 Registry 的既有节点同样拒绝意外 symlink/非预期文件类型。
4. 增加 `/`、home、仓库根、root symlink、运行中替换和内部目录 symlink 的负向测试，断言没有目标外写入或删除。

**关闭条件**

- 所有权威禁止根均在任何创建动作前被拒绝。
- 根或内部目录 symlink/替换不能使安装、update、Registry 或清理访问允许根之外的对象。

---

### P2-01：调用取消会丢失 MetricsMonitor 停止句柄，并留下运行进程与停止采集的不一致状态

**位置**

- `monitor/internal/metrics/collector/collector.go:96-120`
- `monitor/internal/plugin/manager.go:266-292`
- `monitor/internal/plugin/manager.go:360-368`
- `dev/imple/Phase-06/Phase-06-总实施方案.md:271`

**实际证据**

`MetricsMonitor.Disable` 在等待 goroutine 退出之前先把：

```go
m.cancel, m.done = nil, nil
```

然后调用 cancel，并在 `done` 与调用方 `ctx.Done()` 之间选择。若 Backend 超时、浏览器断开或请求已经取消，Disable 可立即返回 `context.Canceled`；后续 Disable 因句柄已经清空会直接返回 nil，不能再等待上一 generation 真正退出。

Manager 的 Stop/Update 在该错误后直接返回，尚未持久化 desired stopped、尚未停止 Exporter。结果是：Exporter 与公共状态仍可保持 running，但采集 generation 已被取消，之后不再产生消息，直到管理员重试或 Monitor 重启。

本次创建并删除临时定向测试，使用一个收到取消后延迟退出的 Publisher。执行：

```bash
(cd monitor && go test -run TestReviewDisableLosesWaitHandleAfterCallerCancellation -count=1 -v ./internal/metrics/collector)
```

测试复现：第一次 Disable 因已取消 context 返回错误；第二次 Disable 立即返回，但原 Publisher/goroutine 当时仍未退出。

**影响**

- 客户端取消会改变服务内部采集状态，却使插件管理操作对外失败且不完成进程状态转换。
- 状态 API 可继续显示 running 和旧 `last_scrape_at`，无法表达采集已经停止。
- 违反“客户端取消传入操作，但不在不可回滚切换中留下不一致状态”的权威约束。

**建议整改**

1. Disable 在 generation 真正结束前保留可 join 的句柄；多个 Disable 应等待同一 generation，而不是在第一次调用时清空。
2. 对已接受的 stop/update 使用 Manager 自身的有界操作 context 完成一致性转换；外部请求取消只影响响应等待，不能留下半完成的内部状态。
3. 如果内部停止确实失败，公共状态应进入有限失败态并保留可重试/恢复句柄。
4. 增加取消发生在 scrape、publish、stop 和 update 各关键点的代表性测试。

**关闭条件**

- 调用方取消后，系统最终要么保持“进程运行且采集运行”，要么完成“采集停止且进程/desired state 已转换”；不得长期处于运行进程但无采集且状态无提示。

---

### P2-02：日常 `dev.sh` 构建当前包但不会更新已安装插件，可能长期运行旧 Exporter

**位置**

- `scripts/dev.sh:27`
- `scripts/dev.sh:639-644`
- `scripts/dev.sh:722-746`
- `scripts/down.sh:205-206`

**实际证据**

`dev.sh` 每次按根 `VERSION` 构建当前 Redis Exporter 包，但启动 Monitor 后只查询固定插件：

- `404`：调用 install；
- `200`：直接继续启动其他服务；
- 不读取已安装 `version`；
- 不比较 package Manifest、entrypoint SHA-256 或源码构建摘要；
- 不调用 update。

`.run/plugins` 不由 `down.sh` 删除，Registry、release 和 desired state 会跨日常启动保留。因此在版本升级、切换到包含新 Exporter 的分支，或同一开发批次重新构建二进制后，正常 `dev.sh` 仍可能恢复旧 release。`verify.sh` 只检查进程归属、health 和 metrics，不证明运行二进制与当前源码包一致。

**影响**

- 开发人员可在当前代码上执行 `dev.sh`/`verify.sh` 并得到成功，但实际验证的是旧 Exporter。
- 迁移到新版本时，MetricsMonitor 契约和 Exporter 实现可能不同步，形成难以诊断的假回归或假通过。
- 干净 CI 每次 install，不会发现持久化工作区升级问题。

**建议整改**

1. `dev.sh` 查询状态后解析已安装版本，并与当前 package Manifest 比较；较旧版本应通过 Monitor update 升级。
2. 对同 SemVer 的活动批次开发，明确采用可重复的重装/开发构建策略或额外构建摘要；不能静默复用旧二进制。
3. `verify.sh` 至少验证插件公共版本与当前预期一致；必要时核对受控 release 摘要。
4. 增加“先安装旧版本 → 更新源码/版本 → 再次 dev.sh → 实际运行新版本”的生命周期验收。

**关闭条件**

- 支持的日常启动路径能确定当前运行 Exporter 与当前仓库预期包一致，且更新失败安全回滚。

---

### P2-03：Monitor/Backend timeout 默认值和允许范围偏离权威配置契约

**位置**

- `dev/imple/Phase-06/Phase-06-总实施方案.md:360-369`
- `.env.example:78-86`
- `monitor/internal/config/config.go:67-95`
- `backend/internal/config/config.go:222-227`

**实际证据**

权威方案固定：

- `MONITOR_REQUEST_TIMEOUT=30s`，允许 `1s..60s`；
- plugin start/stop timeout 为 `1s..30s`；
- scrape interval 为 `1s..5m`；
- scrape timeout 为 `100ms..30s` 且小于 interval。

当前实现为：

- `.env.example`、Monitor 和 Backend 默认 `MONITOR_REQUEST_TIMEOUT=70s`，最大 `2m`；
- plugin startup timeout 允许 `100ms..1m`；
- plugin stop timeout 允许最小 `100ms`；
- scrape interval 允许到 `10m`；
- scrape timeout 允许到 `1m`。

实施记录没有把这些差异列为已批准偏差，也没有更新权威总方案。

**影响**

- 权威文档、示例环境和运行时校验对同一配置给出不同答案。
- 过短 stop/start timeout 容易制造非必要强杀或启动失败；过长请求/scrape timeout 会扩大请求占用和故障检测延迟。
- 后续 Phase 7 复用 Router/Publisher 契约时无法确定哪个边界是稳定公共契约。

**建议整改**

- 以总实施方案为准收紧代码与 `.env.example`；若 70s/2m 等值确有基于 64 MiB 上传或回滚窗口的必要性，应先更新权威方案、风险依据和验收，再保持代码。
- 为每个上下界增加表驱动配置测试，避免 Backend、Monitor、文档再次漂移。

**关闭条件**

- 总方案、`.env.example`、Backend loader、Monitor loader 和测试对默认值及所有上下界完全一致。

## 5. 已验证的正向实现

### 5.1 身份与服务端授权

- `users.role` 通过可逆迁移持久化，Repository 严格解析 `user|admin`；非法角色不会降级为 admin。
- 注册、登录和 `/users/me` 返回当前用户 role，JWT 仍只保存用户 ID。
- `RequireAdmin` 在认证后重新查询 MySQL；普通用户返回 `403 permission_denied`，不存在用户或无认证返回 `401`。
- `scripts/verify-monitor.sh` 使用同一管理员 Cookie 在 CLI 提升后成功访问插件 API，普通用户和未登录请求分别得到 `403`/`401`。

### 5.2 插件包、内部 API 与正常生命周期

- archive 拒绝绝对路径、`../`、重复 entry、链接/设备等非普通文件、错误平台、错误摘要和不完整 Manifest，并限制压缩包、总解压量、单文件、entry 数与路径长度。
- Monitor 内部接口使用常量时间 Bearer token 比较，Backend 不把 Monitor 原始响应或底层错误直接暴露给客户端。
- 正常 install/start/stop/update、单次更新失败回滚、伪造 PID 拒绝和 Monitor 重启恢复在真实隔离环境通过。
- `scripts/verify-exporter.sh` 再次证明真实 Redis INFO、目标停止、认证错误、超时、恢复、SIGTERM 和归属清理无回归。

### 5.3 MetricsMonitor、Envelope 与 Publisher

- target 启用后立即采集，之后由单 goroutine 周期采集；定向单元测试和真实验收未出现并行 scrape 或无界队列。
- 成功消息严格限制固定 metric family/type/labels/count、有限数值、counter 非负、唯一 sample 和稳定排序。
- 真实 Redis key 增加能在 Envelope 中观察到；Redis 停止产生唯一 `up=0` 的 `target_unavailable`，恢复后同一链路重新成功。
- 畸形/非有限 metrics 不生成 Envelope；HTTP Publisher 发送 Bearer、`Idempotency-Key`、JSON，只接受 `202`，失败后下一周期可恢复且不补发旧消息。

## 6. 实际执行的验证

### 6.1 通过

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)

(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)

(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)

(cd frontend && npm test -- --run)   # 9 个测试文件、48 个测试通过
(cd frontend && npm run build)

python3 -m unittest discover -s scripts/ci -p 'test_*.py'  # 24 个测试通过
python3 scripts/ci/validate_versions.py
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet

scripts/verify-monitor.sh --self-test
scripts/verify-monitor.sh
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
scripts/verify-business.sh --self-test
git diff --check
```

定向 Review 复现测试也通过，测试文件执行后已删除，未进入项目 diff：

```bash
(cd monitor && go test -run TestReviewFailedRollbackCanReportRunningWithoutProcess -count=1 -v ./internal/plugin)
(cd monitor && go test -run TestReviewDisableLosesWaitHandleAfterCallerCancellation -count=1 -v ./internal/metrics/collector)
```

### 6.2 失败

#### 完整业务回归

```bash
scripts/verify-business.sh
```

失败原因：

```text
database migration failed: load configuration: MONITOR_API_TOKEN is required
```

这是 Phase-06-04 固定完成门禁的阻断失败，详见 P1-01。

#### 分支治理

```bash
python3 scripts/ci/validate_branch.py --branch develop/1.3.4 --base-ref origin/main
```

结果：

```text
ERROR: VERSION is '1.3.3'; expected '1.3.4' for develop/1.3.4
```

`develop/1.3.4` 已在权威表中唯一分配给 Phase-06-04，因此不是分支缺少分配；当前失败准确反映批次尚未完成。本次 Review 文档不提前把根版本改成 `1.3.4`。

### 6.3 未执行或无法独立确认

- 未执行日常 `scripts/dev.sh → scripts/verify.sh → scripts/down.sh`，因为工作区已有用户拥有的 `.run/bin/gopulse-redis-exporter` 进程；Review 遵守资源归属规则，不停止或覆盖该进程。
- `gh` 命令不可用；匿名 GitHub API 查询又命中 rate limit（HTTP 403），因此本次不声称独立观察到 PR #63～#65 的远程 check 名称和结果。fetch 后的主线提交消息可以确认三个 PR 对应实现已合入。

## 7. 计划、实施记录与治理一致性

- Phase 6 总方案已正确把 Phase-06-04 分配为 `1.3.4` / `develop/1.3.4`，本次无需新增版本或分支分配。
- 总方案的状态表仍把 Phase-06-01～03 标为“未开始”，与三个合入提交和三份实施记录不一致；Phase-06-04 收口时应更新为真实状态。
- Phase-06-03 实施记录明确没有重复执行 `scripts/verify-business.sh`，并把该回归保留给 Phase-06-04；本次 Review 首次执行即发现 P1-01，说明该门禁不能省略或以 Monitor focused acceptance 替代。
- Phase-06-04 实施记录尚不存在是当前阶段未完成的真实状态，不应提前补写或把本报告当作实施记录。
- 根与 Frontend 版本保持 `1.3.3` 正确；只有整改、完整固定门禁、远程检查和 Phase-06-04 记录全部完成后，才可更新为 `1.3.4`。

## 8. Phase-06-04 建议整改顺序

1. **先修复 P1-01**，恢复 `scripts/verify-business.sh`，确认完整 Phase 0～5 回归可以真正开始并完成。
2. **修复 P1-03**，在任何新的真实安装/更新验收前关闭插件根目录安全边界，避免测试清理访问错误位置。
3. **修复 P1-02**，补齐 update 事务的双重失败和持久化失败状态机，再执行运行中/停止态 update、回滚和重启恢复。
4. **修复 P2-01**，验证请求取消不会留下运行进程与停止采集的分裂状态。
5. **修复 P2-02/P2-03**，使日常生命周期运行当前插件，并统一权威 timeout 契约。
6. 执行 Phase-06-04 文档规定的固定完成门禁和必要跨批矩阵；通过后创建真实同名实施记录、更新总方案状态和版本 `1.3.4`。
7. 推送前再次执行 Branch governance；推送后只记录实际观察到的远程检查结果。

## 9. 最终判定与关闭条件

当前判定为 **不通过（Fail）**，不是因为 Phase 6 主体方向错误，而是因为：

- 固定业务回归已实际失败；
- 更新回滚仍可产生虚假的 running 状态；
- 插件根文件系统安全边界未实现；
- Phase-06-04、版本 `1.3.4`、实施记录和远程门禁尚未完成。

Phase 6 Review 可在以下条件全部满足后关闭：

1. P1-01～P1-03 和 P2-01～P2-03 均按各自关闭条件修复并有直接测试或验收证据。
2. `scripts/verify-monitor.sh`、`scripts/verify-exporter.sh`、`scripts/verify-business.sh` 与 Phase-06-04 固定语言/脚本/Compose 门禁在最终 diff 上通过。
3. 日常生命周期升级路径证明运行的是当前预期 Redis Exporter，且不误伤既有资源。
4. Phase-06-04 实施记录如实写入实际改动、命令、结果、偏差和限制；总方案状态同步更新。
5. 根 `VERSION` 与 Frontend npm 元数据更新到 `1.3.4`，`validate_versions.py` 与 `validate_branch.py --branch develop/1.3.4` 通过。
6. `develop/1.3.4` 推送后的远程 Branch governance、Backend、Frontend、Redis Exporter、Monitor、Scripts and Compose、Integration 及自动 PR/合并检查全部成功，并最终合入主远程 `main`。
