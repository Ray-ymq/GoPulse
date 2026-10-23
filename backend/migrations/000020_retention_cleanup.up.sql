ALTER TABLE business_event_requirements ADD COLUMN retention_reserved BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE business_event_requirements r SET retention_reserved=TRUE WHERE closed_at IS NOT NULL OR EXISTS (SELECT 1 FROM business_commands c WHERE c.data_epoch=r.data_epoch AND c.event_id=r.event_id);
CREATE INDEX idx_requirements_expiry ON business_event_requirements(retention_released, retain_until, event_id);
CREATE INDEX idx_commands_expiry ON business_commands(retained_until, event_id);
CREATE INDEX idx_requirements_recent ON business_event_requirements(created_at, retention_reserved, payload_bytes);
