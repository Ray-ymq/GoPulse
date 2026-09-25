# Phase-18-05：双 Elasticsearch 隔离与迁移实施方案

> 目标版本：`2.0.5`
> 开发分支：`develop/2.0.5`
> 直接前序：`2.0.4`；历史升级输入：`1.14.5`

## 1. 批次目标

将共享 Elasticsearch 拆为业务搜索和 Logs/Events 两个独立故障域，完成 clean、`1.14.5`、
直接前序迁移以及 format v1/v2 备份恢复。背压策略留给 Phase-18-06。

## 2. 实施范围

- `search-elasticsearch` 与 `observability-elasticsearch` 使用独立 service、volume、账户、密码、
  健康检查和资源边界。
- Backend/Search Indexer/search-init 只访问搜索 ES；Logs/Events 查询和 Marshaller 只访问观测 ES。
- 旧单 ES 键不得作为静默别名；相同 URL 或相同凭据必须启动失败。
- 迁移保留原卷作为业务搜索输入，将 Logs/Events template、alias 和文档复制到观测 ES；
  count、strict mapping、alias 和抽样 ID 通过后才能切换。
- 中断状态原子记录，可幂等续跑；验证前不删除旧数据。
- backup format v2 分别封装两个 ES；restore 兼容 Phase 16/17 format v1。
- 交叉停机只验证故障隔离和恢复，不在本批加入全链路超载。

## 3. 固定 shard 与预算

| Shard | 最大分钟 |
| --- | ---: |
| preflight 与双 ES 合同 | 5 |
| clean init/up/数据分流 | 12 |
| `1.14.5` 单 ES 迁移 | 18 |
| `2.0.4` 直接前序迁移 | 18 |
| format v1 restore | 12 |
| format v2 backup/restore | 12 |
| 搜索 ES/观测 ES 交叉故障 | 8 |
| 聚合、secret scan 与清理 | 5 |
| 主运行器总计 | 90 |

每个 shard 不超过 20 分钟。迁移在 18 分钟内无法完成即失败；不得提高 timeout。

## 4. 不在本批范围

- Elasticsearch 集群 HA、跨宿主副本或新增 Exporter。
- HTTP、消息队列、Kafka 或 sink 背压参数。
- lifecycle canary、合同生成和完整产品最终验收。

## 5. 验收与完成条件

1. clean、`1.14.5` 和 `2.0.4` 输入均形成双 ES，历史数据与恢复后新写入可查。
2. 服务、卷、凭据和资源边界独立，交叉停机不扩散到另一数据面。
3. format v1/v2 restore 通过，中断可幂等继续，无验证前删除。
4. 所有迁移/恢复 shard 均在预算内；主运行器小于 90 分钟。
5. 完成提交更新 `VERSION=2.0.5`。

## 6. 固定验证命令

```bash
scripts/verify-phase18-task.sh --self-test
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-05 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 5400
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.5 --base-ref upstream/main
git diff --check
```

创建同名实施记录并记录每个迁移输入的实际耗时。超过预算时停止并拆分新批次。
