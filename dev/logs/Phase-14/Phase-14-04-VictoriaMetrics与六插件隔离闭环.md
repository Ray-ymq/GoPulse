# Phase-14-04：VictoriaMetrics 与六插件隔离闭环开发记录

## 1. 当前状态

**未完成：上游映射前置确认发现指标语义不一致，尚未开始 Exporter 接入。**

- 执行日期：2026-09-11。
- 已通过 `upstream` fetch 确认前置批次合入；基线为 `3925a375eefd95f40fee9197001e1a3d1f9c645e`，产品版本 `1.11.3`。
- 已从该远程 main 新建 `develop/1.11.4`；目标版本仍为 `1.11.4`，未完成验收，不提升 `VERSION`。
- 原有未跟踪文件 `~` 未修改、未暂存。
- Git 默认代理不可连接；本次通过命令级 `-c http.proxy=http://127.0.0.1:7890` 完成 fetch，没有修改仓库或用户 Git 配置。

## 2. 锁定版本与探测边界

Compose 锁定 `victoriametrics/victoria-metrics:v1.151.0`。本机对应镜像 ID：

```text
sha256:6d164540a04f49ba4e696cbdb70f9fee78be1e94b8f2a1292743a0b1ab8275bd
```

探测使用单独的 `--rm` 容器 `gopulse-p1404-map-probe`，只绑定宿主回环端口，不加入项目网络、不挂载项目卷、不使用产品凭据。数据位于该临时容器的 `/tmp/vm`；以下写入、删除和强制合并仅针对探测容器。它们不是 Exporter 应有权限或产品采集行为。未操作现有 Compose 服务或历史指标。

实际调用：

1. `docker image inspect victoriametrics/victoria-metrics:v1.151.0`。
2. 启动锁定镜像，设置临时 Basic Auth，从受保护的 `/metrics` 取得冷启动快照。
3. 获取锁定 tag 的官方 `dashboards/victoriametrics.json` 与官方 FAQ，核对 active series 的公开语义；未阅读第三方实现源码。
4. 在临时实例调用 `/api/v1/import/prometheus`、`/internal/force_flush`、`/api/v1/admin/tsdb/delete_series`、`/internal/force_merge`，验证删除计数是否只表示 retention。
5. 再次读取 `/metrics`，仅记录下述筛选结果，不提交原始完整响应、账号或凭据。

资料定位（版本锚点与页面路径）：

- 官方仓库 `VictoriaMetrics/VictoriaMetrics`，tag `v1.151.0`，`dashboards/victoriametrics.json`，面板 `Active series` / `Active time series`。
- 官方文档 `docs.victoriametrics.com`，`/victoriametrics/faq/` 的 `What is an active time series?`。
- 官方文档 `docs.victoriametrics.com`，`/victoriametrics/single-server-victoriametrics/`；镜像 `-help` 中 `-retentionPeriod` 默认值为 `1M`。

## 3. 九个 family 的前置核对

下表是**候选映射与实际观测，不是已实现的白名单**。除明确写出的观测外，完整聚合范围、固定 label allowlist、异常缺失和冷启动处理仍须在接入前完成确认；不能据此宣称九项合同通过。

| 产品 family（统一前缀 `gopulse_victoriametrics_`） | 锁定实例字段 / 候选筛选 | 观测与尚未满足项 |
| --- | --- | --- |
| `up` | Exporter 本次完整快照是否成功 | 本批尚未实现；不能用 HTTP 200 代替完整九项映射成功 |
| `rows_inserted_total` | `vm_rows_inserted_total{type=...}` | 冷启动各协议显式为 0；最终协议 allowlist 和入站 rows 范围尚未锁定 |
| `query_requests_total` | `vm_http_requests_total{path="/api/v1/query"}` 与 `{path="/api/v1/query_range"}` | 冷启动两项均显式为 0；拟只取这两条查询路径，不汇总 metrics/import/admin/其他 HTTP 请求 |
| `active_timeseries` | `vm_cache_entries{type="storage/hour_metric_ids"}` | 锁定 dashboard 使用该字段；官方说明为最近一小时有新样本的 series。冷启动为 0，两条临时 series 写入后为 2；不是累计 created series，也不要求删除后即时减少 |
| `storage_rows` | `vm_rows{type="storage/inmemory\|storage/small\|storage/big"}` | 三项冷启动均显式为 0；与 indexdb rows 分开，不混加索引行。最终存储统计合同尚未完成 |
| `storage_size_bytes` | `vm_data_size_bytes{type=...}` | 观察到 storage/inmemory、small、big、metaindex 及 indexdb 家族；数据/索引/内存的最终 bytes 范围尚未锁定，不以磁盘占用近似代替 |
| `free_disk_space_bytes` | `vm_free_disk_space_bytes{path=...}` | 实例提供单个存储路径样本；路径必须仅内部精确匹配，不能透传为公共 label |
| `active_merges` | `vm_active_merges{type=...}` | 观察到 storage/inmemory、small、big 及 indexdb/inmemory、file；最终是否覆盖 indexdb 尚未锁定 |
| `retention_deletions_total` | 候选 `vm_rows_deleted_total{type="storage/inmemory\|storage/small\|storage/big"}` | **候选不等价：真实反例表明未发生 retention 过期时，显式删除也能增加该计数。没有确认满足现有产品语义的稳定替代字段** |

冷启动中上述候选字段的零是上游显式样本，不是本地补零。该观察不证明所有运行场景都不会省略字段；没有建立任何缺字段补零例外。

## 4. retention 映射的真实反例

临时实例使用默认 `1M` retention，探测历时数分钟，写入样本时间为当前时间。

1. 冷启动三个 `vm_rows_deleted_total` storage 分量均为 0。
2. 导入 `p1404_mapping_probe 1`，强制 flush；通过 `delete_series` 显式删除该 series，然后强制 merge。
3. 导入另一条 `p1404_mapping_keep 2`，再次 flush 和 merge，使待合并数据实际发生合并。
4. 最终脱敏筛选值：

```text
vm_rows_deleted_total{type="storage/inmemory"} 0
vm_rows_deleted_total{type="storage/small"} 1
vm_rows_deleted_total{type="storage/big"} 0
vm_deleted_metrics_total{type="indexdb"} 1
vm_rows{type="storage/inmemory"} 0
vm_rows{type="storage/small"} 1
vm_rows{type="storage/big"} 0
vm_cache_entries{type="storage/hour_metric_ids"} 2
```

结论仅限已证明事实：`vm_rows_deleted_total` 不是 retention 专属删除计数，不能直接映射为现有 `retention_deletions_total`。`vm_deleted_metrics_total` 是另一种单位的 series 删除计数，也不能凭名称替换。此探测没有证明不存在其他可用字段，也没有验证 retention 周期删除的完整计数行为。

依照总方案 §10 / §16.1 和分方案 §2.1，未静默删除 family、填零、改名或将上述候选投入接入代码；官方 catalog 继续保持 VictoriaMetrics unavailable。当前阻断项是第九项的同义映射合同，而不是网络、Docker 或缺少运行时间。

## 5. 本次改动与验证

本次仅新增本开发记录。没有修改生产代码、制品、Schema、Compose、Frontend 元数据、总/分计划或产品版本；未将任何批次或阶段标记为完成。

实际通过的探测：

- 主远程 fetch 与分支创建。
- 锁定镜像启动、受 Basic Auth 保护的真实 `/metrics` 读取。
- 临时样本写入、显式删除、flush/merge 以及上述数值反例。

固定完成门禁尚未运行：新 Exporter 不存在，六 source/isolation 验收未扩展，因此不能运行或声称通过 Go/Frontend 固定回归、真实六插件闭环与故障隔离门禁。文档和治理检查结果在本节追加。

## 6. 恢复实施所需决定与后续项

首先解决以下二者之一，再继续接入：

1. 为锁定 `v1.151.0` 确认真正 retention 专属、稳定且同单位的字段及证据，保留现有目录。
2. 明确调整产品语义：例如将第九项改为 `storage_rows_deleted_total`，只表示选定 storage 合并过程报告的已删除 rows，不声称它是 retention 专属或所有过期数据删除总量。依规则应先在 `update` 修订总/分方案并提交，再回到本开发批次实现，不能只改代码。

尚未擅自选择第二项，因为它改变用户计划要求观测的业务含义。其余 Schema/固定 ID/回环端口已有前批基础，当前不提前启用不满足完整成功快照的插件。

后续仍须完成九项最终映射、独立 Exporter、制品与信任、各层目录、Frontend 状态、六插件并行/故障隔离/重启、自观测停机解释以及所有固定门禁。Phase-14-05 不能据本记录依赖任何新增 VictoriaMetrics producer/schema 已交付。

### 本次收尾结果

- `python3 scripts/ci/validate_versions.py`：通过，元数据仍与已完成版本 `1.11.3` 一致。
- `python3 scripts/ci/validate_branch.py --branch develop/1.11.4 --base-ref upstream/main`：失败，当前 `VERSION=1.11.3` 不等于目标分支 `1.11.4`。这是未完成批次的实际状态；没有为使检查变绿而提前升级版本。
- `git diff --check`：通过；提交前另检查新增记录的 staged diff。
- `docker stop gopulse-p1404-map-probe`：成功，`--rm` 自动删除探测容器及其容器内临时数据。
