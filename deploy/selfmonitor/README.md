# 独立自监控部署（Linux）

此目录提供独立 Prometheus、Grafana、Node Exporter 和中间件 Exporter，以及超级管理员启停入口。采集直接读取进程指标，不经过 Monitor 的产品采集器、Kafka、Marshaller 或 VictoriaMetrics 写入链路。尚未完成的旧埋点和产品页面迁移见 `docs/datalink-plan.md`；安装本目录不会自动关闭旧发布器。

## 部署前准备

1. 将本目录复制到主机 `/opt/gopulse-selfmonitor`，目录及 Compose、`.env`、配置和 systemd 文件由 root 持有且不可被管理后端用户修改。复制 `.env.example` 为 `.env`，填写产品的实际 Docker 网络名称、独立 Grafana 用户和站点 HTTPS 地址。先用 `docker network ls` 核对网络；本项目不创建、不删除产品网络。
2. 在 `secrets/` 中创建以下文件。文件不进入 Git；Prometheus UID 65534、Grafana UID 472 和各 Exporter 必须能读取对应文件，其他服务不应挂载它们。根据实际容器 UID 设置权限，禁止给后端写权限。

   | 文件 | 内容 |
   | --- | --- |
   | `backend_token`、`business-worker_token`、`search-indexer_token`、`monitor_token`、`marshaller_token` | 与各产品进程一致的独立指标令牌，不复用 API 凭据 |
   | `grafana_password` | 独立 Grafana 初始管理员密码；已有数据卷更改密码请使用 Grafana 管理入口 |
   | `mysql.cnf` | 下方只读 MySQL 用户配置 |
   | `redis_passwords.json` | `{"redis://redis:6379":"<只读 ACL 用户密码>"}` |
   | `victoriametrics_username`、`victoriametrics_password` | VictoriaMetrics HTTP Basic 只读采集凭据（现有服务使用统一 HTTP 认证；请在网络侧限制该凭据仅供采集） |

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
4. 创建主机组 `gopulse-selfmonitor`，将管理后端运行用户加入此组。容器部署在原有产品 Compose 命令中附加 `-f deploy/selfmonitor/product.override.yaml`，并设置 `SELFMONITOR_SOCKET_GID` 为该组的数字 GID。该 override 只将全部进程指标端口、RabbitMQ 原生指标和 VictoriaMetrics 映射到主机回环；不会将业务中间件暴露到公网。端口清单与 `prometheus.yaml` 一一对应。自定义副本数时同时修改两个清单及控制器预期实例数，不依赖 DNS 负载均衡发现副本。
5. 在 backend 模块执行 `go build -o /tmp/gopulse-selfmonitor-control ./cmd/selfmonitor-control`，由部署管理员安装到 `/usr/local/libexec/gopulse-selfmonitor-control`，属主 root，模式 0755。安装 `systemd/` 下三个单元到 `/etc/systemd/system/`，执行 `systemctl daemon-reload`，然后 `systemctl enable --now gopulse-selfmonitor-control.socket`。先创建 socket 再启动管理后端容器，确保目录挂载不被 Docker 创建成错误属主。仅启用控制 socket，不启用 `gopulse-selfmonitor.service`。
6. 将 `nginx.conf.example` 合入主机 HTTPS 反向代理站点并校验、重载。Grafana 的 `/grafana/` 与产品使用相同站点，使用 Grafana 自己的登录。产品容器内反向代理无法访问主机回环，应由主机代理提供此入口。

控制器没有 Docker socket；它仅能接受三个固定请求并控制 `gopulse-selfmonitor.service`。后端只挂载受组权限保护的 Unix socket 目录，没有 systemd 总线或 Docker 权限。不要把该控制 socket 转发为公共 HTTP 服务。

## 使用和验证

管理概览提供“启动自监控”“停止自监控”“打开 Grafana”。API 为 `GET /api/v1/admin/self-monitor`、`POST /api/v1/admin/self-monitor/start` 和 `POST /api/v1/admin/self-monitor/stop`，全部经过当前数据库角色的超级管理员授权；不接受请求体或查询参数。

首次启动允许拉取固定版本与 digest 镜像，可能耗时数分钟。启动和停止为 systemd 异步作业；页面每 5 秒读取真实状态。单元 active 后还会检查 Prometheus 目标和 Grafana 健康；缺失目标、失败采集和无法连接的数据源显示“部分不可达”，不会宣称运行健康。启动失败可以重试；重复点击不会创建新 Compose 项目。关闭页面、重启后端或控制服务不停止采集。停止执行固定项目的 `down`，保留两个命名数据卷；不使用 `down -v`，不操作业务项目。

部署校验：

```sh
docker compose --env-file /opt/gopulse-selfmonitor/.env -f /opt/gopulse-selfmonitor/compose.yaml config --quiet
systemctl start gopulse-selfmonitor.service
curl --fail http://127.0.0.1:19090/-/ready
curl --fail 'http://127.0.0.1:19090/api/v1/targets?state=active'
# 服务全部就绪后 45 秒内应有五种 GoPulse job 的样本，所有副本都应返回 up=1。
# 逐一停止 Kafka、Marshaller 验证其余 job 继续更新；在维护环境执行。
# 在 Grafana 保存偏好并确认历史曲线；停止、重启本项目后确认仍存在。
systemctl stop gopulse-selfmonitor.service
```

Prometheus 每 15 秒采集、超时 5 秒、保留 15 天，不开启管理接口或生命周期 HTTP 接口。主机、Prometheus、Grafana 监听回环；中间件 Exporter 只发布回环端口。仪表盘显示空值为“无数据”，无 `or vector(0)` 填零。

参考：[Prometheus 配置](https://prometheus.io/docs/prometheus/latest/configuration/configuration/)、[Grafana 反向代理](https://grafana.com/tutorials/run-grafana-behind-a-proxy/)。
