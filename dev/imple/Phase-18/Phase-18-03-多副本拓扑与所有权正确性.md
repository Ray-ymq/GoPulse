# Phase-18-03：多副本拓扑与所有权正确性

> 优先级：**P0**
>
> 目标版本：`2.0.3`
>
> 开发分支：`develop/2.0.3`
>
> 完成门禁总时限：60 分钟

## 1. 目标

证明 Backend、Business Worker、Search Indexer、Router 和 Marshaller 能以多副本运行并逐实例替换，
同时保持消息所有权、提交顺序和持久副作用正确。吞吐只记录，不设置共享 8 vCPU 宿主上的扩容倍率门槛。

## 2. 实施范围

- Frontend edge×1、Backend×3、Worker/Indexer/Router/Marshaller×2、Monitor×1。
- Kafka topic 至少 4 partitions；已有 partition 只增不减。
- Backend Outbox/alert 使用 lease 与 fencing；Rabbit consumer 手动 ack；Router broker ack 后成功；
  Marshaller store-before-commit；Monitor 第二实例在启动插件前安全退出。
- 每个可扩容组件只做一个 replacement 场景。场景必须持续产生工作，替换前确认目标与存活实例都在推进。
- 不做三轮容量、`1.3x` 比率、双 ES、背压或最终故障矩阵。

## 3. 验收条件

1. 所有外部流量只经过唯一 edge，固定副本数与 Kafka partitions 可核对。
2. 五个 replacement 场景中，存活实例不中断推进，替换实例重新加入；任务提前耗尽判为场景失败。
3. 没有旧 owner 晚提交、错误 ack/commit、重复通知/索引或已接受消息静默丢失。
4. Monitor 第二实例非零退出且不改变 registry、插件或进程状态。
5. 每组件记录处理量和耗时，但吞吐增益不作为通过条件。

任一组件出现首个产品错误即停止正式场景；只保留定向诊断，不继续跑其余矩阵来堆积证据。

## 4. 固定验证

```bash
go -C backend test ./...
go -C router test ./...
go -C marshaller test ./...
go -C monitor test ./...
timeout --signal=TERM --kill-after=20s 50m \
  scripts/verify-phase18-03.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.3 --base-ref upstream/main
git diff --check
```

动态入口只负责编排五个固定 replacement 场景、输出摘要和强归属清理，不实现通用任务协议。

## 5. 完成条件

全部验收通过，创建同名实施记录与脱敏摘要，更新 `VERSION=2.0.3`。原始日志和逐样本数据只作为
GitHub Actions Artifact 保存。
