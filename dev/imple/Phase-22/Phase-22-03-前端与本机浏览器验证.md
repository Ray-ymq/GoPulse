# Phase-22-03：前端与本机浏览器验证

> 状态：待实施。目标 `2.4.3`，分支 `develop/2.4.3`；依赖 02 已完成进入 main。

## 1. 交付与允许文件

交付 make e2e：在独立 test 依赖上运行源码 Backend/Worker/Indexer/Vite，由现有 Playwright 自行注册用户、发帖、评论、点赞、通知、搜索；结束停止测试应用，保留测试依赖卷。使用 01 的环境/进程控制，不复制浏览器 runner。

允许文件：`Makefile`、`scripts/ci/local_development.py`、`scripts/ci/test_local_development.py`、`frontend/vite.config.ts`、`frontend/vite.config.test.ts`、`admin-frontend/vite.config.ts`、`admin-frontend/vite.config.test.ts`、`frontend/playwright.config.ts`、`frontend/e2e/compose-business.spec.ts`、`scripts/test-frontends.sh`、`scripts/verify-admin-frontend.sh`、`dev/validation/local-development-tests.md`；完成元数据 VERSION/.env.example/两前端 package.json/package-lock.json 和同名日志。

本机 Vite 代理使用明确 loopback 后端端口，测试使用 18080/15173，与开发环境分离；支持 CLI 指定测试端口，严格端口占用失败。默认浏览器套件复用 business.spec.ts（排除需专门重建夹具的两条 search seed 用例）和 compose-business.spec.ts 的 business 场景，后者证明实时搜索与两用户通知；不把旧 Compose 场景整体重新命名为源码已证明。

现有前端组件/状态/请求测试继续用 Vitest。verify-admin-frontend 增加 --native 入口只委托两前端测试；已有 Python AdminAcceptance 被 dashboard/visual/phase15 等导入，保持默认接口和容器专有断言。Nginx 头、UID、只读根、镜像无秘密/源码、网络及插件真实安装继续由原容器工具负责。

## 2. 验证映射与固定门禁

| 对象/缺口 | 已有覆盖 | 固定命令/最低层级 |
| --- | --- | --- |
| 组件、认证与异常状态 | 两前端 src/**/*.test.ts；现有 npm scripts | `make test MODULE=frontend`、`make test MODULE=admin-frontend`，各 npm run typecheck 与 npm run build |
| loopback/测试端口与代理 | 两前端 vite.config.test.ts | 只补本批代理/端口变化的成功与失败测试 |
| L03 真实浏览器链路 | frontend/e2e/business.spec.ts、compose-business.spec.ts；当前只缺独立源码环境入口 | `make e2e`；现有 Playwright 用例通过，所选用例无环境不足导致的 skip |
| 失败传播/归属清理/隔离 | 01/02 助手自测 | 原生浏览器失败保留 trace、返回非零并停止自己启动的应用；开发资源和数据不变 |
| 原管理工具独有检查 | scripts/ci/verify_admin_frontend.py / verify_admin_visual.py / verify_dashboard.py | 更新覆盖映射，保留旧 CLI 与必要测试，不以 Vitest 替代容器专有检查 |

完成时执行本批助手自测、Bash 语法、版本/分支校验。不重跑已通过且未变的后端集成全集；真实浏览器是新增边界，必须实际运行。若 Playwright 浏览器缺失，使用现有安装命令，不新增 acceptance 镜像。

## 3. 预算与实验成本

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现/实现 | 45 | 65 |
| 直接前端/工具检查 | 25 | 35 |
| 原生编译/依赖/浏览器准备 | 20 | 30 |
| 本机浏览器验收 | 20 | 35 |
| 日志/元数据/提交/归属清理 | 10 | 15 |
| 总计 | 120 | 180 |

两条业务测试组、既有单条浏览器窗口 60～90 秒，原生启动最多 180 秒；整体浏览器命令最多 10 分钟。无容量测量或长时实验，依赖复用，不创建业务镜像。

## 4. 停止、接续与完成

首次基础设施失败停止整组，同因最多两次各 10 分钟，均计入预算。90/144 分钟报告；保留实际源码/config/tool/浏览器版本、Playwright trace 与 passed/failed/not_started，不手改结果。修复只重验受影响的代理/用例；最终固定选择须通过。完成日志/版本 2.4.3/提交只在实际通过后产生；停点保持未完成，进一步执行须有限修订和明确继续。
