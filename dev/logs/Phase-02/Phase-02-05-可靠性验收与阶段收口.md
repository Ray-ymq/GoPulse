# Phase 2-05：可靠性验收与阶段收口开发记录

## 1. 执行基线

- 执行日期：2026-09-02。
- 开发分支：`develop/0.3.5`。
- 目标版本：`0.3.5`。
- 起始基线：主远程 `main` 提交 `f8eed59`（Phase-02-04，PR #37）。
- 执行环境：WSL2/Linux 文件系统中的 `/home/ray/GoPulse`，使用 Bash 生命周期与单一 Docker 环境。
- PowerShell 文件保持 `0.2.1` 冻结基线，未修改、未纳入本批验收入口。

## 2. 完成内容

### 2.1 Bash 生命周期

- `scripts/dev.sh` 同时构建并启动 Backend、Business Worker 和 Frontend；Worker 使用独立二进制 `.run/bin/gopulse-business-worker`、独立进程组和 `.run/business-worker.json` 身份记录。
- Worker 记录包含 cwd、executable、start ticks 和 command marker；已有、陈旧或伪造记录均沿用仓库归属校验模型处理。
- Backend 与 Worker 获得一致的 Outbox、重试和 RabbitMQ 拓扑配置；Worker 启动失败或意外退出会清理本次启动的应用进程。
- `scripts/down.sh` 在 Backend 前校验并有界停止 Worker，保留日常开发命名卷。
- `scripts/verify.sh` 只读检查 Worker record 与 `/proc` 中的实际进程身份，不创建通知、不消费消息，也不修改业务或队列状态。

### 2.2 隔离可靠性验收

- `scripts/verify-business.sh` 在随机 token、独立 Compose project、数据库、端口、进程目录和命名卷中启动 Backend、Business Worker、Frontend、MySQL、Redis 与 RabbitMQ。
- 对容器使用 Compose labels 与持久化 `HostConfig.PortBindings` 校验归属，因此停止状态的 RabbitMQ 也必须先确认身份才能重启或清理；对应用进程在发信号前校验 PID、cwd、executable、start ticks 和 marker。
- 新增 SQL、RabbitMQ management API 与日志的有界轮询辅助，失败时输出有限诊断信息。
- 实际完成封闭故障矩阵：正常双用户通知、Worker 暂停与恢复、RabbitMQ 故障与自动恢复、pending Outbox 下 Backend 重启、unacked delivery 下 Worker 重启、重复 event ID、有限延迟重试与 dead queue、RabbitMQ durable 重启，以及 Phase 1 Redis/Backend 重启回归。
- 保留真实 Playwright 浏览器验收，并在成功后只清理已验证属于本次随机项目的进程、容器、网络和命名卷；验收前后日常开发栈未被改变。

### 2.3 治理、文档与版本

- 扩展 `scripts/ci/test_verify_business.py`，覆盖自检不依赖 Docker、破坏性清理白名单、四个生命周期脚本的 LF/可执行位/Bash 语法、Worker record 安全标记和故障注入前归属校验。
- 复核现有 GitHub Actions integration job 已提供 MySQL、Redis、RabbitMQ 并强制运行带 `integration` tag 的测试；依赖缺失会失败，因此本批无需修改 workflow。
- README 更新统一 Worker 生命周期、只读诊断、可靠性验收入口、至少一次/幂等/重试/死信语义和已知边界。
- 根 `VERSION`、Frontend package 与 lockfile 版本统一更新为 `0.3.5`。
- Phase 2 总实施方案更新前序批次证据，并记录本批本地阶段验收通过、仍待 PR 合入和远程质量门禁的真实状态。

## 3. 变更文件

- `scripts/dev.sh`
- `scripts/down.sh`
- `scripts/verify.sh`
- `scripts/verify-business.sh`
- `scripts/ci/test_verify_business.py`
- `README.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `dev/imple/Phase-02/Phase-02-总实施方案.md`
- `dev/logs/Phase-02/Phase-02-05-可靠性验收与阶段收口.md`

## 4. 实际验证

- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh`：通过。
- `scripts/verify-business.sh --self-test`：通过；接受 1 个合法目标并拒绝 6 个不安全目标，且不访问 Docker。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，17 个治理测试通过。
- `python3 scripts/ci/validate_versions.py`：通过，根版本与 Frontend npm 元数据均为 `0.3.5`。
- `scripts/verify-business.sh`：通过；真实 Chromium 2 条 E2E 用例通过，封闭可靠性矩阵全部通过，验收资源完成隔离清理。
- `git diff --check`：通过。
- `docker ps -a --filter 'name=gopulse-acceptance-' --format '{{.Names}} {{.Status}}'`：无输出，未发现遗留验收容器。
- `git diff --name-only | grep -E '\.ps1$'`：无输出，PowerShell 文件未修改。

## 5. 调试过程与最小修复

完整故障矩阵的失败迭代均围绕实际阻断项进行最小修复，没有扩展矩阵范围：

1. Browser 通知未出现：Backend 与 Worker 的验收重试拓扑默认值不一致；改为向 Backend 转发同一组 Outbox 设置。
2. 已停止 RabbitMQ 无法通过 `docker port` 恢复：改用容器持久化 `HostConfig.PortBindings` 结合 Compose labels 校验所有权。
3. 初版 unacked 场景通过表锁制造阻塞，导致 API 返回 `500`：改为直接注入合成事件。
4. MySQL 驱动读超时使锁场景无法稳定保留 unacked：改为在确认 Worker 归属后对其进程组发送 `SIGSTOP`，注入消息并观察 unacked，再有界终止和重启 Worker。
5. 短 retry TTL 与 management 统计无法稳定观察 retry queue：改为断言 Worker 的安全重试日志和最终通知事实。
6. 合成消息的 payload `occurred_at` 与 AMQP `timestamp` 不一致，触发 `timestamp_mismatch`：发布器改为从 payload 时间生成 AMQP timestamp，使重试、重复与持久消息场景验证真实业务处理路径。
7. 临时唯一键锁保持时间过短时，数据库连接重试可能绕过 Worker retry：使用 15 秒事务锁，稳定证明一次 delay retry 后最终成功。

每次失败后验收 trap 均完成隔离资源清理；最终实现上重新执行了完整验收并通过。

## 6. 与方案的偏差

- 未修改 `.github/workflows/quality-gates.yml`：复核确认现有 integration job 已满足 MySQL、Redis、RabbitMQ 真实依赖和依赖缺失不得 skip 的要求，无需制造无功能变化的 workflow diff。
- 未修改 Backend 或 Frontend 产品代码；本批阻断问题均位于生命周期、验收拓扑或故障注入逻辑，因此没有追加独立 Backend/Frontend/race 全量套件。
- 故障矩阵内部将 Backend pending Outbox、Broker 恢复等相邻步骤合并输出为 `Matrix 3-5/10`，但每项业务断言均实际执行；Phase 1 Redis 与重启基线在矩阵后的固定函数中执行。

## 7. 已知限制与后续项

- RabbitMQ 投递仍是至少一次语义；MySQL notification 唯一约束负责最终幂等，不承诺 exactly-once。
- 当前是单节点本地 RabbitMQ 验收，不包含生产集群、多节点高可用、网络分区或规模压测。
- retry 次数和延迟有限；dead queue 支持检查与诊断，但自动重放和运营后台不属于 Phase 2。
- Broker 或 Worker 故障会延迟通知物化，但不回滚已提交的评论与首次点赞；MySQL 继续是核心事实和完成通知的权威来源。
- Phase-02-05 已在本地完成并通过阶段验收；Phase 2 仓库里程碑仍需本分支合入主远程 `main`，并取得本批远程质量门禁通过证据后才能最终完成。
