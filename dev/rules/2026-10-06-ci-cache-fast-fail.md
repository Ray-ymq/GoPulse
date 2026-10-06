# CI 缓存与快速失败维护批次

维护编号：CI-2026-10-06。用户明确授权本次直接在 `update` 修改，作为一次性分支例外；
不调整 Phase 分配、产品版本或其他批次合同，不改写仓库常规分支规则。

## 范围与完成条件

- 产品作业依赖现有 governance，通过后仍并行；完整验收和自动合并条件保留。
- 在 `.github/actions/compose-build-cache/action.yml` 与
  `scripts/ci/compose_build_cache.py` 复用 Compose 的 `build --print` 结果，接入每镜像独立的
  Linux/amd64 GHA 层缓存及 Go/npm cache mount 持久化。CI 中共用同一 Buildx builder，
  后续原验收入口复用已经构建的层，仍生成当前 revision 的唯一验收 tag。
- `.github/workflows/cache-warm.yml` 在 main 合并后或手动触发时预热相同构建入口与既有
  Go/npm 缓存；预热不运行验收、不创建 PR、不自动合并。
- `.github/workflows/quality-gates.yml` 接入依赖和缓存动作；现有工具入口与 Dockerfile
  保留。`scripts/ci/test_compose_build_cache.py` 验证缓存缺失回退、错误传播和制品身份。
- 本地固定检查通过、记录实际结果并提交本次文件即完成配置维护。真实 GHA 缓存命中率、
  Linux 完整验收及节省时间需发布后观察，不能用本地静态检查声明这些已验证。

已有能力：Compose 已支持 `build --print`；现有 `verify-compose.sh` 承担权威验收；
setup-go/setup-node 已提供主机依赖缓存。只增加缓存配置适配，不新增验收执行器。

## 预算与门禁

首次实施：2026-10-06 16:12（Asia/Singapore）；前置只读发现未单独计时，属于本次累计
成本，不能因重试、候选变化或 Linux 交接重新计时。
预计活跃时间 90 分钟，累计上限 180 分钟。发现/实现 45 分钟、直接检查 20 分钟、
真实 CLI 配置核对 15 分钟、记录/提交/清理 10 分钟；另留 90 分钟有界诊断余量。
不包含长时产品实验，不缩短原验收窗口。90/144 分钟时报告进度；启动命令前核对剩余预算。
同一未解决原因最多两次、每次 10 分钟最小诊断；到上限保留实际修改和未验证事项，
有界清理后停止，需用户明确要求接续。

固定本地门禁：

1. 新适配器单元测试：原生 Compose 输出的身份/目标保留、缓存缺失冷构建、缓存服务错误
   的一次有界回退、产品构建失败立即传播、临时配置清理。
2. 原拟执行现有工具/治理回归；用户在本次回归尝试结束后明确要求“不需要做 linux
   脚本回归”，因此不再补跑，不以全量回归通过作为本次完成条件。
3. `python3 scripts/ci/validate_versions.py` 与 update 分支范围检查。
4. 真实 Docker CLI 执行 Compose `build --print` 和 Buildx Bake `--print`，验证配置可解析；
   Actionlint 检查全部 workflow 与本地 composite action。
5. `git diff --check`；确认 VERSION、原验收脚本、自动合并入口与用户已有修改未被本次改变。

## 实际执行记录

本文件不作为产品验收证据。

实际修改：两份 workflow、一个共享 composite action、Compose 缓存适配器及七个边界
测试、本维护记录和 Linux 交接文件，共七个文件。产品 VERSION、Dockerfile、原验收脚本
和自动合并入口未改；工作区原有文档修改/删除未纳入本次提交。

已执行检查及结果：

- `python3 -m unittest discover -s scripts/ci -p 'test_compose_build_cache.py' -v`：
  七个测试通过，覆盖冷构建、缓存服务错误的一次回退、编译错误传播、制品身份和清理。
- `python3 scripts/ci/compose_build_cache.py --print --no-cache`：真实 Compose 输出可解析。
  另用真实 Compose/Buildx CLI 对冷配置和含 GHA 参数配置执行 Bake `--print`，十个目标、
  Linux/amd64、版本/revision、缓存参数检查通过；含 GHA 参数的检查只解析配置，没有
  调用真实 GitHub 缓存服务。
- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.11 -shellcheck= -pyflakes=`
  加全部四个 workflow 路径：通过；本地 composite action 同时通过 YAML 解析和字段核对。
  未运行 Shellcheck/Pyflakes。
- `python3 scripts/ci/validate_versions.py`：通过。以本次七个 `--changed-file` 执行
  update 范围验证通过；提交后另对 `upstream/main...HEAD` 检查范围。
- `git diff --check`：通过。

已尝试的全量回归未通过，不能记作成功：

- Mac 系统 Python/Bash 下全量脚本测试：170 个测试，两个失败、三个错误。已观察到
  jsonschema 缺失、Bash 3.2 不支持现有 `[[ -v ]]`、macOS 临时目录路径差异及 `wc` 输出
  差异；在原 HEAD 的临时源码副本复现了 Phase17 路径及业务 self-test 的失败。
- 隔离 `python:3.13-bookworm` Linux/arm64 容器安装 jsonschema 后执行同一测试命令：
  168 个测试，三个错误，均报告缺少 Docker CLI。容器以只读方式挂载源码、未启动业务
  服务，使用 `--rm` 已退出清理。用户随后要求不做 Linux 脚本回归，未继续安装 Docker
  或补跑。未为环境差异改动现有工具。

后续按用户授权提交、推送 update，交接文件说明自动 PR/合并行为和 main 预热观察步骤。
GitHub 真实缓存命中/导出、完整产品验收和耗时收益均未在本次验证。
