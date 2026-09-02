# Phase 2-05：可靠性验收与阶段收口实施方案

> 执行序号：5 / 5  
> 前置批次：Phase 2-01 至 Phase 2-04 已完成并通过验收  
> 总方案来源：[Phase-02-总实施方案.md](Phase-02-总实施方案.md)

## 1. 批次目标

把 Business Worker 纳入 WSL2/Bash 日常生命周期与隔离验收，完成消费者暂停/恢复、RabbitMQ 故障/恢复、重复投递、重试/死信和进程重启的真实故障矩阵，并在证据充分时完成 Phase 2 阶段收口。

本批以验证和工程闭环为主，不扩大业务范围；发现阻断缺陷时在所属模块最小修复，并重跑受影响验收。

## 2. 前置条件

- Phase-02-04 已合入远程 `main`，根版本与前一批目标一致。
- Outbox Producer、Dispatcher、Worker、notifications、API 和 Frontend 通知页均有各批自动化证据。
- 当前 Bash lifecycle、隔离验收白名单和资源归属保护仍通过。
- 已从远程最新 `main` 创建本批权威开发分支，用户工作树改动已安全隔离。

## 3. 实施范围

### 3.1 日常 Bash 生命周期

- `scripts/dev.sh` 构建并启动 Backend、Business Worker 和 Frontend，Worker 使用独立二进制、进程组和 `.run` 身份记录。
- Worker 启动失败必须触发本次启动的应用进程清理；不得停止已有且不属于本次会话的进程。
- `scripts/down.sh` 以 cwd、executable、start ticks 和 command marker 校验 Worker 归属后有界停止，并清理陈旧记录。
- 日常 `verify.sh` 保持只读，可检查 Worker 进程身份/存活和现有 HTTP/readiness，不创建通知或修改队列。
- PowerShell 文件保持冻结，不新增对等 Worker 支持。

### 3.2 隔离完整验收

扩展 `scripts/verify-business.sh`，继续使用随机 token、独立 Compose project、数据库、端口、进程目录和命名卷，并在任何停止/重启/删除前校验资源归属。

故障矩阵固定覆盖以下十项：

1. 正常两用户注册、发帖、评论、首次点赞和通知 UI。
2. 停止 Worker 后继续产生核心事实；队列保留消息，恢复 Worker 后通知各生成一次。
3. 停止 RabbitMQ 后继续评论/首次点赞；API 成功、MySQL 事实和 pending Outbox 存在，`/ready` 明确降级。
4. 恢复 RabbitMQ 后 Dispatcher 和 Worker 无需客户端重试自动补全通知。
5. Backend 在 pending/leased Outbox 存在时重启，租约到期后继续投递。
6. Worker 在 unacked delivery 存在时重启，消息重新投递且通知不重复。
7. 注入相同 event ID 的重复消息，唯一约束确保一条通知。
8. 可恢复处理错误进入 delay retry 并最终成功；超过上限或永久坏消息进入 dead queue，后续合法消息继续处理。
9. RabbitMQ 容器重启后 durable topology、持久消息和命名卷数据符合预期。
10. Redis 清空/停止/恢复和 Backend 重启的 Phase 1 基线继续通过。

以上十项是本批封闭故障矩阵。除非其中某项真实失败暴露新的 P0/P1 风险，不新增排列组合、重复时序、额外依赖故障或压力场景；同一最终构建上每项只执行一次。

### 3.3 CI 与文档收口

- 为 Bash 语法、LF、可执行位、Worker record 安全逻辑和验收 target 白名单增加自动化测试。
- GitHub Actions integration job 提供真实 MySQL、Redis 和 RabbitMQ，运行 Producer/Worker integration；依赖缺失时必须失败，不得 skip 假绿。
- 全量更新 README 的进程、配置、队列语义、命令、通知 API/UI、故障边界和已知限制。
- 核对五份 Phase 2 实施记录、提交范围和版本分配。
- 只有完整阶段验收通过后，才把 Phase 2 总方案批次状态和阶段结论更新为真实完成状态。

## 4. 实施边界与非目标

- 不增加新的业务事件、通知类型或公共功能。
- 不以 RabbitMQ management API 的手工观察替代应用级断言；管理 API 只可辅助确认队列状态。
- 不把 dead queue 自动重放或运营后台纳入本阶段；只要求可检查、可诊断和不阻塞。
- 不执行生产规模压测、RabbitMQ 集群容灾或多节点高可用验收。
- 不修改 PowerShell、Kafka、Elasticsearch、Kubernetes 或后续 Phase 文件，除非只为纠正文档直接冲突且有明确依据。

## 5. 目标文件与交付物

预计涉及：

```text
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-business.sh
scripts/ci/
.github/workflows/quality-gates.yml
backend/**（仅阻断缺陷的最小修复和测试）
frontend/**（仅阻断缺陷的最小修复和测试）
.env.example
README.md
dev/imple/Phase-02/Phase-02-总实施方案.md
dev/logs/Phase-02/Phase-02-05-可靠性验收与阶段收口.md
VERSION
frontend/package.json
frontend/package-lock.json
```

## 6. 详细实施步骤

1. 核对前四批实施记录、未关闭限制和远程质量门禁。
2. 扩展 `dev.sh` 的 Worker build/start/record/cleanup，保持已有锁与归属安全模型。
3. 扩展 `down.sh` 和只读 `verify.sh` 的 Worker 管理与诊断。
4. 为 Worker 生命周期的正常启停增加正向测试，并从陈旧/伪造 record、启动失败和中断清理中按实际改动选择最低数量的代表性负向测试，不复制 Backend 已有同构边界的全部排列。
5. 扩展 `verify-business.sh` 的隔离 Worker 进程和应用级通知断言。
6. 为 RabbitMQ/Worker/MySQL 故障注入增加 project/container/port/PID 多重归属校验。
7. 顺序执行本方案封闭故障矩阵一次；每个故障恢复后先确认系统回到可验收状态。
8. 扩展 CI integration 和 Bash/Compose 检查，确保不修改 PowerShell 基线。
9. 运行脚本治理和阶段级隔离验收；Backend/Frontend 只对本批实际修复的 package 或场景做定向回归，不重复前两批已通过的全量套件。
10. 检查验收成功/失败/中断后无进程、容器、网络或 volume 泄漏，日常开发栈前后不变。
11. 修复范围内阻断问题并只重跑可能受影响的验收，不无限重复已经稳定通过的检查。
12. 更新 README、本批实施记录、总方案状态和最终版本，完成阶段提交。

## 7. 风险与控制

- **故障注入误伤用户资源**：延续随机白名单、label/container/port/PID/路径归属校验，任何不匹配在破坏性操作前失败。
- **进程记录误杀**：Worker 使用与 Backend 同等级别的 executable、cwd、start ticks、marker 验证。
- **异步断言偶发**：使用有界轮询和明确超时，超时输出 event/queue 状态摘要但不泄漏凭据。
- **验收掩盖重复**：既断言最终通知存在，也断言相同 source event 计数严格为 1。
- **无限扩大回归**：按总方案验收完成即停止；非阻断改进登记后续项。

## 8. 验证命令与必要回归

本节是阶段收口的固定完成清单，不是追加测试的起点。最终 diff 上每项执行一次；某项失败后只重跑受修复影响的命令或矩阵项。上下文压缩不触发从头复验。

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
scripts/verify-business.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
scripts/verify-business.sh
git diff --check
```

`scripts/verify-business.sh` 是一次性阶段级 Backend/Frontend/真实基础设施回归入口，禁止再以“更稳妥”为由叠加独立全量 Backend、race、integration、Frontend 或 Playwright 套件。若本批为阻断缺陷修改应用代码，只补跑对应 package/场景的定向命令并记录风险依据。

完整验收只在 WSL2 Linux filesystem 和可确认的隔离资源上执行。若环境无法满足，不得把阶段标记完成或把未执行命令写成通过，也不得用阅读更多源码或补 mock 边界测试代替缺失的环境证据。

## 9. 验收标准

- `dev.sh` 一次启动四类应用/基础组件，Worker 有独立可验证记录；失败和 Ctrl+C 清理行为正确。
- `down.sh` 只停止归属当前仓库的进程并保留日常命名卷；`verify.sh` 保持只读。
- 正常通知链路、消费者暂停/恢复、Broker 停止/恢复、Backend/Worker 重启、重复投递、有限重试和死信矩阵全部通过。
- Broker 停止期间评论/首次点赞的 MySQL 事实与 Outbox 提交成功，恢复后通知自动补齐。
- 队列/进程恢复不会产生重复通知，毒消息不会阻塞合法消息。
- `scripts/verify-business.sh` 中既定的 Phase 1 核心业务、Redis 降级、认证恢复和浏览器交互无回归；不另跑重复套件。
- CI 对 MySQL/Redis/RabbitMQ integration 强制执行，Bash 安全测试通过。
- 验收未改变用户 `.env`、日常数据库/Redis/RabbitMQ 数据或非本次 Compose/进程资源。
- PowerShell 文件与 `0.2.1` 冻结基线保持不变。
- 总方案五批状态、实施记录、Git 历史、根 `VERSION=0.3.5` 和 Frontend npm 元数据一致。

## 10. 明确完成条件

只有第 9 节全部通过、五份实施记录真实完整、无 P0/P1 阻塞且目标版本已提交，Phase-02-05 和 Phase 2 才可同时标记完成。任何故障矩阵、CI、提交或远程合并证据缺失时，保持“未完成”并记录具体阻塞，不以局部成功代替阶段收口。

## 11. 后续交接

向 Phase 3 提供：

- 已验证的 Transactional Outbox、可靠 AMQP 投递和独立 Consumer 运行经验。
- 可持续产生真实异步业务流量的通知链路。
- 明确的至少一次、幂等、重试/死信和恢复边界。
- Phase 3 仍需单独设计 Elasticsearch 索引、搜索 API 和 MySQL 重建策略，不直接复用通知队列承担搜索事实。
