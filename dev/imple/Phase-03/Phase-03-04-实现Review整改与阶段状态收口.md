# Phase-03-04：实现 Review 整改与阶段状态收口

## 1. 批次目标

本批次执行 `dev/review/2026-09-03-Phase-3实现Review报告.md` 的整改结论，关闭 P2-01～P2-03 与 P3-01。目标版本为 `0.4.4`，唯一权威开发分支为 `develop/0.4.4`。

本批次只处理搜索分页快照一致性、游标完整性、Frontend 分页重试和 Phase 3 状态治理，不增加搜索过滤、高亮、推荐、帖子编辑/删除同步或生产 Elasticsearch 高可用能力。

## 2. 实施范围

1. 为相关度排序分页引入 Elasticsearch Point in Time（PIT）：第一页针对当前物理 generation 打开快照，后续页使用同一 PIT、完整 `search_after` tuple 和最新 PIT ID。
2. PIT `keep_alive` 固定为 2 分钟；游标绑定 query digest、物理 generation、PIT ID、客户端过期时间与 `_score / created_at / post_id / _shard_doc`，并使用服务端密钥派生的 HMAC 防止篡改。
3. PIT 过期、重建 generation 变化或游标签名无效统一返回脱敏 `validation_failed`，客户端从第一页重新搜索；普通 Elasticsearch 故障继续返回 `search_unavailable`。
4. 搜索没有下一页、发生 hydration 失败或搜索失败时尽力关闭 PIT；任何日志和公共错误不得输出 PIT ID。
5. Frontend 记录最近一次失败请求。临时分页失败重试原 cursor 并追加结果；快照游标失效时清空旧结果，明确提示并受控重启第一页。
6. 更新 Phase 3 总实施方案、README、Phase-03-03 后续状态和 Review 整改状态；同步根与 Frontend 版本为 `0.4.4`。

## 3. 直接影响文件

- `backend/internal/search/elasticsearch.go`
- `backend/internal/search/service.go`
- `backend/internal/search/service_test.go`
- `backend/internal/search/pit_integration_test.go`
- `backend/cmd/server/main.go`
- `frontend/src/views/SearchView.vue`
- `frontend/src/views/SearchView.test.ts`
- Phase 3 方案、记录、Review、README 与版本元数据

## 4. 验收标准

- 第一页之后向同一物理索引增量写入并 refresh 时，后续页仍与第一页的 PIT 快照一致，不出现新文档混入、旧命中跳过或重复。
- cursor 无法跨 query、跨 generation、跨 PIT 使用；修改 score、时间、ID、`_shard_doc`、PIT 或过期时间而不具备服务端签名时必须被拒绝。
- PIT 已过期或 generation 已切换时，Search API 返回 `validation_failed`，且响应不泄露 PIT ID、物理索引名、Elasticsearch URL 或 DSL。
- 普通搜索依赖故障仍映射为 `503 search_unavailable`；MySQL hydration 和现有搜索事实边界不变。
- Frontend 加载更多临时失败后点击重试仍发送原 cursor、保留已有帖子并追加恢复结果；失效 cursor 明确提示结果已更新，并只从第一页重新启动。
- `develop/0.4.4` 在 Phase 3 总实施方案中有且仅有一个 `Phase-03-04 / 0.4.4` 分配，根和 Frontend 版本均为 `0.4.4`。

## 5. 固定验证命令

```text
(cd backend && go test ./...)
(cd backend && go vet ./...)
(cd backend && go test -race ./...)
(cd backend && INTEGRATION_TESTS=1 ... go test -count=1 -tags=integration ./internal/search)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/0.4.4 --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
git diff --check
```

真实 Elasticsearch PIT 场景由 tagged integration 测试验证。完整 `scripts/verify-business.sh` 已在 Phase-03-03 与实现 Review 中成功，本批次没有改变生命周期、RabbitMQ 拓扑、Outbox、重建命令或浏览器主闭环，因此除非定向门禁发现跨域回归，不重复完整矩阵。

## 6. 完成条件

上述验收标准与固定门禁在最终 diff 上通过，实施记录如实写入 `dev/logs/Phase-03/Phase-03-04-实现Review整改与阶段状态收口.md`，版本更新为 `0.4.4` 并提交到 `develop/0.4.4`。该分支推送后，远程 Branch governance、Backend、Frontend、Scripts and Compose、Integration 与自动 PR/合并流程全部成功，才可关闭 Phase 3 Review 并开始独立的 `develop/1.0.0` release-only 动作。
