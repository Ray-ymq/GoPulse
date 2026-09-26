# Phase-18-02 废止 qualification 归档

本目录保存 2026-09-26 对候选 `4c62d4b1ad28ed20c49b9d6f989f1e612f65ea4e`
执行旧 Phase-18-02 qualification 时形成的可公开、可复核摘要。

## 结论

- 归档状态：`incomplete_without_terminal_receipt`
- 该运行不是验收通过，也不是完整的验收失败 receipt。
- 运行结束后没有生成最终 `qualification.json`，也没有生成 `run-error.txt`。
- 最后持久化的进度仍为 `running`，活动步骤为 `business-worker_replacement`。
- Business Worker 的部分 journal 仍为 `running` 且没有样本，不能形成产品结论。
- 本目录不能用于更新 `VERSION`、完成任何 Phase 18 批次或证明扩容能力。
- 2026-09-26 权威重划已经废止该 runner 和批次设计；本目录只用于保留历史事实。

## 已形成的有限证据

以下步骤在最后一份持久化进度中记录为 passed：

1. 宿主预检。
2. acceptance topology。
3. MySQL/Kafka parser fixture。
4. MySQL lease live probe。
5. cold-start readiness probe。
6. Backend replacement。
7. Backend replacement product outcome 分类。

可复核的摘要包括：

- parser fixture：MySQL NULL/空 owner/微秒时间/缺失行以及 Kafka empty/single/dual/rebalance/
  unassigned/no-offset 用例通过。
- MySQL live probe：future lease、expired reclaim、NULL/空字符串和时间精度观测通过。
- readiness：Business Worker 与 Search Indexer 各处理并确认 100 条；Kafka 为 4 partitions；
  Router 发布 100 条；Backend 3 秒 steady 取得 5,241 个成功请求。
- Backend replacement 固定负载共 28,125 个请求，成功 28,125，显式拒绝、timeout 和 error 均为 0；
  移除 `backend-2` 时 survivor progress 为 44，停止窗口约 5.109 秒，replacement journal 为 passed。

这些结果仅证明对应旧脚本步骤曾完成。由于顶层运行没有终态 receipt，后续 live Kafka、
Business Worker 及未执行步骤均不能推断为通过。

## 归档边界

- 原始 work directory、候选 Bundle、镜像和大体积资源样本没有提交到 Git。
- `evidence-checksums.sha256` 绑定原始附件内容；原始附件仍按私有运行材料处理。
- `candidate-binding.json` 只保存候选 digest，不保存 registry 地址、凭据或私有路径。
- `qualification-progress.sanitized.json` 是脱敏事实摘要，不冒充原始 runner receipt。
- `resource-state.json` 记录归档时的资源检查。旧候选 registry 仍运行且没有强归属 label，
  因此未被本次归档操作删除。
