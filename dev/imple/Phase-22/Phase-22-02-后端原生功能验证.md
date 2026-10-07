# Phase-22-02：后端原生功能验证

> 状态：待实施。目标 `2.4.2`，分支 `develop/2.4.2`；依赖 01 已完成进入 main。

## 1. 交付与允许文件

交付 make integration 及普通后端验证入口，真实证明注册/登录/发帖/评论/点赞经 Outbox/RabbitMQ 到通知与搜索。旧工具中的容器安全、重启持久化、故障恢复及历史调用保持原语义。

允许文件：`Makefile`、`scripts/ci/local_development.py`、`scripts/ci/test_local_development.py`、`backend/internal/http/business_flow_integration_test.go`、`scripts/verify-business.sh`、`dev/validation/local-development-tests.md`；完成元数据 VERSION/.env.example/两前端 package.json/package-lock.json 与同名日志。复用 backend/internal/integrationtest、既有 http/post/comment/like/auth/outbox/worker/search 集成夹具与 Go API，不重写业务实现、不新建 HTTP/Python 断言框架。

integration 使用 01 的独立 test 依赖；入口显式 INTEGRATION_TESTS=1、APP_ENV=test、gopulse_integration 账号/库、Redis 15，串行包 `-p 1`，避免迁移及队列测试并发互扰。执行期间锁定测试环境，不能与 e2e 同时使用；不得运行或删除开发应用/数据库。scripts/verify-business.sh 增加明确的 --native 委托路径，旧参数和默认容器/历史路径保留。

## 2. 验证映射与固定门禁

| 对象 | 已有覆盖/缺口 | 最低层级与命令 |
| --- | --- | --- |
| 注册、登录、Cookie/权限失败 | backend/internal/auth/integration_test.go、internal/http/auth_integration_test.go、router_auth_test.go、middleware/*_test.go | 复用 Go HTTP/集成；不再新增 Python 普通断言 |
| 发帖/评论/点赞及事务 | internal/http/post_integration_test.go、comment_like_integration_test.go；internal/post/comment/like 集成测试 | 复用原生 Go；新业务链路只连接此前分别证明的边界 |
| Outbox→RabbitMQ→通知/搜索 | internal/outbox/integration_test.go、internal/worker/integration_test.go、internal/search/*integration_test.go 已证明各段；缺一个完整普通功能链路 | 新 business_flow_integration_test.go 使用真实 DB/Rabbit/ES 和原生运行组件，检查通知接收者/重复点赞不重复通知/搜索可见，唯一数据和有界等待 |
| 原工具独有覆盖 | scripts/verify-business.sh 与 scripts/ci/test_verify_business.py；备份/正式工具仍调用旧入口 | 保留容器网络/只读/制品/恢复断言，映射写入 local-development-tests.md；测试 --native 参数委托和失败码 |
| L02/L03 | 01 本机助手与测试白名单 | `make integration` → `scripts/verify-business.sh --native`；第二次只验证委托/受影响直接测试，不重复相同完整通过集 |

固定门禁：`make test MODULE=backend`；`make integration` 实际执行 `go -C backend test -p 1 -tags=integration ./...`；`python3 -m unittest discover -s scripts/ci -p test_local_development.py`；`bash -n scripts/verify-business.sh`；原脚本安全自测。验证测试项目与开发项目资源身份不重叠且开发数据未变。完成元数据/分支校验在最后执行。

## 3. 预算与实验成本

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现/实现/覆盖映射 | 55 | 80 |
| 直接检查 | 15 | 25 |
| 测试依赖准备 | 15 | 25 |
| 真实集成门禁 | 25 | 35 |
| 日志/元数据/提交/归属清理 | 10 | 15 |
| 总计 | 120 | 180 |

一次固定集成套件，单条链路有界等待不超过 60 秒，整个 Go 命令最多 15 分钟；依赖复用，不创建业务镜像，无长时实验。具体已有测试窗口保持原测试合同。

## 4. 停止、接续与完成

首次基础设施失败停止套件并复现最小边界；同因最多两次各 10 分钟，累计计入预算。90/144 分钟报告进度；保存源码/测试配置/依赖身份、真实命令结果和未执行列表。Go 套件本身不支持回执拼接；修复后按实际受影响包重验，最终固定门禁须完整成立。通过后才记录同名实际日志、同步 2.4.2、提交；停点不得声明完成，接续按总方案有限修订并由用户明确继续。
