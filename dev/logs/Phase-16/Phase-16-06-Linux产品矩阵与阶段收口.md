# Phase-16-06：Linux 产品矩阵与阶段收口开发记录

## 状态

**未完成，最终验收被宿主时钟反复回拨阻断，尚未推送。** 已产出 `1.13.6` 冻结候选；根与受管版本在本次暂停时恢复为最后已完成的 `1.13.5`，不把候选构建当作完成版本。完整失败诊断及继续条件见末尾。

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
