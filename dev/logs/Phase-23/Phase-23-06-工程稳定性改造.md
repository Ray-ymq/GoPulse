# Phase-23-06 实施日志：工程稳定性改造

## 执行身份与阶段状态

- 执行分支：`develop/2.5.6`，从 `origin/main` 的 `0610f3f` 创建；该 revision 是本批固定基线。
- 目标版本：`2.5.6`。本次实现和本地验收期间 `VERSION` 保持已完成值 `2.5.5`；最终版本元数据尚未收口。
- 实现提交：`7c19660`、`12fcfe8`、`f2e750b`、`4a0c0d8`、`5e78c8f`、`8b4aa62`、`77413f7`、`f29137e`。
- 本日志记录本地与远端实际结果；版本同步和正式发布收口仍单独按完成元数据条件处理。

## 实际交付

### H：文档与入口治理

- 增加 `ci/check_docs.py` 及其行为测试，根 Make 增加 `list-go-modules`、`list-npm-modules` 和
  `docs-check`。检查当前链接、锚点、图片、Make/npm 目标、模块名及冻结正文 diff。
- `python3 -m unittest discover -s ci -p 'test_*.py'`：50 tests passed；`make docs-check
  BASE_REF=0610f3f`：通过；`make check-compose`：通过。

### A-B：本机前置条件、工具准备与前端依赖

- 增加 `devtools/toolchains.json`、doctor/bootstrap、Go lint 工具准备和前端依赖输入摘要/成功标记。
  `make doctor`、两前端 bootstrap、`make bootstrap TOOLS_ONLY=1` 及重复准备均通过；失败不会留下
  成功标记，依赖输入未变化时复用。
- Make、devrun 和 E2E 的前端依赖路径统一使用 `devtools/internal/frontenddeps`，安装锁与活动开发
  进程保护保持在工作区归属范围内。

### C-D：Go 与双前端检查

- `.golangci.yml` v2 首批启用 `errcheck`、`staticcheck`、`unused`；增加原生 `ci/lint.py` 包装及
  共同 ESLint 规则、两前端 flat config 和锁定的检查依赖。
- 17 个模块的单模块 `make verify MODULE=... BASE_REF=0610f3f` 全部通过，最终完整 `make verify`
  也通过。`make lint-all` 如实返回非零：仅报告基线已有告警，新增告警为 0；未将存量告警包装成
  严格扫描通过。
- `make -C devtools test`、`make -C acceptance test` 及治理 Python 测试通过。

### E：失败诊断与清理边界

- 增加有界输出捕获、私有失败目录、环境/命令/复现记录、敏感信息脱敏、符号链接边界和复制容量限制；
  acceptance 在删除 Compose 容器前复制浏览器报告，并将主失败与清理失败分开记录。
- 实际失败回归留下了可读的诊断目录：
  `.run/local/e4e11fe0ac91/failures/20261010T181218.509670944Z-e2e-business-ac7fda098190/`，
  其中包含 `failure.txt`、环境和命令记录，以及 `artifacts/browser-report/`。

### F：统一选择、验证与 CI 门禁

- `ci/quality_scope.py` 消费根模块清单；`devenv verify`、根 `make verify`、`check-compose` 和
  `validate_quality_result.py` 共用选择/失败传播语义。
- `.github/workflows/quality-gates.yml` 增加文档检查、17 模块矩阵、质量结果完整性与失败诊断上传
  路径；本地选择器和行为测试已覆盖文档-only、未知模块、缺失/失败/意外跳过等边界。

## G：固定组合验收

| 门禁 | 实际结果 |
| --- | --- |
| doctor → bootstrap → 重复 bootstrap | 通过；本机 Go、Node/npm、Python、Docker、Compose、buildx 与 lint 工具均满足要求，重复准备复用输入。 |
| `make verify BASE_REF=0610f3f` 与 17 个单模块补验 | 通过；最终 head 的完整 verify 退出码为 0。 |
| `make lint-all` | 按合同非零；存量告警被报告，新增告警为 0。 |
| `make deps` → `make dev` → `make dev-observe` → `make stop` | `make deps`、`make dev`、停止后 `make dev-observe`、最终 `make stop` 通过。直接在业务 dev 生命周期仍活动时调用 `make dev-observe` 被既有归属锁拒绝，先 `make stop` 后按设计继续。 |
| `make integration SCOPE=business/observe` | 两个 scope 均通过。 |
| `make e2e SCOPE=business/observe` | business 3 tests passed；observe 的 3 个 admin tests 与 1 个 Compose observability test 均通过。 |
| `make verify-compose` | 生产 SPA、business、observability setup/admin 均通过。 |
| `make docs-check BASE_REF=0610f3f`、`make check-compose` | 均通过。 |

## 候选 Bundle 与 lifecycle

- 首次标准 `make package PLATFORM=linux/amd64 RUNTIME=1` 构建、元数据检查、Compose runtime gate 和
  lifecycle failure matrix 均通过；其默认临时 registry 按工具合同在命令退出时删除。
- 直接将该命令与独立 `make verify-lifecycle` 串行执行时，后者因 manifest 中的临时 registry 已被
  删除而得到 `registry index unavailable for frontend`。这不是 Bundle 内容失败，而是独立验收超出
  默认 registry 生命周期的命令编排问题。为完成固定门禁，随后使用明确归属的
  `127.0.0.1:15006/gopulse` registry、独立输出目录 `dist/phase23-06-candidate` 重建同一提交候选，
  保持 registry 存活并重跑 lifecycle。
- 持久 registry 候选的关键身份：revision `77413f7ea526a0fa652dcf1b99454c9fe9f4980b`，manifest
  `sha256:eaa6c1576565aa0638aba0b6b55f0572484fb7fa3565dadb2828a596de0dfba4`，Bundle
  `sha256:3ee44c4cf44607a9b6ef1f967547d6b3a8fe7b531a5fd978650d21826a5edecc`，验证回执文件
  `verification-amd64.json` 的摘要为
  `sha256:a41ec7c7f65572a161a2b2dd6680977511dac61572f6fadc2e24846ba99800be`。
- 该候选的 `make package ... RUNTIME=1` 及
  `make verify-lifecycle INSTALL=clean PLATFORM=linux/amd64
  MANIFEST=dist/phase23-06-candidate/release-manifest.json` 均退出 0；doctor、init/verify 边界、
  up/ready、HTTP/logs、stop/restart/verify 和清理矩阵通过，manifest digest 保持一致。
- 验收结束后只删除了本次创建的 `gopulse-phase23-06-registry`，并核对 lifecycle/acceptance 容器
  无残留。E2E 使用的 `gopulse-e4e11fe0ac91-test-255` 项目卷经 Compose 标签确认后定向删除，未使用
  全局 prune。

## 诊断、修复与偏差

- 最新修复提交 `77413f7` 处理了最终完整 verify 首次暴露的两个稳定性缺陷：E2E 用户名 token 的前
  16 位在持久测试卷上会重复，现改为纳入纳秒级唯一前缀；失败浏览器报告复制前的父目录由诊断器
  创建。修复后 business/observe E2E、Compose 验收和最终完整 `make verify` 均通过。
- `make dev-observe` 的第一次直接调用按既有生命周期保护返回“workspace already owns an active
  dev lifecycle”；这证明了错误边界，随后按归属顺序 stop → observe → stop 完成通过路径。
- 本地完整验证输出保留在 `/tmp/gopulse-verify-full-final-77413f7.log` 供本次排查复核；临时 acceptance
  二进制已移出工作树，工作树在写入本日志前保持干净。

## 远端质量补充（2026-10-11）

- `develop/2.5.6` 已推送到 `origin`，远端 head 为 `f29137ef90d6d3150f214d42d0537b6f7cb92068`。
- 初次 push 的 run `38115564843` 在 job 创建前因 workflow 配置错误失败；`actionlint` 定位为
  job-level `if` 使用了不可用的 `matrix` context，已由 `f29137e` 将选择条件下移到 matrix steps。
- 修复后的 [权威质量 workflow run 38115665206](https://github.com/Ray-ymq/GoPulse/actions/runs/38115665206)
  已完成并成功：44 个 job 全部成功，无失败；其中 `Open PR and merge` 按 completion metadata 未就绪
  正确 skipped。治理、文档、模块矩阵、集成、浏览器和 Compose job 均实际运行并通过。
- 随后的日志补充提交 `57c15a8` 首次运行同一 workflow 时，`Integration` 的
  `TestMigrationStateIntegration` 因 owned MySQL 动态端口连接失败返回 `connection_failure`；依赖启动、
  迁移、索引准备与清理均成功，失败诊断摘录已由 `integration-failure-diagnostics` artifact 保留。
  对失败 job 的 attempt 2 重试通过，run `38116197670` 最终为 success，44 个 job 全部成功，证明该次
  失败为可重试的 CI 依赖瞬态，未修改产品代码。

## 未完成收口与后续限制

- `VERSION` 仍为 `2.5.5`，未修改 `.env.example`、前端版本/lock 或 runtime contract 版本字段；这些
  只能在目标版本正式收口时按实际候选同步，并需要重新验证受影响输入。
- completion metadata 校验尚未通过，因此本日志记录为“本地实现、固定门禁与远端质量已通过，正式版本
  收口待执行”，不宣称 Phase-23-06 已完成。
