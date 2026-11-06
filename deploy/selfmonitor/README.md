# 独立自监控部署（Linux）

此目录提供独立 Prometheus、Grafana、Node Exporter 和中间件 Exporter，以及超级管理员启停入口。采集直接读取进程指标，不经过 Monitor 的产品采集器、Kafka、Marshaller 或 VictoriaMetrics 写入链路。各 Go 模块使用官方 Prometheus 客户端及独立 Registry；旧组件指标不再进入产品采集、查询和告警，外部数据源与历史数据保留。

## 当前环境的固定入口

固定非敏感参数位于 `selfmonitor.env`；Prometheus 目标和仪表盘使用本目录已提交的配置。密码、指标令牌和运行数据不提交。当前主机部署目录为 `/opt/gopulse-selfmonitor`，业务项目固定为 `gopulse`，监控项目固定为 `gopulse-selfmonitor`。

```sh
# 从仓库 .env 读取各进程令牌和 VM 认证，保留已有独立密码；不打印密码。
python3 deploy/selfmonitor/prepare.py
# 完成下方 systemd 安装后，启动业务依赖。
./deploy/selfmonitor/product.sh up -d mysql redis rabbitmq elasticsearch observability-elasticsearch kafka kafka-init victoriametrics
# 创建 MySQL 只读用户和 Redis ACL；Redis ACL 文件随产品 override 持久挂载。
python3 deploy/selfmonitor/prepare.py --provision
# 已构建本地业务镜像后启动完整业务环境。
./deploy/selfmonitor/product.sh up -d --wait --wait-timeout 240
sudo systemctl start gopulse-selfmonitor.service
```

`product.sh` 固定合并仓库 `.env.example`、本机 `.env`、`selfmonitor.env` 及指标端口 override，并自动读取控制 socket 的主机组 GID。它不覆盖仓库 `.env`。运行前须按下方步骤将本目录和私密文件复制到 root 管理的部署目录；配置更改后也需同步该目录。

访问 GoPulse：`http://localhost:5173/`，管理页面：`http://localhost:5173/admin/`，Grafana：`http://localhost:23000/`。本次启动已创建 GoPulse 初始超级管理员 `gopulse_admin`，账号密码保存在本机 `deploy/selfmonitor/secrets/gopulse_admin.json`。其他机器首次启动时先注册用户，再通过 `product.sh run --rm admin-role bootstrap --user-id <用户ID>` 声明初始管理员。Grafana 用户为 `selfmonitor-admin`，密码在本机 `deploy/selfmonitor/secrets/grafana_password`，可用 `cat` 查看；不要将其提交。

查看状态：`./deploy/selfmonitor/product.sh ps`、`systemctl status gopulse-selfmonitor.service`。只停止监控：`sudo systemctl stop gopulse-selfmonitor.service`，保留数据和业务服务；业务环境按需使用 `product.sh stop` 停止。

## 部署前准备

1. 将本目录复制到主机 `/opt/gopulse-selfmonitor`，目录及 Compose、`selfmonitor.env`、配置和 systemd 文件由 root 持有且不可被管理后端用户修改。使用已提交的 `selfmonitor.env` 固定配置，填写产品的实际 Docker 网络名称、独立 Grafana 用户和 Grafana 直接访问地址（例如 `http://服务器IP:23000/`）。先用 `docker network ls` 核对网络；本项目不创建、不删除产品网络。
2. 在 `secrets/` 中创建以下文件。文件不进入 Git；Prometheus UID 65534、Grafana UID 472 和各 Exporter 必须能读取对应文件，其他服务不应挂载它们。根据实际容器 UID 设置权限，禁止给后端写权限。

   | 文件 | 内容 |
   | --- | --- |
   | `backend_token`、`business-worker_token`、`search-indexer_token`、`monitor_token`、`marshaller_token` | 与各产品进程一致的独立指标令牌，不复用 API 凭据 |
   | `grafana_password` | 独立 Grafana 初始管理员密码；已有数据卷更改密码请使用 Grafana 管理入口 |
   | `mysql.cnf`、`mysql_password` | 独立只读 MySQL 用户配置和生成的密码 |
   | `redis_password`、`redis.acl`、`redis_passwords.json` | `{"redis://selfmonitor@redis:6379":"<只读 ACL 用户密码>"}` |
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
4. 创建主机组 `gopulse-selfmonitor`，将管理后端运行用户加入此组。容器部署在原有产品 Compose 命令中附加 `-f deploy/selfmonitor/product.override.yaml`，并设置 `SELFMONITOR_SOCKET_GID` 为该组的数字 GID。该 override 只将全部进程指标端口和 RabbitMQ 原生指标映射到主机回环；不会将业务中间件暴露到公网。端口清单与 `prometheus.yaml` 一一对应。自定义副本数时同时修改两个清单及控制器预期实例数，不依赖 DNS 负载均衡发现副本。业务的 internal 网络会忽略端口发布，因此 override 为指标发布者增加独立的端口访问桥，监控 Exporter 也有独立访问桥；Exporter 访问桥上禁止容器互通，产品访问桥保留正常 DNS 通信；所有指标仍只映射回环地址。
5. 在 backend 模块执行 `go build -o /tmp/gopulse-selfmonitor-control ./cmd/selfmonitor-control`，由部署管理员安装到 `/usr/local/libexec/gopulse-selfmonitor-control`，属主 root，模式 0755。安装 `systemd/` 下三个单元到 `/etc/systemd/system/`，执行 `systemctl daemon-reload`，然后 `systemctl enable --now gopulse-selfmonitor-control.socket`。先创建 socket 再启动管理后端容器，确保目录挂载不被 Docker 创建成错误属主。仅启用控制 socket，不启用 `gopulse-selfmonitor.service`。
6. Grafana 直接监听主机 `23000` 端口，使用自己的账号登录，无需配置反向代理。在防火墙或云安全组放行需要访问的客户端。管理页面的“打开 Grafana”使用当前页面主机名及 `http://主机名:23000/`，因此管理页面域名应直接指向部署主机；`GRAFANA_ROOT_URL` 填写同一地址。

控制器没有 Docker socket；它仅能接受三个固定请求并控制 `gopulse-selfmonitor.service`。后端只挂载受组权限保护的 Unix socket 目录，没有 systemd 总线或 Docker 权限。不要把该控制 socket 转发为公共 HTTP 服务。

## 从旧版本升级

先停止旧 Monitor 的组件指标发布，等待其持久 spool 和 Kafka 指标主题的所有 Marshaller 消费组 lag 归零，再升级 Marshaller。维护时先停止 Monitor 以免新增消息，观察原有投递诊断和 `kafka-consumer-groups.sh --bootstrap-server <broker> --describe --group <实际消费组>`；确认 spool 已排空及指标分区均已消费。不要删除 spool、Kafka 消息、VictoriaMetrics 数据或 MySQL 历史记录来跳过排空。随后按正常部署流程应用数据库迁移 16，升级各进程并使用产品 override；迁移关闭旧组件告警并记录结束原因，不重新启用旧规则，外部数据源告警继续工作。

VictoriaMetrics 的统一 HTTP 认证包含写权限，不能交给 Prometheus。独立 vmauth 只将经过 Bearer 认证的精确 `/proxy/metrics` 路径转发到原生 `/metrics`，其他业务 API 不匹配。将 `vmauth.yaml.example` 复制到 `secrets/vmauth.yaml`，填入独立令牌和现有业务 Basic 凭据的 Base64；Prometheus 仅挂载只读令牌。不要给配置添加默认转发或其他路径。

## 使用和验证

管理概览提供“启动自监控”“停止自监控”“打开 Grafana”。API 为 `GET /api/v1/admin/self-monitor`、`POST /api/v1/admin/self-monitor/start` 和 `POST /api/v1/admin/self-monitor/stop`，全部经过当前数据库角色的超级管理员授权；不接受请求体或查询参数。

首次启动允许拉取固定版本与 digest 镜像，可能耗时数分钟。启动和停止为 systemd 异步作业；页面每 5 秒读取真实状态。单元 active 后还会检查 Prometheus 目标和 Grafana 健康；缺失目标、失败采集和无法连接的数据源显示“部分不可达”，不会宣称运行健康。启动失败可以重试；重复点击不会创建新 Compose 项目。关闭页面、重启后端或控制服务不停止采集。停止执行固定项目的 `down`，保留两个命名数据卷；不使用 `down -v`，不操作业务项目。

部署校验：

```sh
docker compose --env-file /opt/gopulse-selfmonitor/selfmonitor.env -f /opt/gopulse-selfmonitor/compose.yaml config --quiet
systemctl start gopulse-selfmonitor.service
curl --fail http://127.0.0.1:23000/api/health
curl --fail http://127.0.0.1:29090/-/ready
curl --fail 'http://127.0.0.1:29090/api/v1/targets?state=active'
# 服务全部就绪后 45 秒内应有五种 GoPulse job 的样本，所有副本都应返回 up=1。
# 逐一停止 Kafka、Marshaller 验证其余 job 继续更新；在维护环境执行。
# 在 Grafana 保存偏好并确认历史曲线；停止、重启本项目后确认仍存在。
systemctl stop gopulse-selfmonitor.service
```

Prometheus 每 15 秒采集、超时 5 秒、保留 15 天，不开启管理接口或生命周期 HTTP 接口。Node Exporter、Prometheus 监听回环，中间件 Exporter 只发布回环端口；Grafana 监听全部 IPv4 地址的 23000 端口。仪表盘显示空值为“无数据”，无 `or vector(0)` 填零。

参考：[Prometheus 配置](https://prometheus.io/docs/prometheus/latest/configuration/configuration/)。

## 当前主机启动验证

已在当前 Linux 主机构建七种业务镜像并启动完整 `gopulse` 项目，迁移到版本 16；所有常驻业务容器健康，迁移、索引初始化、Kafka 初始化任务成功退出。独立 systemd 控制 socket 和自监控服务已安装，自监控仍不配置开机自动启动。

实际采集包括 10 个 GoPulse 进程、Node、六类中间件（两个 Elasticsearch 实例）及 Prometheus，共 19 个目标。管理员登录后的状态 API 返回 `running`，未登录访问返回 401；通过实际管理 API 重复启动、停止验证幂等性。停止监控期间业务容器和网站继续运行；重启后 19 个目标恢复采集，Grafana 用户偏好和旧 Prometheus 样本仍存在。独立 MySQL 和 Redis 账号已配置，Redis ACL 持久挂载且写命令拒绝验证通过；产品 Redis 指标查询返回 200。Exporter 服务使用不同名称，避免与产品服务的 Docker DNS 别名冲突。

本机 Docker 原有镜像代理指向不可用的 7890 端口；本次已将其修正为当前环境实际使用的 10808 代理后成功拉取固定 digest 镜像。这是本机网络设置，不写入共享部署配置；其他主机使用各自可用的 Docker 网络设置。

## 仓库验证记录

取消反代后，另以临时 Grafana 容器验证 `23000/api/health` 和根路径登录页直接访问通过；相关后端 race、vet、构建及管理端类型检查、62 项测试、lint、构建通过。

本次验证使用独立临时 Compose 项目及测试数据，没有安装主机 systemd 单元或重建业务服务：

- Backend、Monitor、Marshaller、runtimekit、devtools、六个产品 Exporter 和 perflab 均通过 race 测试、`go vet` 和构建。管理端类型检查、62 项测试、lint、构建通过；CI 工具 100 项测试通过。
- 真实隔离 MySQL 验证迁移至 16、并发与重复迁移、脏状态拒绝及恢复；旧组件告警关闭，外部指标告警和历史记录保留。
- Compose 单独及产品 override 合并配置通过；Prometheus 配置通过 promtool；Grafana 实际加载 18 个面板。10 个认证模拟进程实例在 11 秒内采集；停止两个 Marshaller 模拟端点且 Kafka 不可用，其余八个进程及 Node Exporter 仍被采集。
- 使用真实 VictoriaMetrics 验证受限代理：有效令牌可读原生指标，错误令牌及业务写入路径被拒绝。停止、重建自监控后，保存的 Grafana 仪表盘及先前 Prometheus 样本仍存在。
- Backend、runtimekit、perflab 完整 lint 通过。其他模块完整 lint 仍有既有问题，不能将本次验证视为全仓库 lint 通过。

实际部署时还需在目标主机执行上述部署校验，验证超级管理员浏览器操作与 systemd、Grafana 直接端口访问、真实全部副本和中间件凭据的完整联动。上方当前主机记录补充实际部署验证；其他目标主机仍须自行验收。
