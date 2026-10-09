# Phase-22-05：Go 原生验收与 Python 脚本精简

> 状态：已分配、待实施。目标版本 `2.4.5`，分支 `develop/2.4.5`。用户于 2026-10-09 授权实施。

## 1. 交付、进入条件与文件边界

一个完整迁移单元：所属 Go 包承载真实迁移 CLI 验收，Make 复用统一入口；等价验收通过后删除迁移 Python 执行器及两个已结束的一次性历史诊断文件。参考 Linkd 的 Go 测试、真实依赖、CLI 子进程、断言与清理方式，不复制其执行框架。

规划只新增本分方案，并修订 [Phase 22 总方案](Phase-22-总实施方案.md)。01～04 保留原结论。规划在 `update` 提交、通过治理检查并以 merge commit 进入主远端 main 后，fetch 主远端、重读总方案与 04 日志，从最新主线创建 `develop/2.4.5`。原工作区的用户改动保持原状。

精确允许文件：

| 文件 | 允许操作 |
| --- | --- |
| `backend/cmd/migrate/main_integration_test.go` | 新增唯一 Go 测试文件，`integration` tag，`TestMigrationStateIntegration` |
| `scripts/ci/verify_migration_state.py` | 原生等价验收成功后删除，121 行 |
| `scripts/ci/phase18_diagnostic.py` | 退役一次性历史诊断后删除，431 行 |
| `scripts/ci/test_phase18_diagnostic.py` | 随已退役能力删除，65 行 |
| `dev/contracts/migration-state.md` | 更新现行迁移验证入口与 JSON 回执边界 |
| `dev/validation/local-development-tests.md` | 登记原生迁移覆盖、退役依据及剩余专项边界 |
| `dev/imple/Phase-22/Phase-22-总实施方案.md` | 追加 05 分配、能力和验收条件；完成时登记实际状态 |
| `dev/imple/Phase-22/Phase-22-05-原生验收与脚本精简.md` | 本分方案；完成时登记实际状态 |
| `VERSION`、`.env.example` | 成功完成时同步 `2.4.5` |
| `frontend/package.json`、`frontend/package-lock.json` | 同步根版本及 lock 根 package 版本 |
| `admin-frontend/package.json`、`admin-frontend/package-lock.json` | 同步根版本及 lock 根 package 版本 |
| `dev/status/capability-status.md` | 记录实际迁移覆盖与退役边界 |
| `dev/logs/Phase-22/Phase-22-05-原生验收与脚本精简.md` | 同名真实完成日志 |

不新增 Python，不修改 Make、CI 选择器、环境管理、产品迁移逻辑、数据库 schema、容量或发布验收工具。不迁移角色、告警、页面及容量工具；不修改冻结设计、已完成分方案、历史日志和原始证据。

## 2. 验证映射与退役合同

| 验证对象 | 已有覆盖及缺口 | 最低有效层级、本批承接 |
| --- | --- | --- |
| 源文件、状态与恢复规则 | `backend/cmd/migrate/main_test.go` 已覆盖解析和驱动断言；没有真实 CLI/MySQL 生命周期 | 保留单测，新增所属包 Go 集成测试 |
| 空库、并发 up、current 与重复 up | `verify_migration_state.py` 的 `empty_concurrent_current_repeat` | 构建真实 migrate 二进制；两次并发仅一次 `changed=true`，后续保持 current |
| 迁移锁超时 | Python 复用 `scripts/ci/testdata/migration-lock.go` 持锁 | 继续构建该 Go fixture，不改它；CLI 返回 6 与 `lock_timeout` |
| dirty/ahead 拒绝 | Python 核对状态、退出 4/5、拒绝原因、表集合及版本不变 | 真实 MySQL 中保留相同断言，并核对 dirty 标记 |
| DDL 权限失败 | Python 撤销 CREATE、真实 up 返回 8，保持 dirty 且表集合不变 | 保留真实权限失败，重复 up 拒绝 dirty；不以 mock 代替 |
| 显式版本 12 恢复 | Python 设置 v12 dirty 后恢复至目标 13 | 保留显式 v12 恢复和完整表集合断言，不扩展任意 dirty 修复 |
| 退出、脱敏、依赖失败与清理 | Python 检查退出码、凭据不出现在输出、容器 owner label | Go Context、`t.TempDir`、`t.Cleanup`；独立 MySQL `8.4.0`，随机凭据、动态 loopback 端口、归属标签；缺 Docker/启动失败必须失败 |
| 回执与现行调用 | 当前合同仍指向迁移 Python | 在测试日志输出 `gopulse.migration-state.v1` JSON，含真实 checks、binary_target、complete 与清理结果；更新合同及映射 |
| 历史诊断能力退役 | `phase18_diagnostic.py` 仅被其自测导入；Phase 18 定界排查已结束 | 退役 null error-list、后端日志摘要/脱敏、负载窗口/慢请求汇总、Outbox 变化/速率四类自测；它们不宣称被迁移测试替代 |

删除前再次检索代码导入、脚本、Make、CI、有效合同及历史证据校验器。历史 Phase 17/18 日志与证据引用保持原路径，历史脚本源码由既有 Git 提交保留。容量、告警、角色和发布工具及其必要自测继续保留。

测试仅操作自有 MySQL 容器，不能连接共享 business/dev 数据库。Docker env-file 权限 0600，命令和测试失败信息不暴露随机密码。清理先核对 owner label，再删除该容器及匿名卷，并核对资源已消失；不做全局 prune。成功 JSON 必须在归属清理确认后产生。失败保留真实 incomplete 事实，不拼接其他 revision 的回执。

## 3. 固定门禁与统计

| case_id | 命令或检查 | 通过条件及证据 |
| --- | --- | --- |
| M01 | `make test MODULE=backend` | 普通 Backend 测试通过，不启动 Docker |
| M02 | `go -C backend test -tags=integration ./... -run '^$' -count=1` | 所有 integration 包编译通过 |
| M03 | 最终候选一次 `make integration SCOPE=business` | 新迁移用例自动进入现有门禁，全部成功、失败、退出、脱敏及清理断言通过；保留原始 Go 日志及 JSON 回执 |
| M04 | `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` | 剩余 Python 自测通过 |
| M05 | 当前引用检索、`git diff --check`、`python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/2.4.5 --base-ref upstream/main --mode completion` | 现行代码/配置没有调用已删除文件；历史引用保留；版本一致、分支分配及完成日志有效 |
| M06 | 按现有 push workflow 的实际 CI | 既有选择规则下所有被选择门禁通过；不修改选择器，不重跑历史容量或发布矩阵 |
| M07 | 同一 Git 跟踪代码扩展名统计 | Python 净减少至少 3 文件、617 行，文件数、行数、占比均下降 |

最小真实失败复现入口：

```sh
INTEGRATION_TESTS=1 go -C backend test -tags=integration ./cmd/migrate -run '^TestMigrationStateIntegration$' -count=1 -v -timeout 6m
```

在最终 business 入口前完成模式、编译、公共方法、退出/回执和清理边界的直接检查；不另外构造真实全栈来探查编译问题。原生用例成功后才删除三个 Python 文件，删除本身不使未变 Go 输入的通过结果失效；若验收代码、依赖、配置或相关候选元数据变化，登记影响并重验受影响门禁。

统计固定后缀 `.cjs`、`.css`、`.go`、`.js`、`.jsx`、`.mjs`、`.py`、`.scss`、`.sh`、`.sql`、`.ts`、`.tsx`、`.vue`，排除 vendor/node_modules；对同一 `git ls-files` 跟踪范围的实际文件统计，新增测试纳入跟踪后再比较。2026-10-09 基线：737 个代码文件，Python 99 个、23,470 行，占代码文件 13.4328%。预期新增 1 Go、删除 3 Python 后：735 个代码文件，Python 96 个、22,853 行、13.0612%。不靠空文件、改后缀或仓库内搬移减少。

## 4. 成本与实验

预计 120 分钟，累计上限 180 分钟；规划登记、发现、实现、构建、环境准备、失败诊断、验收、实际 CI、日志和清理均计入同一台账。

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现与实现（含规划登记） | 45 | 60 |
| 直接检查 | 10 | 15 |
| 环境准备 | 10 | 15 |
| 真实验收 | 15 | 25 |
| 实际 CI | 30 | 55 |
| 日志与归属清理 | 10 | 10 |
| 合计 | 120 | 180 |

无长时实验。迁移测试一次独立容器启动、全组短时 CLI 操作及清理；就绪等待最多 90 秒，锁 fixture 固定持锁 15 秒，最小复现总超时 6 分钟。business 全组沿用已有有界入口，真实验收阶段含既有依赖启动与清理成本，不以短探针代替成功/失败用例。CI 调用现有门禁并复用未变构建缓存。

90、144 分钟报告已完成、剩余门禁、累计耗时及下一步。启动昂贵命令前核对预计耗时加安全清理是否落在剩余预算内。

## 5. 失败、证据接续与完成

首次基础设施失败停止整组，保存失败边界与原始输出。区分 CLI 行为失败和 Docker/依赖/测试基础设施失败；同一未解决原因最多两次定向诊断，每次不超过 10 分钟，计入总预算。诊断优先最小入口或最低有效层；已证明原因、实际修正及直接检查通过后才能重跑受影响组，不靠更换报错或延长超时重新启动。

私有台账登记起止时间、命令结果、passed/failed/not_started、Git revision、相关测试/config/工具 digest、依赖镜像身份、原始日志及清理结果，不含凭据。通过项仅在相关输入及环境未变时有效；本测试不提供单个子场景 resume，受影响真实迁移组需完整重跑，不手改回执。历史原始证据不改写或换绑。

到累计上限或重复诊断停点，停止新工作、有界清理归属资源、保留实际改动和证据，登记未完成项与有限接续方案；不创建完成提交、不自动扩展迁移范围。再次执行须修订剩余预算并由用户明确继续。

M01～M07、L09～L10 全部通过且无阻塞后，记录同名真实日志、同步版本与能力状态，执行完成校验，仅提交本批文件。实际 CI 的远端记录继续保留；未执行或未通过的事项不得写为完成。完成后不沿用此开发分支。规则依据：[根规则](../../../AGENTS.md)、[测试工具规则](../../../scripts/AGENTS.md)、[预算规则](../../rules/implementation-execution-budget.md)。
