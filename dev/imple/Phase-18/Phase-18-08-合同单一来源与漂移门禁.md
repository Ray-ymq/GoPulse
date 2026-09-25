# Phase-18-08：合同单一来源与漂移门禁实施方案

> 目标版本：`2.0.8`
> 开发分支：`develop/2.0.8`
> 批次边界：公共合同生成与验证，不新增业务能力

## 1. 批次目标

将 Envelope、日志、指标和 API error 的手工重复定义收敛到 `contracts/` 机器源，
以确定性生成器和负向漂移测试取代跨语言人工同步。

## 2. 实施范围

- `envelopes.json`：版本、type/source、共享字段、payload schema、大小与时间边界。
- `logs.json`：schema version、字段类型/长度/枚举和 module/message 组合。
- `metrics.json`：producer/source/target、family、kind、unit、labels 和高基数禁止项。
- `api-errors.json`：code、HTTP status、安全默认消息、Frontend 文案 key、重试和 `Retry-After`。
- Python 生成器不访问网络，原子更新；`--check` 在临时目录生成并比较。
- 生成 Go 枚举/验证表、Frontend TypeScript、JSON Schema、Marshaller vocabulary 和双 ES mapping。
- 生成两次字节一致；未知字段/错误码/mapping/label 的负向 fixture 必须失败。
- 本阶段保持已发布版本兼容；不兼容变更必须显式提升相应 schema version。

## 3. 固定 shard 与预算

| Shard | 最大分钟 |
| --- | ---: |
| catalog/schema 校验 | 8 |
| 两次确定性生成 | 12 |
| 漂移与负向 fixture | 8 |
| Go/TypeScript/Marshaller 消费方测试 | 12 |
| 代表性 runtime contract smoke | 10 |
| 聚合与清理 | 5 |
| 主运行器总计 | 55 |

静态编译和分支门禁最多 20 分钟，批次串行总预算不超过 75 分钟。

## 4. 不在本批范围

- 新 Envelope type/source、新指标域、新 API 或 UI。
- 容量、扩容、迁移、背压参数或故障矩阵。
- 以合同生成名义重构未消费的内部常量。

## 5. 验收与完成条件

1. 四个 catalog 是唯一手编辑来源，schema 与版本规则明确。
2. 连续生成两次 checksum 相同，生成后仓库零差异。
3. Go、两个 Frontend、Marshaller 和 Elasticsearch 对同一合同解释一致。
4. 每类负向漂移都能准确失败，未知项不宽松回退。
5. 主运行器小于 60 分钟，全部门禁小于 120 分钟。
6. 完成提交更新 `VERSION=2.0.8`。

## 6. 固定验证命令

```bash
python3 scripts/ci/generate_contracts.py --check
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-08 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 3600
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.8 --base-ref upstream/main
git diff --check
```

创建同名实施记录，记录生成、编译、负向测试和门禁实耗。
