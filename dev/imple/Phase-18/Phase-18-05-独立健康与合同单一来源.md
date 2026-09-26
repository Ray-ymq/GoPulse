# Phase-18-05：独立健康与合同单一来源

> 优先级：**P1**
>
> 目标版本：`2.0.5`
>
> 开发分支：`develop/2.0.5`
>
> 正式运行单次上限：60 分钟
>
> 批次累计上限：120 分钟；最多两次正式运行

## 1. 目标

提供不依赖 Kafka、VictoriaMetrics 或观测 Elasticsearch 自证的基础健康通道，并将 Envelope、日志、
指标和 API error 收敛到机器可读的单一来源。

## 2. 实施范围

- lifecycle `status/doctor/canary --json` 直接检查容器/Probe 和 MySQL、RabbitMQ、Kafka、双 ES、
  VictoriaMetrics；输出可落盘且不经被测观测链路回传。
- 只保留两个动态 canary：全部正常，以及 Kafka、VictoriaMetrics、观测 ES 同时不可用；
  不再拆分单依赖故障，不新增常驻 watchdog。
- `contracts/` 下的 Envelope、日志、指标和 API error catalog 是唯一手编辑来源。
- 生成 Go、TypeScript、JSON Schema、Marshaller vocabulary 和 Elasticsearch mapping；生成器离线、确定性，
  `--check` 发现漂移即失败。
- 不借合同收敛新增业务 API、指标域、页面或通用重构。

## 3. 验收条件

1. `status/doctor/canary --json` 使用同一版本化结果结构，单次调用硬上限 120 秒。
2. 每次正式运行只执行一次正常 canary 和一次三依赖同时不可用 canary；后者必须在 120 秒内准确输出
   Kafka、VictoriaMetrics、观测 ES 三个固定 dependency-id 与 failed 状态，同时 stdout 和本地脱敏结果可用。
3. 两个 canary 分别使用固定 1 个业务事实和 1 组 Metrics/Logs/Events event-id；接受数与最终结果闭合，
   结束后清理，不泄漏 DSN、token、payload 或私有路径。
4. catalog 连续生成两次 checksum 一致；生成后仓库零差异。
5. 首次正式运行前冻结四个负向 fixture：未知字段、错误码、mapping 和指标 label 各一个；全部必须失败。

第一次正式运行失败后只允许一次最长 15 分钟的定向动态诊断。第二次正式运行失败或累计达到
120 分钟时，本批以 incomplete 结束；不得增加 canary、fixture 或修改 deadline 后继续。

## 4. 固定验证

```bash
go -C lifecycle test ./...
python3 scripts/ci/generate_contracts.py --check
timeout --signal=TERM --kill-after=20s 5m \
  scripts/verify-phase18-05.sh --preflight-only --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
timeout --signal=TERM --kill-after=20s 40m \
  scripts/verify-phase18-05.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.5 --base-ref upstream/main
git diff --check
```

动态入口只执行两个 canary；合同验证以静态生成和最低层消费方测试为主。
CI job 须以 60 分钟硬 timeout 覆盖本节全部命令。

## 5. 完成条件

在最多两次正式运行和 120 分钟累计预算内通过，创建同名实施记录与脱敏摘要，记录运行序号与累计耗时，
更新 `VERSION=2.0.5`。原始诊断日志只上传 GitHub Actions Artifact。
