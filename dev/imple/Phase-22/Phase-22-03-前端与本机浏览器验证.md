# Phase-22-03：前端与本机浏览器验证

> 状态：待实施。2026-10-08 修订。目标 `2.4.3`，分支 `develop/2.4.3`；依赖修订后的 02 已完成进入 main，包括观测 test 配置、隔离锁和真实数据链路。

## 1. 交付与允许文件

交付业务和管理页面共同使用的本机浏览器入口：`make e2e` 默认等同 `make e2e SCOPE=business`，在独立 test 依赖上运行源码 Backend/Worker/Indexer/用户 Vite，由现有 Playwright 自行注册用户、发帖、评论、点赞、通知、搜索；`make e2e SCOPE=observe` 使用 02 的观测 test 配置、源码 Backend/Router/Marshaller、固定 Monitor 和两套 Vite，证明管理页面能显示真实观测数据和正确处理权限。结束停止自己启动的测试应用及 Monitor，保留测试依赖卷。使用 01/02 的环境、测试锁和进程控制，不复制浏览器 runner。

允许文件：`Makefile`、`scripts/ci/local_development.py`、`scripts/ci/test_local_development.py`、`frontend/vite.config.ts`、`frontend/vite.config.test.ts`、`admin-frontend/vite.config.ts`、`admin-frontend/vite.config.test.ts`、`frontend/playwright.config.ts`、`frontend/e2e/compose-business.spec.ts`、`frontend/e2e/admin-frontend.spec.ts`、`frontend/e2e/compose-observability.spec.ts`、`admin-frontend/src/services/observability.test.ts`、`admin-frontend/src/views/ObservabilityMetricsView.test.ts`、`admin-frontend/src/views/ObservabilityLogsView.test.ts`、`admin-frontend/src/views/ObservabilityExportersView.test.ts`、`scripts/test-frontends.sh`、`scripts/verify-admin-frontend.sh`、`dev/validation/local-development-tests.md`；完成元数据 VERSION/.env.example/两前端 package.json/package-lock.json 和同名日志。不重写页面或业务实现；已有状态/失败测试没有具体缺口时原样复用。

本机 Vite 代理使用明确 loopback 后端端口，测试使用 18080/15173/15174，与开发环境分离；用户入口的 /admin 代理到测试管理 Vite，使登录和管理操作保持同源。支持 CLI 指定测试端口，严格端口占用失败。默认业务套件复用 business.spec.ts（排除需专门重建夹具的两条 search seed 用例）和 compose-business.spec.ts 的 business 场景，后者证明实时搜索与两用户通知；不把旧 Compose 场景整体重新命名为源码已证明。

观测套件固定复用 admin-frontend.spec.ts 的同源/Cookie/401、普通用户隔离、数据库权限降级三条用例，以及 compose-observability.spec.ts 的 admin 场景。先准备属于 test 环境的管理员、普通用户和用于降级的第二管理员，通过原生 admin-role 引导及原有管理 API 设置角色；稳定引导账号仅在拥有的测试数据库内复用。安装并启动 test Redis 官方插件，产生本次指标、请求日志与运行事件。准备失败立即退出，不用空数据、跳过或浏览器请求 mock 代替真实浏览器门禁。

compose-observability.spec.ts 目前把合法请求 origin 固定为 http://frontend:8080；本批改为从 GOPULSE_BASE_URL 读取，保留旧容器地址调用的兼容性。现有测试中的功能断言继续保留，仅补当前数据的页面显示断言和源码夹具所需配置，不复制历史故障、六插件或恢复场景。

现有前端组件/状态/请求测试继续用 Vitest。verify-admin-frontend 增加 --native 入口只委托两前端测试；已有 Python AdminAcceptance 被 dashboard/visual/phase15 等导入，保持默认接口和容器专有断言。Nginx 头、UID、只读根、镜像无秘密/源码、网络及插件真实安装继续由原容器工具负责。

## 2. 验证映射与固定门禁

| 对象/缺口 | 已有覆盖 | 固定命令/最低层级 |
| --- | --- | --- |
| 组件、认证与异常状态 | 两前端 src/**/*.test.ts；现有 npm scripts | `make test MODULE=frontend`、`make test MODULE=admin-frontend`，各 npm run typecheck 与 npm run build |
| loopback/测试端口与代理 | 两前端 vite.config.test.ts | 只补本批代理/端口变化的成功与失败测试 |
| L03 真实浏览器链路 | frontend/e2e/business.spec.ts、compose-business.spec.ts；当前只缺独立源码环境入口 | `make e2e`；现有 Playwright 用例通过，所选用例无环境不足导致的 skip |
| L08 管理界面状态与错误 | admin-frontend/src/services/observability.test.ts、views/ObservabilityMetricsView.test.ts、ObservabilityLogsView.test.ts、ObservabilityExportersView.test.ts；其他既有服务和图表测试 | 使用现有 Vitest 验证请求、加载、空结果、错误提示、筛选及插件操作；只补本批代理/入口变化导致的实际缺口，不新增通用页面测试框架 |
| L08 管理浏览器与权限 | frontend/e2e/admin-frontend.spec.ts、compose-observability.spec.ts；已有真实 UI/权限断言，但夹具与 origin 绑定旧容器环境 | `make e2e SCOPE=observe`；所选三条管理权限用例和 admin 场景实际通过。图表必须有本次采集的数值点，日志及事件显示本次动作的关联记录；验证插件状态、同源 Cookie、401、普通用户 403、角色降级后清除管理 DOM/候选秘密 |
| 失败传播/归属清理/隔离 | 01/02 助手自测 | 原生浏览器失败保留 trace、返回非零并停止自己启动的应用；开发资源和数据不变 |
| 原管理工具独有检查 | scripts/ci/verify_admin_frontend.py / verify_admin_visual.py / verify_dashboard.py | 更新覆盖映射，保留旧 CLI 与必要测试，不以 Vitest 替代容器专有检查 |

固定命令：`make test MODULE=frontend`、`make test MODULE=admin-frontend`；两前端分别执行 npm run typecheck 和 npm run build；`make e2e SCOPE=business`；`make e2e SCOPE=observe`。业务入口沿用既有两组用例；观测入口在私有夹具环境中分别执行 `npm exec -- playwright test e2e/admin-frontend.spec.ts --grep 'same-origin paths|ordinary user|database demotion'`，以及 `GOPULSE_ACCEPTANCE_SCENARIO=admin npm exec -- playwright test e2e/compose-observability.spec.ts`，工作目录为 frontend、配置使用现有 playwright.config.ts。这两个 SCOPE 入口尚待本批实现，未知值须失败。

完成时执行本批助手自测、Bash 语法、版本/分支校验。记录固定选择和实际用例名称/数量，不允许零用例或必要场景被 skip 后声明通过；默认入口和参数委托只用直接测试核对，不重复完整浏览器套件。不重跑已通过且未变的 02 后端业务/观测 Go 集成；真实浏览器是新增边界，必须实际运行。若 Playwright 浏览器缺失，使用现有安装命令，不新增 acceptance 镜像。

## 3. 预算与实验成本

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现/实现 | 45 | 65 |
| 直接前端/工具检查 | 20 | 30 |
| 原生编译/依赖/浏览器准备 | 20 | 30 |
| 本机业务/观测浏览器验收 | 25 | 40 |
| 日志/元数据/提交/归属清理 | 10 | 15 |
| 总计 | 120 | 180 |

两条业务测试组，加观测三条权限用例和一个 admin 场景；每个 scope 的完整浏览器命令最多 10 分钟。现有单条业务窗口 60～90 秒，观测 admin 场景最多 180 秒，指标冷启动准备最多 90 秒，日志/事件等待保留原用例窗口；原生就绪最多 180 秒。两组串行共享 test 锁，分别有界清理；无容量测量或长时实验，依赖和固定 Monitor 复用，不创建业务镜像。

## 4. 停止、接续与完成

首次基础设施失败停止整组，同因最多两次各 10 分钟，均计入预算。90/144 分钟报告；保留实际源码/config/tool/浏览器版本、Playwright trace、测试数据标识和 L03/L08 各用例的 passed/failed/not_started，不手改结果。修复只重验受影响的代理/用例；最终固定选择须通过。L03/L08、两前端固定检查、失败传播和归属清理全部实际成立后，才记录完成日志、同步版本 2.4.3 并提交；停点保持未完成，进一步执行须有限修订和明确继续。
