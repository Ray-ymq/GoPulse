# Phase-15-03：Logs 与 Events 告警及故障隔离闭环开发记录

## 状态与基线

- 状态：已完成；直接包测试与真实三源固定验收通过，完成版本 `1.12.3`。
- 2026-09-12 fetch `upstream` 和 `origin`，从最新 `upstream/main` 的 `d72ae6c` 创建 `develop/1.12.3`，基线为 `1.12.2`。
- 原未跟踪文件 `~` 未读取、修改或暂存。

## 实际实现

- 从 `logquery.go`、`eventquery.go` 抽出 `vocabulary.go`；公共查询保留既有合法/非法组合，告警 catalog 和验证共享词表及组合谓词。
- Logs 只接受 `service,module,level,message,error_code`；Events 只接受 `source,event_name,severity,plugin_id,operation,error_code`；至少一个非空 selector，固定 `count` reducer。沿用 selector 的 `labels` DTO，计数源的 `metric` 必须为空。
- 两个固定 read alias 的 `AlertCount` repository 重新验证 selector；Events 的 plugin/operation/error 三个字段映射到 `metadata.*`，没有任意索引、查询或上游地址入口。
- 内部 `alert/count` 先检查固定字段的 `_field_caps`（timestamp=`date_nanos`，selector=`keyword`，必须可搜索），再发送只有固定 range/term filter 的 `_count`。窗口为 UTC `[now-15s-window,now-15s]`，两次请求共用最多 2s context。
- 响应最多 64 KiB；整数 count 在 `[0,2^53-1]`，保证转换为现有 float64 value 无损；严格要求有效完整 shard 成功响应。非 200、类型/范围错误、部分 shard、mapping 缺失/不匹配均为 unknown。
- 缺失 alias **不自动视作 0**：本批没有可靠的新装空数据标志，404 因而保守 unknown。真实成功空窗口才是有效 0，符合计划“不能证明新装则阻断该源”的要求。
- Scheduler 接入三源 adapter，共用原有 lease/revision、state/incident/audit 事务。单次 adapter panic 在提交状态前恢复，错误仅持久化 `metrics_unknown|logs_unknown|events_unknown`；unknown 保持 firing/stale，pending 连续性仍由原状态机打断。
- 不新增 migration，不投影审计/告警为 Events，不保存或返回匹配文档，不新增管理 Frontend。

## 直接文件与检查范围

- `backend/internal/alert/{model,handler,scheduler}.go`、`count/` 和 `count_sources_test.go`。
- `backend/internal/{logquery,eventquery}/{原查询文件,vocabulary.go,count.go}`、`backend/cmd/server/main.go`。
- `scripts/ci/verify_alerts.py`、`verify_alert_sources.py`；沿用 `scripts/verify-alerts.sh` 唯一入口。
- 直接测试证明新 selector 合同、固定 UTC count builder/超时/响应边界、mapping unknown、adapter panic 与 count=0。原 Logs/Events 查询测试同时运行，未扩展六插件或全产品验收。

## 已执行验证与过程问题

- 实施中直接 Go 包测试通过。
- 最终生产代码的固定命令 `(cd backend && go test ./internal/alert/... ./internal/logquery ./internal/eventquery ./internal/http)` 已通过；后续代码未改变时保留此结果，不重复执行。
- `bash scripts/verify-alerts.sh --self-test` 已通过，不访问 Docker。
- `python3 -W error -m py_compile scripts/ci/verify_alerts.py scripts/ci/verify_alert_sources.py` 已通过。
- 两次早期 Compose 探测在等待真实 Metrics 时主动中断，以修正验收辅助代码：角色读取改为既有按 ID 接口；脱敏匹配从 `_count` 改为 `/_count`，避免误拒合法的 `evaluation_count` DTO 字段。两次均执行 finally 清理，并确认原有资源保留；不计为完整运行验收通过。
- 当前固定三源入口复用未修改的 `1.11.5` Monitor/Router/Marshaller 镜像，Backend/migrate/admin-role 从当前 checkout 实际编译绑定；不冒充全产品镜像已重建。

## 收口证据

完整结果见下文；原有成功包测试保持有效，未因等待窗口或更新文档/版本重新执行。

### 真实 Events selector 的有界调整

第三次探测已证明 Logs 的精确 UTC 边界和真实空窗口，但等待 `exporter_plugin_started` 时，实际 read alias 中只有 1 条 `exporter_plugin_installed` / `metadata.operation=install`。以受控只读 aggregation 确认后停止该次探测并清理，未扩大调查既有插件生命周期实现。固定验收 selector 改用同一已发布目录内的真实 install 事件（`source=monitor,event_name=exporter_plugin_installed,plugin_id=redis-exporter,operation=install`）；事件仍经真实 Monitor/Router/Kafka/Marshaller/Elasticsearch 链路产生，不伪造文档。该调整不改变生产代码、规则语义或验收合同。


## 最终真实链路证据

固定命令 `bash scripts/verify-alerts.sh --sources metrics,logs,events --fault-isolation` 返回 0；项目 `gopulse-p1401-205ef258dc9e`，本地脱敏证据 `.run/gopulse-p1401-205ef258dc9e/alert-evidence.json`，捕获输出 `/tmp/gopulse-phase1503-runtime.log`。证据时间为 UTC；完整流程在 2026-09-12 09:03–09:09 UTC（北京时间 17:03–17:09）通过。退出时已清理本项目容器/网络/卷及临时凭据，并验证启动前资源仍存在。

### 目录与计数边界

| 来源 | 词表字段及值数量 | 合法组合数 |
| --- | --- | --- |
| Logs | service=4、module=11、message=53、level=3、error_code=18 | service/module/message tuple=98 |
| Events | source=1、event_name=10、severity=3、plugin_id=6、operation=7、error_code=17 | event/severity/operation/可选 error tuple=31 |

Elasticsearch 使用 Compose 锁定 `9.5.2`。两个来源都通过真实文档时间戳的 `gte=lte` 查询，命中 count≥1；真实空窗口查询 count=0 且 shard 无失败。缺失索引返回 HTTP 404 / `index_not_found_exception`，与空计数区分；停 Elasticsearch 后实际规则变为 source-local stale 而不是 0。认证 HTTP 401、异常类型/范围、部分 shard 与 mapping 错误由直接 repository 测试证明 unknown，未将关闭安全认证的 Compose 探测冒充真实认证拒绝演练。

### 真实规则、历史与审计

三条规则均 `window=5m,for=0s,operator=gt`：

| 来源 / rule ID / incident ID | selector 与真实产生路径 | 去重时 value / 评估次数 | 最终 value / 评估次数 |
| --- | --- | --- | --- |
| Metrics / 1 / 3 | `gopulse_redis_connected_clients`、空 labels、`last`、threshold=10；真实认证 Redis BLPOP 客户端 → Exporter/Monitor/Router/Kafka/Marshaller/VM | 21 / 3 | 1 / 8 |
| Logs / 2 / 2 | `service=backend,module=auth,message=user registered`、`count`、threshold=0；真实注册请求 → Backend Log shipper/LogMonitor/Router/Kafka/Marshaller/ES | 2 / 4 | 0 / 8 |
| Events / 3 / 1 | `source=monitor,event_name=exporter_plugin_installed,plugin_id=redis-exporter,operation=install`、`count`、threshold=0；真实插件 install → Monitor/Router/Kafka/Marshaller/ES | 1 / 4 | 0 / 8 |

- firing 时替换 Backend，继续评估后每条规则仍只有同一个 incident；连续至少三次 true，各只有一次 `alert.trigger`。
- 最终 history 共 3 条，全部 `recovered`，current 为空；每条各一次 `alert.trigger`、一次 `alert.recover`，没有重复触发/恢复审计。
- Metrics first trigger=`09:04:24.789750Z`，last trigger=`09:08:30.642761Z`，recovered=`09:09:02.646754Z`。
- Logs first trigger=`09:03:53.474822Z`，last trigger=`09:07:41.822514Z`，recovered=`09:08:30.644289Z`。
- Events first trigger=`09:03:53.475190Z`，last trigger=`09:07:41.822481Z`，recovered=`09:08:30.646652Z`。
- 以上日期均为 2026-09-12 UTC。history 保存受限 object 和最后 value，不保存匹配文档；探测只临时读取 timestamp，不将原始文档写入证据。

### 串行故障与恢复

1. 停 VictoriaMetrics：Metrics firing/stale，`metrics_unknown`；Logs/Events 在新一轮继续 firing/ok；恢复 VM 后 Metrics 回到 ok。
2. 停 Elasticsearch：Logs/Events firing/stale，分别 `logs_unknown`/`events_unknown`；Metrics 在新一轮继续 firing/ok；恢复 ES 后两个计数源回到 ok。
3. 每个故障边界均验证规则/current/history、按 ID 角色读取以及管理员和普通用户的代表性社交读取；没有 `alert.recover` 误增。
4. `ALERT_EVALUATION_ENABLED=false` 替换 Backend，等待 35s：state 快照、incident evaluation_count 总数和 audit 行数均不变；Backend healthy/readiness、社交和管理读取正常。再开启，从持久状态恢复评估；移除真实 Redis 客户端及文档自然滑出窗口后恢复原 incident。
5. API/incident 响应自动检查敏感串；最终 alert audit details 与 Backend 日志检查不含上游口令、地址、alias 或 count 查询。认证/上游原始错误不进入公开 DTO。

## 固定完成命令

- `(cd backend && go test ./internal/alert/... ./internal/logquery ./internal/eventquery ./internal/http)`：通过；包含被抽取词表的现有 Logs/Events 直接契约测试。
- `bash scripts/verify-alerts.sh --self-test`：最终脚本通过。
- `bash scripts/verify-alerts.sh --sources metrics,logs,events --fault-isolation`：完整无干预运行通过，返回 0。
- `python3 -W error -m py_compile scripts/ci/verify_alerts.py scripts/ci/verify_alert_sources.py`、`bash -n scripts/verify-alerts.sh`：通过。
- `python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/1.12.3 --base-ref upstream/main`：通过。
- `git diff --check`、`git diff --cached --check`：工作树与暂存区检查通过；提交后范围检查使用 `git diff --check upstream/main...HEAD`，结果见交付消息。

## 版本与交接

根 VERSION、`.env.example` 产品版本/镜像 tag、Frontend package/lock 根版本同步 `1.12.3`。管理 Frontend 尚未创建，属于 Phase-15-04，不虚构其构建或版本。

Phase-15-04 可直接消费封闭三源 catalog、rules/current/history DTO；权限、计数、局部 unknown、去重和持久恢复由 Backend 负责，不在 Frontend 重做上游查询或状态机。新装缺失 alias 保守 unknown 是已明确的运行限制；无新增外部通知、审计递归、自定义 Elasticsearch DSL 或全产品镜像发布。

## 2026-09-12 推送后 CI 格式修复

- GitHub Actions run `34686300278` 的 Backend `Verify formatting` 失败，后续 Backend Test/Vet/Race 被跳过，自动创建 PR 步骤也被跳过；其他质量检查（包括 Integration 和 Full-stack Compose acceptance）通过。
- 本地 `gofmt -l backend` 精确复现：只有 `backend/cmd/server/main.go` 不符合格式。原因是三源 scheduler 接线调用的参数逗号后缺少空格，上次格式检查没有覆盖该文件。
- 对该文件执行 `gofmt -w backend/cmd/server/main.go`，仅补充空格，无业务逻辑变化；运行 CI 同一命令 `(cd backend && test -z "$(gofmt -l .)")` 通过。
- 该修复是 Phase-15-03 的同批次跟进，继续使用 `develop/1.12.3`，VERSION 保持 `1.12.3`。不重复已通过且不受纯格式变化影响的三源 Compose 验收；推送后由 CI 执行原有 Backend 后续门禁，远程结果以实际检查状态为准。
