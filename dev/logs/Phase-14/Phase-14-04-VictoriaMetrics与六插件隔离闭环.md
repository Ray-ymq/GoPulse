# Phase-14-04：VictoriaMetrics 与六插件隔离闭环开发记录

## 1. 当前状态

**已完成（2026-09-11，版本 `1.11.4`）。最终六插件完整门禁与浏览器验收均通过；下文 §2–§6 保留初次前置探测的历史事实，后续实现与修复见 §7，最终收口见 §8。**

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

## 7. 恢复实施进度（2026-09-11，覆盖前述待决定状态）

用户已确认删除行合同调整，`update` 规划提交 `44fc768` 已推送；本开发分支继续同一任务，
通过 `e6b6da5` 引入修订，没有重建/重命名已有分支。开工 fetch 后主远程 main 仍为
`3925a37`。前述“未开始接入”和“需要用户决定”仅描述初次前置探测，不再是当前状态。

### 已实现的直接范围

- 独立 VictoriaMetrics Exporter：受控 origin、强制 Basic Auth、固定 `/metrics`、禁用代理/
  redirect/compression、1 MiB body、完整九项/九样本输出；缺失/重复/非有限/负数整体失败。
- Monitor/Backend 六类 catalog、配置 adapter、连接测试；双层 metrics 固定目录、Router
  source、Marshaller 来源身份存储与 Backend 精确矩阵查询；Frontend Schema 配置/生命周期/
  source 查询及目标不可用与存储不可用文案。
- 生产 Dockerfile 中独立构建/包/信任登记；acceptance-only 可信失败与高版本成功制品。
- 修正已观测状态缺口：collector 成功发布 target_unavailable 后不再清空目标错误，沿用
  既有安全 `network_failed` 公共码（不改 DTO）；发布失败仍是 `publish_failed`。
- 强归属六 source 验收复用已有 harness，新增仅验收使用的转发拒绝 helper，不对生产
  Router/Monitor 增加故障注入开关或绕过认证路径。

### 最终映射选择

九项精确上游/标签/聚合/单位/缺失规则现已写入 `exporters/victoriametrics/README.md`，
而非继续沿用本记录 §3 的候选状态：

- ingest 为锁定 16 个协议 `vm_rows_inserted_total` 的和；query 只计 query/query_range。
- active 为 `storage/hour_metric_ids`，沿用锁定 dashboard 最近一小时窗口。
- rows 只聚合 storage/inmemory、small、big（3 项），不混加 indexdb rows。
- bytes 聚合锁定 storage/indexdb 的 7 项（含 metadata/inmemory 计量），明确不是文件系统
  占用或进程内存；与官方 dashboard 的数据/索引计量来源一致。
- free disk 精确路径 `/victoria-metrics-data`，不返回路径 label；merges 为 storage/indexdb
  的固定 5 项；deleted rows 为用户确认的 storage 三项。
- 上游冷启动的选定样本实际显式为零，不引入省略补零例外；重启 counter 重置原样传递。

未重跑初次成功的 retention 显式删除反例；未阅读第三方实现源码。官方资料仅核对锁定
tag 的 dashboard 公共查询及 FAQ（此前资料在 `/tmp/p1404-dashboard.json` 保留）。

### 构建与检查进度

- 四个 Go 模块完整测试及 Backend 计划固定四包测试通过，输出在 `.run/p1404/checks/`。
- Frontend 首次测试暴露 fixed-source validator 未允许第六项，已同步修正；随后完整
  `npm test` 17 文件/70 项、`npm run typecheck`、`npm run build` 均通过。
- `bash scripts/verify-plugin-metrics.sh --self-test` 通过，Python 编译检查通过。
- Docker 构建采用前批已记录的环境适配：临时 Dockerfile 仅省略 syntax directive，保留
  锁定生产 Go/runtime stages；前端采用 Node 24.20.0/npm 11.19.0 的静态产物和现有锁定
  nginx 镜像层。没有更改 daemon/全局代理配置，不声称原始 syntax frontend 拉取已修复。
- 生产包重复构建字节完全一致，`cmp` 通过，SHA-256：
  `800f7f52c69b603e2cdb8299ebcccede9bf1de5f2ca8ef2d4071676e3502cfbb`。
  生产 Monitor 实际包含 VM current 包且不存在 VM failure 包。
- 实际六 source Compose 门禁已启动，结果尚待下面收尾记录；完成前不升级 `VERSION`。

### 首次六 source 门禁的具体修正

首次真实运行在 VM Backend up 查询超时退出，harness 清理成功，证据
`.run/p1404/acceptance.log` / project `gopulse-p1401-f748f99de358`。六个子进程已运行并
成功发布，但 Marshaller main 的 source dispatcher 尚未登记 `metrics/victoriametrics`，
因此仅注册 decoder/transformer 不足以持久化；已补上正式 writer 路由。同时补齐 Monitor/
Marshaller/Backend/Frontend 原有 Events ID allowlist 的第六项，否则无法交付本批生命周期
事件合同。仅重跑这些直接变化模块和 Frontend，保留 Exporter/Router 未变化的成功证据。
没有扩大到业务依赖或数据库审计；下一次真实 gate 是修复已观察阻断失败后的重跑。

第二次真实 gate 已通过六 source 的 Backend up/代表值查询，阻断在验收端原始 HTTP
快照完整性断言；日志 `.run/p1404/acceptance-2.log`，project `gopulse-p1401-c2521f6c0502`
清理成功。新 harness 的 `printf | nc` 没有沿用前批保持 stdin 打开的读取方式，已恢复
既有 `{ cat; sleep 3; } | nc` 模式，并增加仅 HTTP code/body 长度/family 数的安全诊断。
该次未修改生产 Exporter，不重复已通过的 Go/Frontend 门禁；继续验证剩余真实合同。

第三次运行主动中断并完成清理：发现 harness 用“相对时间范围返回的整个 point 精确相等”
判断历史保留，但 Backend 每次使用新的 `now` 作为 query_range 网格起点，评估时间戳
本就会移动。已改为恢复后必须仍查到故障前 cutoff 以前的 up=1 历史点，不要求两次
查询网格字典完全一致。这是修正历史验收的确定性，不改变 Backend API 或存储合同。

第四次真实 gate 通过六类数值、VM 完整映射、专用 RabbitMQ 目标认证隔离/恢复、VM
进程崩溃隔离/恢复；可信更新请求遇到 HTTP 413（`.run/p1404/acceptance-4.log`）。根因是
nginx 大包上传精确 location 只有前五类，已为 VM update 添加同样的 65m 精确例外，
没有放宽普通路由 body 限制；只重建 nginx 层，`nginx -t` 通过，不重复未变化的 npm/Go。
另外把新点断言与 Monitor `last_success_at` 前进同时绑定，避免将 query_range 的移动
评估时间戳误当成实际发生新采集。两个改动都直接对应更新闭环/隔离验收合同。

第五次已通过可信失败包回滚和自洽未登记包执行前拒绝，随后 harness 调用继承的
`upload_for` 时漏传 `expected`，在发出成功更新请求前 TypeError 退出；已补 `200`。
`.run/p1404/acceptance-5.log` 清理成功；只修验收调用，不改生产代码/制品。

第六次真实 gate 已通过前八个阶段：六插件闭环、目标/进程/更新/定向发布隔离、共享 Router
故障与同进程恢复、VM 自观测停机语义与历史保留、单损坏记录不阻塞其余五类及修复后六进程。
最后回归读取 Events 时 harness 误用 metrics 的 `range` 参数（Events 只接受 from/to），
该确定性 400 被 wait_until 折叠成超时。已改用 Events 既有默认 15m 和固定 plugin_id
过滤，不改 Events API。`.run/p1404/acceptance-6.log` 清理成功；重跑完成最后的浏览器/
Events/Secret 扫描收口，既有 package 检查仍有效。

## 8. 最终收口与 Phase-14-05 交接

### 固定门禁结果

最终 `bash scripts/verify-plugin-metrics.sh --sources redis,mysql,rabbitmq,kafka,elasticsearch,victoriametrics --fault-isolation`
退出码 **0**，一次完整运行通过全部九个阶段。最终输出 `.run/p1404/acceptance-7.log`；
强归属 project `gopulse-p1401-e9d499a12a52`，脱敏 `results.json`、`topology-evidence.json`、
`browser.log` 均保存在对应 `.run/` 目录。清理成功：仅本次容器、网络、数据卷被移除，
开工前资源仍在；env、override、部署管理员和 collector account JSON 已移除。

| 实际命令 | 最终结果 / 证据 |
| --- | --- |
| `(cd exporters/victoriametrics && go test ./...)` | 通过；`checks/victoriametrics.log` |
| `(cd monitor && go test ./...)` | 通过；`checks/monitor.log`（含六 ID token 串行/隔离及目标失败安全状态） |
| `(cd router && go test ./...)` | 通过；`checks/router.log` |
| `(cd marshaller && go test ./...)` | 通过；`checks/marshaller.log` |
| `(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/eventquery ./internal/http)` | 通过；`checks/backend.log` |
| `(cd frontend && npm test)` | 17 文件 / 71 项通过；`checks/frontend.log` |
| `(cd frontend && npm run typecheck)` | 通过；同上 |
| `(cd frontend && npm run build)` | 通过；同上（版本 metadata 后续仅更新版本号，无代码/依赖变化） |
| `bash scripts/verify-plugin-metrics.sh --self-test` | 通过；封闭六 source 参数与强归属验证 |
| `python3 -m py_compile scripts/ci/verify_plugin_isolation.py scripts/ci/verify_plugin_metrics.py` | 通过 |
| 六 source / fault-isolation 固定命令 | 全部通过，退出 0；`acceptance-7.log` |
| 真实 Playwright `e2e/phase14-isolation.spec.ts` | 1 passed，4.7s；真实管理员 VM connection-test/configure/stop/start/metrics 与清空 Secret DOM |
| 同二进制 Manifest v2 重打包 + `cmp` | 字节相同；`packages/reproducible.log`，SHA-256 见 §7 |
| 生产 Monitor 镜像 VM current 存在 / failure 不存在检查 | 通过；验收失败包不进入正式镜像 |
| `python3 scripts/ci/validate_versions.py` | 通过，根/Frontend/package lock/示例环境版本 `1.11.4` 一致 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.4 --base-ref upstream/main` | 通过 |
| `git diff --check` | 通过；提交前另检查 staged diff，提交后检查相对 main 的提交范围 |

表中 `checks/`、`packages/`、`acceptance-7.log` 均相对 `.run/p1404/`。没有因前端版本
元数据变化而重复已通过的代码测试；Go/Frontend 最终成功检查覆盖最后的生产源代码。

### 真实闭环与隔离证据

- 六种生产包在空卷逐一安装，进程记录 PID 各异，容器实际 `/proc` 每种 executable
  恰好一个；回环端口按固定目录为 9121–9126，每种 Backend up=1 与运行值可读。
- VM 首次完整快照：up=1、ingest rows=1283、query requests=30、active series=62、
  storage rows=1211、storage bytes=64179、free disk bytes=77530329088、active merges=0、
  storage deleted rows=0。九项均经正式存储由 Backend 查询；上游 query 两项为 0/30、
  active=62、storage deleted 三项均为 0，脱敏证据可解释映射。
- VM 恢复后同一 Exporter 的快照：up=1、ingest rows=820、query requests=16、active=62、
  storage rows=5410、storage bytes=65738、deleted rows=0；counter 重置来自 VM 重启，
  没有本地累计掩盖。六插件 sample 身份有界，Exporter 只输出九项，不抓回存储的全量
  `gopulse_*` series；更新 producer version 不增加时序身份。
- 仅修改专用 RabbitMQ collector 密码，得到唯一 up=0/503 与安全状态，其他五类
  `last_success_at` 前进且 Backend 新点可读；恢复后原六进程记录不变。
- VM 错凭据 connection-test/configuration 返回安全 422，配置 active 指针不变；
  VM Exporter 意外退出只使该 ID failed，其余五类继续采集与存储，start 恢复唯一进程。
- acceptance 镜像信任的 `1.11.90` 失败包触发回滚，active 指针不变；自洽但未登记的
  `1.11.89` 包在执行前拒绝，原 process record 不变；可信 `1.11.5` 升级成功。
- 不同 ID 操作重叠、单 ID 定向 publish_failed、共享 Router 完全停机均未停止其他
  Exporter/collector。定向故障其余五类真正采集成功时间前进；共享故障期间不要求
  新点可查，恢复后六类新点收敛。固定 per-ID token 单元断言证明同 ID 不能同时占用，
  其他五 ID 与状态读取不被串行锁阻塞。
- VM 完全停机时 Monitor 返回安全 target unavailable，Backend metrics 返回 503，
  社交帖子读取正常，六进程仍存活。恢复后 Exporter 记录、Monitor/Marshaller 容器身份
  保持，自动查询到新完整数值与停机前历史点；不声称停机当时向 VM 写入了 up=0。
- Monitor restart 时人为损坏仅 MySQL active pointer，其他五类仍按固定 catalog 顺序
  恢复；修复 pointer 后再次 restart，六种 desired running 各恢复一个真实进程。
- 普通用户对六类管理各方法与 VM metrics 查询均被拒绝。Logs/Events、正式 Kafka offset、
  Phase 13 帖子/搜索、VM 管理浏览器与 Secret 文件 0600/公共输出扫描全部通过。
  按 VM plugin_id 查询的生命周期/故障事件仅属于 VM，包含安装、更新、采集失败/恢复、
  目标不可用/恢复；API/Event 公共产物没有 PID、executable path 或私有插件路径。

### 文件与交接范围

实际新增 `exporters/victoriametrics/`（cmd、collector、runtime、针对新合同的测试与映射
README）、六插件验收 `scripts/ci/verify_plugin_isolation.py` 及 acceptance-only forwarding
helper `scripts/ci/testdata/plugin-fault-router.go`、VM 浏览器用例。修改以下直接链路：

- Monitor plugin catalog/adapter/runtime、metrics collector/契约测试、Events allowlist。
- Router source allowlist；Marshaller main dispatcher、envelope/transformer/Events allowlist。
- Backend exporter catalog/configuration、metricquery 精确目录/身份验证测试、Events allowlist。
- Frontend exporter catalog、metrics types/catalog/validator、Events validator、配置默认端口/
  username、目标与存储失败文案；nginx 仅新增 VM update 精确大包路由。
- 官方 packager、Monitor 生产/验收 Docker stages、固定验收入口；根及相关 README、
  总/分方案、此日志、VERSION/Frontend 版本元数据和 `.env.example`。

Phase-14-05 可依赖六个固定 plugin/source/target、Manifest v2 Schema、独立进程/collector/
per-ID 锁与恢复语义，及 VM 九项 producer_kind=`exporter_plugin`、producer_id=
`victoriametrics-exporter`、target_id=`victoriametrics-exporter-local`。VM 存储标签不包含
producer_version；Redis 历史身份不变。Phase 14 整体仍未完成，下一批为 Phase-14-05。

### 偏差与限制

前置指标语义变更经过用户授权并先在 `update` 提交/推送，不是偷偷将 retention 换成
近似量。此前失败/中断 gate 及修复均如实保留，不能把它们计为通过；最终仅最后一次
完整运行作为六插件整体验收通过证据。无未解决的本批阻断失败。

仅在 Linux amd64/Bash/Compose 实施和验收；不宣称 macOS、原生 Windows、多架构或
Kubernetes 支持。环境仍使用前批的 builtin Dockerfile frontend/宿主 Node 构建路径，
不声称上游 syntax frontend 拉取问题已修复或完整 CI 已执行。没有新增 retention
专属监控、历史指标本地队列、第二存储、同类多实例或第三方插件。达到固定门禁后停止，
未附加独立审计/代码评审报告，也未扩展为业务全量回归或覆盖率活动。
