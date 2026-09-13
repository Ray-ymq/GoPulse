# Phase-16-04：一致备份恢复与诊断闭环开发记录

## 状态

**2026-09-13：实施中，仅完成格式层与离线检查，整批未完成、未验收。**

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
