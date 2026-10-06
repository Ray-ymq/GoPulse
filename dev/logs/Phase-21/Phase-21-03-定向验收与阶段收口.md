# Phase-21-03 实施日志

## 结果

- 本批已完成，完成版本为 `2.3.3`，开发分支为 `develop/2.3.3`。
- 实际被测制品是冻结产品 `2.3.2`，源 revision 为 `ca0aa5efb6f03eb303c4ff10efd2e942d289bbcb`，候选 Bundle 为 `2.3.2-candidate-ca0aa5efb6f0`。
- 在独立干净 checkout `/tmp/gopulse-phase21-03-source-20261006-4` 中完成候选 Bundle、严格 manifest preflight、正式 S01～S07、归属清理、正式 receipt verifier、白名单 publication 和 publication verifier。
- S01、S02、S03、S04、S05、S06、S07 全部 `passed`；S05 执行了合同要求的完整 60 秒观察窗口。正式验收运行项目为 `gopulse-runtime-6906dc2cff3d`，清理回执为 `passed`。
- 选定证据已由 checker 生成并复核：[`summary.json`](../Phase-21-03-evidence/summary.json) 和 [`publication.json`](../Phase-21-03-evidence/publication.json)。正式收据及 S01～S07 原始证据保留在任务工作区私有 `.run` 目录，发布物记录其来源路径与 hash。

## 实际完成的工作

- 首次 candidate 构建在 Bundle 组装阶段发现 `business-worker-2` 等 Compose 服务缺少逻辑镜像映射；按有界修复合同增加显式映射及回归测试。
- 修复后又在最小边界发现严格 preflight runner 拒绝带 manifest，以及干净 detached checkout 没有 `.run` 父目录两个工具边界问题；分别增加 manifest 绑定校验和私有证据根初始化，并补充回归测试。
- 保留首次失败候选及后续无效候选的私有失败证据；正式验收只使用修复后重新构建的候选，不拼接不同候选的镜像、manifest 或 receipt。
- 更新 Phase-21 总方案、Phase-21-03 分方案、Phase-21-03R 修复合同和能力状态，登记实际结果、偏差、证据及未覆盖边界。
- 成功后同步 `VERSION=2.3.3`、`.env.example`、两个前端 package 元数据和 `deploy/runtime-contracts.json` 的产品版本字段。

## 实际变更文件

- `scripts/ci/release_artifacts.py`
- `scripts/ci/test_release_artifacts.py`
- `scripts/ci/runtime_acceptance.py`
- `scripts/ci/test_runtime_acceptance.py`
- `scripts/ci/verify_runtime_contracts.py`
- `VERSION`
- `.env.example`
- `frontend/package.json`、`frontend/package-lock.json`
- `admin-frontend/package.json`、`admin-frontend/package-lock.json`
- `deploy/runtime-contracts.json`
- `dev/imple/Phase-21/Phase-21-总实施方案.md`
- `dev/imple/Phase-21/Phase-21-03-定向验收与阶段收口.md`
- `dev/imple/Phase-21/Phase-21-03R-候选Bundle映射修复.md`
- `dev/status/capability-status.md`
- 本文件
- `dev/logs/Phase-21/Phase-21-03-evidence/summary.json`
- `dev/logs/Phase-21/Phase-21-03-evidence/publication.json`

私有 `.run` 目录中的候选构建、正式 receipt 和原始 case 文件未作为普通源码文件编辑；首次失败目录 `phase21-03-20261006-a` 以及后续无效候选目录保留其历史原始证据。

## 实际执行的命令与结果

- `git fetch upstream --prune`：通过；冻结基线为 `upstream/main=12e147799fd89de29cd825d428689b5402d9cb7f`。
- `rtk proxy python3 scripts/ci/verify_runtime_contracts.py --candidate 2.3.2`：通过。
- `rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_release_artifacts.py'`：通过，3 tests；后续 release 测试发现通过，9 tests。
- `rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_runtime_*.py'`：通过；修复迭代后为 5、6、7 tests，均通过。
- `rtk proxy python3 scripts/ci/release_artifacts.py build --registry 127.0.0.1:15021/gopulse ...`：首次失败，`unmapped product service: business-worker-2`；未进入验收。
- `rtk proxy python3 scripts/ci/release_artifacts.py build --registry 127.0.0.1:15024/gopulse ...`：修复后通过，生成候选 manifest；候选镜像 digest、Bundle 映射和源 revision 由 manifest 记录。
- `rtk proxy python3 scripts/ci/runtime_acceptance.py --suite service-split --preflight --candidate 2.3.2 --manifest ...`：修复后严格预检通过，S01～S07 全部通过。
- `rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence .../preflight-receipt.json`：通过。
- `rtk proxy python3 scripts/ci/runtime_acceptance.py --suite service-split --candidate 2.3.2 --manifest ... --preflight-receipt ... --evidence .../formal-receipt.json`：通过，正式 S01～S07 全部通过。
- `rtk proxy python3 scripts/ci/verify_runtime_contracts.py --candidate 2.3.2 --evidence .../formal-receipt.json`：通过。
- `rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence .../evidence.json --publish dev/logs/Phase-21/Phase-21-03-evidence`：通过，生成白名单 summary/publication。
- `rtk proxy python3 scripts/ci/verify_runtime_contracts.py --verify-publication dev/logs/Phase-21/Phase-21-03-evidence/publication.json`：在冻结 checkout 和任务工作区各通过一次。
- 对本批 registry `gopulse-phase21-03-registry-20261006-d`、候选标签 `127.0.0.1:15024/gopulse/*:2.3.2-candidate-ca0aa5efb6f0` 和冻结 checkout 执行精确清理：通过；未执行全局 Docker prune，正式原始证据未删除。
- `rtk proxy python3 scripts/ci/validate_versions.py`：通过。
- `rtk proxy python3 scripts/ci/verify_runtime_contracts.py`：通过，当前完成版本合同为 `2.3.3`。
- `rtk proxy python3 scripts/ci/validate_branch.py --branch develop/2.3.3 --base-ref upstream/main`：通过。
- `rtk git diff --check`：通过。

## 偏差、限制与后续

- 首次 Bundle 映射失败、修复后 preflight manifest 入口失败、以及 clean checkout `.run` 初始化失败均已保留在私有历史证据，并在 Phase-21-03R 中登记；正式候选从修复后的 revision 重新建立。
- 本阶段只声明最终双服务部署中的业务/管理代表性流程、platform 单侧停止观察、角色准入与实例/采集核对，以及归属清理通过；不声明共享 MySQL 故障隔离、状态层高可用、独立 dispatcher、多目标、容量、观测开销或长期稳定性。
- 候选实际版本为 `2.3.2`，完成版本 `2.3.3` 仅用于本批版本元数据和阶段收口；没有用 `2.3.3` 标签替换候选，也没有复用失败候选的回执。
