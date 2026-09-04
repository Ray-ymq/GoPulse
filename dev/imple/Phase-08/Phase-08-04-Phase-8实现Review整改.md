# Phase-08-04：Phase 8 实现 Review 整改

## 1. 目标与范围

以 `dev/review/2026-09-04-Phase-8实现Review报告.md` 为验收输入，在 `develop/1.5.4` 上关闭 P1-01、P2-01、P2-02 和 P3-01。仅修改 Marshaller ownership/commit 分类、loopback 地址构造、无效配置合同、对应生命周期/文档和版本治理；不增加 Dashboard、查询 API、告警、额外 Topic 或 Phase 9+ 能力。

## 2. 实施内容

1. Committer 返回错误后先复查当前 lease；revoke/lost 取消归类为 `ErrOwnershipLost`，lease 仍有效的错误保持 `ErrCommitFailed`。
2. 增加 revoke 与 lost 的 commit-in-flight 定向测试，并证明重新 assignment 后可继续提交；保留独立 commit failure 回归。
3. HTTP listener 使用标准 host/port 拼接；Bash 日常 readiness URL 对 IPv6 loopback加 bracket；覆盖 IPv4/IPv6 地址测试。
4. 删除没有运行效果的 `MARSHALLER_KAFKA_POLL_TIMEOUT` 及所有配置、示例和生命周期引用，明确 poll 由根 context 取消。
5. 更新总方案权威分配、实施记录和根/Frontend 版本到 `1.5.4`。

## 3. 验收标准

- revoke 与 lost 在 commit-in-flight 时均返回 ownership-lost，不产生成功 commit；新 lease 能处理并提交同一 record。
- ownership 有效的 Committer 错误仍返回 commit-failed。
- `127.0.0.1:9093` 与 `[::1]:9093` listener 地址均正确，日常 URL helper 对两者输出合法 URL。
- 仓库除历史 Review 报告外不再存在 `KafkaPollTimeout` 或 `MARSHALLER_KAFKA_POLL_TIMEOUT` 运行合同引用。
- 完整 Marshaller acceptance 覆盖真实 group 恢复、offset 安全和现有故障矩阵，无跨组件回归。
- `VERSION`、Frontend npm 元数据和 `develop/1.5.4` 权威分配一致。

## 4. 固定验证

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.4 --base-ref origin/main
scripts/verify-marshaller.sh
git diff --check
```

## 5. 完成条件

上述验收全部通过、实施记录只记载实际结果、版本更新到 `1.5.4` 且无阻断 finding 时完成。若完整验收因外部环境无法执行，必须明确记录，不能宣称 Review 整改通过。
