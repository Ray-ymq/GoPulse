# Phase-18-01 排查结论

排查范围：`develop/2.0.1` 当前工作树，候选 `5aca875...`，只读分析，未修改门禁、未运行完整压测、未启动 Phase-18-02。环境已清理，无 Phase-18 Compose project 可做短测。

## 1. 重复性

**已证实事实**

- 十次工作目录中有三轮结果的只有 v8、v9、v10；v1–v5 无 `load-report.json`，v6 只有 1 轮，v7 只有 2 轮。v8 使用共享 channel 的旧 load 二进制，v9/v10 使用 `8da9b90` 后的确定性 per-VU channel 二进制。
- v8/v9/v10 均绑定同一候选、同一 manifest、同一 recipe digest 和同一 baseline 配置；归一化端口的 env 无差异。
- 每轮 steady RPS 均为 `150.000`，RPS 偏差 `0%`，dropped slots 均为 0；因此失败不是到达率或配方漂移。
- 三次完整运行均在最终 evidence 写入前被 `three-round repeatability gate failed` 拒绝；`capacity.json` 不存在。
- steady 最差类别 P95/P99（ms）：v8 `53.98/64.64, 250.90/327.13, 248.96/334.30`；v9 `52.51/60.42, 178.74/242.16, 53.64/72.21`；v10 `53.14/63.19, 185.16/264.15, 185.44/244.83`。
- 偏差：v8 `P95 106.67%, P99 111.42%`；v9 `132.92%, 145.48%`；v10 `93.67%, 105.37%`。高值轮由 `content_write`（尤其 DELETE post）主导，但同一轮内所有路由都整体变慢。
- load 调度最大滞后在 v9 第 2 轮仅 `7.27 ms`、v10 高值轮仅 `5.56/6.94 ms`；负载器 RSS 峰值均低于 57 MB。v8 的 `364.21 ms` 滞后不足以解释 v9/v10 的同样异常。
- 资源采样实际平均间隔为 `9.28–9.41 s`，不是每 5 秒；三轮的稳态 Outbox 增长斜率几乎相同，未随高/低延迟轮变化。

**原因或待证假设**

- 重复性根因：**未定位**。已排除固定候选/配方/配置、RPS、dropped slots、跨运行负载器饱和；当前最具体证据是周期性 backend/MySQL 整体停顿，但“DELETE 触发的数据库争用”或“采样器干扰”均只是待证假设。
- Outbox 积压与 HTTP 延迟的稳态相关性不足，不能判为因果；高延迟和非高延迟轮的稳态积压增长均约 `1,958–2,017/min`。

**证据文件**

- `.run/phase18-01-final-v8|v9-test|v10/round-{1,2,3}/load-report.json`
- 同目录下 `resources.json`、相邻轮次 `candidate.env`、`baseline.env`、`recipe.env`
- `.run/phase18-01-final-v9-test/run-error.txt`、`.run/phase18-01-final-v10/run-error.txt`

**仍缺少的最小证据**

- 高值轮内按秒/按请求的延迟轨迹，以及对应 `backend`、MySQL、宿主机 CPU/I/O 日志与 5 秒采样；需能圈出一次具体停顿窗口。当前无受管环境，不能补跑。

## 2. Burst 错误

**已证实事实**

- 修复调度后的 v9/v10 burst 错误为：v9 `1083/1083/1086`，v10 `1083/1083/1083`；`timeouts=0`、`explicit_rejects=0`，错误率均约 `3.008%`。
- 用当前 `corpus.json` 和确定性路由只读重放 193,500 个槽位，得到 404：`GET post 773`、`POST comment 540`、`PUT like 278`、`DELETE like 274`、`PUT bookmark 252`，合计 `2117`；分窗为 warmup `0`、steady `1034 (0.766%)`、burst `1083 (3.008%)`，与 v9/v10 报告完全一致。
- 根因已证实位于负载配方：`loadtest/internal/recipe/generate.go` 的 `corpusFor()` 从 ID `10241` 起把帖子加入 read/interaction 池，但 `delete_post_ids` 使用跨 5,000 偏移的 ID，二者仍有 `5,904` 个交集；DELETE 后仍按旧池读取/互动/评论，服务正确返回 404。
- `TestCorpusPartitionsSessionPostsWithoutOverlap` 只检查 session 内的 edit/delete 分区，没有断言 delete 池与 read/interaction 池不相交，因此漏掉了该缺陷。
- v8 另有 403 和少量 500，其中 403 来自旧共享 channel 调度导致 VU 编辑非本人帖子，已在 `8da9b90` 修正；不应与 v9/v10 的测量混用。
- v9/v10 仍有少量 500：v9 第 2 轮 search 8 次、第 3 轮 delete/notification/user 共 12 次；v10 第 2 轮 delete 9 次。其具体错误码/堆栈未落盘。

**原因或待证假设**

- burst 的 404 失败：**已定位为负载工具配方缺陷**，不是产品超时或背压拒绝。
- 少量 500：**未定位**。现有 route status 不是分窗证据，不能据 route 汇总直接断言 burst 路由；仅能由 phase category counters 交叉确认上述归属。

**证据文件**

- `loadtest/internal/recipe/generate.go:365`
- `.run/phase18-01-final-v9-test/round-{1,2,3}/corpus.json`
- `.run/phase18-01-final-v9-test/round-{1,2,3}/load-report.json`
- `.run/phase18-01-final-v10/round-{1,2,3}/corpus.json`
- `.run/phase18-01-final-v10/round-{1,2,3}/load-report.json`

**仍缺少的最小证据**

- 对每次 500 获取 backend/Nginx 的 request-id、错误码与请求路径；否则不能继续缩小 500 根因。404 本身已有完整证据。

## 3. Outbox

**已证实事实**

- baseline 实际配置为 `OUTBOX_POLL_INTERVAL=1s`、`OUTBOX_CLAIM_BATCH=10`，名义最大 claim/publish 能力为 `600/min`；recipe 阶段的 `10ms/100` 不是候选 baseline 配置。
- v8/v9/v10 的 steady 净积压增长为约 `1,958–2,017/min`，结束时 pending 约 `31.9k–33.2k`；burst 结束时约 `37.1k–37.7k`。
- 10 分钟恢复期 net pending 降幅仅 `5.7–45.3/min`，结束时仍约 `37.0k–37.6k`；oldest age 最终为 `1647–1677 s`。多轮在峰值后数分钟保持不变，之后也常以每次 10 条的批次缓慢下降。
- 因而可证实：实际 drain 远低于配置的 `600/min`，系统在 10 分钟恢复窗口内不能清空；但不等于已证明 Outbox 造成 HTTP P95 跳变。
- `first_bottleneck=outbox_backlog` 的规则可被首个 `pending >= 10` 或恢复末仍有 pending 触发，不能单独作为 HTTP 延迟致因。

**原因或待证假设**

- “存在持续 Outbox 积压”已证实；“为何实际发布率远低于配置上限”**未定位**。待证假设为 claim 失败/等待、RabbitMQ publish-confirm 阻塞或旧记录反复退避。
- “Outbox 造成 steady 重复性失败”未被证实；现有采样没有覆盖到高延迟瞬时的数据库锁等待或具体 publish 失败。

**证据文件**

- `.run/phase18-01-final-v8|v9-test|v10/round-{1,2,3}/resources.json`
- 同目录下 `baseline.env`、`candidate.env`
- `scripts/ci/phase18_capacity.py`、`scripts/ci/phase18_sampler.py`

**仍缺少的最小证据**

- MySQL 按 `status/attempt_count` 的计数与最早 `available_at`，以及 `gopulse_backend_outbox_last_publish_success_timestamp_seconds` 和 outbox claim/publish failed 日志的同一时间窗；当前没有可复用的受管环境。

## 结论与下一次最小验证

主要瞬时验收失败可拆成：404 已定位为负载配方池重叠；500 和 steady 重复性仍未定位；Outbox 积压已证实但因果未证实。Phase-18-01 仍不能验收。

下一次最小动作：先补一个纯单元/静态验证，断言 delete 池与 read/interaction 池完全不相交；待已有受管环境可用时，再只做一个最长 5 分钟的诊断窗口，同时采集按秒延迟、500 的 request-id 日志、Outbox status/last-publish 指标。环境当前不存在，本次停止，不启动下一轮三轮压测。

## 4. 2026-09-24 一次短测后的更新

本次已按 `Phase-18-01-定界排查.md` 执行一次且仅一次新配方短测：未修改候选镜像、配方规模或验收门禁，未运行固定三轮 runner，未生成或冒充 `capacity.json`。短测负载完成，但 load 后汇总器遇到空 5xx 列表被编码为 `null` 的缺陷，因此原始按秒证据完整、Outbox 与资源同窗样本未在当前流程退出前落盘；Compose 项目已在 `finally` 清理，无残留容器、卷或网络，临时 registry 已恢复停止。该实现缺陷已在 `c6c64a3` 修复并加回归测试，但按计划没有重跑。

**候选与负载绑定**

- 产品候选：`5aca8753587a3e23730ecc5f90e45dd6ae8e5470`；manifest SHA-256 `56c67a79445b3e3175a92b08e444b10eda746ac89a6aee76e8bad1eb0782c84e`。
- 配方修复基线：`e4e7fd7`；本次诊断负载工具 commit：`166b08b`。
- load 二进制 SHA-256：`d62bbc06b6e91df93aafeffb9ce7747e98f109a8e29de89a786557ae763f26ed`。
- recipe 二进制 SHA-256：`4aaa5e080eb4a8eef64baf97dacc84d87deebb293e4d5a0ca3811c143e866231`。
- 新 `corpus.json` SHA-256：`e9c6688eddc89c4fbd49d3246ea6b3896af153c33bd8b1e20cbcc4d9048e63e0`。
- 唯一受管 project 的 SHA-256：`66b8639ab8ab3bc7c1f7b17c9384dfa38d8d1fb1c9e783e543ab7f5408f95cc1`；清理结果 `passed`。

**404：本次短测已消失，配方修复通过一次运行时确认**

- 已证实事实：steady 18,000 请求和 burst 18,000 请求全部成功，错误、超时、明确拒绝均为 0；分路由状态中不存在任何 404。steady 与 burst 的状态均为 `200=14,355`、`201=1,350`、`204=2,295`。
- 原因：旧 corpus 的 DELETE 池与 read/interaction 池交集已在 `e4e7fd7` 修复；本次新 corpus 在短测窗口内没有再访问已删除帖子。
- 仍缺少的最小证据：三轮重复性运行中的 404 全零证据。本次仅证明一次最长 5 分钟短测内未复现，不能替代正式门禁。

**500：本次未观察到，根因仍未定位**

- 已证实事实：本次 36,000 请求没有 404/500/其他非预期状态，也没有 timeout 或 transport error，因此没有可关联的 500 `X-Request-ID` 样本。
- 原因或待证假设：原先 v9/v10 的少量 500 **仍是未定位**。新 corpus 消除了 DELETE 池重叠，可能同时消除了部分由删除后访问引发的错误，但本次零 500 不足以证明因果或证明旧 500 已修复。
- 仍缺少的最小证据：再一次稳定复现 500 时，从负载响应头保存 `X-Request-ID`，并与 Backend `http request completed` 的 `request_id/error_code/route/duration_ms` 对齐。本次已实现该采集能力，但没有 500 可采。

**稳态延迟：本次无旧轮次级周期性跳变，但重复性根因仍未定位**

- 已证实事实：steady 18,000 请求全部完成，dropped slots 为 0，`max_schedule_lag_ms=5.2715`，负载器 RSS 峰值约 36.7 MB，负载器不是本次第一瓶颈。类别延迟 P95/P99 为 read `14.69/17.32 ms`、search `16.72/83.81 ms`、content write `15.89/30.63 ms`、interaction write `7.92/10.87 ms`、notification/bookmark read `12.71/23.81 ms`、identity/session `54.58/67.55 ms`。
- 按秒窗口：steady 共 116 个有请求窗口；窗口 P95 的中位数为 `12.79 ms`，P95 为 `44.41 ms`，P99 为 `72.20 ms`；仅首个无预热窗口 P95/P99 达到 `218.82/257.77 ms`，其余仅 3 个窗口 P95 大于 50 ms。burst 60 个窗口的 P95 最大为 `44.23 ms`。
- 原因或待证假设：本次没有出现旧 v9/v10 同高度或相邻秒周期性整体停顿；首个窗口高值更像无预热冷启动，但不能由此确认旧异常根因。稳态重复性根因仍是 **未定位**。
- 仍缺少的最小证据：同一候选在正式长度三轮中的按秒窗口，并在高值窗口同秒保存 Backend/MySQL CPU、连接与锁等待、I/O，才能判定数据库停顿或其他周期性依赖是否真实存在。本次采样器已准备这些字段，但汇总异常后原始资源样本未落盘。

**Outbox：本次未取得可用同窗证据，发布率原因仍未定位**

- 已证实事实：本次没有能力从已清理的临时容器恢复 Outbox 同窗样本；`resources.json` 未生成，因此不得用本次短测声称 pending 走势、claim/publish 成功失败、retry 或 last-success 已改善或恶化。
- 原因或待证假设：此前已证实实际 drain 远低于名义配置上限；具体原因仍是 **未定位**，仍需区分 claim 等待、publish-confirm 阻塞、旧记录退避或数据库争用。
- 仍缺少的最小证据：同一负载时间窗内的 Backend `outbox_pending`、`oldest_age`、`last_publish_success`，MySQL `pending/leased/published`、`attempt_count`、`available_at` 与 row-lock/慢查询状态，以及 Backend `outbox claim failed`、`outbox publish failed`、`outbox release failed`、`outbox event published` 日志计数。诊断 runner 已补齐这些采集项，但本次因上述汇总缺陷没有在退出前持久化资源记录。

**结论与停止点**

- 404 已通过新 corpus 的一次运行确认消失；Phase-18-01 的正式三轮重复性、500 根因和 Outbox 发布率仍未闭环，不能宣布验收通过。
- 诊断准备、一次短测和清理均已完成；未重试、未换种子、未另建 project、未执行全局 prune、未启动 Phase-18-02。
- 原始按秒证据：`.run/phase18-01-diagnostic-v1/diagnostic/load-diagnostic.json`。
- 汇总负载证据：`.run/phase18-01-diagnostic-v1/diagnostic/load-report.json`。
- 部分诊断摘要：`.run/phase18-01-diagnostic-v1/diagnostic/diagnostic-summary.partial.json`。
- 运行异常与绑定：`.run/phase18-01-diagnostic-v1/run-error.txt`、`.run/phase18-01-diagnostic-v1/binding.json`、`.run/phase18-01-diagnostic-v1/evidence/preflight.json`。
