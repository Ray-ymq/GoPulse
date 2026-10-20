# 测试四工具

业务步骤见[测试方案](../../PT/04-测试四/2026-10-20-测试四-数据库专项诊断与稳定性测试方案.md)，实际检查和限制见[验证记录](../../PT/04-测试四/2026-10-20-测试四-工具准备与验证记录.md)。本文件只说明操作入口。

## 准备

```sh
bash perflab/pt4/install-system-tools.sh
python3 perflab/pt4/prepare.py
python3 perflab/pt4/run.py --group baseline-100 --rps 100 --dry-run
```

安装入口适用于 Debian 13 amd64，版本见 [system-tools.json](system-tools.json)，k6 固定为 0.56.0。准备命令将测试三基线、语料和环境复制到私有 `.run/pt4/`，固定无 Redis、API 连接池 8，构建普通和诊断二进制，并生成摘要清单。它增加受限只读采样账号，不恢复业务数据或启动业务负载。修改受记录源码后重新准备；不要手工改摘要。

## 执行与停止

**正式执行会恢复专用测试数据库和索引、清空测试队列并产生业务写入。** 确认测试三及其恢复阶段结束，再停止旧服务。仍有旧测试进程或保护标记时，入口拒绝执行。

```sh
python3 perflab/pt3/environment.py stop
python3 perflab/pt4/run.py --group baseline-100 --rps 100
python3 perflab/pt4/run.py --group reproduce-120 --rps 120
python3 perflab/pt4/stop.py --request-stop
```

每次只执行一个独立组：恢复并冒烟、再次恢复、空载 60 秒、预热 30 秒、测量 300 秒、自然恢复 600 秒。普通业务错误保留完整窗口；资源、身份和观测保护优先停压。`--request-stop` 保留恢复流程；组结束后用无参数 `stop.py` 停止已核对身份的测试四服务。核对停压原因后，下一组前手动移除 `.run/pt4/STOP`。

## 采集和报告

独立探针每秒采样，critical/locks 每 5 秒、digest 每 10 秒；常规最多 4 个专用连接，另预留人工诊断 1 个。各查询独立超时 2 秒，不叠加周期。关键状态 8 秒无有效样本或诊断连续 3 个周期缺失会停压；诊断缺失不等于数据库故障。

```sh
python3 perflab/pt4/report.py .run/pt4/observations/<run-id>
python3 perflab/pt4/plot.py .run/pt4/observations/<run-id>
```

每组目录保存请求开始/结果、原始指标、资源、SQL/锁、恢复及复算报告。SQLite 复算按请求开始窗口归组，超时和已知未完成计为失败；日志截断标记计数不完整。`business_short_window_pass` 只是内置业务阈值复算，不表示瓶颈已经归因或完整验收通过。图表离线生成 PNG/SVG。

SQL 按 schema+digest 保存全量快照并求增量，边界外时间单侧超过 12 秒时证据不足。重启、计数重置、摘要消失或丢失计数变化使比较失效。锁及线程超过 200 行标记截断；PID 可识别服务进程，不能区分同一 API 进程内的 outbox。当前 SQL 不一定是之前的持锁 SQL。

## 按证据启用的诊断

单路由复测使用 `run.py --route-set feed/detail/comments/user/write`，其他选择器见 `node perflab/k6/test4-plan.mjs --routes`。诊断组成不同的结果单独报告。

MySQL CPU 栈在已核对身份的测试容器中使用软件 cpu-clock、49 Hz、DWARF，保存原始数据、文本报告和展开栈：

```sh
python3 perflab/pt4/profiles.py mysql --seconds 30 --directory .run/pt4/observations/<run-id>/profiles/mysql
```

Go 剖析组额外设置 `run.py --profile-role server/worker/indexer`，在组内另一个终端用 `profiles.py go --role <role> --seconds 30 --directory <path>` 采集。诊断构建通过 Go overlay 加入回环监听，端口分别为 19201/19202/19203；普通二进制不含该入口。block/mutex 默认关闭，需要时设置 `PERFLAB_BLOCK_RATE` 和 `PERFLAB_MUTEX_FRACTION`，记录开销。空 profile 或未解析符号不能证明没有热点。

## 工具自检

```sh
node --test perflab/k6/*.test.mjs
python3 -m unittest discover -s perflab/pt4 -p 'test_*.py'
python3 -m unittest discover -s perflab/pt3 -p 'test_*.py'
make ci-check MODULE=perflab
go -C perflab test -race -count=1 ./...
python3 perflab/pt4/db_selftest.py --perf
python3 perflab/pt4/selftest.py --case normal
```

HTTP 模拟只访问临时回环服务；数据库自检创建独立 MySQL 容器并在结束后删除。常规 Go 测试未提供 fixture DSN 时跳过数据库集成用例，自检命令会实际执行。它们都不提供业务容量结论。runner 被强杀或宿主退出时，无法保证完整恢复观测。
