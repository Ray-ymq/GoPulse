# Phase 8-02：可靠消费、故障恢复与运维闭环开发记录

## 1. 实施结果

- 实施日期：2026-09-04
- 目标版本：`1.5.2`
- 开发分支：`develop/1.5.2`
- 基线：`origin/main` / `3ba6203`
- 状态：本地实现与固定验收已完成；远程 push checks、Pull Request 和合入状态待提交后确认。

本批在 Phase-08-01 已完成的 generation ownership lease、安全 commit 状态机和真实 Kafka → VictoriaMetrics 指标闭环上，补齐真实 Kafka broker restart/group rejoin、VictoriaMetrics 同进程恢复、明确未提交后的 Marshaller 进程重启重取、确定性重复投递、只读正式 group 检查和强归属清理证据。交付语义继续明确为 at-least-once，Kafka committed offset 是恢复事实，不引入本地成功猜测、spool、跨 record batch 或 exactly-once 声明。

## 2. 前置核对与资源快照

- 开始前已执行 `git fetch --prune origin`，确认 Phase-08-01 squash commit `ea3b910` 已经由 `3ba6203` 合入远程 `main`，根版本为 `1.5.1`。
- 从最新 `origin/main` 创建 `develop/1.5.2`，并确认远端不存在同名开发分支。
- 开始快照时间为 `2026-09-04T18:39:40+08:00`。当时没有运行中的 GoPulse 日常进程或监听端口；存在既有、已停止的 Phase-02-03 隔离容器和日常 `gopulse_*` 命名卷，本批未停止、删除或提交这些资源。
- 用户未跟踪文件 `使用指南.md` 在开始前已存在，本批保持不变且不纳入提交。
- 为执行方案中的固定 branch gate，本地增加了指向同一主远程 URL 的 `upstream` Git remote 并获取 `upstream/main=3ba6203`；该设置不属于项目文件。

## 3. 实际完成内容

### 3.1 Consumer 状态机复核

- 复核了 `marshaller/internal/consumer/ownership.go`、`processor.go`、`kafka.go` 及定向测试。
- Phase-08-01 实现已经满足本批生产状态机约束：assigned generation lease；revoke/lost 立即取消；Writer、退避和 Committer 共享 lease context；HTTP acceptance 后复验 ownership；commit 失败停止 Consumer 推进；单 partition 每次只 poll/处理一个业务 record。
- franz-go 公共 API 文档确认 `OnPartitionsRevoked` / `OnPartitionsLost` 可在 record 处理期间调用；当前实现未启用 `BlockRebalanceOnPoll`。本批真实恢复矩阵未暴露生产阻断缺口，因此没有机械重写 Consumer 或增加生产故障开关。
- Phase-08-01 的确定性测试继续作为延迟 acceptance、revoke/lost、退避取消和 commit failure 精确竞态证据；本批未重复添加相同行为的测试。

### 3.2 真实恢复与重复投递验收

重构并扩展 `scripts/verify-marshaller.sh` 默认模式，在随机 `gopulse-marshaller-<12 hex>` Compose project、随机 loopback 端口、临时 Basic/Bearer 凭据和临时插件根中执行以下场景：

- 真实 Redis success 指标和全部固定查询；代表性永久无效 record 提交后继续。
- Redis target unavailable/recovery 查询。
- 停止 VictoriaMetrics 后观察真实 `write_retry`，再记录 committed offset，证明故障期间新 record 到达但 committed offset 不推进；验证 Marshaller `/health` 保持 `200`、`/ready` 失败，恢复 VM 后同一 PID 恢复 readiness 并提交。
- 再次停止 VictoriaMetrics，在明确存在未提交 record 后终止强归属 Marshaller 进程；恢复 VM 并启动新 PID，证明正式 group 从原 committed offset 重取和提交，无需修改 Topic、group 或 offset。
- 停止并重启真实 Kafka broker，验证同一 Marshaller PID 保持 health、readiness 在故障期间失败、broker 恢复后正式 group 重新获得固定 partition 并恢复 readiness。
- 停止 Monitor 以隔离重复证据，生成一条固定 timestamp/message body 的合法 Envelope 并原样投递两次；两次 offset 均提交，VictoriaMetrics 窄毫秒窗口只返回一个 `gopulse_redis_up` 点。字节级 transformer 确定性继续由现有 `TestTransformDeterministicSortedAndMillisecondTimestamp` 固定。
- 主矩阵前后比较 `sum(vm_rows_invalid_total)`，确认没有增加；继续只把空 `204` 表述为 HTTP transport acceptance。
- 正常、失败和中断清理均通过 PID executable identity、随机 project label 和固定临时路径约束；结束后核对容器、volume、network 和七个 loopback 端口全部清空。

### 3.3 日常只读验证与生命周期边界

- `scripts/verify.sh` 新增对正式 `gopulse-marshaller-metrics-v1` group、固定 Topic/partition 和数值 committed offset 的只读检查。
- VictoriaMetrics 日常查询收紧为带 `source="redis"`、`target_id="redis-exporter-local"` 的固定 `gopulse_redis_up` 查询。
- 复核 `scripts/dev.sh`：等待 Kafka/VM health，执行固定 Topic initializer，依次启动 Router、Marshaller、Monitor/Exporter；失败 cleanup 只停止本次记录且身份匹配的应用进程，保留基础设施。
- 复核 `scripts/down.sh`：按 Frontend → Monitor/Exporter → Marshaller → Router → 其他应用 → 本项目 Compose 的顺序停止，日常命名卷不使用 `--volumes` 删除。
- 上述 `dev.sh` / `down.sh` 已由 Phase-08-01 正确交付且本批未发现缺口，依据方案“不得机械重写”要求未制造无意义修改。
- 现有 Marshaller CI job 已直接运行 `scripts/verify-marshaller.sh`；扩展默认模式后会自动成为真实 broker/VM/process recovery 门禁，无需新增重复 workflow step。

### 3.4 文档与版本

- 更新根 README 与 Marshaller README，记录 committed-offset 恢复事实、health/readiness 行为、broker/VM/process 恢复、确定性重复和 at-least-once 边界。
- 将实施方案状态更新为本地实现与固定验收完成、待远程确认。
- 根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 同步更新为 `1.5.2`。

## 4. 变更文件

- `scripts/verify-marshaller.sh`
- `scripts/verify.sh`
- `marshaller/README.md`
- `README.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `dev/imple/Phase-08/Phase-08-02-可靠消费故障恢复与运维闭环.md`
- `dev/logs/Phase-08/Phase-08-02-可靠消费故障恢复与运维闭环.md`

## 5. 验证命令与结果

以下固定门禁均在最终代码与 `1.5.2` 版本元数据上执行并通过：

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd router && go test -count=1 ./...)
(cd monitor && go test -count=1 ./...)
(cd exporters/redis && go test -count=1 ./...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
git diff --check
```

具体结果：

- `scripts/ci`：25 项 unittest 通过。
- Marshaller 全包普通测试、`go vet` 和 race 测试通过。
- Router、Monitor、Redis Exporter 必要回归通过。
- Compose 渲染、全部 Bash 语法和四个 safety self-test 通过。
- 最终真实 Marshaller acceptance 输出 broker/group recovery、same-process/restart storage recovery、deterministic replay、offset safety、fixed queries、invalid-row stability 和 owned cleanup 全部通过。
- `validate_versions.py` 确认根与 Frontend 均为 `1.5.2`；`validate_branch.py` 确认 `develop/1.5.2` 与 Phase 权威分配一致。

## 6. 实施中的失败与修正

- 第一次扩展矩阵运行在“明确未提交后重启”场景失败。原因是脚本在 VM 容器完全停止并观察到 retry 之前记录 offset，停机边缘已获得 HTTP acceptance 的 record 可以合法提交。修正为先观察真实 `write_retry`，再取 committed offset 作为未提交基线；产品代码未改动。
- 第二次运行已通过 VM 同进程恢复、Marshaller 重启重取和 Kafka broker recovery，但重复 fixture 在 commit 后首次查询尚不可见。修正为对固定毫秒 query_range 使用有界可见性等待，不把 Kafka commit 后首次查询误当成 VictoriaMetrics 同步索引保证。
- 修正临时 Compose VM healthcheck 的 shell/Compose 变量插值，改为在宿主生成临时 Basic header，避免 Compose 把命令替换结果误解析成环境变量。
- 每次失败运行的随机容器、volume、network 和进程均由 trap 清理，并在修正前核对无残留。

## 7. 偏差、限制与后续

- 未修改 Marshaller 生产 Go 代码：现有状态机和测试已满足计划，真实故障矩阵未发现需要修复的阻断问题。这符合实施方案“只修复真实暴露缺口”和“不机械重写”的边界。
- 未修改 `scripts/dev.sh`、`scripts/down.sh`、Compose 或 workflow：复核结果表明启动/停止顺序、volume 保留、强归属和 CI 接线已经满足本批合同；实际增量集中在恢复验收和只读 group 检查。
- 真实 shell 场景证明可观察的 broker stop/start、readiness、offset 和进程重启结果；不声称 shell timing 精确复现延迟 `204` 与 revoke 同时发生的竞态。该竞态继续由注入式 Consumer 单元测试负责。
- 单节点 VictoriaMetrics `1ms` dedup 仅稳定相同 series/timestamp 的确定性重复，不构成 Kafka/HTTP transaction 或 exactly-once。
- 本记录创建时远程 push checks、Pull Request 和合入尚未发生，因此本批尚未满足实施方案第 10 节的远程完成条件；提交后必须查询并补充真实远程状态。
