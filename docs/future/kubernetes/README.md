# Kubernetes 历史候选设计

本目录保存早期候选设计与当时的路线引用。候选正文中“唯一全局发展总纲”和“B 关卡”
属于原研究基线；目前的架构权威从[设计索引](../../../dev/design/README.md)登记的生效基线读取。
这些材料用于后续设计参考，尚未分配实施 Phase、版本或分支。

- [基础部署设计](deployment-design.md)
- [统一入口设计](unified-ingress-design.md)
- [集群可观测设计](cluster-observability-design.md)

采用其中建议前，核对生效设计、当前产品能力、设备条件和相关合同，再形成详细设计与
正式实施方案；旧候选的独立 Exporter 工作负载、全量采集或分散入口不能自动继承。
产品支持与验证范围见[能力状态](../../../dev/status/capability-status.md)。
