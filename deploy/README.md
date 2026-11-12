# 业务部署

当前仅部署社交业务。`MODE=business` 使用业务 Compose 文件或 Kubernetes overlay；`MODE=all` 保留为业务全栈入口，不再包含管理平台。

## Compose

在仓库根目录配置 `.env` 后执行：

```sh
make build-images
make stack-up MODE=business
make stack-status MODE=business
make stack-verify MODE=business
make stack-down MODE=business
```

核心服务为用户前端、业务 API、Worker、Indexer、MySQL、Redis、RabbitMQ 和 Elasticsearch。数据库迁移和搜索初始化由一次性任务执行。停止命令保留命名卷。

## Kubernetes

集群工具位于 `tools/devtools/kubernetes`，资源位于 `deploy/kubernetes`。

```sh
make k8s-check
make k8s-render MODE=business
make k8s-load-images MODE=business
make k8s-up MODE=business
make k8s-verify MODE=business
make k8s-down MODE=business
```

集群初始化入口为 `make k8s-install`。部署工具检查工作区归属，并在 Compose、Kubernetes、源码会话和压测之间实施互斥。不要删除不属于当前工作区的资源。

## 配置与升级

环境变量示例统一位于根 `.env.example`。`BACKEND_SERVICE_ROLE` 只接受 `business`，旧 `combined` 和 `platform` 值须移除或改为 `business`。

`000021` 迁移删除历史告警表。执行前应备份需要保留的数据；降级只能恢复空表结构。旧平台容器、持久卷和本地凭据不会因源码删除而自动清理。

业务保留本地日志和健康探针。指标接口、指标认证配置、采集器与追踪部署资源已删除。压测保留负载生成、环境检查和业务数据核对。
