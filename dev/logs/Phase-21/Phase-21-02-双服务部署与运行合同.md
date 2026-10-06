# Phase-21-02 实施日志

## 实际完成

- 在 `develop/2.3.2`（从 `upstream/main` 创建）完成业务/平台双服务部署合同：`backend`、`backend-2` 使用 `business` 角色，`platform-api` 使用 singleton `platform` 角色；三者复用同一 Backend 制品映射，HTTP 与私有指标端口按容器命名空间复用。
- 完成同源入口路由：观测、告警、管理和插件四组基路径及子路径固定转发到 `platform-api`，业务 API、登录/current-user 和相似前缀维持原边界；保留插件上传限制与管理超时。
- 完成运行合同 v2、`platform-api → backend` 指标/制品别名、13 个封闭运行单元、实例身份、池/HTTP 限额和三目标 Backend 采集清单；同步 Compose、`.env.example`、schema、lifecycle 和 release artifact 映射。
- 保持 platform 不启动业务 Outbox sampler。由于共享 Backend 指标快照在无 Outbox 采样时默认不可抓取，新增平台角色的固定快照初始化；该中性 Outbox 字段不作为业务积压事实使用，并增加角色测试保护该边界。
- 扩展既有 `runtime_acceptance.py` 与 `verify_runtime_contracts.py` 的有限 `service-split`/evidence 接口和自测；没有新建 runner、六插件故障矩阵、容量或长时实验。
- 同步产品完成版本 `2.3.2`、两个前端包及 lockfile 根版本、运行合同产品版本、合同/验证/用户说明和能力状态。

## 实际变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `admin-frontend/package.json`
- `admin-frontend/package-lock.json`
- `backend/README.md`
- `backend/cmd/server/main.go`
- `backend/cmd/server/roles.go`
- `backend/cmd/server/roles_test.go`
- `componentmetrics/probe.go`
- `componentmetrics/runtime.go`
- `componentmetrics/runtime_test.go`
- `deploy/compose.yaml`
- `deploy/docker/frontend/nginx.conf`
- `deploy/release/BUNDLE-README.md`
- `deploy/release/README.md`
- `deploy/runtime-contracts.json`
- `deploy/runtime-contracts.schema.json`
- `dev/contracts/component-metrics.md`
- `dev/contracts/runtime-contracts.md`
- `dev/imple/Phase-21/Phase-21-02-双服务部署与运行合同.md`
- `dev/status/capability-status.md`
- `dev/validation/Phase-21/phase21-split.md`
- `frontend/package.json`
- `frontend/package-lock.json`
- `lifecycle/internal/control/compose.go`
- `lifecycle/internal/control/compose_test.go`
- `lifecycle/internal/release/manifest.go`
- `scripts/ci/release_artifacts.py`
- `scripts/ci/runtime_acceptance.py`
- `scripts/ci/test_runtime_acceptance.py`
- `scripts/ci/test_runtime_contracts.py`
- `scripts/ci/verify_runtime_contracts.py`
- `scripts/verify-compose-observability.sh`
- `scripts/verify-compose.sh`
- `使用手册.md`
- 本日志文件

## 实际命令与结果

- `git fetch upstream --prune`：成功；从 `upstream/main` 创建 `develop/2.3.2`：成功。
- Compose 配置渲染 `docker compose --env-file .env.example -f deploy/compose.yaml config --no-interpolate --format json`：通过。
- 合同/版本/分支检查：`rtk proxy python3 scripts/ci/verify_runtime_contracts.py --candidate 2.3.2`、`rtk proxy python3 scripts/ci/validate_versions.py`、`rtk proxy python3 scripts/ci/validate_branch.py --branch develop/2.3.2 --base-ref upstream/main` 和 `rtk git diff --check`：全部通过；合同登记 13 个运行单元。
- D01：`rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_release_*.py'` 通过 6 个测试；`rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_compose_acceptance_env.py'` 通过 1 个测试；合同 verifier 通过。
- D02：`rtk go -C componentmetrics test ./...`、`rtk go -C backend test ./cmd/server ./internal/config ./internal/http`、`rtk go -C lifecycle test ./internal/control ./internal/release` 全部通过。
- D03：`rtk proxy python3 -m unittest discover -s scripts/ci -p 'test_runtime_*.py'` 通过 5 个测试；`bash -n scripts/verify-compose.sh scripts/verify-compose-observability.sh && scripts/verify-compose.sh --self-test` 通过。
- D04 最终源码预检：`python3 scripts/ci/runtime_acceptance.py --suite service-split --preflight --candidate 2.3.2 --evidence .run/phase21-02-preflight-20261006-r9/receipt.json` 通过；最终 S01～S07 全部通过，运行证据为 `.run/gopulse-runtime-8eb91b9487f4/evidence.json`，独立项目资源清理完成。
- D04 回执核验：`rtk proxy python3 scripts/ci/verify_runtime_contracts.py --evidence .run/phase21-02-preflight-20261006-r9/receipt.json` 返回 `status=passed`。
- 在 D04 最终回执前还执行了两次完整通过的源码预检：r8 在元数据同步前通过；其证据未作为最终回执使用。r7 的 S01～S07 通过但受到另一个临时 Compose 项目同时出现的归属清理干扰，按基础设施失败处理并未复用。

## 偏差、诊断与修正

- 第一次真实预检的 `admin-frontend` 构建因 npm `ENETUNREACH` 失败；按合同停止整组，确认未变的 `gopulse/admin-frontend:1.13.6` 缓存后在后续源码预检中显式记录复用，未把它作为受影响产品镜像重建。
- 早期 service-split 预检分别暴露了搜索请求参数错误、插件同版本更新夹具错误、伪造版本包被 Monitor catalog 拒绝，以及历史 Monitor 夹具仍使用当前健康检查。已将搜索参数改为产品 `q` 合同，移除伪造包，改用历史 `1.10.6` Monitor 来源生成的真实旧官方包，并对历史 Monitor 使用不带当前 `--wait` 的有界启动。最终 S05 绑定旧包 `1.10.6` 和当前包 `2.3.2` 的实际 SHA-256，安装/运行/更新均通过。
- S06 首次辅助器使用了业务端口 `/metrics`；根据组件实现修正为私有 `19101/internal/v1/metrics` 并带 Backend metrics token。随后暴露 platform 无 Outbox sampler 导致共享 Backend snapshot 返回 503 的真实产品边界，加入平台角色中性快照初始化及测试；最终采集三个 Backend 身份和限额证据通过。
- 曾有一次 S01～S07 全部通过但回执因并发临时项目干扰清理检查而失败；保留失败回执，不删除或改写其 raw evidence，清空归属资源后重新执行 r8/r9。
- 一次直接用模块名调用 `python3 -m unittest scripts.ci.test_runtime_acceptance -v` 因测试模块路径不在 `sys.path` 失败；改用计划中的 discover 入口后通过。该失败不涉及产品代码。

## 已知限制与后续

- 本批完成的是 Linux amd64 源码模式有限 preflight，不是 Phase-21-03 的冻结 Bundle/manifest 正式验收；没有生成正式 publication，也没有声明 S05 的 60 秒持续窗口结论。
- 共享 MySQL、账号/会话和其他状态仍是共同故障域；本批不声明容量、观测开销、长期稳定性、Monitor/状态层 HA 或生产 TLS/SASL。
- Phase-21-03 需从完成 `2.3.2` 的干净候选执行正式 S01～S07、证据脱敏 publication 和最终阶段收口。

## 门禁状态

- D01：通过。
- D02：通过。
- D03：通过。
- D04：通过；最终回执 `.run/phase21-02-preflight-20261006-r9/receipt.json` 已由合同 verifier 核验。
- D05：通过；版本为 `2.3.2`，分支为 `develop/2.3.2`，`git diff --check` 通过。
