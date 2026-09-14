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
