# Phase-23-04：交付入口原生承接与隔离安装

> 状态：初稿 2026-10-09。目标版本 `2.5.4`，分支 `develop/2.5.4`。
> 进入条件：Phase-23-03 完成并进入主远端 main（`VERSION=2.5.3`，完成提交 `1da8eb2`；实测
> `origin/main` 为 `ca74642`，已包含 23-03 与本方案所依据的总方案合并修订）。
> 用户决定（2026-10-09）：本批用 Go 原生承接交付链，真实验收只做本机 `linux/amd64` 候选 +
> `lifecycle` 隔离安装；双平台候选与 Compose 运行时门禁的**执行器承接**留给 23-05（见 §8）。
> 实现从主远端 main 创建已分配分支并使用独立工作树；不沿用完成分支，不把实现放在 `update`。

## 1. 交付与允许文件

**唯一主要交付结果**：`make package` 成为唯一交付入口——从已提交源码树构建 10 个镜像、组装
Bundle/manifest/摘要、校验、promote，全部由 `lifecycle` 模块内的 Go 命令实现，交付入口不再经过
Python；并在该入口产出的候选 Bundle 上完成一次真实的 `lifecycle` 隔离安装、启动与卸载。

职责边界不变（承总方案 §1）：根 Makefile 只做目标、依赖与参数调度；`lifecycle` 模块承载交付实现
与发布清单合同；Compose 仍是运行拓扑的唯一来源；断言仍在既有 Go 测试与 `lifecycle` 自身检查里。
Makefile recipe 保持短小，不放长篇 Shell。

### 1.1 允许新增

| 文件 | 内容 |
| --- | --- |
| `lifecycle/cmd/gopulse-package/main.go` | CLI：`run` / `build` / `verify` / `promote` / `env`；参数校验、退出码、失败传播 |
| `lifecycle/internal/packaging/**` | 候选构建（`git archive` 上下文 + buildx）、Compose 变换、镜像与插件元数据检查、Bundle 组装与校验、promote、loopback registry 归属 |
| `lifecycle/internal/packaging/**/*_test.go`、`testdata/**` | 单元用例与夹具（可注入的命令执行、内存 tar.gz/JSON 夹具、假 registry 命令记录） |
| `lifecycle/bin/`（构建产物） | 由 `lifecycle/Makefile` 生成；`.gitignore` 增加 `lifecycle/bin/` |

### 1.2 允许修改

| 文件 | 改动 |
| --- | --- |
| `lifecycle/Makefile` | 新增 `package` 目标：`$(GO) build -o bin/gopulse-package ./cmd/gopulse-package`；其余目标不动 |
| `Makefile`（根） | 新增 `package` 目标与 `help` 行；`PLATFORM ?= linux/amd64`、`OUTPUT ?= dist`；recipe ≤3 行，不放 Shell 逻辑 |
| `.github/workflows/release-candidate.yml` | 四步 Python/Shell 调用改为同一 Make 目标（`make package RUNTIME=1 PROMOTE=1 PLATFORM=linux/amd64,linux/arm64`）；删除独立 registry 启动/清理两步（改由工具自持 loopback registry）；保留 artifact 上传与超时 |
| `deploy/release/README.md` | 构建/校验/promote 命令改为 `make package`；说明本机只有 amd64、双平台经 CI、`PROMOTE=1` 需要运行时回执 |
| `README.md` | “Existing entry”表（`:102`）的 Bundle/release 一行改为 `make package` 与 `release-candidate.yml`；“Development entry points / Product installation and recovery”处补 `make package` 说明 |
| `dev/validation/Phase-16/phase16-linux-matrix.md` | 只把“固定入口”处（`:16`）的命令改为 `make package RUNTIME=1 …`；候选冻结、验收镜像与矩阵语义原文不动 |
| `deploy/runtime-contracts.json` | `product_version` 由 `2.3.3` 同步为 `2.5.4`（见 §1.7 实测第 3 条） |
| `.gitignore` | 增加 `lifecycle/bin/`（与既有 `backend/bin/`、`devtools/bin/` 同形） |
| `scripts/ci/test_release_snapshot.py` | 删除依赖 `verify_release_artifacts.run_compose_gate` 的用例，其行为迁移到 Go 用例（见 §4.1） |
| `scripts/ci/quality_scope.py`、`scripts/ci/test_quality_scope.py` | 仅当 `lifecycle/**` 需追加选择时才改；默认 `lifecycle` job 已覆盖本批全部 Go 代码，预计不改，若改须在 §6 偏差登记 |
| `dev/imple/Phase-23/Phase-23-04-交付入口原生承接与隔离安装.md`、`dev/logs/Phase-23/Phase-23-04-交付入口原生承接与隔离安装.md` | 本批实测记录与同名日志（`validate_branch.py --mode completion` 要求同名日志） |
| `VERSION`、`.env.example`、`frontend/package{,-lock}.json`、`admin-frontend/package{,-lock}.json` | 同步到 `2.5.4`（`sync_version_metadata.py`） |

### 1.3 允许删除（先取证、后删除）

| 文件 | 行数 | 删除条件 |
| --- | --- | --- |
| `scripts/ci/verify_release_artifacts.py` | 54 | Go `verify` 承接元数据检查、生命周期容器身份检查、固定 Compose 闭包调用与回执写出；其唯一导入方（`test_release_snapshot.py:9`）用例已迁移到 Go |
| `scripts/verify-release-artifacts.sh` | 7 | 其唯一调用方（`release-candidate.yml:33-34`）改为 Make 目标后无剩余调用者 |

删除前必须核对引用面（`scripts/AGENTS.md` §4）：`release-candidate.yml:33-34`、`test_release_snapshot.py:9`、
`deploy/release/README.md:8-10`、`README.md:102`、`dev/validation/Phase-16/phase16-linux-matrix.md:16`。
文档中原有的 `scripts/verify-release-artifacts.sh --self-test` 入口随转发器一并消失：其 Python 套件
（`test_release_*.py`）已由 `governance` job 的 `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`
执行，Go 侧由 `make test MODULE=lifecycle` 覆盖，两处文档不得保留已删除命令。
历史方案与 `dev/logs/**` 中的引用不改写。

### 1.4 明确保留（附实测导入证据，登记给 23-05，不在本批删除）

| 文件 | 行数 | 保留理由（实测） |
| --- | --- | --- |
| `scripts/ci/release_artifacts.py` | 302 | `verify_bundle` / `platform_ref` / `inspect_image` / `plugin_records` / `run` 被 8 个仍然有效的验收执行器与 `release_candidate_env.py:5` 导入：`phase16_acceptance.py:14`、`verify_phase17.py:15`、`verify_phase17_state.py:10`（并在 `:140` 把本文件作为共享文件送入容器）、`verify_phase17_migration.py:16`、`verify_backup_restore.py:18`、`verify_alerts.py:39`、`runtime_acceptance.py:21`、`candidate_runtime.py:10`（后者由 `verify-business.sh:1490`、`verify-marshaller.sh:267` 调用） |
| `scripts/ci/release_manifest.py` | 95 | 被 `release_artifacts.py:15`、`release_candidate_env.py:4`、`phase16_evidence.py:9`、`verify_release_artifacts.py:9` 导入；`test_release_manifest.fixture` 被 `test_phase16_evidence.py:10` 导入 |
| `scripts/ci/release_candidate_env.py` | 15 | `verify-compose-observability.sh:76` 在候选清单模式下调用它输出镜像引用 |
| `scripts/ci/test_release_artifacts.py`、`test_release_manifest.py` | 75 / 47 | 保护上述保留函数的既有断言，后者还提供共享夹具；退役条件与 `release_artifacts.py` 同步 |
| `scripts/ci/test_release_snapshot.py` | 43（改） | 其余两条用例保护保留的 `verify-compose-observability.sh` 行为 |
| `scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh` | 447 / 795 | 本批 `verify --runtime` 仍调用同一固定入口；前者同时是 CI `compose-full-stack` job 的实现，属保留验收执行器 |

**退役条件（登记给 23-05）**：`release_artifacts.py` / `release_manifest.py` 只有在上述 Python 消费方
全部退役或改由 Go 消费之后才能删除；删除前必须附 `grep -rn` 导入面与
`python3 -m unittest discover -s scripts/ci -p 'test_*.py'` 全绿证明。本批不删除它们，也不以
“入口已改为 Go”为由声称交付库已退役。

### 1.5 不属于本批

`scripts/` 其余内容（`verify-compose*.sh`、`verify-business.sh`、`verify-marshaller.sh`、
`candidate_runtime.py`、`runtime_acceptance.py`、`verify_product_lifecycle.py`、
`compose_build_cache.py`、`package-redis-exporter.sh` 等）的承接与删除（23-05）；arm64 真实运行与
双平台候选的本机执行（本机无 binfmt/QEMU，见 §1.7）；`make build` / `make build-images` 的既有语义
（23-02 已完成）；业务与观测产品代码及其断言；`deploy/compose.yaml` 的正式部署合同。

### 1.6 不改变

`VERSION` 单一来源与发布模型（本批不做组件独立发布）；Bundle/manifest/回执/promote 的既有合同与
摘要语义（只换实现语言，不放宽校验）；业务 API、权限、消息与正式证据合同；目录结构；冻结的两篇
设计、落地大纲与 `dev/design/README.md` 注册基线；已退役的 Phase 矩阵不重建；历史证据不改写。

### 1.7 实测事实与硬约束（开工前已核实）

| 事实 | 证据 |
| --- | --- |
| 交付链现状 | `release-candidate.yml:32-35` 为四步：`release_artifacts.py build --registry 127.0.0.1:15001/gopulse` → `verify-release-artifacts.sh --platform linux/amd64 --runtime` → `... --platform linux/arm64 --metadata-only` → `release_artifacts.py promote`；`grep -rn` 确认无其他调用者 |
| 清单合同已在 Go 侧 | `lifecycle/internal/release/manifest.go`（265 行）实现 `Parse`（重复键与未知字段拒绝）、`Validate`（镜像/插件/平台集合交叉校验）、`CheckAssets`（runtime contract 校验和与 `product_version` 相等）、`CheckTool`；`control.go:265-304` 在安装路径上调用它们。该包为 `internal`，Go 侧复用要求交付命令位于同一模块 |
| runtime contract 版本漂移 | `deploy/runtime-contracts.json` 的 `product_version` 仍为 `2.3.3`；实测 `python3 scripts/ci/verify_runtime_contracts.py --candidate 2.5.4` 报 `candidate version does not match runtime contract`，且 `CheckAssets` 要求它与 manifest 版本相等 → 候选构建前必须同步为 `2.5.4`（`test_runtime_contracts.py:13` 以 `check_version=False` 运行，不因该同步失败） |
| 镜像构建面 | 10 个镜像（9 产品 + lifecycle）：`backend`/`business-worker`/`search-indexer` 用 `deploy/docker/backend.Dockerfile` 的三个 target；`router`/`marshaller`/`monitor`/`redis-exporter` 用 `observability.Dockerfile` 的 target；`frontend`/`admin-frontend`/`lifecycle` 用各自 Dockerfile 的默认 target；上下文为 `git archive --format=tar <revision>` 送入 stdin |
| 候选与 Bundle 合同 | tag `<registry>/<name>:<version>-candidate-<revision[:12]>`、`--provenance=false`；单平台构建后若无 manifest list 则 `imagetools create --prefer-index=true`；Bundle 允许集 `{deploy/product/compose.yaml, compose.yaml, README.md, release-manifest.json, checksums, deploy/runtime-contracts.json, deploy/runtime-contracts.schema.json}`，USTAR、mtime 0、mode 0644、uid/gid 0、LF-only，另有 `gopulse-<version>-bundle.tar.gz` 与 `.sha256`（行格式 `"<sha>  <name>\n"`） |
| promote 合同 | 需要 `verification-<arch>.json` 逐字段匹配 `{manifest_sha256, revision, platform, status}`；amd64 状态为 `amd64-runtime-and-compose-passed`；逐镜像 `imagetools create` 复制同一 index 并断言摘要不变；同名 tag 已存在且内容不同则拒绝覆盖 |
| Compose 变换合同 | 12 条别名（`backend-2`/`platform-api`→`backend`、`*-2`→对应产品、`observability-elasticsearch`→`elasticsearch`、`migrate`/`search-init`/`admin-role`→`backend`、`kafka-init`→`kafka`）；`edge` 为 `frontend` 的副本并独占 ports；其余服务去 ports、去 build、`pull_policy: always`；卷/网络/secret 去 `name`；未映射服务必须报错 |
| 本机能力边界 | `docker buildx inspect default` 只有 `linux/amd64, linux/amd64/v2, linux/amd64/v3`；`/proc/sys/fs/binfmt_misc/` 为空且无 `qemu-*-static`（`docker run --platform linux/arm64 alpine:3 uname -m` → `exec format error`）→ 本机只能构建与运行 amd64 候选；`registry:2` 与 Go/Alpine 基础镜像已在本机缓存 |
| CI 调用面 | `release-candidate.yml` 为 `workflow_dispatch`、`timeout-minutes: 120`、用 `setup-qemu-action` 提供 arm64；`quality-gates.yml:350-362` 的 `compose-full-stack`（`timeout-minutes: 60`）跑 `scripts/verify-compose.sh` 源码闭包 |
| 版本元数据 | `sync_version_metadata.py` 只同步 `VERSION`、`.env.example`、4 个前端包文件；`deploy/runtime-contracts.json` 的 `product_version` 不在其中 |
| 治理门禁 | 改 `lifecycle/**` 触发 `lifecycle` job（check/test/race）；改 `deploy/**` 触发 `compose` + `tools`；改 `scripts/**` 触发 `tools`；改根 `Makefile` 触发保守全量选择（既有行为，登记为已知成本） |

## 2. 原生交付入口设计

### 2.1 调用链

```makefile
# 根 Makefile —— 只做调度，保持短小
PACKAGE  := $(MAKE) --no-print-directory -C lifecycle package && ./lifecycle/bin/gopulse-package
PLATFORM ?= linux/amd64
OUTPUT   ?= dist

package:
	@$(PACKAGE) run --output $(OUTPUT) --platform $(PLATFORM) \
	  $(if $(REGISTRY),--registry $(REGISTRY),) $(if $(RUNTIME),--runtime,) $(if $(PROMOTE),--promote,)
```

`lifecycle/Makefile` 自行决定编译参数与产物路径。命令用构建产物而非 `go run`：`go run` 会把程序
非零退出折叠为 1，无法保留退出码（23-03 已实测）。

对外行为：

| 用法 | 行为 |
| --- | --- |
| `make package` | 启动自有 loopback registry（可 `REGISTRY=` 覆盖）→ 构建 10 个镜像 → 组装 Bundle/manifest/摘要 → 元数据校验 → 输出候选路径；默认 `linux/amd64` |
| `make package RUNTIME=1` | 追加生命周期容器身份检查与固定 Compose 闭包（`scripts/verify-compose.sh`，候选引用经 `GOPULSE_RELEASE_MANIFEST`），写出 `verification-<arch>.json` 的运行时回执 |
| `make package RUNTIME=1 PROMOTE=1` | 在前者成功后执行同摘要 promote（缺少匹配回执时明确失败，不放宽） |
| `make package PLATFORM=linux/amd64,linux/arm64` | 双平台候选；本机无 binfmt 时构建失败，只在 CI 使用 |

退出码：`0` 成功；`2` 参数或前置条件错误（源码树未提交、registry 命名空间非法、平台取值非法、
缺少 manifest、输出目录已存在完整候选、Bundle 允许集不符）；`1` 运行期失败（docker/buildx/registry、
拉取、校验和不匹配、promote 被拒）。子进程失败必须传播为非零，不得吞掉。

### 2.2 registry 归属

`--registry` 缺省时由工具自持：以固定摘要的 `registry:2`（沿用 `release-candidate.yml` 现有钉死
digest）在 `127.0.0.1` 随机空闲端口启动唯一命名的容器，`run` 结束（含失败与 SIGINT）只删除该容器；
不做全局 prune，不触碰非本次创建的容器、网络与卷。传入 `--registry` 时调用方自持，工具不启动也不
删除任何容器。CI 因而删除原有的 registry 启动/清理两步，本地“全新检出、无手工准备”成立。

### 2.3 包职责与测试接缝

| 包 | 职责 | 最低有效层级用例 |
| --- | --- | --- |
| `internal/packaging/bundle` | 允许集组装、USTAR/mtime 0/0644/uid-gid 0 归档、checksums 与 detached sha256、Bundle 反校验 | 内存夹具表驱动用例 |
| `internal/packaging/compose` | `docker compose config --format json` → 别名映射、`edge` 生成、镜像引用替换、资源去名、未映射服务拒绝 | 固定 JSON 夹具用例 |
| `internal/packaging/image` | `imagetools inspect --raw` 解析、index/platform 摘要一致性、OCI 标签/入口/数值用户检查 | 录制 raw manifest 夹具用例 |
| `internal/packaging/plugin` | pull/create/cp/rm、tar.gz 读取、ELF 架构与 entrypoint/schema 摘要校验 | 夹具归档用例 |
| `internal/packaging/build` | git archive 上下文、逐镜像 buildx、候选 tag、manifest 最后落盘 | 命令构造用例 + 真实门禁 |
| `internal/packaging/promote` | 回执门禁、同 index 复制与摘要不变断言、已存在不同内容拒绝 | 用例 + 真实门禁 |
| `internal/packaging/verify` | 元数据校验编排、生命周期容器身份检查、固定闭包调用与错误行阻断、回执写出 | 用例 + 真实门禁 |
| `internal/packaging/registry` | 自有 registry 启停与归属 | 命令构造用例 + 真实门禁 |
| `cmd/gopulse-package` | CLI、参数校验、退出码、输出与诊断 | 用例 |

### 2.4 必须逐项保持的等价合同

- 候选身份：tag 格式、`--provenance=false`、`git archive` 上下文、`--metadata-file`、单平台补建 index。
- manifest：`schema_version=1`、字段集合、`runtime_contract`/`runtime_contract_schema` 资产、
  `bundle_sha256`（排除 manifest 与 checksums 的载荷摘要）、`compose.sha256`；manifest 最后落盘，
  失败构建只留诊断不留完整候选；已存在完整候选时拒绝覆盖。
- Bundle：允许集、USTAR、mtime 0、mode 0644、uid/gid 0、LF-only、checksums 内容与排序、归档与 detached 摘要。
- Compose 变换：§1.7 的 12 条别名、`edge` 语义、去 ports/build、`pull_policy: always`、未映射服务报错。
- 校验：回执键与状态文案逐字保持（amd64 需运行时+闭包状态，arm64 为 metadata-only 文案）；
  第三方镜像以非产品模式检查。
- promote：回执不匹配即拒绝；复制同一 index 而非重建或重新序列化；摘要变化即失败。
- 确定性：Bundle 归档与摘要对同一 revision 可重复（USTAR、mtime 0、名称排序）；Go 的 JSON 键排序
  与 Python 的插入序不同，因此本批只承诺“同一 revision 内确定性”，不承诺与旧 Python 输出逐字节
  相同（既有候选摘要均为历史值，不受影响）。

### 2.5 新增能力的边界（`scripts/AGENTS.md` §3）

本批不新增 runner、sampler 或 verifier：交付入口是既有实现的**一对一替换**，断言仍由既有 Go 测试、
`lifecycle` 自身检查、固定 Compose 闭包与 `lifecycle` 隔离安装承担；回执仍写既有
`verification-<arch>.json` 与既有上传路径。新增文件只有交付实现与其模块单元测试，共享部分
（清单合同）复用 `lifecycle/internal/release`，不复制第二份合同实现。

## 3. 执行顺序与强制停点

单批两阶段，中间为**强制停点**：前一阶段门禁未通过不得进入后一阶段，也不得用后一阶段的结果替代
前一阶段的失败。

- **阶段 A（入口原生承接）**：§1.1/§1.2 的实现、单测、Make/CI/文档切换、§1.3 的两处退役、
  版本元数据（含 `deploy/runtime-contracts.json`）、本机 amd64 真实候选。门禁 P1–P5、D1–D3。
- **阶段 B（隔离安装与交付验证）**：在阶段 A 产出的候选 Bundle 上执行真实 `lifecycle` 隔离安装；
  触发 CI 交付门禁。门禁 I1、D4–D5。

**强制停点检查**：阶段 A 结束时报告已完成项、剩余门禁、累计耗时与下一步；剩余预算不足以覆盖
阶段 B 的真实安装与 CI 收尾时，在停点收尾并登记接续（§6），不占用后续预算硬跑。

## 4. 验证映射与固定门禁

| 字段 | 内容 |
| --- | --- |
| 验证对象 | (a) `make package` 是否真实构建同一候选并产出符合既有合同的 Bundle/manifest/摘要；(b) 校验与 promote 的回执门禁是否等价、失败是否非零且不残留完整候选；(c) 退役后是否仍有活引用；(d) CI 是否调用同一目标且交付链仍通过 |
| 已有覆盖 | `lifecycle/internal/release/manifest_test.go`（2 例：清单与服务器边界、amd64 不要求 arm64 制品）、`lifecycle/internal/control/*_test.go`（安装/初始化/拒绝不安全 compose）；`scripts/ci/test_release_artifacts.py`（别名闭包、未映射服务、别名表）、`test_release_snapshot.py`（`run_compose_gate` 的零退出错误行阻断）、`test_release_manifest.py`（清单校验）；`release-candidate.yml`（双平台候选真实回执）；`scripts/ci/verify_product_lifecycle.py --clean-install`（真实安装/启动/卸载） |
| 最低有效层级 | Bundle 组装/归档字节、Compose 变换、别名表、清单与回执校验、参数与退出码用 Go 单元层（无 Docker、可复现注入）；镜像构建、registry、插件归档、真实安装只在真实系统层验证。静态检查不替代真实门禁 |
| 本批变更 | 见 §1；复用既有 `go test`、固定 Compose 闭包、`lifecycle` 隔离安装与治理脚本，不新增执行器、不新增证据 schema、不改回执格式 |
| 固定门禁 | 下表 P1–P5、I1、D1–D5；命令、期望结果、证据与失败分类同时登记 |

### 4.1 既有自测的迁移映射（删除 Python 用例前必须逐条落地）

| 既有用例 | 迁移到 |
| --- | --- |
| `test_release_artifacts.test_service_mapping_closes_current_compose` | `internal/packaging/compose`：别名闭包、`edge` 独占 ports、`pull_policy`、`*-2` 与 `observability-elasticsearch` 映射 |
| `test_release_artifacts.test_unknown_service_is_rejected` | `internal/packaging/compose`：未映射服务在建包前失败 |
| `test_release_artifacts.test_aliases_cover_each_new_split_service` | `internal/packaging/compose`：12 条别名表逐项断言（与保留的 Python 表同值） |
| `test_release_snapshot.test_runtime_gate_rejects_logged_error_with_zero_exit` | `internal/packaging/verify`：闭包输出含 `[gopulse-compose] ERROR:` 时即使退出 0 也判失败 |
| `test_release_manifest`（清单校验、重复键、平台集合、插件目录） | 不迁移：已由 `lifecycle/internal/release` 的 `Parse`/`Validate` 及其测试覆盖，本批补差异用例（见 P2） |
| `test_release_snapshot` 其余两条 | 不迁移：保护保留的 `verify-compose-observability.sh` |

### 4.2 阶段 A 门禁

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| P1 | 入口正确且不含 Python | `make help`；`make -n package`；`grep -rn python Makefile lifecycle/Makefile lifecycle/cmd/gopulse-package lifecycle/internal/packaging`；`grep -c python .github/workflows/release-candidate.yml` | `help` 列出 `package`；dry-run 指向 `lifecycle/bin/gopulse-package run`；两处 Makefile 与新增 Go 代码 0 处 Python；workflow 0 处 `python3`；recipe ≤3 行 |
| P2 | 单元与静态检查 | `make test MODULE=lifecycle`、`make race MODULE=lifecycle`、`make check MODULE=lifecycle`；`python3 -m unittest discover -s scripts/ci -p 'test_*.py'` | 全过；§4.1 映射的行为均有对应用例；Python 套件用例数变化逐项解释（仅少 1 条已迁移用例） |
| P3 | 本机真实候选与 Bundle 合同 | `make package`（默认 amd64）两次（第二次应被拒）；用**保留的独立实现**交叉校验：`python3 -c` 调 `release_artifacts.verify_bundle`；`tar -tzf` 列成员 | 第一次退出 0 并产出 `dist/release-manifest.json`、`dist/gopulse-2.5.4-bundle.tar.gz{,.sha256}`、`checksums`；Go 写出的 Bundle 通过 Python 侧 `verify_bundle` 的全部校验（允许集、成员属性、字节、checksums、detached 摘要），证明两份实现未背离；第二次因“完整候选已存在”非零退出且不改动既有候选 |
| P4 | 元数据与插件真实校验 | 候选构建后的 `verify` 步骤输出；`docker buildx imagetools inspect --raw` 抽查 index 与平台摘要；解包 `verified-plugins/amd64` | 9 个产品 + 6 个第三方 + lifecycle 全部 PASS；6 个 current 插件 + 1 个 1.9.4 upgrade-only 的 ELF 架构、entrypoint 与 schema 摘要一致；与 manifest 记录逐字段相等 |
| P5 | 失败注入非零退出与残留 | (a) 脏源码树；(b) 输出目录已有完整候选；(c) 非法 registry 命名空间；(d) 单元层：篡改 Bundle 任一字节后校验；(e) registry 不可用（占用端口后重跑） | 五种注入均非零退出并给出可操作诊断；不产生完整 `release-manifest.json`；不删除非本次创建的容器/网络/卷；`PROMOTE=1` 而缺少运行时回执时明确拒绝 |
| D1 | 版本元数据一致 | `VERSION`、`.env.example`、4 个前端包文件、`deploy/runtime-contracts.json`；`python3 scripts/ci/validate_versions.py`；`python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.5.4`；`python3 -m unittest discover -s scripts/ci -p 'test_runtime_contracts.py'` | 全部为 `2.5.4`；`validate_versions.py` 与 runtime contract 校验通过（实测该命令在 `2.3.3` 时失败，同步后必须通过） |
| D2 | 文档与入口一致 | `deploy/release/README.md`、`README.md`、`make help` | 文档命令可直接执行且与 `help` 一致；说明本机 amd64、双平台经 CI、`PROMOTE=1` 的回执前置 |
| D3 | 治理门禁 | `python3 scripts/ci/validate_versions.py`；`python3 scripts/ci/validate_branch.py --branch develop/2.5.4 --base-ref origin/main --mode development`；`python3 scripts/ci/quality_scope.py --changed-file lifecycle/internal/packaging/bundle/bundle.go --format json`；改动过的 YAML 解析 | 全过；`lifecycle/**` 选中 `lifecycle`；`deploy/**` 选中 `compose` + `tools`；无残留对已删除文件的引用 |

### 4.3 阶段 B 门禁

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| I1 | 隔离安装、启动与卸载（总方案 §4 判据 4 的本批部分） | `python3 scripts/ci/verify_product_lifecycle.py --clean-install --platform linux/amd64 --manifest dist/release-manifest.json`（全新工作树，无 `.run/`） | 退出 0 且回执 `status=passed`；`doctor/init/verify/status/logs/up/down` 阶段退出码符合既有矩阵；`/`、`/admin/`、`/health` 均 200；状态与密钥文件 0600；输出无密钥泄漏；`down --purge` 后本安装资源全部消失且未影响无关资源；`isolation_preserved=true` |
| D4 | CI 交付门禁（同一目标） | 推送后 `gh workflow run release-candidate.yml --ref develop/2.5.4` 并等待候选 job | 候选 job 绿：双平台构建、amd64 运行时+Compose 闭包回执、arm64 metadata-only 回执、`PROMOTE=1` 通过；artifact `local-candidate-evidence` 上传成功；自有 registry 已清理；回执与 manifest 同 revision |
| D5 | 分支 CI 与完成元数据 | 推送后 `quality-gates.yml` 全部 job；`python3 scripts/ci/validate_branch.py --branch develop/2.5.4 --mode completion`；同名日志存在 | 全部 job 成功且 `lifecycle`、`Full-stack Compose acceptance`、`Scripts and Compose` 实际执行而非跳过；完成校验输出 `Branch governance passed for develop/2.5.4.`；日志只记录实际完成项 |

**失败分类**：产品失败（Bundle/manifest/摘要合同不一致、回执不匹配、promote 被错误接受、真实安装失败、
治理门禁不通过）修最小受影响层并只重验受影响门禁；基础设施失败（buildx、registry、网络、npm 安装、
Docker daemon、GHA runner）先做最小复现，第一次即停止该阶段，不重启整套候选构建。

## 5. 预算与实验成本

| 项目 | 内容 |
| --- | --- |
| 范围 | 见 §1；一个交付结果（`make package` 原生入口 + 两处退役 + 真实隔离安装），不摊入 23-05 的 `scripts/` 其余删除 |
| 总预算 | 预计 140 分钟，**累计上限 180 分钟**（普通实施文件，承总方案 §5.1） |
| 阶段预算 | A 段 110：实现 80 / 直接检查 10 / 构建准备 5 / 真实候选 10 / 证据与版本 5。B 段 60：真实安装 30 / CI 触发与等待准备 10 / 证据、日志与收尾 20。合计 170 ≤ 180；预计 140。A/B 各段内部 50%/80% 报告 |
| 实验成本 | 无长时测量窗口。真实墙钟：本机 amd64 候选（10 镜像，Go/Alpine 基础镜像已缓存）15–30 分钟；元数据与插件校验 2–3 分钟；固定 Compose 闭包不在本机执行（60 分钟级，见 §8）；隔离安装（含冷启动与 `down/up` 循环）15–30 分钟；失败注入 5 次各 ≤2 分钟。合计约 35–70 分钟墙钟，已含在阶段预算内 |
| CI 成本 | `release-candidate.yml` 一次（`timeout-minutes: 120`，双平台 + QEMU arm64 + 固定 Compose 闭包）；`quality-gates.yml` 全量 job。属等待时间，不计入主动预算，但收尾前必须取得结果 |
| 重试成本 | 同一未解决原因最多 2 次定向诊断、每次 ≤10 分钟；不以延长超时或跳过校验掩盖问题；不重复整套候选构建 |
| 停点 | 累计 50%/80%（90/144 分钟）与 §3 强制停点报告已完成项、剩余门禁、耗时与下一步；开始昂贵命令前核对剩余预算 |

## 6. 停止、接续与完成

**完成条件**（缺一不可）：

1. `make package` 的构建、Bundle/manifest/摘要、校验与 promote 由 Go 原生实现，交付入口 0 处 Python；
   `release-candidate.yml` 调用同一目标；
2. P1–P5 全部通过，或未通过项被明确登记为“未开始/待核对”且不计入完成；
3. `scripts/ci/verify_release_artifacts.py` 与 `scripts/verify-release-artifacts.sh` 已删除、仓库内 0 处
   活引用，`test_release_snapshot.py` 的迁移用例已在 Go 落地且 Python 套件全绿；
4. I1 通过：全新环境用 `lifecycle` 安装、启动、校验、停止、卸载 `make package` 产出的 Bundle；
5. D1–D5 通过，含 `deploy/runtime-contracts.json` 同步为 `2.5.4`、CI 交付门禁绿与分支 CI 绿；
6. `VERSION` / `.env.example` / 4 个前端包文件同步为 `2.5.4`，`validate_versions.py` 通过；同名实施
   日志已写入且只记录实际完成项；`VERSION` 在完成提交内更新；
7. §1.4 的保留项按实测导入证据登记，未以“入口已改”声称 Python 交付库已退役。

**偏差处理**：

- 等价性无法证明（Bundle 字节、摘要、回执或 promote 合同不一致）：保留
  `verify-release-artifacts.sh` 与 `verify_release_artifacts.py`，登记差异、最小复现与影响范围，
  本批不声明完成；不以放宽校验、改回执文案或重算摘要作为修复。
- 本机无法自持 registry 或无法构建候选：允许以 `REGISTRY=` 显式传入并登记为环境限制，但不得据此
  声称“全新检出、无手工准备”成立；若因此无法取得 I1 回执，本批不声明完成。
- 基础设施失败：按 §4 失败分类做最小复现，最多两次定向诊断；未取得已证明原因前不重复整套构建。
- 触顶（累计 180 分钟或触及 §3 强制停点）：停止新工作，保留候选身份（revision、`VERSION`、镜像
  tag 与摘要）、通过/失败/未开始清单与清理结果，登记接续清单；不为未完成的批次创建完成提交，
  续做需修订的有界方案与用户明确指示。

**接续粒度与失效规则**：真实回执按段恢复（A 段实现与本地候选、B 段安装与 CI 各自独立，段内不提供
更细粒度恢复）。使其失效的改动：`lifecycle/**` 变化 → 本地候选与安装回执失效；`deploy/**` 变化 →
候选 Bundle、runtime contract 与安装回执失效；`VERSION` 变化 → 全部候选身份与镜像 tag 失效；
`.github/workflows/**` 变化 → CI 回执失效；`scripts/verify-compose*.sh` 变化 → CI 运行时门禁回执失效。
新候选不重置累计耗时。

**接续项（登记给 23-05，不属本批）**：

1. `release_artifacts.py`(302)、`release_manifest.py`(95)、`test_release_artifacts.py`(75)、
   `test_release_manifest.py`(47) 的退役：§1.4 的 8 个 Python 消费方与 `test_phase16_evidence.py` 的
   共享夹具必须先迁移或退役；删除时附导入面证明。
2. `release_candidate_env.py`(15) 的退役随 `verify-compose*.sh` 的处置一并决定。
3. Compose 运行时门禁（`verify-compose.sh` 447 + `verify-compose-observability.sh` 795）与
   `verify_product_lifecycle.py`(210)、`verify_reused_install.py`(36) 等验收执行器的原生承接或保留理由。
4. `release_artifacts.py` 的 `build` / `promote` / `product_compose` 在 Go 承接后成为无调用方代码
   （仅被自测覆盖），由 23-05 决定删除或继续保留。
5. arm64 真实运行仍为 CI-only（本机无 binfmt/QEMU），与 23-03 §6 已登记的沙箱限制同类。
6. amd64 元数据回执沿用了 arm64 措辞的状态文案（既有行为，promote 仍按合同拒绝）；若 23-05 统一
   措辞，须同时改两端并在同一候选上重验。
7. 总方案 §5.4 引用的 `rabbish/PLAN-closeout-2026-10-09.md` 在当前工作树与主远端 main 均不存在
   （实测 `git cat-file -e origin/main:rabbish/PLAN-closeout-2026-10-09.md` 失败），其登记的
   Phase 16/17 退役清单（13 文件 / 1,063 行）需在 23-05 重新取证后再执行。

## 7. 与其他文件的关系

- 复用 23-01 建立的组件 Makefile 调度与模块清单单一来源（本批不新增模块）；复用 23-02 的
  `make build` / `make build-images` 语义与 `componentmetrics` 已发布依赖，不重复实现构建入口；
  复用 23-03 的 `devtools` 仅限本机环境编排，交付链不依赖它。
- `release-candidate.yml` 的调用方式在本批改变，但候选身份、Bundle 合同、回执与 promote 语义不变；
  `deploy/` 下正式部署合同（`compose.yaml`、`runtime-contracts.schema.json`）不改，只同步
  `runtime-contracts.json` 的产品版本字段。
- 不修改 `dev/design/` 两份冻结设计与落地大纲；不重建已退役的 Phase 矩阵；不重写历史证据。

## 8. 决策记录（2026-10-09）

| 决策 | 结论与理由 |
| --- | --- |
| 承接深度 | 交付**入口**原生承接：`make package` 的构建、Bundle/manifest/摘要、校验与 promote 由 Go 实现。用户 2026-10-09 选择本方案，真实验收只做本机 amd64 候选 + `lifecycle` 隔离安装 |
| 实现位置 | 交付命令放在既有 `lifecycle` 模块（`cmd/gopulse-package` + `internal/packaging`），不新建模块。理由：清单合同已在 `lifecycle/internal/release` 实现且为 `internal`，同模块才能复用而不复制第二份合同；`lifecycle` job 已覆盖 check/test/race，无需新增模块、CI job 与 `quality_scope` 条目 |
| registry 归属 | 由工具自持（固定摘要的 `registry:2`、唯一命名、只删自有容器），`--registry` 可覆盖。理由：总方案 §4 判据 1 要求“全新检出、无手工准备”；CI 的独立 registry 两步随之删除，钉死 digest 原样保留在 Go 常量中 |
| promote 与运行时门禁 | 不改回执合同：amd64 仍需运行时+闭包回执才允许 promote，`PROMOTE=1` 缺回执时明确拒绝。因此本机默认 `make package` 只产出候选，双平台候选与 promote 由 CI 上的同一目标完成（本机无 arm64 模拟；固定闭包按设计占用 60 分钟 CI 预算） |
| 不删除共享 Python | `release_artifacts.py` / `release_manifest.py` / `release_candidate_env.py` 被 8 个仍然有效的验收执行器与固定闭包导入（§1.4 实测），本批只退役交付校验入口本身；删除需要先迁移那些消费方，属 23-05 |
| 双实现一致性 | Go 与保留的 Python 在别名表、回执文案、摘要语义上必须一致：Go 侧按同一表与同一文案写用例，真实候选由 Python 侧消费方（固定闭包与 `lifecycle` 安装）在同一制品上验证，避免两份实现背离 |
| 版本元数据 | `deploy/runtime-contracts.json` 的 `product_version` 必须与 manifest 版本相等（`CheckAssets`），本批显式同步为 `2.5.4`；`sync_version_metadata.py` 不覆盖它，故列入 §1.2 允许修改范围 |
