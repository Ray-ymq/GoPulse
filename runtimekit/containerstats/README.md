# 容器资源公共采集器

`containerstats` 负责读取 Linux cgroup v2。PT5、PT6 通过独立命令的 HTTP 协议读取结果，不各自实现文件读取。当前入口是公共独立采集命令，尚未纳入 Monitor 的插件安装和打包流程。

在仓库根目录构建：

```sh
go -C runtimekit build -o /tmp/container-exporter ./cmd/container-exporter
```

调用方通过 Docker 等运行时发现容器，并原子更新私有目标文件。例如：

```json
[{"name":"backend","id":"实际容器 ID","pid":12345}]
```

`pid` 必须是采集器所见宿主机上的 PID。采集器不连接 Docker socket。最多接受 128 个目标，名称不得重复。每次采样重新读取目标文件，不缓存上次成功值。

```sh
/tmp/container-exporter --targets-file /path/to/targets.json
```

程序默认监听 `127.0.0.1:0`，仅允许回环地址。启动后向 stdout 输出一行 JSON，包含实际 `url` 和 `schema_version=gopulse.containerstats.v1`。stderr 用于错误，SIGTERM 用于结束程序。`--procfs` 和 `--cgroupfs` 可指定宿主机文件系统挂载点或测试目录。

| 接口 | 输出 |
| --- | --- |
| `GET /snapshot` | 带版本、采样开始时间、容器身份及原始计数的 JSON |
| `GET /metrics` | `gopulse_container_` 前缀的 Prometheus 指标；CPU 时间统一为秒 |

采集器读取 CPU 使用、配额和节流计数，内存当前值、生命周期峰值、上限和 swap 上限，以及 OOM 事件和任务数。`memory.peak` 在内核不支持时为 `null`；无限配额为 `null` 或 `cpu.max` 中的 `max`。这些值不能改成零。Prometheus 输出省略不支持的峰值或无限上限序列。

任一必需文件缺失、目标退出、必需计数缺失或格式无效时，整个请求返回 HTTP 503，仅输出 `gopulse_container_up 0`，不输出部分数据。采样前后检查 PID 的 cgroup 路径。调用方仍须检查容器运行状态、真实容器身份及窗口内身份变化。

`memory.peak` 是容器 cgroup 生命周期的高水位。PT 的窗口内存峰值应使用窗口内 `memory.current` 的采样最大值，并注明采样间隔。`pids.current` 是任务数，包含线程，不等于进程数。采集器只提供原始量；CPU 使用率、节流比例、窗口增量、边界覆盖和计数重置由 PT 计算。
