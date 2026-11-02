# Compose 与 Kubernetes 双部署

产品同时保留 Compose 和 Kubernetes 部署。两种方式各有 business、observability、all 三种模式；同一工作区同一时间只启动一种产品环境。源码开发、集成检查和正式压测继续使用 Compose。

## 共用构建与配置

`deploy/images.json` 是七种产品镜像的构建定义。执行 `make build-images` 时，工具先准备插件目录，再使用该定义构建镜像。构建过程不连接 Kubernetes 集群。每份产物同时标记为 `gopulse/<组件>:<VERSION>-<Git revision 前12位>` 和 `gopulse/<组件>:latest`。

Compose 默认使用 `latest`，也可以通过已有 `GOPULSE_*_IMAGE` 变量指定不可变标签。Kubernetes 按当前源码 revision 导入和部署不可变标签。产品 Monitor 镜像包含插件目录；源码开发仍可单独构建不含该目录的 `monitor` 阶段，并挂载源码工具准备的插件目录。

`deploy/runtime-contracts.json` 保留原 Compose 字段、实例约束、资源预算和压测约束，并新增 `kubernetes_workload`、`kubernetes_workloads`、`kubernetes_replica` 和环境键的 `kubernetes_value`。Kubernetes 使用自己的工作负载地址和副本配置，不替换 Compose 的实例列表。

公共环境变量示例仍在根 `.env.example`。Kubernetes 产品凭据位于 `.run/kubernetes/<MODE>/credentials.json`，自监控凭据位于同目录的 `selfmonitor-credentials.json`。这些文件与 Compose `.env` 独立保存，不提交到 Git。

Kubernetes 首期仅支持已有的 Debian WSL2、amd64、systemd、cgroup v2 单节点环境。集群资源在 `deploy/kubernetes/cluster/`，工具在 `tools/devtools/kubernetes/`。本次不引入旧环境清理脚本，也不将旧部署迁移记录作为升级步骤。
