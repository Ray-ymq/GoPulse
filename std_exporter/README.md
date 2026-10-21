# GoPulse 标准 Exporter

本工程以 `rabbish/std_exporter` 为基础，保留原目录结构、`prometheus.Collector` 和 `ScrapeFns` 注册方式。工程独立实现 GoPulse 接入协议，不依赖 `runtimekit` 或项目业务模块。日志继续使用 logrus，写入 stderr。

## 构建和运行

需要 Go 1.26。打包另需 Python 3。

```sh
cd std_exporter
make test
make build
./build/gopulse-std-exporter check-config --config config/example.json
./build/gopulse-std-exporter check-connection --config config/example.json
./build/gopulse-std-exporter serve --config config/example.json
```

示例目标必须支持 Basic Auth，并提供两个接口：`GET /` 返回 HTTP 200；`GET /api/stats` 返回 HTTP 200 和 JSON 对象 `{"value":42}`。`check-config` 不连接目标；`check-connection` 只访问 `/`；`/metrics` 每次访问目标并采集 `value`。示例不会产生固定的假业务值。

`config/example.json` 只包含演示凭据。实际配置文件应限制为运行用户可读，不得提交真实凭据。

保留手动启动参数：

```sh
./build/gopulse-std-exporter -host=127.0.0.1 -port=8080 -user=example -password=example-only -timeout=5s
```

`-timeout` 使用 Go duration 格式，例如 `5s`。监听地址默认 `127.0.0.1:9601`；`-web.telemetry-path` 必须为 `/metrics`。Linux 下擦除 argv 中的密码；由 Monitor 管理时使用配置文件传入凭据。

## 文件职责

| 文件 | 职责 |
|---|---|
| `main.go` | 命令、HTTP 服务、信号处理和退出 |
| `common/define.go` | 数据源名称、插件标识、指标前缀和构建版本 |
| `config/config.go` | 严格解析、配置校验及脱敏 |
| `collector/client.go` | 目标连接、认证、请求超时和资源释放 |
| `collector/exporter.go` | 采集调度、指标校验、编码及 HTTP 响应 |
| `collector/scrapeFn.go` | 采集函数签名与注册表 |
| `collector/example.go` | HTTP JSON 采集示例 |
| `logger/` | 原日志实现 |
| `descriptor.json` | 插件元数据 |
| `config.schema.json` | GoPulse 配置声明，不是通用 JSON Schema |
| `VERSION` | 发布版本，构建时注入 `common.Version` |
| `package.py` | 构建 Linux 二进制并生成插件包 |

## 配置规则

配置文件使用 `config`、`secrets`、`runtime` 三段。限制为 64 KiB，拒绝重复键、未知字段、null 和连续多份 JSON 文档。

示例配置要求目标主机、端口、用户名、密码及连接和采集超时。host 模式只允许 localhost 或回环 IP；container 模式允许 DNS 主机名。监听地址在 host 模式下必须为回环 IP；container 模式还允许未指定地址。

连接超时不得小于 100 ms，不得大于采集超时；采集超时不得大于 10 s。若 `runtime` 也声明连接或采集超时，框架取其与插件配置的较小值，并确保实际连接超时不大于实际采集超时。退出超时默认 5 s，可配置为 100 ms 至 30 s。运行时监听地址和端口默认 `127.0.0.1:9601`。

管理命令向 stdout 输出一行 JSON，成功返回退出码 0，配置或运行失败返回 1，管理命令参数错误返回 2。错误结果只包含稳定错误码，不输出底层错误或凭据。

## 开发新插件

1. 复制本工程，修改 `go.mod` 的模块名及 Go import 路径。
2. 修改 `common/define.go` 中的 `Source` 和 `PluginID`，同步元数据、配置声明、Makefile 的二进制名称。构建会自动读取当前 Go 模块路径。
3. 修改 `config/config.go` 的数据源字段、配置校验及 `Redacted()`，同步 `config.schema.json` 和示例配置。协议中的 `runtime` 字段保留。
4. 在 `collector/client.go` 实现 HTTP、SQL 或 SDK 目标访问。连接检查必须执行真实的连接或认证检查，关闭方法必须释放客户端资源。
5. 编写 `scrapeXxx(ctx, client, ch) error`，注册到 `ScrapeFns`，移除示例采集函数。所有注册项都按必需采集处理；任意一个失败，本轮不返回业务指标。
6. 执行测试、构建和打包，再向部署使用的 Monitor 外部 catalog 登记插件。插件包本身不会自动注册新数据源。

在程序启动前完成 `ScrapeFns` 注册，运行期间不得修改注册表。`Describe()` 不连接目标，使用 unchecked collector 模式；每轮 Gather 仍检查指标一致性及重复序列。

同一实例同时只执行一轮采集，等待执行权计入请求超时。轮内最多执行 4 个采集函数。共享客户端必须支持并发使用；不支持时需要在客户端内串行访问。采集函数不得自行关闭指标通道，不得在返回后继续发送指标。

所有目标请求必须接收 `context`，并设置底层 I/O 超时。向通道发送指标时必须同时监听 `ctx.Done()`，参见 `example.go`。Go 无法强制停止忽略取消的采集函数；此类实现会阻碍本轮结束。框架不会缓存旧快照或将失败数据填为零。

## HTTP 和指标协议

- `/health` 表示插件 HTTP 服务状态，不连接目标。版本低于 1.14.3 时返回 `status` 和 `service`，版本从 1.14.3 起按当前 Monitor 的版本判断返回 `status` 和 `contract_version: "2"`。
- `/metrics` 成功返回 HTTP 200、`gopulse_std_up 1` 和本轮业务指标。
- 采集、校验或编码失败返回 HTTP 503，正文仅包含 `gopulse_std_up 0`，不含旧数据和其他指标。
- 指标使用 `gopulse_<source>_` 前缀。第一版接收 Gauge 和 Counter，拒绝 NaN、Inf、负 Counter、显式时间戳及保留标签。
- 指标响应最多 1 MiB、512 个指标族、8192 个样本；每个样本最多 16 个标签。名称、标签和值的长度限制与 GoPulse 当前接收契约一致。

没有默认注册 Go、Process 或额外 `exporter_up` 指标，避免引入协议外指标。插件进程状态通过 `/health` 检查。

## 验证和打包

```sh
make check
make race
make package ARCH=amd64
make package ARCH=arm64
```

包输出到 `build/gopulse-std-<version>-linux-<arch>.tar.gz`，包含 `plugin.json`、`descriptor.json`、`config.schema.json` 和可执行文件。包清单版本为 2，运行协议版本为 1，指标契约版本为 2。打包时计算二进制和配置声明的 SHA-256；配置文件和凭据不会放入包内。

本仓库已将模块登记为 `std_exporter`，可从根目录运行 `make test MODULE=std_exporter`。复制到其他位置后，该工程仍可独立运行其 Makefile。
