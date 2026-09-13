# Phase-16-04：一致备份恢复与诊断闭环开发记录

## 状态

**2026-09-14（本地时间）：整批实现和固定门禁全部通过，版本 `1.13.4`。以下早期部分实施记录按时间保留；最终完成结论以文末为准。**

- 对应计划：`dev/imple/Phase-16/Phase-16-04-一致备份恢复与诊断闭环.md`。
- 批次目标版本/分支：`1.13.4` / `develop/1.13.4`。
- 已执行 `git fetch origin`，从更新后的 `origin/main`（`896c9ba`）创建本分支。
  `origin` 与 `upstream` 均指向 `Ray-ymq/GoPulse` 同一仓库。
- 为修复分支治理门禁，根 `VERSION`、`.env.example` 和两个 Frontend 的受管版本元数据已对齐为 `1.13.4`；这只是目标版本元数据修正，不代表本批验收完成。
- 此次提交是本批的部分实施提交，不是完成提交；不得据此进入 Phase-16-05 验收。
- 已有用户未跟踪文件 `~` 保留，不读取内容、不修改、不纳入提交。

## 前置确认（实际执行）

- `docker info --format '{{.OSType}}/{{.Architecture}}'` 返回 `linux/x86_64`，
  服务端符合 Linux amd64 架构范围。
- `df -h . /var/lib/docker` 当时显示同一 Linux 文件系统约 34 GiB 可用；
  这是环境观察，**不是源/目标数据量、导出量或实际恢复空间验收**。
- 读取了直接影响的 lifecycle command/state/ownership/Compose、Monitor plugin
  storage/runtime/API 边界；当前产品未提供 backup/restore 命令，Monitor 未提供
  本计划所需的逻辑备份 export/import API。
- 官方 MySQL 8.4 mysqldump、Elasticsearch snapshot/restore、VictoriaMetrics
  single-server 文档页面 HTTP 获取成功；**未完成各数据域版本对应的可恢复接口确认，
  未执行数据域导出/导入，不把文档可访问当作前置条件已全部通过**。
- 未启动、停止或删除 Docker 产品资源；无关资源未变动。
- 未读取第三方依赖源码，未派生子代理，未修改冻结 PowerShell 脚本。

## 已实际实现

### 1. 格式与认证边界

新增 `lifecycle/internal/backup/format.go`：

- 格式/schema 1、Linux amd64、版本/release digest/operation/完成标记和时间范围。
- 八个逻辑文件的固定 allowlist，逐项大小和 SHA256；六个数据域的逻辑计数、
  相同 cutover、时间范围、队列 drained、Kafka offsets、插件 catalog digest 声明。
- 标准库 PBKDF2-HMAC-SHA256（600000 次，256-bit key）、AES-256-GCM；
  每个 envelope 独立 32-byte salt，标准库生成 nonce，头部整体作为 AAD。
- Secret 使用独立 `GPSECR01` envelope，外层为 `GPBACK01`，阻止类型互换。
- 认证成功后才解释 archive；全部条目验证通过前不返回任何业务载荷。
- 拒绝非 allowlist 路径、symlink/hardlink、扩展 archive、重复路径、超限声明、
  checksum 错误、未知 JSON 字段、重复键、不一致 cutover、不排空队列、未完成
  archive、缺失结束块和尾随数据。
- 总 archive 上限 256 MiB、manifest 上限 1 MiB；有界内存实现，不宣称大规模备份能力。

### 2. 私有读取与发布原语

新增 `lifecycle/internal/backup/files.go`：

- 口令只从显式私有常规文件读取，16–4096 原始字节，不自动 trim。
- Secret/archive 最终路径拒绝 symlink、非常规文件和 group/other 可读权限；
  非阻塞打开避免 FIFO 阻塞，读取长度有上限。
- 私有目录内密文临时文件、`os.Root` 目录锚定、空间检查、context 取消、fsync、
  原子不覆盖发布及本次临时文件清理。
- 发布函数尚未连接 lifecycle operation lock；调用者须持锁。
- SIGKILL/断电后的残留临时密文清理尚未实现，不宣称满足整批中断恢复合同。

### 3. 离线检查命令

新增 `lifecycle/internal/control/backup_inspect.go`，并在 `control.go` 分派：

```text
gopulse backup-inspect --archive PATH --passphrase-file PATH
```

- 不访问 Docker，不读取/修改安装 state，不创建 ready state。
- 校验外层格式及独立 Secret envelope，输出受限安全 JSON。
- 不输出私有路径、口令、业务内容、计数键或配置。
- 参数退出码 2、Secret source 14、archive/认证错误 21；阶段与恢复建议固定。
- 输出 scope 显式限定为格式认证，**不证明业务数据一致、版本兼容或恢复可用**。
- 未生成包含此命令的新 release Bundle，未实现 doctor 诊断包。

## 本次变更文件

- `lifecycle/internal/backup/format.go`
- `lifecycle/internal/backup/files.go`
- `lifecycle/internal/backup/format_test.go`
- `lifecycle/internal/control/control.go`
- `lifecycle/internal/control/backup_inspect.go`
- `lifecycle/internal/control/backup_inspect_test.go`
- `scripts/test-backup-format.sh`
- `docs/releases/backup-format-v1.md`
- 本记录文件

## 实际验证结果

开发中先运行最小包检查，格式测试第一轮通过。随后因加入内外 envelope 类型隔离、
离线 CLI 和超限声明测试，针对最终代码执行以下检查；未扩展为通用审计或覆盖率活动。

| 命令 | 实际结果 |
| --- | --- |
| `(cd lifecycle && go test ./...)` | 通过：backup、control；release 使用有效缓存；cmd 无测试 |
| `scripts/test-backup-format.sh` | 通过：格式层及 offline inspect 直接测试 |
| `bash -n scripts/test-backup-format.sh` | 通过 |
| `git diff --check` | 通过 |

仓库根没有受管 go.work/go.mod，因此在 lifecycle 模块目录执行 `go test ./...`，
而不是宣称根目录 `go test ./lifecycle/...` 已成功。

最终输出保存于本地忽略目录：

- `dist/phase16-04-format/evidence/lifecycle-tests.log`
- `dist/phase16-04-format/evidence/backup-format-tests.log`

这些是源码测试输出，不是绑定产品候选 manifest 的真实 Compose 恢复 receipt。

### 固定产品门禁尚未执行

- `scripts/verify-backup-restore.sh --platform linux/amd64 --same-arch`：脚本尚未实现。
- `scripts/verify-backup-restore.sh --platform linux/amd64 --failure-matrix`：脚本尚未实现。
- `scripts/verify-product-lifecycle.sh --platform linux/amd64 --reuse-install`：未执行。
- `scripts/verify-compose.sh`：未执行。

没有以 mock、历史 Bundle 或格式测试替代以上门禁，也没有把未执行记作通过。
当前没有真实 cutover、权威计数/digest、恢复事实、新写入或失败矩阵证据。

## 与计划的差距及下一步（全部是本批必需项）

1. 进入维护窗口，关闭写入口，排空 Worker/Indexer/告警/采集异步工作，持有 operation lock，
   捕获权威 cutover、offset、计数与时间范围；失败/中断要恢复源产品服务状态。
2. 核对并实现 MySQL/ES/VM 官方支持的逻辑导出/恢复和 RabbitMQ/Kafka 拓扑恢复。
   格式层只验证元数据声明，不能自行证明数据域的声明为真。
3. 实现 Monitor portable export/import、配置/Secret 分类、目标/授权引用和历史摘要，
   从当前受信 Linux amd64 catalog 重新物化插件并验证 Schema/entrypoint/digest。
4. 实现真正的 `backup` / `restore` 命令，目标空 project 与 installation token 归属检查、
   创建资源前的兼容/空间/状态校验、恢复顺序和只清理本 operation 资源的失败处理。
5. 角色/会话失效、业务/搜索/指标/告警/审计/插件历史事实核对、双 Frontend、六插件及新写入。
6. 统一 doctor/backup/restore 脱敏诊断路径、诊断包及真实空间不足/导入失败/运行中中断矩阵。
7. 实现并运行真实 Linux amd64 固定产品门禁，生成候选绑定证据；全部通过后才能同步
   VERSION/受管版本为 `1.13.4`，补齐记录并创建批次完成提交。

这些剩余工作不是非阻断优化，不能移交下一批后宣称本批完成。当前记录不把它们
伪装为环境故障，也不认定计划不可执行；它们是尚未完成的实现与验收工作。

## PR 门禁修正（2026-09-13）

远端运行 `34752318947` 的失败 job 为 `Branch governance`，唯一失败步骤是 `Test governance rules`。本地复现的失败为：`test_dev_no_build_rejects_same_version_stale_revision_before_up` 预期验证陈旧镜像 revision，但 `scripts/dev.sh` 先因当前分支 `develop/1.13.4` 与根 `VERSION=1.13.3` 不一致退出。`validate_branch.py` 同样拒绝该版本组合。

本次仅把以下受管元数据从 `1.13.3` 对齐到本批目标 `1.13.4`：`VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`。未修改备份实现、未把本批未完成的恢复能力宣称为已验收。

修正后的检查：

- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，41 tests。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/1.13.4 --base-ref origin/main`：通过。
- `scripts/test-backup-format.sh`：通过。
- `git diff --check`：通过。

远端此前已通过的非治理 job 未因该门禁修正而重新本地伪造；推送新提交后等待 GitHub 对新 SHA 重新执行完整 workflow。

## 重复 PR 版本门禁的流程修正（2026-09-13）

本批暴露了一个可重复的开工流程缺陷：手工从 `origin/main` 创建 `develop/1.13.4` 后，
分支名已经进入目标版本，但 `VERSION`、`.env.example` 和两个 Frontend npm 元数据仍是
主线旧版本，导致 `scripts/dev.sh` 的早期分支检查和 `validate_branch.py` 阻断 PR。
这不是备份实现本身的失败。

为避免后续 Phase-16-05/06 及其他批次再次手工遗漏，新增：

- `scripts/ci/sync_version_metadata.py`：预检查所有文件结构后一次性同步版本元数据；
  发现缺失字段或非法版本时不写入任何文件。
- `scripts/start-development-batch.sh`：按总实施方案批次解析唯一版本/分支分配，
  fetch 选定远端 `main`，从精确远端基线创建分支，运行版本/分支门禁，创建英文引导提交，
  可选 `--push` 发布。它拒绝 tracked 工作区改动、重复本地/远端分支、重复规划分配和不安全名称；
  未跟踪用户文件不会被 stage。
- `scripts/ci/test_sync_version_metadata.py`：覆盖全量同步、幂等、非法版本和预检查失败不部分写入。
- quality-gates 的 Bash syntax 门禁现在包含该引导脚本；README 和 Phase-16 总方案已要求使用该入口。

验证：44 个治理单元测试、孤立临时 Git/裸远端的真实批次引导演练、版本/分支校验、
备份格式测试、Bash 语法和 `git diff --check` 均通过。当前 `develop/1.13.4` 已存在，
因此未用该脚本重建或覆盖本分支。

## 2026-09-13：撤回模拟升级尝试，补齐 Monitor 离线传输子项

### 开工与范围

用户同意先补齐本批前置能力，再继续 Phase-16-05，并要求推送。
执行 `git fetch upstream`，基线为 `98799c2`。旧本地 `develop/1.13.4` 因主线 squash 历史无法 fast-forward；
先保留为本地 `archive/phase16-04-before-recovery`，再从 `upstream/main` 重建同名开发分支。
没有重命名已推送分支，也未强制推送。用户原有 `~` 未读取、修改或 stage。

前次 Phase-16-05 工作区的模拟升级、手写逻辑 fixture、虚构目标 manifest、伪备份恢复脚本和完成声明已撤回。
未跟踪的本次尝试文件移到仓库外 `/tmp/gopulse-rejected-phase16-05/`；这些不作为产品输入或验收证据。
根与受管版本恢复为主线 `1.13.4`，不把未完成批次升级为 `1.13.5`。

### 实际变更

- `monitor/internal/plugin/portable.go`：读取现有 active revision 的公开状态/Secret 双区传输，复用各插件配置适配器与 image-owned catalog；相同 catalog 的空目标恢复重新物化可信包，不执行备份携带的二进制。
- `monitor/internal/plugin/runtime.go`：runtime 与离线传输共享存储 lease；未完成导入 marker 阻止正常启动。
- `monitor/internal/plugin/portable_command.go`、`monitor/cmd/monitor/main.go`：同一个 Monitor 可执行文件中的 pipe-only `plugin-state export/import`，没有新 HTTP 凭据导出端点；非 pipe 和错误输入给出固定脱敏失败。
- `monitor/internal/plugin/portable_test.go`：从真实 runtime revision 进行 round trip；验证正常 runtime 可重新启动恢复的插件，以及运行中导出、非空目标、未知字段、公开配置混入 Secret、包篡改、未完成 marker 的拒绝。
- `scripts/verify-plugin-state.sh`、`scripts/ci/verify_plugin_state.py`：独立的真实 Redis/Monitor 容器子项检查，明确标注不等价于产品备份验收。
- `.github/workflows/quality-gates.yml`：将新 Bash 入口加入语法检查；没有新增声称备份恢复通过的 CI job。
- `docs/releases/backup-plugin-state.md`：内部传输、Secret 边界、互斥、同 catalog 恢复和未交付范围。

### 实际验证与修正

- `(cd monitor && go test ./... && go vet ./...)`：最终生产代码通过。
- `(cd monitor && go test -race ./internal/plugin)`：通过。扩展理由：本次修改了 runtime 与离线命令共享的持久状态并发边界。
- 按 `deploy/docker/observability.Dockerfile` 的生产 `monitor` target 在真实 Linux amd64 Docker 上构建，使用文件锁定的 Go/Alpine 构建环境；不是修改旧镜像中的二进制冒充候选。
- 构建命令：
  `docker build --platform linux/amd64 --target monitor -f deploy/docker/observability.Dockerfile --build-arg TARGETARCH=amd64 --build-arg VERSION=1.13.4 --build-arg REVISION=working-tree-portable -t gopulse/monitor:phase16-04-portable .`
- 首轮脚本失败：使用了 `Path.open` 不支持的 `opener` 参数，改为内置 `open`；后续发现缺少独立 metrics tokens/产品版本，补齐与现有 Monitor 相符的启动配置。
- 实现过程中发现离线适配器默认 host 模式会拒绝 Compose 服务名，修正为 container 模式并同步直接测试；也补齐了测试的真实 bootstrap 配置。
- 因这些相关生产/脚本修改，重建镜像并重跑尚未通过的容器子项；不把失败运行当作验收成功。
- 最终命令：
  `scripts/verify-plugin-state.sh --monitor-image sha256:fb4de90141e8173e47cf771cf898acc22a665bbe16979d63a50a6981287da2c5 --evidence .run/phase16-04-portable/evidence.json`
  **通过**。
- 镜像对应工作树候选，label 明确为 `working-tree-portable`，不是正式 release manifest 或整套产品发布。
- Redis 使用锁定平台 digest `sha256:015185fd658093359cc83aa8396e06c1f39ba36df3c93f79528ec23ab409e73f`。
- 实际导出的 catalog digest：`sha256:a0ab8dd7657ff3bf36252a6dca422f87b2165b83bb49cde6b3e2b54a45c13d1c`。
- 真实验证结果：1 个 Redis 插件的 ID、版本、desired state、安装/更新时间保持；恢复后产生新的成功采集时间；拒绝 live export 和 nonempty import；本次带随机 label 的容器、volume、network 全部清理。
- 容器子项使用内置 discard publisher；没有验证下游历史入库，不以此宣称六插件或跨数据域恢复成功。

### 剩余必需工作（不是非阻断优化）

1. Lifecycle maintenance/write quiesce、异步排空及一致 cutover。
2. MySQL、Elasticsearch、VictoriaMetrics 权威导出/导入，以及 RabbitMQ/Kafka 拓扑和 offset 合同。
3. 将此内部插件 transport 接入 format v1：Secret 独立加密、配置可移植转换、各域实际 counts/time ranges；当前 transport 本身不是加密备份。
4. Lifecycle 空 project 恢复、operation ownership 清理、ready 发布与失败/中断诊断。
5. 最后采集时间/历史摘要的持久化迁移与各业务域事实核对。
6. 同架构产品恢复、六插件和双 Frontend、新写入、tamper/口令/空间/导入失败/中断的固定产品门禁。

本次只完成上述 Monitor 子项，**Phase-16-04 整批仍未完成，不得进入 Phase-16-05 验收**。
未新增虚假的 `verify-backup-restore.sh` 通过入口；本次未执行尚未实现的完整备份恢复固定门禁。

提交前检查（本次实际执行）：

- `bash -n scripts/verify-plugin-state.sh`：通过。
- `python3 -m py_compile scripts/ci/verify_plugin_state.py`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：44 tests，通过。
- `python3 scripts/ci/validate_versions.py`：通过，保持 `1.13.4`。
- `python3 scripts/ci/validate_branch.py --branch develop/1.13.4 --base-ref upstream/main`：通过。
- `git diff --check`：通过。

以上均为部分实施的检查记录，不替代计划第 8 节尚未通过的固定产品门禁。

## 完整闭环继续实施（2026-09-13，验收待记录）

用户再次明确要求完成全部计划后才推送。本次继续同一批活动任务；fetch 后主线为 `a0a049e`，与当前树无内容差异。

直接影响的发布基础设施调整理由：原 release builder/Go/Python/schema 强制每个产品候选包含 ARM 镜像和 12 个双平台 current 插件，违反 Phase 16 Linux amd64 独立构建验收、不依赖早期 ARM 产物的规则。
因此允许完整的 amd64-only 产品集合，同时继续读取历史双平台 manifest；仍拒绝缺少 amd64 或产品间平台集合不一致。针对这个公共合同扩展 Go/Python 的直接验证，不扩大为通用依赖审计。

正在实施真实 lifecycle backup/restore 和各域 adapter；本节不表示完成或验收通过。最终命令和结果将在真实执行后补充。

首轮真实候选/源产品演练：构建 `d10f9a3` 的 Linux amd64 全产品候选，真实用户、业务搜索、六插件采集、告警历史和审计种子成功；backup 在 `search-export` 拒绝实际按日期命名的 ES 索引（名称含 `.`）。已修正为独立的受限 ES 名称合同并加入该已观察缺陷的直接测试。该轮不计验收通过；源 project 已经通过其原 bundle 的 ownership 检查执行 `down --purge`，没有修改无关资源。

构建过程中发现单平台 buildx 产生单 manifest 而非 index，已让 builder 用实际内容构建单平台 OCI index，而不是填入不存在的另一架构。还将 observability runtime Dockerfile 的版本 label ARG 移到包安装层之后：实际多次构建中无关 revision 使相同 APK 安装反复耗时约 100 秒。backend 的尝试没有改变其 LABEL 前置缓存行为，不计为已完成的缓存优化。未改变依赖或运行内容，最终候选继续核对真实 OCI 标签。

第二轮真实候选 `bb815b1`：源产品种子、六插件真实采集、维护停写、六域加密 backup、独立 inspect 及公开 payload Secret 扫描均成功。首次 inspect 调用误带生命周期通用参数，修正 harness 后从已有真实 backup 继续，没有重跑已经成功的源备份。空目标恢复已经执行 MySQL、ES、VM、RabbitMQ/Kafka 导入，但在 Monitor 停止态容器创建阶段失败；本机 `docker compose create --help` 确认不支持 `--no-deps`，现改用受支持的 `up --no-start --no-deps`。该轮未判定恢复完成，失败目标由原操作清理，随后用原 bundle 清理源 project。

继续补齐计划必需合同：共享严格 JSON 解析、带 operation 身份的中断 ciphertext 清理、精确 failed_stage 和显式私有 doctor diagnostics、ES 规范化/时间范围、RabbitMQ 原生队列排空及拓扑摘要、Kafka 动态 topic 配置和新 topic 零点 rebase 验证。源 offset 作为 cutover 证据保存，不用伪消息填充新 topic 来冒充原 offset。

第三轮真实候选 `95bbedd0b5286298654e90f08335db25d0a9c2cd`（`dist/phase16-04-recovery-v3`）：
`same-arch` 全部通过，包括源种子、实际维护停写、认证加密六域备份、空项目恢复、六插件、
告警/审计/业务事实、双前端真实浏览器矩阵和恢复后新写入。证据为
`.run/phase16-04-recovery/same-arch-v3.log` 与私有 product acceptance receipt；不发布其中凭据。
失败矩阵已通过拒绝覆盖源、私有 doctor、错误口令/tamper、真实低空间和原生 SQL 导入失败清理。
低空间脚本初次失败原因是 root 加 cap-drop ALL 无权读取宿主用户 0700 目录，修正为原用户及同 uid/gid 的 1 MiB tmpfs；不是伪造 statfs。
中断及重试仍在运行，未提前计通过。

本轮补全运行文档、空且已 purge 的 stopped 目标重试、doctor 版本/有界健康摘要。
实际通过 `(cd lifecycle && go test ./... && go vet ./...)`、`scripts/test-backup-format.sh`、
Python harness 编译及 `git diff --check`。由于 bundle README 是受摘要绑定的 payload，最终候选
必须重新构建；新 lifecycle failure 路径和新的候选身份是后续最终矩阵执行理由，而不是因上下文切换重复验收。

第三轮候选剩余失败矩阵和 reuse-install 已全部通过：真实 SIGTERM 清理、同归档重试、源状态和业务计数不变、诊断 Secret 扫描；reuse-install 保持安装身份、只读 verify/status 和双前端路由。所有第三轮测试 project 已经由原 bundle 强归属 purge；私有证据保留在 `.run/phase16-04-recovery/accepted-v3`。

最终候选 `4bc686205755fe37cfd97f8b0fdecaccf3a6da7a` 构建完成，路径 `dist/phase16-04-recovery-v4`。
初次构建 APK 网络停滞超过 15 分钟后主动取消；仅传宿主代理在 bridge 内连接被拒。使用 `/tmp` 临时 Docker wrapper 给 buildx 传标准代理 build args 和 `--network host` 后真实完整构建成功（未修改源码/依赖、未把代理写入 Dockerfile）。构建日志为 `build-final-host.log`；最终新候选恢复矩阵已启动，尚不计完成。


## 最终完成验收（2026-09-14，本地 Linux amd64）

最终产品源码 revision：`4bc686205755fe37cfd97f8b0fdecaccf3a6da7a`。
Manifest：`sha256:f6e8ff1ad05d8b0f784c21e37ec19d7bec0c208b3af4db58115ad31729c3e20f`。
候选：`dist/phase16-04-recovery-v4/release-manifest.json`，真实 registry `127.0.0.1:15001/gopulse`，非公网发布。
后续提交仅追加开发记录，不改变该候选的源码/运行内容。

固定门禁实际结果：

| 命令 | 结果 / 证据 |
| --- | --- |
| `(cd lifecycle && go test ./... && go vet ./...)` | 通过；root 无 go workspace，因此用模块目录执行计划等价命令 |
| `scripts/test-backup-format.sh` | 通过 |
| `scripts/verify-backup-restore.sh --platform linux/amd64 --same-arch --manifest dist/phase16-04-recovery-v4/release-manifest.json --acceptance-image <真实不可变ID>` | 通过，`same-arch-v4.log` |
| 同脚本 `--failure-matrix`、同 manifest | 通过，`failure-matrix-v4.log` |
| `scripts/verify-product-lifecycle.sh --platform linux/amd64 --reuse-install --install <私有target绝对路径> --manifest <v4绝对路径>` | 通过，`reuse-v4.log` |
| `GOPULSE_RELEASE_MANIFEST=<v4绝对路径> scripts/verify-compose.sh` | 通过，`compose-final.log`；在干净 detached worktree `/tmp/gopulse-phase16-04-final` 执行，避免触碰用户未跟踪文件 |

上述日志位于 `.run/phase16-04-recovery/`，真实 fixture receipt 为私有 `product/acceptance.json`。
最终 cutover 为 `2026-09-13T15:51:38.252301740Z`（本地 23:51:38）；MySQL 有 2 用户、1 业务文章、
1 告警事件、13 管理审计；ES 有 3 索引（6 events、77 logs、1 post）；VM 有 372 series/1218 samples；
RabbitMQ 为 6 queues/6 exchanges/20 bindings，Kafka 单 topic/partition 的已排空源 offset 为 131；
6 插件及受信 catalog 摘要均独立核对。恢复后 SQL/搜索/指标/拓扑/插件事实一致，实际新文章被搜索命中；
双前端桌面/窄屏浏览器和三来源规则入口通过。JWT 轮换使旧会话失效，保留真实用户凭据与角色。

错误口令、tamper、真实 1 MiB 空间不足、原生 SQL 导入失败、真实 SIGTERM 中断、重试、拒绝源覆盖、
源状态和计数不变、私有 doctor 诊断与 Secret 扫描全部通过。v4 源/目标/negative 项目均已通过原
bundle 执行强归属清理；Compose 脚本也完成自身清理，无关资源及用户文件保留。

实际文件范围：lifecycle backup/control/release 模块和 acceptance fixture 命令；Monitor 插件传输与采集历史；
release amd64 合同的 Go/Python/schema/builder；backup/reuse/browser 验收脚本；根 README、bundle README
和 `docs/releases/backup-{restore,plugin-state}.md`。早期提交已将所有受管版本对齐 `1.13.4`，本批不再重复 bump。

边界：256 MiB 有界逻辑备份、完整停写维护窗口、相同 manifest 的空项目同架构恢复、单 broker/topic 支持；
Kafka 已确认历史在 ES/VM，空目标 offset 重置而不伪造消息；不宣称在线备份、ARM、Windows、macOS 或
1.9.4 升级。本批无阻断项；Phase-16-05 必须在本批合入 main 后另建分支独立实施和验收。
