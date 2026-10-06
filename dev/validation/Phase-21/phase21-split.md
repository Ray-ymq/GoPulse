# Phase 21-02 双服务部署与运行合同

本批把同一个 Backend 镜像拆成两个运行角色：`backend`/`backend-2` 使用
`business`，`platform-api` 使用 `platform`。后者是 Backend 的固定指标与制品
别名，不是第二个镜像。四组管理/观测 API 由同源 Nginx 入口精确转发到
`platform-api`；其余 API 仍进入业务池。所有服务监听器保持私网可见，宿主机不
新增端口。

机器合同是 `deploy/runtime-contracts.json`，合同版本为 2，登记 13 个运行单元。
`platform-api` 使用 `platform-api-1` 身份、MySQL pool 4 和 HTTP 并发 32；业务
Backend/Worker/Indexer 的每进程 MySQL 上限为 8，Monitor 使用显式的
`backend,backend-2,platform-api` 目标清单。

## 固定检查

```bash
python3 scripts/ci/verify_runtime_contracts.py --candidate 2.3.2
python3 -m unittest discover -s scripts/ci -p 'test_runtime_*.py'
python3 -m unittest discover -s scripts/ci -p 'test_release_*.py'
go -C componentmetrics test ./...
go -C backend test ./cmd/server ./internal/config ./internal/http
go -C lifecycle test ./internal/control ./internal/release
```

有限运行入口保留默认套件，并增加 service-split 模式。源码预检只接受目标版本，
不传 manifest；正式模式必须绑定同一冻结 Bundle 的 manifest 与成功预检回执：

```bash
python3 scripts/ci/runtime_acceptance.py --suite service-split --preflight \
  --candidate 2.3.2 --evidence "$PHASE21_PREFLIGHT_RECEIPT"
python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_PREFLIGHT_RECEIPT"
python3 scripts/ci/runtime_acceptance.py --suite service-split \
  --candidate 2.3.2 --manifest "$PHASE21_MANIFEST" \
  --preflight-receipt "$PHASE21_PREFLIGHT_RECEIPT" --evidence "$PHASE21_RECEIPT"
python3 scripts/ci/verify_runtime_contracts.py --evidence "$PHASE21_RECEIPT" \
  --publish "$PHASE21_PUBLICATION"
python3 scripts/ci/verify_runtime_contracts.py --verify-publication "$PHASE21_PUBLICATION"
```

回执必须包含 S01～S07、角色/实例、源与配置摘要、命令 raw、项目资源前后清单及
清理状态。失败回执也必须落盘并有界清理；发布件只保留白名单字段和 raw 哈希，不能
包含凭证、Cookie、个人内容或宿主机绝对路径。02 的源码 preflight 不是正式 Bundle
验收，正式 S01～S07 冻结结论留给 Phase 21-03。
