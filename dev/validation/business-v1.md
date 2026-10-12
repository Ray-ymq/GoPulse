# Business V1 原生验收合同

本文件登记 Phase-24-04 提供的工具边界；V01～V12 的正式动作、阈值和完成判断由
[Phase-24-05](../imple/Phase-24/Phase-24-05-V1冻结验收与阶段收口.md)冻结。本批实现工具和短预检，
不产生完整 V1 能力结论。

## 入口

旧入口保持不变：

```bash
make verify-business
```

V1 只接受同一候选的完整身份：

```bash
make verify-business SCOPE=v1 \
  MANIFEST=/private/candidate/release-manifest.json \
  RECEIPT=/private/evidence/preflight.json \
  PREFLIGHT=1 BUDGET=30m
```

`MANIFEST` 必须与当前 `VERSION`、HEAD revision、Bundle payload、Bundle detached checksum、runtime contract
和所有镜像 linux/amd64 digest 一致。工具拉取 digest 后只使用 candidate 环境变量；缺 manifest、候选漂移或
任一 asset/image/plugin 校验失败都会在启动栈前失败。

`PREFLIGHT=1` 运行 owned Compose 的 smoke、账号准备和新的真实浏览器链路：创建一条帖子，进入业务阶段页，
验证精确 Logs 跳转及普通用户拒绝，并写入唯一 `V01` raw。它不是正式矩阵；formal 模式会执行完整 V01～V12
案例计划：V01 沿用浏览器基线，V02～V12 使用候选栈上的受限原生 HTTP/Compose/持久事实 driver，并为每个已取得
真实提交身份的案例写入有界 numeric facts。当前候选尚未执行 formal；driver 无法控制的故障边界如实记为
`infrastructure_failure`，前置失败后的案例保持 `not_started`，因此不能产生 passed 回执。

证据核验不启动 Docker：

```bash
make verify-business SCOPE=v1 \
  MANIFEST=/private/candidate/release-manifest.json \
  RECEIPT=/private/evidence/preflight.json \
  VERIFY_EVIDENCE=1 PUBLICATION=/private/evidence/publication.json
```

它重新读取候选 manifest/Bundle 和 receipt/raw，核对身份、时间顺序、案例计数、sha256、敏感字段和 cleanup
事实；只按白名单写 publication，不复制环境、凭证、SQL 或请求正文。`KEEP` 在 V1 模式被拒绝，Compose 清理
只使用本次随机 project、volume、network 和 one-shot container。

## 回执字段

外壳仍是 `gopulse.acceptance.v1`。`v1_evidence` 包含 candidate/manifest/Bundle/source/contract/tool/config、
epoch/command/event/post/generation/revision、环境变量名和受限负载摘要。每个案例记录 planned/actual action、
`passed|failed|not_started`、失败分类、关键时刻、窗口、raw 相对路径及 `sha256:` checksum。preflight raw 是非敏感的
阶段浏览器断言；formal raw 只允许固定名称的非负数值事实，均不包含正文、digest、Cookie、凭证或原始 SQL。

预检和完整矩阵必须使用不同的实际 candidate/revision；本批不编辑回执、不换绑制品，也不把 timeout 参数当作
跨准备、浏览器、诊断和清理的总预算证明。
