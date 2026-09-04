# Phase-08-04：Phase 8 实现 Review 整改记录

## 1. 实施信息

- 实施日期：2026-09-04
- 权威分支：`develop/1.5.4`
- 基线：`origin/main` 的 `a7b9d94`，分支原有 `3633308 docs: add Phase 8 implementation review`
- 目标版本：`1.5.4`
- 验收输入：`dev/review/2026-09-04-Phase-8实现Review报告.md`
- 范围：关闭 P1-01、P2-01、P2-02、P3-01；未扩展 Phase 9+ 能力。

## 2. 实际完成工作

### 2.1 Commit 期间 ownership cancellation

- `Processor.commit` 在 Committer 返回错误后重新检查 lease 有效性和 lease context。由 revoke/lost 取消 in-flight commit 时返回 `ErrOwnershipLost`；ownership 仍有效的独立 commit error 继续返回 `ErrCommitFailed`。
- 增加 revoke、lost 两个定向子测试：旧 generation 的 commit attempt 被取消后不会被判为永久 commit failure；重新 assignment 后的新 lease 能继续处理并提交。
- 保留并通过 `TestProcessorCommitFailureHalts`，确认真实 ownership 未丢失时仍执行受控 halt 合同，不越过当前 record。README 明确该状态保持 liveness、readiness 失败并要求受控进程重启。
- 完整验收新增 acceptance-only `verify-group-member`：真实第二个同 group 成员接管单 partition但不提交，随后 replacement Marshaller 从最后正式 committed offset 重取并提交。

### 2.2 IPv4/IPv6 loopback 一致性

- HTTP server 改用 `net.JoinHostPort`，分别生成 `127.0.0.1:9093` 与 `[::1]:9093`。
- 配置测试明确接受 IPv4 和 IPv6 loopback；HTTP server 测试覆盖两类 listener 地址。
- `scripts/dev.sh` 增加统一 `http_url` helper，IPv6 host 自动加 bracket 后再执行 readiness 探测。源函数专项检查输出 `http://127.0.0.1:9093/ready` 和 `http://[::1]:9093/ready`。
- README 将 `MARSHALLER_HTTP_HOST` 合同明确为 IPv4 或 IPv6 loopback。

### 2.3 分支和版本治理

- Phase 8 总方案新增唯一 `Phase-08-04` → `1.5.4` / `develop/1.5.4` 权威分配、跨批次摘要、拆分方案链接和整改验收条件。
- 新增对应拆分实施方案与本实施记录。
- 根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 均更新到 `1.5.4`。

### 2.4 删除无效 poll timeout

- 删除 `Config.KafkaPollTimeout`、解析/范围校验、`.env.example` 和 `scripts/dev.sh` 传递/default。
- README 明确 Kafka poll 只由 Marshaller 运行根 context 取消，不再暴露独立应用层 poll timeout。
- 除历史 Review 和整改计划对 finding 的描述外，运行配置与实现中无 `KafkaPollTimeout` / `MARSHALLER_KAFKA_POLL_TIMEOUT` 陈旧引用。

## 3. 变更文件

- `.env.example`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `marshaller/README.md`
- `marshaller/cmd/verify-group-member/main.go`
- `marshaller/internal/config/config.go`
- `marshaller/internal/config/config_test.go`
- `marshaller/internal/consumer/processor.go`
- `marshaller/internal/consumer/processor_test.go`
- `marshaller/internal/httpserver/server.go`
- `marshaller/internal/httpserver/server_test.go`
- `scripts/dev.sh`
- `scripts/verify-marshaller.sh`
- `dev/imple/Phase-08/Phase-08-总实施方案.md`
- `dev/imple/Phase-08/Phase-08-04-Phase-8实现Review整改.md`
- `dev/logs/Phase-08/Phase-08-04-Phase-8实现Review整改.md`

工作区原有未跟踪文件 `使用指南.md` 未读取、未修改、未暂存。

## 4. 实际验证与结果

以下验证均于 2026-09-04 在 WSL2 Linux filesystem `/home/ray/GoPulse` 执行。

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
```

结果：通过。Marshaller 全部 package、`cmd/verify-group-member` 构建、vet 和 race 均成功。

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh
scripts/verify-marshaller.sh --self-test
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.4 --base-ref origin/main
git diff --check
```

结果：全部通过；无 Docker self-test 拒绝 9 个不安全配置/project/query/port 场景；25 项 CI unittest 通过；版本元数据一致；branch governance 确认 `develop/1.5.4` 唯一映射到 Phase-08-04。

```bash
eval "$(awk '/^http_url\(\) \{/,/^}/' scripts/dev.sh)"
test "$(http_url 127.0.0.1 9093 /ready)" = 'http://127.0.0.1:9093/ready'
test "$(http_url ::1 9093 /ready)" = 'http://[::1]:9093/ready'
scripts/verify-marshaller.sh
```

结果：通过。完整隔离验收实际证明：

- 真实 Redis success 10 families/11 samples、`target_unavailable` 与恢复；
- 三类永久异常跳过并继续；
- VictoriaMetrics outage 保留 offset 并同进程恢复；显式未提交 record 在重启后恢复；
- 真实第二 group member 接管 partition 且不提交，replacement Marshaller 从最后 committed offset 重取；
- Kafka broker restart 后同进程 group rejoin/readiness 恢复；
- 捕获真实 Envelope 重放保持稳定毫秒点；
- 内部访问边界、invalid-row 稳定和随机容器/network/volume/端口清理全部通过。

## 5. 偏差、限制与后续

- 没有为生产代码增加测试钩子。commit-in-flight 精确 revoke/lost 窗口由确定性单元测试覆盖；真实 acceptance 使用第二 group member 覆盖实际 partition handoff、正式 offset 不推进和 replacement owner 重取，两者组合满足整改合同。
- 独立 Kafka commit failure 沿用既有受控 halt + 外部重启策略，没有在真实 broker 中注入 commit-only failure；其分类和不继续行为由现有定向单元测试覆盖，README 已明确运维语义。
- 未运行 Frontend、Backend、Router、Monitor 或 business 的完整独立验收。原因是本批未修改其产品实现；共享 Bash/Marshaller 集成变化已由 Compose 渲染、脚本门禁和完整 Marshaller acceptance 覆盖，没有观察到扩大回归范围的依据。
- 尚未执行 push、GitHub Actions 或 PR 合入；这些属于后续远程流程，本文不将其记录为已完成。
