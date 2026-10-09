# Phase-23-04：交付入口原生承接与隔离安装

> 实际状态：已完成。P1–P5、I1、D1–D3、D4、D5 全部通过。
> 目标版本 `2.5.4`，分支 `develop/2.5.4`，基线 `origin/main` = `99ad935`。
> 实现提交：`319c078`（原生交付入口）、`e3ca8d1`（compose 别名修复）、`e689c8a` 与 `e252f16`（CI builder）。
> 本机候选身份：revision `e3ca8d1`，manifest `sha256:4a5c8f9f…`，Bundle `sha256:a1a5f797…`。
> CI 候选身份：revision `e252f16`，manifest `sha256:5e6cb5c7…`，双平台回执同 revision。

## 实际完成

### A 段：`make package` 由 Go 原生承接（0 处 Python）

- 新增 `lifecycle/internal/packaging`（10 个实现文件 + 6 个测试文件）：常量与仓库身份
  （`packaging.go`）、镜像记录与 index/平台摘要校验（`image.go`）、Compose 变换与别名闭包
  （`compose.go`）、Bundle 归档与校验（`bundle.go`）、候选构建（`build.go`）、校验与运行时回执
  （`verify.go`）、promote（`promote.go`）、自有 registry（`registry.go`）、候选环境输出（`env.go`）。
- 新增 `lifecycle/cmd/gopulse-package`（`run|build|verify|promote|env`），`lifecycle/Makefile` 增
  `package` 目标；根 `Makefile` 的 `package` 目标体改为单行调度
  （`PACKAGE := $(MAKE) --no-print-directory -C lifecycle package && ./lifecycle/bin/gopulse-package`）。
- 候选合同保持不变：tag `<registry>/<name>:<version>-candidate-<revision[:12]>`、`--provenance=false`、
  `git archive` 上下文、无 index 时 `imagetools create --prefer-index=true`；Bundle 允许集、USTAR
  成员属性（0644 / uid=gid=0 / mtime 0 / LF）、`gopulse-<version>-bundle.tar.gz{,.sha256}`、
  `checksums`、`deploy/runtime-contracts.{json,schema.json}` 与回执字段
  （`manifest_sha256`、`revision`、`platform`、`status`）逐字段沿用。
- registry 归属改为工具自持：固定摘要的 `registry:2`、唯一容器名、随机可用端口，成功与失败路径都只
  删除自有容器；`--registry` 仍可交由调用方自持。
- `release-candidate.yml` 的 registry 两步与 4 条 Python/Shell 命令替换为
  `make package PLATFORM=linux/amd64,linux/arm64 RUNTIME=1 PROMOTE=1`；`python3` 引用为 0。
- 退役交付校验入口 `scripts/ci/verify_release_artifacts.py`（54 行）与
  `scripts/verify-release-artifacts.sh`（7 行）；`scripts/ci/test_release_snapshot.py` 中已迁移到 Go 的
  `test_runtime_gate_rejects_logged_error_with_zero_exit` 一并删除（83 条用例保持全绿）。
  共享的 `release_artifacts.py` / `release_manifest.py` / `release_candidate_env.py` 按计划保留：
  8 个仍然有效的验收执行器仍在导入（§1.4 实测），删除属 23-05。
- 文档同步：`deploy/release/README.md`（原生命令、退出码、实现位置）、`README.md` 命令表、
  `dev/validation/Phase-16/phase16-linux-matrix.md` 增加本批入口说明。

### B 段：隔离安装与真实验收

- `python3 scripts/ci/verify_product_lifecycle.py --clean-install --platform linux/amd64` 在**全新
  TMPDIR**、真实 registry 与真实候选上通过：14 条命令退出码全部等于矩阵期望
  （`doctor`0、`init`0、`init`14、`verify`19、`up`0、`verify`0、`status`0、`logs`0、`logs`2、
  `down`0、`down`0、`up`0、`verify`0、`down`0），回执 `status=passed`、`cleanup_passed=true`、
  `isolation_preserved=true`，无密钥泄漏断言通过。
- 首次安装失败暴露既有缺陷：`lifecycle/internal/control` 的 compose 别名表只有 7 条，缺
  `business-worker-2` / `search-indexer-2` / `router-2` / `marshaller-2` /
  `observability-elasticsearch`，而这些副本服务自 Phase 18（`ec481f1`）起就在产品 Compose 中，
  安装会在 `doctor` 阶段以 `service image missing from manifest` 退出 10。修复为与交付侧别名表
  对齐的 12 条表（含 `edge`），并新增回归用例
  `TestComposeAliasesCoverDeliveryAliasesAndEdge`（逐条比对交付侧表，防止再次漂移）。

## 实际命令与结果

### P1 入口正确且不含 Python

| 命令 | 结果 |
| --- | --- |
| `make help` | 第 12 行 `make package [PLATFORM=] [OUTPUT=] [REGISTRY=] [RUNTIME=1] [PROMOTE=1]` |
| `make -n package` | `make --no-print-directory -C lifecycle package && ./lifecycle/bin/gopulse-package run --output dist --platform linux/amd64` |
| `grep -rn python Makefile lifecycle/Makefile lifecycle/cmd/gopulse-package lifecycle/internal/packaging` | 0 处 |
| `grep -c python3 .github/workflows/release-candidate.yml` | `0` |
| 根 `Makefile` 的 `package` recipe | 1 行 |

### P2 单元与静态检查（最终 revision `e3ca8d1`）

| 命令 | 结果 |
| --- | --- |
| `make test MODULE=lifecycle` | `ok` control / packaging / release |
| `make race MODULE=lifecycle` | `ok` control 6.09s / packaging 1.07s / release 1.02s |
| `make check MODULE=lifecycle` | `go vet ./...` 无输出；`gofmt -l lifecycle` 为空 |
| `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` | `Ran 83 tests … OK`（较批次前少 1 条，即已迁移到 Go 的运行时门禁用例） |
| `go test ./...`（`lifecycle` 模块） | backup / control / packaging / release 全部 `ok` |

新增 Go 用例：别名闭包与未映射服务、11 条交付别名表逐项断言、Bundle 往返确定性与篡改检测、
Compose 运行时门禁（通过 / 退出 0 但含 `[gopulse-compose] ERROR:` / 退出 3）、插件归档身份与架构、
index 与平台摘要、`releaseTarget` 末冒号回归、compose 别名覆盖交付表。

### P3 本机真实候选与 Bundle 合同

| 命令 | 结果 |
| --- | --- |
| `make package`（自持 registry，revision `e3ca8d1`） | 退出 0；产出 `dist/release-manifest.json`、`dist/gopulse-2.5.4-bundle.tar.gz{,.sha256}`、`dist/checksums`、`dist/verification-amd64.json`、各镜像构建元数据与 `verified-plugins/` |
| 再次 `make package` | 退出 2：`refusing to overwrite complete candidate`，且 manifest 摘要不变（`2a6f60d188ded64c…`） |
| `release_artifacts.verify_bundle(Path('dist/release-manifest.json'))`（保留的 Python 独立实现） | 通过：版本 2.5.4、revision 与 HEAD 相等、9 产品 / 6 第三方 / 7 插件、允许集与成员属性一致 |
| `tar -tzvf dist/gopulse-2.5.4-bundle.tar.gz` | 7 个成员，均 `-rw-r--r-- 0/0`、时间戳为 mtime 0 |
| `cat dist/checksums`、detached `.sha256` | 6 项资产摘要 + Bundle 摘要；detached 摘要与 `sha256sum` 实测一致（`31ac3bfc66ad4ab1…`） |

### P4 元数据与插件真实校验

- 构建内 `verify` 步骤：9 个产品 + lifecycle + 6 个第三方全部 `PASS … metadata`（含
  `docker.elastic.co` 第三方），arm64 记 `metadata-only; real arm64 runtime DEFERRED to Phase-16-06`。
- `dist/verified-plugins/amd64/`：6 个 current 插件 + `redis-1.9.4` upgrade-only 归档；ELF 架构、
  entrypoint 与 schema 摘要与 manifest 记录逐字段相等（用例与真实构建同源校验）。

### P5 失败注入

| 注入 | 结果 |
| --- | --- |
| (a) 脏源码树 | 退出 2：`candidate builds require a committed source tree`；自有 registry 已删除、无残留 |
| (b) 输出目录已有完整候选 | 退出 2：`refusing to overwrite complete candidate`，既有候选未被改动 |
| (c) 非法 registry 命名空间（`Bad_Name/../x`） | 退出 2：`invalid registry namespace` |
| (d) 篡改 Bundle 任一字节 | 真实制品：`gopulse-package verify` 退出 1 `bundle asset checksum mismatch`；单元层另有篡改用例 |
| (e) registry 不可达（`--registry 127.0.0.1:9/gopulse`） | 退出 1：`failed to push … connection refused`，不产出完整 manifest |
| `PROMOTE=1` 而缺少运行时回执 | `gopulse-package promote` 退出 1：`candidate lacks matching successful verification receipts` |

### I1 隔离安装、启动与卸载

命令（`TMPDIR` 与 socket 的环境适配见偏差 §6、§7）：

```bash
TMPDIR=<workspace>/.run/tmp GOPULSE_DOCKER_SOCKET=<workspace>/.run/socks/docker.sock \
python3 scripts/ci/verify_product_lifecycle.py --clean-install \
  --platform linux/amd64 --manifest dist/release-manifest.json
```

结果：退出 0，回执 `status=passed`、`platform=linux/amd64`、`mode=clean-install`、
`manifest_sha256=4a5c8f9ff6b20c27…`、`cleanup_passed=true`、`isolation_preserved=true`；
`started_at 14:46:25Z → finished_at 14:50:09Z`；命令退出码序列见 B 段；`down --purge` 后本次安装的
容器/网络/卷全部消失，未影响无关资源。

### D1–D3 版本、文档与治理

| 命令 | 结果 |
| --- | --- |
| `python3 scripts/ci/validate_versions.py` | `Version metadata matches root VERSION.`（`VERSION`、`.env.example`、4 个前端包文件、`deploy/runtime-contracts.json` 均为 `2.5.4`） |
| `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.5.4` | 通过，`processes: 13` |
| `python3 -m unittest discover -s scripts/ci -p 'test_runtime_contracts.py'` | `Ran 2 tests … OK` |
| `python3 scripts/ci/validate_branch.py --branch develop/2.5.4 --base-ref origin/main --mode development` | `Branch governance passed for develop/2.5.4.` |
| `python3 scripts/ci/quality_scope.py --changed-file lifecycle/internal/packaging/bundle/bundle.go --format json` | 选中 `lifecycle` |
| `python3 scripts/ci/quality_scope.py --changed-file deploy/runtime-contracts.json --format json` | 选中 `compose` + `tools` |
| 改动过的 YAML 解析 | `release-candidate.yml` 与 `deploy/compose.yaml` 均可解析 |
| `grep -rn "verify-release-artifacts\|verify_release_artifacts" scripts Makefile lifecycle .github deploy` | 0 处活引用；其余 15 处仅在 `dev/imple`（8）、`dev/logs`（6）、`dev/validation`（1）的计划与历史文档中 |

## 偏差

1. **别名表数量**：分方案 §4.1 写“12 条别名”，交付侧 `COMPOSE_IMAGE_ALIASES` 实测 11 条；真实验收
   证明两张表并集才是 12 条：交付侧 11 条 + 控制器侧独有 `edge`。控制器侧原本仅 7 条，缺 5 条副本
   服务别名，导致隔离安装必然失败——已按“两张表并集”修复（`e3ca8d1`），超出分方案 §1 的文件清单，
   属验收暴露的既有缺陷最小修复。
2. **Bundle 允许集不符的退出码**：分方案把它归入参数类（退出 2）；实现按既有 Python 语义返回
   验证/提升失败（退出 1）。真实注入 (d) 实测退出 1。
3. **JSON 字节一致性**：Go 写出的 manifest 键顺序与非 ASCII 转义与 Python 不同，未主张字节相等；
   语义等价由保留的 Python `verify_bundle` 与固定闭包在同一制品上验证。
4. **首次候选作废**：`make package` 第 1 次运行在第 3 方 registry 拉取 TLS 超时（`docker.elastic.co`
   handshake timeout，随后手工复测退出 0）后失败，已删除其 `dist/` 并在同 revision 重跑（第 2 次
   退出 0）；第 1 次的 registry 已由工具自行删除，无法再校验，未复用其任何回执。
5. **`VERSION` 位置**：候选必须由已提交源码树构建，故 `2.5.4` 的版本同步位于实现提交 `319c078` 内，
   完成提交不再改动 `VERSION`。
6. **沙箱 TMPDIR**：本机 user namespace 下 Python 的默认 `TMPDIR=/tmp` 会落进验收工具自身 Compose
   服务的 `tmpfs: /tmp` 内，安装目录的绑定挂载被遮蔽（`doctor` 退出 14）；`TMPDIR` 指到工作区后
   `doctor` 通过。Bundle 的 `tmpfs: /tmp` 与 Python 原实现逐字一致，非本批回归。
7. **Docker socket gid**：沙箱把宿主 `/var/run/docker.sock` 的 gid 989 显示为未映射的 65534，验收工具
   据此给容器 `--group-add 65534`，容器内连接被拒（`doctor` 退出 11）。改用工作区内 world-accessible
   的 socket 转发并显式 `GOPULSE_DOCKER_SOCKET=` 后通过；属环境适配，产品与 Bundle 未改。
8. **CI 多平台 builder**：`release-candidate.yml` 增加 `docker/setup-buildx-action@v3`（`e689c8a`）。
   首次 CI 候选 job 失败于 `Multi-platform build is not supported for the docker driver`；退役的
   Python 链路使用同样的 `docker buildx build --platform`，属既有基础设施缺口，非本批回归。
9. **候选 revision 与分支 HEAD**：本机候选与 I1 回执绑定 `e3ca8d1`；其后仅 `.github/workflows/**`
   变化（`e689c8a`），按分方案“刷新粒度与失效规则”只失效 CI 回执，本机候选与安装回执保持有效。

## 已知限制与后续项

1. arm64 真实运行仍为 CI-only（本机无 binfmt/QEMU，实测 `/proc/sys/fs/binfmt_misc` 为空），双平台
   候选与 promote 只能在 CI 上完成；与 23-03 §6 登记的沙箱限制同类。
2. amd64 元数据回执沿用 arm64 措辞的 `status` 文案（既有行为，promote 仍按合同拒绝）；统一措辞需
   两端同时改并在同一候选重验（23-05）。
3. `release_artifacts.py` / `release_manifest.py` / `release_candidate_env.py` 仍被 8 个验收执行器
   导入，退役需先迁移消费方（23-05）。
4. `dev/validation/Phase-16/phase16-linux-matrix.md` 第 7、16 行仍保留历史
   `verify-release-artifacts.sh` 命令（历史记录，不重写）。
5. 总方案 §5.4 引用的 `rabbish/PLAN-closeout-2026-10-09.md` 在当前工作树与 `origin/main` 均不存在，
   Phase 16/17 退役清单需在 23-05 重新取证。
6. 验收工具在本机需要 `TMPDIR` 位于容器 `tmpfs /tmp` 之外，并要求 Docker socket 对目标 gid 可读；
   后续在本机重跑 I1 时应沿用这两个适配。
7. 本机候选的镜像位于自持 registry（`127.0.0.1:15031`）；该容器随本次会话清理后，重跑 I1 需重新
   构建候选（段 A/B 可独立恢复）。

## 执行预算

- 分方案预算：期望 140 分钟（A 110 + B 60），累计上限 180 分钟。
- 实际（13:57Z → 15:32Z）：约 95 分钟，未触顶。构成本次耗时的关键项：4 次本机 `make package`、
  3 次诊断（TMPDIR 遮蔽、socket gid 未映射、控制器别名表缺失，各一次最小复现）、
  1 次真实隔离安装、3 次 CI 候选 job（前两次为 CI builder 缺口与网络可见性，未重启本机构建）。
  未使用“放宽门禁”或“重跑整套本地构建”路径。

## 分支 CI

- `Auto PR and Merge`（`develop/2.5.4`，run `37948365686`，HEAD `e252f16`）：**全部质量门禁 job 成功**——
  `Branch governance`、`Lifecycle installer`、`Full-stack Compose acceptance`、`Scripts and Compose`、
  `Native observability integration`、`Integration`、`Backend`、`Frontend`、`Admin frontend`、
  `Monitor`、`Marshaller`、`Message Router`、`Component metrics`、各 exporter 与 `Local environment
  helper`、`Load test tool`、两个浏览器 job 均实际执行；`Open PR and merge` 按预期 skipped（当时尚未
  提交同名日志，完成校验未就绪）。
- `Immutable release candidate`（run `37948399213`，HEAD `e252f16`）：**候选 job 绿**，下载
  `local-candidate-evidence` 核对：manifest `version=2.5.4`、`revision=e252f1619b74…`，产品 9 个镜像与
  lifecycle 均为 `linux/amd64` + `linux/arm64` 双平台，6 个第三方、13 条插件记录；
  `verification-amd64.json` = `amd64-runtime-and-compose-passed`、
  `verification-arm64.json` = `metadata-only; real arm64 runtime DEFERRED to Phase-16-06`，两者
  `manifest_sha256` 相同；日志含 `Same-digest promotion verified; bundle checksum unchanged.` 与自有
  registry 删除行（`gopulse-package-registry-eca8d825`）。
- 前置失败与修复（同属本批 CI 入口）：run `37947267012` 失败于 `Multi-platform build is not supported
  for the docker driver`（runner 默认 driver），run `37947538563` 失败于容器 driver builder 在默认 bridge
  网络内无法访问回环端口上的自有 registry；已在 `release-candidate.yml` 增加
  `docker/setup-buildx-action@v3`（`driver-opts: network=host`）后通过。
