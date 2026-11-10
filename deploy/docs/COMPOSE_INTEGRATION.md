# Compose 整合与验收记录

## 范围与基准

本记录对应 `Plan.md` 的第一阶段，分支为 `feat/deploy-compose-integration`。本次不修改 `main`，不迁入 Kubernetes 分支的旧环境清理动作。

实施前核对结果：`main`（`707ac08c`）与 `34ce7889` 的文件树均为 `5e4e307ffc99ea7c74a8eb56bb3dcef9060d940d`。以下 7 个提交按原顺序迁入，没有发生冲突。

| 源提交 | 集成提交 | 改动 |
|---|---|---|
| `67c36c71` | `ec809a21` | 后端、自监控与工程结构 |
| `4e4b775d` | `b2b4bad1` | 后端系统与工程工具目录 |
| `1ecba7d7` | `dff90613` | 诊断采集、压测与路径适配 |
| `ca3b0878` | `7fbc60cf` | 项目介绍与开发入口 |
| `3aaf862e` | `6b82a41c` | Compose 部署与开发配置目录 |
| `585e62ca` | `543b783d` | Compose 部署说明 |
| `49534f4a` | `18d4244d` | 三种 Compose 部署模式 |

迁入后执行 `git diff --quiet main-docker HEAD`，确认 `18d4244d` 的文件树与 `main-docker` 完全一致。后续只补充本次验收记录，并修正压测 fixture 文档中的 `PYTHONPATH` 路径。

## 静态检查与构建

验证日期为 2026-11-02。以下结果来自当前集成分支，未沿用源提交中的历史验证结论。

| 检查 | 实际结果 |
|---|---|
| 根 Makefile 注册的 13 个 Go 模块 | 每个模块的 `make ci-check`、`make ci-test`、`make ci-build` 均通过；包括 lint 和竞态测试 |
| `frontend/user` | `make check/lint/test/build MODULE=frontend/user` 均通过；79 项测试通过 |
| `frontend/admin` | `make check/lint/test/build MODULE=frontend/admin` 均通过；70 项测试通过 |
| `make ci-tools` | 113 项测试通过 |
| `make check-commits BASE_REF=main` | 迁入的 7 个提交通过校验 |
| `make check-compose` | 三种产品配置，以及开发、调试、追踪和自监控覆盖配置通过渲染检查 |
| 独立自监控配置 | 使用 `selfmonitor.env` 渲染其 Compose 配置，通过；Prometheus v3.5.0 的 `promtool check config --syntax-only` 通过，未校验真实凭据或目标连通性 |
| 运行契约 | 用 `jsonschema.validate` 校验 `deploy/runtime-contracts.json` 与对应 Schema，通过 |
| `make build-images` | 七种产品镜像均构建并加载到本机 Docker；镜像中的源码修订为 `18d4244d` |
| CI 模块计划 | 显式传入全部 13 个 Go 模块标签后，`make ci-plan` 成功 |
| 实际 k6 fixture | `GOPULSE_PT_K6_FIXTURE=1 PYTHONPATH=tools/perflab python3 -m unittest flow.test_k6_fixture -v` 通过；未运行正式压测 |

本机原始日志位于 `.run/compose-integration-validation/`。该目录不进入 Git，账号、Cookie 和私有配置不写入本记录。

## 产品验证方法

使用独立项目 `gopulse-compose-review-business`、`gopulse-compose-review-observability` 和 `gopulse-compose-review-all`，分别对应三份产品 Compose 文件。环境文件单独保存为 `.run/compose-integration-validation/product.env`，未覆盖仓库已有 `.env`。访问端口为业务及整体模式 `25173`、独立可观测模式 `25174`。

每种模式按以下顺序检查：

1. 执行 `up -d --wait --wait-timeout 300`，检查常驻服务健康和初始化任务成功退出。
2. 经产品 HTTP 入口注册账号，使用该模式的 `admin-role promote --username <测试用户名>` 声明初始管理员，再登录。
3. 检查管理总览、组件状态和自监控状态 API。业务及整体模式另检查发帖和读取帖子。
4. 执行 `down`，保留命名卷；重新执行 `up`，确认账号、管理员权限，以及适用模式中的帖子仍然存在。
5. 再次执行 `down`，保留该测试项目的命名卷。

自监控状态 API 返回 HTTP 200，仅表示接口可访问，不代表自监控采集和启停链路通过。

| 模式 | 启动与 HTTP 检查 | `down` 后重建 |
|---|---|---|
| business | 通过；包含管理员初始化、管理查询、发帖与帖子读取 | 账号、管理员权限和帖子保留 |
| observability | 通过；包含独立管理入口、管理员初始化和管理查询 | 账号和管理员权限保留 |
| all | 通过；包含管理员初始化、管理查询、发帖与帖子读取 | 账号、管理员权限和帖子保留 |

`make integration SCOPE=business` 和 `make integration SCOPE=observe` 均通过，隔离依赖容器已由工具清理。观测测试使用源码进程执行 `TestObservabilityFlowIntegration`，不能代替三种产品模式各自的观测链路验收。此次部署重建检查和源码集成检查使用不同测试数据，不读取现有 Kubernetes 数据。

## 本机环境处理

开始产品验证时，现有 `gopulse-all` Kubernetes 工作负载仍在运行。用户明确允许停止这些工作负载，并保留 PVC、Secret 和凭据。

停止前，将 Deployment、StatefulSet 的副本数和 DaemonSet 的节点选择器记录在 `.run/compose-integration-validation/kubernetes-workloads-before.json`。随后将 Deployment、StatefulSet 缩容到零，为 `selfmonitor-node` 增加临时节点选择条件，使其停止运行。没有删除命名空间、工作负载定义、PVC 或 Secret。检查到的 12 个 PVC 均保持 Bound。

全部 Compose 产品测试和源码集成测试结束后，已恢复原副本数，并移除 DaemonSet 的临时节点选择条件。原 StatefulSet、Deployment 和 DaemonSet 的 rollout 状态均通过检查；原先为零副本的工作负载仍保持零副本。Kubernetes 运行的仍是原有镜像，不是本分支新构建的镜像。三个 Compose 验收项目已停止，命名卷保留。

## 升级与回滚边界

现有部署升级前必须备份数据库，并保留原项目名、环境文件和命名卷。首次在独立测试项目中应用迁移成功，不能代替对现有数据执行升级验收。

迁移 `000016_independent_selfmonitor` 会修改匹配旧进程指标前缀的 Metrics 告警：关闭正在触发的告警事件，记录 `selfmonitor_moved`，禁用对应规则状态，清理待触发时间和租约，并禁用仍启用的规则。已启用规则的 revision 增加。规则匹配以迁移 SQL 中的 source 和 metric 前缀条件为准。

该迁移的 down 脚本仅执行 `SELECT 1`，不会重新启用规则、恢复规则状态或重新打开告警事件。回滚应用或执行 down 迁移均不能自动恢复这些状态；需要按备份恢复流程处理，或由管理员逐项审核后恢复规则。迁移脚本见 `backend/business/migrations/000016_independent_selfmonitor.up.sql` 和对应 down 文件。

Compose 命名卷与 Kubernetes PVC、Secret 和凭据分别保存。切换部署方式不会自动迁移或同步数据。本次没有对旧部署执行数据清理。

## 后续验收与评审

第一阶段的迁入完成，不表示整个双部署计划已经完成。Docker 整合验收与评审完成后，才从本分支结果创建 `feat/deploy-kubernetes-support`。

当前分支保持原 Docker 命令行为：`stack-*` 仍只管理整体系统，没有新增 Kubernetes 命令，也没有完成统一互斥锁或 `MODE` 分发。这些接口改造属于后续双部署整合，不能将本分支描述为已经支持双部署。

本机 Compose 自监控的三个固定 systemd 单元处于 masked 状态。本次没有解除屏蔽、覆盖 root 管理的部署目录或修改已有私有凭据。因此，管理页的自监控启动、停止、Prometheus 目标、数据源健康及控制权限失败场景仍需实际验收。

插件安装和配置持久性、指标/日志/事件完整链路、浏览器操作、双向切换、并发启停、残留状态恢复、WSL 重启、节点地址变化与可选追踪链路均不得写成已通过。真实专用业务环境的缩短压测全流程也未执行。
