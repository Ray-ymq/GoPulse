# Compose 与 Kubernetes 双部署

产品同时保留 Compose 和 Kubernetes 部署。两种方式各有 business、observability、all 三种模式；同一工作区同一时间只启动一种产品环境。源码开发、集成检查和正式压测继续使用 Compose。

## 共用构建与配置

`deploy/images.json` 是七种产品镜像的构建定义。执行 `make build-images` 时，工具先准备插件目录，再使用该定义构建镜像。构建过程不连接 Kubernetes 集群。每份产物同时标记为 `gopulse/<组件>:<VERSION>-<Git revision 前12位>` 和 `gopulse/<组件>:latest`。

Compose 默认使用 `latest`，也可以通过已有 `GOPULSE_*_IMAGE` 变量指定不可变标签。Kubernetes 按当前源码 revision 导入和部署不可变标签。产品 Monitor 镜像包含插件目录；源码开发仍可单独构建不含该目录的 `monitor` 阶段，并挂载源码工具准备的插件目录。

`deploy/runtime-contracts.json` 保留原 Compose 字段、实例约束、资源预算和压测约束，并新增 `kubernetes_workload`、`kubernetes_workloads`、`kubernetes_replica` 和环境键的 `kubernetes_value`。Kubernetes 使用自己的工作负载地址和副本配置，不替换 Compose 的实例列表。

公共环境变量示例仍在根 `.env.example`。Kubernetes 产品凭据位于 `.run/kubernetes/<MODE>/credentials.json`，自监控凭据位于同目录的 `selfmonitor-credentials.json`。这些文件与 Compose `.env` 独立保存，不提交到 Git。

Kubernetes 首期仅支持已有的 Debian WSL2、amd64、systemd、cgroup v2 单节点环境。集群资源在 `deploy/kubernetes/cluster/`，工具在 `tools/devtools/kubernetes/`。本次不引入旧环境清理脚本，也不将旧部署迁移记录作为升级步骤。

## 管理页自监控控制

HTTP API、响应字段和超级管理员权限保持不变：`GET /api/v1/admin/self-monitor` 查看状态，两个 POST 接口 `/start` 和 `/stop` 控制生命周期。

`GOPULSE_SELFMONITOR_CONTROL` 只接受 `compose` 和 `kubernetes`，默认 `compose`。未知值使后端拒绝启动。Compose 清单明确注入 `compose`，继续使用固定 Unix socket 与固定 systemd 服务。Kubernetes 平台进程明确注入 `kubernetes`，使用当前 ServiceAccount 的令牌、CA 和命名空间文件，只控制当前受支持命名空间内固定名称的自监控工作负载。Kubernetes 控制失败不会回退到 systemd。

控制器分别检查生命周期、预期 Prometheus 目标、数据源指标和 Grafana 健康。Compose 保留原有目标集合；Kubernetes 按当前模式检查自己的目标集合。控制权限不足显示不可用，目标或数据源异常显示部分不可达，不把控制失败显示为已停止。部分工作负载启停失败后，可以重试同一动作。

Compose 自监控仍需按 [原有安装说明](selfmonitor/README.md) 安装 root 管理的固定控制服务及产品覆盖配置。不能用 Kubernetes ServiceAccount 替代 Compose 的安装步骤，也不能将被屏蔽的 systemd 服务状态视为采集健康。
