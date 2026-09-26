# Phase-18-02 Outbox 瓶颈修复与短时容量验证实施记录

## 1. 实际完成

- 按 Phase-18 总方案从最新 `origin/main` 创建 `develop/2.0.2`，读取并执行本批次的直接实现范围。
- 定位 Outbox 调度循环在每个满批次后固定等待 `OUTBOX_POLL_INTERVAL` 的瓶颈；满批次成功后立即继续 claim，空批次、部分批次或错误仍回到轮询间隔。
- 保留现有 lease、fencing、发布确认、幂等状态转换和失败释放路径不变。
- 增加回归测试，验证满批次后的下一次 claim 不等待轮询间隔。
- 增加本批次薄入口和私有 runner：固定 `1 分钟 warmup + 5 分钟 150 RPS steady + 1 分钟 300 RPS burst + 最长 3 分钟 recovery`，复用现有候选绑定、recipe、诊断采样、资源汇总和精确 Compose 清理。
- 增加入口自身的成功/失败窗口门禁自测，并让 `scripts/verify-runtime-contracts.sh` 在无参数时使用根 `VERSION`，以匹配本批固定命令。
- 完成一次不启动产品的宿主预检；没有构建或绑定新的 2.0.2 候选，没有启动正式容量窗口，也没有生成通过摘要。

## 2. 实际改动

- `backend/internal/outbox/dispatcher.go`
- `backend/internal/outbox/dispatcher_test.go`
- `scripts/ci/phase18_02.py`
- `scripts/ci/test_phase18_02.py`
- `scripts/verify-phase18-02.sh`
- `scripts/verify-runtime-contracts.sh`
- `dev/logs/Phase-18/Phase-18-02-Outbox瓶颈修复与短时容量验证.md`

未修改 `VERSION`、候选 manifest 或镜像；本批未满足完成条件。

## 3. 实际执行的命令与结果

批次准备：

```bash
git fetch origin main
git switch -c develop/2.0.2 origin/main
```

结果：分支从 `origin/main=97b8226` 创建成功。

实现和静态检查：

```bash
gofmt -w backend/internal/outbox/dispatcher.go backend/internal/outbox/dispatcher_test.go
go -C backend test ./internal/outbox
go -C backend test ./...
go -C loadtest test ./...
python3 -m py_compile scripts/ci/phase18_02.py scripts/ci/test_phase18_02.py
bash -n scripts/verify-phase18-02.sh scripts/verify-runtime-contracts.sh
scripts/verify-phase18-02.sh --self-test
scripts/verify-phase18-capacity.sh --self-test
git diff --check
```

结果：全部通过；Phase-18-02 自测为 `3 tests OK`，容量基础自测为 `34 tests OK`。

宿主预检：

```bash
PYTHONPATH=scripts/ci python3 scripts/ci/phase18_capacity.py \
  --preflight-only --work .run/phase18-02-host-preflight-20260926
```

结果：失败并停止后续正式运行。宿主事实为 Linux amd64 WSL2、8 vCPU、约 12.68 GiB RAM、16 GiB swap、Docker Server 29.7.2、Compose v5.5.0、无运行中的 Compose project；可用空间为 `95548612608` bytes，低于固定的 `100 GiB` (`107374182400` bytes) 门槛。

因此没有执行候选构建、固定 `verify-phase18-02.sh --preflight-only --manifest ...`、正式负载、运行时合同验收、版本同步或分支完成校验，也没有执行 Docker 全局清理。

## 4. 计划偏差

- 宿主可用磁盘空间未达到批次固定预检门槛；按方案停止，不通过降低门槛、复用旧候选或复用旧证据继续运行。
- 因预检失败，本批没有正式运行次数、产品容量结果或可发布的 Phase-18-02 摘要。
- `VERSION` 仍为 `2.0.1`，因为批次没有成功完成；不得把本次实现提交解释为 2.0.2 发布完成。

## 5. 已知限制与后续项

- Outbox 调度修复已通过直接 Go 测试，但尚未经过本批固定候选上的容量窗口验证。
- 需要先释放或提供至少 `100 GiB` 可用空间，再从本分支的实现 revision 构建新的 2.0.2 候选并重新执行固定预检；不能使用其他 revision 的 manifest、镜像或 raw evidence 拼接结果。
- 本次没有启动 Compose project，因此没有需要清理的本批容器、卷或网络。
