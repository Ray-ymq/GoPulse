# Phase 7-03：Review 整改与阶段再验收实施方案

> 权威目标版本与开发分支以 `Phase-07-总实施方案.md` 第 3.2 节为准：本批对应 `1.4.3` / `develop/1.4.3`。
>
> 当前状态：本地实现与固定验收完成，待推送、远程门禁和 Pull Request 合入。

## 1. 批次目标

关闭 `dev/review/2026-09-04-Phase-7实现Review报告.md` 记录的 1 项 P1 和 5 项 P2，使 Phase 7 的 Envelope v1 边界、Kafka Producer 有界模型、真实拒绝验收、资源归属和版本/分支治理与权威合同一致。本批不新增 Topic、消息类型、Marshaller、存储或其他 Phase 8 能力。

## 2. 实施范围

1. `schema_version` 必须是 JSON integer `1`；字符串、浮点、布尔或 null 均不得进入 Producer。
2. 移除容量 1 的全局串行 slot；由 franz-go 的 `MaxBufferedRecords` / `MaxBufferedBytes` 提供真实非阻塞准入，缓冲满立即失败。HTTP 调用取消只结束该调用等待，不执行全客户端 abort；readiness 可与 publish 并行，shutdown 失败时才允许全局 abort。
3. 为 Producer 增加代表性并发、buffer-full、单调用取消、readiness 和有界关闭测试。
4. `verify-router.sh --self-test` 不访问 Docker，负向覆盖 token、PID/start ticks/executable/cwd/marker、project、container、volume、五端口冲突、固定 Topic、Compose 摘要与清理上下文，并证明不会停止无关进程。
5. 五个隔离端口在同一 Python 进程持有 socket 后统一返回，并执行两两唯一检查。
6. 在同一 Kafka 基线 offset 上执行无/错 token、query token、普通用户/admin Cookie-only、Content-Type、Content-Encoding、超限、重复 key、尾随 JSON、Idempotency-Key 缺失/重复/不匹配、未知字段、字符串 schema 和 unsupported schema/type/source 的 HTTP/code 矩阵；整组结束后 offset 必须不增长。
7. 同步 Phase-07-01 远程完成事实、Phase 7 权威批次分配、README、实施记录和 `1.4.3` 版本元数据。

## 3. 固定验证与必要回归

最终 diff 上执行一次：

```bash
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd exporters/redis && go test -count=1 ./...)
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.4.3 --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh
tmp_bin=$(mktemp -d); for tool in env bash dirname readlink mktemp sha256sum awk python3 go setsid sleep rm; do ln -s "$(command -v "$tool")" "$tmp_bin/$tool"; done; PATH="$tmp_bin" scripts/verify-router.sh --self-test
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-router.sh
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
```

`verify-business.sh` 完整回归仅在 Router/Kafka 验收或共享 Compose 变更出现具体业务回归证据时扩大执行；本批不修改 Backend、业务数据库、RabbitMQ 或 Elasticsearch 合同。

## 4. 验收标准

- 字符串和小数 `schema_version` 的 unit/HTTP 请求失败且 Producer/Kafka 未被调用或写入；合法 integer `1` 保持原始 bytes。
- 两个健康 publish 可同时进入 franz-go；records/bytes 满立即返回错误；一个 HTTP context 取消不 abort 或污染其他 record；readiness 不被 publish 阻塞；关闭在传入 context 内完成或安全中止。
- 无 Docker PATH 下 Router self-test 返回 0，列出的资源安全负向类别均被拒绝，测试进程保持存活直到显式清理。
- 五端口集合大小固定为 5；相邻和非相邻重复组合均被 self-test 拒绝。
- 18 类真实非法请求的 HTTP status/error code 与合同一致，整组前后 Kafka end offset 相同，日志不输出 token、Cookie 或原始 payload。
- 正常 direct record、真实 Monitor success/target_unavailable、Kafka outage/recovery、PID 不变和强归属清理继续通过。
- 分支治理只匹配本批一个权威分配；根与 Frontend 版本均为 `1.4.3`；对应开发记录只记录实际执行结果。

## 5. 明确完成条件

第 3 节固定门禁和第 4 节验收标准全部通过、无阻断失败，完成对应开发记录并提交 `develop/1.4.3` 后，本地整改批次完成。只有后续远程门禁成功且 Pull Request 合入 `main` 后，才更新文档为远程完成状态。
