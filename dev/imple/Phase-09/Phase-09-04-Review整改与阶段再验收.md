# Phase-09-04 Review 整改与阶段再验收

## 1. 批次目标

在 `develop/1.6.4` 上关闭 `dev/review/2026-09-04-Phase-9实现Review报告.md` 的全部 findings，不扩展 Phase 9 产品范围：

1. 为日志 shipper 的 `Enqueue`/`Close` 建立明确线性化边界，保证返回成功的队列项由关闭流程接管并在超时边界内 drain。
2. 对持续 queue-full 状态日志做有界节流，并为受限指数退避加入可测试 jitter。
3. 让 Marshaller 在运行期重新验证 Elasticsearch 日志 template 合同，并在写入后确认目标索引仍具有 strict mapping 与固定 read alias，避免空集群替换后提前提交 offset。
4. 将管理员日志查询的 `module`、`message` 与 `error_code` 收敛到 Schema v1 已知词汇。
5. 更新实施记录、阶段文档和版本，重新通过直接受影响检查、必要回归与真实日志链路验收。

## 2. 范围与非目标

- 只修改直接涉及上述 Review findings 的 Backend、Marshaller、日志验收脚本、Phase 9 文档与版本文件。
- 不新增日志源、查询参数、Elasticsearch 生命周期管理、Frontend 页面或 Phase 10 能力。
- 不开展通用代码审计、依赖升级、覆盖率扩张或无失败依据的重构。
- 不修改冻结的 PowerShell 脚本。

## 3. 验收标准

- 并发 `Enqueue`/`Close` 的仓库回归测试可确定性证明：关闭线性化点后新 enqueue 被拒绝；该线性化点前返回成功的项不会留在无人消费队列。
- 持续 queue-full 时状态日志数量有界，队列恢复后可再次报告；retry delay 始终位于配置范围内并包含可注入、可验证的 jitter。
- Marshaller 保持运行而 Elasticsearch 被替换为空集群后，固定 template 会重新建立；目标日志索引具有 `dynamic: strict` mapping 和 `gopulse-logs-v1-read` alias，验证失败时写入返回暂时失败且 consumer 不提交 offset。
- 管理员查询接受代表性合法 `module/message/error_code`，拒绝未知词汇并返回既有 `validation_failed` 边界。
- Phase 9 总方案对 Phase-09-04、`1.6.4`、`develop/1.6.4` 存在唯一权威分配，版本文件一致。
- `dev/logs/Phase-09/Phase-09-04-Review整改与阶段再验收.md` 如实记录实际改动、命令结果、偏差和限制。

## 4. 固定完成门禁

```bash
(cd backend && go test ./internal/observability/logship ./internal/logquery)
(cd backend && go test -race ./internal/observability/logship ./internal/logquery)
(cd marshaller && go test ./internal/elasticsearch ./internal/consumer)
(cd marshaller && go test -race ./internal/elasticsearch ./internal/consumer)
(cd backend && go test ./... && go vet ./...)
(cd marshaller && go test ./... && go vet ./...)
bash scripts/verify-logs.sh --self-test
bash scripts/verify-logs.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.4 --base-ref origin/main
git diff --check
```

只有上述验收标准与固定门禁全部通过、无阻断问题，才可更新实施记录与 `VERSION`/Frontend 版本为 `1.6.4`，提交并推送本批次。
