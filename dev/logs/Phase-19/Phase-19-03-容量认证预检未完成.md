# Phase 19 容量认证顺延前预检记录

> 记录对象：原 Phase-19-03“容量认证与阶段收口”。该批次现已顺延为 Phase-19-04；本文件不是
> 新 Phase-19-03“容量验收工具修订”的完成记录。

## 执行状态

- `execution_status=incomplete`。
- 正式容量入口未调用；没有生成正式容量 evidence、`summary.json` 或 `evidence-manifest.json`。
- 未产生 `target_met` 或 `boundary_found` 能力结论。
- 根 `VERSION` 保持 `2.1.2`，Phase 19 未收口。

## 已完成检查

- 执行 `git fetch upstream`；工作树当时在 `develop/2.1.3`，基于 `upstream/main=911b13e`，执行前干净。
- 核对 `loadtest/capacity-profile.json` 与 Phase-19-02 固定配方；配方 receipt 的 digest 为 profile 规定的 `sha256:0e61a5473f72d735ab322261e312290249f39997b649837fea32bfe2c947cf14`。
- `scripts/verify-phase19-capacity.sh --self-test`：通过；Go loadtest 测试和 12 个 Phase 19 Python 测试通过，Python 编译检查通过。
- `scripts/verify-phase19-capacity.sh --calibration`：通过；输出 `formal=false`、`capacity_status=null`、`writes_formal_summary=false`。
- `python3 scripts/ci/validate_versions.py`：通过；当时版本元数据一致为 `2.1.2`。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过；仅验证 Compose 配置解析，未启动产品。
- 宿主资源、Docker server、活动容器和 Compose 版本已做只读 preflight；CPU、内存、swap、磁盘、Linux amd64、Docker server 和无运行中 Compose project 满足 profile，其余结论见限制。

## 实际变更文件

- 本预检记录；未修改产品代码、profile、runner、采样器、evidence verifier、Compose、README、能力状态、Phase 状态文件或版本元数据。

## 未通过的固定前置条件与偏差

- `python3 scripts/ci/validate_branch.py --branch develop/2.1.3 --base-ref upstream/main` 失败：根 `VERSION` 仍为 `2.1.2`。
- runner 的 host preflight 失败：实际 `docker compose version` 为 `Docker Compose version v5.5.0`，冻结 profile 只接受 Compose v2；因此未调用正式入口。
- 代码检查发现正式编排在启动新的随机 Compose project 后直接执行 load binary，没有配方生成或灌库步骤；每个新项目的数据库卷不会拥有 profile 要求的确定性数据。
- 代码检查发现每轮使用不同 Frontend 端口，而正式 CLI 只接收一个固定 `--base-url`，三次重复不能由同一次调用正确寻址。
- 异步与观测回执由整轮资源 summary 推导并写入固定布尔进展，不足以证明逐阶梯真实恢复和新鲜度。
- 当前仓库和工作目录没有可绑定的 `2.1.3` candidate manifest；没有在前置失败时构造候选或修改版本文件。

## 限制和后续项

- 上述问题由新的 [`Phase-19-03 容量验收工具修订`](../../imple/Phase-19/Phase-19-03-容量验收工具修订.md)处理；正式认证顺延到 Phase-19-04。
- 在修订和 deterministic preflight 通过前，不能声明指定环境容量、第一瓶颈或 Phase 19 完成。
