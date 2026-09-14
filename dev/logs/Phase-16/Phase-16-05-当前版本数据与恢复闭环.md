# Phase-16-05：当前版本数据与恢复闭环开发记录

> 最终状态：已完成，`1.13.5`。最终候选、固定门禁、清理结果与限制见末尾“最终完成验收”；前文保留实际执行与失败历史。

## 范围与状态（2026-09-14）

本记录对应同名当前版本方案；原《1.9.4 升级与恢复闭环》记录保留为历史尝试，不作为当前恢复验收结果。仅验收当前候选、相同 manifest 的数据恢复，不声明跨版本升级支持。

初始状态：实施中，尚未通过固定门禁。版本元数据先同步为 `1.13.5` 以构建不可变候选，不代表本批完成。

## 已执行

- `git fetch origin`；检查已推送 `develop/1.13.5` 仅有历史记录增量，通过普通 merge 同步最新 `origin/main`，未重置、改名或强推。
- Docker 服务端为 Linux x86_64；未触碰用户未跟踪文件 `~` 或现有业务 project。
- 增加 `scripts/ci/verify_current_recovery.py`，从正式入口生成数据，复用 backup/inspect/restore，串联 A/B/C、继续写入/六插件采集/告警/双前端，以及两个代表故障。
- 扩展 `scripts/ci/verify_backup_restore.py` 的 `--current-product` 组合参数；旧低层矩阵保持原入口。
- backup-fixture 的只读独立检查结果增加公开域 payload checksum，不输出明文业务数据或凭据。
- 创建本任务独立 registry `gopulse-p1605-registry`，仅发布到 `127.0.0.1:15002`；不复用用户现有 registry 数据卷。

## 验证记录

待记录构建和固定门禁的真实结果。私有运行输出保存在 `.run/phase16-05-recovery/`，不提交原始快照和凭据。

候选构建前最小检查已通过：`python3 -m py_compile scripts/ci/verify_current_recovery.py scripts/ci/verify_backup_restore.py`、`(cd lifecycle && go test ./cmd/backup-fixture)`（包无测试文件，编译通过）、`python3 scripts/ci/validate_versions.py`、`git diff --check`。

## 当前候选与首轮故障

- 候选构建及同版本 acceptance 镜像构建通过；product revision `ce3d11f3bbf247332e91623c5fbb2a5bc43dab46`，manifest SHA-256 `c19a4216201f543a6263670c1b8cb0832eea4b0e9da67c8588cbbf618d715475`；输出 `dist/phase16-05-current`，registry `127.0.0.1:15002/gopulse`。
- 使用私有 Docker wrapper 传递已有代理 build args 和 `--network host`，未修改宿主 Docker 配置或产品 Dockerfile。
- 首轮源 API 数据生成、六插件继续采集、新规则告警、新写入搜索及桌面 browser 通过，窄屏 browser 失败。后台请求日志在 `2026-09-14T06:08:48.717116047Z` 后出现 `06:08:46.954769535Z`，对应新签发 JWT 的 issued-at 在当时系统时间未来，引发 401。`timedatectl` 显示 NTP active；不修改用户时钟配置、不放宽产品认证合同。
- 扩展既有 browser helper 的可选进度回调，保存各 viewport 成功状态。依据首轮明确到达 narrow browser 的 traceback，重建 `source-api-continuity` 和 `source/desktop` 成功进度，后续只续跑未完成的 narrow/three-source 检查；此后同类恢复自动记录，不凭空标记未执行检查。
- 为内容比较新增一个针对本批合同的最低层测试：允许新增行，原始行内容被篡改时拒绝；同时确认公开 evidence 不含私有登录口令，文件权限 0600。`python3 -m unittest discover -s scripts/ci -p test_verify_current_recovery.py` 通过。

## 第一候选恢复通过与清理缺陷

A/B/C 与两个代表故障均已通过（日志 `current-product-resume.log`、`failures.log`），但随后全局资源集合对照发现新增 15 个匿名卷，清理门禁失败，未执行串联的 full Compose 门禁，不能宣告本批完成。

实际检查确认锁定的 Kafka 镜像声明 `/etc/kafka/secrets`、`/mnt/shared/config`、`/var/lib/kafka/data` 三个 VOLUME，Compose 仅为 broker 数据目录提供命名卷，kafka-init 没有显式挂载。这些镜像隐式匿名卷在普通 `down` 删除容器后失去引用，后续 `down --purge` 无法清除。原有所有容器、网络、卷 ID 均保留。

必要修复及扩大验证依据：在共享 Compose 配置中显式使用 tmpfs 覆盖 broker 的两个临时路径和 init 的三个临时路径，保留 broker 的命名数据卷；共享基础设施及候选 manifest 改变，因此重新构建候选，并在新候选重新运行 A/B/C、故障及固定 full Compose 门禁，不复用不同 manifest 的旧备份冒充通过。只检查直接受影响 Compose 合同，不扩大为依赖审计。

旧匿名卷已无可靠 installation 标签及容器挂载归属，不能凭差集自动删除，作为失败轮次遗留物保留；不声称首轮清理通过，不对用户资源执行 prune。新轮次使用独立私有目录及新的资源基线验证零增量清理。

修复后最小检查通过：Compose `config --format json` 真实渲染并断言 broker 命名数据卷不变、两个 scratch 路径以及 init 三个路径均为 tmpfs；`scripts/verify-compose.sh --self-test`、`python3 -m unittest discover -s scripts/ci -p test_compose_acceptance_env.py`、受影响 Python 文件编译与 `git diff --check` 均通过。

第二候选源码 revision `b624ea3`。源环境安装 Elasticsearch 插件时又观察到时钟倒退：Monitor 返回 `installed_at=2026-09-14T06:29:17.89949793Z`、`started_at=06:29:16.26839688Z`，Backend 按既有时间顺序合同拒绝状态，edge 返回 `monitor_unavailable` 503。未直接写数据库或修改插件状态文件；通过正式 edge 管理 API 对该插件执行 stop/start（均 200），状态读取恢复为 200，保留该真实失败记录后续跑尚未完成的数据生成。此处不修改安全验证、时间顺序合同或系统 NTP。

第二候选 A/B/C、两个代表失败和强归属清理均通过；`cleanup-v2.log` 记录 `owned-cleanup-and-isolation-passed`，新轮次所有容器/网络/卷集合精确恢复到开跑前状态（旧轮次保留的无标签匿名卷未动）。

最后 Compose 门禁第一次调用在 Docker 访问前被用户原有未跟踪文件 `~` 的 clean-source 检查拒绝。保持该文件不读、不移、不改，不放宽门禁；从候选提交 `b624ea303d64790a497b7a322e3fd9df57882764` 创建临时 detached 干净 worktree `.run/phase16-05-recovery/compose-checkout`，用同一 manifest、同一 Linux amd64 Docker daemon 执行固定命令。此为执行目录隔离，不是跳过检查或变更候选。

干净 worktree 首次启动的私有 Docker wrapper 使用 `/usr/bin/env bash`，被 full gate 的受限 PATH 拒绝；改为绝对解释器 `/bin/bash` 后继续执行，未改项目受限工具合同。该轮在实际产品验证前失败，不记通过。

## Full Compose 尾部采集检查超时

干净 worktree 的 full gate 已通过镜像/权限/网络合同、业务写入与依赖恢复、完整 down/up 持久化、独立 Exporter 和管理员 browser，但尾部 `compose-release-plugins.spec.ts` 在等待实际 `up=1` 查询点时触发 60 秒超时，整体退出失败。原门禁自动清理了该项目，未将前面通过结果改写为 full gate 成功。

只对失败范围做独立诊断：同一候选新建隔离 lifecycle 项目，通过正式 API 创建管理员及插件，以与 full gate 相同的最小权限 MySQL/RabbitMQ 用户采集，不授予业务表访问权限。六类插件第一次及第二次观测时已经有 `last_success_at`，但查询尚无样本；第三次起全部真实返回 `up=1`，此后持续正常（私有日志 `diagnose-plugins.log`）。未发现需要修改产品或放宽断言的证据，未增加测试或改动产品依赖。

清理诊断项目后重新执行 full gate：前轮运行环境已由原脚本销毁，该固定脚本没有子步骤续跑接口，因此本次在新隔离环境重新执行完整固定命令；这是必需失败门禁的重试，不重新执行已经通过且候选未变的 A/B/C 与故障恢复门禁。

## 最终完成验收（2026-09-14，真实 Linux amd64）

**状态：本批完成，产品版本 `1.13.5`。** 前文为按发生顺序保留的实施与失败记录，不代表最终候选仍未通过。

### 冻结候选与制品

- 产品源码 revision：`b624ea303d64790a497b7a322e3fd9df57882764`。其后的完成提交仅包含使用说明、开发记录和脱敏证据，不改变已验收产品/脚本。
- Manifest：`dist/phase16-05-current-v2/release-manifest.json`，SHA-256 `a8988797ad7dfd40c2422392c4cef8cbca8167b359693f99376b9edd27a040c5`。
- 本地 registry：`127.0.0.1:15002/gopulse`；版本 `1.13.5`、Linux amd64。不是公网发布声明。
- 当前六插件的 archive/entrypoint/schema digest、产品及第三方镜像 digest、Bundle checksum、两份备份 checksum、切点域摘要、恢复标记、真实命令/结果、browser 镜像 ID 及各日志 checksum，见同目录 `Phase-16-05-current-recovery-evidence.json`。
- 私有运行证据与加密备份：`.run/phase16-05-recovery/accepted-v2/`；源/目标对应 `source`=A、`target`=B、`second`=C。凭据/事实原文不提交。

### A/B/C 的实际事实与持续使用

| 项目 | 已验证事实 |
| --- | --- |
| A | 两个真实账号及超级管理员 bootstrap 关系；2 篇文章（包括真实编辑）、1 条评论；5 条告警规则、2 个初始 incident、26 条审计记录；六 current v2 插件真实采集 |
| Backup A | `2981188d24e0b68eb2964fc33ec255321b14aa95c5dd60489ab6e5ba1d0e6147`；加密 format v1，独立 inspect/公共域 Secret 扫描通过；324 条日志、10 条事件、6975 个历史指标样本，6 个插件 |
| B | 引擎在启动前精确核对 SQL、搜索文档、历史指标摘要及插件配置/意图；API 核对原对象内容、普通用户/管理员登录与权限边界；新的业务写入可按确切 ID 搜索，新采集及新规则 incident 实际产生；共 3 篇文章、4 个 incident、33 条审计 |
| Backup B | `d0a2cb52f0bb20d5f56d6c5b592d9e0c377e25f89957d752e2a3e554aa94b821`；独立 inspect 通过；510 条日志、12 条事件、8231 个指标样本，含 A 的历史与 B 的新增事实 |
| C | 恢复 Backup B，启动前完整域校验及 API 对照通过；保持原有及 B 新增对象，继续生成第 4 篇文章、后续采集/告警/审计事实 |

A/B/C 均通过 desktop、narrow 双前端实际 browser 路由和 three-source 告警创建检查。数量只是辅助摘要；业务/身份/规则/审计代表行按完整内容核对，恢复引擎按一致切点 digest 校验数据域，不以计数相同替代内容保持。告警评估时间与计数、采集时间允许向前推进，会话按原合同失效并重新登录。

B/C 重复恢复至非空项目均返回预期拒绝码 17；原始内容仍在，稳定身份/业务/规则关系逐行不变。真实非法 SQL 导入失败返回 18；真实 restore-infrastructure 阶段 SIGTERM 返回 20。两者均保留 operation id/阶段/恢复指引、没有半完成 ready，清理新目标后从权威备份正式重试成功。源安装状态及权威备份 checksum 未改变。

### 固定门禁与最终结果

| 实际命令 / 范围 | 结果 |
| --- | --- |
| `scripts/verify-backup-restore.sh --manifest dist/phase16-05-current-v2/release-manifest.json --work .run/phase16-05-recovery/accepted-v2 --platform linux/amd64 --current-product`，同版本不可变 acceptance ID 通过 `GOPULSE_ACCEPTANCE_IMAGE` 提供 | 通过；`current-product-v2-resume.log`，包含 A/B/C 两次恢复、双前端及继续使用 |
| 上述命令增加 `--failure-matrix` | 通过；`failures-v2.log`，仅本批两个代表失败/重试及权限脱敏检查 |
| 上述命令增加 `--cleanup` | 通过；`cleanup-v2.log`，容器/网络/卷完整集合与运行前完全相同 |
| 干净候选 worktree 中 `GOPULSE_RELEASE_MANIFEST=<候选绝对路径> scripts/verify-compose.sh --keep` | 退出 0；`compose-v2-retry.log`，含所有默认 full 项目和尾部六插件真实 `up=1`、单插件停止隔离；尾部用例实际 53.4 秒通过 |
| 复用原 full runner 的 `assert_project_ownership`、`compose --profile exporter down --volumes --remove-orphans`、`cleanup_acceptance_images`、`assert_snapshot_preserved` | 通过；`compose-cleanup.log`，已清理诊断保留项目及唯一验收镜像别名，原资源/镜像标签/Git 状态保持不变 |

最后重试为诊断使用 `--keep`，通过后实际执行了原脚本同一组归属清理及快照校验，不省略默认门禁的清理合同。主 checkout 的 `~` 始终未读取、未移动、未修改、未暂存。未重复已成功且未变的同候选 A/B/C 与故障门禁，也未重复 Phase-16-04 的低层完整矩阵。

### 实际改动文件

- `scripts/ci/verify_current_recovery.py`：当前候选数据配方、跨域内容对照、A/B/C、故障/重试、私有 journal/脱敏 evidence 和隔离清理。
- `scripts/ci/verify_backup_restore.py`：复用入口的 `--current-product`、模式组合和 acceptance image 环境变量。
- `scripts/ci/frontend_bundle_browser.py`：可选 viewport 进度回调，失败续跑不重复成功 browser 检查。
- `scripts/ci/test_verify_current_recovery.py`：直接保护本批内容一致、公开证据不含口令及私有权限合同。
- `lifecycle/cmd/backup-fixture/main.go`：独立 inspect 的公开域 payload checksum；未更换备份格式或引擎。
- `deploy/compose.yaml`：保留 Kafka 命名数据卷，对其非持久化路径和 kafka-init 路径显式使用 tmpfs。
- `dev/phase16-current-recovery.md`、本记录及同目录结构化 evidence：运行配方、事实语义、偏差及交接。
- `VERSION`、`.env.example`、两套 Frontend 的 package/package-lock：同步为 `1.13.5`。

### 偏差、限制与下一批

1. 先同步版本以构建候选，最终完成由本节和证据明确标识；第一候选的成功恢复不替代修复后候选验收。
2. 保留历史升级尝试记录；本批没有历史 fixture、跨版本/跨 manifest 升级或 Redis v1 迁移成功声明。
3. 初轮遗留的 15 个匿名卷因无法重建强归属而保留，不冒险删除；新候选已修复根因并实际通过零新增遗留资源验收。清理旧匿名卷需另行确认归属，不阻断已修复候选。
4. 时钟倒退及一次冷指标可见性超时均保留原始失败记录；未放宽认证、状态 DTO 或 `up=1` 断言。运行环境需维持稳定时钟。
5. 不声明 macOS、Windows、arm64、Kubernetes、跨版本升级或公网镜像发布支持；本地 registry 和私有加密备份保留供复核。
6. 交给 Phase-16-06 的是当前数据配方、可续跑 runner 与脱敏 evidence 合同；必须在 `1.13.6` 候选重新生成自己的同 manifest 数据和备份。

完成记录提交前检查：`git diff --check`、结构化 evidence 的 JSON 解析及显式凭据扫描通过。此前已通过的版本一致性、分支治理及受影响编译/单测输入未变，未重复运行。
