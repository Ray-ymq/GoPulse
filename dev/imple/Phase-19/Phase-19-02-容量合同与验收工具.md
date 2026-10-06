# Phase-19-02：容量合同与验收工具

> 目标版本：`2.1.2`
>
> 开发分支：`develop/2.1.2`

## 1. 目标

建立 Phase 19 独立容量 profile、runner、资源采样、统计和 evidence verifier。在正式容量候选冻结前，
通过 self-test 与有界 calibration 证明工具能够区分产品边界和验收基础设施错误。

## 2. 实施范围

- 复用 Phase 18 的确定性数据配方和混合业务语义，不修改历史 evidence 或历史脚本结果。
- 新增机器可读 profile/schema，冻结宿主下限、数据规模、虚拟用户、路由比例、阶梯、窗口、重复次数、
  同步/异步/观测门禁、硬停止条件和聚合算法。
- 正式 profile 固定三次独立重复；每次包含 `50/100/150/200 RPS` 阶梯和有界恢复。
- runner 记录每阶梯 P50/P95/P99/max、错误、超时、明确拒绝、调度滞后、SUT/负载器资源和系统积压。
- evidence verifier 严格检查候选/profile/配方/环境绑定、三次原值、聚合、失败阶段和清理。
- calibration 只验证到达率、信号可采集性和安全停止，不产生容量结论，不进入正式汇总。

## 3. 允许变更文件与逐文件验收

| 文件 | 文件级验收条件 |
| --- | --- |
| `loadtest/capacity-profile.schema.json`、`loadtest/capacity-profile.json` | 严格、完整、无隐藏默认值；digest 可绑定 |
| `loadtest/report.schema.json` | 支持阶梯、重复、明确拒绝、异步/观测和资源引用且向后兼容历史报告 |
| `loadtest/cmd/load/main.go` | 只接受已验证 profile，正式参数不能覆盖冻结阈值 |
| `loadtest/internal/load/types.go`、`loadtest/internal/load/types_test.go` | 报告类型覆盖原值和分类，不混淆 429/503、超时与非预期错误 |
| `loadtest/internal/load/runner.go`、`loadtest/internal/load/runner_test.go` | 开环调度、阶梯、窗口、重复和停止条件确定且有测试 |
| `loadtest/internal/load/workload.go`、`loadtest/internal/load/workload_test.go` | 路由比例和预期状态由 profile 校验，业务语义不漂移 |
| `loadtest/internal/recipe/artifact_test.go` | 数据配方身份和容量 profile 引用可复核 |
| `scripts/ci/phase19_capacity.py`、`scripts/ci/test_phase19_capacity.py` | 编排三次独立重复，保留任一重复失败并继续安全可执行部分 |
| `scripts/ci/phase19_sampler.py`、`scripts/ci/test_phase19_sampler.py` | 分离记录负载器/SUT/宿主信号，样本逐条持久化并校验间隔 |
| `scripts/ci/phase19_evidence.py`、`scripts/ci/test_phase19_evidence.py` | 校验 binding、三次原值、median/min/max/CV、未执行阶梯、失败和清理 |
| `scripts/verify-phase19-capacity.sh` | 提供 self-test、calibration 和唯一正式入口；正式模式拒绝次数/阈值覆盖 |
| `scripts/verify-phase19-evidence.py` | 对指定目录做只读严格验证，不修复或补写 evidence |
| `dev/validation/Phase-19/capacity-methodology.md` | 解释负载模型、统计、明确拒绝、容量拐点和能力声明边界 |
| `.github/workflows/quality-gates.yml` | 运行工具 self-test，不在普通 PR 中执行昂贵正式容量认证 |
| `README.md`、`dev/status/capability-status.md` | 记录容量工具已冻结但尚无正式结论 |
| `dev/logs/Phase-19/Phase-19-02-容量合同与验收工具.md` | 记录实际文件、calibration、检查、偏差和限制 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 完成时六处版本一致为 `2.1.2` |

## 4. 验收标准

- profile/schema 能拒绝缺阶梯、重复次数漂移、无界窗口、非法阈值和未绑定数据配方。
- 正式入口不能通过 CLI 改写 profile；只能显式提供候选 manifest 和工作目录。
- runner/self-test 能区分 `target_met`、`boundary_found` 和 `incomplete`，其中 `incomplete` 返回非完成状态。
- 三次原始分位数分别保留；只对同名数值计算 median/min/max/CV，不合并原始延迟样本伪造分位数。
- calibration 不写正式 summary，不消耗正式执行，不被文档描述为容量证据。
- 归属检查和清理只作用于本批随机 Compose project，不执行全局 Docker prune。

## 5. 验证命令

```text
go -C loadtest test -count=1 ./...
python3 -m unittest discover -s scripts/ci -p 'test_phase19_*.py'
python3 -m py_compile scripts/ci/phase19_capacity.py scripts/ci/phase19_sampler.py scripts/ci/phase19_evidence.py
scripts/verify-phase19-capacity.sh --self-test
scripts/verify-phase19-capacity.sh --calibration
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.1.2 --base-ref upstream/main
git diff --check
```

## 6. 完成条件

self-test、calibration、全部固定检查和清理通过；创建同名实施记录并同步版本 `2.1.2`。本批不得执行
正式三重复容量认证，也不得发布容量通过或边界结论。
