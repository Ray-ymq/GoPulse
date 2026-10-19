-- Old component rules remain available as history, but no longer evaluate
-- samples from the retired product self-monitoring publication path.
UPDATE alert_incidents i JOIN alert_rules r ON r.id=i.rule_id
SET i.status='closed', i.closed_at=UTC_TIMESTAMP(6), i.resolution_reason='selfmonitor_moved'
WHERE i.status='firing' AND r.source='metrics'
AND JSON_UNQUOTE(JSON_EXTRACT(r.selector,'$.metric')) REGEXP '^gopulse_(backend|business_worker|search_indexer|monitor|marshaller)_';
UPDATE alert_rule_states s JOIN alert_rules r ON r.id=s.rule_id
SET s.state='disabled', s.data_status='unknown', s.pending_since=NULL,
    s.active_incident_id=NULL, s.lease_owner='', s.lease_until=NULL,
    s.error_code='selfmonitor_moved'
WHERE r.source='metrics'
AND JSON_UNQUOTE(JSON_EXTRACT(r.selector,'$.metric')) REGEXP '^gopulse_(backend|business_worker|search_indexer|monitor|marshaller)_';
UPDATE alert_rules SET enabled=FALSE, revision=revision+1, updated_at=UTC_TIMESTAMP(6)
WHERE enabled=TRUE AND source='metrics'
AND JSON_UNQUOTE(JSON_EXTRACT(selector,'$.metric')) REGEXP '^gopulse_(backend|business_worker|search_indexer|monitor|marshaller)_';
