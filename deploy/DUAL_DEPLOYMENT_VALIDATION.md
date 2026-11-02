# 双部署整合验收记录

## 分支与提交

本记录对应 `Plan.md` 第二阶段。第一阶段分支为 `feat/deploy-compose-integration`，验收提交为 `9101c667`；第二阶段分支为 `feat/deploy-kubernetes-support`。`main` 保持 `707ac08c`，未直接合并或推送。

参考来源为 `main-docker..main-kubernetes` 的六个提交：`a62a7107`、`d965ed4b`、`5c9bcf06`、`82dc6c00`、`9ee064f5`、`ce5459a1`。仅选择产品部署、集群工具、镜像导入、受限自监控、启动依赖及恢复检查。不迁入旧环境清理、Kubernetes 源码开发、集成依赖或固定压测。

| 提交 | 内容 |
|---|---|
| `98b0e613` | 产品清单、共享镜像定义和双部署运行契约 |
| `5e1e7cc8` | 显式选择 Compose 或 Kubernetes 自监控控制实现 |
| `fa722033` | 三模式生命周期、统一互斥、资源归属、凭据恢复和工程入口 |
| `2b7e9948` | Kubernetes 网络配置隔离、插件包缓存及真实部署修复 |

本记录所在提交还包含 Grafana 配置加载隔离、自监控停止范围、Compose 资源标签和指标等待窗口修复。

原始日志和私有验收数据位于本机 `.run/dual-deploy/`，不提交到 Git。共享镜像的源码 revision 为 `2b7e9948f0e9`；产品版本为 `2.6.5`。

## 静态检查与构建

- 全部 13 个 Go 模块均执行 `make ci-check`、`make ci-test`、`make ci-build` 并通过；Go 测试包含 race 检查。两个前端的检查、测试、构建通过，用户端 79 项测试，管理端 70 项测试。第二阶段 PR 应显式选择 `ci:backend/business` 和 `ci:tools/devtools`；本机已验证这两个标签的模块计划。
- 上述 45 项模块检查及 `make ci-tools` 共 46 项结果均成功。后续缓存修复重新执行 devtools 检查、测试和构建。
- `make check-compose` 和 `make check-runtime-contracts` 通过。覆盖三种 Compose 配置、三种 Kubernetes 清单、两套契约绑定、Kubernetes 前端路由、自监控目标及 Grafana 数据源地址。
- 工作区互斥测试 11 项、Kubernetes 安全测试 18 项通过，包含并发锁、双向冲突、资源归属、未完成压测、Secret 恢复、凭据冲突和迁移失败后不启动应用。
- 七种共享产品镜像构建成功，同时生成 `latest` 和不可变标签。独立模块重建的七种插件包清单与原发布包一致，包括二进制摘要。没有放宽已安装插件的摘要校验。
- `k8s-check` 通过；`k8s-cluster-verify` 实际验证节点、DNS、Service、镜像及 Pod 重建后的 PVC 读写。

## 实际产品验收

Kubernetes 三种模式均完成真实部署、功能验收、Pod 重建、停止后再启动和持久性检查。各模式完成管理员初始化，业务模式验证发布、幂等、点赞、评论、异步通知及搜索；可观测模式验证插件安装及启停、新指标、日志和生命周期事件。整体模式同时验证两条链路。

Kubernetes 三种模式的管理 API 均完成自监控停止、启动和 Pod 重建，目标数分别为 business 11、observability 10、all 16。整体模式使用 `TRACE=1`，本次请求的新 span 和追踪 PVC 读取检查通过。

| 部署方式 | business | observability | all |
|---|---|---|---|
| Compose | 功能、容器重建及持久性通过 | 功能、容器重建及持久性通过 | 两条链路、容器重建及持久性通过 |
| Kubernetes | 功能、Pod 重建及持久性通过 | 功能、Pod 重建及持久性通过 | 两条链路、Pod 重建、持久性和追踪通过 |

Compose 三种模式运行期间，实际执行 Kubernetes 启动均被互斥检查拒绝。Compose 整体模式的管理 API 完成重复启动、重复停止和重启；19 个目标健康。实际停止一个 Marshaller 后显示降级；MySQL Exporter 使用错误密码时，采集端点仍全部健康，但数据源检查显示降级；临时取消控制 socket 的组访问权限后显示不可用。恢复配置或权限后均恢复健康。

Compose 自监控停止再启动后，Prometheus 历史查询和 Grafana 用户设置保留。原三个 systemd 单元已恢复为 masked，验收产品环境已停止，命名卷保留。

Kubernetes 整体模式实际验证了以下场景，均通过：

- 临时撤销控制 Role 权限，查看与停止均显示不可用，未误报已停止；恢复权限后恢复健康。
- 临时移除 Backend 采集目标，管理 API 显示降级；恢复目标后恢复健康。
- MySQL Exporter 使用错误密码时，16 个采集端点仍全部健康，但数据源检查显示降级；恢复凭据后恢复健康。
- 工程命令停止后再启动自监控，DaemonSet 正常恢复，Prometheus 历史查询、Grafana 用户设置及自监控 Secret 均保留。
- 本地数据库凭据与 Secret 不一致时，启动被拒绝，原运行 Pod UID 不变；临时移走业务模式本地凭据文件后，可从 Secret 恢复原值。
- Kubernetes 运行时执行 `make stop`，产品与自监控工作负载的期望副本数保持不变。

最终核对原整体模式 12 个 PVC 和 2 个 Secret，名称及 UID 均保留。业务模式恢复出的两份凭据文件与原备份值一致。Kubernetes 整体模式、自监控及追踪保持运行，最终 `k8s-check` 通过。业务模式、可观测模式和 Compose 验收环境均已停止，持久数据保留。

首次真实部署暴露了前端 Docker DNS、自监控地址配置和插件缓存依赖不一致，均已修复后重新验收。Compose 自监控验收还发现 Kubernetes 专用 Grafana 配置被 Compose 自动加载，造成两个默认数据源。已将 Kubernetes 配置移出 Compose 自动加载目录，并增加默认数据源数量及地址检查。整体模式保留的 Kafka 历史消息导致最新插件指标超过原 120 秒等待窗口；消费积压清零后，延长至 600 秒的指标验收通过。未重置消费位点，也未清空历史消息。

## Compose 工程回归

`make integration SCOPE=business`、`make integration SCOPE=observe` 均通过。`GOPULSE_PT_K6_FIXTURE=1 PYTHONPATH=tools/perflab python3 -m unittest flow.test_k6_fixture -v` 实际运行 k6 容器并通过；未执行正式压测。

`make dev`、`make dev-observe` 均成功启动，通过工具的就绪检查，并用 `make stop` 停止。两种源码模式运行时，实际执行 `make k8s-up MODE=all` 均被互斥检查拒绝。集成检查持有工作区锁时，实际 Compose 和 Kubernetes 并发启动也都被拒绝。

首次源码启动遇到本机 `3306` 端口占用；原 `.env` 还保留旧 Kafka Topic 和消费组名称。验收使用独立私有文件 `.run/dual-deploy/source.env` 设置空闲端口和当前 Topic、消费组后重试成功，未覆盖原 `.env`，未停止占用端口的其他服务。

## 环境与数据保留

执行前备份现有整体模式数据库，并记录 PVC、Secret UID。数据库备份保存在 `.run/dual-deploy/preflight/mysql.sql`，权限为 `0600`。原始 Kubernetes 工作负载按用户授权停止，未删除原 PVC、Secret 或本地凭据。

Compose 验收使用独立项目与私有环境文件。整体模式使用固定项目 `gopulse`，以便验证既有 systemd 自监控控制边界。原 `.env`、`/opt/gopulse-selfmonitor` 和旧安装文件不覆盖。自监控真实验收使用 root 管理的临时配置和运行时单元，结束后恢复原三个 systemd 单元的 masked 状态。

数据库升级及迁移 `000016` 的状态变更与回滚限制见 [第一阶段记录](COMPOSE_INTEGRATION.md#升级与回滚边界)。本次不通过删除数据或修改插件摘要来恢复部署。

## 未完成的外部验收

本次会话未执行 Windows 端的实际 WSL 关机重启，也未更改真实节点地址。这两项仍需维护窗口验收，不能以单元测试、服务重启或既有历史记录替代。地址检查及恢复边界见 [双部署说明](DUAL_DEPLOYMENT.md#wsl-与节点地址恢复)。

Kubernetes 固定压测不属于本次交付；本次不将其历史验收缺口记为通过。正式压测继续使用 Compose。
