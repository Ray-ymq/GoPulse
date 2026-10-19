# 独立自监控部署（Linux）

此目录提供独立 Prometheus、Grafana、Node Exporter 和中间件 Exporter，以及超级管理员启停入口。采集直接读取进程指标，不经过 Monitor 的产品采集器、Kafka、Marshaller 或 VictoriaMetrics 写入链路。各 Go 模块使用官方 Prometheus 客户端及独立 Registry；旧组件指标不再进入产品采集、查询和告警，外部数据源与历史数据保留。

## 部署前准备

1. 将本目录复制到主机 `/opt/gopulse-selfmonitor`，目录及 Compose、`.env`、配置和 systemd 文件由 root 持有且不可被管理后端用户修改。复制 `.env.example` 为 `.env`，填写产品的实际 Docker 网络名称、独立 Grafana 用户和站点 HTTPS 地址。先用 `docker network ls` 核对网络；本项目不创建、不删除产品网络。
2. 在 `secrets/` 中创建以下文件。文件不进入 Git；Prometheus UID 65534、Grafana UID 472 和各 Exporter 必须能读取对应文件，其他服务不应挂载它们。根据实际容器 UID 设置权限，禁止给后端写权限。

   | 文件 | 内容 |
   | --- | --- |
   | `backend_token`、`business-worker_token`、`search-indexer_token`、`monitor_token`、`marshaller_token` | 与各产品进程一致的独立指标令牌，不复用 API 凭据 |
   | `grafana_password` | 独立 Grafana 初始管理员密码；已有数据卷更改密码请使用 Grafana 管理入口 |
   | `mysql.cnf` | 下方只读 MySQL 用户配置 |
   | `redis_passwords.json` | `{"redis://redis:6379":"<只读 ACL 用户密码>"}` |
   | `victoriametrics_token` | 独立只读 Bearer 令牌，仅允许代理的 `/proxy/metrics` 路径 |
   | `vmauth.yaml` | 按 `vmauth.yaml.example` 配置令牌及现有 VictoriaMetrics HTTP Basic 凭据；仅挂载给受限代理，Prometheus 不接触业务凭据 |

   `mysql.cnf` 示例（独立账号）：

   ```ini
   [client]
   host=mysql
   port=3306
   user=selfmonitor
   password=<独立密码>
   ```

   MySQL 管理员创建用户并授予 `PROCESS, REPLICATION CLIENT, SELECT ON *.*`，设置 `MAX_USER_CONNECTIONS 3`。Redis 管理员创建 `selfmonitor` ACL 用户，仅授予 Exporter 使用的只读 `PING`、`INFO`、`CONFIG GET`、`CLIENT LIST`、`SLOWLOG`、`LATENCY`、`MEMORY` 查询权限；禁止写命令。按部署的 Redis 版本核对具体子命令。不要使用业务写账号。
3. 产品 override 已持久挂载 RabbitMQ 插件清单，启用 `rabbitmq_management` 和 `rabbitmq_prometheus`；更新产品部署会重建 RabbitMQ，安排业务维护窗口。该插件原生 `/metrics` 不需要管理 API 账号，仅通过回环映射采集。Elasticsearch 当前部署关闭认证；启用认证后必须先给 Exporter 配置独立只读 `monitor` 权限和 TLS，不能直接沿用此示例。
4. 创建主机组 `gopulse-selfmonitor`，将管理后端运行用户加入此组。容器部署在原有产品 Compose 命令中附加 `-f deploy/selfmonitor/product.override.yaml`，并设置 `SELFMONITOR_SOCKET_GID` 为该组的数字 GID。该 override 只将全部进程指标端口和 RabbitMQ 原生指标映射到主机回环；不会将业务中间件暴露到公网。端口清单与 `prometheus.yaml` 一一对应。自定义副本数时同时修改两个清单及控制器预期实例数，不依赖 DNS 负载均衡发现副本。
5. 在 backend 模块执行 `go build -o /tmp/gopulse-selfmonitor-control ./cmd/selfmonitor-control`，由部署管理员安装到 `/usr/local/libexec/gopulse-selfmonitor-control`，属主 root，模式 0755。安装 `systemd/` 下三个单元到 `/etc/systemd/system/`，执行 `systemctl daemon-reload`，然后 `systemctl enable --now gopulse-selfmonitor-control.socket`。先创建 socket 再启动管理后端容器，确保目录挂载不被 Docker 创建成错误属主。仅启用控制 socket，不启用 `gopulse-selfmonitor.service`。
6. 将 `nginx.conf.example` 合入主机 HTTPS 反向代理站点并校验、重载。Grafana 的 `/grafana/` 与产品使用相同站点，使用 Grafana 自己的登录。产品容器内反向代理无法访问主机回环，应由主机代理提供此入口。

控制器没有 Docker socket；它仅能接受三个固定请求并控制 `gopulse-selfmonitor.service`。后端只挂载受组权限保护的 Unix socket 目录，没有 systemd 总线或 Docker 权限。不要把该控制 socket 转发为公共 HTTP 服务。

## 从旧版本升级

先停止旧 Monitor 的组件指标发布，等待其持久 spool 和 Kafka 指标主题的所有 Marshaller 消费组 lag 归零，再升级 Marshaller。维护时先停止 Monitor 以免新增消息，观察原有投递诊断和 `kafka-consumer-groups.sh --bootstrap-server <broker> --describe --group <实际消费组>`；确认 spool 已排空及指标分区均已消费。不要删除 spool、Kafka 消息、VictoriaMetrics 数据或 MySQL 历史记录来跳过排空。随后按正常部署流程应用数据库迁移 16，升级各进程并使用产品 override；迁移关闭旧组件告警并记录结束原因，不重新启用旧规则，外部数据源告警继续工作。

VictoriaMetrics 的统一 HTTP 认证包含写权限，不能交给 Prometheus。独立 vmauth 只将经过 Bearer 认证的精确 `/proxy/metrics` 路径转发到原生 `/metrics`，其他业务 API 不匹配。将 `vmauth.yaml.example` 复制到 `secrets/vmauth.yaml`，填入独立令牌和现有业务 Basic 凭据的 Base64；Prometheus 仅挂载只读令牌。不要给配置添加默认转发或其他路径。

## 使用和验证

管理概览提供“启动自监控”“停止自监控”“打开 Grafana”。API 为 `GET /api/v1/admin/self-monitor`、`POST /api/v1/admin/self-monitor/start` 和 `POST /api/v1/admin/self-monitor/stop`，全部经过当前数据库角色的超级管理员授权；不接受请求体或查询参数。

首次启动允许拉取固定版本与 digest 镜像，可能耗时数分钟。启动和停止为 systemd 异步作业；页面每 5 秒读取真实状态。单元 active 后还会检查 Prometheus 目标和 Grafana 健康；缺失目标、失败采集和无法连接的数据源显示“部分不可达”，不会宣称运行健康。启动失败可以重试；重复点击不会创建新 Compose 项目。关闭页面、重启后端或控制服务不停止采集。停止执行固定项目的 `down`，保留两个命名数据卷；不使用 `down -v`，不操作业务项目。

部署校验：

```sh
docker compose --env-file /opt/gopulse-selfmonitor/.env -f /opt/gopulse-selfmonitor/compose.yaml config --quiet
systemctl start gopulse-selfmonitor.service
curl --fail http://127.0.0.1:29090/-/ready
curl --fail 'http://127.0.0.1:29090/api/v1/targets?state=active'
# 服务全部就绪后 45 秒内应有五种 GoPulse job 的样本，所有副本都应返回 up=1。
# 逐一停止 Kafka、Marshaller 验证其余 job 继续更新；在维护环境执行。
# 在 Grafana 保存偏好并确认历史曲线；停止、重启本项目后确认仍存在。
systemctl stop gopulse-selfmonitor.service
```

Prometheus 每 15 秒采集、超时 5 秒、保留 15 天，不开启管理接口或生命周期 HTTP 接口。主机、Prometheus、Grafana 监听回环；中间件 Exporter 只发布回环端口。仪表盘显示空值为“无数据”，无 `or vector(0)` 填零。

参考：[Prometheus 配置](https://prometheus.io/docs/prometheus/latest/configuration/configuration/)、[Grafana 反向代理](https://grafana.com/tutorials/run-grafana-behind-a-proxy/)。

## 仓库验证记录

本次验证使用独立临时 Compose 项目及测试数据，没有安装主机 systemd 单元或重建业务服务：

- Backend、Monitor、Marshaller、runtimekit、devtools、六个产品 Exporter 和 perflab 均通过 race 测试、`go vet` 和构建。管理端类型检查、62 项测试、lint、构建通过；CI 工具 100 项测试通过。
- 真实隔离 MySQL 验证迁移至 16、并发与重复迁移、脏状态拒绝及恢复；旧组件告警关闭，外部指标告警和历史记录保留。
- Compose 单独及产品 override 合并配置通过；Prometheus 配置通过 promtool；Grafana 实际加载 18 个面板。10 个认证模拟进程实例在 11 秒内采集；停止两个 Marshaller 模拟端点且 Kafka 不可用，其余八个进程及 Node Exporter 仍被采集。
- 使用真实 VictoriaMetrics 验证受限代理：有效令牌可读原生指标，错误令牌及业务写入路径被拒绝。停止、重建自监控后，保存的 Grafana 仪表盘及先前 Prometheus 样本仍存在。
- Backend、runtimekit、perflab 完整 lint 通过。其他模块完整 lint 仍有既有问题，不能将本次验证视为全仓库 lint 通过。

实际部署时还需在目标主机执行上述部署校验，验证超级管理员浏览器操作与 systemd、反向代理、真实全部副本和中间件凭据的完整联动。隔离模拟测试不代替这项现场验收。
