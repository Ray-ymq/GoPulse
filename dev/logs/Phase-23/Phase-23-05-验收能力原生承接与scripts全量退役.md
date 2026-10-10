# Phase-23-05 实施日志：验收能力原生承接与 `scripts/` 全量退役

## 结果与范围

- 执行分支：`develop/2.5.5`，来源为 `origin/main` 的 `d6f2a80`；执行工作树为
  `/tmp/gopulse-phase23-05`，全新 G1-G5 检出为 `/tmp/gopulse-phase23-05-final-checkout-cab06eb`。
- 目标版本：`2.5.5`。`VERSION`、`.env.example`、两个前端 package 与 lockfile、
  `deploy/runtime-contracts.json` 已在 `73d9fa6` 同步。
- 最终候选：`cab06eb3aa1aa48695647551af3890e5a7b4c5f5`；目录
  `/tmp/gopulse-phase23-05-candidate-runtime-final-cab06eb-retry`；manifest
  `sha256:c56d89986a37cd429dbbf37f6cd9a6ae93278d9da74545e66fd36858f4e75c7d`；
  `verification-amd64` 为
  `sha256:3594f92e6abe0fe73ce39804989829e71ac7e2e87972323d431e4a32a9d11ba8`；
  Bundle 为
  `sha256:aa044441edcf842beb78d577fb7b4f3929c64cc729e5ff8f172c935200a5be2e`。
- 相对 `origin/main` 实际变更 172 个文件：新增 `acceptance/**` 原生验收模块；新增
  `devtools/internal/buildcache` 与 `devtools/internal/stack`；迁移治理工具到 `ci/**`；
  调整根 `Makefile`、Compose/CI/workflow、验收 Dockerfile、lifecycle/monitor/backend
  入口、文档与能力台账；迁移三个夹具到 `acceptance/internal/fixtures`、迁移
  `backend/testdata/migration-lock.go`；删除全部 tracked `scripts/**` 执行器、转发器、
  夹具及其已退役自测。冻结设计文件保持未修改。

## A-F 实际完成

- `093f36b` 完成 Go 构建缓存承接：`devenv build-cache`、`make build-images CACHE=gha|none`、
  workflow cache 入口和 digest 输入迁移。原生/旧定义对比、token 脱敏、devtools test/race/check
  与真实 Buildx bake 均通过。
- `2ec60a1`、`5b124fc`、`c6af183` 完成 acceptance harness、Compose、业务/观测/插件/告警/
  角色/页面、stack、lifecycle 与 acceptance 镜像入口承接；真实业务与观测 Compose、专项套件、
  插件归档确定性、stack 清理均通过。
- `41b0261` 修复失败矩阵的两个 harness 边界：占用端口场景复用 ready edge 验证产品 code 15；
  外部同名 volume 场景先停止自有安装，再注入未归属 volume 验证 code 17，最后恢复安装。修复后
  code 10、11、13、14、15、16、17、18、19 与 signal 20 全部通过并清理。
- `cbc3394`、`659c720` 完成治理迁移和非治理 acceptance/CI 资产退役。迁移后保留 5 个治理自测
  模块、38 个用例；基线为 19 个模块、83 个用例。删除的 14 个模块分别属于 Compose/缓存/
  marshaller/business 原生承接，以及 Phase 14-17、运行时矩阵、交付库、恢复路径等 §1.5
  明确退役能力，差异已登记，未解释的测试减少不存在。
- `3ea38ee` 修复 G2 发现的 migration fixture 路径：`backend/cmd/migrate/main_integration_test.go`
  改用迁移后的 `backend/testdata/migration-lock.go`。
- `cab06eb` 修复 G2 observe 集成与 E2E 之间的账号清理边界：观测清理现在覆盖由管理员账号派生的
  integration user，并增加 `TestObservationUsernamesIncludesIntegrationUser`。

实际执行的阶段门禁包括：

- `python3 -m unittest discover -s ci -p 'test_*.py'`：38 tests passed；`python3 ci/validate_versions.py`、
  `python3 ci/validate_branch.py --branch develop/2.5.5 --base-ref origin/main --mode development`、
  `python3 ci/quality_scope.py --changed-file acceptance/internal/scenario/lifecycle.go --format json`：均通过。
- `make test MODULE=acceptance`、`make test MODULE=backend`、devtools test/race/check、
  `make check-all`：通过；`make package` 的既有 dirty-tree 拒绝检查通过。
- 旧入口、交付库和 Phase 16/17 活引用扫描、workflow YAML 解析、`python3 scripts/` workflow 扫描：
  通过。`deploy/phase16-acceptance.yaml` 与历史文档按计划保留。

## G1-G5 固定候选复验

### G1 开发环境入口

- 在全新 detached checkout 中执行两次 `make deps`：首次依赖健康，第二次输出 already running。
- `make dev` 后 `http://127.0.0.1:8080/health` 与 `http://127.0.0.1:5173/` 返回 200；`make stop` 通过。
- `make dev-observe` 后 backend、frontend、admin（跟随重定向）、router 9091、marshaller 9093、
  monitor 9090 均返回 200；再次 `make stop` 通过。停止后本工作区容器、网络和端口均已清除，命名
  volume 保留。

### G2 模块、集成与浏览器

- 17 个模块的 `make test MODULE=...` 全部通过，记录在
  `/tmp/gopulse-phase23-05-evidence/g2-module-tests-cab06eb-final.log`；`make check-all` 通过。
- `make integration SCOPE=business` 与 `make integration SCOPE=observe` 通过。
- 并发第二个 `make integration SCOPE=observe` 在环境锁处立即以非零退出，输出
  `another integration or browser check owns the test environment lock`；第一个 business 集成随后
  正常通过并清理。
- `make e2e SCOPE=business`：3 tests passed；`make e2e SCOPE=observe`：3 个 admin tests 与 1 个
  Compose observability test 全部 passed。对应日志在 evidence 根目录的 `g2-*cab06eb-final.log`。

### G3 CI 与治理

- 推送 `origin/develop/2.5.5` 后 GitHub Actions run `38020663371` 完成并为 success。24 个质量 job
  （含 governance、17 模块相关 job、integration、native observability integration、full-stack Compose、
  acceptance tooling、两套 browser checks 与 lifecycle）均为 success；条件性的 `Open PR and merge`
  job 为 skipped，不影响质量 job。最终核对结果保存在
  `/tmp/gopulse-phase23-05-evidence/g3-ci-run-38020663371-final.json`。
- `grep -rn 'python3 scripts/' .github` 为空，治理 job 实际使用 `ci/`；workflow 四文件 YAML 解析通过。

### G4 固定候选 lifecycle

- 固定候选已由 `make package PLATFORM=linux/amd64 ... RUNTIME=1` 完成 runtime/Compose gate，包含
  lifecycle failure matrix。
- 在该 manifest 上执行 `make verify-lifecycle INSTALL=clean MANIFEST=.../release-manifest.json
  PLATFORM=linux/amd64`：doctor、init/verify 失败边界、up、HTTP、logs、down/restart/verify 与 cleanup
  全部通过，manifest digest 始终为 `sha256:c56d8998...`。
- D1-7 复用验收使用显式、含空格的外部安装路径：clean keep 回执
  `/tmp/gopulse-phase23-05-evidence/reuse-clean-receipt-cab06eb.json` 与 reuse 回执
  `/tmp/gopulse-phase23-05-evidence/reuse-receipt-cab06eb.json` 均为 passed，保持安装 identity、
  state/secrets 只读校验和 isolation。外部路径模式下 harness 不拥有安装目录，完成后按回执中的确切
  Compose project `gopulse-022d001d21fe` 执行了定向 `down`、网络/volume 精确删除，并删除安装目录；
  未使用全局 prune。

### G5 负向入口

- `./acceptance/bin/gopulse-acceptance compose --scope invalid` 退出 1。
- `make integration SCOPE=bogus` 退出 2。
- `PYTHONPATH=ci python3 -m unittest test_validate_versions` 的 7 个版本负向/正向自测通过，覆盖
  非 semver 与前端、lockfile、admin、Compose 元数据漂移。
- 失败矩阵已覆盖端口、依赖、Bundle、锁、卷、信号与未就绪状态；候选构建 dirty-tree 拒绝在
  `659c720` 阶段已实际通过，相关 lifecycle packaging 源在最终候选中未变化。

## 偏差、诊断与限制

- 最终候选第一次 package retry 因 Docker address pool 已被先前显式复用安装残留资源耗尽而失败。
  最小诊断定位到旧的 `gopulse-1a6b299c3506` project 及其四个 network、八个 volume，并另有一个
  临时 candidate network；逐一停止/删除这些确切资源后，仅重试 package 一次即通过，未执行 broad prune。
- D1-7 第一次 doctor 曾因外部 Elasticsearch platform digest 的 Docker distribution 请求临时返回
  不可用而 code 10；直接复现到 daemon distribution API，确认平台镜像缓存/registry 状态恢复后，
  同一候选的 clean/reuse gate 通过。该 transient 诊断未修改候选或放宽门禁。
- 早期候选与中间回执在 `cab06eb` 与最终 manifest 固定前均视为失效；本日志只把 cab06eb 候选和其
  最终 evidence 计为完成。
- 曾做过真实 backup/restore 探索，但未把它计为通过：当前生产 Compose 的 Kafka topic/副本拓扑与
  backup engine 的 single-partition contract 不兼容，实验在 Kafka code 19/ready 边界失败。D1-7
  当前复用 runner 的 `restore-result.json` 是明确标注的 synthetic precondition，只证明 runner 的
  facts_verified 前置条件和正常拓扑 reuse；未来若要宣称真实 backup/restore，需另行调整并验证
  topology/contract，属于后续范围。
- H2 的 `git grep` 唯一命中为仓库根 `AGENTS.md` 的治理规则文字（说明本批维护范围下的
  `scripts/`），不是可执行引用；该规则文件按用户提供的仓库约束保留。其余仓库外 `dev/docs` 的
  活引用扫描为空，历史计划/验证文档按要求保留原始上下文。
- `gh run watch --exit-status` 最后一次因 GitHub annotations API EOF 返回 1；随后用
  `gh run view 38020663371 --json status,conclusion,jobs` 重新核对，run 为 completed/success，所有
  质量 job 为 success；因此不把 annotations API 的读取异常计为 CI 失败。

## 完成时实际使用的证据

- 私有 evidence 根：`/tmp/gopulse-phase23-05-evidence`；其中保存阶段回执、候选 package/runtime、
  G1-G5、治理与 CI 结果。该目录不属于提交内容。
- 最终候选 registry 为手工启动的 `gopulse-phase23-05-candidate-registry`，候选测试结束后做定向
  停止/删除；不保留验收或 lifecycle 容器、网络、volume 残留。
- 完成提交前执行 `git ls-files scripts` 为空、`test ! -d scripts`、`ci` governance/branch/version
  completion 校验与 `git status` 核对；提交只包含本实施日志。
