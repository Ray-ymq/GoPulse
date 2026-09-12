# Phase-16-06：三宿主矩阵与阶段收口实施方案

> 执行序号：6 / 6。目标版本`1.13.6`、开发分支`develop/1.13.6`及Phase完成条件以[Phase-16-总实施方案.md](Phase-16-总实施方案.md)为准。

## 1. 批次目标

本批不首次设计制品、生命周期、Frontend、backup或upgrade，而是让同一最终candidate在三类真实宿主上完成固定产品矩阵并收口Phase 16：

```text
same release manifest / bundle family / source revision
  ├─ Linux amd64 + linux/amd64 containers
  ├─ macOS arm64 + linux/arm64 containers
  └─ Windows amd64 + linux/amd64 containers
        ↓
  clean install + unique edge + two roles/apps
  lifecycle + persistence + signal + ownership
  upgrade and backup/restore assigned cross-host cases
        ↓
  three redacted evidence JSON files
        ↓
  schema/digest/host uniqueness aggregation
        ↓
  VERSION 1.13.6 + docs/logs + Phase 17 handoff
```

缺少任一真实宿主、用模拟/交叉编译替代，或三个host没有消费同一candidate digest，均必须保持Phase 16未完成。

## 2. 前置条件

- Phase-16-01至Phase-16-05已按顺序合入最新`upstream/main`，各自实施记录和固定门禁通过，根完成版本为`1.13.5`。
- fetch后从最新主线创建本批分支；除最终version/release/evidence harness/文档外，不预期新增产品能力。
- 三host owner、运行窗口、Docker server/Compose共同支持版本、磁盘/内存和网络拉取条件已经重新确认；macOS Docker daemon必须实际可用，Windows必须为Linux container mode。
- 生成一个来自最终本批source revision的candidate release manifest与三host bundle，全部image/platform digest固定；开始任一host长矩阵后不得对同一candidate静默重建。
- 每个host使用独立空交付目录、随机project/token/edge port和与日常资源隔离的backup/evidence目录；开始前保存Docker、端口和文件快照。
- 浏览器测试数据、Secret、passphrase和host path只保存在调用方私有临时目录，不提交仓库；evidence只包含总方案允许的脱敏字段。

## 3. 实施范围

### 3.1 统一acceptance runner与证据

- 提供跨平台test-only acceptance runner，从同一源码构建三个host版本并只调用正式product CLI/API；不把另一套生命周期嵌入runner。
- 固定命令语义为`gopulse-acceptance phase16 --bundle <path> --evidence <new-file>`或总方案先行修订后的等价入口；Windows使用`.exe`，参数/场景保持一致。
- runner创建和操作唯一随机owned project，支持按host profile分配场景；正常/失败/signal均执行强归属finally并保存before/after摘要。
- evidence schema固定匿名run ID、host OS/arch、Docker server OS/arch、Compose version、source revision、bundle/manifest/platform digest、project token hash、开始/结束时间、gate ID/result和资源摘要。
- evidence不含hostname、username、绝对path、IP、Cookie、credential、passphrase、业务正文、完整container/volume ID或原始log；三个文件通过Secret/path哨兵扫描。

### 3.2 三host共同clean-install矩阵

每个平台都必须执行：

- 从本机原生bundle运行`version`、`doctor`和错误架构/daemon不可达/unsafe path或port的代表性早期失败；记录Docker server而非只有client。
- 在无Git/Go/Node/Python/数据库客户端依赖的产品PATH中执行`init → up → verify → status → logs`，镜像只按candidate digest pull，不build源码。
- 确认全部预期service/job、container platform digest、numeric user、read-only root、network、volume和只有一个`127.0.0.1`edge端口。
- Browser从唯一origin完成普通用户注册/登录、帖子、评论或点赞、关注/Following、收藏、搜索、通知、编辑/删除代表流程。
- 建立bootstrap super_admin并完成统一登录、默认管理大屏、六插件状态/一个代表生命周期操作、Metrics/Logs/Events、三源告警及角色/审计代表流程；普通用户管理API固定403。
- 执行保留卷`down/up`、一个stateless container替换、edge端口重新发现和一个可观测局部故障；事实恢复且社交主路不被可观测故障阻断。
- 对前台命令发送平台原生interrupt，核对bounded终态；最终普通`down`保留volume，验收清理显式删除且只影响当前project。

### 3.3 Linux amd64分配场景

- 使用Linux文件系统路径和一个Docker daemon，确认可执行位、LF、POSIX权限、SIGINT/SIGTERM和无宿主runtime。
- 从锁定`102aa4f...`真实`1.9.4`fixture升级到最终`1.13.6`candidate，核对Phase-16-05全部核心before/after事实和pre-upgrade backup。
- 在升级完成project上生成最终candidate encrypted backup，作为后续macOS跨架构restore输入；backup经独立inspect/digest验证。
- 运行最完整的业务、管理、三源告警和既有Compose回归；其他host只补平台差异和代表闭环，避免三份完全重复的长矩阵。

### 3.4 macOS arm64分配场景

- 从APFS含空格的本地目录运行`darwin/arm64`CLI，Docker server必须是`linux/arm64`；不得设置`platform=linux/amd64`或依赖Rosetta/QEMU完成产品运行。
- 验证Docker Desktop文件共享、case-insensitive路径冲突拒绝、Terminal interrupt、文件权限/私有temp和edge端口占用恢复。
- 运行用户/管理两个应用的`390x844`与`1440x900`代表viewport、非UTC timezone、deep link/refresh/session expired和键盘路径。
- 将Linux amd64生成的encrypted backup恢复到全新arm64 project，核对业务/角色/告警/审计/历史，并证明六插件使用arm64 catalog重新物化且不执行amd64 binary。

### 3.5 Windows amd64分配场景

- 从NTFS用户目录且路径包含空格的解压bundle，在Windows PowerShell/Terminal直接运行`gopulse.exe`与`gopulse-acceptance.exe`；不得从WSL shell或`/mnt/*`调用。
- 记录Windows build、process architecture、Docker Desktop/Compose和server`linux/amd64`；确认Linux containers mode、drive/file sharing和可用资源。
- 验证ZIP/tar解压、CRLF/`.gitattributes`、Compose/YAML/env读取、反斜线/空格/Unicode路径、PowerShell参数引用和当前用户ACL；不要求Bash。
- 验证edge/backend端口占用的Docker前失败、动态port重新发现、Windows console interrupt、Docker Desktop停止/恢复后的doctor/status和cleanup。
- 执行双Frontend代表浏览器流程，包含普通用户管理API403、super_admin管理操作、session过期和两个SPA deep link刷新。
- 按Phase-16-05已分配合同完成第二amd64host的最小`1.9.4`升级重复证据，至少覆盖source识别、migration、角色/bootstrap、Redis v1插件和核心历史；若已在该最终candidate的Phase-16-05记录真实运行且输入未变，可引用，但最终version/digest变化时必须重跑受影响步骤。

### 3.6 失败、清理与宿主隔离

- 三host各选择一个不同的代表失败：Linux升级/恢复失败、macOS跨架构restore中断、Windowspath/file-sharing/console中断；共同早期校验由self-test覆盖，不重复全排列。
- 每次stop/kill/down/remove前校验project/token/service/resource/digest；不得用全局prune、宽泛name/glob、home/root递归删除或修改用户Docker Desktop配置。
- 最终before/after snapshot包含container/network/volume/image tag→ID映射、published port和验收目录allowlist；除当前candidate缓存/已记录evidence外无未解释变化。
- 运行失败保留必要脱敏诊断供修复，确认后只删除当前owned资源；用户其他container/volume/image、日常`.env`、未跟踪文件和端口保持不变。

### 3.7 Evidence聚合与文档收口

- 聚合器验证三份evidence schema、host OS/arch与Docker server组合、source revision、manifest/bundle/platform digest、时间、gate全集和资源清理结果。
- 相同host伪装两个结果、缺host/server field、不同candidate、模拟arch、过期或redaction失败均阻断聚合。
- 更新`README.md`、使用手册、platform support、upgrade、backup/restore和release notes，只描述真实通过的最低环境、命令、限制和恢复步骤。
- 更新总方案与六份split plan的真实状态时区分本地run、remote checks、PR与merge；Phase未合入main前只写“本地/候选完成”。
- 创建同名实施记录，汇总前五批仍有效证据与本批三host结果；不复制Secret/evidence全文入Git。

## 4. 不在本批范围

- 新产品命令、业务、插件、告警、Frontend页面、backup格式或升级来源。
- 为通过某个平台静默降低另两个平台合同、切换到amd64模拟、增加第二套PowerShell/Bash编排。
- Kubernetes、Ingress、公网TLS、原生Windows Service/macOS daemon或生产高可用。
- 默认代码/架构Review、依赖/CVE审计、SBOM/签名、性能/容量测试或全页面视觉排列组合。
- 修复不阻断Phase 16固定矩阵的历史问题；记录为Phase 17或独立follow-up。

## 5. 建议实施顺序

1. fetch/branch后冻结最终candidate revision、manifest、三个bundle和image platform digest，运行artifact/self-test并保存各host before snapshot。
2. 先运行Linux amd64完整矩阵、`1.9.4`升级和最终encrypted backup；真实阻断只在最小直接范围修复后重建新candidate。
3. 在macOS arm64运行clean install/UI/platform项和Linux backup跨架构restore。
4. 在Windows amd64从NTFS/PowerShell运行clean install、path/CRLF/file sharing/console/UI及最小旧版本升级。
5. 每个host完成强归属cleanup并生成redacted evidence；运行聚合器，任何candidate digest不一致则三host相关结果失效重跑。
6. 只修复固定矩阵暴露的阻断，按影响host/合同重验；未变化host证据在candidate内容/digest未变时保持有效。
7. 更新版本、release manifest、文档、总/分方案状态和同名实施记录，运行最终治理门禁，提交并等待远程checks/合入。

## 6. 预计直接影响文件

- 跨平台`gopulse-acceptance`runner、evidence schema/redaction和聚合器
- `scripts/verify-platform-matrix.*`或等价调度/CI artifact配置
- 仅在真实阻断时修改`lifecycle/**`、`deploy/product/**`、Frontend/Backend/Monitor直接代码
- `.github/workflows/`中的候选artifact和三host/self-hosted调度、上传/聚合配置
- `README.md`、`使用手册.md`、`docs/platform-support.md`、`docs/upgrade.md`、`docs/backup-restore.md`和release notes
- 总方案、六份split plan状态及本批同名实施记录
- `VERSION`、两个Frontend package/lockfile、CLI/release manifest/image/plugin metadata

预计产品代码不应大范围变化。任一超出acceptance runner、文档和版本的修改必须在实施记录关联到具体阻断gate和重验范围。

## 7. 批次与阶段验收标准

### 7.1 三host真实性

- Linux amd64、macOS arm64、Windows amd64分别由真实host OS和Docker server组合生成evidence；mac运行linux/arm64，两个amd64host运行linux/amd64。
- 三份evidence绑定同一source revision和release manifest，逻辑image index相同、各host platform digest正确；无buildx/QEMU/WSL替代。
- 每个平台从对应bundle、PowerShell/Terminal或native shell运行，不要求产品用户安装Go/Node/Python/Bash/数据库客户端。

### 7.2 产品共同闭环

- 每个平台clean install、唯一edge、双Frontend统一登录、user/social、super_admin/dashboard/plugin/observability/alert和普通用户403通过。
- `down/up`、container替换、平台interrupt、可观测局部故障和edge端口恢复后，持久事实与应用可继续；其他Docker资源不变。
- CLI/output/log/API/bundle/DOM/evidence/diagnostic无Secret、Cookie、连接串、hostname/username或host绝对path。

### 7.3 跨批数据闭环

- 最终candidate完成一次真实`1.9.4 → 1.13.6`完整升级和第二amd64host关键重复证据；before/after历史边界、Redis v1和失败backup恢复成立。
- Linux current backup在macOS arm64空project恢复；MySQL/ES/VM/plugin/config/Secret合同正确，目标plugin binary为arm64并真实采集。
- backup/upgrade/restore失败路径只影响当前owned资源，不发布partial完成状态或删除source/backup。

### 7.4 平台特有项

- Linux filesystem/permission/LF/signal/one-daemon通过。
- macOS APFS空格路径、file sharing、case-fold、native arm64、Terminal interrupt、viewport/timezone通过。
- Windows NTFS空格/Unicode路径、CRLF、PowerShell quoting、ACL、Docker Desktop Linux mode、file sharing、port、console interrupt、cleanup和两个Frontend通过。

### 7.5 Phase完成条件

- 三份evidence聚合退出0，前五批全部实施记录仍与最终candidate兼容，无blocking gate。
- 根、两个Frontend、CLI、manifest、9镜像和六插件current metadata统一为`1.13.6`；分支为`develop/1.13.6`。
- 六份Phase 16 split plan均有同名真实实施记录；总方案/README/使用手册只描述真实通过范围。
- 本批提交、远程checks和PR合入主线后才能把Phase 16标记完成；Milestone 4仍待Phase 17收口。

明确停止条件：上述项目通过后立即提交和停止，不添加Review、Kubernetes、性能、签名、额外平台或无关重构。

## 8. 固定验证命令与回归范围

最终candidate在各host运行等价命令：

Linux/macOS：

```bash
./gopulse-acceptance phase16 --bundle <platform-bundle> --evidence <new-evidence.json>
```

Windows PowerShell：

```powershell
.\gopulse-acceptance.exe phase16 --bundle <platform-bundle> --evidence <new-evidence.json>
```

聚合与仓库门禁：

```bash
(cd lifecycle && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json
scripts/verify-lifecycle.sh --self-test
scripts/verify-product-ui.sh --self-test
scripts/verify-backup-restore.sh --self-test
scripts/verify-upgrade.sh --self-test
python3 scripts/ci/verify_phase16_evidence.py \
  --linux <linux-evidence.json> \
  --macos <macos-evidence.json> \
  --windows <windows-evidence.json>
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.13.6 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- Linux profile承担最终完整Compose、完整旧版本升级和backup；macOS profile承担arm64、跨架构restore、viewport/timezone；Windows profile承担native path/CRLF/PowerShell/file sharing/console和第二amd64关键升级。
- 一个host失败只在candidate未变化且合同独立时重跑该host；任何产品/Compose/image/bundle内容变化产生新digest后，重跑所有受影响host，不因已耗时而沿用旧digest证据。
- 已成功且输入未变的前五批unit/package结果直接引用；本批不重跑每个历史Phase专项脚本。
- 提交后补充`git diff --check upstream/main...HEAD`；remote checks和merge真实状态写入实施记录后才收口。

## 9. 实施记录与 Phase 17 交接

完成前创建`dev/logs/Phase-16/Phase-16-06-三宿主矩阵与阶段收口.md`，至少记录：

- candidate source/manifest/bundle/image/plugin digest与三host实际OS/server/Compose摘要。
- 三hostgate结果、platform特有项、浏览器用例、upgrade/backup/restore跨host分工。
- 每次失败、根因、最小修复、candidate是否变化、实际重验范围和最终资源快照。
- evidence checksum/redaction结果、文档/version/remote checks/PR/merge真实状态和未阻断follow-up。

交给Phase 17的固定输入是`1.13.6`完整Compose产品、三个受支持host bundle、双架构制品、共享生命周期、统一双Frontend、backup format v1和`1.9.4`升级合同。Phase 17只做Kubernetes前工程质量收口；不得把Kubernetes作为验收条件，也不得在无直接影响时重新执行完整三宿主矩阵。
