# 固定 PT 流程

统一入口为 `make pt`，连续加压默认从 10 RPS 开始。`START_RPS=250` 只改变连续加压起点。扫描上限为 6400 RPS，达到上限不代表找到系统极限。

`config.json` 定义固定流程，`resources.json` 是资源唯一来源。执行器由资源配置生成 Compose 覆盖文件，并核对部署值和 cgroup 实际值。系统余量及应急预留不创建容器。Redis 全程启用，禁止自动调参和单因素对照。

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

Go 工具 `pt-load` 接收明确任务，负责翻倍、二分建议、业务判定及吞吐平台判断。k6 统一使用 `flow/workload.js`，复用公共请求和路由定义。连续档位保留语料序号；请求记录包含开始、完成和停压取消，登录准备不进入业务窗口。最近 10 秒失败率严格超过 50% 才停止当前组，窗口未满或没有完成请求时不填零。延迟、普通业务失败和丢弃迭代不直接触发首次失败停压。压测器无有效完成请求、语料耗尽或保护状态失效会结束当前运行并保留证据。
