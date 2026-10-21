# Prometheus Node Exporter 原生插件

本目录只维护官方发布包的版本、校验和、许可证及 GoPulse 插件元数据。它没有 Go 模块或框架代码。Monitor 直接运行官方 `node_exporter` 二进制，并采集原生 `node_*` 指标。

当前固定使用官方 v1.9.1，与仓库自监控部署版本一致。下载地址和 SHA-256 来自[官方发布页](https://github.com/prometheus/node_exporter/releases/tag/v1.9.1)。`upstream.json` 固定 Linux amd64、arm64 发布包的校验和。发布包中附带官方 LICENSE 和 NOTICE。

## 构建与登记

在仓库根目录执行：

```sh
make package-plugin SOURCE=node ARCH=amd64
make package-plugin SOURCE=node ARCH=arm64
```

打包工具下载并校验官方压缩包，提取其中的可执行文件，不修改二进制。也可通过现有 `BINARY` 参数提供预先准备的官方二进制。发布版本使用本目录的 `VERSION`。

本地观测环境的插件包准备流程会将此插件加入外部 catalog。使用自定义部署目录时，将生成的插件包加入部署 catalog，再由 Monitor 加载。插件 ID 为 `node-exporter`，source 为 `node`。

## 安装与配置

在管理端选择 Prometheus Node Exporter，可执行采集检查或安装。原生插件不使用目标主机地址、目标端口或凭据。最小管理请求为：

```json
{"config":{},"secrets":{}}
```

可选路径配置：

```json
{"config":{"rootfs":"/","procfs":"/proc","sysfs":"/sys"},"secrets":{}}
```

路径必须为规范的绝对路径。默认值如上。Monitor 使用固定的回环监听地址 `127.0.0.1:9127`，通过原生首页检查进程状态，通过 `/metrics` 检查实际采集结果。采集检查在临时目录、临时端口启动官方程序，检查完成后关闭进程，不保存插件配置。

在容器中运行时，默认采集容器可见的系统。要采集宿主机，部署方必须提供宿主机文件系统挂载和所需的 PID、网络命名空间访问，并设置对应路径。例如将宿主机根目录只读挂载到 `/host` 后，路径可设为 `/host`、`/host/proc`、`/host/sys`。仅修改路径不能提供宿主机网络命名空间。部署要求见[官方说明](https://github.com/prometheus/node_exporter/tree/v1.9.1#docker)。

## 指标与故障行为

Monitor 固定启用 cpu、meminfo、loadavg、diskstats、filesystem、netdev、stat、vmstat、time 和 uname 采集器，覆盖 CPU、内存、负载、磁盘、文件系统、网络和系统状态。禁用其余默认采集器，以及 Go、Process 和 promhttp 运行指标。

指标保留原名称、类型和标签，例如 `node_cpu_seconds_total{cpu="0",mode="idle"}`、`node_memory_MemAvailable_bytes`、`node_disk_read_bytes_total` 和 `node_network_receive_bytes_total`。查询链路通过 source、target 和 producer 元数据区分来源，不改写原生名称。

Monitor 添加 `gopulse_node_up` 表示本轮采集状态。任意启用的采集器报告失败时，本轮仅发布 `gopulse_node_up 0`；全部采集成功且快照校验通过时，发布业务指标和 `gopulse_node_up 1`。网络错误、HTTP 错误、非法指标或超出响应预算时，不发布该轮快照，按现有 Monitor 流程记录采集错误。原生 HTTP 响应保持官方行为。

## 验证

单元测试覆盖原生参数、配置、指标隔离、故障快照及发布包校验。真实生命周期测试需要准备插件包和对应 catalog：

```sh
cd monitor
GOPULSE_TEST_NODE_CATALOG=/absolute/path/catalog.json \
  go test -race -count=1 -run '^TestNativeNodeLifecycle$' ./internal/metrics/collector
```

测试 catalog 应只包含 node 插件。测试会使用插件声明的 9127 端口，并覆盖采集检查、安装、采集、停止、启动和 Monitor 重启恢复。
