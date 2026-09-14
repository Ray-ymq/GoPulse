# Phase 16 Linux amd64 最终产品矩阵

## 候选生产与冻结

`develop/1.13.6` 继承已合入主线的 `1.13.5`。允许在验收前从已提交源码构建并发布一次候选。产品安装只使用 Docker/Compose 与独立解压的 Bundle；验收工具容器与开发机的构建工具不是产品宿主依赖。

使用 `scripts/ci/release_artifacts.py build --registry <registry>/gopulse --output <candidate> --platform linux/amd64` 生产候选。用相同 Git revision 构建 `deploy/docker/acceptance.Dockerfile` 并推送验收镜像，记录其 registry digest 和 image ID。最终矩阵和 Compose 固定门禁使用 `GOPULSE_ACCEPTANCE_IMAGE=sha256:…`，不得再构建验收镜像或产品制品。

候选冻结后如产品/验收实现出现直接阻断，保留失败记录、修复并提交新 revision，重新生成新候选；不可原地覆盖已冻结 manifest、Bundle 或混用不同候选通过证据。

## 固定入口

先在对应候选 revision 的干净工作树执行：

```bash
GOPULSE_ACCEPTANCE_IMAGE=sha256:… scripts/verify-release-artifacts.sh \
  --manifest <candidate>/release-manifest.json --platform linux/amd64 --runtime
```

该命令包含完整 `scripts/verify-compose.sh`，无需单独重复。成功生成 `verification-amd64.json`。将 Bundle 解压至独立路径（支持空格），同时保留原 tar.gz 供校验；不能直接在构建输出目录安装。

验收 Compose 入口为 `deploy/phase16-acceptance.yaml` 的 `acceptance` profile，避免开发 Compose 的服务依赖、构建定义和私密环境污染矩阵 runner：

```bash
# 全部路径为 Linux 绝对路径；work 权限 0700，work/tmp 已存在。
export GOPULSE_ACCEPTANCE_REF='<registry>/gopulse/acceptance@sha256:…'
export GOPULSE_ACCEPTANCE_IMAGE='sha256:…'
export GOPULSE_TOOL_UID=$(id -u) GOPULSE_TOOL_GID=$(id -g)
export GOPULSE_SOCKET_GID=$(stat -c %g /var/run/docker.sock)
export GOPULSE_PHASE16_BUNDLE='<独立 Bundle 路径>'
export GOPULSE_PHASE16_WORK='<私有运行目录>'

docker compose -p gopulse-phase16-matrix -f deploy/phase16-acceptance.yaml \
  --profile acceptance run --rm --no-deps acceptance phase16 \
  --manifest "$GOPULSE_PHASE16_BUNDLE/release-manifest.json" \
  --work "$GOPULSE_PHASE16_WORK" \
  --artifact-receipt "$GOPULSE_PHASE16_WORK/verification-amd64.json" \
  --evidence "$GOPULSE_PHASE16_WORK/evidence/linux-amd64.json"
python3 scripts/verify-phase16-evidence.py \
  --linux "$GOPULSE_PHASE16_WORK/evidence/linux-amd64.json"
```

Artifact receipt 必须来自上述同 revision、同 manifest 固定命令。独立 `release-manifest.json`、生命周期 JSON/日志、恢复 JSON 与最终 evidence 应整组交付，不能只复制主 JSON。实际受支持平台只有 Linux amd64，不声明 macOS、Windows、arm64、Kubernetes 或跨版本升级。

## 场景、恢复与证据

Runner 串行运行正式生命周期 clean install 与 Linux 故障、A → backup A → B → backup B → C、恢复后继续写入/六插件采集/双前端、三源告警、恢复失败和 scoped cleanup。产品操作均通过正式生命周期/API/浏览器入口；只读数据库对照和独立备份审计只用于事实验证，不能用 SQL 构造成功业务事实。`backup-fixture` 在验收镜像中预编译，运行阶段不调用宿主 Go。

每个场景有实际起止时间、同 manifest/revision/Bundle checksum、结果、事实及脱敏附件引用；恢复 project/token 仅公开 hash。最终 JSON 只有聚合校验成功后才原子发布完成标志。证据合同由 `scripts/ci/phase16_evidence.py` 实现；测试数据只用于拒绝逻辑测试，不是运行证据。

私有 `matrix-progress.json` 和 `recovery/acceptance.json` 支持续跑同候选未完成步骤；后者含登录凭据，禁止提交。失败时保留受管安装以便诊断，不进行全局 prune。先查脱敏日志与正式 `status`/diagnostics，不手工制造 ready。续跑必须保持原候选、环境和外部资源稳定，只有已通过且输入未改变的检查可复用。

备份格式和持续恢复合同见 `dev/phase16-current-recovery.md`。私有加密备份与口令分开保管，不随公开 evidence 发布。可拉取的 loopback registry 仅代表本机候选发布，不等于公网发布。Phase 16 的主线合并条件与 Phase 17 交接状态必须分别报告，不提前声明 Milestone 4。

诊断续跑说明：若同候选完整 Compose 门禁失败，可使用现有 `scripts/verify-compose.sh --keep` 保留本次受管项目和原始快照，额外挂载私有 Playwright 输出目录只用于保留失败 trace。通过后必须执行原 runner 的 `assert_project_ownership`、同 project 的 Compose down/volumes、`cleanup_acceptance_images`、`assert_snapshot_preserved`；不省略清理条件。可复用该候选此前已成功且输入未变的 artifact metadata/lifecycle runtime 检查，与实际通过的完整 Compose/cleanup 合并生成同结构 receipt；须在实施记录列出原始命令、日志、输入 digest 和合并依据，不能以其他候选结果或部分 Compose 用例代替。

## 已验收的 1.13.6 交付与宿主时间要求

最终已验收候选 revision 为 `a76089fc5097ebf4d218644e66da034472c1ebba`。整组公开证据位于 `dev/logs/Phase-16/Phase-16-06-evidence/`，其中 `release-manifest.json` 为权威候选，`linux-amd64.json` 为机器校验入口。Bundle archive SHA-256 为 `2016df48b4097509da3dc9fa9386a68811370bfdee0207f396fd70fa46c2c128`；本机产物路径 `dist/phase16-06-v3/gopulse-1.13.6-bundle.tar.gz`。本机 loopback registry 已完成同 digest 晋升，不代表外部发布。

安装、生命周期、备份恢复仍使用 Bundle 正式入口及前述当前数据配方。宿主需保持时间连续且准确：本次 WSL2 环境中多个发行版 NTP 与系统 PHC chrony 同时调节共享时钟导致回拨、新 JWT 暂时无效。临时单一同步来源下完整产品矩阵通过；原配置已于验收后恢复。若再次出现登录后 401/时间早于安装时间，先采样 wall/monotonic 与检查共享宿主同步进程，保留诊断；不要修改产品鉴权、数据库时间或伪造 ready。任何跨发行版时钟调整需先取得宿主 owner 同意。

Phase 17 可直接消费本候选及同 manifest A/B 两份私有加密备份；公开 recovery JSON 中有独立检查及 checksum，口令不得进入公开交接材料。该交付不要求 Kubernetes，不提供跨版本升级或非 Linux amd64 产品支持承诺。
