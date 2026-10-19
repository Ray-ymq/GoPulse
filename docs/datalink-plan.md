# 独立自监控改造计划

## 目标

1. 前端增加“启动自监控”“停止自监控”“打开 Grafana”按钮。
2. 用独立 Docker Compose 启动 Prometheus、Grafana 和 Exporter，监控全部 GoPulse 服务、中间件及主机；数据持久保存。
3. 用标准 Prometheus 埋点替换并彻底删除 `componentmetrics`，原有产品采集链路保持原行为。
4. 完成测试并提交 Git commit。GitHub 只提交代码、配置和仪表盘，不上传二进制、镜像或监控数据。

## 实施约定

- 部署环境为 Linux 单机。自监控使用独立 Compose 项目，镜像固定版本和 digest，首次启动可拉取镜像。
- 自监控由 Prometheus 直接采集、Grafana 展示，不经过 Monitor、Kafka、Marshaller 或 VictoriaMetrics 的产品数据链路。
- 监控范围包含 Backend、Worker、Indexer、Monitor、Marshaller 的全部运行实例，MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics，以及主机 CPU、内存、磁盘、网络和文件系统。
- 中间件通过原生指标端点或独立 Exporter 采集，主机使用 Node Exporter。独立 Exporter 不依赖产品插件管理器；必要插件及只读采集凭据在部署阶段准备。
- 按钮仅供超级管理员使用，调用固定的状态、启动和停止接口。后端通过受限的 systemd 控制单元管理固定 Compose 项目，不获得通用 Docker 权限，不接受前端传入命令、路径或服务名。
- 页面展示真实的启动、运行、部分不可达和失败状态，支持重试并避免重复创建容器。关闭页面或重启管理后端不停止自监控。
- 默认不启动，也不随主机开机自动启动。停止只处理自监控项目，保留数据卷，不影响业务服务。
- Grafana 使用独立登录和站点反向代理入口；Prometheus、Grafana 和 Exporter 默认监听回环地址，Prometheus 管理接口不对外开放。
- Prometheus 默认采集间隔为 15 秒、超时为 5 秒、数据保留 15 天。本次提供采集和仪表盘，不新增告警通知渠道。
- 指标定义归各模块内部所有，使用官方 Prometheus Go 客户端；保留现有关键指标含义，限制标签取值范围。日志、健康检查、停机工具和产品传输协议等仍需共享的能力分别归位后，彻底删除 `componentmetrics` 及全部依赖。
- 清理旧自监控在产品采集、查询、页面及告警中的接入，保留组件健康检查、外部数据源功能和历史数据。升级前停止旧组件指标发布并排空消息。

## 执行顺序与验收

1. 接通独立 Docker 采集和 Grafana 仪表盘，再完成前端按钮及启停接口。
2. 替换旧指标实现，迁走必要公共能力，删除 `componentmetrics`，同步更新构建、配置、运行契约、开发工具和 CI。
3. 验证启动、停止、重试、权限限制和数据持久化；服务就绪后 45 秒内出现首批 GoPulse 样本，缺失数据不能显示为零。
4. 验证 Kafka 或 Marshaller 停止后，其余存活服务仍可采集；自监控停止或故障时，业务、健康检查和原产品采集链路不受影响。
5. 完成受影响 Go 模块的静态检查、构建和 race 测试，以及管理端类型检查、测试和构建；校验 Compose、Prometheus 配置及 Grafana 仪表盘。
6. 确认不存在 `componentmetrics` 模块或有效引用，且 Git 未跟踪二进制、镜像和监控数据；按仓库提交规范记录实际验证结果，创建 commit 并交付 hash。

执行完后删除本 .md 文件，并将删除操作提交到 Git。
