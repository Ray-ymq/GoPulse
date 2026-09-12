# Phase-16-02：共享产品生命周期与安全初始化闭环实施方案

> 执行序号：2 / 6。目标版本`1.13.2`、开发分支`develop/1.13.2`及批次依赖以[Phase-16-总实施方案.md](Phase-16-总实施方案.md)为准。

## 1. 批次目标

在Phase-16-01不可变制品上实现一个真正跨平台的产品控制面，让用户只依赖Docker/Compose和对应host bundle完成安全初始化、启动、停止、状态、日志、只读验证与诊断：

```text
gopulse doctor
  → gopulse init
  → gopulse up
  → gopulse verify / status / logs
  → Ctrl+C / SIGTERM / console interrupt
  → gopulse down
  → gopulse up（保留状态恢复）
```

Linux、macOS、Windows可执行文件必须来自同一Go命令实现。平台差异只允许存在于路径、权限、signal和console适配层，不得把生命周期复制到新的Bash/PowerShell脚本。

## 2. 前置条件

- Phase-16-01已合入最新`upstream/main`，目标版本`1.13.1`的9个双架构产品镜像、六插件包、release manifest与三个host bundle均有真实digest/checksum记录。
- fetch后从最新主线创建本批分支，不继续使用制品批分支。
- 按总方案§17有界确认三host Docker CLI/Compose共同命令、exit code、project label、pull-by-digest、path sharing、signal和Windows console行为。
- Linux amd64与macOS arm64 Docker daemon可实际运行；Windows本批至少完成native CLI/path/fake-engine合同，最终真实完整运行留给Phase-16-06，但不能以此宣称Windows已支持。
- 现有`scripts/dev.sh`、`verify.sh`、`down.sh`只作为已实现语义和回归输入；产品CLI不得直接exec这些脚本。

## 3. 实施范围

### 3.1 共享CLI命令与内部边界

- 在`lifecycle/`完成`version`、`doctor`、`init`、`up`、`down`、`status`、`logs`、`verify`命令树，所有命令使用稳定exit code、结构化内部错误和不含Secret的用户消息。
- Docker/Compose调用集中在一个runner接口，生产实现使用参数数组而非shell字符串；测试实现记录调用并注入stdout/stderr/exit/signal，不发生真实Docker访问。
- host OS差异由最小adapter处理：可执行路径、原子rename、目录权限/Windows ACL、process group、终端中断、path canonicalization；业务顺序和安全校验保持共享。
- `version`和纯参数校验不访问Docker；`doctor`只读；变更命令统一经过operation lock、identity加载和资源ownership检查。

### 3.2 安全初始化与安装状态

- `init`只接受空安装状态，在私有父目录生成product config、Secret文件、project identity和初始state；已有任何受管文件默认拒绝，不提供静默overwrite。
- 使用系统加密随机源为数据库、JWT、RabbitMQ、VictoriaMetrics及每个内部API/metrics身份生成独立Secret；不复制`.env.example`开发凭据，不输出到console或argv。
- 固定project name语法、随机identity token、edge loopback port和release manifest digest；用户可选择合法未占用端口，但不能选择wildcard、远程host、内部service地址或空project。
- 文件先在同目录私有temp写入、flush/sync并原子替换；失败删除本次temp，不留下半配置。POSIX mode和Windows当前用户ACL均有直接检查；无法建立安全目录即失败。
- 非敏感state只记录schema、project、version、revision、manifest digest、Compose asset digest和已认领resource摘要，不内联Secret、Cookie、用户名、宿主绝对path或registry凭据。

### 3.3 启动、初始化job与唯一edge

- `up`校验CLI/manifest/config/state版本、host/server arch、image platform digest、磁盘/内存、端口和project归属后，按digest pull并以`--no-build`启动产品Compose。
- migration、Kafka topic、Elasticsearch template/alias/search init和Monitor bootstrap继续使用幂等one-shot/服务内逻辑；任一必要job失败阻断完成，不返回伪healthy。
- 产品Compose只发布`127.0.0.1:<edge-port>`；Backend宿主端口移出product profile。开发脚本需要Backend端口时使用明确developer override，不能改变产品manifest。
- 启动成功后自动执行同一`verify`实现，并打印唯一用户入口。失败保留当前强归属资源供`status/logs/doctor`检查，不自动删卷或改写完成version。
- `up`不要求Git/repository/source tree，也不从当前工作目录推导revision；全部制品身份来自bundle manifest。

### 3.4 Verify、status、logs与doctor

- `verify`只读核对预期service数量/状态/health、one-shot exit、运行image platform digest、numeric UID、read-only root、network/port、edge`/health`/`/ready`、两个SPA和API smoke。
- `verify`不创建用户、帖子、告警或插件变更，不写MySQL/RabbitMQ/Kafka/ES/VM，不打开无法关闭的cursor/PIT。
- `status`显示产品版本、service安全状态、唯一edge URL、volume是否存在和最近operation结果；不显示container ID全值、PID、内部URL、Secret或宿主path。
- `logs`只对当前owned project调用Compose logs，支持固定service allowlist、`--tail`与`--since`上限；不接受任意Compose参数或shell片段，不读取`.env`打印。
- `doctor`检查CLI/manifest匹配、Docker server OS/arch、Compose能力、文件共享、端口、可用资源、路径长度/case/line ending和existing project冲突；输出固定reason code和脱敏修复建议。
- `doctor --json`/`verify --json`供Phase-16-06 evidence消费，schema不包含hostname、username、绝对path、IP、credential或业务正文。

### 3.5 Stop、锁与资源强归属

- 每个安装目录一次只允许一个变更命令；operation lock包含随机ID、命令、开始时间和process identity摘要，不含Secret。陈旧lock只能在证明原process不存在且state一致后受控恢复。
- `down`在每个stop/remove前联合验证project name/token、Compose project/service/working-dir label、resource ID、manifest digest与允许集合；unknown/multiple/mismatch安全拒绝。
- 默认`down`只停止container/network并保留named volume；删卷必须同时给出`--volumes --confirm-project <exact>`，且逐volume重新验证归属。
- 重复`down`在无owned resource时幂等成功；不得按name/glob清理其他project、镜像或用户文件。
- POSIX SIGINT/SIGTERM和Windows console interrupt进入同一bounded cancellation；命令必须报告稳定终态，未提交temp清理，已运行stack按命令合同保留或停止，不留下第二个owner。

### 3.6 开发路径兼容

- 现有Bash开发入口继续可用且不被产品文档推荐为安装方式；冻结PowerShell历史文件不修改。
- 必要Compose公共定义可抽取复用，但产品默认pull-by-digest/无Backend端口，开发默认local build/tag/可选Backend端口必须显式区分。
- `README`、使用手册和platform support文档分别说明产品入口与源码开发入口，避免用户混用同一project或volume。

## 4. 不在本批范围

- `backup`、`restore`、`upgrade`实现；CLI可保留明确“未在本版本启用”占位，但不能返回伪成功。
- 双Frontend完整设计令牌、viewport、键盘和时区产品化；只保证现有页面通过唯一edge可用。
- Windows完整Compose/浏览器矩阵、`1.9.4`升级、跨架构恢复或阶段收口。
- 远程host/TLS、daemon安装、Docker Desktop自动启动、宿主service注册、GUI安装器或自动更新器。
- 修改Phase15业务、角色、插件、告警、审计和数据查询合同。

## 5. 建议实施顺序

1. 从现有脚本提取行为清单和ownership失败条件，先建立共享runner、path/platform adapter、operation lock与fake-engine tests。
2. 实现manifest/config/state严格解析及`version/doctor`，证明全部早期错误在Docker资源创建前失败。
3. 实现`init`随机Secret、原子写入、权限/ACL和已有状态拒绝。
4. 改造product Compose digest注入和唯一edge，完成`up`与one-shot initialization。
5. 实现只读`verify/status/logs`，再实现`down`保留卷/显式删卷和中断状态机。
6. 在Linux amd64执行完整clean install/down/up，在macOS arm64执行最小完整栈；用Windows原生CLI完成不访问daemon的path/参数/fake-engine tests。
7. 更新产品/开发文档、版本、release manifest和同名实施记录，运行固定门禁后提交。

## 6. 预计直接影响文件

- `lifecycle/**`共享CLI、Docker runner、config/state、platform adapter和tests
- `deploy/compose.yaml`、新增/调整`deploy/product/**`与developer override
- release manifest/schema、bundle构建与version metadata
- 新`scripts/verify-lifecycle.sh`及`scripts/ci/`直接验证代码
- `scripts/dev.sh`、`verify.sh`、`down.sh`仅在Compose复用导致必要回归时修改；`scripts/*.ps1`不得修改
- `README.md`、`使用手册.md`、`docs/platform-support.md`和CLI help文档
- `.gitattributes`/`.gitignore`仅在交付行尾、可执行位或本地state忽略直接需要时修改
- 本批同名实施记录、`VERSION`、两个Frontend package/lockfile及受管release metadata

## 7. 批次验收标准

### 7.1 CLI与初始化

- 三个平台CLI来自同一revision；命令/flag/exit code一致，平台adapter之外无重复生命周期流程。
- `init`生成各自随机且满足长度的Secret，文件/ACL安全、原子完成；已有配置、危险目录、unsafe port、坏manifest、错误arch各有代表性Docker前拒绝。
- config/state严格解析且不含Secret；`version/doctor --json`通过schema与脱敏扫描。

### 7.2 运行与唯一edge

- Linux amd64和macOS arm64均从bundle、digest与空project完成`init/up/verify`，不使用Git/Go/Node/Python/curl或源码build。
- 仅edge发布一个IPv4 loopback端口；Browser/host不能直达Backend、admin-frontend或任何内部service，edge`/health`、`/ready`、用户/管理SPA和API正常。
- 全部container使用目标platform digest、预期network/volume、numeric user/read-only root；initialization job失败时不伪装启动成功。
- 保留卷`down/up`后用户、插件desired state和可观测历史保持或按现有合同恢复。

### 7.3 状态、日志、停止与中断

- `verify`只读前后数据摘要相同；`status/logs/doctor`只访问当前project且无Secret/私有path。
- concurrent mutation被operation lock拒绝；stale lock只在可证明条件下恢复。
- 正常down、重复down、startup failure、SIGINT/SIGTERM和Windows console unit/fake路径均得到稳定终态；无关Docker资源与用户文件快照不变。
- `--volumes`缺少精确confirm时在删除前失败，正确确认也只删除当前逐项验证volume。

### 7.4 既有回归与完成条件

- Linux完整Compose中普通用户代表社交、super_admin管理大屏/插件/三类查询/告警和普通用户403仍通过；产品化没有弱化数据库授权或局部故障隔离。
- 开发Bash入口继续通过现有self-test/必要smoke，冻结PowerShell文件hash不变。
- 根与全部受管metadata为`1.13.2`，分支为`develop/1.13.2`，同名实施记录完整；全部固定门禁通过后提交并停止。

## 8. 固定验证命令与回归范围

```bash
(cd lifecycle && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
(cd lifecycle && go test -race -count=1 ./internal/config/... ./internal/lock/... ./internal/ownership/... ./internal/runner/...)
scripts/verify-lifecycle.sh --self-test
scripts/verify-lifecycle.sh --platform linux/amd64 --clean-install
scripts/verify-lifecycle.sh --platform darwin/arm64 --clean-install
scripts/verify-compose.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.13.2 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- 两个`--clean-install`必须在对应真实host/Docker server运行；macOS结果不能由Linux cross-build代替。
- Linux入口包含现有完整Compose代表业务/管理回归；macOS本批只需完整stack、双SPA/API smoke、digest/port/volume/signal，不重复所有Phase15故障场景。
- Windows本批运行`go test`中共享逻辑和Windows adapter，或在原生host执行CLI的`version/doctor`早期路径；完整Windows daemon/Compose留给Phase-16-06，不写已支持。
- 若产品/开发Compose共享改动影响现有runner，补跑`scripts/verify-compose.sh`一次并记录风险；未影响的Frontend unit不重复。
- 提交后补充`git diff --check upstream/main...HEAD`。

## 9. 实施记录与下一批交接

完成前创建`dev/logs/Phase-16/Phase-16-02-共享产品生命周期与安全初始化闭环.md`，记录CLI命令/exit code、config/state schema、Secret生成与权限、Compose差异、两个真实host/server、唯一edge、operation lock、signal/cleanup、只读verify和全部失败轮次。

交给Phase-16-03的固定输入是稳定的唯一origin、同一CLI clean install、两个独立SPA和不变的Backend授权；backup/restore/upgrade仍未实现，不能由CLI占位命令或手工卷操作冒充。
