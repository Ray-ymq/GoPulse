# Phase-18-04：独立健康通道与合同单一来源实施方案

> 目标版本：`2.0.4`  
> 开发分支：`develop/2.0.4`  
> 优先级：P1，在 Phase-18-01/02/03 的 P0 容量、扩容、隔离和背压合同成立后开工

## 1. 批次目标

建立不经可观测数据面回传的 lifecycle 健康与 canary receipt，并将 Envelope、日志、指标和 API 错误的手工重复目录收敛到 `contracts/` 机器源。
本批不改变 Phase-18-03 的容量参数或用诊断结果替代产品数据面。

## 2. 前置条件

- Phase-18-01/02/03 已合入主线，固定数据/SLO、多副本、双 ES 和背压 evidence 可复核。
- 从最新 `upstream/main` 创建 `develop/2.0.4`，使用同一 8 vCPU/12 GiB 参考宿主。
- 在改动前盘点并锁定当前 Go/TypeScript/Marshaller/ES 的全部手工合同消费点，不将未使用或内部实现常量扩大为公共合同。

## 3. 独立 lifecycle 健康通道

### 3.1 版本化输出

`status --json`、`doctor --json` 和 `verify --canary --json --receipt <path>` 使用同一 schema：

```json
{
  "schema_version": 1,
  "candidate": {"version": "2.0.4", "revision": "...", "manifest_sha256": "..."},
  "generated_at": "RFC3339 UTC",
  "status": "healthy|degraded|unhealthy",
  "checks": [{"id": "...", "status": "passed|failed|unknown", "reason": "...", "facts": {}}]
}
```

- `facts` 只允许 schema 声明的数值、布尔值、有限枚举和脱敏 digest，禁止 DSN、URL userinfo、token、payload 和宿主私有路径。
- 命令在检查失败时仍输出完整 JSON；参数/候选身份无效与运行检查失败使用不同非零退出码。
- receipt 在同一目录临时写入、fsync 并 rename，不接受 symlink/非归属路径，中断不留下伪完成文件。

### 3.2 检查与 canary

- `status` 从 Docker inventory 和组件 `/startup|/live|/ready` 取得进程与就绪事实，记录副本数、restart/OOM 和受管资源状态。
- `doctor` 另直接检查 MySQL Schema/pool、Redis、Rabbit queue/unacked、Kafka topic/group lag、两个 ES、VM、Outbox 与 Monitor 单所有者，每项限时且限并发。
- canary 生成一个 32 位小写十六进制 ID，通过唯一 edge 写入代表业务事实，并通过正式 Router 入口注入 Metrics/Logs/Events canary。
- lifecycle 直接查询 MySQL/搜索 ES/Rabbit 结果/Kafka lag/VM/观测 ES 确认收敛；无论某个数据面是否可用，结果都写到 stdout/本地 receipt。
- canary 不调整告警、不污染真实用户数据；固定 canary 账户/记录具有安全清理和保留策略。

## 4. `contracts/` 单一来源

### 4.1 手编辑 catalog

- `envelopes.json`：版本、type/source 组合、共享字段、payload schema 引用、大小与时间界限。
- `logs.json`：log schema version、必填/可选字段、类型、长度、枚举、事件与 module/message 允许组合。
- `metrics.json`：producer/source/target、family、kind、unit、labels、允许 tuple 和高基数禁止项。
- `api-errors.json`：稳定 code、HTTP status、安全默认 message、Frontend 文案 key、是否可重试及 `Retry-After`。

四个 catalog 与其 schema 是唯一手工维护的公共合同；字段按固定键排序，schema/catalog version 只能显式递增。

### 4.2 生成物与兼容

- Python 生成器使用仓库内固定逻辑，不访问网络；`generate` 使用原子替换更新，`--check` 只在临时目录生成并比较。
- 生成 Go 枚举/验证表/指标目录、Frontend TypeScript error/validator/catalog、JSON Schema、Marshaller vocabulary 和 Logs/Events ES mapping/template。
- 生成文件带 `Code generated` 标记；手工修改生成物而未更新 catalog 时 `--check` 失败。
- 本阶段保持已有 Envelope/log/API 版本兼容性；不兼容改动必须提升对应 schema version，不以生成器名义偷渡。

### 4.3 漂移与负向门禁

- 连续生成两次的文件 checksum 完全一致，生成后 `git diff --exit-code` 为零。
- 删除/改动一个 Go 常量、TypeScript code、Marshaller 字段、ES mapping 或指标 tuple 时，漂移检查能准确失败。
- 未知字段、未知错误码、非法 type/source、额外 mapping property 和高基数 label 的运行时测试保持严格拒绝。

## 5. 不在本批范围

- 新业务 API、新 Envelope type/source、新指标域或新页面。
- 将诊断 receipt 发送到 Kafka/VM/观测 ES，或以 canary 代替完整产品验收。
- 再次修改已通过的容量、多副本或背压参数，除非本批直接暴露合同漂移缺陷。

## 6. 验收与完成条件

1. 观测面正常、Kafka 停机、VM 停机、观测 ES 停机和三者同时停机时，诊断均在限时内输出完整结构化结果。
2. canary 可区分进程健康、依赖故障和数据链路不收敛，receipt 原子、脱敏且绑定候选。
3. 四个 catalog 生成全部目标，两次生成字节一致，`--check` 和全仓漂移检查通过。
4. 负向 fixture 能覆盖未知字段/错误码/mapping/label，双 Frontend、Go 生产者和 Marshaller 对同一 catalog 解释一致。
5. 重跑 150 RPS 代表负载，证明生成化改造未破坏 SLO、背压和数据收敛。

## 7. 固定验证命令

```bash
python3 scripts/ci/generate_contracts.py --check
scripts/verify-phase18-contracts.sh --self-test
scripts/verify-phase18-contracts.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
python3 scripts/verify-phase18-evidence.py --contracts "$GOPULSE_PHASE18_WORK/evidence/contracts.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.4 --base-ref upstream/main
git diff --check
```

创建同名实施记录，更新 `VERSION=2.0.4`，只提交本批文件后停止。
