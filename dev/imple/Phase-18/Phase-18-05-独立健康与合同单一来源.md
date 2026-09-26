# Phase-18-05：独立健康与合同单一来源

> 优先级：**P1**
>
> 目标版本：`2.0.5`
>
> 开发分支：`develop/2.0.5`
>
> 完成门禁总时限：60 分钟

## 1. 目标

提供不依赖 Kafka、VictoriaMetrics 或观测 Elasticsearch 自证的基础健康通道，并将 Envelope、日志、
指标和 API error 收敛到机器可读的单一来源。

## 2. 实施范围

- lifecycle `status/doctor/canary --json` 直接检查容器/Probe 和 MySQL、RabbitMQ、Kafka、双 ES、
  VictoriaMetrics；输出可落盘且不经被测观测链路回传。
- 只保留两个动态 canary：全部正常、观测数据面不可用；不新增常驻 watchdog。
- `contracts/` 下的 Envelope、日志、指标和 API error catalog 是唯一手编辑来源。
- 生成 Go、TypeScript、JSON Schema、Marshaller vocabulary 和 Elasticsearch mapping；生成器离线、确定性，
  `--check` 发现漂移即失败。
- 不借合同收敛新增业务 API、指标域、页面或通用重构。

## 3. 验收条件

1. `status/doctor/canary --json` 使用同一版本化结果结构，单次调用默认不超过 120 秒。
2. 观测数据面不可用时，stdout 和本地脱敏结果仍可用并准确指出故障依赖。
3. canary 使用强归属测试数据，结束后可清理，不泄漏 DSN、token、payload 或私有路径。
4. catalog 连续生成两次 checksum 一致；生成后仓库零差异。
5. 未知字段、错误码、mapping 或指标 label 的代表性负向 fixture 会失败。

## 4. 固定验证

```bash
go -C lifecycle test ./...
python3 scripts/ci/generate_contracts.py --check
timeout --signal=TERM --kill-after=20s 40m \
  scripts/verify-phase18-05.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.5 --base-ref upstream/main
git diff --check
```

动态入口只执行两个 canary；合同验证以静态生成和最低层消费方测试为主。

## 5. 完成条件

全部验收通过，创建同名实施记录与脱敏摘要，更新 `VERSION=2.0.5`。原始诊断日志只上传
GitHub Actions Artifact。
