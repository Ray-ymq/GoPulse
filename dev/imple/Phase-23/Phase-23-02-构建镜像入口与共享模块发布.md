# Phase-23-02：构建镜像入口与共享模块发布

> 状态：初稿 2026-10-09。目标版本 `2.5.2`，分支 `develop/2.5.2`。
> 进入条件：Phase-23-01 完成并进入主远端 main（`VERSION=2.5.1`，实测 commit `7e23cc9`）。
> 用户已确认：本批**保持单批不拆分**，累计上限 180 分钟；插件打包承接采用 Go 工具 + `go test`
> （见 §4 预算与 §8 决策记录）。总方案 23-02 的三项内容全部在本批内完成。

## 1. 交付与允许文件

**唯一主要交付结果**：`make build` 成为程序、前端与本地镜像的统一构建入口；支撑镜像构建的
两条硬依赖同时改为可持续形式——插件打包由 `monitor/cmd/plugin-package` 原生承接且输出字节
不变，`componentmetrics` 由 `v0.0.0 + replace` 的本地共享目录改为已发布的真实版本依赖。

允许新增：

| 文件 | 内容 |
| --- | --- |
| `monitor/cmd/plugin-package/main.go` | 打包 CLI：参数校验、v1/v2 元数据、包内布局与权限、固定参数调用系统 `tar`/`gzip`、原子替换输出 |
| `monitor/cmd/plugin-package/main_test.go` | 表驱动用例：参数与失败退出、v1 清单字节、归档字节与参考回执一致 |
| `monitor/cmd/plugin-package/testdata/plugin-package-fixture` | 可执行夹具（文本即可，mode 0755），用于字节合同用例 |
| `monitor/cmd/plugin-package/testdata/legacy-v1-reference.tar.gz` | 旧实现产出的 v1 参考归档（≤1 KB），使等价证据在本批删除旧实现后仍可复验；期望摘要写在用例常量里 |

允许修改：

| 文件 | 改动 |
| --- | --- |
| `Makefile`（根） | 新增 `IMAGE_TARGETS`；`build` 拆为 `build-programs`（遍历 `MODULES`）+ `build-images`；裸 `build` 表示"全部程序 + 前端 + 本地镜像"，`build MODULE=<name>` 语义不变；新增 `package-plugin`；更新 `help` |
| `monitor/Makefile` | 新增 `package-plugin`，调用 `go run ./cmd/plugin-package`（组件自己决定参数与产物） |
| `monitor/internal/plugin/metadata.go`（新） | 由 `cmd/plugin-package-metadata` 提取的 v2 元数据写入函数，两个命令共用；同时提供 v1 排序键写出函数 |
| `monitor/cmd/plugin-package-metadata/main.go` | 改为调用上述共享函数，**对外行为与输出字节不变** |
| `deploy/docker/observability.Dockerfile` | 17 条 `RUN`（共 22 次调用，`:161` 循环按 6 个 source 展开）改调 `plugin-package`；删除 `COPY scripts/package-redis-exporter.sh`；删除 10 处已无用的 `COPY componentmetrics/`；`exporter-package` 阶段 `apk add` 去掉不再需要的 `bash python3` |
| `deploy/docker/acceptance.Dockerfile` | 2 条 `RUN` 改调 `plugin-package`；删除脚本 `COPY` 与 2 处 `COPY componentmetrics/`；同步 `apk add` |
| `deploy/docker/backend.Dockerfile` | 删除 1 处已无用的 `COPY componentmetrics/` |
| `go.mod` + `go.sum`（10 个消费模块） | `backend`、`marshaller`、`monitor`、`router`、`exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}`：改为 `componentmetrics v0.1.0`，删除 `replace` |
| `scripts/package-redis-exporter.sh` | 由 63 行实现改为薄转发入口（参数与环境准备），保留既有 CLI；删除归 23-07（见 §3.3） |
| `deploy/plugins/README.md`、`exporters/{redis,mysql,rabbitmq,victoriametrics}/README.md` | 打包命令改为 `make package-plugin ...`；保留既有钉死摘要表述 |
| `VERSION`、`.env.example`、`frontend/package{,-lock}.json`、`admin-frontend/package{,-lock}.json` | 同步到 `2.5.2`（`sync_version_metadata.py`） |
| `dev/imple/Phase-23/Phase-23-02-*.md`、`dev/logs/Phase-23/Phase-23-02-*.md` | 完成后的实测记录与同名日志（`validate_branch.py --mode completion` 要求） |

不属于本批：`make deps` / `dev` / `dev-observe` / `stop`（23-03）、`integration` / `e2e`（23-04、05）、
`make package` 的 Bundle/manifest/promote 与 `lifecycle` 安装（23-06）、`scripts/` 其余内容及本批
转发入口的删除（23-07）、`compose_build_cache.py` 的原生承接与 CI 镜像步骤切换（见 §3.4）。

**不改变**：`VERSION` 单一来源与发布模型（`componentmetrics` 是库，不是部署组件，不进入发布
清单）；目录结构；不签入 `go.work`（`.gitignore:37` 已忽略 `go.work`/`go.work.sum`，本地跨模块
联调仍可用个人未跟踪工作区）；`deploy/plugins/` 历史输入与 `phase16-acceptance.yaml` 不改。

**实测事实与硬约束**（本批开工前已核实）：

| 事实 | 证据 |
| --- | --- |
| `componentmetrics` 有 **10** 个消费模块，不是总方案写的 9 个 | `grep -l componentmetrics */go.mod exporters/*/go.mod` = 10（不含自身）；10 个模块的 Go 源码均实际 import |
| 仓库 0 个 git tag；模块路径公开可解析 | `git tag` 空；`proxy.golang.org` 与 `goproxy.cn` 对 `@<sha>` 均返回 200，`Origin.Subdir=componentmetrics` |
| 打包输出被 **7 个钉死摘要**约束 | `observability.Dockerfile:155`（`redis-1.10.6.tar.gz` = `b992b0df…`）；`:163` ← `deploy/plugins/phase14-1.11.5.sha256`（6 项）；`deploy/plugins/README.md:11` |
| 纯 Go `archive/tar` + `compress/gzip` **不能**复现这些字节 | 实测：Go 4096 B / `gzip.BestCompression` 183 B；GNU `tar --format=ustar` 10240 B / `gzip -n -9` 186 B，摘要不同 |
| Go 可按排序键复现 v1 清单字节 | 实测：`map[string]any` + `Encoder.SetEscapeHTML(false)` + 结尾换行与 Python `json.dump(separators=(',',':'), sort_keys=True)` 逐字节相同（`schema_version` 必须是整数 `1`） |
| 打包 CLI 另有 8 处活调用 | `scripts/verify-monitor.sh:143,198,221,228,251`、`scripts/verify-events.sh:150,151`、`scripts/verify-router.sh:435`；另 `scripts/ci/local_development.py:302` 把它列入 Monitor 镜像输入摘要 |
| Dockerfile 调用面 | 2 个 Dockerfile、19 条 `RUN`、24 次调用（`observability.Dockerfile:161` 循环按 6 个 source 展开）；`COPY componentmetrics/` 共 13 处（observability 10、acceptance 2、backend 1） |
| 本机构建面可用 | `docker 29.7.2` / `compose v5.5.0`；`docker compose … build --print` 对 10 个逻辑目标返回 13 个 bake target，且正确透传 `VERSION/REVISION/UPDATE_VERSION` |

**总方案勘误（不静默改写）**：总方案 §2 表格与 §3.2.1 记"9 个模块 / 9 个 `go.mod`"，实测为
**10**。本批按 10 执行，并建议在总方案中同步更正该计数（不影响批次、版本与分支分配）。

## 2. `make build` 契约

```makefile
IMAGE_TARGETS := backend business-worker search-indexer admin-frontend frontend \
                 acceptance router marshaller monitor redis-exporter
build: build-programs build-images
build-programs:   # 遍历 $(MODULES) → $(MAKE) -C <module> build
build-images:     # 计算 4 个环境值 → docker compose … build $(if $(IMAGES),$(IMAGES),$(IMAGE_TARGETS))
```

- 裸 `make build` = 全部程序 + 前端 + 本地镜像；`make build MODULE=<name>` 保持 23-01 的组件语义。
- `build-images` 的环境值与现 CI 助手一致：`GOPULSE_VERSION`/`GOPULSE_IMAGE_TAG` ← `VERSION`，
  `GOPULSE_REVISION` ← `git rev-parse HEAD`，`GOPULSE_UPDATE_VERSION` ← patch+1；`VERSION` 非
  三段 SemVer 时立即非零退出。
- `make build-images IMAGES=<子集>` 用于有界验证；`make build-images DRY_RUN=1` 打印 build
  定义（`docker compose … build --print`），不构建。
- 目标失败必须非零退出：B1 一次性注入错误 `VERSION` 与一个必然失败的镜像目标，核对退出码；归属
  与清理仍遵守总方案 §3.4（只清本工作区创建的资源，不做全局 prune）。
- `make monitor-image` 本批**保持不动**（两个现有调用者：`quality-gates.yml:514` 的
  `integration-observe` job 与 `make dev-observe`）。总方案 §3.1 允许并入 `dev-observe` 或
  `build`；其内容摘要 tag 与复用标记属 23-03/23-04 的环境助手资产，故并入这两个目标，删除归
  23-03/23-04，本批只登记移交，不重复实现。

## 3. 插件打包承接与共享模块发布

### 3.1 打包工具设计（字节合同优先）

- 新命令置于 `monitor/cmd/plugin-package`：`plugin` 域、`internal/plugin` 已拥有清单 schema，
  且相关镜像阶段本来就构建 monitor 模块。
- **归档必须调用系统 `tar`/`gzip`**，参数与现实现逐字相同（`--sort=name --mtime='@0' --owner=0
  --group=0 --numeric-owner --format=ustar`，`gzip -n -9 -c`，临时文件后原子 `mv`）。Go 只负责
  参数校验、包内布局与权限（`plugin.json`/`config.schema.json` 0644、`bin/…` 0755）、元数据与
  退出码（用法/校验错误 `2`，环境或构建失败 `1`）。
- **v1 清单必须用排序键写出**（Go `map[string]any` 编码），不得复用 `internal/plugin.Manifest`
  的结构体字段顺序——结构体顺序会产生不同字节并破坏 7 个钉死摘要。
- v2 清单复用 `internal/plugin` 的共享函数，`plugin-package-metadata` 改为同源调用，输出不变。
- CLI 保持兼容：`--source`、`--version`、`--output`、`--binary`、`--contract-version`、`--arch`；
  省略 `--binary` 时按 `exporters/<source>` 自行构建。`--version` 与 `--output` 在 Go 工具内必填，
  由薄转发入口按现规则补齐默认值。
- 不新增执行器：字节合同由所属模块 `go test` 与 Dockerfile 既有钉死摘要检查共同保护，不建
  新的 runner/verifier。

### 3.2 等价证据（先取证、后替换）

1. 先用**旧实现**对固定夹具产出 v1 redis、v2 redis、v2 mysql 三个归档，记录 `sha256`，并把
   v1 归档提交为 `testdata` 参考回执（期望摘要写入用例常量），旧实现删除后仍可复验。
2. 新工具以相同参数产出同样三个归档，逐字节比较。
3. 生产级证据由镜像构建给出：`--target official-packages` 内部即校验 `redis-1.10.6` 内联摘要
   与 `phase14-1.11.5.sha256` 的 6 项摘要——新工具必须让这 7 个历史摘要原样通过。

### 3.3 旧实现的处置（记录保留，不是长期并存）

`scripts/package-redis-exporter.sh` 的实现迁入 Go 后，原路径只保留薄转发入口（`exec` 到
`go run ./cmd/plugin-package`，并补默认 `--version`/`--output`），用途明确：为
`verify-monitor.sh`、`verify-router.sh`、`verify-events.sh` 的 8 处调用与
`local_development.py:302` 的摘要输入保留稳定 CLI。这些调用者本身在 23-07 退役，届时该转发
入口一并删除；本批在完成记录中登记该保留项、调用者清单与删除归属，不以"更安全"为理由续期。

### 3.4 `componentmetrics` 发布与依赖切换

- 打子目录 tag `componentmetrics/v0.1.0`，指向**分支基线提交**（`develop/2.5.2` 从主远端 main
  创建时的 tip，已在 main 上），推送 tag 到主远端；用 `git ls-remote --tags` 与
  `GOPROXY=https://goproxy.cn,direct go list -m -versions …` 确认可解析。
- 本批**不得修改 `componentmetrics/**` 源码**；一旦需要修改，说明 tag 内容与工作树不一致，
  必须停下报告（重新打 tag 属新的发布决定）。
- 10 个模块逐个执行：`go mod edit -dropreplace=… -require=…@v0.1.0` →
  `go mod download github.com/Ray-ymq/GoPulse/componentmetrics`（写入 `go.sum`）→
  `go build ./... && go test -count=1 ./...`。是否追加 `go mod tidy` 由实际 diff 决定：仅当它
  只做本批相关整理时采用，否则保留 `go mod edit` 的最小 diff 并登记该选择。
- 删除 13 处已无用的 `COPY componentmetrics/`（依赖改为发布版本后，镜像内本地副本不再参与解析）。
- 记录两项已核实后果：(a) 消费模块不再使用 `replace`，本地跨模块改动需个人未跟踪 `go.work`；
  (b) `quality_scope.py:_local_replace_consumers` 对 10 个模块变为空集——`componentmetrics/**`
  改动不再触发消费模块检查，这与"消费已发布 tag"一致，但意味着**改库必须重新打 tag**，作为
  后续项登记（未来可增加"库变更需新 tag"的检查）。
- CI 镜像步骤本批不切换：`compose_build_cache.py` 仍提供 bake + GHA 层缓存，去掉它会显著增加
  compose/e2e job 耗时。本批提供 `make build-images` 入口并留下 `--print` 等价证据（§5 B2），
  该脚本的原生承接与 CI 切换登记为 23-07 的承接项（其分方案须重新登记预算）。

## 4. 预算与实验成本

| 项目 | 内容 |
| --- | --- |
| 范围 | 见 §1；一个交付结果（构建镜像入口 + 两条硬依赖改形），不摊入 03 起的目标 |
| 总预算 | 预计 160 分钟，**累计上限 180 分钟**（用户决定保持单批；已超出普通文件 120 分钟目标，故不设机动余额） |
| 阶段预算 | 实现 A（`make build`）20/25；实现 B（打包承接 + Dockerfile/文档）45/50；实现 C（tag + 10 模块）30/32；直接检查 20/22；真实验收 30/31；证据/版本/日志/提交 15/20。合计 160/180 |
| 实验成本 | 无长时实验。成本项：13 个 Go 构建 + 2 个前端构建（暖缓存各 1–3 分钟）；`--target official-packages` 一次真实镜像构建（含 8 个历史 Go 程序编译，预计 4–8 分钟，上限 12 分钟）；`acceptance-packages` 一次（上限 8 分钟）；1–2 个本地镜像构建（各 1–3 分钟）；10 个模块各一次临时目录解析与测试（各 ≤1 分钟） |
| 重试成本 | 同一未解决原因最多 2 次定向诊断，每次 ≤10 分钟；第一次镜像/代理/基础设施失败即停止整套验收，改用最小复现（单个 stage、单个模块、单个夹具） |
| 停点 | 50% / 80% 报告已完成项、剩余门禁、耗时与下一步；开始下一条昂贵命令前核对剩余预算；触顶按 §6 收尾 |

## 5. 验证映射与固定门禁

| 字段 | 内容 |
| --- | --- |
| 验证对象 | (a) `make build` 是否真的构建程序、前端与镜像并正确传参、失败非零退出；(b) 打包 CLI 的参数合同、失败退出与**归档字节**是否与旧实现一致；(c) 10 个模块是否能在没有本地 `replace` 的情况下从已发布版本解析并构建测试；(d) 版本元数据是否与 `VERSION` 一致 |
| 已有覆盖 | `monitor/internal/plugin/{manifest,archive,schema,contract_v2}_test.go`（v1/v2 清单与归档读写）；`deploy/docker/observability.Dockerfile:154-163` 与 `deploy/plugins/phase14-1.11.5.sha256`（7 个钉死摘要，现成且必须继续通过）；`scripts/verify-{monitor,router,events}.sh`（打包 CLI 的真实消费方）；`scripts/ci/test_compose_build_cache.py`（从 compose 派生 build 定义）；`quality-gates.yml` 各模块 job（`make test/check/race`）；`scripts/ci/{validate_versions,validate_branch,quality_scope}.py` |
| 最低有效层级 | 归档与清单是确定性制品变换 → 单元层 + 字节夹具是最低有效层级；`--target official-packages` 与一次本地镜像构建保留为真实制品/系统层，因为 7 个历史钉死摘要只在那里被校验；10 个模块的解析用最小真实依赖（临时目录内 `go build`）验证，不用静态检查替代。不新增执行器 |
| 本批变更 | 见 §1；复用既有 `go test`、既有 Dockerfile 钉死摘要检查、既有 `make` 目标与治理脚本，不新增 runner/sampler/verifier |
| 固定门禁 | 见下表 B1–B11；命令、期望结果、证据与失败分类同时登记 |

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| B1 | 构建入口可用、语义不变、失败非零退出 | `make build MODULE=backend`、`make build MODULE=frontend`、`make -n build`；注入 `VERSION=2.5` 与一个不存在的镜像目标 | 组件构建成功；`-n` 显示 `build-programs` 与 `build-images` 两段；缺 `MODULE` 的既有报错保留；两个注入用例均非零退出并给出诊断；只清本工作区创建的资源 |
| B2 | 镜像定义正确、与现 CI 助手等价 | `make build-images DRY_RUN=1` 对比 `python3 scripts/ci/compose_build_cache.py --print` | 同一 10 个目标集合、同一 tag（`gopulse/<name>:<VERSION>`）与 args；`acceptance` 的 `UPDATE_VERSION` = patch+1；差异仅限对方新增的 `platforms`/`output`/`cache-*` |
| B3 | 镜像真实构建 | `make build-images IMAGES=frontend`（如预算允许再 `IMAGES=monitor`） | 构建成功且 `docker image inspect gopulse/frontend:2.5.2` 存在；日志尾部留证 |
| B4 | 打包 CLI 合同 | `cd monitor && go test ./cmd/plugin-package/... ./internal/plugin/...` | 通过：非法 SemVer、未知 source、contract 1 非 redis、未知参数、缺失/不可执行 `--binary` 均以退出码 2 失败并给出诊断；`--arch` 校验生效 |
| B5 | 字节等价（旧 vs 新） | 旧实现对三个夹具产出摘要 → 新工具同参数复现 → 逐字节比较 | 三个归档 `sha256` 完全一致；v1 归档 = 提交的 `testdata` 参考回执；证据写在完成记录 |
| B6 | 生产钉死摘要未破坏 | `docker build -f deploy/docker/observability.Dockerfile --target official-packages --build-arg VERSION=2.5.2 --build-arg REVISION=$(git rev-parse HEAD) -t gopulse/official-packages:2.5.2 .` | 构建成功；`redis-1.10.6` 内联摘要与 `phase14-1.11.5.sha256` 6 项 `sha256sum -c` 全部通过；再执行一次 `--target acceptance-packages` |
| B7 | 镜像整体仍可构建 | `docker build … --target monitor`（或 `make monitor-image`） | 成功；`git diff` 中 `monitor_input_digest` 行为无意外变化 |
| B8 | 已发布依赖可解析 | 对 10 个模块：`tmp=$(mktemp -d); git archive HEAD:<module> \| tar -x -C "$tmp"; (cd "$tmp" && GOFLAGS=-mod=mod go build ./... && go test -count=1 ./...)` | 10 个模块在**没有兄弟 `componentmetrics/` 目录**时全部成功；`go list -m -f '{{.Version}}' github.com/Ray-ymq/GoPulse/componentmetrics` 输出 `v0.1.0`；`go.sum` 含该模块两行 |
| B9 | 依赖切换无回归 | `make test MODULE=<10 个模块>`（或分支 CI 对应 job） | 全部通过；`make check MODULE=<模块>` 无新增 `gofmt`/`vet` 报错 |
| B10 | 版本与治理门禁 | `python3 scripts/ci/validate_versions.py`；`python3 scripts/ci/validate_branch.py --branch develop/2.5.2 --base-ref <primary>/main --mode development`；`python3 scripts/ci/quality_scope.py --base-ref <primary>/main`；`python3 -m unittest discover -s scripts/ci -p 'test_*.py'`；`bash -n` 改动过的 shell 文件；解析改动过的 YAML | 全部通过；`quality_scope.py` 选中的检查与本批改动匹配（含 `componentmetrics` 消费模块不再被 `replace` 触发的说明）。`<primary>` 指先 `git fetch` 的主远端（本机 `origin`/`upstream` 为同一 URL，`upstream/main` 可能落后于 23-01 的合并，取能解析到 `7e23cc9` 的 `main`） |
| B11 | 文档与命令一致 | 复核 `deploy/plugins/README.md`、4 个 exporter README 的命令与摘要表述；`make help` | 文档命令可执行且指向 `make package-plugin`；README 中 7 个历史摘要表述保持为真 |

**失败分类**：产品失败（字节不一致、模块解析失败、镜像构建失败、门禁不通过）必须修最小受影响层
并只重验受影响门禁；基础设施失败（代理/网络、registry、docker daemon、GHA runner 缓存）先做
最小复现，不得重启整套构建。

## 6. 停止、接续与完成

**完成条件**（缺一不可）：

1. §5 的 B1–B11 全部通过，或未通过项被明确登记为"未开始/待核对"且不计入完成；
2. `componentmetrics/v0.1.0` 已推送且在 10 个模块中替代 `replace`，B8 通过；
3. 镜像构建不再依赖 `scripts/package-redis-exporter.sh` 的实现，且 7 个历史钉死摘要原样通过；
4. `VERSION`、`.env.example` 与 4 个前端包文件同步为 `2.5.2`，`validate_versions.py` 通过；
5. `dev/logs/Phase-23/Phase-23-02-构建镜像入口与共享模块发布.md` 已写入实际完成项、实际改动
   文件、实际命令与结果、偏差与后续项；`VERSION` 在完成提交内更新；
6. 分支 CI 绿（模块 job、`scripts-and-compose`、`compose-full-stack`）。

**偏差处理**：

- tag 无法推送或无权限：停下报告，**不**把 `replace` 删除（保留可构建状态），在日志登记阻塞
  事实与已完成项，本批不声明完成。
- 字节无法复现：保留旧实现作为镜像构建实现，登记失败事实、最小复现与影响范围，本批不声明完成；
  不以"放宽/重算钉死摘要"作为修复。
- `--target official-packages` 因基础设施（网络、registry、缓存）失败：按 §5 失败分类做最小复现，
  最多两次定向诊断；未取得已证明原因前不重复整套构建。
- 触顶（累计 180 分钟）：停止新工作，保留候选身份（revision、`VERSION`、镜像 tag 与摘要）、
  通过/失败/未开始清单与清理结果，登记接续清单；不为未完成的批次创建完成提交，续做需修订的
  有界方案与用户明确指示。

**接续粒度**：门禁按条可独立恢复——B4/B5 只依赖夹具与 `tar`/`gzip` 版本；B8 按模块独立；
B6/B7 依赖镜像构建缓存（复用缓存不等于复用无效证据）。会使其失效的改动：Go 工具或
`internal/plugin` 元数据逻辑（B4/B5/B6）、Dockerfile 或 compose 构建定义（B2/B3/B6/B7）、
任一 `go.mod`/`go.sum`（B8/B9）、`VERSION` 与版本元数据（B10 及所有镜像 tag）。新候选不重置
累计耗时，旧参考回执在夹具与 `tar`/`gzip` 版本未变时保持有效。

**接续项（登记给后续批次，不属本批）**：`compose_build_cache.py`（108 行）的原生承接与
`quality-gates.yml`/`cache-warm.yml` 镜像步骤切换归 23-07，其分方案必须重新登记预算
（该批原预算 90/180 未包含此项）；`make monitor-image` 的职责并入 23-03/23-04；`componentmetrics`
变更需重新打 tag 的检查作为后续项。

## 7. 与其他文件的关系

- 复用 23-01 已建立的组件 `Makefile` 调度与模块清单单一来源，不重复定义第二份模块表。
- 总方案 §3.2.1 的"发版并切换依赖"、§3.5 的"并存期等价 + 到位即删"在本批执行；§3.3 的 CI
  镜像步骤切换因缓存机制依赖本批不承接，已在 §3.4 与 §6 记录归属。
- 不修改 `dev/design/` 两份冻结设计与落地大纲；不重建已退役的 Phase 矩阵；不重写历史证据。

## 8. 决策记录（2026-10-09）

| 决策 | 结论与理由 |
| --- | --- |
| 批次边界 | 保持单批 23-02（用户决定），累计上限 180 分钟；三项内容不拆分，按 §4 阶段预算执行并在 50%/80% 报告 |
| 打包实现形式 | Go 工具 + `go test`（用户决定）；归档字节由系统 `tar`/`gzip` 固定参数保证，已有实测支持 |
| `replace` 处置 | 按总方案删除 `replace`、改用已发布 `v0.1.0`；不签入 `go.work`，本地跨模块联调使用未跟踪工作区 |
| 旧脚本 | 保留薄转发入口至 23-07（8 处活调用 + 摘要输入），登记为有理由保留项，不视为长期并存 |
