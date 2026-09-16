# Phase-17-04：Migration 与消息处理可靠性闭环开发记录

- 执行日期：2026-09-16（Asia/Shanghai）。
- 目标版本 / 分支：`1.14.4` / `develop/1.14.4`。
- 基线：fetch 后的 `upstream/main`，产品版本 `1.14.3`。
- **状态：实施中，未达到本批完成条件；不是 Phase-17-05 的已验收输入。**
- 使用计划指定的 `scripts/start-development-batch.sh Phase-17-04 --remote upstream` 创建分支；该脚本已自动创建 bootstrap 元数据提交 `5a46e79`。

## 实际生产实现

### Migration

- 新增内嵌 inventory 校验：连续编号、唯一方向、同名 up/down 配对、非空 SQL；当前 inventory 为 `000001` 至 `000013`。
- `validate` 不连接数据库；`status` 输出 JSON binary/database version 和 clean/behind/current/dirty/ahead；`up` 在同一个 MySQL migration lock 内检查版本、写 dirty、执行每条 up、成功后清 dirty。
- 安全退出码区分参数/配置 2、连接 3、dirty 4、ahead 5、锁超时 6、apply 8、source 9。底层 SQL、DSN、错误参数不进入 CLI 输出。
- 保留 version 12 专用 resume，其他 dirty 一律拒绝；`down` 帮助明确仅供本地开发。
- Compose job 执行 validate/status/up；status dirty 允许进入 up 进一步判断，但除专用 12 外仍被拒绝。长运行服务保持依赖 one-shot 成功。
- Backend readiness 从相同内嵌 inventory 派生 target，不再另写常量 13。
- **没有新增 Schema**：本次生产修改不增加持久化实体；binary target 仍为 13。

### Rabbit / Kafka / alert

- Rabbit secondary publish 在发送前和 confirm 后检查 context；取消不能授权原 delivery ack，走原有 nack/requeue。使用 channel publish sequence 匹配 confirm，避免上一条超时发布的迟到确认误 ack 下一条消息。增加 retry/dead 取消以及迟到 confirm 后新消息 ack/nack 的直接测试。
- Kafka commit 最多三次、指数有界等待，不在 commit 重试间重复目标写入；每次检查 context/lease，耗尽返回原有终止错误，复用已经存在的进程非零退出路径。
- Kafka storage retry 等待同时响应主进程取消和 lease 取消。
- Alert round panic 不再通过顶层 recover 静默终止整个 scheduler；round/evaluation/database/apply 失败使用固定安全 reason。复用已有事务 lease/revision 和连续性规则。
- 增加仅三种 source label 的 alert evaluation-known / last-success gauges；Backend 指标预算从 707 调整为 713，补齐日志传输/查询的 alert vocabulary。

## 真实验证及已观察失败

### 已通过的直接门禁

- Backend：`go test -count=1 ./cmd/migrate ./migrations ./internal/worker/... ./internal/notification ./internal/search ./internal/alert/...` 通过。
- Backend：`go test -race -count=1 ./internal/worker/... ./internal/alert/...` 通过。
- Marshaller：consumer 普通测试和 race 测试通过。
- Router：`go test -count=1 ./internal/kafka/... ./internal/httpserver/...` 通过。
- 直接共享边界：Backend platform / cmd/server / logquery、componentmetrics、Monitor logs、Marshaller logs 的相应测试通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：51 tests 通过。扩展原因：修改了产品 Compose migration 入口、现有验收脚本及共享指标/日志合同，需要检查直接受影响的 Compose/环境断言。
- `scripts/verify-compose.sh --self-test`、alerts verifier self-test、修改后的 Shell/Python 语法检查通过；**self-test 不等于完整 Compose 产品验收**。
- 版本/分支治理检查在 bootstrap 目标元数据为 1.14.4 时通过；最终状态见本记录末尾。

### 隔离 MySQL 实测

`python3 scripts/ci/verify_migration_state.py` 使用唯一命名/归属标签的 MySQL 8.4 容器，仅发布动态 loopback 端口，凭据来自私有 env 文件，结束后校验归属并删除本次容器和匿名卷。

实际通过：

1. 空库 status=clean；两个 runner 并发 up，只有一个返回 changed=true；最终 current=13，重复 up 无变化。
2. 同一个公开 MySQL driver 的锁被占用时，第二个 runner 以 code 6 安全失败。
3. dirty=11 / ahead=14 拒绝，表集合和注入的 version marker 不变；输出不含随机 credential canary。
4. 在本地测试库单独 down 13 构造 version 12 后，专用 dirty-12 resume 再推进 13 通过。此步骤只测专用 resume，**不是产品恢复或前序升级证明**。

证据：`dev/logs/Phase-17/evidence/Phase-17-04-migration.json`。

锁超时首轮实测发现 WithInstance 的 metadata 初始化已取锁，原错误被归为 connection_failure=3。为定位这个**必需失败测试**，仅查阅已锁定 golang-migrate MySQL driver 的 `WithInstance`/`WithConnection`、`ensureVersionTable`、`Lock` 三个相关路径，确认 10 秒 GET_LOCK 返回 `database.ErrLocked`；将初始化阶段也接入锁原因映射后复测通过。未开展依赖审计。

### 验收工具兼容修复与失败轮次

- business 首轮在 Compose 插值阶段失败：缺少 Phase-17-03 的六个 metrics token；已补齐。首次将日志写入 `.run` 导致 verifier 自己的全目录快照变化，后续改为 `/tmp` 保存运行日志。
- business 第二轮进入真实 API/浏览器验证：4 passed、3 failed、3 skipped。已观察问题为 bookmark 退出后未等待导航、delete/profile 使用泛化 status locator 与新增管理状态提示冲突；仅修复这三处 E2E 同步/定位，未更改 Frontend 生产行为。
- Marshaller 旧 verifier 依次暴露缺少 metrics token、旧 readiness 鉴权断言、原生 Monitor 缺少镜像内强制插件目录，以及旧上传 API/动态 host 插件地址与当前配置合同冲突。修复方向是使用当前源码构建的 Router/Monitor 容器和私有 env/归属卷，保持目标存储、Kafka offset、重平衡和重启原断言，不改产品配置以绕开限制。
- alerts 旧 verifier 使用不存在的 1.11.5 镜像；改为从当前源码构建本轮唯一 tag 的服务，不再混用历史消息二进制。
- alerts 首轮新镜像已通过三源目录/权限、真实 count、三轮 firing+Backend replacement 去重、VM/ES 分别停机的 source-local stale 和社交可用检查，但未完成后续恢复门禁。真实有限 count 窗口可在慢恢复期间自然过期，不能强制恢复后依旧 firing；断言调整为清除 degraded，保留 incident/audit 恰好一次的最终要求。
- 初次并行运行资源 verifier 时，一个 verifier 的 baseline 包含另一个随后被清理的本次临时资源，触发 cleanup 检查失败。后续资源门禁改为串行；没有故障注入或删除用户项目。不得把这次 cleanup 记为通过。

## 仍须完成的批次合同

- [ ] 正式 `1.13.6` 当前数据配方 → backup/inspect → 隔离 restore → `1.14.4` 候选迁移与角色/业务/插件/告警/审计事实对照、新写入。
- [ ] 失败项目不 ready，且通过正式 restore 回到验收前快照。
- [ ] `scripts/verify-phase17-state.sh --from-manifest ... --manifest ... --work ...` 候选级统一入口、结构化 receipt、secret/resource/cleanup 闭环。
- [ ] 同一不可变候选的 Rabbit/Kafka/alert 故障组合、社交隔离及完整必要终态证据。
- [ ] 固定 business / marshaller / alerts 和完整 Compose 门禁全部通过，并完成 receipt 复用关系。

已找到 `dist/phase16-06-v3/release-manifest.json`（1.13.6）、前序正式备份及本地 lifecycle/backend digest 制品；原 registry 容器处于停止状态。未启动或修改原 registry 容器。后续创建本次唯一归属 registry，将原数据卷只读挂载以读取源制品；没有把存在的历史 evidence 当成本次升级通过证据。

## 验证范围与偏差

未执行 Kubernetes、跨架构、性能、通用依赖审计或独立代码审查。新增测试只对应本次 Migration、提交/取消、scheduler panic 合同。共享 Compose/指标/日志改动的直接回归原因如上。

本记录如实记录已完成子项，**未将实施计划勾选为完成，也未发布 1.14.4 里程碑**。最终固定门禁结果、提交和剩余限制将在本次执行结束前补充。


## 候选构建与当前检查点

- 生产实现提交：`f674e1e`（`feat: strengthen migration and message state reliability`）。该提交明确包含未完成记录，不是验收完成声明。
- 基于该已提交源码执行 `python3 scripts/ci/release_artifacts.py build --registry 127.0.0.1:15004/gopulse --output dist/phase17-04-candidate` 成功，得到 Linux amd64 `1.14.4` 不可变候选 Bundle。没有 promote 或外部发布。
- 新建 registry project `gopulse-state-06c3c53d82ba`：源 registry 只读挂载历史 registry volume，候选 registry 使用独立存储；原 registry 容器保持停止。
- 候选 manifest：`dist/phase17-04-candidate/release-manifest.json`。构建日志：`/tmp/gopulse-phase17-04-candidate.log`。
- Marshaller 验收脚本捕获的真实 Kafka 记录必须按 `metrics/redis + success` 筛选，不能再假定分区最后一条一定属于 Redis（当前 Monitor 同时发布组件指标）。
- Redis plugin 目标失败响应预算需小于 Monitor scrape timeout，验收配置采用 connect=100ms / scrape=500ms / Monitor=800ms；不修改产品上限或绕过验证。

### 本批变更文件

- `.env.example`
- `VERSION`
- `admin-frontend/package-lock.json`
- `admin-frontend/package.json`
- `backend/cmd/migrate/main.go`
- `backend/cmd/migrate/main_test.go`
- `backend/cmd/server/main.go`
- `backend/internal/alert/count_sources_test.go`
- `backend/internal/alert/scheduler.go`
- `backend/internal/logquery/vocabulary.go`
- `backend/internal/platform/runtime_schema.go`
- `backend/internal/worker/handler.go`
- `backend/internal/worker/handler_test.go`
- `backend/internal/worker/runtime.go`
- `backend/internal/worker/runtime_test.go`
- `backend/migrations/validate.go`
- `backend/migrations/validate_test.go`
- `componentmetrics/catalog.go`
- `componentmetrics/registry_test.go`
- `deploy/compose.yaml`
- `dev/logs/Phase-17/Phase-17-04-Migration与消息处理可靠性闭环.md`
- `dev/logs/Phase-17/evidence/Phase-17-04-migration.json`
- `docs/migration-state.md`
- `frontend/e2e/bookmark.spec.ts`
- `frontend/e2e/delete.spec.ts`
- `frontend/e2e/profile.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `marshaller/internal/consumer/processor.go`
- `marshaller/internal/consumer/processor_test.go`
- `marshaller/internal/logs/validation.go`
- `monitor/internal/logs/logs.go`
- `scripts/ci/testdata/migration-lock.go`
- `scripts/ci/verify_alert_sources.py`
- `scripts/ci/verify_alerts.py`
- `scripts/ci/verify_migration_state.py`
- `scripts/verify-business.sh`
- `scripts/verify-marshaller.sh`

## 收尾门禁检查点

- 最终隔离 MySQL receipt 增加 `apply_failure_keeps_dirty`：测试库本地移除 13 后临时撤销验收账户 CREATE 权限，真实 up 失败得到 code 8、保持 dirty=13，再次 up 拒绝 code 4；表集合不被失败应用修改。恢复权限仅用于本次 disposable fixture 的专用 12 resume 验证，不构成产品 force/回退流程。
- 三源 alerts 串行重跑通过：三源正常/失败/恢复、Backend replacement 去重、停用 evaluator 持久事实不变、窗口自然过期后的同一 incident 恢复、trigger/recover 各一次、安全日志/审计、owned cleanup 和既有资源保留。摘要：`evidence/Phase-17-04-alerts.json`；私有原始记录：`.run/gopulse-p1401-caaa8968fdf7/alert-evidence.json`。
- 正式不可变 `1.13.6` lifecycle image 执行历史正式 archive 的 `backup-inspect` 通过。结果明确限定 authenticated format，不证明数据一致性或 restore readiness；`evidence/Phase-17-04-source-backup-inspect.json` 保留这一限制。
- `scripts/verify-compose.sh` 实际执行失败（exit 1），不是仅 self-test：生产镜像构建、冷启动、migration job、Compose smoke/business 及前序观测场景已进行；`e2e/compose-observability.spec.ts:176` 的 `vm-down` 场景未找到预期 VictoriaMetrics 故障提示。记录为未解决阻断，未降低该断言或宣称完整 Compose 通过。日志：`/tmp/gopulse-phase17-04-compose.log`。
- business 完整门禁发现旧 Rabbit/Redis outage 断言仍要求 Backend `/ready=503`，与已经交付的 social readiness 隔离合同及本计划冲突。只改为要求 200，保留真实社交写入、队列/Outbox/最终通知收敛断言。原失败轮次不能计为通过。
- Marshaller 捕获真实 record 时首次字段误写 `payload.status`，实际为 `payload.scrape_status`；已修正，并保留原始失败记录，不把它记为通过。
- 候选 registry 数据已从独立容器内部打包保存到 `dist/phase17-04-candidate/registry-data.tar`（私有权限 0600），用于后续恢复相同候选 registry；没有改写 manifest/digest。

## 本次执行最终状态：未完成

### 固定门禁结果

| 门禁 | 实际结果 |
| --- | --- |
| Backend 固定包测试及 worker/alert race | 通过 |
| Marshaller consumer 普通/race 测试 | 通过 |
| Router kafka/httpserver 测试 | 通过 |
| 隔离 MySQL 状态、并发、锁、拒绝、真实 apply failure、专用 resume | 通过 |
| `scripts/verify-business.sh` | 最终 exit 0；完整社交/搜索/通知/故障矩阵、日志校验与 cleanup 通过 |
| `scripts/verify-alerts.sh` | 最终 exit 0；三源故障恢复、重复抑制、停用/重启和 cleanup 通过 |
| `scripts/verify-marshaller.sh` | 最终 exit 1；VM outage 的 retry 日志断言未通过 |
| `scripts/verify-compose.sh` | exit 1；`vm-down` 浏览器错误提示断言未通过 |
| `verify-phase17-state.sh` / 前序正式 restore→当前候选 | 统一入口尚未实现，正式升级路径未运行 |
| `validate_versions.py` | 最终通过，元数据一致为当前已完成版本 `1.14.3` |
| `validate_branch.py --branch develop/1.14.4 --base-ref upstream/main` | 最终 exit 1：未完成批次的 target 为 1.14.4，VERSION 保留 1.14.3，不满足完成门禁 |
| `git diff --check` | 通过 |

business 最终日志 `/tmp/gopulse-phase17-04-business-completion.log`；其中旧日志文案曾写 readiness degraded，但执行的实际断言已经是 social readiness=200，后续只修正文案，没有因文案变化重复成功的产品门禁。

Marshaller 最后一次冻结脚本重跑日志 `/tmp/gopulse-phase17-04-marshaller-frozen.log`：真实 Redis 10 families/11 samples、第二 group member 接管、不提交 peer、replacement consumer 从已提交位置重取、三种永久异常提交跳过、真实有效记录继续、Redis unavailable→recovery 已通过；进入 VictoriaMetrics 停机后，脚本未观察到它匹配的 `write_retry` 字样而失败。当前源码将 `write_retry` 放在 event 属性中，而共享 logger 会重新派生 event；这个日志观察合同仍需修复和验证。不能据此称存储失败/恢复及后续 broker/SIGTERM 全部通过。

此前一次运行在 Bash 正在读取自身脚本时被本次编辑打断，出现 EOF 语法错误；静态 `bash -n` 通过后，用不再编辑的冻结脚本完整重跑，以上结果来自冻结轮次，未把被打断的一轮计为通过。

对本次新增容器 cleanup helper 增加了 foreign-label 负例：即使调用方使用 `|| true`，ownership mismatch 也必须显式 return，不能继续 stop/rm。`python3 -m unittest discover -s scripts/ci -p 'test_marshaller_cleanup.py'` 通过；该修复只改变拒绝 foreign resource 的路径。

### 版本与资源

- 根据“VERSION 是当前已完成产品版本；批次完成时才推进”规则，将 bootstrap 提前设置的六处版本元数据恢复为 `1.14.3`，目标分支仍是 `develop/1.14.4`。不能用版本号提前推进掩盖未完成验收。
- 临时只读 source registry 创建后实际 exit 2，未成为可用的源制品入口；正式 backup-inspect 使用的是本地已有 immutable lifecycle image，而非声称源 registry 可用。
- 本次两只 registry 容器已经验证 owner label 后删除，原 `gopulse-p1606-registry-data` 卷仍存在，原 registry 容器未启动或修改。
- 所有本次运行栈均已结束并清理；保留当前候选 Bundle、私有 registry-data archive、私有失败日志与上述 allowlisted evidence，供后续同批次继续。
- 没有推送 Git 分支、创建 PR 或发布/promote 候选。

**仍须完成正式前序升级/恢复事实对照、统一候选级 runner/receipt、Marshaller 完整门禁与 Compose 失败修复，之后才能恢复目标版本 1.14.4 并宣布本批完成。** 当前记录及 checkpoint 的 `complete=false` 是续做依据，不能交付为已验收的 Phase-17-05 输入。
