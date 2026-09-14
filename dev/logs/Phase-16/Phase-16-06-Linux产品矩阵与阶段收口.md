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
