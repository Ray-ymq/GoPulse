# Phase-22-04：CI 与文档收口

> 状态：待实施。目标 `2.4.4`，分支 `develop/2.4.4`；依赖 03 已完成进入 main。

## 1. 交付与允许文件

交付按真实改动选择 CI，分开开发校验与完成自动合并校验，README 默认源码启动，覆盖映射登记全部保留工具类别。保留既有自动 PR/合并偏好；不增加一套发布执行器。

允许文件：`.github/workflows/quality-gates.yml`、`.github/workflows/auto-pr-merge.yml`、`.github/workflows/cache-warm.yml`、`.github/workflows/release-candidate.yml`、`scripts/ci/quality_scope.py`、`scripts/ci/test_quality_scope.py`、`scripts/ci/validate_branch.py`、`scripts/ci/test_validate_branch.py`、`scripts/start-development-batch.sh`、`README.md`、`backend/README.md`、`admin-frontend/README.md`、`dev/validation/local-development-tests.md`、`dev/status/capability-status.md`、本 Phase 五份方案的状态标记；完成元数据 VERSION/.env.example/两前端 package.json/package-lock.json 和同名日志。冻结设计、历史日志/证据和用户已有改动不在范围内。

quality_scope 只提供确定的路径到现有 job 映射。Go 模块/本地 replace 依赖变化触发消费者；前端分别选择；backend 消费者变化触发真实 integration；部署/Dockerfile 变化触发既有容器门禁；工具变化运行相关自测/语法；文档只做治理。未知非文档路径保守触发产品检查，不静默跳过。base 使用共同祖先比较，不能仅看最后一条提交。Docker cache warm 仅实际构建输入变化时运行。

validate_branch 增加 `--mode development|completion`，默认 completion 保持既有完成调用语义。development 允许 base main 的已完成版本或本批目标，拒绝任意其他版本；completion 要求目标版本和本批同名完成日志。初始化脚本默认从唯一 primary remote/upstream 解析，创建分支后不改版本、不造 bootstrap 完成提交。自动 PR/合并在实际质量门禁成功且完成条件成立后运行；未完成批次只测试，不自动合并。

README 快速开始包含 make dev/test/integration/e2e/stop、首次工具安装和 Monitor 特殊边界，另列容器/压测/恢复/发布入口。覆盖映射列出原普通断言的模块测试、保留容器安全/插件/恢复/容量/制品/证据及导入公共助手；仍有调用的旧程序与历史校验器不删除。没有闭合迁移依据的整文件删除不在本批授权范围。

## 2. 验证映射与固定门禁

| 对象 | 已有覆盖与缺口 | 固定检查 |
| --- | --- | --- |
| L05 job 选择 | quality-gates 现有 Go/frontend/integration/Compose job；缺按改动选择 | 新 quality_scope 原生 unittest 覆盖 docs、单模块、共享依赖、部署、工具、未知路径、空 diff |
| L06 开发/完成区分 | scripts/ci/test_validate_branch.py、test_validate_versions.py、test_sync_version_metadata.py | 完成默认兼容；开发 base/target 通过，其他版本失败；完成版本/日志缺失失败；初始化不提前版本提交 |
| 自动合并仍要求完成 | auto-pr-merge 原质量依赖 | 质量 workflow 暴露可合并结果，非完成分支关闭后续 mutations；检查 YAML/表达式与分支模式 |
| 文档/覆盖映射 | 原工具 imports、CI、有效计划、历史 verifier | `rg` 核对保留调用；本批 Markdown 链接/版本/接口实际一致 |
| 实际 CI | 既有 GitHub runner 与缓存 | 推送最终候选，实际受影响 job 通过；源码日常路径没有完整 Compose/Bundle/registry push |

固定门禁：相关 Python unittest（quality_scope、validate_branch、validate_versions、sync_version_metadata、local_development）；`bash -n scripts/start-development-batch.sh`；`make help` 与必要 CLI dry-run；`make test MODULE=backend`、两前端 npm test（未变结果可按身份复用）；最终完成分支校验、真实 GitHub CI、git diff --check。依赖/应用未变时不重跑 02/03 的成功真实门禁；引用实际日志和输入身份。

## 3. 预算与实验成本

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现/CI与治理实现 | 50 | 75 |
| 直接检查/路径矩阵 | 20 | 30 |
| 配置渲染/环境准备 | 10 | 15 |
| 真实 CI/阶段核对 | 30 | 45 |
| 日志/元数据/提交/归属清理 | 10 | 15 |
| 总计 | 120 | 180 |

一次最终 CI，等待与失败诊断计入累计成本；无容量/长时实验。源码/配置未变的 01～03 成功结果继续有效。配置选择测试是工具测试，不能冒充业务/容器验收；相关部署变化仍必须触发真实容器 gate。

## 4. 停止与阶段完成

首次基础设施失败停整组，同因最多两次各 10 分钟，90/144 分钟报告。保留工作树/实际 CI revision、选择清单、raw 输出、日志与未开始 gate；不为了通过而把失败 job 从选择器移除。预算/诊断停点保留未完成，接续按总方案有限修订且用户明确继续。

全部固定门禁通过，四批真实日志齐备，映射保留独有覆盖，README 与命令相符后，才更新 Phase 状态、能力状态和 VERSION=2.4.4，创建完成提交。自动 PR/合并检查该最终提交；当前未提交用户文件始终不纳入本批。
