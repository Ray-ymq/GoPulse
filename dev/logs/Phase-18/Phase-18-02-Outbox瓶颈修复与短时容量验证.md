# Phase-18-02 Outbox 瓶颈修复与短时容量验证实施记录

## 1. 实际完成

- 按 Phase-18 总方案从最新 `origin/main` 创建 `develop/2.0.2`，读取并执行本批次的直接实现范围。
- 定位 Outbox 调度循环在每个满批次后固定等待 `OUTBOX_POLL_INTERVAL` 的瓶颈；满批次成功后立即继续 claim，空批次、部分批次或错误仍回到轮询间隔。
- 保留现有 lease、fencing、发布确认、幂等状态转换和失败释放路径不变。
- 增加回归测试，验证满批次后的下一次 claim 不等待轮询间隔。
- 增加本批次薄入口和私有 runner：固定 `1 分钟 warmup + 5 分钟 150 RPS steady + 1 分钟 300 RPS burst + 最长 3 分钟 recovery`，复用现有候选绑定、recipe、诊断采样、资源汇总和精确 Compose 清理。
- 增加入口自身的成功/失败窗口门禁自测，并让 `scripts/verify-runtime-contracts.sh` 在无参数时使用根 `VERSION`，以匹配本批固定命令。
- 清理旧的 `.run`、`dist`、Go 构建缓存，以及明确属于历史 Phase 验收的旧镜像、停止容器和 Registry 卷；未执行 Docker 全局 prune。
- 构建过一次 `2.0.2` amd64 候选并完成两次候选预检。第一次正式运行因 Elasticsearch Registry 匿名令牌请求 `EOF` 失败；定向拉取固定镜像成功后，第二次正式运行完成了完整负载窗口，但入口门禁失败。
- 发现并修正入口门禁读取错误：实际 `DiagnosticSampler` 记录使用 backend Outbox 指标，评估器此前只读取缺失的 MySQL status 快照；同时放宽数据库状态查询的时间下界以覆盖容器时区表示。修正后的入口自测为 `4 tests OK`，尚未用新候选重新执行正式窗口。

本批次未完成。未生成可发布的通过摘要，根 `VERSION` 已恢复为当前已完成版本 `2.0.1`。

## 2. 实际改动

- `backend/internal/outbox/dispatcher.go`
- `backend/internal/outbox/dispatcher_test.go`
- `scripts/ci/phase18_02.py`
- `scripts/ci/test_phase18_02.py`
- `scripts/verify-phase18-02.sh`
- `scripts/verify-runtime-contracts.sh`
- `dev/logs/Phase-18/Phase-18-02-Outbox瓶颈修复与短时容量验证.md`
- 候选准备期间临时修改并随后恢复：`VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`、`deploy/runtime-contracts.json`。

当前根版本为 `2.0.1`；失败候选使用的 `2.0.2` 元数据、镜像引用和本地 Registry 已清理，不能作为后续证据复用。

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

结果：实现相关 Go 测试、负载工具测试、Shell/Python 检查和差异检查通过；初始 Phase-18-02 自测为 `3 tests OK`，修正入口后为 `4 tests OK`，容量基础自测为 `34 tests OK`。

宿主预检和清理：

```bash
PYTHONPATH=scripts/ci python3 scripts/ci/phase18_capacity.py \
  --preflight-only --work .run/phase18-02-host-preflight-20260926
```

第一次结果：失败并停止后续正式运行。宿主可用空间为 `95548612608` bytes，低于固定的 `100 GiB` (`107374182400` bytes) 门槛。

实际清理了旧 `.run` 内容（保留当前预检目录）、旧 `dist` 内容和 Go 构建缓存；随后再次预检通过，可用空间为 `107823198208` bytes。之后又精确删除历史候选镜像、历史本地 Registry 引用、6 个停止的历史 Registry 容器和 3 个对应 Registry 卷；没有执行全局 Docker prune。

候选构建和候选预检：

```bash
docker run -d --name gopulse-p1802-registry-20260926 \
  -p 127.0.0.1:15007:5000 \
  registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373
python3 scripts/ci/release_artifacts.py build \
  --registry 127.0.0.1:15007/gopulse --output dist --platform linux/amd64
```

结果：候选构建成功，候选 revision 为 `af3e5d8c783407c8df8875585bfeebfef261ce90`，manifest digest 为 `sha256:31084ebc2ee787523f7ad9e8e5b82da94e8449815cdc2b9320870305b667be6f`，bundle digest 为 `sha256:efe83b125031db0b0793e095be9324c4ccc9339c8aaeceecad77403bbed5a864`。

```bash
export GOPULSE_PHASE18_WORK=.run/phase18-02-af3e5d8c7834
timeout --signal=TERM --kill-after=20s 5m scripts/verify-phase18-02.sh \
  --preflight-only --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
```

结果：通过，候选版本、镜像摘要、宿主资源和 `problems=[]` 均符合要求。

正式运行和定向诊断：

```bash
timeout --signal=TERM --kill-after=20s 20m scripts/verify-phase18-02.sh \
  --manifest dist/release-manifest.json --work .run/phase18-02-af3e5d8c7834
```

结果：第一次正式运行在重启基线 Outbox 配置时失败，私有错误为 Elasticsearch Registry 匿名令牌请求 `EOF`；项目精确清理通过。

```bash
timeout --signal=TERM --kill-after=20s 15m docker pull \
  docker.elastic.co/elasticsearch/elasticsearch@sha256:9c1e1afc2bda921b35025e21c72ec6e392266995aa35ad6a47887363592718be
```

结果：固定摘要拉取成功，显示镜像已是最新；随后第二次候选预检通过。

```bash
timeout --signal=TERM --kill-after=20s 20m scripts/verify-phase18-02.sh \
  --manifest dist/release-manifest.json --work .run/phase18-02-af3e5d8c7834-r2
```

结果：完整负载请求成功（warmup `4500/4500`、steady `45000/45000`、burst `18000/18000`），无超时、错误、OOM，swap 增量为 0；但门禁摘要为失败。第二次摘要记录 `70` 条采样、最大 Outbox pending `213`、最大 oldest age `3.6215s`，评估器将稳态/恢复样本和 Outbox 状态判为不可用，私有错误为 `Phase 18-02 short capacity gates failed`。项目精确清理通过。

工具修正后的局部验证：

```bash
python3 -m py_compile scripts/ci/phase18_02.py scripts/ci/test_phase18_02.py
scripts/verify-phase18-02.sh --self-test
git diff --check
```

结果：`4 tests OK`，差异检查通过。由于批次已达到最多两次正式运行，未用修正后的入口重跑正式窗口。

以下固定完成门未执行：

```bash
go -C backend test ./...
go -C loadtest test ./...
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
```

原因：正式容量门禁未通过，批次完成条件不成立。

## 4. 计划偏差

- 初始宿主磁盘空间不足；通过针对性清理后达到预检要求。
- 第一次正式运行受外部 Elasticsearch Registry 令牌请求瞬时 `EOF` 影响；一次定向拉取诊断成功。
- 第二次正式运行暴露出批次入口评估器与实际采样数据结构不一致，导致门禁失败。已修正入口并通过局部自测，但计划规定最多两次正式运行，因此不能在本批内重新验证。
- 没有发布候选、通过摘要或 `2.0.2` 完成版本；根 `VERSION` 保持 `2.0.1`。

## 5. 已知限制与后续项

- Outbox 调度修复已通过直接 Go 测试，第二次窗口的实际负载请求和资源指标也未显示 OOM、swap 或请求错误；但正式验收证据因入口门禁缺陷无效，不能据此宣布容量条件通过。
- 需要在新的候选 revision 上重新执行候选构建、预检和正式窗口；不能复用 `af3e5d8...` 候选、manifest、镜像或两次正式运行的 raw evidence。
- 失败运行的私有摘要和 raw evidence 保留在 `.run/phase18-02-af3e5d8c7834*`，未加入 Git；当前本批容器、临时 Registry 和候选镜像引用已精确清理。
