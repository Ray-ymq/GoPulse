# PT6

业务部署使用 `deploy/compose.business.yaml`。凭据、语料、快照和每轮结果分别保存在 `.run/pt6/`，不读取其他轮次的私有状态。

前端 CPU 分配为用户端 0.13 核、管理端 0.12 核，合计 0.25 核，避免当前环境把两个 0.125 核均向上取整为 0.13 核。

在仓库根目录执行：

```sh
python3 perflab/pt6/prepare.py           # 首次部署、造数、冒烟、恢复基线、采集预检
python3 perflab/pt6/run.py --preflight   # 开测前复核，不发送业务负载
python3 perflab/pt6/run.py               # 正式压测：250 起步，通过后每档增加 50 RPS
```

准备失败后先检查记录和日志。修复原因后，使用 `prepare.py --resume` 继续同一准备过程。该命令只适用于准备阶段；正式压测失败后不得用它重试或覆盖现场。

每档预热 30 秒、测量 300 秒，使用 512 VU 和 5 秒超时。首次失败立即停压并输出报告，随后最多进行 10 分钟只读恢复采集。各档分别写入新建目录，档间保留业务状态。

创建 `.run/pt6/STOP` 可停止当前压测及恢复观测。下一轮必须先核对现场，再人工移除该标记。

工具自检使用模拟 API，不访问业务系统：

```sh
python3 perflab/pt6/test_tools.py -v
node --test perflab/k6/sequence.test.mjs
```
