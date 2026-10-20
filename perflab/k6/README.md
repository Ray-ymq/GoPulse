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
node --test perflab/k6/sequence.test.mjs
node --check perflab/k6/workload.js
go -C perflab test ./internal/k6
```

离线检查覆盖精确比例、路由交错、每个用户的全部删除目标、热点重复率、冷读唯一性和语料耗尽。配置文件仅用于后续明确授权的压测，本次没有执行 k6、R09、吞吐或 TTL 对照实验。
