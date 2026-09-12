# Phase-15-06：Compose 集成验收与阶段收口开发记录

## 状态与基线

- 状态：2026-09-12 本地实施与固定验收完成；根与受管元数据已同步 `1.12.6`，待合入 `upstream/main`，不宣称主线 Phase 15 已完成。
- 目标：`1.12.6` / `develop/1.12.6`；2026-09-12 执行 `git fetch upstream`，从 `upstream/main` 的 `fd9d2b7` 创建分支。前五批代码和同名记录均在该主线基线中，根/两 Frontend 的已完成版本为 `1.12.5`。
- 开工已有未跟踪文件 `~`，不读取、不修改、不暂存或提交。
- 本批不新增告警语法、角色、插件或 Phase 16 能力；只接线跨批 Compose 验收。没有第三方依赖源码检查或独立 Review。

## 证据复用与本批范围

保留前五批同名日志中相关输入未变化的 Backend/user/alert/logquery/eventquery/adminoverview、组件 catalog 和管理前端 package 证据；不重复运行这些专项 package 测试。本批构建两个正式 Frontend 镜像时运行 Dockerfile 固定的 tests/typecheck/build。新增浏览器仅覆盖本批 clean-install 和真实三源目录规则创建；角色、双应用、dashboard 和 Phase 13 主路复用既有 browser scenario。

新增固定入口 `bash scripts/verify-compose.sh --phase15`，实现位于 `scripts/ci/verify_phase15_closure.py`；拒绝 `--keep`。复用已验证的随机项目名、动态 loopback edge 端口、受控 SQL 只读检查、真实 source/state adapter 和 Docker ownership 检查；额外 override 取消 Backend 宿主端口，所有宿主 API 与容器浏览器只访问 frontend origin。凭据和账户状态只保存于权限受限 `.run` 目录，清理时删除。

升级使用已有真实 `gopulse/backend:1.11.5` / `gopulse/monitor:1.11.5` Phase 14 runtime 生成 schema、两个 admin、普通用户/业务/搜索、活动 Cookie、六插件及遥测历史；通过旧版本 operations 命令赋予旧 admin，不直接 SQL 赋权。升级后保留插件的旧版本和 desired state，不伪称重新安装即迁移。

## 实际执行记录（历史轮次）

- `python3 -m py_compile scripts/ci/verify_phase15_closure.py`：通过。
- `bash scripts/verify-compose.sh --self-test`：通过。
- `bash scripts/verify-role-management.sh --self-test`：通过。
- `bash scripts/verify-alerts.sh --self-test`：通过。
- `bash scripts/verify-admin-frontend.sh --self-test`：通过。
- 第一轮 `bash scripts/verify-compose.sh --phase15`：未整体通过。项目 `gopulse-p1401-774de08b3a79`，证据 `.run/gopulse-p1401-774de08b3a79/`，输出 `/tmp/gopulse-phase15-closure.log`。当前产品镜像构建、clean install 注册/无自动提权/唯一幂等 bootstrap/浏览器登录通过；Phase 14 fixture 的 Kafka 安装因验收输入使用错误固定 topic 返回 400。修正为已发布 `gopulse-observability-v1` / `gopulse-marshaller-metrics-v1`，没有放松产品校验。finally 清理通过，前后 container/network/volume 集合完全相同。第一轮构建标签版本仍为 `1.12.5`，不算目标版本制品证据。
- 第二轮输出 `/tmp/gopulse-phase15-closure-2.log`，项目 `gopulse-p1401-0a07cbb3ca73`：未整体通过（后文记录真实阻断）。镜像采用目标版本 `1.12.6`，根完成版本仍待全部验收通过后才更新。
- 根据实际管理 UI 的 catalog tuple 修正 Events 浏览器选择器：`event_name=exporter_plugin_started,severity=info,operation=start`；UI 固定 tuple 不提供 plugin_id/source 附加选择，不能假装存在这些选项。通过真实 stop→start 产生事件，不是对已经 running 的插件幂等 start 后期待新事件。仅重新构建第二轮 acceptance 镜像，记录 `browser-fixture-build.log`，未重复未变的产品 package 检查。

## 当前完成条件与限制

第八轮已完成升级/重跑、完整角色和双应用矩阵、三源真实持续/重启/恢复/关闭、串行故障、Phase 13/14 回归、制品/脱敏/SIGTERM 和最终资源快照；最终结果见下文。前七轮失败保留为历史，不作为整体通过依据。

本批只证明 WSL2/Linux/Bash/单 Docker daemon 下的 Compose；Phase 16 仍需真实 Linux/macOS/Windows 支持矩阵、多架构制品、共享生命周期、从 `1.9.4` 升级及备份恢复。Phase 15 本地完成也必须写待合入，不能在第六批尚未合入主线时宣称阶段或 Milestone 4 完成。

### 第二轮发现的实际阻断与最小修复

第二轮已通过两个旧 admin/活动 Cookie 的角色升级、最小 ID bootstrap、用户密码/创建时间/业务不变、六插件 desired state/版本不变、migration 重跑和 bootstrap DELETE 外键拒绝；资源清理通过。随后 browser 发现两项失败：

1. 旧 `/admin/observability` 测试期望 `/admin/metrics`，但已发布 Nginx 契约重定向 `/admin/`。修正测试的该单项断言，不改路由。
2. 新 Monitor 不包含旧六插件 `1.11.5` 的受信 release，升级后插件 observed state 为 failed。真实持久数据/信任边界阻断，因此最小扩展到 `deploy/docker/observability.Dockerfile`：归档原 fixture 标记 revision 的 exporter 源码，按原工具链/参数重现六个历史包，对原镜像中提取的 SHA-256 逐项严格校验，编译为 retained release 并随镜像只读携带。不读取第三方源码，不引入运行时信任跳过，也不从可写插件卷建立信任。升级断言加强为六个 retained plugin 必须恢复 running，而非仅 desired state 未变。

原镜像 digest 取证通过唯一随机命名/标签、`--network none --read-only --rm` 的临时容器完成；未停止、修改或删除任何原有容器。归档是 39 KiB 纯源码，不提交二进制或凭据。构建哈希和真实六插件恢复是直接验证门禁。

### 第三轮与 clean-install setup 补线

第三轮 `gopulse-p1401-a69abb740551` / `/tmp/gopulse-phase15-closure-3.log` 已通过历史六包精确 SHA-256 构建、六插件 running 恢复、角色升级/API 拒绝、4 个双应用 browser、2 个 dashboard browser 和 6 个 Phase 13 browser（20.0s），六组件/六插件 Metrics、Logs/Events 查询与真实 healthy overview。三源创建脚本在登录请求完成前主动导航，导致请求被取消、重新落回 login；为 helper 增加登录落点等待，不改变应用认证。整个项目已清理且前后资源集合相同。

对照 clean-install 条款发现既有安全跳转缺少固定 setup 提示。本批最小补线：`/users/me` 在 repository 支持时提供可选 boolean `management_setup_available`；仅表达只读初始化状态，不用于授权。查询错误省略 unknown，不将 setup 查找失败升级为社交失败。两端严格 DTO 校验仅额外允许该 boolean，用户 shell 在明确 false 时显示固定提示；普通用户仍不挂载管理页、不发管理数据请求。新增直接测试只覆盖该公共合同与 malformed boolean；clean browser 增加真实提示断言。

已执行并通过：

- `(cd backend && go test ./internal/auth ./internal/user ./internal/http)`；`/tmp/gopulse-phase15-setup-backend.log`。
- `(cd frontend && npm test -- src/services/setup.test.ts)`：1/1；`/tmp/gopulse-phase15-setup-user.log`。
- `(cd admin-frontend && npm test -- src/services/setup.test.ts)`：1/1；`/tmp/gopulse-phase15-setup-admin.log`。

这是实际缺失的 setup 接线和已变化的认证响应合同的直接回归范围，不是额外覆盖率活动。Exporter 故障门禁改为先核对归属容器/进程可执行身份，再杀死 RabbitMQ Exporter，验证 failed 与恢复；有意 stop 的既有 browser 操作不能替代非预期进程退出故障。

### 第四轮已获得的阶段证据

项目 `gopulse-p1401-398477f96617`，输出 `/tmp/gopulse-phase15-closure-4.log`；证据目录同名 `.run/`。最终生产代码的镜像构建内用户端 17 files / 63 tests、管理端 7 files / 24 tests、两端 typecheck/build 均通过；`build.log` 记录实际结果。前一轮已经执行六个历史 archive SHA-256 校验，本轮无相关输入变化的 Docker build cache 保留该有效结果；源归档 SHA-256 为 `cc788700da4f5450482d721ec477b7fe656ac41ec45596c1ae6c5fe6f3c03d06`。

clean-install browser 现在确实核对“管理尚未初始化”提示，非仅安全重定向。角色升级、双应用四用例、大屏两用例、Phase 13 六用例，以及真实 RabbitMQ Exporter 非预期退出/failed/恢复均已通过。

为明确区分“历史未丢失”和“仅有新数据”，在本轮同一运行环境补跑只读 `Phase15Acceptance.preserved_observability()`（构造现有项目的只读调用上下文，不调用构造器/start/cleanup，不改主验收资源）。它已接入固定入口的 upgrade 后步骤；当前运行解释器已载入此前代码，所以该项以等价补充调用计入，不伪称由原进程执行。输出 `/tmp/gopulse-phase15-preserved-history.log`，证据 `preserved-observability.json`：在最后一个旧插件安装时刻 `2026-09-12T14:17:57.865870832Z` 之前的 **22 条 Logs、5 条 Events、1 个 Redis 原始指标点** 仍存在；不使用升级后新样本冒充历史保留。该方法只读既有 ES aliases / VM export，未直接写存储。

`--phase15 --keep` 已确认在 Docker 访问前被拒绝（非零退出码 1）；首次检查错误地期望参数退出码 2，随后按合同“必须拒绝”核对非零结果通过，没有改变运行清理语义。`bash -n scripts/verify-compose.sh` 和最终新增方法的 Python 严格语法检查通过。

第四轮随后通过三源 recovered、三种 closed 原因、VM/ES 分区故障 browser、evaluator disabled browser、audit/redaction browser 和 Backend 日志脱敏；但不算整体成功：审计下一页的验收调用重复携带 signed cursor 内已有的 `limit`，被既有合同正确返回 400。修正为既有角色验收使用的 cursor-only 下一页 URL，不改 Backend。强归属清理和宿主监听端口比较均通过。

为满足“全部自研镜像”而非仅八个常驻服务，固定入口最后增加独立 Redis Exporter 第九个产品镜像的构建、真实十类指标、版本/revision、numeric UID、read-only、无宿主端口/business-only 和 SIGTERM 门禁；它使用同一强归属项目，没有另起全仓库测试。第五轮将历史保留补验、正确审计翻页和独立 Exporter 一并在同一个入口运行；产品代码和前三源状态机未再修改。第四轮成功的前端 package 构建仍由无相关输入变化的 build cache 复用，不人为重跑。

## Phase 16 交接合同

以下合同不得在后续产品化中改成另一套授权/告警/路由实现；本批正式收口仍以最终门禁及合入状态为准。

- **双应用**：两个独立 Frontend 工程及镜像，同源 `/admin/` 与用户入口；安全 redirect、HttpOnly Cookie、实时数据库角色分流；普通用户不挂载管理 DOM。setup 提示为 `/users/me` 可选只读 boolean，不是授权凭据、初始化操作或社交 readiness 条件。
- **身份与审计**：最终 `user|super_admin`；最小 ID 旧 admin 迁移为受保护 bootstrap；空库只能由规范 `admin-role bootstrap --user-id` 声明；非 bootstrap 按 ID 提升/降级，成功变更和 Monitor 跨进程 requested/completed/unknown 审计保留。
- **三源告警**：固定 Metrics/Logs/Events catalog、窗口/持续/reducer/operator；MySQL 持久规则/状态/incident/history，firing 重复抑制、unknown/stale 不伪恢复，closed 与 recovered 分离；不扩展外部通知、细粒度 RBAC 或自动修复。
- **Compose 与制品**：八个常驻自研服务加独立 Redis Exporter 镜像；六插件、六组件、大屏和社交业务保留源局部故障边界。`1.11.5` 六插件使用固定源码及精确 archive hash 作为 retained release，未承诺任意历史插件版本均可迁移，也未放宽可写卷/上传包的信任边界。
- **Phase 16 独立责任**：多架构制品、至少 Linux amd64/macOS arm64/Windows amd64 真实宿主支持矩阵、共享产品生命周期、从 `1.9.4` 升级和最小一致备份/恢复；不能从本批 WSL2/Linux 证据推导这些能力已完成。Kubernetes 不作为本阶段业务验收前提；Milestone 4 到 Phase 17 才最终收口。

### 第五轮与只读运行合同

第五轮 `gopulse-p1401-dbd9415fd3fb` / `/tmp/gopulse-phase15-closure-5.log` 已通过升级/历史保留、全部浏览器和三源故障/恢复/closed、审计 cursor 遍历与六类插件操作/脱敏；未整体通过：运行镜像门禁发现 Backend 的 `ReadonlyRootfs=false`。直接检查受影响 Compose 三个业务服务，Backend/Business Worker/Search Indexer 均尚未声明只读。

依据本批第 3.7 节的明确合同，在 `deploy/compose.yaml` 为这三个 stateless 服务启用 `read_only: true`。Backend 另配 `/tmp` 80 MiB `noexec,nosuid` tmpfs，容纳既有最大 64 MiB 插件上传的临时文件而不打开持久根文件系统；其余两个业务进程不增加持久写目录。未修改 Go 业务逻辑或引入新的卷。本轮清理/端口前后比较通过。

这项运行环境变更需要完整 Compose 功能回归，不能沿用修改前可写根文件系统的 runtime 结论。为避免在长窗口之后才发现镜像/信号问题，第六轮将九个镜像、SIGTERM 门禁提前至升级恢复之后，再执行原固定业务/三源闭环；验收范围不扩大。

第六轮 `gopulse-p1401-8768697ea4c5` 已通过新只读配置下的 clean install、升级和八镜像运行合同，但在 SIGTERM 后 `docker compose start` 返回非零；原 command helper 仅保留通用错误，不能把未捕获的 stderr 写成确定根因。检查发现升级 fixture 只用 `run --rm migrate` 升级 schema，却仍保留旧 migration service 容器。修正固定接线为重建正式 current migrate service，信号后的单服务恢复使用 `up -d --no-deps`，避免重启旧 initializer/无关依赖。Compose 失败现在保存随机值脱敏后的 stderr 便于定位真实失败，不再丢弃诊断。该轮强归属资源、端口清理通过。

另外将 evaluator 关闭期间的 edge `/ready` 200 明确加入固定门禁；不能以 `/health` 或普通 GET 替代 readiness。为第六轮准备的只读补充探测未到达 evaluator 阶段，已终止，**不算通过证据**；第七轮由主入口直接执行该断言。

第七轮 `gopulse-p1401-8c1791d422a9` 已通过八服务 SIGTERM/恢复及第九独立 Exporter 镜像/十类指标/SIGTERM。随后宿主 API 仍使用 Frontend 停启前的动态端口而连接拒绝；修正为从 Compose 重新读取 edge 端口，仅更新三个现有 Client 的 base，保留其 Cookie jar，并记录恢复后的 origin。

本轮 finally 的后置断言还发现已退出的 exporter profile 容器未被未启用该 profile 的 `down` 删除；不是“清理成功”。读取并核对其完整 project/service labels、exited 状态及初始 snapshot 非归属排除后，显式移除唯一残留容器 `5156c7870b35` 和该轮独立 Exporter 的唯一测试 image tag，再删除本轮剩余临时凭据；最终 `snapshot-after.json` 和端口比较均通过。今后固定入口的所有 down 都显式启用 exporter/acceptance profiles，退出后的全项目断言继续保留，不以“没有运行中的容器”冒充无残留。

## 第八轮最终验收（完成）

- 命令：`bash scripts/verify-compose.sh --phase15 > /tmp/gopulse-phase15-closure-8.log 2>&1`，**退出 0**。2026-09-12 23:07–23:22（Asia/Shanghai）执行，项目 `gopulse-p1401-935759064e7d`，证据 `.run/gopulse-p1401-935759064e7d/`。
- clean-install origin `http://127.0.0.1:44660`，升级 fixture origin `http://127.0.0.1:44661`，SIGTERM 恢复后 origin `http://127.0.0.1:44662`；均仅 edge 动态 loopback 端口。
- clean-install 两个 browser 各 1 通过；角色/双应用 4、大屏 2、Phase 13 6、三源 UI 创建 1、VM 局部故障 1、ES 局部故障 1、evaluator 关闭 1、审计脱敏 1：共 **19 个浏览器用例通过**。
- 升级保留两旧 admin、普通用户、原 Cookie/password hash、业务帖子/搜索、六插件版本/安装时间/desired state；migration 重跑和 bootstrap 保护通过。`preserved-observability.json` 证明 `2026-09-12T15:09:13.992288577Z` 之前的 **22 条 Logs、5 条 Events、1 个原始 Redis 指标点** 保留。
- 三源规则 revision 均为 1：Metrics `gopulse_redis_connected_clients` 无附加 labels、阈值 >10；Logs `service=backend,module=auth,message=user registered` 计数 >0；Events `event_name=exporter_plugin_started,operation=start,severity=info` 计数 >0。三源均为窗口 `5m`、持续 `0s`、operator `gt`；Metrics reducer `last`，Logs/Events reducer `count`。输入见本批 browser fixture，状态及 incident 见 `alert-evidence.json`。
- 三轮持续及 Backend 重启后，Events/Logs/Metrics 分别仍为 incident **4/5/6**，evaluation_count **5/5/4**；真实恢复仍是同一行，计数 **16/16/9**，每源只有 **1 trigger + 1 recover** 审计。更新/禁用/删除另验证三个不同 closed 原因，未冒充 recovered。
- VM/ES source unknown/stale 不伪恢复，其他源及社交/MySQL 可用；evaluator 关闭冻结状态并验证 edge `/ready` 200，恢复正常评估；真实 Exporter 非预期退出/失败态/恢复通过。
- 8 个常驻产品镜像及独立第 9 个 Redis Exporter 的目标版本/修订标签、数字非 root UID、只读根、内部网络与唯一 edge 端口、独立 bundle、无 source map/toolchain、PID1 有界 SIGTERM 通过。独立 Exporter 真实 10 类指标通过。镜像目标版本为 `1.12.6`，构建 revision 为当时 HEAD `fd9d2b7`，包含本批工作树输入，**不是最终提交 SHA**。
- requested/completed/unknown 插件审计、六类插件操作、角色/规则/告警审计与稳定 cursor 完整遍历 **82 行**；API/日志/事件/审计/DOM/bundle 脱敏通过。
- 最终打印 runtime gates 与 strong ownership cleanup 两个 PASS；`snapshot-before.json` 和 `snapshot-after.json` 完全一致：既存 **35 containers / 10 networks / 220 volumes** 均保留；`ports-before.txt` 与 `ports-after.txt` 相同。本次 profile 容器、网络、卷、唯一镜像 tag 和临时凭据已清理。

### 直接变更文件

- 固定验收：`scripts/verify-compose.sh`、`scripts/ci/verify_phase15_closure.py`、`frontend/e2e/phase15-closure.spec.ts`、`frontend/e2e/admin-frontend.spec.ts`。
- 升级与运行阻断修复：`deploy/compose.yaml`、`deploy/docker/observability.Dockerfile`、`deploy/plugins/README.md`、`deploy/plugins/phase14-1.11.5-source.tar.gz`、`deploy/plugins/phase14-1.11.5.sha256`。
- setup 合同：`backend/internal/auth/service.go`、`service_test.go`、`backend/internal/user/model.go`；两个 Frontend 的 `src/types/api.ts`、`src/services/api.ts`、`src/services/setup.test.ts`；用户端 `src/components/UserAppShell.vue`。
- 元数据/收口：`VERSION`、`.env.example`、两个 Frontend 的 `package.json` / `package-lock.json`、本批 split plan、总方案与本记录。仅同步 root package version，不升级依赖。

### 最终验证与停止边界

直接 Backend package tests、两个 setup tests 与两个 Frontend 正式构建 tests/typecheck/build 已通过（见前文与构建记录），不因上下文恢复重跑。Frontend 正式构建分别 **17 files / 63 tests**、**7 files / 24 tests**；未变输入的构建缓存继续有效。

最终 Bash/Python 语法、自检及版本/分支/Git 治理结果在下方记录；无独立 Review、依赖审计或额外覆盖率活动。本批本地完成后提交并推送，不自动合并 main；Phase 15 正式合入与 Phase 16 的独立责任保留。

- `bash -n scripts/verify-compose.sh`：通过。
- `python3 -W error -m py_compile scripts/ci/verify_phase15_closure.py`：通过。
- 四个固定 `--self-test`（compose / role-management / alerts / admin-frontend）：最终接线修改后通过，均未访问 Docker。
- `python3 scripts/ci/validate_versions.py`：通过，受管版本与根 `VERSION` 一致。
- `python3 scripts/ci/validate_branch.py --branch develop/1.12.6 --base-ref upstream/main`：通过。
- `git diff --check`：通过。
- `git diff --cached --check`：通过；仅暂存本批 28 个文件，未跟踪 `~` 保持原状。
