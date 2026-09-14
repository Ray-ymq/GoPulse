# Phase-16-06：Linux 产品矩阵与阶段收口开发记录

## 状态

**本批已完成真实验收，产品完成版本 `1.13.6`。** 最终同候选证据见 `Phase-16-06-evidence/` 与末尾“最终完成验收”。前文保留实际失败/暂停历史，不替代最终结果。分支提交/推送不等于 Phase 16 全部批次已合入主线。

## 执行基础与边界

- 从包含 Phase-16-05 的 `origin/main`（`0ec74c93e88b8ea9747dbd4f3cc5a3ddf61557e6`）创建并继续 `develop/1.13.6`；继承 `1.13.5` 产品实现与当前数据恢复合同。
- 用户确认继承前批成果继续构建；对齐总/拆分计划：允许冻结前生产本批候选，禁止冻结后的源码构建替代及 digest 漂移。
- 分支已在前置核对中创建，因此复用启动脚本的版本同步及检查步骤，不重建既有分支。
- 已核对真实 Docker server Linux x86_64、4 CPU、约 12 GB 内存、Compose v5.5.0、磁盘约 77 GB 可用。尚不代表产品矩阵通过。
- 不读取或修改用户未跟踪文件 `~`，不改动用户现有 project、registry 或无关卷。
- 当前执行未派生子代理，不读取第三方依赖源码。新增检查仅覆盖本批证据与候选绑定合同。

## 验证

待运行本批固定候选门禁；历史 `1.13.5` evidence 不作为本批通过证据。

## 候选构建前实现与最小检查

- 新增 acceptance profile 容器编排、原子 evidence/聚合器和 Linux 矩阵文档；复用原 lifecycle/recovery/browser 实现。
- 冻结模式下完整 Compose 门禁复用同 revision 的不可变 acceptance image，禁止临时重建。验收镜像预装 Docker/Compose、Python 与独立 backup-fixture，产品生命周期不依赖宿主语言工具。
- 新增真实三源告警事实检查、当前候选错误口令/tamper 场景和受管 MySQL 启动失败注入；不修改产品实现。
- 已通过：`python3 scripts/ci/validate_versions.py`；`python3 -m unittest discover -s scripts/ci -p test_phase16_evidence.py`（1 个合同测试含必需拒绝分支）；`python3 -m unittest discover -s scripts/ci -p test_verify_current_recovery.py`（1 个内容/脱敏合同测试）；受影响 Python `py_compile`；`bash -n scripts/verify-compose-observability.sh`；`git diff --check`。这些仅为开发检查，不替代真实候选验收。

## 首个冻结候选与执行窗口

- 候选源码提交 `4d0d510b461386cf91139b01775d392324f2dfdc`；产品版本 `1.13.6`，继承主线 `1.13.5`。
- 构建目录 `dist/phase16-06-v1/`，独立交付目录 `.run/phase16-06/delivery v1/`（真实空格路径），干净验收工作树 `.run/phase16-06/source-v1/`。
- 新建本任务 loopback registry `gopulse-p1606-registry` / `127.0.0.1:15003` 及具名数据卷；候选实际 push/pull 成功，不声明公网发布。未复用或删除前批 registry。
- Manifest SHA-256：`dbb867acefa7fb2fe7fde93afaddc73a33323c4acf6e52fa32e0e0705da41109`。
- Bundle archive SHA-256：`8506026c0aaad0f230b2d690c8c94e06f95f0686bf2ebcbd446698e7258f0cad`。
- Acceptance registry ref：`127.0.0.1:15003/gopulse/acceptance@sha256:820b0068cc194461a9a951715a44b5bdef98479e0d4b649fb3e8130a7dd992f5`；本机 image ID 与该 digest 相同，实际镜像 label revision 与产品一致。
- `release_artifacts.py build --registry 127.0.0.1:15003/gopulse --output dist/phase16-06-v1 --platform linux/amd64` 通过；acceptance 从同一 `git archive HEAD` 构建/push 通过。复用前批私有 wrapper 传递既有网络代理 build args，不修改 Docker 全局配置。
- 验收镜像实际运行检查通过：Docker CLI 29.5.2、Compose v2.40.3、矩阵 CLI 与预编译 backup-fixture。宿主 Compose v5.5.0，Ubuntu 24.04.4 LTS，WSL2 kernel `6.6.87.2-microsoft-standard-WSL2`，Intel Core i7-14650HX，Docker 分配 4 CPU/约 12 GB 内存。
- 运行日志位于 `.run/phase16-06/`；2026-09-14 20:45–20:49（Asia/Shanghai）完成构建/独立解压并启动固定 artifact runtime/full Compose 门禁。实际最终结果待补充。

## 首轮固定门禁通过与 runner 预检修复

- 同候选 `verify-release-artifacts.sh --manifest <独立 delivery v1>/release-manifest.json --platform linux/amd64 --runtime` 退出 0；日志 `artifact-runtime-v1.log`。包含镜像/plugin 元数据、真实 lifecycle runtime、完整 Compose 门禁及清理，尾部六插件采集/单插件停止用例实际 54.1 秒通过。
- 随后聚合 runner 在首个资源快照处失败，尚未执行安装/恢复：Docker server 对 `docker ps --filter label!=…` 返回 `invalid filter 'label!'`。在宿主及验收镜像分别复现同一命令错误，未改产品源码或第三方依赖。
- 最小修复为受支持的正向 `label=io.gopulse.phase16.runner=true` 查询，并要求恰好一个本批 runner 后做本地集合差；无过滤器/daemon 兼容性猜测。新增一个直接复现/保护该错误的测试。
- 因 acceptance 源码与候选 revision 必须一致，提交修复后生成 v2 候选并重新执行候选级固定门禁。v1 的通过只保留追溯，不混入 v2 完成 evidence。这是重跑原因，不是无依据重复历史检查。

## v2 运行结果与最终隔离摘要修复

- v2 product/runner revision `5b54a5f8294b`，候选 `dist/phase16-06-v2/`，独立 `delivery v2/`；Manifest SHA-256 `fda72782129825c86ca1538d6487f0e87e3380f36772df80d1ee96b28e38d6f9`。Acceptance registry digest `sha256:279497a6ae44030b5d751eafb12774b3efa51f9de51683419d4de5cbc35173a1`。
- 已真实通过 v2 artifact runtime/full Compose（六插件尾部 51.2 秒）、clean install/lifecycle、Linux failure matrix（含受管 MySQL pause 启动失败返回 18、不发布 ready）、当前 A/B/C 两次恢复/内容比较/继续采集写入/两套前端 desktop+narrow/非 UTC 浏览器、三源真实 incident、错误口令/tamper、导入失败/restore 中断及重试、受管资源集合清理。
- 初次源 init 的 Elasticsearch index 查询和稍后 restore 的 Elasticsearch digest pull 各发生一次临时 registry 失败（退出 10）。保持候选不变，实际按同 digest 再次 pull 成功后仅续跑未通过步骤，未重跑已成功场景。日志 `matrix-v2.log`、`matrix-v2-resume1.log`、`matrix-v2-resume2.log`、`elasticsearch-pull-retry.log` 保留各轮结果。
- 最后 `secret-isolation` 拒绝无关 Docker 资源的配置摘要。直接连续调用同一快照函数五次得不同 hash；比较两份实际 Docker inspect 仅报告 `Mounts` 数组顺序不同、成员相同，未输出配置值/凭据。原实现保存摘要而未保存原始私有对象，不能据此把最终门禁记为通过。
- 最小修复：按完整 Mount JSON 对容器挂载集合规范化排序；仍比较 ID、Image、Config、挂载内容、StartedAt、RestartCount 与网络/卷合同。新增一个直接保护“换序相同、内容改变拒绝”的测试。保留前后原始快照到 0600 私有文件用于后续实际诊断，不提交含用户配置的快照。
- 修复验收工具需要新的同 revision 候选 v3，因此重新执行候选级固定门禁；v2 通过结果只作追溯，最终 evidence 不引用其通过结果。

## v3 固定 Compose 浏览器失败诊断

- v3 同源候选 `a76089fc5097ebf4d218644e66da034472c1ebba`，验收镜像 `sha256:f8b533bba7981145300cf536918e6fc6cc75e44ca2fdba909aee0caee417d503`；修复后在宿主及实际验收镜像分别连续五次快照稳定。
- v3 artifact metadata 与 lifecycle runtime 已通过并进入完整 Compose。第一轮在管理浏览器创建 `closure-metrics` 后 5 秒内未出现列表行而失败；第二轮在 `transport-down` 场景创建社交帖子后，“取消点赞”按钮 5 秒内未出现而失败。两轮均未生成通过回执，日志 `artifact-runtime-v3.log`、`artifact-runtime-v3-retry.log`。不据此断言产品、时钟或网络根因。
- 因第二次出现不同 UI 断言失败，停止盲目无诊断重试；改用原 `verify-compose.sh --keep` 保留同候选受管项目，私有 Docker wrapper 只为 acceptance 容器挂载 Playwright test-results 输出目录。未改测试、断言、超时、产品镜像或候选源码，不输出私有 trace 中凭据。日志 `compose-v3-diagnostic.log`。通过后须复用原归属清理/快照断言，再与已通过的同候选 metadata/runtime 合并记录固定门禁。

## 真实宿主时钟阻断（2026-09-14，未完成）

### 实际定位

- `--keep` 诊断轮在 `redis-fallback` 登录后访问新建帖子页面时失败；private trace 被实际保存在 `.run/phase16-06/browser-diagnostics/`，未发布其中登录密码、Cookie 或完整请求。
- 只提取请求时间/状态及 JWT 时间声明：登录 `13:57:44.742Z` 返回 200、`iat=1789394264`（13:57:44 UTC），之后先出现成功请求，后续 `/api/v1/users/me` 的时间变为 `13:57:43.836Z` 并返回 401，浏览器回到登录页。该次失败不是元素定位器变更或缺少产品功能。
- 执行 120 秒、50ms 间隔的 `time.time() - time.monotonic()` 观测，实际多次发生约 0.9–2 秒负跳变；公开脱敏诊断见同目录 `Phase-16-06-clock-observation.json`。这只是失败原因证据，不是最终产品通过证据。
- `timedatectl status/timesync-status` 显示 NTP active、synchronized=yes，但当时 jitter 751.967ms；`current_clocksource` 为 `tsc`。尝试读取 timesyncd journal 时当前用户缺少完整系统日志权限，未据空日志推断时钟正常。
- 没有修改宿主 clocksource/NTP、Windows/WSL 配置，没有放宽 JWT 鉴权、测试超时、断言或产品安全合同。修正共享验收宿主时钟需先取得用户确认，不能以临时回拨或 mock 时间制造通过。

### 已清理与保留

- 从原 runner 复用 `assert_project_ownership`、同项目 `compose --profile exporter down --volumes --remove-orphans`、`cleanup_acceptance_images`、`assert_snapshot_preserved`，实际退出 0；日志 `compose-diagnostic-cleanup.log`。清理只针对诊断项目 `gopulse-accept-130e6d83ef4c`，未清理其他项目或全局 prune。
- v1/v2/v3 候选及本任务 loopback registry、私有加密恢复备份、私有日志/trace 保留供复核。v2 的恢复和清理通过不能替代最终 v3 验收。
- 不发布 `linux-amd64.json` 的最终完成标志，不声称 Phase 16 或 Milestone 4 完成。按“完成后推送”的请求，本批尚不推送未验收分支。
- 根与受管版本恢复到最后完成版本 `1.13.5`；已冻结 v3 的源工作树/镜像仍为 `1.13.6` / `a76089fc5097ebf4d218644e66da034472c1ebba`。后续可直接消费既有候选，无需因为文档/诊断记录提交而重建。

### 继续执行入口

1. 用户确认并稳定真实 WSL2/Linux Docker 宿主时间；不得放宽产品鉴权。
2. 复用 v3 已通过且输入未变的制品 metadata/lifecycle runtime 检查，完成 v3 完整 Compose 及原归属清理固定门禁；现有环境改变若影响某检查，记录原因后只重跑相关范围。
3. 运行 `.run/phase16-06/run-matrix-v3.sh` 对同候选执行独立 Bundle lifecycle/Linux failure/A-B-C/current-product/failure/cleanup/Secret 聚合。不得复制 v2 的通过进度。
4. 聚合验收通过后将根与受管版本同步为 `1.13.6`，更新本记录、提交并推送 `develop/1.13.6`；总阶段主线合入条件另行如实报告。

### 本次暂停前检查

受影响证据合同 2 测试、恢复内容/正向 Docker 过滤器 2 测试及两侧实际五次快照稳定检查已通过。未重复未受后续修改影响的通过检查。暂停前执行版本一致性检查、JSON 解析/显式 Secret 扫描和 `git diff --check`；最终产品/阶段验收仍失败，不把开发检查替代产品门禁。

### 本任务相对主线的实现/文档文件清单

- `.dockerignore`
- `deploy/docker/acceptance.Dockerfile`
- `deploy/phase16-acceptance.yaml`
- `dev/imple/Phase-16/Phase-16-06-Linux产品矩阵与阶段收口.md`
- `dev/imple/Phase-16/Phase-16-总实施方案.md`
- `dev/logs/Phase-16/Phase-16-06-Linux产品矩阵与阶段收口.md`
- `dev/logs/Phase-16/Phase-16-06-clock-observation.json`
- `dev/phase16-linux-matrix.md`
- `scripts/ci/acceptance-entrypoint.sh`
- `scripts/ci/frontend_bundle_browser.py`
- `scripts/ci/phase16_acceptance.py`
- `scripts/ci/phase16_evidence.py`
- `scripts/ci/test_phase16_evidence.py`
- `scripts/ci/test_verify_current_recovery.py`
- `scripts/ci/verify_backup_restore.py`
- `scripts/ci/verify_current_recovery.py`
- `scripts/ci/verify_product_lifecycle.py`
- `scripts/verify-compose-observability.sh`
- `scripts/verify-phase16-evidence.py`

版本元数据曾用于候选构建，当前已恢复为主线完成版本，不在最终相对主线变更清单中。

## 用户授权后恢复执行：时钟同步冲突排除（2026-09-14）

- 用户明确同意临时调整 WSL2 宿主时钟同步/clocksource 后继续最终验收与推送。继续同一任务分支，不创建新分支、不重建冻结 v3。
- 初始记录在 `.run/phase16-06/clock/before.txt`；原 clocksource `tsc`，GoPulse 发行版 `systemd-timesyncd` active。先临时切换为已存在的 `hyperv_clocksource_tsc_page`，180 秒观测仍有回拨；再暂停本发行版 timesyncd，仍出现回拨。这两步没有被记作成功修复。
- 在独立 tracefs instance `gopulse-clock` 中只启用四个时钟调整 syscall 事件，观测 45 秒，实际发现 `chronyd` 和另一个 `systemd-timesyncd` 正在调用 `clock_adjtime`；两者不在当前发行版 PID 列表中。采样后已禁用事件并移除该 instance，未改全局 tracer。
- 只读检查正在运行的 `Ray-Work` 与 WSL 系统环境，发现 Ray-Work 的 timesyncd active，WSL 系统 chrony 使用 PHC0 且曾报告约 64052 ppm 的异常频率修正。临时停止 Ray-Work 的 timesyncd 后保留 WSL 系统 PHC chrony 单一来源。未停止其他发行版、Docker、业务容器或 chrony；试图向只读检查中已消失的 timesyncd PID 175 发送 STOP 返回 `No such process`，没有实际暂停该进程。
- 随后 `2026-09-14T15:01:03.839200Z` 至 `15:04:03.872275Z` 连续 180 秒/50ms 采样，超过 100ms 的墙钟相对单调钟跳变为 **0**；chrony 报告偏差收敛。该结果支持继续验收，不将 clocksource 切换单独宣称为根因修复。
- 稳定窗口启动同 v3 `verify-release-artifacts --runtime` 及矩阵串联。虽然候选未变，时钟执行环境发生相关改变，重新运行受影响的最终固定门禁有明确依据。继续保留私有浏览器 trace；不改 JWT、UI 断言、超时或候选 digest。
- 临时配置均未持久化。原始状态和结束时恢复结果另行记录；共享 WSL 时钟不可同时由多个发行版抢占调整的现象需作为宿主维护事项告知用户。

## 最终完成验收：1.13.6 / v3（2026-09-14）

### 冻结候选与交付输入

- 唯一最终 revision：`a76089fc5097ebf4d218644e66da034472c1ebba`，未在最终验收期间重建或替换任何产品/plugin/runner。后续提交只记录文档、完成版本和 evidence；候选源码 worktree 仍可复核。
- Manifest SHA-256：`09b59b4e818dc8116428563bc099d98e4a0cdb7fa1502d2405a4699d60735687`。
- Bundle archive SHA-256：`2016df48b4097509da3dc9fa9386a68811370bfdee0207f396fd70fa46c2c128`。
- 镜像/plugin 全部 index、Linux amd64 platform、archive、entrypoint/schema digest 随 `Phase-16-06-evidence/release-manifest.json` 提交；包含 9 产品镜像、lifecycle、6 third-party、6 current 插件（历史插件条目仅保留，不参与跨版本验收）。
- 验收镜像：`127.0.0.1:15003/gopulse/acceptance@sha256:f8b533bba7981145300cf536918e6fc6cc75e44ca2fdba909aee0caee417d503`，image ID/revision 由 runner 与产品运行 label 实际比对。
- Bundle 保留于 `dist/phase16-06-v3/gopulse-1.13.6-bundle.tar.gz`；独立交付目录 `.run/phase16-06/delivery v3/`，包含空格路径，清洁源码 worktree `.run/phase16-06/source-v3/`。
- 运行前分配随机 project 与 token，四个恢复 project 的身份 hash 随最终 JSON 交付；明文 state/secrets、备份口令、业务行对照和原始 Docker 配置只留在 0700 私有运行目录，不提交。

### 实际最终命令与结果

| 命令/固定范围 | 实际结果 |
| --- | --- |
| `GOPULSE_ACCEPTANCE_IMAGE=<v3 image ID> .run/phase16-06/source-v3/scripts/verify-release-artifacts.sh --manifest '<delivery v3>/release-manifest.json' --platform linux/amd64 --runtime` | 退出 0；`artifact-runtime-v3-clock-stable.log`，完整 metadata、plugin catalog、lifecycle runtime 和默认完整 Compose/清理均通过；六插件尾部真实用例 42.4 秒 |
| `.run/phase16-06/run-matrix-v3.sh`（实际为计划对齐的 acceptance profile/container `phase16` 命令） | 退出 0；`matrix-v3-clock-stable.log`；所有七场景通过，最终 JSON 原子完成 |
| 上述 runner 内 `verify_product_lifecycle.py --platform linux/amd64 --manifest … --clean-install --evidence …` | 通过；真实 doctor/init/up/verify/status/logs/down/up、只读快照、空格路径、私有权限、清理 |
| 上述 runner 内 `verify_product_lifecycle.py … --failure-matrix` | 通过；并发锁、daemon、manifest tamper、1MiB tmpfs 磁盘不足、端口、权限、外来卷、SIGINT/SIGTERM、真实 MySQL pause 启动失败/非 ready 与清理 |
| 上述 runner 复用 `CurrentRecovery.current_product/current_failures/cleanup`（等价三个 `--current-product` 子命令） | 通过；A/B/C、继续写入/六插件、双前端、三源 incident、非空目标拒绝、错误口令/tamper、导入失败/中断重试、归属清理 |
| `python3 scripts/verify-phase16-evidence.py --linux .run/phase16-06/matrix-v3/evidence/linux-amd64.json` | 退出 0；同 manifest/revision/Bundle、host/server、场景/附件 hash、project 隔离、Secret 标记均通过 |
| `python3 scripts/ci/release_artifacts.py promote --manifest '<delivery v3>/release-manifest.json'` | 退出 0；10 个产品/lifecycle 镜像在隔离本机 registry 晋升为 `1.13.6`，未重建，index digest 和 Bundle checksum 不变；`promote-v3.log` |

聚合 runner 实际窗口 `2026-09-14T15:19:27.570368Z` 至 `15:41:12.520106Z`（Asia/Shanghai 23:19:27–23:41:12）。完整门禁及矩阵期间的持续时钟观测窗口 `15:04:43.004031Z` 至 `15:41:27.653442Z`，超过 100ms 的 wall-minus-monotonic 跳变 **0**。

### 已证明的事实与证据位置

- 所有七个场景均为最终 v3 同 manifest 通过；`linux-amd64.json` 引用的 JSON/日志附件整组提交，副本与原始公开证据逐字节一致。v1/v2 仅保留失败/历史，不被最终聚合器消费。
- 桌面 1440×900、窄屏 390×844、键盘/焦点、语义入口、错误/恢复状态、登录/session/角色/登出、安全响应头与 Asia/Shanghai 非 UTC 场景来自实际 browser。A/B/C 三个项目各自通过 desktop/narrow/three-source-create；不是浏览器 mock 替代产品闭环。错误状态用浏览器请求故障注入验证 UI。
- 六插件的真实成功时间/transported metrics/继续采集、业务搜索继续写入由 CurrentRecovery 正式 API 断言；源三类告警各有真实 rule/incident，操作审计实际存在。完整 Compose 另覆盖业务通知/Worker 恢复，不声明新增外部告警通知渠道。
- 两份 format v1 加密备份分别来自 A 和恢复后继续写入的 B，独立 backup-inspect/fixture 审计均通过；B/C 都验证原有及新增内容，不只是行数一致。非空目标拒绝不改稳定事实；导入失败与 SIGTERM 中断均正式重试成功、无半完成 ready。
- 实际全局资源集合以及无关容器 Image/Config/Mounts/StartedAt/RestartCount、网络/卷合同前后相同；用户 sentinel 未变；私有快照仅本地保存。Secret 扫描通过，未操作用户文件 `~`，未执行全局 prune。

### Phase 17 交接与边界

- 交接为 Linux amd64 完整 Compose 产品 `1.13.6`、冻结 Bundle/manifest、共享生命周期、唯一 edge/双前端、六插件/三源告警、backup format v1、当前数据配方与同 manifest 的两次恢复证据。公开入口文档为 `dev/phase16-linux-matrix.md`，恢复配方为 `dev/phase16-current-recovery.md`。
- 备份保留在 `.run/phase16-06/matrix-v3/recovery/source/product.gpb` 和 `target/product.gpb`，口令独立私有保存；SHA-256 见公开 recovery JSON。不提交备份/口令/原始 trace。
- 实际发布仅为 `127.0.0.1:15003` 隔离 registry 的同 digest 晋升；不宣称公网发布、跨主机可直接拉取、macOS/Windows/arm64、Kubernetes、历史或跨版本升级。
- Phase-16-01 至 06 同名记录均存在。总方案 §15.1–15.4 技术验收已由本候选固定矩阵证明；§15.5 的“所有批次合入主线”仍须本开发分支实际合并后才能成立，本次提交/推送不提前宣称该行政条件或 Milestone 4 完成。
- 按授权“临时调整”范围，验收结束后已将 clocksource 恢复为 `tsc`，GoPulse 与 Ray-Work 的 timesyncd 均恢复 active；没有持久化主机配置、停止 Docker 或修改业务服务。原始多源同步冲突可能再次导致宿主回拨，长期治理应统一 WSL 共享时钟的同步来源，不能通过放宽产品 JWT 解决。该宿主维护项不伪装成产品修复。
- 完成版本及两套 Frontend 元数据最终同步为 `1.13.6`；后续文档/证据提交不更换已验证的候选 revision。

### 最终 diff 检查与提交

- `python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/1.13.6 --base-ref origin/main`、`git diff --check` 均通过。
- 已验证公开证据的提交副本与原 evidence 逐字节相同；JSON 解析及使用本候选四个安装的真实 Secret/token/登录口令进行扫描均通过。不重新运行已成功且输入未变的产品门禁。
- 根与受管版本更新为 `1.13.6`，新增整组 `Phase-16-06-evidence/` 及更新宿主诊断、Linux 产品矩阵、当前数据恢复文档。本次未修改冻结候选实现或受管 Bundle 内容。
- 提交并推送当前批次 `develop/1.13.6`；最终远端提交 hash 以实际 push/ls-remote 结果为准。不包含用户未跟踪文件 `~`。
