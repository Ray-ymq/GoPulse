# Phase-18-04：双 Elasticsearch 隔离与代表性背压

> 优先级：**P0**
>
> 目标版本：`2.0.4`
>
> 开发分支：`develop/2.0.4`
>
> 完成门禁总时限：60 分钟

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

1. clean 与直接前序迁移后，业务搜索、Logs 和 Events 的历史数据与新写入均可查。
2. 停止观测 ES 不阻断业务搜索；停止业务 ES 不阻断 Logs/Events 接收；恢复后各自收敛。
3. HTTP 饱和返回 `503`、`server_overloaded` 和 `Retry-After`，拒绝请求不产生部分副作用。
4. Rabbit 达上限时 publish 被明确拒绝并由来源保留；解除压力后无重复副作用。
5. Kafka/sink 不可用时 lag/in-flight 有界，Marshaller 不提前 commit；恢复后 5 分钟内排空本场景积压。
6. 无 OOM、挂死、伪成功、无界重试或静默丢失已接受数据。

不增加 Elasticsearch HA、多版本迁移/恢复矩阵或七层逐项故障组合。

## 4. 固定验证

```bash
go -C backend test ./...
go -C marshaller test ./...
scripts/verify-router.sh
scripts/verify-marshaller.sh
timeout --signal=TERM --kill-after=20s 50m \
  scripts/verify-phase18-04.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.4 --base-ref upstream/main
git diff --check
```

动态入口只编排迁移、两个交叉停机和三个背压场景，不提供可扩展矩阵 DSL。

## 5. 完成条件

全部验收通过，创建同名实施记录与脱敏摘要，更新 `VERSION=2.0.4`。大型迁移日志与采样数据只上传
GitHub Actions Artifact。
