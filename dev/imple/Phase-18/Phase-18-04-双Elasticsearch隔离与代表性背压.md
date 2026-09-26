# Phase-18-04：双 Elasticsearch 隔离与代表性背压

> 优先级：**P0**
>
> 目标版本：`2.0.4`
>
> 开发分支：`develop/2.0.4`
>
> 正式运行单次上限：60 分钟
>
> 批次累计上限：120 分钟；最多两次正式运行

## 1. 目标

将业务搜索与 Logs/Events 从共享 Elasticsearch 拆为两个故障域，并用三个代表场景证明系统在超载时
明确拒绝或有界积压、恢复后继续收敛。

## 2. 实施范围

- `search-elasticsearch` 与 `observability-elasticsearch` 使用独立 service、volume、账户、密码、
  健康检查和资源边界；相同 URL 或相同凭据必须启动失败。
- Backend/Search Indexer 只访问业务 ES；Logs/Events/Marshaller 只访问观测 ES。
- 只支持 clean 启动和 `2.0.3` 单 ES 到双 ES 的直接前序迁移；迁移验证前不删除旧数据。
- 代表性背压固定为：HTTP 在途请求饱和、Rabbit main/retry/dead 积压、Kafka/Marshaller sink 积压。
- Outbox 修复结果只做回归，不重复 Phase-18-02 的容量验收。

## 3. 验收条件

1. clean 与直接前序迁移后，各 index 的 document count、alias 和 mapping 与源清单一致；每个数据面
   固定抽查 20 个历史 ID，并写入 20 个新 ID，全部在 60 秒内可查。
2. 每个 ES 停机 2 分钟。期间向未受影响数据面发送固定 100 个请求/事件，预期成功数必须为 100；
   故障 ES 恢复后，本场景已接受数据在 5 分钟内全部可查。
3. HTTP 饱和场景固定发送 1,000 个 request-id；成功数、明确拒绝数与总数闭合，拒绝只允许 `503`、
   `server_overloaded` 和 `Retry-After`，且不产生部分副作用。
4. Rabbit 场景固定发送 1,000 个 event-id；达到上限后的 publish 被明确拒绝并由来源保留，恢复后
   接受数与唯一持久结果闭合，无重复副作用。
5. Kafka/sink 场景固定发送 1,000 个 event-id；lag/in-flight 不超过运行配置的硬上限，Marshaller
   不提前 commit；恢复后 5 分钟内排空本场景积压，接受数与存储数闭合。
6. 无 OOM、挂死、伪成功、无界重试或静默丢失已接受数据。

不增加 Elasticsearch HA、多版本迁移/恢复矩阵或七层逐项故障组合。

第一次正式运行失败后只允许一次最长 15 分钟的定向动态诊断。第二次正式运行失败或累计达到
120 分钟时，本批以 incomplete 结束；样本数、deadline 和阈值不得在批次内调整。

## 4. 固定验证

```bash
go -C backend test ./...
go -C marshaller test ./...
scripts/verify-router.sh
scripts/verify-marshaller.sh
timeout --signal=TERM --kill-after=20s 5m \
  scripts/verify-phase18-04.sh --preflight-only --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
timeout --signal=TERM --kill-after=20s 50m \
  scripts/verify-phase18-04.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.4 --base-ref upstream/main
git diff --check
```

动态入口只编排迁移、两个交叉停机和三个背压场景，不提供可扩展矩阵 DSL。
CI job 须以 60 分钟硬 timeout 覆盖本节全部命令。

## 5. 完成条件

在最多两次正式运行和 120 分钟累计预算内通过，创建同名实施记录与脱敏摘要，记录运行序号与累计耗时，
更新 `VERSION=2.0.4`。大型迁移日志与采样数据只上传 GitHub Actions Artifact。
