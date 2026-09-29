# Phase-19-02 实施记录

## 已完成工作

- 执行前获取 `upstream`，确认 Phase-19-01 已位于 `upstream/main=5b56a8ca87a76ddc66b7e18d07487103ef245506`；从该基线创建 `develop/2.1.2`，同步完成版本 `2.1.2`。
- 新增严格的容量 profile/schema，绑定 Phase 18 确定性配方、宿主下限、1024 VU、固定业务混合、22 条路由及预期状态、`50/100/150/200 RPS` 阶梯、窗口、三次重复、同步/异步/观测门禁、停止条件、采样信号和统计算法。
- 扩展 Go loadtest 类型、开环 runner 和 workload 合同；每次重复独立输出报告，分离成功、429/503 明确拒绝、超时、传输错误和非预期错误，保留窗口原值和调度滞后。
- 新增独立资源 sampler，分别持久化宿主、负载器、SUT、饱和度和消息积压信号；原始 JSONL 逐条 fsync 并校验序列和采样间隔。
- 新增严格 evidence verifier 和正式编排工具，校验 profile/candidate/recipe、三次原值、恢复窗口、总量、median/min/max/CV、未执行阶梯、资源摘要、归属和清理；失败清理时保留已有原始采样和已完成报告。
- 新增 self-test、bounded calibration 和只读 evidence 验证入口；calibration 只检查到达率、信号形状和安全停止，不写正式容量汇总。
- 更新兼容的历史 load report schema、质量门禁、README、能力状态、容量方法说明和六处版本元数据。

## 实际变更文件

- `.env.example`、`.github/workflows/quality-gates.yml`、`README.md`、`VERSION`。
- `frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`。
- `loadtest/capacity-profile.schema.json`、`loadtest/capacity-profile.json`、`loadtest/report.schema.json`、`loadtest/cmd/load/main.go`。
- `loadtest/internal/load/runner.go`、`loadtest/internal/load/runner_test.go`、`loadtest/internal/load/types.go`、`loadtest/internal/load/types_test.go`、`loadtest/internal/load/workload.go`、`loadtest/internal/load/workload_test.go`、`loadtest/internal/recipe/artifact_test.go`。
- `scripts/ci/phase19_capacity.py`、`scripts/ci/phase19_sampler.py`、`scripts/ci/phase19_evidence.py` 及对应三个 `test_phase19_*.py`。
- `scripts/verify-phase19-capacity.sh`、`scripts/verify-phase19-evidence.py`。
- `docs/capacity-methodology.md`、`docs/capability-status.md`。

## 已执行命令及结果

- `git fetch upstream`：通过；从最新 `upstream/main` 创建 `develop/2.1.2`。
- `gofmt -w`（本批 Go 文件）：通过；`git diff --check`：通过。
- `go -C loadtest test -count=1 ./...`：通过。
- `go -C loadtest test -race -count=1 ./...`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_phase19_*.py'`：通过，12 个测试。
- `python3 -m py_compile scripts/ci/phase19_capacity.py scripts/ci/phase19_sampler.py scripts/ci/phase19_evidence.py`：通过。
- `scripts/verify-phase19-capacity.sh --self-test`：通过。
- `scripts/verify-phase19-capacity.sh --calibration`：通过；输出 `formal=false`、`capacity_status=null`、`writes_formal_summary=false`。
- `python3 scripts/ci/validate_versions.py`：通过，版本元数据与根 `VERSION` 一致。
- `python3 scripts/ci/validate_branch.py --branch develop/2.1.2 --base-ref upstream/main`：通过。
- profile、Phase 19 report 和历史 Phase 18 report 的 JSON Schema 校验：通过。

## 偏差

- 在固定门禁之外增加了 Go race 检查和 JSON Schema 正负边界复核。
- 证据校验额外固定了恢复窗口时长、窗口完成率、报告总量与窗口原值的一致性，并为清理失败但四阶梯已完成的场景保留实际报告；未改变正式容量结论语义。

## 限制和后续项

- 按本批完成条件未执行正式 Docker 三重复容量认证，因此没有发布 `target_met` 或 `boundary_found` 容量结论；正式候选冻结、容量运行和第一瓶颈分析属于 Phase-19-03。
- 正式入口要求显式候选 manifest、私有工作目录、配方 corpus、credentials 和符合 profile 的 Linux/amd64 WSL2、Docker Compose v2 环境；本批只验证了 runner、sampler、统计和 evidence 合同。
