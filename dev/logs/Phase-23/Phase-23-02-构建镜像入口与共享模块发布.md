# Phase-23-02：构建镜像入口与共享模块发布

> 实际状态：已完成（本文件记录实测结果）。目标版本 `2.5.2`，分支 `develop/2.5.2`。
> 基线 `origin/main` = `696ea24`（含本批分方案）；实现提交 `b1e1b99`。
> 计划预算：预计 160 分钟 / 上限 180 分钟。实际活跃耗时约 45 分钟（2026-10-09 18:54–19:40，
> 含等待后台镜像构建与 `make check/test` 的时间），未触及上限，无预算类停止。

## 实际完成

- `make build` 成为程序、前端与本地镜像的统一入口：裸 `make build` = `build-programs`
  （遍历 15 个模块的组件 `build`）+ `build-images`（10 个逻辑 Compose 目标）；
  `make build MODULE=<name>` 保持 23-01 的组件语义。新增 `IMAGE_TARGETS` 与 `make package-plugin`。
- 插件打包实现从 `scripts/package-redis-exporter.sh`（63 行）迁到 `monitor/cmd/plugin-package`
  （Go，含 5 个用例的表驱动测试）。归档仍以固定参数调用系统 `tar`/`gzip`，v1 清单用排序键
  map 写出，**输出字节与旧实现完全一致**（7 个钉死摘要全部原样通过）。
- `monitor/internal/plugin/packagemetadata.go` 新增共享元数据函数：`WritePackageMetadata`
  （v2，由 `cmd/plugin-package-metadata` 原样复用，该命令文档化行为不变）与
  `LegacyManifest`（v1，冻结字节）。
- `componentmetrics` 以子目录 tag 发布为真实版本：`componentmetrics/v0.1.0`（annotated
  `c4c98cf` → 提交 `696ea24`）。10 个消费模块改为 `v0.1.0` 并删除本地 `replace`；
  3 个原先无需 `go.sum` 的模块（elasticsearch / rabbitmq / victoriametrics）新增 2 行 `go.sum`。
- 镜像构建不再依赖工作树内的 `componentmetrics/` 副本：删除 13 处 `COPY componentmetrics/`
  （observability 10、acceptance 2、backend 1），17+2 条打包 `RUN` 改为 `go run ./cmd/plugin-package`。
- `scripts/package-redis-exporter.sh` 保留为 14 行薄转发入口（构建缓存二进制后 `exec`，退出码
  语义不变），供 8 处既有调用与 `monitor_input_digest` 使用；删除归属 23-07。
- `deploy/plugins/README.md`、`exporters/{redis,mysql,rabbitmq,victoriametrics}/README.md`、
  `monitor/README.md` 的打包命令改为 `make package-plugin ...`，7 个历史摘要表述保持不变。

## 实际命令与结果

- 发布解析：`curl proxy.golang.org/.../@v/v0.1.0.info` → HTTP 200，
  `Origin.Subdir=componentmetrics`、`Hash=696ea24…`；`go list -m …@v0.1.0` → `v0.1.0`。
  `goproxy.cn` 同路径 HTTP 200（单次约 8.5 s，首测偶发超时后复测正常）。
- 参考回执（旧实现，固定夹具 `sha256=a622e287…`）：v1 redis `f8a83143…`、v2 redis `cf08b9c9…`、
  v2 mysql `6cb6f2aa…`。新工具与 `make package-plugin` 三条路径复现同样摘要；v1 归档提交为
  `monitor/cmd/plugin-package/testdata/legacy-v1-reference.tar.gz`，摘要与 v1 清单字节写入用例常量。
- `cd monitor && go test -count=1 ./cmd/plugin-package/... ./internal/plugin/...`：全部通过。
  非法 SemVer、未知 source、contract 1 非 redis、未知 contract、非法 arch、未知参数、位置参数、
  缺失/不可执行 `--binary` 均按 usage 失败；经转发入口实测进程退出码为 2。
- `make build-images DRY_RUN=1` 与 `python3 scripts/ci/compose_build_cache.py --print`：10 个逻辑目标
  的定义（context/dockerfile/args/tags/target/labels）逐字段相同，差异仅为 CI 助手新增的
  `platforms`/`output`/`cache-from`/`cache-to`；`acceptance` 的 `UPDATE_VERSION=2.5.3`（patch+1）、
  `REVISION=b1e1b99…` 正确注入。两条路径的原始 `--print` 都额外列出 `backend-2`/`platform-api`/
  `router-2`（复用同一镜像的附加服务），CI 助手按其 10 个 `TARGETS` 过滤。
- 失败传播：`VERSION=2.5` → 退出 2 并给出诊断；`IMAGES=does-not-exist` → 非零退出（compose 报错）。
- `docker build --target official-packages`（57 s）：`redis-1.10.6.tar.gz` 内联摘要
  `b992b0df…` OK，`phase14-1.11.5.sha256` 六项（elasticsearch/kafka/mysql/rabbitmq/redis/
  victoriametrics）全部 OK。`--target acceptance-packages`（42 s）成功。
- `docker build --target monitor`（12 s）成功；镜像内嵌 6 个当前包 + 6 个 1.11.5 保留包 +
  `redis-1.10.6.tar.gz`，说明新版打包产物可正常生成 release catalog。
- `make build-images IMAGES=frontend`（37 s）→ `gopulse/frontend:2.5.2`
  （label `org.opencontainers.image.version=2.5.2`、`revision=b1e1b99…`）。
- `docker build -f deploy/docker/acceptance.Dockerfile --target exporter-package`（35 s）：两个验收包
  由新工具产出，`entrypoint_sha256=3cdc54ff…` 与阶段内 `/out/gopulse-redis-exporter` 实测摘要一致。
- B8 依赖切换（每模块 `git archive HEAD:<module>` 到临时目录，无兄弟 `componentmetrics/`）：
  10 个模块 `go build ./...` + `go test -count=1 ./...` 全部退出 0，`go list -m` 均为 `v0.1.0`，
  `go.sum` 各 2 行。
- B9 全模块入口：`make check MODULE=<15>`、`make test MODULE=<15>` 全部退出 0（含 componentmetrics、
  lifecycle、loadtest 与两个前端）。
- 治理：`validate_versions.py` 通过；`validate_branch.py --branch develop/2.5.2 --base-ref origin/main
  --mode development` 通过；`quality_scope.py --base-ref origin/main` 选中全部模块检查 + `compose`
  + `tools`（理由逐文件列出）；`python3 -m unittest discover -s scripts/ci -p 'test_*.py'` 98 项通过；
  `bash -n scripts/package-redis-exporter.sh` 通过；本批未改动 YAML。
- 文档一致性：`make package-plugin SOURCE={mysql,rabbitmq} VERSION=1.11.2`、
  `SOURCE=victoriametrics VERSION=1.11.4`、`CONTRACT_VERSION=1 VERSION=1.10.6` 四条 README 命令
  实际执行成功（省略 `BINARY=` 的自编译路径生效）；`make help` 已列出新目标与镜像目标清单。

## 实际变更文件

见提交 `b1e1b99`（34 个文件）：

- 新增：`monitor/cmd/plugin-package/{main.go,main_test.go,testdata/*}`（4）、
  `monitor/internal/plugin/packagemetadata{,_test}.go`（2）。
- 删除：`monitor/cmd/plugin-package-metadata/main_test.go`（用例迁入 `internal/plugin`）。
- 修改（模块依赖）：`{backend,marshaller,monitor,router}/go.{mod,sum}`、
  `exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}/go.mod`，
  其中 elasticsearch/rabbitmq/victoriametrics 新增 `go.sum`。
- 修改（入口与镜像）：`Makefile`、`monitor/Makefile`、`scripts/package-redis-exporter.sh`、
  `deploy/docker/{observability,acceptance,backend}.Dockerfile`。
- 修改（文档）：`deploy/plugins/README.md`、`exporters/{redis,mysql,rabbitmq,victoriametrics}/README.md`、
  `monitor/README.md`。
- 完成后提交：`VERSION`、`.env.example`、`{frontend,admin-frontend}/package{,-lock}.json`（同步 2.5.2）
  与本日志。

## 偏差与修正

1. **默认值位置**：§3.1 计划由薄转发入口补齐 `--version`/`--output` 默认值；实际把
   `--version`（读 `<repoRoot>/VERSION`）、`--output`（`.run/packages/gopulse-<source>-exporter-<version>-linux-<arch>.tar.gz`）、
   `--arch`（`go env GOARCH`）默认值实现在被测试的 Go 工具内，转发入口只做 14 行参数与工作目录准备。
   原因：默认值属于公共 CLI 合同，放在 `go test` 覆盖范围内比放在 shell 更可靠。
2. **转发入口用预编译二进制而非 `go run`**：`go run` 会把子进程的退出码 2 折叠成 1，破坏
   `verify-monitor.sh:143` 依赖的“用法错误非零/可区分”语义。改为构建到 `.run/bin/plugin-package`
   （先临时名再 `mv`，避免并发执行半成品）后 `exec`，实测用法错误退出码为 2。
3. **新增 `--repo-root` 参数**（计划未列）：使默认值与自编译路径在任意工作目录可用；
   Dockerfile 调用不传该参数，因此镜像路径不依赖工作目录。
4. **`COPY VERSION` / `COPY scripts/…` 与 `ARG GOPROXY`**：observability 的 `exporter-package`
   阶段删除不再需要的 `VERSION` 与脚本拷贝，改为声明 `ARG GOPROXY` + `ENV GOPROXY` 并设
   `WORKDIR /src/monitor`（新工具需要从发布版本拉取 `componentmetrics`，且 17 条 `RUN` 保持单行）。
   acceptance 的 `exporter-package` 阶段同样删除两处无用拷贝并声明 `ENV GOPROXY`；
   `apk add` 从 `bash python3 tar gzip` 收敛为 `tar gzip`。
5. **未执行 `go mod tidy`**：按 §3.4 的允许项选择保留 `go mod edit` 的最小 diff；核对 CI 与
   Makefile 均无 `go mod tidy` 检查。
6. **测试文件移动**：`cmd/plugin-package-metadata` 的 v2 元数据用例随实现迁到
   `internal/plugin/packagemetadata_test.go`，并新增 v1 清单字节冻结用例与 CLI 用例。
7. **文档命令**：仅替换打包命令为 `make package-plugin …`；同代码块中的 `go test` 命令保持原样，
   不扩大文档改动范围。

## 已知限制与后续项

- `quality_scope.py:_local_replace_consumers` 对 10 个模块变为空集：`componentmetrics/**` 改动
  不再触发消费模块检查。这与“消费已发布 tag”一致，但意味着**改库必须重新打 tag**；后续可增加
  “库变更需新 tag”的检查（本批只登记，不实现）。
- 本地跨模块联调需个人未跟踪 `go.work`（`.gitignore:37` 已忽略）；本批未签入。
- `scripts/package-redis-exporter.sh` 转发入口与 `monitor/cmd/plugin-package-metadata` 的调用者
  退役/清理归 23-07；后者在本批后已无仓库内调用者，保留原因是其文档化 CLI 仍被
  README/历史验收引用。
- `compose_build_cache.py` 的原生承接与 CI 镜像步骤切换归 23-07，其分方案需重新登记预算。
- `make monitor-image` 本批未改（`quality-gates.yml:514` 与 `make dev-observe` 仍调用）；其职责
  并入 `dev-observe`/`build` 与删除归 23-03/23-04。
- v2 清单 `name` 字段沿用旧实现的小写 source（如 `GoPulse mysql Exporter`）；本批以字节兼容
  为先，未修正该文案。
- `docker compose build <service>` 的打印定义包含 3 个复用镜像的附加服务；CI 助手按 10 个
  `TARGETS` 过滤，两者一致，仅记录为信息项。
- `git tag componentmetrics/v0.1.0` 不可移动：如后续需要改库，必须发布新版本而不是重打 tag。
