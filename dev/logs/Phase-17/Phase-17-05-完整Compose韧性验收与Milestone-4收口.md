# Phase-17-05 完整 Compose 韧性验收与 Milestone 4 收口实施记录

## 1. 结论

本批在真实 Linux `amd64` Docker server 上完成同一不可变 `1.14.5` 候选的
完整 Compose 韧性验收。最终候选的 runtime/artifact/Compose 门禁、Phase 17
聚合矩阵、脱敏 evidence verifier 和固定收口检查通过后，满足本批实施方案
定义的 Phase 17 / Milestone 4 技术验收条件。

本记录只记录实际执行的结果。私有工作目录中的环境文件、密码、备份载荷和
运行日志不进入仓库；仓库中只保存 allowlisted、脱敏、可校验的 evidence。

## 2. 执行环境与候选身份

| 项目 | 实际值 |
| --- | --- |
| 分支 | `develop/1.14.5` |
| 版本 | `1.14.5` |
| 最终 revision | `7e87bca62b1f94904db3c3ec6ed26d01308e7d8c` |
| Linux host | WSL2 `x86_64`，kernel `6.6.87.2-microsoft-standard-WSL2` |
| Docker server | Linux `amd64`，version `29.7.2` |
| Compose | Docker Compose `v5.5.0` |
| 前序候选 | `1.13.6`，manifest `sha256:09b59b4e818dc8116428563bc099d98e4a0cdb7fa1502d2405a4699d60735687` |
| 最终 manifest | `sha256:ad377875d2855635a47c8664d48d906653a264ab1dff11ab0cf4ca260af11c84` |
| 最终 Bundle | `sha256:e76134207ef150f9c7f231ab119c87ecb89fe4479909808531ab77b52a445acc` |
| runtime contract | `sha256:19f31816ec250e7cb2913dcb98e8ecc2e431c432cb984e234764e9d0acf64880` |
| 候选 artifact receipt | `dist/phase17-05-candidate-v5/verification-amd64.json` |
| 最终 evidence | `.run/phase17-05-final-v5/evidence/linux-amd64.json`；归档于 `dev/logs/Phase-17/evidence/Phase-17-05-evidence/linux-amd64.json` |

最终候选所有产品镜像、lifecycle image、六个 current plugin 和第三方镜像均由
manifest digest 绑定。候选只通过本机 loopback registry `127.0.0.1:15006`
进行构建/拉取，没有外部或公网发布。

## 3. 实际执行命令与结果

### 3.1 预检与脚本回归

以下脚本测试和语法检查实际通过：

```bash
python3 -m unittest discover -s scripts/ci -p test_phase17_evidence.py
python3 -m unittest discover -s scripts/ci -p test_phase17_state.py
python3 -m unittest discover -s scripts/ci -p test_verify_current_recovery.py
python3 -m py_compile scripts/ci/phase17_evidence.py scripts/ci/verify_current_recovery.py
bash -n scripts/verify-phase17.sh
python3 scripts/ci/verify_runtime_contracts.py \
  --contract deploy/runtime-contracts.json \
  --compose deploy/compose.yaml \
  --env .env.example
```

这些检查覆盖了 evidence schema、候选 receipt 复用、当前候选 recovery scope、
runtime contract 和脚本语法。最终 evidence 的脱敏、candidate binding、矩阵和
cleanup 验证实际通过：

```bash
python3 scripts/verify-phase17-evidence.py \
  --linux .run/phase17-05-final-v5/evidence/linux-amd64.json
# PASS: Phase 17 Linux amd64 evidence, candidate binding, matrix, redaction and cleanup
```

### 3.2 最终候选门禁

最终候选 v5 由当前 revision 构建到
`dist/phase17-05-candidate-v5/`，随后执行：

```bash
scripts/verify-release-artifacts.sh \
  --manifest dist/phase17-05-candidate-v5/release-manifest.json \
  --platform linux/amd64 \
  --runtime
```

结果：

```text
amd64-runtime-and-compose-passed
```

最终聚合命令为：

```bash
scripts/verify-phase17.sh \
  --manifest dist/phase17-05-candidate-v5/release-manifest.json \
  --runtime-contract deploy/runtime-contracts.json \
  --from-manifest dist/phase16-06-v3/release-manifest.json \
  --work .run/phase17-05-final-v5 \
  --evidence .run/phase17-05-final-v5/evidence/linux-amd64.json
```

结果：

```text
PASS: Phase 17 Linux amd64 final evidence and Milestone 4 technical matrix
```

最终聚合实际执行了 runtime acceptance、clean lifecycle、failure lifecycle、
直接前序 state acceptance、当前候选 backup/restore regression 和 cleanup。
同一 v5 candidate 的完整 Compose receipt 被复用，receipt 中记录了复用依据，
没有把旧候选的运行结果冒充为 v5 结果。

## 4. 最终场景结果

最终 `linux-amd64.json` 报告以下十一项场景全部 `passed`：

| 场景 | 实际结果 |
| --- | --- |
| `release-runtime-compose` | v5 artifact/runtime/完整 Compose receipt 通过并复用 |
| `runtime-contract-probes-signals` | 12 组件 Probe、配置、依赖、信号、日志和权限矩阵通过 |
| `lifecycle-clean-install` | doctor/init/up/verify/status/logs/down/up clean lifecycle 通过 |
| `lifecycle-failure-cleanup` | 配置/状态故障矩阵、预监听失败和 cleanup 通过 |
| `migration-direct-predecessor` | `1.13.6 → 1.14.5`、重复/current、dirty refusal、restore-after-failure 通过 |
| `rabbit-reliability` | Rabbit ack/retry/dead/reconnect、业务 worker 和 search side effect 通过 |
| `kafka-reliability` | Marshaller store-before-commit、retry、ownership、permanent failure、replacement persistence 通过 |
| `alert-reliability` | metrics/logs/events 三源告警、incident/audit、恢复和去重通过 |
| `complete-product-permissions` | 完整 Compose、双 frontend、roles、内部路由拒绝、六插件通过 |
| `backup-restore-current` | 当前候选 source/target API continuity、backup/inspect/restore、事实比较和 cleanup 通过 |
| `secret-ownership-cleanup` | evidence redaction、secret scan、owned cleanup、无关资源保持不变通过 |

关键恢复事实包括：恢复后的 identity、posts、comments、likes、audit、incidents、
bootstrap 和 rules 与源事实一致，并成功执行恢复后的新写入；Rabbit/Kafka/alert
相关结果由同一候选的 state receipt 和 current recovery receipt 支撑。

## 5. 失败轮次、偏差与最小修复

最终 evidence 没有复用失败候选。实际经历的失败轮次如下：

1. **candidate v1**：完整产品矩阵通过，但最终 receipt copy 触发
   `NameError: shutil is not defined`。修复 receipt copier 后，v1 evidence 作废。
2. **candidate v2**：runtime、Compose、migration、Rabbit/Kafka/alert 和浏览器矩阵
   通过；backup/restore regression 错误复用了 Phase 16 专用 `three_sources()`
   seed 规则并要求 `closure-logs/events`。改为 Phase 17 scoped current regression，
   v2 evidence 作废。
3. **candidate v3**：产品矩阵通过，但最终 Docker resource snapshot 把已清理后
   留下的 disposable anonymous volumes 当作外部资源，造成 isolation 误报。
   仅调整 isolation snapshot，忽略带 `com.docker.volume.anonymous` label 的一次性
   volume，仍精确比较 named volumes、containers 和 networks，v3 evidence 作废。
4. **candidate v4**：产品和完整聚合场景通过，但 evidence secret scanner 把
   runtime contract 中的 `${AUTH_JWT_SECRET:?…}`、`${MYSQL_PASSWORD:?…}` 等
   required-env 模板误判为真实凭据。改为扫描 JSON scalar values、跳过完整环境模板，
   同时保留真实 credential/URL userinfo 检测，并增加 fixture test，v4 evidence 作废。
5. **candidate v5**：重新构建并通过所有固定技术门禁，生成最终 evidence。

以上修复均没有放宽产品断言、删除场景或引入 Kubernetes 旁路。

## 6. 资源、Secret 与发布边界

- 初始存在且必须保持不变的产品资源为：
  `gopulse-p13-local-monitor-1`、`gopulse-p13-local-kafka-1`、
  `gopulse-p13-local-elasticsearch-1`。
- Phase 16 source registry `gopulse-p1606-registry` 仅为本地测试所需，初始为停止
  状态；最终任务清理时恢复为停止状态。
- 本任务 registry `gopulse-p1705-registry` 与
  `gopulse-p1705-registry-data` 只用于本地 loopback candidate，最终仅删除这两个
  任务自有资源，没有 global prune、广泛名称删除或触碰 Phase 16 volume。
- 私有 work 目录中的 passphrase、`.env`、backup payload、secrets 文件和 journal
  不进入 git；tracked evidence 仅包含 allowlisted JSON、checksum 和脱敏标记。
- 最终 evidence secret scan 通过；没有对公网、外部 registry 或生产环境发布。

## 7. 范围限制与 Phase 18 交接

本批支持边界为真实 Linux `amd64` Docker server 上的完整 Compose 产品。结果不
宣称支持 macOS、Windows、`linux/arm64`、Kubernetes、生产 HA、容量性能、镜像
签名/SBOM/CVE 平台或公网 registry。Phase 18 可以复用：

- 四类 runtime Probe 语义和 readiness withdrawal；
- 共享 shutdown budget、SIGTERM/SIGINT 和 consumer/producer drain 合同；
- schema target 13、Migration dirty refusal、backup/restore boundary；
- RabbitMQ/Kafka/alert ownership、retry、dedup 和恢复合同；
- dual frontend auth/role boundary、六插件监督和最终 evidence binding。

Phase 18 只改变部署位置和编排方式，不应改变上述应用合同或把 Kubernetes 作为
Phase 17 Compose 成立条件的补证。

## 8. 完成条件对照

- [x] 真实 Linux `amd64` Docker host/server 和 Compose inventory 已记录。
- [x] 单一 `1.14.5` revision/manifest/Bundle/runtime contract/image/plugin digest 已绑定。
- [x] runtime、Probe、配置、信号、错误、日志、clean/failure lifecycle 已通过。
- [x] `1.13.6 → 1.14.5` migration、Rabbit、Kafka、alert reliability 已通过。
- [x] 双 frontend、roles、internal denial、六插件和完整 business/observability Compose 已通过。
- [x] current backup/restore、Secret、ownership、cleanup 和 unrelated resource isolation 已通过。
- [x] 脱敏 evidence 已生成、归档并由 verifier 复核。
- [x] Phase 18 handoff、支持范围和已知限制已记录。
- [x] VERSION 保持 `1.14.5`；本批没有引入额外版本号。

## 9. Tracked evidence

```text
dev/logs/Phase-17/evidence/Phase-17-05-evidence/linux-amd64.json
dev/logs/Phase-17/evidence/Phase-17-05-evidence/attachments/
```

这些文件来自最终 v5 work directory 的 allowlisted attachments；私有日志、凭据、
备份内容和 recovery journal 没有复制到 tracked evidence。
