# Phase-16-05：当前版本数据与恢复闭环开发记录

## 范围与状态（2026-09-14）

本记录对应同名当前版本方案；原《1.9.4 升级与恢复闭环》记录保留为历史尝试，不作为当前恢复验收结果。仅验收当前候选、相同 manifest 的数据恢复，不声明跨版本升级支持。

状态：实施中，尚未通过固定门禁。版本元数据先同步为 `1.13.5` 以构建不可变候选，不代表本批完成。

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
