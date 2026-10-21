# 测试四工具

本目录复用 MySQL exporter 和官方 node_exporter。观测器只自行执行三类检查：新建连接执行 `SELECT 1`、数据库身份与数据版本及迁移校验、业务读写与异步积压恢复检查。

## 准备

```sh
bash perflab/pt4/install-system-tools.sh
python3 perflab/pt4/prepare.py
python3 perflab/pt4/run.py --group baseline-100 --rps 100 --dry-run
```

安装入口固定 Debian 13 amd64 的 matplotlib 和 Prometheus Python 客户端版本。准备入口沿用测试三快照、语料和配置，固定无 Redis、API 连接池 8，构建业务程序、探针和当前 MySQL 插件，并通过仓库打包工具下载、校验和提取官方 node_exporter。准备过程不恢复数据或启动业务负载，但需要专用测试依赖已经运行。修改源码后重新准备，不手工更新摘要。

## 执行

**正式执行会恢复专用数据库和索引、清空测试队列并产生业务写入。** 确认旧测试及恢复阶段结束，再停止旧服务。

```sh
python3 perflab/pt3/environment.py stop
python3 perflab/pt4/run.py --group baseline-100 --rps 100
python3 perflab/pt4/run.py --group reproduce-120 --rps 120
python3 perflab/pt4/stop.py --request-stop
```

每次只执行一个独立组：恢复并冒烟、再次恢复、空载 60 秒、预热 30 秒、测量 300 秒、自然恢复 600 秒。普通业务错误保留完整窗口；保护条件触发时停压。`--request-stop` 保留恢复过程；组结束后用无参数 `stop.py` 停止测试服务及遗留 exporter。核对原因后再移除 `.run/pt4/STOP`。

## 观测与结果

观测器启动专用 MySQL exporter（回环端口 19122）和官方 node_exporter（19127），每 5 秒保存一次原始输出，同时保存 API、worker 和 indexer 已有的应用指标。MySQL 插件使用专用压测实例及只读账号，不连接产品数据库。每秒用新连接执行 `SELECT 1`，每 5 秒读取数据库身份、数据 epoch、迁移和业务积压；三个数据库连接分别用于 exporter、探针和检查。

原始指标保存在 `exporter-metrics.jsonl`。HTTP 失败、分项采集失败及指标过期会标记观测失败，不能算作数据库持续不可用。采集配置、截断和 digest 丢失指标原样保存。观测器不直查 SQL 摘要或锁信息，不读取系统资源计数，也不采集日志或 perf。pprof 由下述独立命令按需采集。

主机内存、磁盘空间、OOM 和 MySQL 连接数保护读取 exporter 指标；业务积压和环境保护来自专用检查。node_exporter 提供主机指标，不提供 MySQL 容器 CPU 配额、节流或进程 RSS。原有容器内存与 k6 RSS 停压项已移除，不以主机指标代替这些指标。进程管理仍核对 PID 身份。

```sh
python3 perflab/pt4/report.py .run/pt4/observations/<run-id>
python3 perflab/pt4/plot.py .run/pt4/observations/<run-id>
```

`report.json` 保留请求窗口、错误率和分位数，并引用 `metrics-summary.json`。后者只从 exporter 输出计算计数增量和 gauge 范围；缺少窗口边界、采样间断、计数重置、序列缺失、采集失败、截断或 digest 丢失时标记证据不完整，不补零。摘要排名变化和采集配置仍需人工核对；SQL 累计耗时不等于 CPU 时间。恢复结束时要求最后一次业务读写成功、环境样本有效且全部 epoch 的未完成需求和 parked 消息归零。

## 按需使用 pprof

pprof 默认关闭。`prepare.py` 通过 Go overlay 构建三个测试专用诊断二进制，普通运行使用正常二进制。显式指定 `--profile-role server|worker|indexer` 才启用对应入口，分别监听 `127.0.0.1:19201/19202/19203`。阻塞和锁竞争采样默认均为 0；启用后会增加开销，诊断组的配置会记录到 `run.json` 和 `report.json`。

```sh
python3 perflab/pt4/run.py --group pprof-120 --rps 120 --profile-role server --block-rate 1000000 --mutex-fraction 10
# 在另一终端、需要诊断的时间段执行，使用本组实际 run-id。
python3 perflab/pt4/profiles.py --role server --seconds 30 --directory .run/pt4/observations/<run-id>/pprof
```

`--block-rate` 是阻塞采样的平均间隔，单位为纳秒；`--mutex-fraction 10` 表示平均采集 1/10 的锁竞争事件。独立命令保存 CPU、heap、goroutine、block、mutex 数据、CPU top 和摘要；CPU 采集结束后依次取得其余数据，各文件不代表同一个瞬时窗口。空 block/mutex 文件不能证明没有等待。诊断结束并完成恢复检查后，执行 `python3 perflab/pt4/stop.py` 关闭测试进程及 pprof 入口。剖析文件保存在 `.run/`，不提交 Git。

## 验证

```sh
python3 -m unittest discover -s perflab/pt4 -p 'test_*.py'
python3 -m unittest discover -s perflab/pt3 -p 'test_*.py'
go -C perflab test -race -count=1 ./...
python3 perflab/pt4/exporter_selftest.py
python3 perflab/pt4/selftest.py --case normal
```

exporter 自检创建临时 MySQL 容器和 HTTP 服务，验证真实插件、环境变化停压和进程清理，结束后删除临时容器。HTTP 负载自检不访问业务服务；这些自检不提供业务容量结论。
