# Docker Compose 单机部署

根 `compose.yml` 定义 10 个服务容器：Nginx、API、通知 Worker、搜索 Indexer、MySQL、Redis、RabbitMQ、Elasticsearch，以及迁移和搜索初始化两个一次性任务。正常启动完成后，8 个容器运行，两个任务以退出码 0 结束。

项目构建两个镜像：`gopulse/backend` 和 `gopulse/web`。后端镜像包含三个常驻进程及 `migrate`、`search-reindex`、`admin-role` 维护命令，各容器选择自己的入口。MySQL、Redis、RabbitMQ、Elasticsearch 使用 Compose 中固定版本的上游镜像。不要将旧数据库或 RabbitMQ 的数据卷直接挂载到不同主版本；已有数据需要按上游升级流程迁移。

## 准备环境

需要 Docker Engine、支持 `up --wait` 的 Docker Compose v2 或更新版本，以及 Make。构建使用 BuildKit 缓存，宿主机无需安装 Go 或 Node.js。首次构建和启动需要联网拉取镜像、Go 模块、npm 和 Alpine 软件包。

Elasticsearch 固定 1 GiB JVM 堆，容器内存上限为 2 GiB。主机还需为其他服务和系统预留内存；这些值是起始配置，不能视为容量保证。Linux 主机的 `vm.max_map_count` 至少设置为 `1048576`。先检查当前值，低于要求时由主机管理员调整，并配置重启后保留。参考 [Elastic 容器要求](https://www.elastic.co/docs/deploy-manage/deploy/self-managed/install-elasticsearch-docker-prod)。

```sh
cat /proc/sys/vm/max_map_count
```

本方案由 Nginx 直接终止 TLS，仅发布 HTTP 与 HTTPS 端口。API、数据库、缓存、消息队列、搜索和后端健康检查均不发布主机端口。域名的 DNS 记录需指向部署主机，主机防火墙需放行对应端口。

当前 Elasticsearch 客户端没有认证配置，Compose 因此关闭 Elasticsearch 认证，并将其限制在仅连接 API、Indexer、搜索初始化任务的内部网络。此方案适用于信任同一 Docker 主机及相连容器的场景；如果要求服务之间也必须认证或加密，需先补充客户端支持。Compose 不提供多机高可用。

## 配置与证书

从仓库根目录执行，保留已有 `.env`：

```sh
test -f .env || cp .env.example .env
chmod 600 .env
```

配置示例只维护在根 `.env.example`。部署前编辑 `.env`：

- 设置 `GOPULSE_DOMAIN`，值仅包含域名，不含协议、端口或路径。
- 替换 `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`REDIS_PASSWORD`、`RABBITMQ_DEFAULT_PASS` 和 `AUTH_JWT_SECRET`。JWT 密钥至少需要 32 字节，不得使用示例凭据。
- 设置 `RABBITMQ_DEFAULT_USER`，并让 `COMPOSE_RABBITMQ_URL` 的凭据与之匹配。URL 的主机必须为 `rabbitmq`；账号和密码中的特殊字符需百分号编码。可用 `openssl rand -hex 32` 生成不需要 URL 编码的随机密码。
- 将证书链和私钥放入 `GOPULSE_TLS_DIR` 指定的目录，文件名分别为 `fullchain.pem` 和 `privkey.pem`。默认目录为 `.run/tls`，不得提交私钥。证书需覆盖 `GOPULSE_DOMAIN`，私钥需可被容器内 Nginx 主进程读取。
- 如端口已占用，调整 `GOPULSE_HTTP_PORT` 和 `GOPULSE_HTTPS_PORT`。HTTPS 使用非 443 端口时，还需设置对应的 `GOPULSE_HTTPS_SUFFIX`，例如端口 `8443` 对应 `:8443`。
- 如 `172.30.0.0/24` 与已有网络冲突，修改 `GOPULSE_PROXY_SUBNET`。API 自动信任此专用代理网段；该网络只连接 Nginx 和 API。

Compose 显式将必要环境变量注入各角色，不会将整份 `.env` 放入镜像或传给容器。运行模式固定为 `container`，API 固定监听 `0.0.0.0:8080`，依赖地址固定为服务 DNS 名称。API 固定使用 `production`、Secure Cookie 和 Redis；宿主机开发配置 `APP_ENV`、`AUTH_COOKIE_SECURE`、`MYSQL_HOST`、`HTTP_PORT` 等不会改变这些容器配置。宿主机使用的 `RABBITMQ_URL` 与容器使用的 `COMPOSE_RABBITMQ_URL` 分开配置。

Nginx 保留原始 Host，并覆盖 `X-Forwarded-Proto` 和 `X-Forwarded-For`，以满足同源校验并避免信任客户端伪造的转发头。当前配置假定客户端直接连接 Nginx；如前置 CDN 或负载均衡器，需按可信来源重新配置真实客户端地址。Nginx 内部动态解析 `api`，API 容器重建后无需依赖旧 IP。

## 构建与首次启动

`VERSION` 是产品版本的唯一来源。Make 默认将其用作两个项目镜像的标签。需要保留不同构建时，可在下列所有 Make 命令中一致传入 `GOPULSE_IMAGE_TAG=<自定义标签>`；它只改变镜像标签，不改变镜像内的产品版本标签。

```sh
make container-build
make container-config
make container-up
```

`container-build` 只构建两个项目镜像，不要求配置运行凭据或提供证书。`container-config` 只校验 Compose 配置，不会输出解析后的敏感值。启动前仍需确认凭据、证书文件和主机资源已准备好。

首次启动按依赖顺序执行：MySQL 健康后运行 `migrate up`；迁移成功且 Elasticsearch 健康后运行 `search-reindex --if-missing`；初始化成功且各自依赖健康后启动三个业务进程；API 就绪后启动 Nginx。已有搜索别名会跳过重建。任一初始化任务失败都会阻止后续业务容器启动。

HTTP 会跳转到 HTTPS。首次部署使用 `.env.example` 的域名示例时，需自行提供覆盖 `localhost` 的证书；公网部署应使用实际域名和有效证书。Nginx 不负责自动申请或续期证书。

直接使用 Docker Compose 时，先在当前终端设置镜像标签。使用额外参数可单独查看状态和日志：

```sh
export GOPULSE_IMAGE_TAG="$(cat VERSION)"
docker compose ps -a
docker compose logs --tail=100 api worker indexer
```

探针使用 `/ready` 判断业务进程就绪；Nginx 探针使用 HTTP `/health`。`unhealthy` 不会触发 Docker 自动重启，`restart: unless-stopped` 处理进程退出。需要持续监测状态并按故障原因处理，不能用健康检查代替恢复机制。

## 升级与失败处理

升级会中断服务。执行前先备份数据库并验证恢复方法，记录旧代码版本与镜像标签。迁移 `000021_remove_observability` 会删除旧告警表，降级只能恢复表结构，不能恢复已删除的数据。

在旧配置仍有效时停止业务与初始化容器，并保留依赖和数据卷：

```sh
export GOPULSE_IMAGE_TAG="$(cat VERSION)"
docker compose stop web api worker indexer
docker compose rm -f migrate search-init
```

然后检出目标代码并更新配置，重新构建和启动：

```sh
unset GOPULSE_IMAGE_TAG
make container-build
make container-config
make container-up
```

如果使用自定义镜像标签，构建和启动时需显式传入目标标签。删除已退出的初始化容器可确保本次发布重新执行迁移和索引检查。不要在旧业务进程运行时直接启动新迁移。初始化失败时先查看 `migrate` 或 `search-init` 日志，修复原因后移除这两个任务容器并重试；不要通过跳过初始化来启动业务。

镜像回退不等于数据库回退。迁移完成后是否可以运行旧代码，需要按数据库版本与兼容性判断。需要数据恢复时使用备份恢复及仓库已有的 epoch 恢复流程，不要将 `migrate down` 当作产品数据恢复入口。

## 管理与停止

管理员维护命令复用 API 容器中的二进制。用户需先注册，再执行初始管理员设置：

```sh
export GOPULSE_IMAGE_TAG="$(cat VERSION)"
docker compose exec api /app/admin-role bootstrap --user-id <实际用户ID>
```

更新证书后重新加载 Nginx：

```sh
docker compose exec web nginx -t
docker compose exec web nginx -s reload
```

`make container-logs` 跟随日志；Ctrl+C 只停止日志查看。停止并删除容器和项目网络，同时保留数据卷：

```sh
make container-down
```

持久化卷不等于备份。必须另行安排数据库和所需状态的备份与恢复验证。`docker compose down -v` 会删除本项目的数据卷，包含数据库、缓存持久化、消息和搜索索引，不得用于保留数据的停止或升级流程。依赖镜像中的初始化账号变量只对空数据卷生效；更改 `.env` 不会自动修改已有 MySQL 或 RabbitMQ 账号密码。

构建上下文使用各 Dockerfile 相邻的 `Dockerfile.dockerignore` 白名单，仅发送相应源码和 Nginx 模板，排除 `.env`、本地运行目录、前端依赖及构建产物。后端镜像使用非 root 用户；常驻后端及维护容器均使用只读根文件系统，并向标准输出写日志。
