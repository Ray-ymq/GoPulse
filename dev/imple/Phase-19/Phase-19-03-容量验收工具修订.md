# Phase-19-03：容量验收工具修订

> 目标版本：`2.1.3`
>
> 开发分支：`develop/2.1.3`

## 1. 修订背景

原 Phase-19-03 在正式入口调用前的 deterministic preflight 中确认验收程序无法安全完成正式三重复：

- profile 以 Compose major `2` 为硬条件，而当前受支持环境与实际 runner 只需要满足 Compose 最低版本合同；
- runner 为每轮创建空的独立数据卷，但没有物化固定数据配方；
- 每轮分配不同宿主端口，却向三轮 load process 传递同一个外部 `base-url`；
- 异步恢复与 Metrics/Logs/Events 新鲜度回执由资源 summary 推导或直接填充布尔值，不是逐阶梯原始证据；
- 计划中的 `--manifest/--work` 正式接口与程序实际要求的 profile、base URL、corpus、credentials 参数不一致；
- 在同一批次内既要求先有目标版本候选，又禁止执行完整前更新目标版本，候选冻结顺序不闭合。

该次预检没有调用正式容量入口，没有生成容量结论。记录见
[`顺延前预检记录`](../../logs/Phase-19/Phase-19-03-容量认证预检未完成.md)。

## 2. 目标

修订 Phase 19 容量验收工具，使正式入口能够从一个已完成、已合入主线的 `2.1.3` 候选开始，
自行完成三轮隔离配方物化、逐阶梯负载、真实恢复/新鲜度采样、严格 evidence 和安全清理；在
Phase-19-04 冻结候选前，用 self-test、calibration 和非正式有界 preflight 证明整条验收路径可执行。

本批只修复验收基础设施，不执行正式三重复容量认证，不产生 `target_met` 或 `boundary_found`。

## 3. 固定设计

### 3.1 候选与版本顺序

- 本批完成并合入后，根 `VERSION` 为 `2.1.3`。
- Phase-19-04 从包含本批完成提交的最新 primary remote `main` 创建 `develop/2.1.4`，再构建不可变
  `2.1.3` candidate manifest；候选 revision 必须是该主线提交，不使用未提交版本覆盖。
- Phase-19-04 的 `2.1.4` 表示容量认证与阶段收口批次完成；正式报告必须始终明确其被测候选为
  `2.1.3`，不得声称测试了另一个镜像 digest。

### 3.2 正式入口与私有输入

正式入口保持为：

```text
scripts/verify-phase19-capacity.sh --manifest <2.1.3-manifest> --work <new-owned-directory>
```

- checked-in profile、Compose 和 runtime contract 均由入口从仓库固定位置读取，不接受路径覆盖；
- runner 自行生成每轮私有 corpus/credentials，外部不能传入 base URL、配方、次数、RPS、窗口、阈值或 seed；
- 每轮 base URL 必须由该轮实际端口派生，并在启动 load process 前做候选 identity 与 readiness 检查；
- manifest、profile、Compose、runtime contract、recipe descriptor 和 runner binary digest 在三轮间不可漂移。

### 3.3 每轮配方和生命周期

每轮使用新的强归属 Compose project 和空数据卷，固定顺序为：

```text
依赖与迁移就绪
→ 生成并复核确定性配方
→ 搜索重建及异步初始闭合
→ 恢复基线运行参数
→ 捕获观测基线
→ 执行四阶梯负载与逐阶梯恢复
→ 保存原始 evidence
→ 仅清理本轮归属资源
```

recipe receipt 必须绑定候选、seed、规模、ID 范围和 digest；第二次向非空目标灌入必须安全拒绝。
corpus、credentials、DSN 和环境文件保持 `0600`，不进入发布 evidence。

### 3.4 真实恢复与资源证据

- load runner 以 append-only、逐条 fsync 的进度记录暴露每个 warmup/measurement/recovery 边界；
- sampler 按这些边界保存逐阶梯原始样本，不能用整轮最大值替代某阶梯恢复终值；
- Outbox、RabbitMQ ready/unacked、Kafka lag、通知、搜索投影和 Metrics/Logs/Events 进展必须来自真实查询，
  并保存基线、终值、耗时和原始引用；不得写死 `true` 或零值；
- Backend、MySQL、Redis、RabbitMQ、两个 Elasticsearch、Kafka、VictoriaMetrics 和所有自研进程的
  CPU、RSS、I/O、连接/队列及已定义饱和信号按 profile 要求留存；缺信号属于 `incomplete`；
- evidence verifier 必须从原始报告、进度记录、资源 JSONL 和恢复回执重算门禁，拒绝只改 summary 的替换。

### 3.5 宿主合同

Compose 合同与产品支持边界统一为可解析的最低版本，而不是字符串前缀或固定 major。profile/schema
记录最低版本，preflight 保存实际 client/server 版本并拒绝低于下限、无法解析或不兼容的环境。
CPU、内存、swap、磁盘、平台和无竞争 Compose project 的既有下限不降低。

## 4. 允许变更文件与逐文件验收

| 文件 | 文件级验收条件 |
| --- | --- |
| `loadtest/capacity-profile.json`、`loadtest/capacity-profile.schema.json` | 只修订宿主兼容和真实 evidence 所需合同；RPS、窗口、重复次数、业务比例和能力阈值不变 |
| `loadtest/cmd/load/main.go` | 私有运行参数仍不能覆盖 profile；支持受控进度记录并绑定 `2.1.3` 候选 |
| `loadtest/internal/load/runner.go`、`loadtest/internal/load/runner_test.go` | 四阶梯执行顺序不变；可靠发布窗口边界，取消/失败保留已完成与未执行阶段 |
| `loadtest/internal/load/types.go`、`loadtest/internal/load/types_test.go`、`loadtest/report.schema.json` | 进度、恢复和资源引用类型严格且兼容既有历史报告 |
| `scripts/ci/phase19_capacity.py`、`scripts/ci/test_phase19_capacity.py` | runner 自行完成配方、轮次 endpoint、真实恢复、候选绑定和归属清理；覆盖三轮端口及失败路径 |
| `scripts/ci/phase19_sampler.py`、`scripts/ci/test_phase19_sampler.py` | 样本绑定窗口/阶梯，覆盖规定组件资源与信号，缺失或采样失败不可伪装为零 |
| `scripts/ci/phase19_evidence.py`、`scripts/ci/test_phase19_evidence.py` | 严格校验原始进度、逐阶梯恢复/新鲜度、资源引用、聚合、敏感字段和清理 |
| `scripts/verify-phase19-capacity.sh`、`scripts/verify-phase19-evidence.py` | 与计划固定 CLI 一致；正式模式拒绝隐藏覆盖；preflight 明确 `formal=false` 且不写正式 summary |
| `docs/capacity-methodology.md`、`README.md`、`docs/capability-status.md` | 说明工具修订、候选版本顺序和“尚无正式容量结论” |
| `.github/workflows/quality-gates.yml` | 运行无 Docker 的修订工具 self-test；不在普通 PR 执行正式容量认证 |
| `dev/logs/Phase-19/Phase-19-03-容量验收工具修订.md` | 只记录实际修订、检查、preflight、偏差和限制 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 本批完成时六处一致为 `2.1.3` |

允许在 `scripts/ci/` 下新增仅供 Phase 19 使用的私有配方、进度或恢复辅助模块及对应测试；实施记录必须
列出实际新增文件。不得修改产品业务行为、资源上限、负载模型或容量阈值。

## 5. 验收标准

1. 计划固定的正式命令只接受 manifest 和新 work 目录；所有外部覆盖均被拒绝。
2. 单元测试证明三轮分别使用各自 endpoint、空项目先灌入配方、receipt 绑定候选且非空重灌安全拒绝。
3. 单元/负例证明伪造观测 `true`、整轮最大值冒充恢复终值、缺失组件资源、summary 替换和敏感字段均被拒绝。
4. 非正式有界 preflight 在真实 Compose 中完成“启动、配方、轮次 endpoint、短负载、真实恢复、严格验证、清理”，
   输出 `formal=false`、`capability_status=null`，不消耗 Phase-19-04 唯一正式调用。
5. preflight 前后无非归属资源变化；不执行 Docker global prune，不读取或删除其他项目资源。
6. `50/100/150/200 RPS`、三次重复、窗口、业务比例和门禁值与 Phase-19-02 保持不变。

## 6. 必需回归范围

- Phase 19 profile/schema、load report schema、Go load runner、sampler、orchestrator 和 verifier 的全部测试。
- Phase 18 确定性 recipe artifact/generation 测试，确保复用配方不会改变规模、ID 范围、非空拒绝和私有文件边界。
- 历史 Phase 18 report 与 Phase-19-02 evidence fixture 的兼容读取；不得追溯改写历史 evidence。
- Compose 配置、runtime contract、版本元数据和分支治理检查。
- preflight 只覆盖验收基础设施闭环；产品完整 Compose 回归不因本批没有产品行为修改而重复扩大。

只在观察到具体回归时扩大测试；不得把本批变成一般性能优化、全仓覆盖率或依赖审计。

## 7. 固定验证

```text
go -C loadtest test -count=1 ./...
go -C loadtest test -race -count=1 ./...
python3 -m unittest discover -s scripts/ci -p 'test_phase19_*.py'
python3 -m py_compile scripts/ci/phase19_capacity.py scripts/ci/phase19_sampler.py scripts/ci/phase19_evidence.py
scripts/verify-phase19-capacity.sh --self-test
scripts/verify-phase19-capacity.sh --calibration
scripts/verify-phase19-capacity.sh --preflight --work <new-owned-directory>
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.1.3 --base-ref upstream/main
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
git diff --check
```

preflight 使用独立短时测试合同，不读取或改写正式 profile 的阶梯、窗口、重复和阈值；其产物不能复制为
Phase-19-04 evidence。

## 8. 完成条件

修订代码、负例、self-test、calibration、真实有界 preflight 和清理全部通过；创建同名实施记录，
同步版本 `2.1.3` 并提交。不得执行正式三重复，不得发布容量能力状态。
