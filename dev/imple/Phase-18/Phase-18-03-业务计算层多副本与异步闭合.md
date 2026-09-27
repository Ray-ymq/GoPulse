# Phase-18-03：业务计算层多副本与异步闭合

> 目标版本：`2.0.3`
>
> 开发分支：`develop/2.0.3`
>
> 最终验收次数：每个验收单元固定 `2` 次（首次 + 唯一一次重复）。

## 1. 目标

让 Backend、Business Worker 和 Search Indexer 以至少两个副本共同工作，并留下跨副本请求、
Outbox、RabbitMQ、通知、告警和搜索最终闭合的完整证据。本批判断边界，不以所有检查通过作为
版本更新条件。

## 2. 实施范围

- 单一入口将请求实际分配到至少两个 Backend；JWT/Cookie 会话不依赖粘性路由。
- MySQL 连接池、HTTP 并发、Worker/Indexer prefetch 与关闭预算具有每副本和整套候选上界。
- Outbox owner/lease/fencing 与满批连续 claim 支持多 Backend dispatcher。
- 告警调度租约在多 Backend 下仍保持单条规则单 owner；不重复生成持久事实。
- Worker/Indexer 使用唯一 consumer identity，副本退出时完成或安全重投当前消息。
- 每个实例具备非敏感、有界的实例身份，可进入日志、指标和 evidence，但不参与授权。
- 验收能关联 accepted event、Outbox 状态、RabbitMQ delivery、通知/搜索投影和最终闭合。

## 3. 允许变更文件与逐文件验收

下表是开发分支允许变更的产品/验收文件清单。某行最终不需要修改时，实施记录标记
`not_needed`；需要增加清单外文件时，必须先在 `update` 修订本文。

| 文件 | 文件级验收条件 |
| --- | --- |
| `.env.example` | 新增副本、连接或实例身份配置具有安全默认值、范围说明且不含真实凭据 |
| `deploy/compose.yaml` | 两个 Backend、Worker、Indexer 可被唯一识别并共享正确网络/Secret；Frontend 是唯一宿主机入口，Backend 仅由 Frontend upstream 访问且不发布宿主端口 |
| `deploy/docker/frontend/nginx.conf` | 单一入口可把请求分配给两个 Backend，失败实例不会要求用户重新登录或暴露内部地址 |
| `.github/workflows/quality-gates.yml` | Compose 发布检查与单一 Frontend 入口一致：恰有一个 `127.0.0.1` 宿主机绑定，并继续校验镜像版本、内部网络和迁移依赖 |
| `scripts/verify-compose-observability.sh` | 权威全栈闭合检查只要求 Frontend 绑定一次 IPv4 loopback，并明确拒绝 Backend 发布宿主端口；其他内部服务继续禁止发布端口 |
| `deploy/runtime-contracts.json`、`docs/runtime-contracts.md` | 新配置、实例身份和副本角色与实际进程/Compose 一致，不提前声明未交付能力 |
| `backend/internal/config/config.go` | 新配置强类型解析、上下界和交叉预算验证完整，错误只包含 key |
| `backend/internal/config/config_test.go` | 覆盖默认值、边界、非法值和总连接预算负例 |
| `backend/internal/config/worker.go`、`backend/internal/config/search_indexer.go` | Worker/Indexer 使用与副本预算一致的 MySQL 连接池配置，不回退到进程内硬编码放大 |
| `backend/internal/platform/mysql.go` | 连接池不再硬编码放大；生命周期和 timeout 语义保持 |
| `backend/internal/platform/platform_test.go` | 证明连接池配置被准确应用且非法预算不能启动 |
| `backend/cmd/server/main.go` | 每个副本使用独立实例身份；Outbox/告警后台职责的 owner 与关闭顺序明确 |
| `backend/cmd/server/main_test.go` | 覆盖多副本配置装配、readiness 撤销和后台任务有界退出 |
| `backend/internal/outbox/dispatcher.go`、`backend/internal/outbox/repository.go` | 多 owner 下 claim/lease/fencing、确认后完成和满批连续 claim 不退化 |
| `backend/internal/outbox/dispatcher_test.go`、`backend/internal/outbox/repository_test.go`、`backend/internal/outbox/integration_test.go` | 覆盖两个 owner 竞争、租约失效、重复确认和最终零 pending/leased |
| `backend/internal/alert/scheduler.go`、`backend/internal/alert/repository.go` | 两个调度器竞争同一规则时只有当前 lease owner 能应用结果 |
| `backend/internal/alert/repository_test.go` | 覆盖租约竞争、过期 owner、重复 incident/审计负例 |
| `backend/internal/worker/runtime.go`、`backend/internal/worker/profile.go` | consumer identity 唯一；prefetch、ack/requeue、重连和退出保持有界 |
| `backend/internal/worker/runtime_test.go`、`backend/internal/worker/integration_test.go` | 两个 consumer 都处理消息；退出/重连后无永久丢失或越权 ack |
| `componentmetrics/config.go`、`componentmetrics/catalog.go` | 实例身份与新增容量指标词汇固定、有限且不含随机高基数标签 |
| `componentmetrics/logging.go` | 公共结构化日志携带有界实例身份且保留保留字段冲突保护 |
| `componentmetrics/runtime_test.go`、`componentmetrics/registry_test.go` | 验证实例标签、目录容量和重复/未知标签拒绝 |
| `monitor/internal/metrics/collector/components.go` | 采集不会因 DNS 随机选择而遗漏业务计算副本 |
| `monitor/internal/metrics/collector/components_test.go` | 两个同类实例均被采集且各自身份可区分 |
| `scripts/verify-phase18-business-scale.sh` | 正式模式只接受固定 `--repetitions 2`；run-1 失败仍保存/清理并继续 run-2，不执行全局 prune |
| `scripts/ci/phase18_business_scale.py` | 绑定同一候选/条件，写 `run-1`、`run-2` 和平均摘要，禁止第三次运行 |
| `scripts/ci/test_phase18_business_scale.py` | 覆盖次数拒绝、平均计算、失败保留、证据不可覆盖和候选不一致拒绝 |
| `frontend/e2e/compose-business.spec.ts` | 故障场景的异步业务写入在场景退出前完成，避免验收时序掩盖 Outbox/RabbitMQ 恢复结果 |
| `backend/README.md`、`README.md` | 只记录实际多副本配置、运行入口、结果类型和仍未证明的状态层边界 |
| `dev/logs/Phase-18/Phase-18-03-业务计算层多副本与异步闭合.md` | 只记录实际修改、两次命令/结果、平均值、偏差、限制和结果类型 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 六处产品版本均为 `2.0.3`，由同步/校验脚本证明一致 |

## 4. 固定最终验收单元

正式 runner 以一次 `--repetitions 2` 调用创建 `run-1` 和 `run-2`。以下每一行都是 runner
在两个 run 中各执行一次的验收单元，不能额外单独补跑：

| 单元 | 验证内容 | 合并方式 |
| --- | --- | --- |
| U1 | 直接 Go 测试：config、platform、outbox、alert、worker、componentmetrics、collector | 记录 `0/2`～`2/2` 和两次退出码 |
| U2 | runner/self-test 的安全、次数和 evidence 负例 | 记录 `0/2`～`2/2` |
| U3 | 冻结 `2.0.3` 候选的真实多副本业务矩阵 | 数值取两次算术平均，确定性项记 `0/2`～`2/2` |
| U4 | runtime contract、版本、分支和 `git diff --check` | 各命令记录 `0/2`～`2/2` |

U3 两次都使用全新但同配方的强归属项目，依次覆盖正常并发请求、停一个 Backend、停一个
Worker、停一个 Indexer、RabbitMQ 短故障、搜索 ES 短故障、恢复和最终闭合。

## 5. 完成条件

1. 文件清单逐行有 `changed`、`not_needed` 或已先行批准的 `deviation` 记录。
2. 正式 runner 只调用一次且参数固定为 `--repetitions 2`；U1～U4 在两个 run 中各出现一次，
   `run-1`、`run-2`、binding、原始输出和 summary 完整。
3. 两次原值和平均值如实记录；成功、失败或分歧都不触发第三次运行。
4. 结果归类为 `target_met`、`boundary_found` 或 `execution_failed`。
5. 无论属于哪一类，创建实施记录、同步 `VERSION=2.0.3`、只提交本批文件并停止。

版本更新不允许被描述为多副本能力已经通过；能力声明只依据两次 evidence。
