# Phase-22-04：CI 与文档收口

> 实际状态：已完成。目标版本 `2.4.4`，分支 `develop/2.4.4`，候选 revision `db0d833`。产品固定 CI run `37781739950` 全部通过。

## 实际完成

- 增加基于共同祖先 diff 的确定性 `quality_scope`，覆盖 backend、componentmetrics、router、marshaller、monitor、六个 exporter、两套前端、business/observe integration、business/observe e2e、Compose 与 tools；文档变更只触发治理，未知非文档路径保守触发产品检查。
- 在既有 quality workflow 中补齐 componentmetrics、五个缺失 exporter、独立 admin frontend、observe integration/e2e 和 business/observe e2e 入口；自动 PR/合并增加完成元数据条件，cache warm 收窄到真实构建输入。
- `validate_branch.py` 区分 development/completion；初始化脚本从配置的 primary remote 创建分支，不提前改 `VERSION` 或创建 bootstrap 完成提交。
- 用户端和管理端 Vite 的 dev lifecycle、business e2e、observe e2e 启动均显式绑定 `127.0.0.1`，与既有 readiness 探测一致。
- README、backend/admin frontend README 和本机验证映射已更新；Phase-22-01/03/04、总方案和 capability status 已登记真实完成状态；版本元数据同步到 `2.4.4`。

## 实际变更文件

- `.github/workflows/quality-gates.yml`
- `.github/workflows/auto-pr-merge.yml`
- `.github/workflows/cache-warm.yml`
- `scripts/ci/local_development.py`
- `scripts/ci/quality_scope.py`、`scripts/ci/test_quality_scope.py`
- `scripts/ci/validate_branch.py`、`scripts/ci/test_validate_branch.py`
- `scripts/start-development-batch.sh`
- `README.md`、`backend/README.md`、`admin-frontend/README.md`
- `dev/validation/local-development-tests.md`
- `.env.example`、`VERSION`、两前端 `package.json` 与 `package-lock.json`
- `dev/imple/Phase-22/Phase-22-总实施方案.md`
- `dev/imple/Phase-22/Phase-22-01-本机启动与依赖隔离.md`
- `dev/imple/Phase-22/Phase-22-03-前端与本机浏览器验证.md`
- `dev/imple/Phase-22/Phase-22-04-CI与文档收口.md`
- `dev/status/capability-status.md`
- 本日志文件

## 实际命令与结果

- `git fetch origin`：通过；从 `origin/main` 创建独立 worktree 和 `develop/2.4.4`。
- `python3 scripts/ci/sync_version_metadata.py --repo . --version 2.4.4`、`python3 scripts/ci/validate_versions.py`：通过。
- `PYTHONPATH=scripts/ci python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：最终 205 个测试通过；版本元数据同步前曾有一次旧版本断言失败，修正候选元数据后重跑通过。
- 11 条固定 Go 模块命令 `make test MODULE=backend`、`monitor`、`router`、`marshaller`、`componentmetrics` 和六个 exporter：全部通过。
- `npm ci`、两前端 `npm test -- --run`：通过，用户端 18 个文件/72 个测试，管理端 13 个文件/54 个测试。
- `python3 -m py_compile scripts/ci/local_development.py`、`bash -n scripts/start-development-batch.sh`、`make help`、business/observe CLI dry-run、四个 Vite 启动调用检查、workflow YAML 解析和 `git diff --check`：全部通过。
- 本地用户端/管理端 Vite 使用 `--host 127.0.0.1` 启动后，分别通过 `127.0.0.1` readiness probe。
- CI `37778322603`：治理、模块和部分集成检查通过；business browser 在 `test user Vite` readiness 处失败，按规则取消剩余矩阵。
- CI `37779825125`：尝试仅保留 runner 工具链环境后仍在同一 readiness 边界失败，取消剩余矩阵；该无效 workflow 注入已从最终候选移除。
- CI `37781739950` / revision `db0d833`：Branch governance、全部 Go 模块、两前端、business/observe integration、business/observe browser、scripts/Compose、full-stack Compose 和全部 exporter jobs 全部通过。

## 偏差与修正

- 首次真实 CI 暴露 GitHub runner 上 Vite 绑定与 IPv4 readiness 探测不一致的边界。用户批准后修订本分方案允许文件和续行预算，仅在 `scripts/ci/local_development.py` 四个 Vite 启动调用增加 `--host 127.0.0.1`；未修改页面实现、浏览器断言或生命周期语义。
- 首次修复候选中的 runner 环境注入未改变失败结果，随后从最终 workflow 中移除；最终修复由显式 IPv4 绑定完成。
- CI 非阻塞提示包括 GitHub Actions Node.js 20 deprecation warning 和部分依赖缓存未命中；不影响 job 结果。
- 初始候选的 Open PR and merge 因 completion log 尚未存在而跳过；本收口提交补齐同名日志、Phase 状态和 capability status 后再执行完成分支校验。

## 已知限制与后续项

- 本批未重跑 Phase 18～21 历史正式矩阵、容量、长期运行、恢复或发布候选验收；这些专项入口和历史证据保持不变。
- 本地最终未重复执行 01～03 已通过的 business/observe 真实门禁；本批引用其未变输入身份，并由 `37781739950` 执行真实 GitHub integration/e2e 门禁。
- GitHub runner 的 loopback DNS 配置差异由显式 IPv4 绑定规避；前端代理的既有业务接口合同未改变。
