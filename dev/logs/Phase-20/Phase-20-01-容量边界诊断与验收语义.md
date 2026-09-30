# Phase-20-01：容量边界诊断与验收语义实施记录

- 日期：2026-09-30。
- 分支：`develop/2.2.1`；当前状态：已完成。
- 被测产品：`2.1.4`，revision `505da366a4c66f1cae47ecca12e64df1075c979f`。
- 完成版本：`2.2.1`；根 VERSION 与其余版本元数据已同步。
- 私有工作目录：`/var/tmp/gopulse-phase20-20260930/`，原始数据未发布到仓库。

## 已完成的工作

1. fetch origin 并从主线创建 allocated branch。按用户授权在 update 登记脱敏证据目录，
   通过 PR #199 以 merge commit 合入主线，保留 update。
2. 按用户选择，将 Events 判据修订为管理查询、Kafka Envelope 与 ES 文档 ID 唯一关联，
   保持产品接口，通过 PR #200 以 merge commit 合入主线。登记旧 Python 测试需要的 PYTHONPATH。
3. 编写独立 Phase 20 profile/schema、单阶梯负载入口、请求到达/终态台账、排空回执、
   事实/事件关联、独立恢复探针、资源采样及原始证据重算入口。
4. 增加 D01～D06 工具自测与新旧负载合同兼容检查。工具自测不作为真实产品验收证据。
5. 从未修改的产品源码构建并绑定九个自研镜像与七项依赖镜像的本地不可变 image ID。
   manifest 位于私有目录；未重标记或替换任何历史候选。
6. 安装仓库外验收依赖 kafka-python 2.2.15、python-snappy 0.7.3、cramjam 2.11.0。
   采样使用长驻 Kafka 客户端并绕过私网 HTTP 的宿主代理配置；不提交产品消费组 offset。
7. 真实预检发现关注/取消关注的公共成功响应为 200，而历史 profile 写为 204。
   仅修正独立 Phase 20 合同及其驱动状态判定；保留业务比例、请求序列和零非预期错误门槛。
   Phase 19 合同、历史候选及其结论保持不变。

## 实际变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `admin-frontend/package-lock.json`
- `admin-frontend/package.json`
- `dev/logs/Phase-20/Phase-20-01-evidence/baseline.json`
- `dev/logs/Phase-20/Phase-20-01-evidence/candidate-manifest.json`
- `dev/logs/Phase-20/Phase-20-01-evidence/profile.json`
- `dev/logs/Phase-20/Phase-20-01-evidence/publication-manifest.json`
- `dev/logs/Phase-20/Phase-20-01-evidence/self-tests.json`
- `dev/logs/Phase-20/Phase-20-01-evidence/self-tests.txt`
- `dev/logs/Phase-20/Phase-20-01-容量边界诊断与验收语义.md`
- `dev/phases/Phase-20-端到端性能闭环与可观测治理.md`
- `dev/phases/Plan.md`
- `dev/phases/README.md`
- `docs/capability-status.md`
- `docs/phase20-capacity-methodology.md`
- `frontend/package-lock.json`
- `frontend/package.json`
- `loadtest/cmd/load/main.go`
- `loadtest/cmd/load/main_test.go`
- `loadtest/internal/load/runner.go`
- `loadtest/internal/load/runner_test.go`
- `loadtest/internal/load/types.go`
- `loadtest/internal/load/types_test.go`
- `loadtest/internal/load/workload.go`
- `loadtest/internal/load/workload_test.go`
- `loadtest/phase20-capacity-profile.json`
- `loadtest/phase20-capacity-profile.schema.json`
- `loadtest/report.schema.json`
- `scripts/ci/phase20_diagnostic.py`
- `scripts/ci/phase20_evidence.py`
- `scripts/ci/phase20_sampler.py`
- `scripts/ci/test_phase20_diagnostic.py`
- `scripts/ci/test_phase20_evidence.py`
- `scripts/ci/test_phase20_sampler.py`
- `scripts/verify-phase20-diagnostic.sh`
- `scripts/verify-phase20-evidence.py`

## 已执行命令与结果

- `git fetch origin`；创建并同步 `develop/2.2.1`：成功。
- `git diff --check`：截至当前已执行检查通过。
- `go -C loadtest test -count=1 ./...`：多次在受影响代码变化后执行，已结束的检查通过。
- 最初按原文无 PYTHONPATH 执行旧 Python 测试：三个模块导入失败；
  以 `PYTHONPATH=scripts/ci` 执行后 14 项通过，并将环境前提登记到计划。
- `PYTHONPATH=scripts/ci:/tmp/gopulse-phase20-tooldeps python3 -m unittest
  scripts.ci.test_phase20_diagnostic scripts.ci.test_phase20_sampler scripts.ci.test_phase20_evidence`：最初 12 项通过；补充接口回归后 14 项通过。
- `python3 -m py_compile scripts/ci/phase20_diagnostic.py scripts/ci/phase20_sampler.py scripts/ci/phase20_evidence.py`：通过。
- `python3 scripts/ci/validate_versions.py`：当前完成版本 2.1.4 的六处元数据一致。
- `python3 scripts/ci/validate_branch.py --branch develop/2.2.1 --base-ref origin/main`：
  在批次尚未完成时按预期拒绝 VERSION=2.1.4；尚未取得最终完成门禁通过结果。
- 尝试 `python3 -m venv /tmp/gopulse-phase20-toolenv`：宿主缺 ensurepip，失败；
  改用 `python3 -m pip install --target /tmp/gopulse-phase20-tooldeps ...` 安装外置依赖，成功。
- 实际构建命令由 `phase19_capacity.build_preflight_images` 顺序执行九项 Compose build，
  完成后 inspect image ID 并冻结 manifest；成功。
- `scripts/verify-phase20-diagnostic.sh --preflight --manifest /var/tmp/gopulse-phase20-20260930/manifest.json
  --work /var/tmp/gopulse-phase20-20260930/preflight-r1`：退出 1，错误的管理引导服务名；安全清理通过。
- 同入口 `preflight-r2`：退出 1，Kafka 客户端心跳与 session 配置不合法；安全清理通过。
- 同入口 `preflight-r3`：退出 1，私网探针受宿主 HTTP 代理影响而超时；安全清理通过。
- 同入口 `preflight-r4`：退出 1，缺 Snappy 解压库；安全清理通过。
- 同入口 `preflight-r5`：退出 1，关注响应合同错误导致事件不能关联。
  50 RPS 负载本身执行 complete，achieved RPS=50，排空约 0.006 秒；这不代表预检通过。
  原始记录保留 105 次关注、90 次取消关注实际返回 200 的事实；安全清理通过。
- 同入口 `preflight-r6`：退出 1，插件动作要求空 POST，而验收发送了 JSON 空对象，得到 400。
  50 RPS 测量窗口非预期错误为零；安全清理通过。

- 同入口 `preflight-r7`：退出 1，已真实产生三个标记；管理查询 UTC 参数未使用 Z，返回 400，业务搜索探针错误使用 id 而非 post_id。Metrics 标记实际可见，业务轮询到截止；此结果属于工具错误。安全清理通过。

- 同入口 `preflight-r8`：退出 130，在发现 Logs 管理 Entry 省略运行环境字段后主动停止工具候选；业务、Metrics、Events 原始探针均已达到，Logs 未达到。安全清理 passed。该轮未作为通过或容量边界引用。
- 修正 Logs 公开字段投影并增加原始 ES payload 与 Envelope 的复核，15 项工具测试通过。

- 同入口 `preflight-r9`：退出 0，两个独立单元均 `complete / target_met`；原始台账、事实、标记、水位、独立恢复和清理顺序重算通过。

- 首次 `--baseline --work /var/tmp/gopulse-phase20-20260930/baseline`：退出 1，在第三单元 150 RPS 的业务查询遇到 Linux 单参数长度上限。前两单元已完整并清理，但整个入口 incomplete，不能作为可信正式基线。第三单元 failure-cleanup passed。
- 修正 SQL 由 stdin 传入，在同一 MySQL 会话中保持一致性快照；超过 128KiB SQL 的回归通过，工具测试共 16 项通过。重新冻结工具候选并启动 `preflight-r10`。

- 同入口 `preflight-r10`：退出 0，两个单元 complete / target_met，严格原始证据重算通过。
- 无损归档失败运行 preflight-r1～r8 及首次 baseline 到私有 `failed-runs.tar.gz`：260 个文件逐一 SHA256 核对通过，另存 `failed-runs-archive.json`；释放原目录副本，没有改写回执或执行全局清理。磁盘可用空间约 103GiB。历史命令路径可由此档案恢复。

- 已被 SQL 工具修订取代的 `preflight-r9` 无损归档为 `superseded-preflight-r9.tar.gz`：55 个文件逐一核对通过；本轮选定的 `preflight-r10` 保持原目录和原始字节不变。归档在正式负载测量前完成。

- 最终工具候选 `scripts/verify-phase20-diagnostic.sh --baseline --manifest /var/tmp/gopulse-phase20-20260930/manifest.json --work /var/tmp/gopulse-phase20-20260930/baseline-r2`（PYTHONPATH 使用外置依赖）：退出 0；该候选正式入口调用一次，十二个独立单元均 complete / target_met。
- `python3 scripts/verify-phase20-evidence.py --diagnostic /var/tmp/gopulse-phase20-20260930/baseline-r2`：退出 0；原始台账、精确业务事实、四维水位与恢复、采样、清理和三重复统计重算通过。输出留在私有 `final-verification-cli.json`。
- 从该基线生成固定六文件脱敏投影；`python3 scripts/verify-phase20-evidence.py --diagnostic /var/tmp/gopulse-phase20-20260930/baseline-r2 --publication dev/logs/Phase-20/Phase-20-01-evidence`：退出 0，publication_status=verified；严格比较选定工件、来源 digest 与公开文件清单。输出留在私有 `publication-verification-cli.json`。
- 在私有复制品上分别添加额外文件和 actor_id 字段：均被 verifier 按预期拒绝；原选定工件未改写。
- 最终 `go -C loadtest test -count=1 ./...`：全部包通过。
- 最终新旧 Python 测试合并执行：30 项通过（Phase 20 16 项、Phase 19 14 项）；D01～D06 实际工具自测回执完整，无跳过。
- 最终 `python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/2.2.1 --base-ref origin/main`、`git diff --check`：通过。元数据仅调整项目版本，lockfile 依赖版本不变。

## 最终基线事实

被测 candidate 为 `2.1.4 / 505da366a4c66f1cae47ecca12e64df1075c979f`；完成版本为 `2.2.1`。
逐阶梯事实、原值/中位数/范围/CV、来源 digest、依赖与工具绑定见
[脱敏基线](Phase-20-01-evidence/baseline.json)；固定公开清单见
[发布 manifest](Phase-20-01-evidence/publication-manifest.json)。

| 重复 | RPS | P95 ms | P99 ms | 排空 s | 业务恢复 s | Metrics s | Logs s | Events s |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 50 | 14.91 | 220.93 | 0.0074 | 9.64 | 8.72 | 5.91 | 5.90 |
| 1 | 100 | 33.70 | 86.89 | 0.0033 | 8.90 | 2.50 | 2.62 | 2.61 |
| 1 | 150 | 38.04 | 117.85 | 0.0040 | 7.63 | 26.17 | 25.08 | 22.22 |
| 1 | 200 | 38.72 | 133.80 | 0.0020 | 15.22 | 19.03 | 19.19 | 18.20 |
| 2 | 50 | 16.91 | 46.50 | 0.0139 | 6.20 | 4.46 | 2.57 | 2.56 |
| 2 | 100 | 41.36 | 94.36 | 0.0031 | 5.40 | 4.50 | 1.55 | 1.54 |
| 2 | 150 | 47.40 | 124.91 | 0.0030 | 6.61 | 23.03 | 15.07 | 16.05 |
| 2 | 200 | 24.83 | 57.90 | 0.0019 | 9.84 | 7.49 | 3.59 | 3.57 |
| 3 | 50 | 16.28 | 46.24 | 0.0170 | 4.44 | 2.43 | 0.50 | 1.51 |
| 3 | 100 | 40.11 | 95.06 | 0.0051 | 8.36 | 6.92 | 4.05 | 4.04 |
| 3 | 150 | 37.32 | 108.43 | 0.0034 | 6.38 | 27.14 | 23.32 | 24.24 |
| 3 | 200 | 50.69 | 158.56 | 0.0018 | 12.42 | 44.97 | 35.19 | 42.19 |

所有单元的测量窗口错误、超时、拒绝和丢调度槽均为零；负载调度滞后最大约 62.46ms，
最长排空约 0.017 秒；十二份归属清理回执 passed，资源清单恢复，无全局 prune。
四维恢复均小于 120 秒，最大为第三次 200 RPS 的 Metrics，观察上界约 44.97 秒。
正确性没有产品失败；当前已执行范围没有 unresolved 容量项，未执行范围不外推。

单次 Kafka CLI 耗时范围约 1.34～1.49 秒，全部在正式负载窗口外；持续采样改用长驻客户端。
同项目启用/停用采样各 250 次、50 RPS、五秒读取：P95 23.14/19.85ms，
验收进程 CPU 3.656/3.259 秒。该对照不是瓶颈因果或统计显著性证明。
各单元前后观测 Elasticsearch 的原始 docs/store 数据及脱敏数值已保存，作为后续生命周期输入。
严格校验结果为 execution_status=complete、capability_status=target_met、publication_status=verified。

## 偏差、限制及后续项

- 增补证据目录与 Events 唯一关联判据由用户授权，已先在 update 修订并合入 main。
- 关注响应合同修正依据真实记录；没有修改历史 Phase 19 profile 或产品代码，没有放宽错误率门槛。
- 多轮预检失败属于验收基础设施/合同错误，均保留目录，不复用其结果，不宣称产品容量边界。
- 最终工具候选的预检、十二单元正式基线及选定脱敏发布校验均已完成；失败候选没有混入最终三次重复。
- 本地 image ID 只绑定本 Docker 引擎中的候选，不替代跨宿主发布 Bundle 的证明。
- 本批没有产品优化；新旧隔离/恢复/采样语义同时变化，不能把新结果当作产品优化 A/B，不能据此证明旧超时的单一根因。
- 五秒开销对照与短窗口 Elasticsearch docs/store 增量只说明有限窗口，不证明多日稳定、完整保留期或单位资源容量。
- 02～06 尚未实施，Trace、新鲜度 SLO、生命周期、预算与最终持续验收仍待后续批次。
