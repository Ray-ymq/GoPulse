# 测试五工具

本目录在测试四流程外补充 MySQL CPU 配额核验、5 秒容器 CPU/节流采样和按资源 ID 的写入核对。负载、数据、exporter、恢复流程和基线仍由测试四提供，测试五不另建一套环境。

## 准备

测试四环境必须已准备；测试五新增或修改 `perflab` 源码后要重新执行测试四准备入口，使 `prepared.json` 的源码摘要与当前工作区一致。

```sh
python3 perflab/pt4/prepare.py
python3 perflab/pt5/run.py --group cpu2-120 --rps 120 --cpus 2 --preflight
python3 -m unittest discover -s perflab/pt5 -p 'test_*.py'
```

`--preflight` 只设置并核验配额、采样 11 秒，不恢复数据、不启动负载。正式执行前确认旧组已结束、测试服务已停止，且不存在 `.run/pt4/STOP`。

## 执行

```sh
python3 perflab/pt3/environment.py stop
python3 perflab/pt5/run.py --group cpu2-120 --rps 120 --cpus 2
python3 perflab/pt5/run.py --group cpu4-120 --rps 120 --cpus 4
python3 perflab/pt5/run.py --group cpu4-140 --rps 140 --cpus 4
python3 perflab/pt5/run.py --group cpu2-140 --rps 140 --cpus 2
python3 perflab/pt4/stop.py
```

每组依次执行：设置并核验配额 → 恢复并冒烟 → 再次恢复 → 清空性能模式汇总 → 空载 60 秒 → 预热 30 秒 → 测量 300 秒 → 恢复 600 秒 → 核对组后配额。**正式执行会恢复专用数据库和索引、清空测试队列并产生业务写入。**

配额只允许 2 或 4 vCPU。`quota.json` 同时记录 docker 配置与 cgroup 实际值，并核验内存、内存交换上限和重启策略。组间调整配额必须在上组恢复结束后进行。

## 采样与窗口

`resources.jsonl` 每 5 秒保存 `cpu.stat`、`cpu.max`、`memory.current`、`memory.peak`、`memory.events` 和 `pids.current`。`resources-summary.json` 使用 `report.json` 的实际正式窗口，取窗口前后边界样本计算：

- 平均 CPU 使用率与 5 秒瞬时峰值；
- 节流周期数、节流周期比例、累计节流时间和节流时间比例；
- 内存峰值、cgroup OOM kill；
- 窗口内 `cpu.max` 是否保持预期配额。

采样间断超过 10 秒、边界样本距离窗口超过 10 秒、配额变化或计数回退时标记证据不完整，不补零。

## 写入核对

`audit.py` 从 `events.log` 读取正式窗口内开始的写请求：

- `POST /posts`、`POST /posts/:postId/comments`：按响应 `response_id` 查行，校验作者和标题/正文。
- `PATCH /posts/:postId`：取同一资源窗口内最后完成的成功更新，校验 `edited_at`、`content_revision` 和内容。
- `DELETE /posts/:postId`：幂等 204/404 均要求最终行不存在。
- 点赞、书签、关注：按最后完成的 PUT/DELETE 计算预期存在性，与 `post_likes`、`post_bookmarks`、`user_follows` 比对。
- 窗口内数据库新建行必须能对应到请求；无法对应记为不匹配。
- 窗口前开始、窗口内完成的请求，以及窗口内开始、窗口后完成的请求单独列出，不计入窗口内差额。

恢复结束后才执行核对查询，不进入正式窗口指标。`write-audit.json` 的 `failures` 汇总缺失行、内容不一致、互动最终状态不一致、无请求对应的新建行、应用结果失败和孤儿行。

## 汇总

```sh
python3 perflab/pt5/analyze.py
```

读取 `.run/pt5/groups/` 下四组清单，输出错误率、成功 RPS、P95/P99、CPU、节流、COMMIT、连接池等待、行锁等待和写入核对对比，写入 `.run/pt5/comparison.json`。判断规则见 [测试五方案](../../PT/05-测试五/2026-10-21-测试五-CPU配额对照测试方案.md)。

## 验证

```sh
python3 -m unittest discover -s perflab/pt5 -p 'test_*.py'
python3 -m unittest discover -s perflab/pt4 -p 'test_*.py'
go -C perflab test -race -count=1 ./...
```
