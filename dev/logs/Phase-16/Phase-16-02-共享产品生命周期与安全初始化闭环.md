# Phase-16-02 实施记录

## 状态

目标 `1.13.2`，分支 `develop/1.13.2`。**本批次已完成验收**。最终候选与真实门禁结果见下文；前面的实施中检查和失败轮次作为历史记录保留。提前提交实现快照是现有 release builder 的 committed-source 输入要求，不曾被用来代替验收。

## 已实施

- 单一 lifecycle Go module 新增 doctor/init/up/down/status/logs/verify JSON schema 1 控制面。
- 显式 Unix Docker endpoint、Linux amd64 preflight、Docker >=24 / Compose >=2.24、资源检查、Bundle checksum / digest 检查。
- 0700 安装目录、0600 Secret/state、同目录 sync+rename、安装级 flock、随机 operation / installation token；重复 init 拒绝覆盖。
- 候选 Bundle 独立 edge、project-scoped volume/network、manifest/installation/service 标签；变更和日志操作校验归属，默认保留数据。
- 顺序基础设施/初始化 job/常驻服务；严格只读 verify、allowlist logs 与 Secret 脱敏；SIGINT/SIGTERM 终止整个 CLI process group 并保留诊断状态。
- 产品文档使用同一工具容器；未扩展 PowerShell；现有开发 Compose 保持不变。

## 已运行检查

- `(cd lifecycle && go test ./...)`：通过（实施中最小包检查）。
- `scripts/test-lifecycle.sh`：通过，包含 Go module 测试和 Compose Bash self-test。
- `scripts/verify-release-artifacts.sh --self-test`：5 项通过。
- `python3 -m py_compile scripts/ci/verify_product_lifecycle.py`：通过。

## 环境与边界

- Linux amd64，4 CPU，内存约 11 GiB，安装文件系统空闲约 53 GiB。
- Docker Engine 29.7.2、Compose v5.5.0。
- 原 Phase-16-01 loopback registry 已启动用于读取历史候选，不是正式发布。
- 用户未跟踪文件 `~` 和其他既有 Docker 项目不纳入提交/清理。
- 当前 release schema 保留 Phase-16-01 两架构元数据合同；本批产品运行验收只针对 Linux amd64，不宣称 arm64 产品支持。

## 首个候选提交时的待验证项（已在下文闭环）

同 revision 候选构建、真实 clean-install / failure matrix、release runtime / 直接 Compose 回归、PowerShell hash 对比和最终结果记录。

## 实施中的失败与调整

- 首轮候选 `c3b7f706fce1` 构建成功，输出 `dist/phase16-02/`；首次 clean-install 在 doctor 的 VictoriaMetrics digest 检查失败，尚未创建产品资源。
- 原因：`docker manifest inspect` 在工具客户端直接访问 Docker Hub，没有复用 daemon 的 registry mirror，实际返回 registry EOF。使用同一 endpoint 的 Engine distribution API 验证成功，因此改为 daemon-side 结构化 descriptor/platform 查询；不退回 tag、不跳过 digest、不传播 raw registry 诊断。
- 新增直接对应验收的错误 server arch、失败 pull 非 ready 状态测试；failure matrix 增加 Bundle 篡改和真实低容量 tmpfs。

- Engine distribution API 的实际单平台响应 `Platforms=null`，仅 index 提供平台列表；据此保留 index 平台校验并按 descriptor digest 验证 child，增加真实 Unix HTTP 边界回归。
- `ceff812` 候选已真实完成 init/up，全部常驻容器健康；验证 runner 的业务快照错误引用不存在的 `user_roles` 表，改为实际 `bootstrap_super_admin`。该轮未被记为 clean-install 通过；finally 的强归属清理完成。
- Bundle 新增根 `compose.yaml` 作为正式工具入口，纳入 payload / archive checksums；clean-install 改为直接通过交付 Bundle 的 Compose 调用，failure matrix 保留同镜像 Docker run 用于隔离故障注入。旧 Phase-16-01 Bundle 校验保留兼容。

## 配置与运行合同

- Bundle 根 `compose.yaml` 仅定义短命 lifecycle 服务，按 Linux amd64 platform digest 选择工具，使用 host network（本地 loopback registry / 端口检查）、read-only rootfs、`cap_drop: ALL`、no-new-privileges 和 `/tmp` tmpfs。
- 必需的宿主数值 UID/GID、socket GID、Bundle / installation 绝对路径均显式配置；只给工具挂载 endpoint。产品 Compose 不包含宿主 bind mount/socket。
- 私有 `state.json` schema 1 保存 completed target version、manifest digest、project、installation token、edge port、phase、operation id；公开 JSON 不输出 installation token。`secrets.json` 是私有应用配置/随机凭据映射，VictoriaMetrics 的文件 Secret 只在 up 时以 0600 物化。
- phase 顺序为 `initialized → pull → infrastructure → initialize → services → ready`；down 为 `stopping → stopped`；signal 为 `interrupted`。失败保留实际阶段与数据，不把半启动标成 ready，不自动清理可诊断的产品资源。
- exit code：2 参数/确认，10 manifest/checksum/digest，11 daemon/Compose/version，12 platform，13 capacity，14 private directory/state，15 port，16 lock，17 ownership，18 execution，19 not-ready，20 signal。
- 每次变更均获取 flock 并记录独立 operation id。容器校验 project + installation + service + manifest + selected image digest；network/volume 校验 project + installation + resource + manifest，并检查无 project 标签的同名资源碰撞。
- 默认 down 保留全部持久 volume；`--purge --confirm PROJECT` 才删除强归属 volume，安装文件仍保留。拒绝外来资源时不采用宽泛匹配进行清理。
- verify 仅执行结构化 server/version、project list/inspect；不调用 Compose up/down，不生成配置/Secret，不触及业务写接口。logs 服务名固定来自 Bundle，tail 限制 1..1000，时间为 RFC3339，脱敏 Secret 与 installation token。
- 首次注册保持普通用户；不创建默认管理密码、不自动提升首次注册用户。原有 super-admin 显式 bootstrap 业务语义不变，双 Frontend / 统一登录的完整产品交接继续由 Phase-16-03 执行。

## 第 4 轮候选身份与已通过的产品门禁（历史）

候选目录：`dist/phase16-02-v4/`；证据/原始命令输出：`.run/phase16-02/`。该候选由同一 Git archive revision `4832c0ee489f4f061e556cec703bc418f0e95179` 构建全部十个镜像，产品版本 `1.13.2`。

- Manifest SHA256：`b930a86159ed9dd3df82b3dfc6bd36eed61323af61ba3c00fe361a3c2c982e05`
- Bundle payload：`sha256:c90e4dffceef4f4deb9acc966747671fe8139a111547cbcab13988b754a949d0`
- Bundle archive：`42a7bc963d827b2f2792c6f59e4fc451c328bd30499d5bd4bc943624fc09cf70`
- Lifecycle Linux amd64 digest：`sha256:eac911c318c6bb6ddd5e9af58392c8e70568fc6db3ff47a5c73400b2eca59532`
- Lifecycle index：`sha256:4165d6f7975909e64cec19f069be6043d46b955b9c3489acecff548ac96fa777`

已实际通过：

1. `scripts/test-lifecycle.sh`：Go module + Bash Compose self-test，见 `test-lifecycle-final.txt`。根仓库没有 Go module，计划中的 `go test ./lifecycle/...` 等价执行为 `(cd lifecycle && go test ./...)`，未新增无用 go.work。
2. `scripts/verify-release-artifacts.sh --self-test`：5 项通过，见 `release-selftest-final.txt`。
3. `scripts/verify-product-lifecycle.sh --manifest dist/phase16-02-v4/release-manifest.json --platform linux/amd64 --clean-install`：通过，见 `clean-install-v4.txt`。使用带空格的独立临时安装路径，通过 Bundle 的 Compose 工具入口完成 doctor/init/up/verify/status/logs/down/down/up/verify；应用及管理页面、后端 health 真实 HTTP 成功。verify 前后容器 ID/image/status/start time/restart count、users/posts/bootstrap 计数、state/Secret 字节完全相同；全产品健康和唯一 edge 检查通过。最终显式 purge 的强归属清理完成。
4. `scripts/verify-product-lifecycle.sh --manifest dist/phase16-02-v4/release-manifest.json --platform linux/amd64 --failure-matrix`：通过，见 `failure-matrix-v4.txt`。覆盖重复 init 不换 Secret、未启动 verify=19、flock 并发=16、daemon 缺失=11、篡改 Bundle=10、实际 1 MiB tmpfs 容量=13、占用端口=15、非私有目录=14、错误 purge 确认=2、无 project 标签的同名外来 volume=17 且不被删除、SIGINT/SIGTERM=20 且 phase=interrupted、重复 down。所有采集输出均检查不包含安装凭据/token。
5. `sha256sum -c /tmp/gopulse-p1602-ps1.sha256`：全部通过，见 `powershell-hashes.txt`。哈希基线在实现前采集。
6. Python runner / builder `py_compile` 和 `git diff --check`：通过。

上述已成功验证不因随后补写日志而重跑。release runtime / 完整 Compose 结果见最终回归结果。

## 最终回归的隔离调整

首次 release runtime 门禁已通过本候选全部镜像 metadata、插件 catalog 和 lifecycle version 运行检查，但其内嵌 Compose runner 在资源创建前把用户原有未跟踪文件 `~` 识别为 dirty runtime source，整体返回失败，未生成通过 receipt。没有移动、删除、忽略或提交该用户文件。

按现有发布文档建立 revision `4832c0ee489f4f061e556cec703bc418f0e95179` 的独立干净 detached worktree，在其中重跑这个失败的固定门禁，仍绑定 `dist/phase16-02-v4/release-manifest.json` 的同一候选。仅改变验收源码路径，不修改产品，不重跑已成功的 clean-install / failure matrix / Go / Bash self-test。该隔离路径保存在 `.run/phase16-02/acceptance-worktree.txt`，完整输出写入 `release-runtime-clean-worktree.txt`。

## 实际变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `admin-frontend/package-lock.json`
- `admin-frontend/package.json`
- `deploy/docker/lifecycle.Dockerfile`
- `deploy/release/BUNDLE-README.md`
- `deploy/release/README.md`
- `dev/logs/Phase-16/Phase-16-02-共享产品生命周期与安全初始化闭环.md`
- `frontend/e2e/compose-observability.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `lifecycle/cmd/gopulse/main.go`
- `lifecycle/internal/control/compose.go`
- `lifecycle/internal/control/control.go`
- `lifecycle/internal/control/control_test.go`
- `lifecycle/internal/control/state.go`
- `scripts/ci/release_artifacts.py`
- `scripts/ci/verify_product_lifecycle.py`
- `scripts/test-lifecycle.sh`
- `scripts/verify-product-lifecycle.sh`


干净 worktree 的完整回归在 `reset_for_management` 场景遇到阻断：关闭自动 bootstrap 后，通过 UI 安装/停止/启动 Redis 插件成功，但 Metrics 页面 `.metric-value` 在 45 秒内仍为 0。此前 cold start、业务、权限、故障隔离、服务替换、signal、保留卷恢复等已运行的检查不被重跑来定位此失败。仅抽取同一 runner 的准备代码及失败的 manage 场景到临时诊断入口，继续绑定同一候选、同一干净源码，并保留本次专属项目和浏览器失败工件。诊断入口不作为验收替代、不进入产品；定位只覆盖这个实际失败的指标路径。

同一候选、未改源码/断言的独立 fresh manage 场景在 39.5 秒通过，输出 `manage-diagnostic.txt`；运行期间采样的 Router/Marshaller 未报告传输失败，记录正常消费提交。该 45 秒超时未在最小复现中稳定出现，不能据此宣称已发现或修复某个业务缺陷。未延长原超时、未跳过 Metrics/Events 断言，也未修改无关应用代码。诊断专属项目 `gopulse-accept-f13392fb523f` 经 project/working_dir/config-file 标签校验后已清理，随后重跑此前失败的固定 release runtime 门禁；已成功的 lifecycle 门禁仍不重跑。

有限前置资料补充：VictoriaMetrics 官方 Key concepts / Query latency 说明 `search.latencyOffset` 默认 30 秒且同时影响 query/query_range（`https://docs.victoriametrics.com/victoriametrics/keyconcepts/`）。当前 Compose 未覆盖该参数，Monitor 默认 scrape interval 为 15 秒，Backend 的 15m 查询步长也为 15 秒。结合独立场景 39.5 秒通过，冷安装首条样本的旧 45 秒测试窗口存在时序边界风险；这只能解释其可能的时间预算不足，不能把未采集到的实际故障时序当成已证实根因。本批没有修改这些产品默认值，也没有检查第三方依赖源码。

第二次完整门禁再次仅在 reset 后的 manage 首条 Metrics 样本 45 秒超时，说明不能把这两次完整场景失败简单当成已消失的偶发问题。已直接读取官方 Query latency 文档核对默认 30 秒 latency offset，并结合本仓库 15 秒 scrape interval、15 秒查询网格确认旧冷启动等待预算不足以覆盖这些阶段。因此仅把 `manage`（禁用 bootstrap、无历史样本）的首条真实指标等待改为有界 90 秒，保留普通/恢复场景的 45 秒、`metric-value > 0`、Events、角色、origin 和所有其他断言，输出实际首条可见耗时；不修改任何应用行为或产品默认配置。这是为本次实测失败所作的最小回归修正，不是泛化扩大超时或绕过检查。

该测试修正需要新的同 revision 候选，最终候选目录将改为 `dist/phase16-02-v5/`。先前生命周期生产行为验证仍有效，但 manifest/image identity 发生变化，故重新执行两个绑定 manifest 的 product-lifecycle 门禁；未受影响的 Go / Bash self-test 和 PowerShell hash 检查不重复。

等待预算的首次文本替换意外匹配到 Logs helper；在审视本次测试 diff 时发现其未定义变量，立即恢复 Logs 原始 45 秒逻辑，并对受影响的单个 Playwright 文件执行 TypeScript no-emit 类型检查通过。第 5 轮候选不作为最终交付；最终候选改为 `dist/phase16-02-v6/`，未改生命周期/应用生产代码。本次不再使用仅 `--list` 作为该测试文件的完整静态检查。

单文件 TypeScript 命令的初次调用被当前编译器以 TS5112 拒绝（显式文件参数需 `--ignoreConfig`）；按该公开错误提示补上此参数后，`tsc --ignoreConfig --noEmit --skipLibCheck --moduleResolution bundler --module esnext --target es2022 --types node e2e/compose-observability.spec.ts` 实际通过，最终输出 `manage-typecheck.txt`。前文所述类型检查通过指这次修正后的真实结果，不把 TS5112 当成通过。

第 5 轮的 release runtime 在切换到修正后源码后被现有 `revision mismatch` 检查阻断，未进入完整 Compose 运行，也未生成成功 receipt；它明确废弃，不与第 6 轮证据混用。第 6 轮源码只修正上述回归 helper，两个绑定新 manifest 的 lifecycle 门禁和原完整 release runtime 门禁按同一候选重新运行。


## 最终候选与固定门禁结果

最终候选目录：`dist/phase16-02-v6/`；全部十个镜像由同一 revision `72451de6423aef8b7e293a856c78080d9a25e961` 的 Git archive 构建，版本 `1.13.2`。

- Manifest SHA256：`9c19b08b4c1e0691e852cd07f41b92e859389fbf6fdc3a6ef74876495ffffe37`
- Bundle payload：`sha256:eb746b3344d3936bed614edf9f73cc82fbcf622c4aa6abc65d1f81ed22d6b5c4`
- Bundle archive：`617703c0419df07593a8da052615f9e0292d51d9071682cd43593c78dc4c3851  gopulse-1.13.2-bundle.tar.gz`
- Lifecycle Linux amd64：`sha256:80d6843f0bc16efe492c4b1e4b7befeacdaf8162ca136dcc470b2b624ced5a09`


最终固定门禁全部通过：

1. Go module、Bash self-test、release self-test、冻结 PowerShell hash 结果见前文；对应源码未改，不重复运行。
2. 修改后的单个 Playwright 文件通过 TypeScript `tsc --ignoreConfig --noEmit --skipLibCheck --moduleResolution bundler --module esnext --target es2022 --types node e2e/compose-observability.spec.ts` 检查，输出 `manage-typecheck.txt`；不把之前仅 --list 的加载检查代替类型检查。
3. `scripts/verify-product-lifecycle.sh --manifest dist/phase16-02-v6/release-manifest.json --platform linux/amd64 --clean-install` 通过，输出 `clean-install-v6.txt`。在新 digest 候选上重新证明 Bundle Compose 完整生命周期、唯一 edge、健康状态、私有 Secret、重复 init/down/up、只读 Docker/业务/state 快照和 allowlist logs。
4. 同一命令的 `--failure-matrix` 通过，输出 `failure-matrix-v6.txt`。覆盖的故障项与前文 v4 矩阵相同，signal 在活动 pull 阶段注入，所有断言绑定本次最终 manifest。
5. 在独立干净 worktree 运行 `scripts/verify-release-artifacts.sh --manifest /home/ray/GoPulse/dist/phase16-02-v6/release-manifest.json --platform linux/amd64 --runtime` 通过，输出 `release-runtime-v6.txt`。其内嵌 `scripts/verify-compose.sh` 已完成计划要求的完整 Compose 回归，不再另行重复。全部镜像 metadata、Monitor 插件 catalog、工具镜像运行、业务/权限/故障/替换/signal/保留卷/管理，以及六插件真实集成均通过。

最终 release receipt 为 `dist/phase16-02-v6/verification-amd64.json`，状态 `amd64-runtime-and-compose-passed`，与两个 lifecycle receipt 绑定同一 manifest/revision。不是首次全部通过；此前失败和修正均保留在本记录中。

最终冷安装首条 Metrics 可见耗时（真实 browser 输出）：44565 ms；仍要求真实数据点和 Events，未改产品默认配置或暖态 45 秒断言。


## 完成状态、清理和下一批交接

- 本批全部验收与固定门禁通过，无未通过的阻断项。根 `VERSION`、`.env.example` 及两个 Frontend package/lock 受管版本均为 `1.13.2`。
- 产品实际运行全程 Linux amd64。为沿用历史 release schema 构建而临时安装的 qemu-aarch64 注册已撤销；它不是产品运行/验收依据，没有引入 macOS、Windows 或 arm64 产品支持。
- clean-install、failure matrix、manage 诊断和完整回归的专属 Docker 产品资源已按归属清理；干净验收 worktree 已删除。Phase-16-01 的 loopback registry 保留，供下一批读取当前候选；无关既有 Docker 项目和用户文件 `~` 保持不动。
- 发布边界：本地不可变候选，不是正式外部 registry 发布。本批未推送分支、创建 PR 或合并 main；没有覆盖根 dist 中保留的旧批次制品。
- Phase-16-03 的明确输入是 `dist/phase16-02-v6/`、Bundle Compose 工具入口、schema 1 private state、同一 origin 的 edge、稳定生命周期与只读 verify；backup/restore 和 1.9.4 upgrade 仍属于后续批次。
- 最终记录提交仅补充真实验收证据，不改变通过验收的产品源码。不因补写记录重跑已通过检查；达到停止条件后停止，不追加无关重构、覆盖率专项或独立 Review 门禁。
