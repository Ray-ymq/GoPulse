# 双部署合并候选验收

本记录的产品源码候选为 `f95c80f277d4bc0084908eadfb28412bbe0e9f04`，main 基线为 `707ac08cc3d7d6e5374cb4cbf2dbf3a1392d2dbf`。PR 为 [#255](https://github.com/Ray-ymq/GoPulse/pull/255)。后续验收资料提交不改变产品代码或镜像构建定义。

## 已执行的检查

- 全部 13 个 Go 模块执行 `make ci-check`、`make ci-test`、`make ci-build MODULE=...`。首轮 perflab 测试受到真实工作区锁影响；修复单测隔离后，该模块三项检查通过。
- 两个前端分别执行 `make check/lint/test/build MODULE=...`，均通过。用户端 79 项、管理端 70 项测试通过。
- `make ci-tools` 的 113 项测试、`make deployment-tests` 的 34 项测试、`make check-compose`、`make check-runtime-contracts`、提交规范与差异空白检查通过。
- PR 显式选择全部 13 个 Go 模块。[候选 CI](https://github.com/Ray-ymq/GoPulse/actions/runs/42033430896) 全部通过。干净 runner 曾因缺少 `prometheus_client` 失败；已声明固定版本并在 CI 虚拟环境安装，本机干净虚拟环境和远端复验通过。
- `GOPULSE_PT_K6_FIXTURE=1 PYTHONPATH=tools/perflab python3 -m unittest flow.test_k6_fixture -v` 通过。本次没有执行正式压测，不声明性能或容量验收通过。

## 共享产物

七种镜像由候选 SHA 执行 `make build-images` 构建。不可变标签和本地 OCI 摘要见 [镜像清单](../validation/candidate-images.json)。Compose 使用摘要引用，并核对运行容器的镜像身份。Kubernetes 导入前将同一批摘要恢复到候选标签，避免重复构建更新标签后混用产物。镜像保存在本机；本次未发布到镜像仓库。

## 实际部署

Compose 的 business、observability、all 均完成 `stack-up`、`stack-verify`、管理员初始化、适用的业务和观测链路、`stack-down` 后重新启动及持久性检查。三种模式运行时，Kubernetes 启动均被拒绝。

Compose 整体模式的自监控完成 API 重复启停、19 个采集目标、数据源健康、控制权限失败与恢复检查。Prometheus 历史数据和 Grafana 用户设置在重启后保留。验收后恢复原三个 systemd 单元的 masked 状态。

迁移失败注入使初始化任务退出码为 31，应用未运行。验收脚本曾覆盖自身 Compose 覆盖文件，导致清理失败；随后使用原项目名与基础 Compose 文件完成停止，命名卷保留。这是验收脚本问题，未修改产品部署代码。

工程回归全部通过：两类 integration、dev、dev-observe、源码运行时拒绝 Kubernetes，以及并发启停的工作区锁检查。

Kubernetes 三种模式均完成启动、管理员初始化、适用业务与观测链路、自监控 API 启停、Pod 重建、停止再启动及持久性检查，并拒绝 Compose 启动。整体模式的新 span 与追踪 PVC 读取、权限撤销、目标缺失、数据源认证失败及恢复、凭据冲突、从 Secret 恢复本地凭据、make stop 保留产品工作负载均通过。

已沿 OCI 索引、平台 manifest 和 config 摘要核对 Kubernetes 的 imageID，确认两套部署使用同一批镜像内容。三种模式原 PVC、Secret UID 与 Secret 值均保留。验收产品环境已停止，持久数据保留。命令与退出码见 [验收结果](../validation/candidate-results.json)。

## 数据库升级与恢复

隔离 MySQL 使用 main 的完整 schema 15 和 9 组代表性告警数据，先备份，再还原到独立副本，使用候选镜像中的迁移程序升级到 schema 20。此数据为旧 schema 的测试样本，不是生产数据库副本。

已核对迁移 000016 对五类旧进程指标规则的禁用、revision 增加、触发事件关闭及状态清理。已禁用规则、已恢复事件和不匹配规则分别核对。down SQL 没有恢复这些状态；还原备份后，完整数据库导出与升级前备份逐字节一致。私有 SQL、账号、凭据和原始日志保留在 `.run/merge-final/`，不入库。

## 范围与回滚

本次交付代码和现有支持能力，不包含生产切换、跨部署数据迁移或多节点支持。Kubernetes 支持范围为 Debian WSL2 单节点。两套部署停止时保留命名卷、PVC、Secret 和凭据。

环境升级前仍需对实际旧数据备份并验证恢复。应用回退及迁移 down 都不能恢复已修改的告警状态。代码合并采用 merge commit；需要回退时，通过 PR 回退该 merge commit。代码回退不会自动回滚环境或数据库。

真实 WSL 重启和在线节点地址迁移的历史证据见 [整合验收记录](DUAL_DEPLOYMENT_VALIDATION.md)。本轮未重做这两项，也未覆盖旧地址突然失效或 etcd 完全不可访问。
