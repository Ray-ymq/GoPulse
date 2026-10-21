# 请求序列

默认 `smooth-mixed` 在每个 100 请求周期保持配置比例，并按各类别出现次数轮换路由。用户按每种路由的访问序号选择；每名用户的编辑、删除目标另按访问轮次轮换。

配置样例：

- `smooth-mixed.json`：均匀交错的混合业务。
- `burst-mixed.json`：保留连续类别和路由的明确突发场景。
- `hot-repeat.json`：只访问详情，在 16 篇帖子中重复选择。
- `cold-distinct.json`：只访问详情，每个阶段（含预热）只遍历一次不同帖子；样例只有一个边界阶段。必须准备足够且尚未缓存的语料，耗尽后会报错，不循环冒充冷读。重复运行前需重新准备语料。

`delete_succeeded` 只统计 HTTP 204，`delete_already_absent` 单独统计 HTTP 404；请求结果兼容原来的允许状态码。

只检查序列、语法和报告解析（不发送 HTTP 请求）：

```sh
node --test perflab/k6/*.test.mjs
node --check perflab/k6/workload.js
go -C perflab test ./internal/k6
```

离线检查覆盖精确比例、路由交错、每个用户的全部删除目标、热点重复率、冷读唯一性和语料耗尽。这些检查验证脚本和报告统计，不提供 GoPulse 的容量结论。

## 多档位指标内存

`workload.js` 使用三个原生指标记录窗口计数、成功率和延迟，以固定的 `summary_metric` 标签区分阶段与路由。初始化运行时一次性注册全部子指标；业务 VU 只保留当前档位和阶段的句柄，不复制全部汇总配置。空阈值只启用子指标聚合，不增加业务门槛或停压条件。

`handleSummary` 将子指标恢复为原有的 `business_requests_stage0_measurement`、`route_latency_stage0_measurement_1` 等字段，Go runner 的报告格式保持一致。原始 JSON 采样中的窗口指标改用 `window_summary_count`、`window_summary_rate`、`window_summary_latency` 及上述标签；按原始指标名处理数据的外部工具需要同步调整。

`window-metrics.test.mjs` 在固定 k6 二进制已安装时，额外运行不发送 HTTP 请求的指标测试，核对跨窗口计数、成功率和 P95/P99。未安装时跳过该项。PT4/PT5 使用独立的 `test4.js`，不使用此窗口指标实现。

2026-10-21 的单次本机模拟对照使用 k6 0.56.0、512 VU、9 个档位、120 RPS 和 18 秒业务窗口，语料规模与现有 PT3 一致，请求只发往临时 HTTP 服务。修复前后均启用 k6 pprof，并每 100 ms 读取进程 RSS；峰值分别为 2,176.7 MiB 和 646.2 MiB，下降约 70%。原始记录位于 `.run/k6-memory/before-pt3-9stages-1792570252357050188/` 和 `.run/k6-memory/after-pt3-current-window-1792570552249886702/`。这次短时对照只验证指标对象的内存开销，不代表长期运行的内存上限或业务容量。
