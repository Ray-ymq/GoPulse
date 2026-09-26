# Phase-18-06：最终容量与故障恢复验收

> 优先级：P0/P1 收口
>
> 目标版本：`2.0.6`
>
> 开发分支：`develop/2.0.6`
>
> 完成门禁总时限：90 分钟

## 1. 目标

在同一冻结候选上，用一次有界运行确认 Phase 18 的跨边界结果。该批只验收，不重建通用 runner，
也不重放 Phase-18-02～18-05 的完整场景。

## 2. 固定场景与预算

| 场景 | 最长时间 |
| --- | ---: |
| 候选/环境预检与受影响静态检查 | 10 分钟 |
| 150 RPS 稳态 | 30 分钟 |
| 300 RPS 突发与恢复 | 10 分钟 |
| 一个代表性实例替换 | 10 分钟 |
| 六类依赖短故障与恢复 | 20 分钟 |
| 摘要、Secret 扫描和强归属清理 | 10 分钟 |
| 总计 | 90 分钟 |

六类依赖是 MySQL、RabbitMQ、Kafka、业务 ES、观测 ES、VictoriaMetrics。每类只验证一个最短的
失败/恢复路径，不组合故障，不重复历史迁移或所有权矩阵。

## 3. 验收条件

1. 30 分钟稳态达到 150 RPS；读 P95/P99 不高于 500 ms/1.5 s，写不高于 800 ms/2 s，
   非预期错误、超时和连接失败总和不超过 1%。
2. 300 RPS 突发只有预期成功或明确过载拒绝，无 OOM、挂死、伪成功或无界增长。
3. Outbox/Rabbit/Kafka/sink 压力解除后在规定窗口恢复，搜索/通知和观测新数据最终可查。
4. 代表性实例替换期间持续服务，无越权提交、重复持久副作用或静默丢失。
5. 六类依赖故障均能被独立健康通道识别；恢复后不需要人工修数据。
6. 全部结果来自同一 revision、manifest、镜像 digest、配置和参考宿主。

出现首个阻断问题时立即停止；不得跑完其余场景来制造“部分通过”，也不得更换候选后拼接证据。

## 4. 固定验证

```bash
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
python3 scripts/ci/generate_contracts.py --check
timeout --signal=TERM --kill-after=20s 80m \
  scripts/verify-phase18-06.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.6 --base-ref upstream/main
git diff --check
```

正式动态入口每个候选最多运行一次，只输出脱敏摘要并清理当前 Compose project。

## 5. 完成条件

全部验收在 90 分钟内通过，创建同名实施记录和最终容量摘要，更新 `VERSION=2.0.6`，Phase 18 与
Milestone 5 结束。原始日志、采样流和压测附件只上传 GitHub Actions Artifact。
