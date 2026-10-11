# Phase-23-01：组件级 Makefile 与统一测试检查入口

> 实际状态：已完成。目标版本 `2.5.1`，分支 `develop/2.5.1`。基线 `origin/main` = `f244028`。

## 实际完成

- 新增 15 个组件 `Makefile`，合计 285 行。13 个 Go 模块提供 `test`（`go test -count=1 ./...`）、
  `race`（`-race`）、`check`（`gofmt -l` + `go vet`）、`build`（`go build ./...`）；2 个前端提供
  `test` / `check`（`typecheck`）/ `build`，并以 `node_modules` 作为目录目标前置依赖，
  仅在目录缺失时执行 `npm ci --no-audit --no-fund`。
- 改写根 `Makefile`（57 行）：`GO_MODULES` / `NPM_MODULES` 成为模块清单唯一来源；
  `test` / `race` / `check` / `build` 经 `$(MAKE) --no-print-directory -C $(MODULE)` 调度；
  新增 `check-all` 与模块清单输出。既有 `dev` / `dev-observe` / `integration` / `e2e` /
  `stop` / `monitor-image` 六个目标未改。
- 删除 `scripts/ci/local_development.py` 的纯转发层：`MODULES` 表、`run_test`、`test` 子命令
  及 `--module` 参数，文件由 1187 行降为 1156 行（−31）。`ensure_npm_dependencies` 保留，
  因为 `start_lifecycle`（674、679）与 `run_e2e`（836、841）仍调用它。
- `scripts/ci/quality_scope.py` 将 `lifecycle`、`loadtest` 加入 `MODULES` 与 `GO_MODULE_ROOTS`，
  并新增 `STANDALONE_MODULES` 提前返回，使这两个模块不触发 `integration_observe` 或
  `e2e_observe`。实测 `lifecycle/**` 只触发 `lifecycle`，`backend/**`、`monitor/**`、
  `scripts/**` 的既有行为不变。
- `.github/workflows/quality-gates.yml`：11 个 Go job 改为 `make check` / `make test` /
  `make race`，2 个前端 job 改为 `make test` / `make build`，新增 `lifecycle` 与 `loadtest`
  两个 job（20 → 22）。7 个 job 的 `defaults.run.working-directory` 移除，命令统一在仓库根执行。
- `lifecycle`（6 个测试文件 / 692 行）与 `loadtest`（8 个 / 785 行）共 1477 行测试首次接入
  统一入口与 CI。两者均为 hermetic：`lifecycle` 用写入临时目录的伪造 `docker` 脚本，
  `loadtest` 用 `httptest` 回环，无需 Docker 或外部服务。
- `VERSION` 与 6 处产品版本元数据同步到 `2.5.1`。

## 实际命令与结果

- `make test MODULE=<15 个模块逐一>`：全部通过。Go 侧包结论数 backend 35、marshaller 11、
  monitor 9、router 4、loadtest 4、exporters/redis 4、lifecycle 3、exporters/kafka 2、
  exporters/victoriametrics 2、componentmetrics / exporters/{elasticsearch,mysql,rabbitmq} 各 1；
  两个前端以 vitest 退出码 0 通过（frontend 18 文件 / 72 用例）。
- `make check-all`：通过，15 个模块依次执行 `gofmt` + `go vet` 或 `typecheck`，退出码 0。
- 变异 1（`check` 的 gofmt 分支）：在 `componentmetrics/backend.go` 追加未格式化函数后
  `make check MODULE=componentmetrics` 退出码 2，stdout 列出 `backend.go`；还原后退出码 0。
- 变异 2（`check` 的 vet 分支）：新增 gofmt 干净但 vet 失败的 `componentmetrics/vetprobe.go` 后
  退出码 2，诊断为 `vetprobe.go:5:31: fmt.Printf format %d has arg "not an int" of wrong type string`；
  删除该文件后目录恢复干净。
- `make test`（缺 `MODULE`）：退出码 2，输出 `[gopulse] ERROR: MODULE is required, for example
  make test MODULE=backend`。
- `make check MODULE=frontend`：通过，`vue-tsc --noEmit` + `tsc --noEmit` 无输出。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，98 个测试；静态计数与收集数
  一致（逐文件计数合计 98）。
- `python3 scripts/ci/validate_versions.py`：通过，`Version metadata matches root VERSION.`
- `python3 scripts/ci/validate_branch.py --branch develop/2.5.1 --base-ref origin/main --mode development`：
  通过，`Branch governance passed for develop/2.5.1.`
- LF 检查（`git ls-files --eol` 覆盖 `scripts/*.sh` 与 5 组 Go glob）：通过。
- `bash -n`（17 个脚本，与 CI 同一清单）：通过。
- 9 个 `--self-test`（verify-compose / business / exporter / monitor / router / marshaller /
  logs / events / observability-ui）：全部通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config`：渲染通过；
  `gopulse/{admin-frontend,frontend,backend,business-worker,search-indexer,router,marshaller,monitor}:2.5.1`
  八个镜像标签齐全；`host_ip: 127.0.0.1` 1 处；`internal: true` 2 处。
- `python3 /tmp/rewrite_workflow.py`（一次性转换脚本）：改写 11 个 Go job、2 个 npm job，
  插入 2 个新 job。
- 转换后的结构化前后对比：job 数 20 → 22；无丢失 job；**所有 job 的 `if` 条件 0 变化**；
  13 个模块 job 的检查项一一守恒（gofmt→check、test→test、vet→check、race→race、
  npm test→test、npm run build→build）。
- 残留字面命令核对：`go test` 0、`gofmt` 0、`go vet` 0、`npm test` 0、`npm run build` 0。
  仅存 2 处 `npm ci --no-audit --no-fund`，位于 `e2e-business` 与 `e2e-observe`（Playwright
  浏览器安装，属 23-05 范围，不在本批）。

## 实际变更文件

新增（15）：

| 文件 | 行数 |
| --- | --- |
| `backend/Makefile`、`componentmetrics/Makefile`、`router/Makefile`、`marshaller/Makefile`、`monitor/Makefile`、`lifecycle/Makefile`、`loadtest/Makefile` | 各 19 |
| `exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}/Makefile` | 各 19 |
| `frontend/Makefile`、`admin-frontend/Makefile` | 各 19 |

修改（11）：`Makefile`、`VERSION`、`.env.example`、`.github/workflows/quality-gates.yml`、
`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、
`admin-frontend/package-lock.json`、`scripts/ci/local_development.py`、
`scripts/ci/quality_scope.py`、`scripts/ci/test_local_development.py`，以及本批分方案
`dev/imple/Phase-23/Phase-23-01-统一测试与检查入口.md`（补验证映射并修正与实现不符的门禁）。

合计 27 个文件，451 插入 / 159 删除。

## 偏差与修正

1. **`race` 改为独立目标**。方案原文写 `make test MODULE=x RACE=1`，实现为独立的
   `make race MODULE=x`。目标与参数分离后组件 Makefile 更简单，前端也不必声明不支持的变量。
   已同步修正方案 §2 的门禁措辞。
2. **CI job 数量由 20 增至 22**，而非方案原文的"数量与改动前一致"。原文同时要求把
   `lifecycle`/`loadtest` 纳入 CI，两者不可能同时成立；实际保留全部原 job 并新增 2 个。
   已同步修正方案。
3. **新增 `scripts/ci/quality_scope.py` 到允许修改范围**。方案未列该文件，但新增 CI job 必须
   同步扩展其 `checks`，否则新 job 的 `if` 永远为假。同时发现并规避了一个真实陷阱：该文件对
   所有非 `backend` 模块都会置 `integration_observe`，直接加入两个新模块会让改安装器或压测
   工具误触发可观测集成矩阵。
4. **前端 job 未加 `make check`**。`frontend/package.json` 的 `build` 脚本是
   `npm run typecheck && vite build`，再加 `check` 会在同一 job 内重复执行同一检查，
   违反"同一检查保持一个明确执行位置"。类型检查仍由 `make build` 覆盖。
5. **根 `build` 只做组件级构建**。CI 前端 job 原本执行 `npm run build`，故本批提供
   `make build MODULE=` 调度；产品级构建、镜像与 Bundle 仍属 23-02 / 23-06。
6. **`VERSION` 在实现中途即升至 `2.5.1`**，而非留到提交时。原因是
   `scripts/ci/test_verify_business.py::test_dev_no_build_rejects_same_version_stale_revision_before_up`
   在真实仓库运行 `scripts/dev.sh`，而 `dev.sh:128` 要求 `develop/*` 分支等于
   `develop/$VERSION`；分支为 `develop/2.5.1` 而 `VERSION=2.4.5` 时该用例先命中分支错误。
   这与 `dev/logs/Phase-22/Phase-22-05-原生验收与脚本精简.md` 记录的同一现象一致
   （该处记为"推送候选仍为 `VERSION=2.4.4`，使 stale-revision 自测先命中分支版本错误"）。

## 已知限制与后续项

- `node_modules` 目录目标只在目录不存在时安装，与旧 `ensure_npm_dependencies` 的
  `is_dir()` 判断行为一致，不检测 lockfile 变新。可考虑改为 `node_modules: package-lock.json`
  以获得更安全的失效判定；本批为保持与旧入口等价未改，未列入本批范围。
- 前端 `check`（`typecheck`）不被 CI 调用，仅由 `make check-all` 与本地使用。
- 本批抽测了 `make race` 的 `componentmetrics` 与 `loadtest` 两个模块，未在本地对 13 个
  Go 模块全量执行 `make race`（CI 侧会对受影响模块全量执行）。
- 观察项，不属本批范围：`dev/logs/Phase-22/Phase-22-05-原生验收与脚本精简.md` 记录
  `unittest discover` 为"201 个测试"，而本次实测静态计数与收集数均为 98。按"已完成日志保留
  原有路径与历史上下文、不得改写回执"的规则，未修改该历史日志，仅在此记录差异。
- 本批未删除 `scripts/` 下任何内容；`make test` 不再经过 Python，但
  `local_development.py` 仍承载 `dev` / `dev-observe` / `stop` / `integration` / `e2e` /
  `monitor-image`，其替代属 23-03 至 23-06。
