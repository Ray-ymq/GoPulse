# Phase-15-04：独立管理 Frontend 与既有管理能力迁移开发记录

## 1. 完成状态与版本

- 完成日期：2026-09-12；目标版本、根 `VERSION`、两个 package/lockfile 和 `.env.example` 受管版本均为 `1.12.4`。
- 开工执行 `git fetch upstream`，从包含 Phase-15-03 的最新 `upstream/main`（`a760d3a`）创建 `develop/1.12.4`，未复用已完成的 `develop/1.12.3`。
- 本批固定本地检查、真实双镜像/同源浏览器验收及运行边界检查均通过。没有修改 Backend DTO、告警状态机或新增管理大屏、alerts/users/audit 页面。
- 原有未跟踪文件 `~` 未读取、修改或提交。

## 2. 实际迁移与文件映射

| 原路径 | 本批结果 |
| --- | --- |
| `frontend/src/components/AdminLayout.vue` | 移到 `admin-frontend/src/components/AdminLayout.vue`；四个管理链接改为独立 router 路径，返回社交使用完整文档导航 |
| `frontend/src/views/Observability{Metrics,Logs,Events,Exporters}View.vue` | 同名迁入 `admin-frontend/src/views/`，保留真实 API、strict DTO、分页、错误提示、操作确认与文件/Secret 清理 |
| `frontend/src/views/ObservabilityExportersView.test.ts` | 同名迁入管理工程，保留升级后目录刷新与失败提示测试 |
| `frontend/src/services/{observability,exporters,componentMetrics}.ts` 及专用测试 | 同名迁入管理工程；组件指标合同生成物检查路径同步更新 |
| `frontend/src/types/{observability,exporter}.ts`、`composables/usePagedObservability.ts` | 同名迁入管理工程 |
| `frontend/src/views/ObservabilityOverviewView.vue` | 删除；本批根路由授权后进入 Metrics，不把旧临时总览或静态卡片当作下一批大屏 |
| `frontend/src/styles.css` 的管理专用区域 | 移到管理工程；管理端保留所需基础按钮/表单样式，不带社交布局 |
| 用户端 router、`AuthForm`、`AuthRecoveryView`、`AppNav`、`UserAppShell`、`main.ts` | 删除管理页面依赖，统一受限 redirect，按角色登录/恢复默认分流，跨应用使用浏览器导航 |
| `frontend/e2e/observability.spec.ts` | 由新的 `frontend/e2e/admin-frontend.spec.ts` 同源双应用合同替代；旧宿主进程/临时总览故障矩阵不作为本批门禁 |
| 现有 Compose/Phase 14 E2E | 更新管理路径、直接受影响的管理员登录预期和目标配置输入；未把未运行的历史全量故障矩阵记为通过 |

新增管理工程拥有自己的 package/lockfile、TypeScript/Vite/Vitest 配置、HTML/main/App/router/styles、测试与 `dist`。仅复制无 UI 的严格 HTTP/认证响应合同，管理 `types/api.ts` 只保留用户身份和分页接口，没有社交路由、页面或服务。

`admin-frontend/src/App.vue` 在 `/users/me` 返回 `super_admin` 前不挂载 RouterView。HTTP 403 不依赖合法错误 DTO：先降低角色、等待卸载及清理，再去 `/posts`；合法认证 401 去 `/login?redirect=...`。服务故障保留可重试认证恢复界面，不以网络错误伪装注销。

## 3. 同源入口、镜像与生命周期

- 新增 `deploy/docker/admin-frontend.Dockerfile` 和 `deploy/docker/admin-frontend/nginx.conf`。
- 管理 Vite base 为 `/admin/`，路由为 `/admin/metrics`、`/admin/logs`、`/admin/events`、`/admin/plugins`；`/admin/` 授权后进入 Metrics。
- edge 对 `/admin` 返回 308 `/admin/`；旧 `/admin/observability` 及 metrics/logs/events/exporters（含尾斜线）分别重定向新路由。
- 两个 Nginx 都设置 `absolute_redirect off`。edge 保留请求 URI 代理内部 `admin-frontend:8080`；管理资产使用 `/admin/assets/` 且不存在的资产 404，管理 deep link fallback 仅指向 `/admin/index.html`。
- 用户应用 fallback 仍为用户 `index.html`；`/api/v1/` 仍走 Backend，原安装/六类 update 的 65 MiB 和超时边界保留。
- Compose 中 `frontend` 仍是浏览器唯一公布入口；`admin-frontend` 只连接 edge、没有宿主端口。管理容器内部端口为 8080。现有 Backend loopback API 端口合同未删除，但浏览器验收未使用它。
- 两个 Frontend 均为 numeric `101:101`、只读根文件系统、`/tmp` tmpfs，独立 healthcheck、10 秒 stop grace。镜像内执行各自 tests/typecheck/build，递归移除 source map；实测 SIGTERM 正常退出，退出码 0。
- `scripts/dev.sh`、`verify.sh`、Compose 验收脚本和 CI image 清单纳入管理服务，不新增第二套产品生命周期。旧 `verify-observability-ui.sh` 作为兼容入口委托新门禁。
- 本地 UI 开发时用户 Vite 的 `/admin` 代理到管理 Vite 5174；产品验收仍以 Compose edge 为准。

## 4. 有限前置实证、修正与偏差

1. 实际容器探测发现默认 Nginx 对 `/admin` 返回 `Location: http://127.0.0.1:8080/admin/`，泄漏内部监听端口。改为相对重定向后，浏览器经随机宿主端口验证新旧深链接及刷新均正确，没有落入用户 fallback。
2. 第一轮验收脚本误把“已安装插件列表”当作“六插件目录”，等待超时；修正为 `/api/v1/exporter-plugins/catalog`。失败项目 `gopulse-p1401-d09ad363c705` 已按标签清理。
3. 第二轮新旧路径、Cookie、普通用户隔离、降级清理三个浏览器用例通过；管理数据用例遇到真实 Metrics 503。原因是验收启动集合遗漏 Marshaller/VictoriaMetrics，并非页面或 Backend DTO 问题；补齐本批真实数据链路必需服务后通过。失败项目 `gopulse-p1401-7c9a46539e77` 已按标签清理。
4. 为满足制品不携带部署内部配置的合同，插件表单不再预填内部 host、Kafka Topic/consumer group、MySQL database/内部 username，改为操作者输入；协议、生命周期与校验不变，直接使用这些默认值的历史 E2E fixture 同步显式输入。公开 source/metric/route 标识仍是正常 DTO 合同，不等同于内部 origin。
5. 没有查看第三方依赖源码，没有扩展 Backend/三源告警故障矩阵、架构审计、覆盖率任务或平台支持范围。迁移涉及共享生命周期及版本校验，因此额外运行了直接相关脚本安全/版本单测和 Bash 语法检查。

## 5. 实际验收矩阵与证据

成功项目：`.run/gopulse-p1401-05e114750b6c/`，复用现有强归属 acceptance helper 的项目命名和清理，不访问日常项目数据。

| 检查 | 实际结果 |
| --- | --- |
| 未登录管理 deep link | 先请求会话、去用户登录页，super_admin 登录后恢复原 logs path |
| 新旧路径和刷新 | `/admin`、`/admin/`、四个新路由及五个旧路径都经同一 edge origin 进入管理应用 |
| Cookie / 浏览器 origin | 同 browser context 的所有 HTTP(S) 浏览器请求保持公布 origin；Cookie 均 HttpOnly，`document.cookie` 为空，无 localStorage token |
| 默认分流与非法 redirect | user 登录到 `/posts`，super_admin 到 `/admin/` 后 Metrics；外部 redirect 被拒绝，user 的 admin redirect 被拒绝；编码/控制字符/反斜线/路径规范化绕过由单测覆盖 |
| 普通用户隔离 | 四个管理路由均返回社交页，不发管理数据请求；直接四类 API 请求均 403 |
| 原有管理能力 | 六插件目录可见；真实 Redis 停止/启动；Metrics 显示真实数值，Logs/Events 显示真实记录，均取自 Backend 而非 mock |
| 401 与社交回归 | 管理请求 401 去登录；用户登录及 `/posts/new` 表单可用；会话清理后受保护社交路由进入登录 |
| 数据库降级 | 非 bootstrap super_admin 通过实际角色 API 降级；下个管理请求 403 后管理 DOM/候选 Secret 清理、转到 `/posts`，`/users/me` 仍 200 |
| 镜像和运行隔离 | 两个独立 runtime 构建，version=1.12.4，revision 与构建时 HEAD 一致；numeric user、read-only、health、无 Node/npm/map、禁止内部 URI/Topic/alias 扫描、管理无 host port、仅 edge、SIGTERM 通过 |
| 清理 | 三轮全部唯一项目的 containers/networks/volumes 检查为空；成功项目保存原有资源快照且确认未删除，验收独有 image tags 已清理 |

制品构建 revision 是本批开工 HEAD `a760d3a` 对应的完整 revision，由门禁读取 Git 后核对 OCI label；镜像使用当前工作区内容，不把该 revision 冒充提交后的新 commit hash。

成功项目保留 `build.log`、`startup.log`、`browser.log`、`results.json`、`user-gates.log`、`admin-gates.log`。浏览器为 4/4 passed（27.9 秒）。验收使用随机宿主端口，不把端口写成固定产品配置。

## 6. 验证命令与结果

| 命令 | 结果 |
| --- | --- |
| `(cd frontend && npm test)` | 16 个测试文件、61 个测试通过 |
| `(cd frontend && npm run typecheck)` | 通过 |
| `(cd frontend && npm run build)` | 通过；独立用户 bundle，不含迁出的管理页面 |
| `(cd admin-frontend && npm ci --ignore-scripts)` | 独立安装通过；镜像内另执行正常 `npm ci` |
| `(cd admin-frontend && npm test)` | 5 个测试文件、22 个测试通过 |
| `(cd admin-frontend && npm run typecheck)` | 通过 |
| `(cd admin-frontend && npm run build)` | 通过；独立 `/admin/` bundle |
| `bash scripts/verify-admin-frontend.sh --self-test` | 通过；不访问 Docker |
| `bash scripts/verify-admin-frontend.sh --existing-management` | 最终通过；双镜像、真实 Backend/browser、运行隔离与清理 |
| `python3 scripts/ci/validate_versions.py` | 通过，包含两个 package/lockfile |
| `python3 scripts/ci/validate_branch.py --branch develop/1.12.4 --base-ref upstream/main` | 通过 |
| `python3 -m py_compile scripts/ci/verify_admin_frontend.py scripts/ci/validate_versions.py` | 通过 |
| `bash -n scripts/{verify-admin-frontend,verify-observability-ui,dev,verify,verify-compose,verify-compose-observability}.sh` | 通过 |
| `(cd scripts/ci && python3 -m unittest test_validate_versions test_verify_business)` | 18 个通过；唯一失败为验收前 VERSION 仍为 1.12.3 导致 stale-image 测试先命中分支版本守卫；完成验收更新 VERSION 后单独重跑该失败用例通过，其余成功检查未重复 |
| `git diff --check`、`git diff --cached --check` | 通过；提交前再次检查新增记录的暂存 diff |

迁移后管理 HTTP 测试曾因提前处理 malformed 401 失败；已恢复既有合法认证错误合同，仅对本批明确要求的任意 403 提前撤权，最终管理端全部测试通过。

提交后还需执行计划规定的 `git diff --check upstream/main...HEAD`；该提交后动作以实际命令输出为准，不在提交前伪造完成记录。

## 7. 下一批交接与限制

- 已交付可独立构建/发布的 `/admin/`、同源会话与角色失效处理、四个原管理页及六插件目录/配置/生命周期能力。
- Phase-15-05 的大屏、alerts/users/audit UI 尚未开发；本批管理首页只是授权后进入 Metrics，不表示大屏完成。
- 保留本次本地和容器证据即可，不因上下文恢复重跑成功门禁。本次未运行历史 Phase 14 全量迁移/故障矩阵，不宣称它们重新通过。
- 本批真实验收为当前 Linux/Bash 环境；不代表 macOS/Windows、多架构、TLS 或 Kubernetes 已获得支持。
