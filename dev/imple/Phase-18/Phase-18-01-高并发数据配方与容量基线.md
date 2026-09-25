# Phase-18-01：高并发数据配方与容量基线实施方案

> 目标版本：`2.0.1`  
> 开发分支：`develop/2.0.1`  
> 验收平台：WSL2 Linux `amd64`，8 vCPU / 12 GiB RAM / 8 GiB swap

## 1. 批次目标

本批建立后续五批共用的确定性数据、负载、资源取样和 evidence 基础，并对当前单副本产品取得三轮未优化基线。
基线即使不满足阶段 SLO 也必须如实保留；2026-09-24 规划修订后，本批允许以
“失败基线已交付、重复性与恢复门禁未通过”收口。严格重复性、最终收敛和完整 SLO
不在本批追加重跑，统一留给 Phase-18-06。本批不为追逐 RPS 修改产品算法、Schema
或中间件参数。

## 2. 前置条件

- Phase 17/Milestone 4 已在 `1.14.5` 完成，从最新 `upstream/main` 创建本批分支。
- WSL2 预检确认 8 vCPU、12 GiB RAM、8 GiB swap、Linux 文件系统和至少 80 GiB 可用 SSD 空间。
- 宿主没有与本测试竞争的非本批 Compose project；基线前记录 Docker/Compose/kernel/CPU/内存/磁盘事实。
- 使用本批 `2.0.1` 候选的不可变 manifest/Bundle/image digest，不从验收目录构建源码。

## 3. 实施范围

### 3.1 数据配方与生成器

- 在独立 `loadtest/` Go module 实现离线数据生成和负载执行，不把压测代码编进产品二进制。
- 固定 seed `18002005`，产生 5,000 用户、50,000 帖子、100,000 评论、200,000 点赞、200,000 关注和 25,000 收藏。
- 通知和 Outbox 必须由业务事实确定推导，不使用随机总数。生成后调用正式 search reindex 并等待 Rabbit/Kafka 收敛。
- recipe receipt 记录 schema version、seed、各表计数、ID 范围、非敏感摘要 digest、生成耗时和候选身份。
- 只允许在新建的强归属 project 执行；非空目标安全失败，清理不使用全局 prune。

### 3.2 负载工具

- 按总方案 45/10/10/15/15/5 的类别比例执行混合负载，并将每个路由标注为 read、write、search 或 session。
- 负载器使用固定虚拟用户池和长连接；预先登录建立会话，正式窗口不重复创建账号。
- 采用单调度器的 open-loop 到达率，不因服务变慢而自动降低 RPS；记录 coordinated-omission-safe 延迟。
- 输出每路由请求数、状态码、P50/P95/P99/max、超时、明确拒绝与非预期错误，不保存 Cookie 或 payload。
- 在无网络的 mock server 上校验调度精度、分位数、分类、超时和报告确定性。

### 3.3 资源与链路取样

- 每 5 秒记录宿主/Compose 的 CPU、RSS、block I/O、network I/O、restart/OOM 和 swap 增量。
- 记录 MySQL pool/wait、Outbox pending/oldest age、Rabbit 队列深度与 unacked、Kafka lag、Router buffer、Marshaller retry/in-flight，
  以及搜索、通知、Metrics/Logs/Events 端到端收敛。
- 每个资源样本先以单行 JSON 独立追加并 `fsync` 到 `resources.raw.jsonl`，再进入内存汇总；
  `resources.json` 或最终容量汇总失败不得丢失已采集样本。
- 每轮在负载前写入 `load-binding.json`，绑定候选版本/revision/manifest、corpus SHA-256、
  负载源码 commit、负载/recipe 二进制 SHA-256 和固定负载参数。
- 负载器的 CPU/RSS/调度滞后单独记录；若它首先饱和、无法达到指定到达率或丢失超过 0.1% 调度槽，本轮无效。

### 3.4 单副本三轮基线

- 每轮使用新建的相同 recipe project，顺序执行 5 分钟预热、15 分钟 150 RPS、2 分钟 300 RPS 和最长 10 分钟恢复。
- 三轮之间不改变候选、配置、宿主配额或 recipe；记录 RPS、P95/P99 重复性，但失败时不重跑筛选，
  也不生成或改写通过 evidence。
- 报告对照阶段 SLO，但基线未通过 SLO 不允许改写为通过；必须保留 `capacity-failure.json`、
  原始资源样本和多窗口诊断，并以资源、延迟、队列或收敛证据标出第一瓶颈。
- 第 3 轮 18 次 500 的 request-id 线索保持可追溯；只有后续出现重复或扩大时另开最小修复任务。

## 4. 不在本批范围

- 多副本 Compose、Kafka 分区扩容、负载均衡修复或所有权改造。
- 双 Elasticsearch、背压参数、合同生成或独立诊断。
- 为基线数据做索引、SQL、缓存、连接池或中间件的泛化调优。

## 5. 预计直接影响

- `loadtest/` 中的 Go 数据/负载工具、单元测试和报告 schema。
- Phase 18 宿主预检、候选绑定、资源取样、基线 runner 与 evidence verifier。
- 容量报告文档和 `dev/logs/Phase-18/Phase-18-01-高并发数据配方与容量基线.md`。
- 本批成功时的 `VERSION` 与受管版本元数据。

## 6. 验收与完成条件

1. 相同 seed 两次生成得到完全相同的计数和摘要 digest，非空/非归属目标安全失败。
2. 负载比例、到达率、分位数和错误分类自测通过，原始资源在汇总前逐样本持久化。
3. 只执行一次固定三轮，三轮都绑定同一 `2.0.1` 候选、corpus 哈希和负载版本；通过时写
   `capacity.json`，失败时停止并写独立 `capacity-failure.json` 与各轮原始样本，不重跑筛选。
4. 报告如实记录 SLO 对照、第一瓶颈、资源峰值、积压、HTTP 错误和恢复时间；失败基线可作为
   Phase-18-02 输入，但不代表重复性、恢复或 SLO 通过。
5. 没有 OOM，测量窗口 swap 增量不超过 256 MiB，本批资源完整清理。
6. 第 3 轮 18 次 500 的 request-id 线索被保留；本批不因未复现而扩展为一般修复。

## 7. 固定验证命令

```bash
go test ./...                         # 在 loadtest/ 内
scripts/verify-phase18-capacity.sh --self-test
scripts/verify-phase18-capacity.sh --manifest dist/release-manifest.json --rounds 3 --work "$GOPULSE_PHASE18_WORK"
test -f "$GOPULSE_PHASE18_WORK/evidence/capacity.json" || \
  test -f "$GOPULSE_PHASE18_WORK/evidence/capacity-failure.json"
python3 scripts/verify-phase18-evidence.py --capacity "$GOPULSE_PHASE18_WORK/evidence/capacity.json"  # 仅通过分支
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.1 --base-ref upstream/main
git diff --check
```

创建同名实施记录，更新 `VERSION=2.0.1`，只提交本批文件后停止。失败分支必须明确记录
“单副本失败基线已交付，重复性与恢复门禁未通过”，不得把 `capacity-failure.json`
转换为 `capacity.json`。
