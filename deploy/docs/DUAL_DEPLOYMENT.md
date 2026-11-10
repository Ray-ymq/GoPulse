# Compose 与 Kubernetes 双部署

本次整合的提交和实际检查结果见 [双部署验收记录](DUAL_DEPLOYMENT_VALIDATION.md)。

产品同时保留 Compose 和 Kubernetes 部署。两种方式各有 business、observability、all 三种模式；同一工作区同一时间只启动一种产品环境。源码开发、集成检查和正式压测继续使用 Compose。

## 共用构建与配置

`deploy/images.json` 是七种产品镜像的构建定义。执行 `make build-images` 时，工具先准备插件目录，再使用该定义构建镜像。构建过程不连接 Kubernetes 集群。每份产物同时标记为 `gopulse/<组件>:<VERSION>-<Git revision 前12位>` 和 `gopulse/<组件>:latest`。

Compose 默认使用 `latest`，也可以通过已有 `GOPULSE_*_IMAGE` 变量指定不可变标签。Kubernetes 按当前源码 revision 导入和部署不可变标签。产品 Monitor 镜像包含插件目录；源码开发仍可单独构建不含该目录的 `monitor` 阶段，并挂载源码工具准备的插件目录。

`deploy/runtime-contracts.json` 保留原 Compose 字段、实例约束、资源预算和压测约束，并新增 `kubernetes_workload`、`kubernetes_workloads`、`kubernetes_replica` 和环境键的 `kubernetes_value`。Kubernetes 使用自己的工作负载地址和副本配置，不替换 Compose 的实例列表。

Kubernetes 为用户前端挂载独立 Nginx 配置，使用集群 Service 地址；Compose 镜像继续保留双 Backend 和 Docker DNS。契约检查比较两份配置的路由，防止路由发生差异。两种自监控分别保存 Prometheus 和 Grafana 地址配置。

插件包使用 `GOWORK=off` 按各模块依赖构建。包缓存同时绑定源码、打包工具、元数据实现和 Go 工具链；切换工作区不能复用按另一套依赖生成的缓存。已有插件仍按固定清单和二进制摘要校验，不通过修改摘要或删除配置绕过校验。

公共环境变量示例仍在根 `.env.example`。Kubernetes 产品凭据位于 `.run/kubernetes/<MODE>/credentials.json`，自监控凭据位于同目录的 `selfmonitor-credentials.json`。这些文件与 Compose `.env` 独立保存，不提交到 Git。

Kubernetes 首期仅支持已有的 Debian WSL2、amd64、systemd、cgroup v2 单节点环境。集群资源在 `deploy/kubernetes/cluster/`，工具在 `tools/devtools/kubernetes/`。本次不引入旧环境清理脚本，也不将旧部署迁移记录作为升级步骤。

## 管理页自监控控制

HTTP API、响应字段和超级管理员权限保持不变：`GET /api/v1/admin/self-monitor` 查看状态，两个 POST 接口 `/start` 和 `/stop` 控制生命周期。

`GOPULSE_SELFMONITOR_CONTROL` 只接受 `compose` 和 `kubernetes`，默认 `compose`。未知值使后端拒绝启动。Compose 清单明确注入 `compose`，继续使用固定 Unix socket 与固定 systemd 服务。Kubernetes 平台进程明确注入 `kubernetes`，使用当前 ServiceAccount 的令牌、CA 和命名空间文件，只控制当前受支持命名空间内固定名称的自监控工作负载。Kubernetes 控制失败不会回退到 systemd。

控制器分别检查生命周期、预期 Prometheus 目标、数据源指标和 Grafana 健康。Compose 保留原有目标集合；Kubernetes 按当前模式检查自己的目标集合。控制权限不足显示不可用，目标或数据源异常显示部分不可达，不把控制失败显示为已停止。部分工作负载启停失败后，可以重试同一动作。

Compose 自监控仍需按 [原有安装说明](../selfmonitor/README.md) 安装 root 管理的固定控制服务及产品覆盖配置。不能用 Kubernetes ServiceAccount 替代 Compose 的安装步骤，也不能将被屏蔽的 systemd 服务状态视为采集健康。

## 命令入口

所有命令在仓库根目录执行，`MODE` 默认 `all`。

| 操作 | Compose | Kubernetes |
|---|---|---|
| 启动 | `make stack-up MODE=...` | `make k8s-up MODE=...` |
| 停止并保留持久数据 | `make stack-down MODE=...` | `make k8s-down MODE=...` |
| 查看状态 | `make stack-status MODE=...` | `make k8s-status MODE=...` |
| 验收 | `make stack-verify MODE=...` | `make k8s-verify MODE=...` |
| 本机访问 | 启动和验证时打印地址 | `make k8s-access MODE=...` |
| 自监控 | `make selfmonitor-up/down/status/access/verify` | `make k8s-selfmonitor-up/down/status/access/verify MODE=...` |

表中的斜线表示分别执行对应命令，例如 `make selfmonitor-up`，不是一个完整的 Make 目标名。管理前端不提供部署方式切换按钮。

Compose 使用既有三份配置文件。Make 命令的默认项目名为：整体模式 `gopulse`、业务模式 `gopulse-business`、可观测模式 `gopulse-observability`。它们与直接执行 Compose 时配置文件中的默认项目名可能不同。使用 `COMPOSE_PROJECT_NAME` 接管部署时，启动、停止和验证必须使用同一个项目名。`GOPULSE_ENV_FILE` 可指定独立私有环境文件，默认使用根 `.env`；缺少默认文件时从 `.env.example` 创建，已有文件不覆盖。

新 Compose 命名卷和网络带有工作区路径标签。工具拒绝接管属于其他工作区或缺少归属标签的持久资源。对旧资源先核对原项目名、配置路径和数据归属，保留备份，再按原 Compose 命令管理；不要为通过归属检查删除命名卷。仍可直接执行 Docker Compose 命令，但这些外部命令不受 Make 工具的工作区锁约束。切换前必须确认原环境已停止。

运行契约检查需要 Python 3 和 `jsonschema`；Debian 可使用 `python3-jsonschema` 软件包。

首次准备 Kubernetes：

```sh
make k8s-install
make k8s-check
make k8s-cluster-verify
make build-images
make k8s-load-images MODE=all
make k8s-up MODE=all
make k8s-access MODE=all
```

安装器使用独立 containerd，不更改 Docker 的运行时。已有其他工作区的集群或未完成的初始化会使安装停止；工具不执行自动 reset。需要预取控制平面镜像时，使用 `make k8s-prefetch`；`DIRECT=1` 仅影响该次下载。

Kubernetes Grafana 用户名为 `admin`，密码是当前模式 `selfmonitor-credentials.json` 中的 `GRAFANA_PASSWORD`。

Kubernetes 访问入口只绑定本机回环地址：business 和 all 使用 `18080`，observability 使用 `18081`。首次注册后，通过当前命名空间的 `platform-api` 声明初始超级管理员：

```sh
kubectl --kubeconfig .run/kubernetes/cluster/admin.conf -n gopulse-all \
  exec deployment/platform-api -- /usr/local/bin/admin-role bootstrap --user-id <注册响应中的用户ID>
```

已有初始化声明时，命令不会覆盖它。修改源码并提交后，应重新构建、导入和部署同一 revision 的镜像。`k8s-verify` 会运行真实业务和观测验收，并写入专用测试账号与数据；`k8s-recreate` 会重建所选 Pod 并检查此前验收数据的持久性，执行前应安排服务中断窗口。

## 切换、持久数据与失败恢复

从 Compose 切换到 Kubernetes，若已启动独立自监控，先执行 `make selfmonitor-down`；再用原项目名执行 `stack-down`，最后执行对应模式的 `k8s-up`。反向切换时先执行 `k8s-down`，再执行 `stack-up`。两套环境独立保存数据库、插件配置和凭据，不自动复制或同步数据。

产品、源码、集成和压测入口共用 `.run/deploy/operation.lock`。启动前还检查当前工作区的实际容器、Kubernetes 工作负载、源码进程和未完成压测记录。冲突会使操作退出，并提示对应停止命令。运行记录位于 `.run/deploy/state.json`，包含部署方式、模式、资源归属和状态；工具不会仅凭旧状态文件判断环境已停止。

`make deps/dev/dev-observe/integration/stop/pt*` 继续使用 Compose。`make stop` 只处理源码会话，不隐式停止 Kubernetes 产品环境。正式压测仍使用原专用 Compose 项目和资源约束，未增加 Kubernetes 压测入口。

Compose `down` 保留命名卷。Kubernetes `down` 缩容本工作区产品命名空间中的工作负载，保留 PVC、Secret 和本地凭据。丢失本地凭据文件时，工具会从已有 Secret 恢复；发现本地值与集群不一致时拒绝覆盖，不自动轮换数据库密码。数据库迁移、Topic 或搜索初始化失败时，不继续启动应用；修复失败原因后重新执行同一启动命令。

升级前备份数据库，并阅读 [迁移 000016 的状态变更与回滚边界](COMPOSE_INTEGRATION.md#升级与回滚边界)。应用回滚不能自动恢复被关闭的告警事件或被禁用的规则状态。

## WSL 与节点地址恢复

WSL 重启会终止当前会话，必须从 Windows 端安排执行。重新进入 Debian 后，确认 `gopulse-containerd` 和 `kubelet` 已启动，等待节点 Ready，再执行 `make k8s-check`、`make k8s-cluster-verify` 和当前模式的 `make k8s-access`。

恢复验收使用已有的专用验收账号和数据。先完成当前模式的 `k8s-verify` 及 `k8s-selfmonitor-verify`，并确认自监控健康，再记录基线：

```sh
python3 tools/devtools/kubernetes/recovery.py capture --mode all --name before-maintenance
```

重启后等待集群和应用恢复，再核对同一基线：

```sh
make k8s-cluster-verify
python3 tools/devtools/kubernetes/recovery.py verify --mode all \
  --baseline before-maintenance --name after-reboot --require reboot
```

工具重新建立本机访问入口，登录原账号，读取原文章、插件配置及指标，并检查所有预期自监控目标。它比较 Node、命名空间、PVC、Secret 的身份、凭据摘要和原验收数据。`--require reboot` 要求主机 boot ID 实际变化；`--require address` 要求 Node InternalIP 实际变化。未发生对应变化时，验收失败。工具不执行重启、地址变更或数据修复。

记录保存于 `.run/kubernetes/<MODE>/recovery/<记录名>.json`，权限为 `0600`，其中凭据只保存摘要。记录名只能包含字母、数字、连字符和下划线；已有记录不得覆盖。每次维护使用新的记录名，保留原基线。

`k8s-check` 要求节点 InternalIP 与私有 kubeadm 配置一致，且仍绑定在本机接口。VPN 改变外网路由不表示节点地址发生变化。地址检查失败时，保留配置、证书、etcd 数据和 PVC，停止继续部署；不要通过自动 reset 修复。

节点地址变更应安排维护窗口。在控制平面和 etcd 仍可访问时，先备份私有 kubeadm 配置、控制平面配置和证书，创建并校验 etcd 快照，再协调更新节点地址、etcd 成员 peer URL、静态 Pod 地址及证书。更新 etcd peer URL 时，先提交成员地址变更，再重启成员，见 [etcd 运行时配置说明](https://etcd.io/docs/v3.6/op-guide/runtime-configuration/)。新地址必须包含在对应服务证书的 SAN 中；普通 `kubeadm certs renew` 会沿用旧证书的 SAN，见 [kubeadm 证书管理说明](https://kubernetes.io/docs/tasks/administer-cluster/kubeadm/kubeadm-certs/)。维护命令的代理排除列表也需包含新地址。完成控制平面恢复后，重新验证节点、DNS、Service、PVC 和应用数据，并使用原基线执行恢复核对，指定 `--require address`。本次工具不自动执行这类地址变更；实际重启和地址变化的验收结果必须单独记录。

旧地址已经失效或 etcd 不可访问时，先保留控制平面配置和数据目录，完成离线备份，再根据可用快照制定恢复方案。上述在线成员更新步骤不适用于该状态；本次补验未覆盖此类故障。

可选追踪通过 `TRACE=1 make k8s-load-images MODE=all`、`TRACE=1 make k8s-up MODE=all` 和 `TRACE=1 make k8s-verify MODE=all` 验证。关闭开关后再次启动会停止追踪采集器，保留追踪 PVC；该开关不改变 Compose 正式压测条件。
