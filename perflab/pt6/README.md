# PT6

业务部署使用 `deploy/compose.business.yaml`。凭据、语料、快照和每轮结果分别保存在 `.run/pt6/`，不读取其他轮次的私有状态。

前端 CPU 分配为用户端 0.13 核、管理端 0.12 核，合计 0.25 核，避免当前环境把两个 0.125 核均向上取整为 0.13 核。

在仓库根目录执行：

```sh
python3 perflab/pt6/prepare.py           # 首次部署、造数、冒烟、恢复基线、采集预检
python3 perflab/pt6/prepare.py --diagnostics  # 为已有环境准备并预检完整诊断，不恢复基线
python3 perflab/pt6/run.py --preflight   # 开测前复核，不发送业务负载
python3 perflab/pt6/run.py               # 正式压测：250 起步，通过后每档增加 50 RPS
python3 perflab/pt6/prepare.py --new-round  # 停压汇报后恢复基线、复验冒烟并重新就绪
python3 perflab/pt6/prepare.py --refresh    # 只复核环境并刷新源码指纹
```

准备失败后先检查记录和日志。修复原因后，使用 `prepare.py --resume` 继续同一准备过程。该命令只适用于准备阶段；正式压测失败后不得用它重试或覆盖现场。停压并汇报后，如需开始新一轮，使用 `prepare.py --new-round` 恢复基线并重新采集预检。

每档预热 30 秒、测量 300 秒，使用 512 VU 和 5 秒超时。首次失败立即停压并输出报告，随后最多进行 10 分钟只读恢复采集。各档分别写入新建目录，档间保留业务状态。

每 5 秒采集容器资源、业务指标、数据库状态和队列深度。队列深度取自 `rabbitmq_prometheus`（端口 15692），避免在 0.5 核 RabbitMQ 容器里反复启动 Erlang VM；`rabbitmqctl` 只用于开测前的队列清空检查。调用 `docker compose` 时会剔除宿主机上与 `test.env` 同名的变量，避免本地 `.env` 覆盖压测配置。

## 数据库与主机诊断

PT6 复用仓库 MySQL exporter 的全部 17 项采集，包括基础状态、事务、热点行锁、SQL digest、预编译执行事件、COMMIT、错误、文件 I/O、内部等待及表/索引访问。主机指标使用官方 node_exporter，采集 CPU、内存、负载、磁盘、文件系统、网络及 vmstat。PT4 的指标窗口校验、PT5 的容器资源统计和指标取值方法统一放在 `perflab/diagnostics/`，三套工具共用这些实现。

准备阶段构建 `perflab/cmd/dbdiag`，并直接准备官方 node_exporter 程序。PT6 依次检查本轮目录、`.run/tools/node_exporter/<version>/linux-<arch>/` 公共缓存、PT4 和 PT3 已下载的官方二进制；版本与架构匹配时直接复用。仅在没有可用副本时，才下载官方发布包并校验仓库固定的 SHA-256。PT6 不生成 GoPulse 插件包，也不通过 Monitor 运行采集。程序来源写入 `diagnostics.json`。

准备阶段创建独立诊断账号，并启用及复核所需 Performance Schema consumers、instruments。诊断账号仅有 PROCESS、Performance Schema SELECT 和环境表 SELECT 权限，最多使用 4 个数据库连接。准备时先保存旧诊断快照，再清空清单中的 Performance Schema 汇总和历史表，避免导入、索引重建及上一轮记录占用有界输出。该步骤不清空业务表。

已有 PT6 环境先执行 `prepare.py --diagnostics`。该命令不会恢复业务基线，也不会把已停压的环境自动改为可开测状态。下一轮仍须执行 `prepare.py --new-round`。首次准备和新轮准备均自动准备诊断。工具、共享代码、二进制、诊断 DSN 和配置均纳入就绪指纹；每次开测前会检查诊断配置和采集状态。

诊断在宿主机运行，通过 MySQL 容器私网地址建立持久连接，不增加业务服务或修改业务配额、连接池、请求超时。MySQL exporter 通过 Go 包复用采集实现，不额外启动 HTTP 服务。node_exporter 仅监听宿主机临时回环端口。两个诊断进程都由本次运行管理，阶段结束后回收。

| 频率或事件 | 保存内容 |
| --- | --- |
| 每 5 秒 | 完整 MySQL 汇总指标、主机指标、7 个应用实例指标，以及各业务容器 CPU、节流、内存和配额 |
| 每 200 ms | InnoDB 事务、等待/阻塞事务及线程 ID、表/索引/锁模式/锁数据、当前慢语句、当前等待，以及存在 PENDING 锁的对象上全部相关元数据锁 |
| 每秒 | 慢语句和错误历史、仍存活的预编译模板、文件 `sync` 事件历史 |
| 首次失败、阶段结束 | 立即请求一组现场和汇总快照；阶段结束等待末次汇总确认后退出 |
| 停压后 | 按阶段时间范围保存服务日志，每个服务最多保留最后 10,000 行；恢复期继续采集诊断和资源，最多 10 分钟，不发送业务负载 |

每档新增以下文件。恢复期文件单独保存在运行目录的 `recovery/` 中，汇总结果由 `recovery-result.json` 引用。

- `database-diagnostics.jsonl`：MySQL 原始指标及现场。记录查询失败、截断、起止时间和单次采集耗时。
- `diagnostics-summary.json`：正式窗口的指标增量、COMMIT 均值、两个 API 连接池各自及合计等待、文件 I/O、行锁、各容器资源和证据完整性。
- `failure-trigger.json`：首次客户端失败及工具发现该事件的时间。该文件可用于区分原始失败与停压取消的后续请求。
- `diagnostic-runtime.json`：采样数、进程退出状态和观察进程 CPU 开销。子进程 CPU 项包含本轮 Docker 客户端。
- `service-*.log`、`service-logs.json`：服务日志和时间范围、截尾上限及读取失败清单。

诊断进程退出、分项采集失败或心跳超时会触发停压。汇总发现缺少边界、计数重置、采样间隔过长、配额变化或输出截断时，会标为 `incomplete`，该档不能判为通过。`load_passed` 单独记录负载是否通过，`passed` 要求负载与诊断均通过。报告用 `stopped_rps` 记录停压档位；负载已通过而诊断不完整时，`failed_rps` 为空，`load_passed_rps` 保留已通过负载的最高档位。

node 指标仅对一种情况作例外处理：veth 的计数序列在所有前序窗口样本中连续存在，且整个设备只在测量结束后的收尾边界样本消失。此时将相关序列列入 `omitted_series`，不补零或计算完整窗口增量。窗口内缺样、同一设备仅部分指标缺失、稳定网卡缺失和计数重置仍使证据不完整。原始指标全部保留。

恢复期先取得首个资源样本，再确定观测开始时间；结束时间在末次采样之前确定，确保各源有前后边界。资源汇总使用各容器实际配额，两个 backend 的 API 池分别统计，合计值不包含 outbox 池。历史查询发生的真实截断仍保留 `scene_truncated`，不因负载已通过而忽略。

采集存在以下边界，分析报告必须保留：

- 现场查询依次执行，不构成原子快照；200 ms 采样可能遗漏更短事件。
- 一般查询最多保存 128 行，配置查询最多保存 1,024 行；SQL 文本最多保存 2,048 个字符。原始记录明确标记行数截断。历史缓冲区也可能在两次采样之间覆盖记录。
- 当前执行可能没有 SQL 文本。预编译模板按线程保存，不能把同线程中的任意模板认定为正在执行的 SQL。
- 锁持有线程的 current 语句可能已经结束，须检查对应 `END_EVENT_ID`，并用事件 ID 区分取得锁的语句和当前语句。
- 元数据锁记录保留同一对象上 PENDING 和 GRANTED 的状态。持有者是候选阻塞者，须结合锁类型兼容性判断。
- `file_io` 的 `misc` 包含多种文件操作，不能直接等同于 fsync。`sync_history` 仅表示实际观察并去重的同步事件，可能包含窗口前的历史，不能视为窗口完整总量。
- 指标窗口采用前后边界样本，最多向前后各扩展 10 秒；实际扩展量和缺样原因写入汇总。诊断采集本身有开销，应结合单次查询耗时、主机指标和观察进程 CPU 判断。

凭据、SQL、锁数据和日志均保存在 `.run/pt6/` 私有目录，不提交 Git。历史运行不会因工具升级而自动获得缺失的诊断记录。

创建 `.run/pt6/STOP` 可停止当前压测及恢复观测。下一轮必须先核对现场，再人工移除该标记。

工具自检使用模拟 API，不访问业务系统：

```sh
python3 perflab/pt6/test_tools.py -v
python3 perflab/pt6/test_diagnostics.py -v
node --test perflab/k6/sequence.test.mjs
```

`dbdiag` 的真实 MySQL 集成测试仅接受名为 `gopulse_diag_fixture` 的独立数据库。DSN 文件须指向单独的测试实例；测试会创建 fixture 表、账号和锁等待，不得指向业务环境：

```sh
GOPULSE_DIAGNOSTIC_TEST_DSN_FILE=/absolute/path/to/fixture.dsn go -C perflab test -race ./cmd/dbdiag
```
