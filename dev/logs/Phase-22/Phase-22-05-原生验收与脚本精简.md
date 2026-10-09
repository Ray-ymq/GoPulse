# Phase-22-05：Go 原生验收与 Python 脚本精简

> 实际状态：已完成。目标版本 `2.4.5`，分支 `develop/2.4.5`。候选 revision `7a2520b`；实际 CI run `37878900214` 全部通过。

## 实际完成

- 在 `backend/cmd/migrate/main_integration_test.go` 增加带 `integration` tag 的 `TestMigrationStateIntegration`，通过真实 migrate CLI 和自有 MySQL 8.4.0 容器覆盖空库并发、重复 up、锁超时、dirty/ahead 拒绝、DDL 权限失败后的 dirty 保留、显式 v12 恢复、退出码、凭据脱敏和 owner label 清理。
- 测试使用随机凭据、0600 env 文件、动态 loopback 端口、Context/t.TempDir/t.Cleanup；成功回执在容器及匿名卷清理确认后输出 `gopulse.migration-state.v1` JSON。
- 删除已被 Go 等价验收承接的 `scripts/ci/verify_migration_state.py`，以及 Phase 18 已结束的一次性 `phase18_diagnostic.py` 和 `test_phase18_diagnostic.py`。历史日志与原始证据中的路径引用未改写。
- 更新迁移状态合同、本机验证映射、Phase-22 总方案、能力状态和本日志；同步 `VERSION`、`.env.example`、两前端 package 与 lock 根版本到 `2.4.5`。
- 固定后缀的同一 Git 跟踪范围统计：基线 `737` 个代码文件、Python `99` 个/`23,470` 行/`13.4328%`；候选 `735` 个代码文件、Python `96` 个/`22,853` 行/`13.0612%`。

## 实际命令与结果

- `git fetch upstream --prune`：通过；从 `upstream/main` `9a6b137` 继续已分配的 `develop/2.4.5` 独立 worktree。远端已有的原生测试提交 `c03f6f7` 属于同一未完成批次，本次在其上继续。
- `gofmt -d backend/cmd/migrate/main_integration_test.go`：通过，无格式差异。
- `go -C backend test -tags=integration ./cmd/migrate -run '^$' -count=1`：通过。
- `go -C backend test -tags=integration ./... -run '^$' -count=1`：通过，所有 integration 包编译通过。
- `make test MODULE=backend`：通过。
- `INTEGRATION_TESTS=1 go -C backend test -tags=integration ./cmd/migrate -run '^TestMigrationStateIntegration$' -count=1 -v -timeout 6m`：通过，28.43 秒；JSON 回执 `complete=true`，检查为 `empty_concurrent_current_repeat`、`lock_timeout`、`dirty_rejected_unchanged`、`ahead_rejected_unchanged`、`apply_failure_keeps_dirty`、`explicit_v12_resume_then_v13`，目标版本 13，归属容器和匿名卷已删除。
- `make integration SCOPE=business`：通过；共享 business 测试项目、迁移包（28.20 秒）及全部 Backend integration 包通过，依赖资源完成清理。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，201 个测试。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/2.4.5 --base-ref upstream/main --mode development`：通过。
- `git diff --check`：通过；固定后缀统计通过上述同一跟踪范围复核。
- 候选 `be3e54e` 的 CI run `37878811257` 首次在治理自测失败，产品 job 未启动；原因是推送候选仍为 `VERSION=2.4.4`，使 stale-revision 自测先命中分支版本错误。同步允许的六个版本元数据文件到 `2.4.5` 后，没有重跑无关本地验收，仅重推修正候选。
- 修正候选 `7a2520b` 的 CI run `37878900214`：Branch governance、Backend、全部模块/exporter、Scripts and Compose、business/observe integration、两类浏览器、Full-stack Compose acceptance 全部通过；Open PR and merge 按完成元数据尚未登记而跳过。

## 实际变更文件

- `backend/cmd/migrate/main_integration_test.go`
- `scripts/ci/verify_migration_state.py`（删除）
- `scripts/ci/phase18_diagnostic.py`（删除）
- `scripts/ci/test_phase18_diagnostic.py`（删除）
- `dev/contracts/migration-state.md`
- `dev/validation/local-development-tests.md`
- `dev/imple/Phase-22/Phase-22-总实施方案.md`
- `dev/imple/Phase-22/Phase-22-05-原生验收与脚本精简.md`
- `VERSION`、`.env.example`
- `frontend/package.json`、`frontend/package-lock.json`
- `admin-frontend/package.json`、`admin-frontend/package-lock.json`
- `dev/status/capability-status.md`
- 本日志文件

## 偏差与修正

- 远端已预先存在同一批次的 `develop/2.4.5` 和原生测试提交；其祖先为最新 `upstream/main`，未另建同名分支或改名，使用独立 worktree 继续。
- 首次推送候选未同步目标版本，触发治理自测的前置版本错误；按日志证据只修正版本元数据并重推，第二次 CI 全部通过。未修改治理测试、CI 选择器或产品代码。

## 已知限制与后续项

- 本批不重跑 Phase 17～21 历史正式矩阵，不覆盖备份恢复、持续告警周期、容量、长期运行、完整 Trace、角色、页面或发布证据；这些入口和历史证据保持原路径。
- 退役诊断脚本的四类一次性 Phase 18 断言不宣称由迁移测试替代；它们随已结束的定界诊断能力退役。
- 本批完成后不沿用 `develop/2.4.5` 作为后续开发分支。
