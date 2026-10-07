# Phase-22-01：本机启动与依赖隔离

> 状态：待实施。目标 `2.4.1`，分支 `develop/2.4.1`；分配以[总方案](Phase-22-总实施方案.md)为准。

## 1. 交付与允许文件

交付可实际运行的 make dev/dev-observe/test/stop 和显式 monitor-image。保留 scripts/dev.sh/down.sh 的容器语义。integration/e2e 在本批只登记后续接口，未实现时必须明确失败，不能报告通过。

允许文件：`Makefile`、`scripts/ci/local_development.py`、`scripts/ci/test_local_development.py`、`deploy/compose.local.yaml`、`deploy/compose.local-linux.yaml`；完成元数据 `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`；同名 `dev/logs/Phase-22/` 日志。其他应用代码、历史执行器、冻结设计不修改。

本机助手只准备环境、编译/启动进程并转发测试命令。配置由 .env.example 与调用者显式本机环境文件合成，不覆盖既有 .env。私有状态存入 .run/local/，记录工作树身份、PID/进程出生身份、实际启动命令、输入 digest 和日志；不把密码打印到终端或写入可发布回执。已有进程归属不符时失败，不抢占端口。

Compose 使用现有固定第三方版本及独立本机配置：开发默认端口 3306/6379/5672/9200，测试为 13306/16379/15673/19200；命名卷/项目按源码根身份和 dev/test 区分。测试 MySQL 数据库/账号严格 gopulse_integration，Redis DB 15；开发为独立 gopulse 数据库/DB 0。测试应用端口 18080/15173/15174，与开发 8080/5173/5174 分开。

默认启动单个 combined Backend、Worker、Indexer、用户 Vite；使用现有 host 模式，Trace 与告警评估关闭。先就绪依赖，再用现有 migrate/search-reindex 命令准备并启动本机进程。不得重建/推送业务镜像。源码 hash 覆盖实际模块与本地 replace 依赖；没有变化复用编译结果。观测增加现有 Router/Marshaller/管理 Vite 和依赖，Monitor 仅使用已显式准备的 Linux 开发镜像，不冒充本机可信插件支持。monitor-image 只调用现有 Monitor Dockerfile，输入未变复用已准备镜像。

## 2. 验证映射与固定门禁

| 对象/缺口 | 已有覆盖与最低层级 | 本批检查 |
| --- | --- | --- |
| 原容器归属/保卷合同 | scripts/dev.sh、scripts/down.sh、scripts/ci/test_verify_business.py；保留不改 | 新助手必要自测保护环境白名单、PID 身份、失败传播与进程组停止；不复制业务断言 |
| 原生进程和隔离缺口 | backend/internal/integrationtest/environment.go 已校验测试目标；新入口此前不存在 | `python3 -m unittest discover -s scripts/ci -p test_local_development.py` |
| Make/Compose 接口 | 现有 Compose 的固定第三方版本、Monitor Dockerfile | `make help`、`make -n dev dev-observe test MODULE=backend`；`docker compose -f deploy/compose.local.yaml config --quiet`，Linux overlay 同样渲染 |
| L01/L02 真实启动与停止 | Backend/Worker/Indexer 私有 probes 与 Vite；最低真实依赖层 | `make dev` → `make dev` → `make stop`；重复启动后容器/卷身份不变，源码进程健康，不新增 gopulse 业务镜像，停止后无已拥有应用进程，卷仍在 |
| L04 观测 | 既有 Monitor 官方包与 Router/Marshaller probes | 必要首次 `make monitor-image` 单独记构建；`make dev-observe`，管理 Vite 与观测 probes 成功；`make stop` |
| 直接回归 | 入口复用 Go 命令 | `make test MODULE=backend`；完成时 validate_versions 与 validate_branch |

新增 CLI/Compose 接口为待实现接口，必须在本批实现并真实验证，02 才能依赖。端口占用与子进程退出使用隔离测试进程验证，不杀既有服务。原容器正式验收不在本批重跑。

## 3. 预算与实验成本

| 阶段 | 预计分钟 | 上限分钟 |
| --- | --- | --- |
| 发现/实现 | 55 | 80 |
| 直接检查 | 15 | 20 |
| 构建/依赖准备 | 20 | 35 |
| 真实启动/重复/观测检查 | 20 | 30 |
| 日志/完成元数据/提交/归属清理 | 10 | 15 |
| 总计 | 120 | 180 |

实验为业务启动两次和观测启动一次，无容量测量窗口；每次就绪最多 180 秒，停止最多 30 秒，加依赖首次就绪最多 420 秒/次与单次 Monitor 准备。上限内不重复下载相同依赖；镜像不可用归类基础设施失败，不缩小应检查的实际链路。

## 4. 停止、接续与完成

首次基础设施失败停止后续整组；同一原因最多两次各 10 分钟诊断，计入上述预算。90/144 分钟报告进度。保留实际源码/环境/镜像输入身份、命令结果、passed/failed/not_started 和私有日志；相关输入不变的直接测试复用，真实进程环境改变时重验对应启动单元。不承诺不存在的细粒度 resume。

所有门禁通过、归属停止完成、日志如实记录后才同步 2.4.1 并创建完成提交。预算/诊断停点保持未完成并给出实际剩余清单；进一步执行须用户明确继续及修订剩余预算。
