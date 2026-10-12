# Phase-24-04：管理端阶段页面与原生验收

> 状态：已实施。管理端阶段页面、原生 V1 preflight 与证据核验已通过；正式 V01～V12 仍由 Phase-24-05 执行，本日志不声明完整 V1 通过。

## 实际交付

- 分支：`develop/2.6.4`；工作树 `VERSION` 保持 `2.6.3`，符合本批短预检使用已完成版本身份的约定。
- 实现提交：`b56aa003a040e07fb806156d9e0d7e3f1b77b4e5`、`f59e40a78a022699588f477140e9dd59b6e84bdd`、`3df27de`、`5c23fa3`。
- 管理端新增 `/admin/business-stages`，支持当前/历史 epoch、command/event/post 单一身份查证、阶段质量与来源分离、精确 Logs 关联、取消及晚到响应保护；浏览器用例覆盖真实创建、阶段查询和普通用户拒绝。
- acceptance 新增 pinned candidate 身份、V1 receipt/raw validator、preflight/Verify Evidence 入口和仅验收 fault fixture；默认 business scope 保持不变。

## 候选、制品与回执

- 候选目录：`.run/phase24-04/preflight-candidate-5c23fa3-v1`；revision `5c23fa3bcd35d0fe3cc0c5680fb9a4d3ab704ff4`，版本 `2.6.3`。
- `release-manifest.json` SHA-256：`sha256:d615d0c71249236f2ce00ba1c42b64b66104f47580a3f21ae236a8f7958fa9ab`。
- Bundle archive SHA-256：`sha256:9a540c19cae066eea68e6c26e3c88aee8a559b4ce2202c62a3c3d50049b01699`；manifest 内的 Bundle payload SHA-256 单独为 `sha256:6ac378cfff997442c3b7e5254ba88a83c53661dc8b7f6d6499b5d40d00fcc1f0`，两者语义未混用。
- package 的 `verification-amd64.json` 为 `amd64-runtime-and-compose-passed`；实际包含 Compose smoke/observability 和 lifecycle clean/failure matrix，候选未发布。
- 本批自有 registry 为 `gopulse-phase24-04-registry`，镜像 `registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373`，绑定 `127.0.0.1:15024`；最终验证后已按归属移除。
- V1 回执：`.run/phase24-04/preflight-receipt-5c23fa3-v1.json`；status `passed`、mode `preflight`、source `pinned-candidate`、contract `phase24-v1`、顶层及 V01 cleanup 均为 `true`。raw 目录为 `.run/phase24-04/preflight-receipt-5c23fa3-v1.json.raw-444e1fde7d69`，目录权限 `0700`，V01 raw 文件权限 `0600`。
- V01 状态为 `passed`；身份为 `data_epoch=9306fd41-c616-11f1-b8ca-5a336841872b`、`command_id=6a537436-4a90-4786-a4af-ca6cc8a56085`、`event_id=18da46e4-95ca-441b-89ed-60c0af2a462b`、`post_id=1`。raw SHA-256 为 `sha256:910cd929f09f6ad79d8fad06428322e3def21b13272fd6a9e34e6d2d2de57dba`。
- `VERIFY_EVIDENCE=1` 已在不启动 Docker、不补写案例的条件下通过，确认候选、manifest、Bundle archive、身份、raw hash、窗口/状态和清理事实一致。

## 验证结果

- `admin-frontend`：Vitest `60/60`，typecheck、build、lint 通过；`make verify MODULE=admin-frontend BASE_REF=origin/main` 通过。
- `frontend`：19 个测试文件、76 个测试，typecheck、build、lint 通过；`make verify MODULE=frontend BASE_REF=origin/main` 通过。
- `acceptance`：`make verify MODULE=acceptance BASE_REF=origin/main` 通过，存量 lint `24`、新增 `0`，test/race/vet 通过；最终 Bundle 候选上的 acceptance build/runtime gate 通过。
- `devtools`：`make verify MODULE=devtools BASE_REF=origin/main` 通过，存量 lint `24`、新增 `0`，test/race/vet 通过。
- `backend`：`make verify MODULE=backend BASE_REF=origin/main` 通过，存量 lint `107`、新增 `0`，go test/race/vet/build 通过。
- `make e2e SCOPE=observe` 通过：3 个管理端回归、1 个 `business-stages` 浏览器用例、1 个 Compose observability 用例；`make check-compose` 通过。
- `make docs-check BASE_REF=origin/main`、`python3 ci/check_docs.py --base-ref origin/main`、`make check-compose`、分支校验、版本校验和 `git diff --check` 均通过。

## 偏差与限制

- 最初一次候选 package 的 lifecycle failure matrix 受 Elastic registry 瞬时索引/鉴权状态影响返回 code 10；未修改候选。对同一固定候选补充固定 digest pull 后，复用候选执行 `gopulse-package verify --runtime`，完整通过并写入上述 verification receipt。
- preflight 只执行 V01 短链路并使用 `mode=preflight`；没有运行正式 60 秒故障矩阵、容量/长期实验或 V02～V12，因此不把本批结果表述为完整 V1 通过。
