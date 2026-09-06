# GoPulse Phase 12 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-06 |
| 用户指定权威 Review 分支 | `develop/1.9.4` |
| Review 分支创建方式 | fetch 后远端不存在 `origin/develop/1.9.4`；从最新 `origin/main` 创建本地 `develop/1.9.4`，未推送 |
| Review 基线 | `dc211ceda45e96c10cb0939bbe7522ad94c92123`（PR #108，Review 开始时与 `origin/main` 一致） |
| Phase 12 原规划基线 | `26d4355`（Phase 11 Review 整改完成版本 `1.8.4`） |
| Phase 12 计划合入点 | `eaebfcd`（PR #103） |
| Phase 12 主线实现提交 | `d9a5602`（Phase-12-01 / PR #104）、`1e63bdd`（Phase-12-02 / PR #106）、`6c3b7b5`（Phase-12-03 / PR #107） |
| Phase 12 完成记录 | `80f06c4`、`5905553`，最后一项通过 PR #108 合入 Review 基线 |
| 当前完成版本 | 根 `VERSION`、Frontend `package.json` 与 lockfile 均为 `1.9.3`；本次只新增 Review 文档，不修改版本 |
| `develop/1.9.4` 治理状态 | Phase 12 总实施方案只分配到 `1.9.3`；`validate_branch.py` 对 `develop/1.9.4` 返回 1，当前分支尚不能按仓库规则推送 |
| Review 环境 | WSL2 Linux filesystem `/home/ray/GoPulse-1.9.4`；Docker Engine 与 Compose 可用；Go、Node/npm、Python 检查均实际执行 |
| Review 范围 | Phase 12 总/拆分方案与实施记录、Dockerfile/Compose、容器运行模式、日常生命周期、权威 Compose 验收、网络/端口/身份/凭据边界、版本与分支治理 |
| Phase 12 变更规模 | 相对原规划基线 `26d4355` 至 Review 基线：75 个文件，5550 行新增、2351 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. 权威 Compose 验收是否真的从所声明的 Git revision 构建制品，并在随机 project 之外保持用户镜像和日常资源不变。
2. 日常 `dev.sh --no-build` 与 `verify.sh` 是否拒绝版本相同但 revision 已过期的本地镜像。
3. `host|container` 运行模式是否覆盖 Backend、Worker、Indexer、Monitor、Router、Marshaller、Exporter 的全部依赖 URL/host 边界。
4. Compose 是否按进程职责最小化网络、环境变量和内部身份，而不是把 Backend 的全部高权限凭据复制给所有同源作业。
5. Frontend/Backend 宿主端口、官方服务健康检查和强归属清理是否与 Phase 12 权威方案一致。
6. 用户指定的 `develop/1.9.4` 是否已有权威批次、版本和验收分配。

本次 Review 未修改产品代码。用于复现 Backend 配置问题的临时 Go 测试在执行后删除；原工作区未跟踪文件未进入本 Review worktree，也未被暂存或提交。

## 2. 总体结论

Phase 12 已交付主体完整的容器运行基线：

- Frontend、Backend、Business Worker、Search Indexer、Router、Marshaller、Monitor 和 Redis Exporter 均已有固定基础镜像、多阶段构建、数字非 root 用户、明确 entrypoint 和 OCI version/revision/source label。
- 默认 Compose 已包含六个官方基础设施、八类自研长运行进程、migration/search/Kafka 初始化作业、三张网络与七个命名卷。
- 默认 `.env.example` 下只有 Frontend/Backend 发布 IPv4 loopback 端口；数据与可观测组件不发布宿主端口，Frontend 只加入 edge 网络。
- Monitor 可从镜像内确定性 package 启动受管 Redis Exporter，并把 plugin desired state 保存在专用卷。
- Phase-12-03 的远程运行 `34014586632` 已记录为成功，包含权威 full-stack Compose job；三批实施记录和 Phase 13 交接信息均已存在。
- 本次重新执行 Backend/Monitor/Router/Marshaller/Exporter 全量测试与 vet、直接影响 package race、Frontend 58 个测试与构建、Python 27 个脚本测试、版本/Compose/Bash/self-test 等检查，除分支治理外均通过。

但是，当前实现仍不能通过 Phase 12 独立 Review：

1. 权威 Compose 验收允许 dirty source，却把镜像标成当前 `HEAD`；同时用固定 `gopulse/*:<VERSION>` tag 覆盖 Docker daemon 中的同名用户镜像，并把这些旧 image ID 从保护快照中排除。
2. 日常 `--no-build` 路径和只读 `verify.sh` 只核对版本，不核对 revision/source，能够启动并通过版本检查的过期镜像。
3. Backend VictoriaMetrics 配置漏掉了 Phase 12 新增的运行模式 host 校验；container 模式接受 loopback、`host.docker.internal` 和非法端口。
4. Compose 把完整 Backend 环境锚点复制给 Worker、Indexer 和所有一次性作业，向不需要这些能力的进程暴露 JWT、Monitor 管理 token 和 VictoriaMetrics 凭据。
5. `PUBLISHED_HOST` 未在启动前限定为 `127.0.0.1`；非 loopback 配置会先启动并暴露服务，再由 `verify.sh` 报错，而且容器会被保留。
6. 用户指定的 `develop/1.9.4` 没有 Phase 12 权威批次分配，分支门禁失败。
7. MySQL、Redis、VictoriaMetrics 健康检查及 VictoriaMetrics 主进程把密码放入运行时 argv/Compose command，与权威方案的健康检查凭据边界不一致。

本次未发现 P0 或 P1 问题；共记录 **6 个 P2、1 个 P3**。这些问题不会否定已完成的主闭环，但会破坏“可重现制品、强归属资源、运行模式安全、最小凭据和治理可推送”这些 Phase 12 明确合同，因此结论为 Fail。

## 3. Findings 摘要

| ID | 级别 | Finding | 直接影响 |
| --- | --- | --- | --- |
| P2-01 | P2 | 权威验收允许 dirty source，并覆盖固定版本的用户镜像 tag | 验收可证明一个无法由 label revision 重建的镜像，并修改随机 project 之外的 Docker 全局状态 |
| P2-02 | P2 | `dev.sh --no-build` / `verify.sh` 不校验 OCI revision | 相同版本下的旧代码镜像可被启动并被日常验证接受 |
| P2-03 | P2 | Backend VictoriaMetrics URL 漏掉 runtime-mode host 校验 | container 模式可退回 loopback/宿主旁路，非法端口也在启动配置阶段通过 |
| P2-04 | P2 | Backend 环境锚点向 Worker/Indexer/一次性作业过度分发凭据 | 较低职责进程获得 JWT、Monitor 管理和 VictoriaMetrics 身份，可跨服务扩大失陷面 |
| P2-05 | P2 | 非 loopback `PUBLISHED_HOST` 在启动后才被发现 | Frontend/Backend 可短期或持续暴露到所有宿主接口，并使用开发态凭据/Cookie 配置 |
| P2-06 | P2 | `develop/1.9.4` 无权威批次分配 | Review/后续整改分支无法通过仓库治理门禁或合规推送 |
| P3-01 | P3 | 官方服务健康检查与 VM command 把密码放入 argv | 与方案明示的“不在 command line 放 token/password”不一致，增加同容器/宿主诊断面的凭据暴露 |

## 4. 详细 Findings

### P2-01：权威验收允许 dirty source，并覆盖固定版本的用户镜像 tag

**位置**

- `scripts/verify-compose-observability.sh:107-150`
- `scripts/verify-compose-observability.sh:271-317`
- `scripts/verify-compose-observability.sh:613-616`
- `deploy/compose.yaml:244-251,277-284,297-304,321-327,454-517`
- 对照 `dev/imple/Phase-12/Phase-12-03-全栈Compose验收与阶段收口.md:35-47,229-236`

**问题**

权威 runner 只保存当前 Git status，并在结束时确认 status 没有变化：

```bash
git status --porcelain=v1 --untracked-files=all -z > "$SNAPSHOT_DIR/git-status"
cmp -s "$SNAPSHOT_DIR/git-status" <(git status ...)
```

它没有要求工作树 clean。随后使用：

```bash
REVISION=$(git rev-parse HEAD)
compose build backend ... redis-exporter
```

Docker build context 会包含未提交但未被 `.dockerignore` 排除的产品源文件，而镜像仍被标记为 `REVISION=$HEAD`。因此 dirty code 可以通过全部 image revision 断言，验收结果却无法从该 revision 重建。

同时，Compose 使用固定全局 tag `gopulse/<service>:$VERSION`。runner 会主动重建这些 tag，并明确把已有同名 tag 的旧 image ID 放进 `replaced-images`，从保护快照中排除。随机 project 隔离了 container/network/volume，却没有隔离 Docker daemon 的 image tag namespace。

**影响**

- 权威验收可以给“HEAD + 未提交源代码”的混合制品发出成功结论，破坏 OCI revision 的可追溯性。
- 用户或日常 project 已存在的 `gopulse/*:1.9.3` tag 会被改指向本次验收构建；旧 image ID 可能因运行容器而暂存，但原 tag 归属不会恢复。
- 后续 `dev.sh --no-build`、container recreate 或另一个 worktree 可能使用被验收重写的镜像，而不是该工作区原先的镜像。
- 实施记录把排除精确版本 tag 描述为预期替换，但 Phase-12-03 验收标准要求“日常 project、用户卷/镜像和工作区改动保持”。当前实现以放宽断言代替保持用户镜像。

**建议整改**

1. 权威验收在任何 Docker build 前要求 tracked/untracked build-context source clean；如允许特定 Review 文档 dirty，必须证明它不进入任何 build context，而不是笼统接受全部 status。
2. 为验收使用运行级唯一 tag，例如 `gopulse/backend:${VERSION}-accept-${TOKEN}`，并让临时 Compose override/变量引用该 tag；不要覆盖日常版本 tag。
3. 如果必须复用固定 tag，开始前保存 ref→image ID 映射，结束时原子恢复 tag，并验证映射完全一致；这仍不如唯一 tag 安全。
4. 增加行为测试：dirty 产品源应在 Docker access/build 前失败；预置同版本 tag 后运行失败/成功/signal 路径，原 tag 映射必须保持。

### P2-02：`dev.sh --no-build` 与 `verify.sh` 不校验 OCI revision

**位置**

- `scripts/dev.sh:53-85`
- `scripts/verify.sh:84-95`
- 对照 `dev/imple/Phase-12/Phase-12-总实施方案.md:223-224`

**问题**

`dev.sh` 总是计算当前 Git revision，但 `--no-build` 时不会在 `compose up` 前核对本地 tag 的 OCI revision。`verify.sh` 对运行镜像只检查：

- 数字 UID:GID；
- `org.opencontainers.image.version == VERSION`。

它没有检查：

- `org.opencontainers.image.revision == git rev-parse HEAD`；
- source label；
- 运行容器 image ID 是否等于当前版本 tag 的 image ID；
- Backend/Worker/Indexer 等同版本镜像是否来自同一 revision。

本次 Review 环境已有直接证据：

```text
HEAD=dc211ceda45e96c10cb0939bbe7522ad94c92123

gopulse/backend:1.9.3  revision=13740c1448e0905250fec1baf5aec8eb5b53fa25
gopulse/frontend:1.9.3 revision=13740c1448e0905250fec1baf5aec8eb5b53fa25
gopulse/monitor:1.9.3  revision=13740c1448e0905250fec1baf5aec8eb5b53fa25
```

这里代码树可能因后续仅文档合入而等价，但脚本无法区分“仅文档提交”和“同一目标版本内已有产品代码变化”。所有同批提交共享一个目标版本，版本相同并不能证明代码相同。

**影响**

- `--no-build` 可以在切换提交、变更同批代码或另一个 worktree 覆盖 tag 后启动旧镜像。
- `verify.sh` 仍可能打印通过，使用户以为当前 checkout 已被验证。
- 版本标签存在但 revision 合同失效，Phase 13 若把这些制品作为迁移输入，会得到错误的代码来源。

**建议整改**

- `dev.sh --no-build` 在 `compose up` 前检查全部自研产品 tag 的 version/revision/source；任一缺失或 revision 不等于当前 HEAD 时安全拒绝并提示去掉 `--no-build`。
- `verify.sh` 同时核对运行 container image ID、当前 tag image ID、version、revision 和 source；全部自研镜像必须使用同一 revision。
- 为“同版本、不同 revision”增加最小 fake-Docker/self-test 或 image-label 测试，证明拒绝发生在启动前。

### P2-03：Backend VictoriaMetrics URL 漏掉 runtime-mode host 校验

**位置**

- `backend/internal/config/config.go:648-672`
- `backend/internal/config/runtime_mode.go:42-59`
- `backend/internal/config/runtime_mode_test.go:48-70`

**问题**

Phase 12 为 Backend 依赖增加了 `validateDependencyHost` / `validateOriginHost`：

- host 模式只允许 loopback；
- container 模式只允许服务 DNS，并拒绝 `localhost`、`host.docker.internal` 和固定 IP。

MySQL、Redis、RabbitMQ、Elasticsearch、Monitor 与 log ship URL 均接入了这套检查，但 `loadVictoriaMetricsConfig` 虽然接收 `runtimeMode` 参数，却从未调用 `validateOriginHost`。现有 unsafe-container-host 表也没有 `BACKEND_VICTORIAMETRICS_URL` 项。

本次临时 Go 测试在 container 环境中分别设置：

```text
http://127.0.0.1:8428
http://host.docker.internal:8428
http://victoriametrics:99999
```

三项均被 `LoadFrom` 接受：

```text
=== RUN   TestReviewPhase12AcceptsUnsafeVictoriaMetricsContainerHost
--- PASS: TestReviewPhase12AcceptsUnsafeVictoriaMetricsContainerHost (0.00s)
```

临时测试通过表示问题被稳定复现，不表示这些配置正确；文件已删除。

**影响**

- Backend 的 VictoriaMetrics 路径成为运行模式安全合同中的唯一明显缺口。
- container 模式可以退回容器 loopback、宿主旁路或不可用端口；配置阶段不再 fail closed。
- host 模式也不能保证 VictoriaMetrics Basic credential 只发送给 loopback origin。
- 非法端口会推迟到真实管理查询时失败，使启动配置看似有效但管理员 Metrics 功能持续降级。

**建议整改**

- 在 scheme/path/query/fragment 检查后调用 `validateOriginHost(runtimeMode, "BACKEND_VICTORIAMETRICS_URL", parsed)`。
- 对显式 port 执行 `1..65535` 校验；是否要求必须显式端口应与 Monitor/Marshaller 的内部 origin 合同统一。
- 在现有 runtime-mode 表中增加一个合法 VM service DNS，以及 loopback、`host.docker.internal`、固定 IP、非法端口各一个代表性拒绝；无需复制全部 URL parser 用例。

### P2-04：Backend 环境锚点向低职责进程和一次性作业过度分发凭据

**位置**

- `deploy/compose.yaml:6-67`
- `deploy/compose.yaml:216-235,277-319,344-355`
- `backend/internal/config/worker.go:44-50`
- `backend/internal/config/search_indexer.go:19-64`
- `backend/cmd/migrate/main.go:24-35`
- `backend/cmd/admin-role/main.go:68-77`

**问题**

Compose 定义一个包含 Backend 全部能力的 `x-backend-environment`，然后原样赋给：

- `migrate`；
- `search-init`；
- `backend`；
- `business-worker`；
- `search-indexer`；
- `admin-role`。

因此这些容器全部获得 `AUTH_JWT_SECRET`、Redis password、Monitor 管理 token、log ingest token、VictoriaMetrics username/password、RabbitMQ URL、MySQL password 和各类 Backend 查询配置。

代码本身已经说明 Worker 的 loader “intentionally reads only MySQL, RabbitMQ, and worker settings”且不需要 HTTP、Redis、JWT 或 Cookie；Indexer 也只需要 MySQL、RabbitMQ、Elasticsearch、log ship 和自身 worker 参数。渲染 `.env.example` 后，本次实际观察到 `migrate`、`search-init`、`business-worker`、`search-indexer` 均包含：

```text
AUTH_JWT_SECRET
MONITOR_API_TOKEN
BACKEND_VICTORIAMETRICS_PASSWORD
LOG_MONITOR_INGEST_TOKEN
RABBITMQ_URL
MYSQL_PASSWORD
```

**影响**

- Worker/Indexer 已连接 observability 网络；其中任一进程失陷后可直接读取 Monitor 管理 token 和 VictoriaMetrics credential，而这些能力与其业务职责无关。
- JWT secret 允许低职责进程扩大到用户身份伪造面；Monitor token 可扩大到插件管理面。
- 一次性 migration/search 作业和 `admin-role` 容器也携带大量不必要的长期身份，增加诊断 dump、`docker inspect` 和进程失陷时的暴露范围。
- 当前网络隔离看似细分，但环境身份把多个安全域重新合并在同一进程权限中，不利于 Phase 13 按 workload/Secret 最小化迁移。

**建议整改**

1. 把 Compose 环境拆成 Backend、Worker、Indexer、migration、search-init、admin-role 六个最小锚点或显式块。
2. 为 migration/admin-role 增加只读取 MySQL 的最小 config loader，避免为了复用 Backend binary 而要求 JWT、Monitor 和 VM 配置。
3. Worker 只获得 MySQL、RabbitMQ、必要 log ship 身份和 worker 参数；Indexer 额外获得 Elasticsearch，不获得 JWT/Monitor admin/VM 查询身份。
4. 增加渲染后测试，明确断言每个 service 不包含不属于其职责的高权限变量。

### P2-05：非 loopback `PUBLISHED_HOST` 在启动后才被发现，并保留已暴露容器

**位置**

- `.env.example:10-14`
- `deploy/compose.yaml:244-265,321-334`
- `scripts/dev.sh:34-85`
- `scripts/verify.sh:72-82`
- 对照 `dev/imple/Phase-12/Phase-12-总实施方案.md:221-224`

**问题**

权威方案要求 Bash 入口在冷启动前验证宿主 loopback 端口。当前 `dev.sh` 不读取或验证 `PUBLISHED_HOST`，Compose 则直接把它用于 Frontend/Backend 端口发布。

本次仅修改临时 env 并执行 Compose 静态渲染，`PUBLISHED_HOST=0.0.0.0` 得到：

```text
frontend [{'host_ip': '0.0.0.0', 'target': 8080, 'published': '5173', ...}]
backend  [{'host_ip': '0.0.0.0', 'target': 8080, 'published': '8080', ...}]
```

`verify.sh` 确实会拒绝非 `127.0.0.1`，但它由 `dev.sh` 在 `compose up --detach --wait` 成功之后调用。验证失败时 `dev.sh` 没有 down/回滚逻辑，已启动的容器和端口会被保留。

`.env.example` 还写明可为 remote access 显式设置该值，但同一文件使用固定 development-only credential、`AUTH_COOKIE_SECURE=false` 和无 TLS 的 HTTP 入口；这不是一个安全的远程访问模式。

**影响**

- 一次误配置会先把 Frontend/Backend 暴露到 WSL/宿主可达接口，再得到失败结果。
- 因失败路径保留资源用于诊断，暴露可能持续到用户手工执行 `down.sh`。
- Backend 授权仍存在，但开发态固定凭据、明文 Cookie/HTTP 和开放注册面不应被隐式当成远程部署合同。

**建议整改**

- `dev.sh` 在任何 build/up 前解析 env，并要求 `PUBLISHED_HOST` 精确等于 `127.0.0.1`；同时验证 Frontend/Backend port 是合法单端口值。
- 增加 self-test/fake-Docker 用例，证明 `0.0.0.0`、`localhost`、IPv6 wildcard 在 Docker access 前被拒绝。
- 如果确需远程开发访问，使用单独显式 override/profile，定义 TLS、Cookie secure、credential rotation、允许来源和风险提示；不要复用默认本地生命周期。

### P2-06：`develop/1.9.4` 没有 Phase 12 权威批次分配

**位置**

- `dev/imple/Phase-12/Phase-12-总实施方案.md:61-79`
- `scripts/ci/validate_branch.py:97-119`

**问题**

Phase 12 权威分配表只包含：

- Phase-12-01 → `1.9.1` / `develop/1.9.1`；
- Phase-12-02 → `1.9.2` / `develop/1.9.2`；
- Phase-12-03 → `1.9.3` / `develop/1.9.3`。

本次用户指定的 `develop/1.9.4` 在权威表中不存在，根 `VERSION` 仍是已完成产品版本 `1.9.3`。实际执行：

```text
python3 scripts/ci/validate_branch.py --branch develop/1.9.4 --base-ref origin/main
ERROR: develop/1.9.4 must map to exactly one authoritative allocation; found 0
```

即使先增加分支行，真正整改完成前 `VERSION` 仍不会等于 `1.9.4`，所以该门禁需要按“新增整改批次 → 完成整改 → 更新版本”的顺序闭合。

**影响**

- 当前本地分支可承载 Review 文档，但不能合规推送或创建普通开发 PR。
- 后续产品整改若直接提交到该分支，仍会被 Branch governance 阻断。
- 缺少拆分计划和验收标准时，Review findings 无法形成符合仓库规则的独立执行批次。

**建议整改**

在开始产品修复前：

1. 在 Phase 12 总实施方案中增加 Phase-12-04 → `1.9.4` / `develop/1.9.4` 的权威分配。
2. 新增对应拆分实施方案，验收标准直接映射本报告 findings，并限定必要回归。
3. 仅新增 Review/计划文档时不修改 `VERSION`；Phase-12-04 实际完成时再同步根和 Frontend 版本为 `1.9.4`，并创建对应实施记录。

### P3-01：官方服务健康检查与 VictoriaMetrics command 把密码放入运行时 argv

**位置**

- `deploy/compose.yaml:143-173`
- `deploy/compose.yaml:429-447`
- 对照 `dev/imple/Phase-12/Phase-12-总实施方案.md:253-260`

**问题**

总方案明确要求健康检查不在 command line 或普通输出打印 token/password。当前配置包含：

```yaml
mysqladmin ... --password="$MYSQL_ROOT_PASSWORD"
redis-cli ... -a "$REDIS_PASSWORD" ping
wget --header "Authorization: Basic <encoded username:password>" ... /health
```

Shell 扩展后，MySQL/Redis 密码或 VM Basic header 会出现在短生命周期探针进程 argv 中。

此外，VictoriaMetrics 主进程 command 在宿主 Compose 插值阶段直接变成：

```text
-httpAuth.password=local-victoriametrics-password32
```

本次 `docker compose config --format json` 已确认明文密码存在于 service command。VictoriaMetrics `v1.151.0 -help` 同时表明该 flag 支持 `file:///abs/path`，当前并非上游能力限制。

**影响**

- 能读取容器进程列表、Docker inspect 或诊断快照的主体可看到不必要的密码/Basic credential 表达。
- 与实施方案宣称的健康检查凭据边界不一致，现有 image config/history 扫描也不会检查运行 service command/healthcheck argv。
- 风险主要限于已有容器/宿主诊断权限的主体，因此定为 P3，而不是外部认证绕过。

**建议整改**

- VictoriaMetrics 使用 file-backed password flag，并以专用只读 secret/config mount 提供；不要把实际密码插值到 command。
- Redis 探针优先使用 `REDISCLI_AUTH` 环境而不是 `-a` 参数；MySQL 使用不把密码放入 argv 的本地配置/credential file，或使用无需鉴权且能准确表达 liveness 的本机探针。
- VM healthcheck 使用不会把 Basic header 写入 argv 的方式，或将 liveness 与 authenticated readiness 分开。
- 扩展 Compose 静态测试，扫描最终 service `command` 与 `healthcheck.test`，而不仅扫描镜像 layer/history。

## 5. 已通过的关键检查

### 5.1 代码、静态检查与构建

以下命令在本次 Review 基线上实际执行并通过：

```text
(cd backend && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd monitor && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd router && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd exporters/redis && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)

(cd backend && go test -race -count=1 \
  ./internal/config ./internal/exporterplugin ./internal/observability/logship \
  ./internal/metricquery ./internal/worker ./internal/search ./internal/http/...)
(cd monitor && go test -race -count=1 \
  ./internal/config ./internal/plugin ./internal/metrics/... ./internal/events)
(cd marshaller && go test -race -count=1 \
  ./internal/config ./internal/consumer ./internal/victoriametrics ./internal/elasticsearch)
(cd exporters/redis && go test -race -count=1 ./...)

(cd frontend && npm ci)
(cd frontend && npm test -- --run)  # 11 files / 58 tests
(cd frontend && npm run build)

python3 -m unittest discover -s scripts/ci -p 'test_*.py'  # 27 tests
python3 scripts/ci/validate_versions.py
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh \
  scripts/verify-compose-observability.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-compose.sh --self-test  # 拒绝 6 个不安全 project name
git diff --check
```

Frontend 首次直接执行测试时因新 worktree 尚无 `node_modules` 而报告 `vitest: not found`；执行 lockfile 固定的 `npm ci` 后，测试、typecheck 和 production build 均通过。该问题属于 Review 环境准备，不是产品 finding。

`validate_branch.py --branch develop/1.9.4` 是唯一预期失败项，已记录为 P2-06，而不是误报为通过。

### 5.2 已有真实容器证据与本次执行边界

仓库实施记录显示：

```text
Phase-12-03 remote run 34014586632: PASS
Pull Request #107 merge commit 6c3b7b5
```

该远程运行覆盖 Branch governance、各 Go/Frontend/Scripts jobs、Integration 和 Full-stack Compose acceptance。Review 基线在 Phase-12-03 产品树之后仅增加计划完成记录，未改变产品实现。

本次没有重新执行无参数 `scripts/verify-compose.sh`。原因不是缺少 Docker，而是 P2-01 已确认该命令会主动覆盖 Docker daemon 中现有 `gopulse/*:1.9.3` tag，并把这些旧 tag 对应 image ID 从保护快照排除。继续运行会在已知强归属缺陷下修改共享用户镜像，不适合作为 Review 的无副作用验证。

本次仍执行了 Compose render、self-test、全部模块测试/vet/race、Frontend 构建和脚本测试，并核对权威远程完整矩阵证据。修复 P2-01 后，整改批应在唯一 tag/clean tree 条件下重新运行完整矩阵。

### 5.3 定向 Finding 复现

实际执行的定向复现包括：

```text
# P2-02：本地 1.9.3 产品镜像 revision 与 Review HEAD 不一致
HEAD=dc211ceda45e96c10cb0939bbe7522ad94c92123
image revision=13740c1448e0905250fec1baf5aec8eb5b53fa25

# P2-03：临时 Go 测试确认三类不安全 VM URL 被 container mode 接受
(cd backend && go test -count=1 ./internal/config \
  -run TestReviewPhase12AcceptsUnsafeVictoriaMetricsContainerHost -v)

# P2-04：Compose JSON 渲染确认 Worker/Indexer/一次性作业持有完整 Backend 凭据集
docker compose ... config --format json

# P2-05：临时 env 设置 PUBLISHED_HOST=0.0.0.0 后静态渲染
frontend host_ip=0.0.0.0
backend  host_ip=0.0.0.0

# P2-06：分支治理
python3 scripts/ci/validate_branch.py --branch develop/1.9.4 --base-ref origin/main

# P3-01：VM command 与上游本地镜像 help
docker compose ... config --format json
docker run --rm victoriametrics/victoria-metrics:v1.151.0 -help
```

临时 Backend 测试文件执行后已删除；Review worktree最终只新增本报告。

## 6. Review 结论与整改顺序

### 6.1 结论

Phase 12 的默认容器拓扑、业务/搜索/可观测主闭环、非 root 产品镜像、内部网络、持久卷、初始化作业和远程 full-stack 证据均已建立，当前状态不是“容器化不可用”。

但制品 revision 与用户镜像归属、日常旧镜像拒绝、VictoriaMetrics 运行模式、最小凭据、启动前 loopback 边界和开发分支治理仍未满足权威方案，因此本次 Review 结论为：

> **Fail：不得把当前状态视为 Phase 12 Review 已关闭。**

### 6.2 建议整改顺序

1. 先在 Phase 12 总实施方案中分配 Phase-12-04 → `1.9.4` / `develop/1.9.4`，新增最小整改实施方案，使分支治理合法。
2. 优先修复 P2-01/P2-02：权威验收使用 clean tree + 随机唯一 image tag；日常 `--no-build` 和 `verify.sh` 强制 version/revision/source/image-ID 一致。
3. 修复 P2-03：把 VictoriaMetrics 纳入 Backend runtime-mode origin/port 校验，并补代表性正负配置测试。
4. 修复 P2-04：按 service 拆分最小环境与配置 loader，重点移除 Worker/Indexer 的 JWT、Monitor admin 和 VM credential。
5. 修复 P2-05：在 build/up 前拒绝非 IPv4 loopback 发布；如保留远程模式，必须使用单独显式安全合同。
6. 修复 P3-01：把 VM 主密码与官方服务健康检查凭据移出 argv，并增加最终 Compose command/healthcheck 扫描。
7. 在最终 diff 上运行受影响 Go/Frontend/Scripts 检查、`validate_versions.py`、`validate_branch.py --branch develop/1.9.4`、clean-tree 唯一-tag `scripts/verify-compose.sh` 和 `git diff --check`。
8. Phase-12-04 实际完成后同步根/Frontend 版本到 `1.9.4`，创建对应实施记录并停止，不扩展为 Kubernetes、供应链或无关容器加固。

## 7. 非阻断说明

- 本次没有发现普通用户可绕过 Backend admin 授权、Frontend 默认直接访问内部服务、默认 `.env.example` 下内部数据端口发布到宿主，或 Monitor 出现第二个受管 Exporter 实例的证据。
- Backend/Frontend/可观测模块的单元、构建、vet 和直接 race 均通过；findings 集中在容器制品归属、配置和运行安全边界，不要求推翻现有业务实现。
- Elasticsearch 单节点且关闭安全、Kafka PLAINTEXT、Compose 无 TLS/HA、多架构未作为门禁，均是 Phase 12 已记录的开发/验收边界，本次不升级为新的生产供应链或高可用 finding。
- 冻结的 PowerShell 生命周期未在 Phase 12 修改，符合 WSL2/Bash-only 平台规则。
