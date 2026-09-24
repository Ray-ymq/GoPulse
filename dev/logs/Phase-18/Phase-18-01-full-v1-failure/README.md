# Phase-18-01 full-v1 失败证据

本目录保存 `2026-09-24` 唯一一次正式三轮的失败证据。执行状态为
`failed`，没有生成或冒充 `capacity.json`。不得在本证据上补写通过结果，
也不得因未达到重复性门禁而重跑筛选轮次。

## 结果

- 候选版本：`2.0.1`
- 候选 revision：`5aca8753587a3e23730ecc5f90e45dd6ae8e5470`
- candidate manifest SHA-256：`56c67a79445b3e3175a92b08e444b10eda746ac89a6aee76e8bad1eb0782c84e`
- 负载源码 commit：`0a71f57be2f01d3950d490c7973a225cc7e3b5bc`
- 负载二进制 SHA-256：`eb19673497ca0adee4204237da1f5ecbf5f1c85f64b9e59d5eb6bfd2057c5354`
- corpus SHA-256：`e9c6688eddc89c4fbd49d3246ea6b3896af153c33bd8b1e20cbcc4d9048e63e0`
- RPS 偏差：`0.0%`（门禁 `<= 5%`）
- P95 偏差：`93.845%`（门禁 `<= 10%`）
- P99 偏差：`100.036%`（门禁 `<= 10%`）
- 失败阶段：repeatability
- 失败原因：`repeatability_gate_failed`

三轮均完成 `193500` 个请求。第 3 轮记录 `18` 个 500，其中 steady
`8` 个、burst `10` 个；该轮 burst 最大调度滞后为 `360.395629 ms`。
三轮恢复截止时仍有约 `38K` 个 Outbox pending，第一瓶颈分别记录为
Backend Outbox backlog 和 loadtest 调度。

## 文件和校验

- `capacity-failure.json`：完整失败汇总，包含三轮 load report、资源摘要、
  convergence 与本轮绑定。
- `run-error.txt`：正式 runner 的最终错误。
- `evidence-manifest.json`：归档成员、原始大小、SHA-256、候选与 corpus 绑定。
- `phase18-01-full-v1-failure-evidence.tar.gz`：除凭据和被绑定二进制外的全部原始证据。
- `phase18-01-full-v1-failure-evidence.tar.gz.sha256`：归档校验文件。

归档保留三轮 `resources.raw.jsonl`、`resources.json`、`load-report.json`、
`load-diagnostic.json`、`load-binding.json`、`corpus.json`、recipe receipt、
samples receipt、preflight 和失败汇总。

以下文件按安全或可复现原则排除：

- `*.env`：包含运行时凭据和 token。
- `credentials.json`：包含负载登录密码和账号凭据。
- `bin/*`：可由已记录的源码 commit 重建，且二进制 SHA-256 已写入每轮
  `load-binding.json`。
- `.lock`：空锁文件。

归档校验：

```bash
cd dev/logs/Phase-18/Phase-18-01-full-v1-failure
sha256sum -c phase18-01-full-v1-failure-evidence.tar.gz.sha256
```

## 停止状态

- 没有重跑正式三轮，没有换种子或候选。
- 没有生成 `capacity.json`。
- 受管 Compose project 已清理，候选 registry 已停止。
- 未执行 Docker global prune。
- Phase-18-01 未通过，未启动 Phase-18-02。
