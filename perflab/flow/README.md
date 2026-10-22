# 固定 PT 流程

统一入口为 `make pt`，连续加压默认从 10 RPS 开始。`START_RPS=250` 只改变连续加压起点。扫描上限为 6400 RPS，达到上限不代表找到系统极限。

`config.json` 定义固定流程，`resources.json` 是资源唯一来源。执行器由资源配置生成 Compose 覆盖文件，并核对部署值和 cgroup 实际值。系统余量及应急预留不创建容器。Redis 全程启用，禁止自动调参和单因素对照。

CPU 覆盖使用整数 `cpu_quota/cpu_period`。覆盖文件将基础 Compose 的 `cpus` 设为 `0`，防止 Compose 将 `0.13` 转换为较小的 NanoCPU 值；实际 CPU 上限仍由整数配额限制。前端使用 `13000/100000`，管理前端使用 `12000/100000`。资源检查同时核对 Docker 配额和 cgroup 实际配额，不放宽允许偏差。

公共记录采用 `gopulse.pt.<对象>.v1`。准备记录、任务、请求事件、组结果及总结果分别使用 `preparation`、`task`、`request`、`group`、`result`。凭据只通过私有文件引用，不写入事件或报告。

| 历史能力 | 公共位置 |
| --- | --- |
| 边界搜索、细化、持续测试 | Go 负载策略及固定流程 |
| 数据库诊断、资源采样 | 公共采集与窗口分析 |
| 单接口测试 | routes 阶段 |
| cgroup 核验、写入核对 | 环境检查及正确性检查 |
| CPU、缓存、连接池、实例数对照 | 取消 |
| 轮次编排、私有状态衔接 | 停用，不导入 |

历史报告保留原样。工具准备不隐式部署业务程序。正式压测必须显式执行，不在实施验证中自动启动。

`make pt-plan START_RPS=250` 展示固定条件、Go 生成的档位和耗时。该命令不读取或修改业务环境。`make pt-status` 查看当前状态，`make pt-stop` 保存整次停止请求。运行记录位于 `.run/perflab/<run-id>/`，状态原子更新，事件追加保存。环境锁拒绝重复启动。检测到未完成运行时拒绝自动重放，先用停止入口保存中断状态。

准备前，将专用环境配置放在 `.run/perflab/private/test.env`，目录权限设为 `0700`，文件权限设为 `0600`。必须使用 `MYSQL_DATABASE=gopulse_pt`、`MYSQL_USER=pt`、`FRONTEND_PORT=18086`。项目名固定为 `gopulse-pt-business`。先用 `python3 -c 'import sys; sys.path.insert(0,"perflab"); from flow.config import load,compose_resources; from flow.runtime import write_json; write_json(".run/perflab/private/resources.json",compose_resources(load()["resources"]))'` 生成资源覆盖文件，再显式使用 `deploy/compose.business.yaml` 和该覆盖文件部署、迁移。部署凭据和镜像来源由操作者提供；工具不复制旧 PT 状态。

`make pt-prepare` 会构建工具，在专用数据库生成语料、建立索引和快照，然后执行“恢复 → 冒烟 → 再恢复”。已有快照时复用并校验，不覆盖历史快照。该命令会恢复专用业务数据，运行前必须确认环境用途。准备完成后，源码、工具或配置变化均使准备记录失效。完整资源核验包括 Docker 配额、重启与 OOM、实例集合、buffer pool、堆和配置指纹。应用停止或崩溃时拒绝自动修复。

冒烟先验证代理入口的业务读写和异步处理，再逐一登录两个 API 实例并读取帖子。多实例核对只统计本次冒烟窗口内的成功日志；不依赖少量串行请求在代理中的分配结果，也不使用历史处理记录。

自然恢复要求数据库和业务探针成功、异步预算及队列归零。预算与队列样本的时间必须晚于停压时间；停压前的零积压样本不能证明本组已恢复。

收尾后状态中的 `collection` 为 `closed`。`business_passed` 汇总已测的低负载、稳定性和单接口业务结果，未测时为 `null`；它与执行完成、数据正确性及容量异常档分别记录。

正式语料为 512 个压测账号各准备 4127 条一次性删除目标，共 2113024 条。语料校验使用排序数组检查重复、重叠和归属，并按每批最多 1000 条查询数据库，避免同时建立多个全量哈希表。真实准备已验证完整语料可在固定 256 MiB 临时容器内完成校验；这不代表全流程验收已经通过。

搜索重建按帖子 ID 分页。每个查询先从帖子表和当前 epoch 的删除记录中各取至多一页，再合并并取下一页，避免每页重新物化整张事实表。最大 ID 分别从两张表计算。该修复保留删除版本、epoch 过滤和切换后的补偿扫描，不放宽 MySQL 的 1 秒业务读取超时。

Go 工具 `pt-load` 接收明确任务，负责翻倍、二分建议、业务判定及吞吐平台判断。k6 统一使用 `flow/workload.js`，复用公共请求和路由定义。连续档位保留语料序号；请求记录包含开始、完成和停压取消，登录准备不进入业务窗口。最近 10 秒失败率严格超过 50% 才停止当前组，窗口未满或没有完成请求时不填零。延迟、普通业务失败和丢弃迭代不直接触发首次失败停压。压测器无有效完成请求、语料耗尽或保护状态失效会结束当前运行并保留证据。

公共观测复用标准 MySQL exporter、container-exporter、官方 node_exporter、应用 metrics 和 RabbitMQ Prometheus 端点。Redis 当前保存 INFO 基础统计，全量 exporter 窗口列为未接入；覆盖表不宣称实现全部指标。诊断账号的 4 个连接预算分配为 dbdiag 2、MySQL exporter 1、独立探针 1。采集开销写入 `diagnostic-runtime.json` 并计入系统余量。诊断历史不在运行中清空。

自然恢复最多观察 10 分钟。期间只运行单独标记的数据库和业务探针，要求预算与 RabbitMQ ready/unacked 归零。环境不一致、必需观测失效或恢复超时均不得进入下一组。写入核对读取公共事件，覆盖资源 ID、内容、删除、互动、异步处理结果和孤立行。核对前不得恢复快照。取消的写入或缺少资源 ID 会使正确性无法通过。

阶段固定为：环境与数据检查、观测预检、60 秒空载、10 RPS 低负载、连续翻倍、最多 8 次边界二分、异常档独立重复 3 次、正常档 100% 与 80% 各 30 分钟稳定性、全部 22 项路由、总报告。各负载组预热 30 秒，常规测量 5 分钟。连续档位不恢复数据，独立组恢复相同基线。低负载未达标时结束容量流程；没有有效边界时明确跳过。10 RPS 低负载可以作为正常下界，报告保留来源。

各组分别保存执行、业务、停止、恢复、正确性、证据和资源保持结果。容量搜索中的异常档不单独导致执行失败。`make pt-report RUN=<运行编号>` 仅读取归档并生成总结果和 Markdown。退出码：0 为流程完成且必需验收通过；1 为必需业务或正确性失败；2 为参数或前提不足；3 为环境、资源或观测异常导致未完成；130 为人工中止。正确性失败后即使退出码为 1，总报告仍将后续未完成阶段保留为未完成。

本地回归：

```sh
make test MODULE=perflab
make race MODULE=perflab
make check MODULE=perflab
make build MODULE=perflab
GOPULSE_PT_K6_FIXTURE=1 PYTHONPATH=perflab python3 -m unittest flow.test_k6_fixture -v
```

perflab 的 test/race 命令同时运行 Python、JavaScript 和公共配置测试，因此带 `ci:perflab` 标签的模块 CI 会执行这些检查。默认测试不访问业务库和 Docker daemon；显式 k6 fixture 只访问临时 HTTP 服务，使用固定 k6 镜像及 1.5 核 / 1.5 GiB 配额。

`pt-report` 通过 Go 从归档请求事件重新计算业务窗口。重算不连接业务服务，也不启动负载。诊断原始记录和各窗口汇总保持可追溯。修改分析算法后，不能借重新生成报告补回缺失的原始证据。

当前工程验证包括全阶段模拟 fixture、实际采集进程、实际 k6 与离线重算回归。真实专用业务环境的缩短全流程验收尚未完成，因此当前不具备正式压测条件。该验收须保持全部固定资源，只缩短时间和语料；不得用模拟通过代替真实环境验收。

显式部署示例（配置与镜像必须先准备好）：

```sh
docker compose --project-name gopulse-pt-business \
  --env-file .run/perflab/private/test.env \
  -f deploy/compose.business.yaml -f .run/perflab/private/resources.json \
  up -d --wait mysql redis rabbitmq elasticsearch
# 使用相同前缀执行 run --rm migrate，完成迁移后执行 up -d --wait，启动业务实例。
make pt-prepare
make pt-plan START_RPS=250
make pt-check
# 真实隔离验收完成后，才执行 make pt START_RPS=250。
```

部署时须剔除宿主机与 `test.env` 同名的变量，避免 Compose 优先读取宿主机值。公共工具已经执行这一处理；手工部署后仍须用 `pt-check` 复核实际值。准备记录不完整且没有可验证基线时，工具拒绝自动重放造数步骤。

真实隔离验收入口为 `make pt-acceptance`。它使用同一专用部署、全部固定资源和全部阶段，只将空载缩为 5 秒、预热缩为 1 秒、常规测量缩为 10 秒、稳定性每组缩为 15 秒、恢复上限缩为 30 秒。十秒失败窗口和全部业务要求保持不变。验收成功且 22 项接口全部归档后，工具才保存 `accepted.json`。准备仍生成正式流程所需语料。

`make pt` 在正式负载前检查该验收记录。源码、工具逻辑或资源定义改变后，验收记录失效，必须重新完成真实验收。模拟 fixture 不生成该记录。本次实施没有执行 `make pt-acceptance` 或正式压测。

源码指纹同时包含 backend、frontend、共享运行时、数据库迁移和镜像构建输入。修改业务源码后，不能沿用仅工具源码未变化的旧准备或验收记录。忽略目录中的凭据、构建缓存和运行日志不进入源码指纹。

固定 MySQL 总连接预算另计两个控制检查连接，分别供配置核验与环境 SQL 使用。它们不占用独立诊断账号的 4 个连接上限。每 5 秒资源采样同时复核 buffer pool、堆、镜像身份和应用配置指纹；采样耗时及失败原样保留，不自动放宽窗口。
