-- 删除可观测平台告警数据。执行前应备份需要保留的历史记录。
DROP TABLE IF EXISTS alert_rule_states;
DROP TABLE IF EXISTS alert_incidents;
DROP TABLE IF EXISTS alert_rules;
