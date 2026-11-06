CREATE INDEX idx_requirements_event_retention ON business_event_requirements (event_id, closed_at, retain_until);
CREATE INDEX idx_commands_event_retention ON business_commands (event_id, retained_until);
