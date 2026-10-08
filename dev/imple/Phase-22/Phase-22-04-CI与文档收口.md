# Phase-22-04：CI 与文档收口

> 状态：实现候选已完成，等待实际 CI 与日志收口。2026-10-08 修订。目标 `2.4.4`，分支 `develop/2.4.4`。

## 1. 交付与允许文件

交付按真实改动选择 CI，分开开发校验与完成自动合并校验，README 默认源码启动，覆盖映射登记全部保留工具类别。保留既有自动 PR/合并偏好；不增加一套发布执行器。

允许文件：`.github/workflows/quality-gates.yml`、`.github/workflows/auto-pr-merge.yml`、`.github/workflows/cache-warm.yml`、`.github/workflows/release-candidate.yml`、`scripts/ci/quality_scope.py`、`scripts/ci/test_quality_scope.py`、`scripts/ci/validate_branch.py`、`scripts/ci/test_validate_branch.py`、`scripts/start-development-batch.sh`、`README.md`、`backend/README.md`、`admin-frontend/README.md`、`dev/validation/local-development-tests.md`、`dev/status/capability-status.md`、本 Phase 五份方案的状态标记；完成元数据 VERSION/.env.example/两前端 package.json/package-lock.json 和同名日志。冻结设计、历史日志/证据和用户已有改动不在范围内。

quality_scope 只提供确定的路径到检查映射，不建立新验收执行器。Go 模块/本地 replace 依赖变化触发消费者；前端分别选择；backend 消费者变化触发必要业务 integration；观测采集/传输/存储/查询及插件接口变化触发 02 的 observe integration；同源代理、登录/角色与管理入口变化触发 03 对应浏览器检查；部署/Dockerfile 变化触发既有容器门禁；工具变化运行相关自测/语法；文档只做治理。未知非文档路径保守触发产品检查，不静默跳过。base 使用共同祖先比较，不能仅看最后一条提交。

现有 quality-gates 只有 Redis Exporter 独立 job，缺另外五个 Exporter 和 componentmetrics 的独立选择。本批在既有 quality-gates.yml 内补齐模块/矩阵项；backend、monitor、router、marshaller、componentmetrics 和六个 exporters 各能按自身改动执行 Go 测试。componentmetrics 等本地 replace 依赖变化必须根据实际 go.mod 选中消费者，不能只测试共享库。观测链路相关路径包括 backend/internal/observability、metricquery、logquery、eventquery、alert、相应 HTTP/配置和 monitor/router/marshaller；普通帖子页面样式变化不因此启动完整观测或容器矩阵。

真实业务和观测 job 分别调用已实现的 `make integration SCOPE=business`、`make integration SCOPE=observe`；浏览器按选择调用 `make e2e SCOPE=business` 或 `make e2e SCOPE=observe`，源码/测试环境归属及退出语义沿用 01～03。CI 需要官方插件时，在独立准备步骤确认固定 Monitor 镜像输入身份；首次无可复用镜像时显式仅构建 Monitor，输入未变时复用可用镜像/构建缓存，不产生 Backend/Router/Marshaller 业务镜像或候选 Bundle。Linux 插件真实运行失败不能改为 macOS 单元 skip 来放行。

Docker cache warm 仅实际构建输入变化时运行；Go 测试、文档和普通页面变化不隐式预热全部镜像。容器安全、完整官方插件生命周期、故障恢复、容量和正式发布验收继续保留明确入口，按相关改动或当前实施合同要求调用，不把历史 Phase 矩阵重新加入普通提交。

validate_branch 增加 `--mode development|completion`，默认 completion 保持既有完成调用语义。development 允许 base main 的已完成版本或本批目标，拒绝任意其他版本；completion 要求目标版本和本批同名完成日志。初始化脚本默认从唯一 primary remote/upstream 解析，创建分支后不改版本、不造 bootstrap 完成提交。自动 PR/合并在实际质量门禁成功且完成条件成立后运行；未完成批次只测试，不自动合并。

README 快速开始包含 make dev/dev-observe/test/integration/e2e/stop、business/observe 参数、首次工具安装和 Monitor 特殊边界，另列容器/插件/告警周期/压测/恢复/发布入口。覆盖映射同时列业务与可观测普通断言的模块测试、真实链路、管理页面验证，以及保留的容器安全/插件/恢复/容量/制品/证据和导入公共助手；仍有调用的旧程序与历史校验器不删除。没有闭合迁移依据的整文件删除不在本批授权范围。

## 2. 验证映射与固定门禁

| 对象 | 已有覆盖与缺口 | 固定检查 |
| --- | --- | --- |
| L05 job 选择 | quality-gates 现有 Go/frontend/integration/Compose job；缺按改动选择 | 新 quality_scope 原生 unittest 覆盖 docs、单模块、共享依赖、部署、工具、未知路径、空 diff |
| L05 观测模块与真实链路选择 | monitor/router/marshaller/Redis Exporter 现有 job；其余 Exporter 与 componentmetrics 缺独立项，02/03 新增 native observe 接口 | 固定选择测试覆盖每个观测模块、六个 Exporter、共享库消费者、后端指标/日志/事件/告警路径、管理代理/权限、无关业务 UI。选中实际所需 Go/集成/浏览器检查，不选完整历史矩阵；选中但没有 job 必须失败 |
| L06 开发/完成区分 | scripts/ci/test_validate_branch.py、test_validate_versions.py、test_sync_version_metadata.py | 完成默认兼容；开发 base/target 通过，其他版本失败；完成版本/日志缺失失败；初始化不提前版本提交 |
| 自动合并仍要求完成 | auto-pr-merge 原质量依赖 | 质量 workflow 暴露可合并结果，非完成分支关闭后续 mutations；检查 YAML/表达式与分支模式 |
| 文档/覆盖映射 | 原工具 imports、CI、有效计划、历史 verifier | `rg` 核对保留调用；本批 Markdown 链接/版本/接口实际一致 |
| 实际 CI | 既有 GitHub runner 与缓存 | 推送最终候选，实际受影响 job 通过；源码日常路径没有完整 Compose/Bundle/registry push |

固定门禁：相关 Python unittest（quality_scope、validate_branch、validate_versions、sync_version_metadata、local_development）；`bash -n scripts/start-development-batch.sh`；`make help` 与 business/observe CLI dry-run；02 定义的十一条 Go 模块命令、两前端 npm test（未变结果可按身份复用）；最终完成分支校验、真实 GitHub CI、git diff --check。输入未变时不在本地重复 02/03 的业务/观测真实门禁，引用实际日志和输入身份；工作流本身改变必须通过实际选中 job 验证执行命令和退出传播。各 job 在 scope 测试中可达且调用已实现入口，不以 YAML 中出现 job 名称替代真实执行成功。

## 3. 预算与实验成本

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现/业务与观测 CI、治理实现 | 45 | 70 |
| 直接检查/路径矩阵 | 20 | 25 |
| 配置渲染/环境准备 | 10 | 15 |
| 真实 CI/业务与观测阶段核对 | 35 | 55 |
| 日志/元数据/提交/归属清理 | 10 | 15 |
| 总计 | 120 | 180 |

一次最终 CI，Go/前端 job 可在独立 runner 并行；真实业务与观测分别执行固定入口，各自包含准备和有界清理，浏览器按必要改动选择，单次最终 CI 预计适配 35 分钟、上限 55 分钟。等待与失败诊断计入累计成本；无容量/长时实验，不新增虚假路径提交来反复启动 CI。源码/配置未变的 01～03 成功结果继续有效。配置选择测试是工具测试，不能冒充业务/观测/容器验收；相关部署变化仍必须触发真实容器 gate。

## 4. 停止与阶段完成

首次基础设施失败停整组，同因最多两次各 10 分钟，90/144 分钟报告。保留工作树/实际 CI revision、选择清单、raw 输出、日志与未开始 gate；不为了通过而把失败 job 从选择器移除。预算/诊断停点保留未完成，接续按总方案有限修订且用户明确继续。

全部固定门禁通过，四批真实日志及 L01～L08 结果齐备，业务/可观测覆盖映射保留独有检查，README 与实际命令相符后，才更新 Phase 状态、能力状态和 VERSION=2.4.4，创建完成提交。自动 PR/合并检查该最终提交；当前未提交用户文件始终不纳入本批。日志必须分别列出原生模块、真实观测链路、管理浏览器、保留专项检查，不能把“工具继续存在”登记为专项检查本次已通过。
