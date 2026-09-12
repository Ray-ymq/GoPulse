# Phase-16-04：一致备份恢复与诊断闭环实施方案

> 执行序号：4 / 6。目标版本`1.13.4`、开发分支`develop/1.13.4`及数据边界以[Phase-16-总实施方案.md](Phase-16-总实施方案.md)为准。

## 1. 批次目标

为完整Compose产品建立一次有界维护窗口内的最小一致备份和空项目恢复能力，并让失败可通过脱敏诊断定位：

```text
owned running project
  → lock + preflight
  → close edge / stop producers / drain Outbox + RabbitMQ + Kafka
  → stop writers at a recorded boundary
  → logical MySQL + ES + VM + Monitor state + portable config
  → digest + authenticated encryption + atomic backup
  → resume original state

encrypted backup
  → authenticate/decrypt/validate before Docker writes
  → new empty owned project
  → restore facts/history/config
  → rebuild cache/search/message topology/plugin runtime
  → verify + representative new writes
```

本批不提供在线热备、增量备份、原地覆盖或任意版本恢复。备份无法证明排空和完整性时必须失败，不能发布“部分成功”文件。

## 2. 前置条件

- Phase-16-03已合入最新`upstream/main`，目标版本`1.13.3`的共享CLI、唯一edge、双Frontend和release manifest稳定。
- fetch后从最新主线创建本批分支，不继续使用UI批分支。
- 有界确认锁定MySQL/Elasticsearch/VictoriaMetrics版本提供的逻辑dump/snapshot/restore公开API、原子完成标识和失败清理语义；只检查直接调用点和官方文档。
- 确认Backend Outbox、RabbitMQ queue、Kafka group lag及Monitor/Router/Marshaller生产/消费的可读排空判据，避免只等待固定sleep。
- 列出Monitor持久volume中权威逻辑状态与可重建runtime投影，设计不含binary/PID/symlink的export/import合同。
- 确认Linux/macOS/Windows私有temp、终端无回显输入、文件替换和中断清理共同实现；不自行设计未经审查的加密算法。

## 3. 实施范围

### 3.1 Backup preflight与维护窗口

- `gopulse backup --output <new-file>`只接受当前owned、version/manifest一致且无其他变更operation的project；目标文件存在、危险路径、空间不足、错误权限或不支持format时在停写前失败。
- 获取安装级lock，保存各service原运行状态、edge URL、队列/offset/数据摘要和资源快照；backup结束按原状态恢复，不把原本stopped的service擅自启动。
- 先关闭edge接受新请求，再停止Monitor等周期遥测producer；Backend保留至Outbox排空，Worker/Indexer保留至RabbitMQ业务队列排空，Router/Marshaller保留至Kafka目标group lag为0。
- 排空使用状态事实和连续稳定观察，不只sleep。任一队列在有界deadline内未排空，恢复原状态并失败，不创建最终backup。
- 排空后按依赖顺序停止Backend alert/outbox、Worker、Indexer、Router、Marshaller等写入者，记录统一backup boundary和每个source最后成功位置。

### 3.2 权威数据导出

- MySQL使用锁定版本逻辑dump/consistent snapshot，覆盖全部schema、migration、用户/业务/Outbox、bootstrap/角色、告警规则/状态/incident/history和management audit；不把SQL密码放入argv或dump header。
- Elasticsearch使用官方snapshot/export能力保留Logs、Events及template/alias metadata；业务search index可一并保存，但restore后必须以MySQL事实校验或重建，不能反向成为事实源。
- VictoriaMetrics使用锁定版本官方snapshot/backup能力保留时序历史和必要metadata；不得直接复制正在写入的数据目录。
- Redis只作为可重建cache记录“有意排除”；RabbitMQ/Kafka在成功排空后只保存拓扑/consumer contract并于restore重新初始化，不复制opaque volume。
- 每个导出产生逻辑组件metadata、size和SHA-256；snapshot完成前的临时结果不进入backup manifest。

### 3.3 Monitor插件逻辑export/import

- 新增受限离线export，只输出六个固定plugin ID的Manifest identity、受信version引用、desired state、非敏感config、独立Secret和必要revision语义。
- 明确排除release archive/entrypoint、PID/process record、runtime port、active/current symlink、临时revision、原始日志和宿主/container path。
- export重新校验每个active revision、config/Secret schema和catalog identity；unknown/corrupt状态使backup失败并保留原volume，不静默跳过某插件。
- import只在新空Monitor state中执行，由目标arch current/retained catalog重新物化。相同受信version/arch存在则保留；不存在时按明确兼容映射升级到目标arch current并记录before/after。
- desired=running的插件在目标真实采集成功后提交active；desired=stopped保持stopped且无runtime process。一个插件失败使restore未完成，但不执行backup中的任何binary。

### 3.4 Portable config与Secret

- backup保存恢复所需product config allowlist和Secret，使JWT/session、数据服务与内部身份可继续；宿主path、project name、edge port、runtime ID、image ref和registry token不作为可移植值强制复用。
- config和Secret始终位于加密payload内；明文temp使用随机私有目录，禁止符号链接/重解析点，写入后fsync，成功或失败均有界删除。
- 使用锁定的成熟authenticated-encryption实现和显式format/KDF参数；passphrase从无回显terminal或受限descriptor读取两次确认，不接受CLI flag/env，日志只记录reason code。
- 错passphrase、tamper、截断和payload digest错误必须在创建Docker资源前失败；不区分到可帮助猜测密码的细粒度错误。

### 3.5 Backup format v1与安全解析

- 加密payload根目录固定包含`backup-manifest.json`、mysql、elasticsearch、victoriametrics、monitor和config sections；manifest声明format、product version/revision、source identity、时间边界、drain evidence、payload size/digest和restore compatibility。
- 解析拒绝unknown required section、duplicate path、absolute/parent path、link/device、case-fold collision、单项/总量超限、压缩炸弹、digest mismatch、错误format或未声明数据。
- backup输出使用同目录临时文件并原子rename；已有output默认拒绝，不提供`--force`覆盖。失败删除本次partial，不删除旧backup。
- `gopulse backup inspect`只在认证成功后显示脱敏manifest摘要，不导出Secret或业务正文。

### 3.6 空project恢复

- `restore`要求新安装目录、全新project identity和无同名container/network/volume/config；任一existing resource在Docker写入前拒绝。
- 先在私有temp完成decrypt、schema、size、path和全部digest验证，再生成新host-specific edge port/project并创建资源。
- 恢复MySQL后运行当前migration；恢复ES Logs/Events与VM历史；重建业务search和Redis cache；重新创建RabbitMQ/Kafka拓扑为空。
- 导入Monitor逻辑状态并运行真实采集；恢复两个Frontend/Backend后执行只读verify、升级前历史查询和至少一个新社交写、一个新metric/log/event及一个告警评估。
- 全部通过后原子标记new project可用。失败只删除本次operation创建且逐项证明归属的新资源/明文temp，保留source project和backup；失败报告可供重试。

### 3.7 跨架构恢复与诊断

- 固定完成一次Linux amd64创建backup、macOS arm64恢复到`linux/arm64`产品栈；用户/管理事实和遥测历史保持，插件使用arm64受信package。
- backup中不得包含可执行文件。安全测试即使注入ELF/PE/Mach-O或`arch=amd64`entrypoint section也应被format allowlist拒绝，不能在restore运行。
- `gopulse doctor --bundle <path>`生成脱敏诊断包，只包含allowlist版本、manifest/digest、service安全状态、health reason、队列摘要、固定数量日志尾部和operation报告。
- 诊断包明确排除`.env`、backup payload、Cookie、业务正文、Secret、完整连接串、registry credential、hostname/username和宿主绝对path，并通过哨兵扫描。

## 4. 不在本批范围

- 在线无停机、增量、连续、计划任务或远程backup；跨地域/高可用恢复。
- 恢复到非空project、原地覆盖、自动删除source project或无确认volume清理。
- 原始MySQL/ES/VM/RabbitMQ/Kafka/Monitor volume tar跨架构复制。
- 任意历史产品版本restore或`1.9.4`升级事务；下一批只消费本批format。
- 用户自定义backup插件、外部KMS/云对象存储、密钥托管UI或自研密码算法。

## 5. 建议实施顺序

1. 真实探测三个数据服务官方snapshot/dump、队列排空和Monitor state，冻结backup format v1/schema/limits。
2. 先实现纯文件format、authenticated encryption、path/size/digest安全解析和跨平台temp tests，不访问Docker。
3. 实现operation lock下的edge关闭、producer停止、三队列排空、writer停止与原状态恢复。
4. 接入MySQL/ES/VM导出和Monitor逻辑export，生成加密backup并验证中断/失败不发布partial。
5. 实现空project restore顺序、search/cache/message重建、plugin arch映射和完成标记。
6. 运行同架构恢复、真实amd64到arm64恢复、tamper/wrong-passphrase/导入失败与强归属清理。
7. 实现脱敏diagnostic bundle，更新backup/restore runbook、版本和实施记录，运行固定门禁后提交。

## 6. 预计直接影响文件

- `lifecycle/**`中的backup/restore/diagnostic、encryption、format、drain和operation实现/测试
- `monitor/**`受限logical export/import命令或package及直接测试
- `deploy/product/**`中的backup/restore one-shot service、只读mount和临时volume配置
- 数据服务官方snapshot所需最小Compose volume/config；不得开放宿主管理端口
- 新`scripts/verify-backup-restore.sh`与`scripts/ci/`format/diagnostic验证器
- backup format schema、`docs/backup-restore.md`、`docs/platform-support.md`及使用手册
- `.gitignore`中仅本地backup/evidence/temp模式（不得忽略源码或文档）
- 本批同名实施记录、`VERSION`、两个Frontend和release metadata

## 7. 批次验收标准

### 7.1 Backup一致性

- edge关闭后无新业务请求；Outbox、RabbitMQ和Kafka均以真实状态排空，writer停止和backup boundary可解释。
- MySQL、ES Logs/Events、VM、Monitor逻辑state和portable config/Secret全部进入format v1并通过size/digest；Redis/search/message排除或重建理由明确。
- drain timeout、数据导出失败、加密中断、目标已存在和SIGINT各不发布partial backup，原project数据不变且按原状态恢复。
- 最终backup认证加密，明文temp清理；argv/env/log/diagnostic/evidence无passphrase或Secret。

### 7.2 Restore正确性

- wrong passphrase、tamper、路径穿越、link、case collision、超限、digest错误在Docker创建资源前失败。
- 非空目标安全拒绝；空project同架构restore后MySQL全部权威事实、Logs/Events、VM、plugin desired/config/Secret和portable config正确。
- cache/search/RabbitMQ/Kafka按合同重建，并能完成一个新业务写、新search查询、新metric/log/event和告警评估。
- restore失败只清理当前新project资源，source project/backup/其他Docker资源和host files不变。

### 7.3 跨架构和诊断

- Linux amd64 backup在macOS arm64恢复；每个running插件使用`linux/arm64`受信entrypoint且真实采集，stopped插件不启动。
- 注入binary/错误arch/unknown plugin logical state的backup被严格拒绝或按明确规则失败，不执行输入文件。
- diagnostic bundle allowlist、size和日志tail有界，Secret/业务正文/hostname/username/绝对path哨兵扫描通过。

### 7.4 完成条件

全部验收及固定门禁通过，无持久数据或Secret阻断；根与受管metadata为`1.13.4`，分支为`develop/1.13.4`，同名实施记录包含真实snapshot API、文件digest和失败恢复结果。提交后停止，不提前执行`1.9.4`升级。

## 8. 固定验证命令与回归范围

```bash
(cd lifecycle && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd lifecycle && go test -race -count=1 ./internal/backup/... ./internal/restore/... ./internal/diagnostic/... ./internal/lock/...)
(cd monitor && test -z "$(gofmt -l .)" && go test -count=1 ./internal/plugin/... && go vet ./internal/plugin/...)
scripts/verify-backup-restore.sh --self-test
scripts/verify-backup-restore.sh --same-arch
scripts/verify-backup-restore.sh --source linux/amd64 --target darwin/arm64
scripts/verify-lifecycle.sh --existing-product
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.13.4 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- `--same-arch`覆盖完整数据、failure injection和新写入；`amd64→arm64`只补跨架构plugin/runtime及关键事实/历史，不重复所有tamper组合。
- backup改变全体writer的有序停止，最终运行一次代表用户/管理/告警回归；不重跑无变化的全部Frontend viewport矩阵。
- 如锁定第三方snapshot API迫使Compose配置变化，扩大到对应数据服务replace/restart检查并先在实施记录写明风险。
- 提交后补充`git diff --check upstream/main...HEAD`。

## 9. 实施记录与下一批交接

完成前创建`dev/logs/Phase-16/Phase-16-04-一致备份恢复与诊断闭环.md`，至少记录真实drain值/边界、数据服务API和版本、format/KDF/加密library、payload清单/digest、同/跨架构restore、插件version映射、失败清理、diagnostic扫描和未覆盖限制。

交给Phase-16-05的固定输入是已验证的backup format v1与空project restore。升级批必须调用正式`gopulse backup`，不得另写一套临时卷复制或在migration后尝试未知兼容的旧镜像降级。
