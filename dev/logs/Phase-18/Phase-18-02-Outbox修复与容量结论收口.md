# Phase-18-02 Outbox 修复与容量结论收口实施记录

## 1. 实际完成

- 按 Phase-18 总方案读取批次分配，获取 `upstream` 并从实际最新的
  `upstream/main=a4b5cb2a1a75a1eb068d96c3e6359a2bccdffe12` 创建
  `develop/2.0.2`。
- 采纳 Outbox 调度修复：一次 claim 返回满批且整个批次无错误时立即继续 claim；
  空批、部分批、错误和取消路径仍按原 poll interval 或取消语义处理。
- 将 `DispatchOnce` 保持为原有错误返回接口，内部增加批次是否已满的调度信息；
  未改变 owner、lease、fencing、发布幂等和确认后标记完成语义。
- 为测试 fake 增加可控 claim 序列和 claim 观测，新增满批连续调度回归测试。
- 将根版本及仓库已有产品版本元数据同步到 `2.0.2`。
- 未修改 runner、receipt、observer 或容量验收脚本；未执行 150/300 RPS 容量运行。

## 2. 容量结论

历史运行结论保持冻结，结果为 `boundary_found`：

- 150 RPS steady 的 `45,000` 请求和 300 RPS burst 的 `18,000` 请求均完成，
  历史记录无请求错误、超时和 OOM。
- Outbox steady pending 从 `15` 增长到 `31`，超过冻结上限 `25`；恢复结束
  pending 为 `0`。
- 未观测到 event-state 总量行，因此没有证明所有已接受事件闭合。
- 本批不把上述结果改写为通过，也不宣称已证明 150 RPS 稳定容量。

## 3. 实际修改文件

- `.env.example`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`
- `admin-frontend/package.json`
- `admin-frontend/package-lock.json`
- `backend/internal/outbox/dispatcher.go`
- `backend/internal/outbox/dispatcher_test.go`
- `dev/logs/Phase-18/Phase-18-02-Outbox修复与容量结论收口.md`

## 4. 实际执行的命令与结果

批次准备：

```text
git fetch upstream --prune
git switch -c develop/2.0.2 upstream/main
```

结果：获取成功；分支从 `upstream/main=a4b5cb2` 创建并跟踪成功。

实现和版本元数据：

```text
gofmt -w backend/internal/outbox/dispatcher.go backend/internal/outbox/dispatcher_test.go
python3 scripts/ci/sync_version_metadata.py --version 2.0.2
```

结果：格式化通过；`VERSION`、`.env.example`、两个前端 package 文件和两个
lockfile 更新为 `2.0.2`。

直接回归和固定检查：

```text
go -C backend test ./internal/outbox/...
go -C backend test ./...
go -C backend test -count=1 ./internal/outbox/...
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
git diff --check
```

结果：全部通过；uncached Outbox 测试通过，Backend 全量测试通过，版本元数据、
分支治理和差异检查通过。

运行时合同：

```text
scripts/verify-runtime-contracts.sh
```

结果：按方案原样执行，因当前脚本要求显式 `--candidate x.x.x`，返回 exit 2
并打印 usage。

```text
scripts/verify-runtime-contracts.sh --candidate 2.0.2
```

结果：通过。六个 exporter package/race gates、Linux amd64 Compose 启动、四类
健康路径、配置负例、依赖故障矩阵、插件生命周期、断开 drain、SIGTERM/SIGINT
重启、日志合同和受管资源清理全部通过。

运行时构建首次遇到 BuildKit 的 Alpine CDN 连接长时间无响应。为完成同一合同检查，
使用当前可用 CDN 地址对 backend 和 monitor 做了定向 cache prewarm；prewarm
镜像随后已按明确 tag 删除。期间尝试的 legacy builder 和临时 builder 失败均为
构建基础设施错误，未修改仓库文件、未执行全局 Docker cleanup；最终固定合同命令
使用默认 builder 完成并通过。

## 5. 偏差与已知限制

- 方案中的规划基线记录了 `upstream/main=97b8226`，实际执行前获取到的最新
  `upstream/main` 为 `a4b5cb2`，按“从最新 primary remote main 创建”规则使用后者。
- 方案列出的 runtime 合同命令缺少当前脚本必需的 candidate 参数；无参调用的
  失败已保留事实，使用 `--candidate 2.0.2` 完成了实际合同检查，未修改脚本接口。
- Phase 18 容量结果仍为 `boundary_found`。多副本正确性、双 Elasticsearch
  隔离与背压、独立健康、合同单一来源以及 Outbox/event-state 闭合未证明项作为
  后续阶段输入；本批不重跑容量场景。
