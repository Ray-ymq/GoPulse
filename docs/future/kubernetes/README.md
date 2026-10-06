# Kubernetes 未来设计

本目录保存 GoPulse 早期 Kubernetes 候选设计，作为设计输入保留。
[唯一全局发展总纲](../../../dev/phases/GoPulse-高并发与可观测后续路线图.md)已经选定
B 关卡交付 Kubernetes；尚未分配实施 Phase、版本或分支，当前已验证产品环境仍为 Linux `amd64` Compose。

- [基础部署设计](deployment-design.md)
- [统一入口设计](unified-ingress-design.md)
- [集群可观测设计](cluster-observability-design.md)

进入 B 前，依据总纲和当时的产品基线、设备资源重新编写权威实施方案，并冻结一种环境，
再分配 Phase、版本和开发分支。旧候选中的独立 Exporter 工作负载、节点级全量采集或
分散入口不得自动纳入范围：总纲选定 Monitor 单一插件所有者、当前命名空间观测与统一入口。
本目录不另外决定发展路线，也不构成实现或验收证据。
