# Exporter 开发与构建

`mysql`、`kafka`、`elasticsearch`、`redis`、`rabbitmq` 和 `victoriametrics` 按 `std_exporter` 的结构组织。每个插件都是独立 Go 模块，自行实现配置、采集、日志和 HTTP 服务，不依赖 `runtimekit`。

```text
<source>/
├── main.go
├── common/
├── config/
├── collector/
│   ├── client.go
│   ├── exporter.go
│   ├── scrapeFn.go
│   └── collector.go
├── logger/
├── descriptor.json
├── config.schema.json
├── VERSION
└── Makefile
```

`std_exporter` 是新插件模板，使用方法见 [模板说明](std_exporter/README.md)。现有六个插件保留原生客户端、配置字段、业务指标及可选采集项的处理方式。

## 修改采集逻辑

- `config/` 解析 `config`、`secrets`、`runtime`，校验字段并提供脱敏配置。文件加载不修改进程环境。数据源配置中的连接和采集超时保持原有优先级。
- `collector/client.go` 创建客户端，设置连接与请求超时，并释放资源。
- `collector/collector.go` 保留各数据源的原有采集逻辑。
- `collector/scrapeFn.go` 定义 `ScrapeFn` 和 `ScrapeFns` 注册表。现有完整快照登记为一个任务，保持相关指标的一致性。新增任务必须接收 `context.Context`，发送指标时必须监听取消信号。
- `collector/exporter.go` 实现 `prometheus.Collector`、采集调度和 HTTP 输出。`Describe()` 不连接目标。同一实例同时执行一轮采集，每轮最多运行四个任务。

所有注册任务均为必需任务。任意任务失败、超时、panic 或输出非法指标，本轮丢弃业务指标。原有采集逻辑内部的可选项继续按原有规则处理。

## 运行协议

Monitor 使用以下命令管理插件：

```sh
./build/gopulse-mysql-exporter check-config --config execution.json
./build/gopulse-mysql-exporter check-connection --config execution.json
./build/gopulse-mysql-exporter serve --config execution.json
```

`check-config` 只校验配置；`check-connection` 执行一次采集检查。插件也保留环境变量启动方式和 `--check`。配置文件包含凭据时，应限制文件权限，不得提交真实凭据。

`/health` 表示插件服务状态，不连接采集目标。`/metrics` 成功时返回 HTTP 200、`gopulse_<source>_up 1` 和业务指标；失败时返回 HTTP 503，正文仅包含 `gopulse_<source>_up 0`。指标输出保留响应大小、样本数和标签限制，不注册 Go 或进程指标。

## 验证和打包

在仓库根目录执行，将 `mysql` 替换为目标数据源：

```sh
make ci-check MODULE=exporters/mysql
make race MODULE=exporters/mysql
make build MODULE=exporters/mysql
make package-plugin SOURCE=mysql ARCH=amd64
make package-plugin SOURCE=mysql ARCH=arm64
```

构建入口为插件模块根目录。打包沿用现有工具链，插件包继续包含二进制、`plugin.json`、`descriptor.json` 和 `config.schema.json`。元数据中的安装路径和 Monitor 接口保持不变。
