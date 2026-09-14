# Phase-16-06：Linux 产品矩阵与阶段收口开发记录

## 状态

实施中，尚未完成最终验收。根与受管版本预先同步到候选构建版本 `1.13.6`，不作为验收完成声明。

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
