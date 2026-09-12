# Phase-16-03：统一登录与双 Frontend 产品体验闭环实施方案

> 执行序号：3 / 6。目标版本`1.13.3`、开发分支`develop/1.13.3`及前后批次以[Phase-16-总实施方案.md](Phase-16-总实施方案.md)为准。

## 1. 批次目标

在Phase-16-02唯一edge与共享产品生命周期上，把已经物理拆分的用户Frontend和管理Frontend收敛为一致但不混同的产品体验：

```text
唯一 /login + 同源 HttpOnly Cookie
  → Backend /users/me读取数据库当前角色
       ├─ user        → 用户信息架构 /posts...
       └─ super_admin → 管理信息架构 /admin/...

共享：design tokens、基础控件、异步状态、focus、时间格式
独立：entry、router、layout、业务view、bundle、runtime image
```

本批不更改Phase 13～15业务/API/告警/插件范围，也不把两个应用重新合并成单bundle。

## 2. 前置条件

- Phase-16-02已合入最新`upstream/main`，产品bundle可在Linux amd64与macOS arm64通过唯一edge启动，Backend不再从产品profile发布宿主端口。
- fetch后从最新主线创建本批分支；根和两个Frontend当前版本为`1.13.2`。
- 有界列出两个应用现有route、layout、全局CSS、重复基础控件、loading/empty/error/expired状态、时间格式、危险操作和候选Secret清理点。
- 按总方案§17确认浏览器固定viewport、系统timezone切换/注入方法、session过期触发、键盘Tab顺序和edge deep-link行为。
- 现有用户/管理API strict DTO和数据库权威授权保持冻结；发现公共DTO实际缺失页面状态时先记录直接需求，不借UI批次改写业务语义。

## 3. 实施范围

### 3.1 共享前端基础包

- 建立一个构建期共享包，仅包含CSS design tokens、基础Button/Form/Notice/Loading/Empty/Error/Expired状态组件、focus helper和RFC3339时间格式工具。
- tokens固定颜色、字体栈、字号、间距、圆角、边框、阴影、focus ring、状态色、窄屏断点和动画降级；两应用不再复制根变量和基础控件定义。
- 共享包不包含router、layout、认证状态、业务service、页面view、管理员DTO或社交DTO；任何共享扩展必须被两个应用真实使用。
- 两个应用分别测试、typecheck、build和生成独立assets；用户bundle扫描不得包含管理page/module字符串，管理bundle不得包含发帖/关注/收藏等用户view代码。

### 3.2 统一登录、恢复与角色分流

- `/login`继续只由用户Frontend提供。登录或会话恢复后，`user`默认进入`/posts`，`super_admin`默认进入`/admin/`。
- 未登录直达合法用户/管理deep link时保留同origin绝对path；登录后只有角色允许的路径可恢复。scheme/host、`//`、反斜线、控制字符、编码绕过和user的admin目标拒绝。
- 管理Frontend在mount任何管理view或发出管理数据请求前完成`/api/v1/users/me`恢复；401回到统一login，非`super_admin`回到用户端。
- 任一管理API返回403时清除已渲染管理数据、分页cursor、候选插件Secret和未提交表单，再转入用户端；不把仍有效的普通用户session误清除。
- 当前非bootstrap super_admin被降级后，下一个管理请求立即触发上述路径；bootstrap保护仍由Backend/MySQL执行。
- 两端的expired/recovery错误使用一致状态和重试，但不显示原始Backend/upstream错误、Cookie值或内部地址。

### 3.3 页面状态与交互

- 用户端代表页面固定覆盖：时间线/帖子详情、搜索/Following、通知、编辑表单。管理端代表页面固定覆盖：大屏、插件、告警、Metrics/Logs/Events表格或列表。
- 每个异步区域区分initial loading、refreshing、empty、partial unavailable、validation error、request error和session expired；旧数据刷新失败时按页面合同标明stale，不静默显示为最新。
- 表单提交和危险操作显示pending并防重复；成功/失败消息与对应控件关联，候选Secret在成功、失败、取消、路由离开和权限失效时清理。
- 管理大屏保持分区部分降级，不因一个VM/ES/Monitor请求失败替换整个页面；用户社交页面不因可观测错误出现管理状态。
- 不用静态fixture、硬编码统计或前端推导替代真实Backend数据。

### 3.4 响应式、键盘与可访问基础

- 固定`390x844`、`768x1024`、`1440x900`三个viewport，两个应用均无页面级横向溢出；导航、主要操作、表格/卡片和错误状态在窄屏可到达。
- 所有link/button/input/select/textarea按视觉顺序可用Tab到达并有可见`focus-visible`；disabled控件不可误触且状态不只靠颜色表达。
- route变化后把focus移到主标题或明确容器；局部refresh不抢走当前控件focus。错误summary可聚焦并关联字段错误。
- 支持`prefers-reduced-motion`，loading动画不成为唯一状态指示；语义heading、label、button name和live status满足代表性浏览器可访问检查。
- 本批只修复直接影响验收的语义和focus问题，不启动全面WCAG认证或全站组件重写。

### 3.5 时间与时区

- API时间继续只接受带offset的RFC3339；共享工具对合法时间统一显示浏览器local timezone，并在`datetime`属性保留机器可读原值。
- 管理Logs/Events/Alerts/Audit和用户Post/Notification使用同一日期/相对时间策略，界面可识别当前timezone，不把本地时间误标为UTC。
- invalid/missing时间显示固定unknown状态并保留安全错误，不出现`Invalid Date`、浏览器locale异常或对无offset字符串猜测。
- 以UTC和一个非UTC时区各验证跨日/排序代表项；排序和cursor仍由Backend事实决定，Frontend格式化不改变查询边界。

### 3.6 Edge与安全headers直接收口

- 验证`/`、`/login`、用户deep link、`/admin`、管理deep link、旧管理重定向和`/api/v1`在唯一origin下各自fallback正确。
- 两个Frontend runtime返回相容的基础安全header和cache策略；HTML入口可更新、带hash asset可长期缓存，用户/管理asset base不互相覆盖。
- bundle、DOM、error、source map和browser network不包含Secret、Cookie、内部service、host path或另一个应用不应包含的业务模块。

## 4. 不在本批范围

- 修改关注、收藏、帖子、插件、Metrics/Logs/Events、告警、角色或审计Backend契约。
- 将两个应用合并为monorepo单bundle、共享router/layout/business view，或新增第三个Frontend。
- 品牌重设计、国际化体系、用户可配置主题/时区、完整WCAG认证、视觉回归平台或所有页面截图矩阵。
- backup/restore、`1.9.4`升级、Windows完整运行或三宿主阶段收口。
- 公网域名、TLS、Ingress、CDN或远程Cookie策略。

## 5. 建议实施顺序

1. 记录重复tokens/控件/状态和两端route/bundle边界，确定共享包最小public API。
2. 先迁移tokens、基础控件和状态组件，分别保持两端unit/typecheck/build通过。
3. 收敛login/redirect/recovery/403降级和候选Secret清理，运行角色负向矩阵。
4. 处理三个viewport、keyboard focus、reduced motion和代表页面异步状态。
5. 统一RFC3339/timezone工具，并验证UTC与非UTC跨日显示不改变Backend排序。
6. 通过唯一edge执行两端真实浏览器闭环、bundle/network/redaction和安全header检查。
7. 更新版本、产品UI文档和同名实施记录，在最终diff运行固定门禁后提交。

## 6. 预计直接影响文件

- 新共享前端基础目录（例如`web-shared/`）及其tokens/components/time/tests
- `frontend/src/**`与`admin-frontend/src/**`中直接迁移的styles、layout、auth、router和代表view
- 两个Frontend package/lockfile、Vite/Vitest/TypeScript配置和Docker build COPY边界
- `deploy/docker/frontend/nginx.conf`、`deploy/docker/admin-frontend/nginx.conf`及必要edge cache/header配置
- Frontend E2E与新`scripts/verify-product-ui.sh`、`scripts/ci/`直接检查
- `README.md`/用户文档中统一入口、响应式和时间显示说明
- 本批同名实施记录、`VERSION`、两个Frontend和release metadata

## 7. 批次验收标准

### 7.1 工程与bundle边界

- 共享包只含批准的design/status/time基础；两个应用分别test/typecheck/build并生成独立runtime image。
- 用户bundle不含管理views/routes/DTO，管理bundle不含社交views/routes；无source map、Secret、内部host或构建路径。
- token/基础控件不存在两份漂移定义；两个应用仍有独立entry/router/layout和信息架构。

### 7.2 登录、状态与授权

- user/super_admin从同一`/login`进入正确应用，合法deep link/刷新恢复；外部或角色不匹配redirect拒绝。
- 未登录401、普通用户管理API403、超级管理员成功和角色降级后即时403由真实Backend证明；Frontend路由拒绝不替代API拒绝。
- loading/empty/error/partial/expired与插件Secret清理在代表页面可观察；失败不泄漏raw error或保留敏感DOM。
- 大屏一个分区失败时其他分区可用，用户社交主路不受观测故障影响。

### 7.3 Viewport、键盘与时间

- 390、768、1440三个viewport下两端代表页面无全局横向溢出，导航/主要动作/表格或卡片可用。
- 完整键盘路径能登录、发布代表操作、进入管理页和完成一个安全管理操作；focus可见、route/错误focus正确、reduced motion有效。
- UTC和非UTC环境中用户/管理代表时间显示一致、timezone可解释、机器时间保留；invalid时间安全降级且排序未改变。

### 7.4 运行与完成条件

- 唯一edge的用户/管理deep link、asset cache、旧route redirect、API proxy和security header通过；Browser network不访问内部origin。
- Linux amd64运行完整角色/业务/管理浏览器矩阵，macOS arm64运行两个viewport与timezone代表场景；无需重复本批不影响的backup/upgrade。
- 根与受管metadata为`1.13.3`，分支为`develop/1.13.3`，同名实施记录完整；全部固定门禁通过后提交并停止。

## 8. 固定验证命令与回归范围

```bash
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
(cd admin-frontend && npm test -- --run)
(cd admin-frontend && npm run typecheck)
(cd admin-frontend && npm run build)
scripts/verify-product-ui.sh --self-test
scripts/verify-product-ui.sh --browser --viewports 390x844,768x1024,1440x900 --timezones UTC,Asia/Shanghai
scripts/verify-lifecycle.sh --existing-product
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.13.3 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- Browser入口必须使用产品唯一edge和真实Backend数据，至少覆盖user/super_admin、401/403、角色降级、一个用户写操作、一个插件/告警操作和管理分区失败。
- 一个完整viewport/角色矩阵在Linux运行；macOS只补arm64 runtime、窄屏/桌面和非UTC差异，不在本批等待Windows全量矩阵。
- 如果共享包改变Docker build context或Nginx asset路径，补跑两个image digest/runtime检查；不重跑无影响的六插件故障全矩阵。
- 提交后补充`git diff --check upstream/main...HEAD`。

## 9. 实施记录与下一批交接

完成前创建`dev/logs/Phase-16/Phase-16-03-统一登录与双Frontend产品体验闭环.md`，记录共享/独立文件边界、route/redirect矩阵、页面状态、viewport、keyboard focus、timezone、bundle/network扫描、实际browser环境、命令结果和偏差。

交给Phase-16-04的固定输入是两个稳定独立应用、统一登录与产品edge；下一批只处理持久数据、Secret和恢复，不应在backup工作中继续扩展UI设计。
