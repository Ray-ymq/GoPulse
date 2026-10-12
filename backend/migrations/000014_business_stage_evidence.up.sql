CREATE TABLE IF NOT EXISTS business_environment (
 singleton TINYINT UNSIGNED NOT NULL PRIMARY KEY,
 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 status ENUM('ready','restoring') NOT NULL DEFAULT 'ready',
 mode ENUM('legacy_v1','v2') NOT NULL DEFAULT 'legacy_v1',
 v1_compatibility_enabled BOOLEAN NOT NULL DEFAULT TRUE,
 activation_revision VARCHAR(64) CHARACTER SET ascii NOT NULL DEFAULT 'phase-24-01',
 restore_operation_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 UNIQUE KEY uq_business_environment_epoch (data_epoch),
 CONSTRAINT chk_business_environment_singleton CHECK (singleton = 1),
 CONSTRAINT chk_business_environment_epoch CHECK (data_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 CONSTRAINT chk_business_environment_operation CHECK (restore_operation_id IS NULL OR restore_operation_id REGEXP '^[0-9a-f]{64}$')
) ENGINE=InnoDB;

INSERT INTO business_environment (singleton, data_epoch, status, mode, v1_compatibility_enabled, activation_revision)
SELECT 1, UUID(), 'ready', 'legacy_v1', TRUE, 'phase-24-01'
WHERE NOT EXISTS (SELECT 1 FROM business_environment WHERE singleton = 1);

CREATE TABLE IF NOT EXISTS business_commands (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 command_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 user_id BIGINT UNSIGNED NOT NULL,
 command_type VARCHAR(48) CHARACTER SET ascii NOT NULL,
 idempotency_key_hash BINARY(32) NULL,
 request_digest BINARY(32) NOT NULL,
 post_id BIGINT UNSIGNED NULL,
 event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
 status ENUM('succeeded','failed','unknown') NOT NULL,
 committed_at DATETIME(6) NULL,
 retained_until DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 UNIQUE KEY uq_business_command_id (data_epoch, command_id),
 UNIQUE KEY uq_business_command_idempotency (data_epoch, user_id, command_type, idempotency_key_hash),
 KEY idx_business_commands_retention (retained_until, id),
 KEY idx_business_commands_event (data_epoch, event_id),
	CONSTRAINT chk_business_command_epoch CHECK (data_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 CONSTRAINT chk_business_command_id CHECK (command_id REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
	 CONSTRAINT chk_business_command_event CHECK (event_id IS NULL OR event_id REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
	 CONSTRAINT chk_business_command_digest CHECK (OCTET_LENGTH(request_digest) = 32 AND (idempotency_key_hash IS NULL OR OCTET_LENGTH(idempotency_key_hash) = 32))
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS business_event_requirements (
 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 consumer VARCHAR(32) CHARACTER SET ascii NOT NULL,
 routing_contract_revision VARCHAR(64) CHARACTER SET ascii NOT NULL,
 applicable_object_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
 payload_bytes INT UNSIGNED NOT NULL DEFAULT 0,
 closed_at DATETIME(6) NULL,
 retain_until DATETIME(6) NULL,
 retention_released BOOLEAN NOT NULL DEFAULT FALSE,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (data_epoch, event_id, consumer, routing_contract_revision),
 KEY idx_business_requirements_event (data_epoch, event_id, consumer),
 KEY idx_business_requirements_cleanup (closed_at, retain_until, retention_released),
	 CONSTRAINT chk_business_requirement_epoch CHECK (data_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 CONSTRAINT chk_business_requirement_event CHECK (event_id REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
	 CONSTRAINT chk_business_requirement_payload CHECK (payload_bytes <= 16384),
	 CONSTRAINT chk_business_requirement_closure CHECK ((closed_at IS NULL AND retain_until IS NULL) OR (closed_at IS NOT NULL AND retain_until IS NOT NULL))
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS business_application_results (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 consumer VARCHAR(32) CHARACTER SET ascii NOT NULL,
 routing_contract_revision VARCHAR(64) CHARACTER SET ascii NOT NULL,
 projection_generation VARCHAR(255) CHARACTER SET ascii NOT NULL,
 result ENUM('applied','no_effect_resource_absent') NOT NULL,
 actual_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
 completed_at DATETIME(6) NOT NULL,
 UNIQUE KEY uq_business_application_result (data_epoch, event_id, consumer, routing_contract_revision, projection_generation),
 KEY idx_business_application_event (data_epoch, event_id, consumer, completed_at),
	 CONSTRAINT chk_business_application_epoch CHECK (data_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
	 CONSTRAINT chk_business_application_event CHECK (event_id REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
	 CONSTRAINT chk_business_application_generation CHECK (projection_generation <> '')
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS business_event_failures (
 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 consumer VARCHAR(32) CHARACTER SET ascii NOT NULL,
 attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
 retry_count INT UNSIGNED NOT NULL DEFAULT 0,
 dead_count INT UNSIGNED NOT NULL DEFAULT 0,
 last_reason VARCHAR(64) CHARACTER SET ascii NOT NULL DEFAULT '',
 last_failed_at DATETIME(6) NULL,
 last_retry_at DATETIME(6) NULL,
 last_dead_at DATETIME(6) NULL,
 retry_summary VARCHAR(128) CHARACTER SET ascii NOT NULL DEFAULT '',
 dead_summary VARCHAR(128) CHARACTER SET ascii NOT NULL DEFAULT '',
 PRIMARY KEY (data_epoch, event_id, consumer),
	 CONSTRAINT chk_business_failure_epoch CHECK (data_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 CONSTRAINT chk_business_failure_attempts CHECK (attempt_count <= 1000),
 CONSTRAINT chk_business_failure_event CHECK (event_id REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS business_message_parking (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 source_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
 event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
 event_type VARCHAR(64) CHARACTER SET ascii NOT NULL DEFAULT '',
 schema_version INT UNSIGNED NOT NULL DEFAULT 0,
 routing_key VARCHAR(128) CHARACTER SET ascii NOT NULL DEFAULT '',
 source_kind VARCHAR(32) CHARACTER SET ascii NOT NULL,
 payload VARBINARY(16384) NOT NULL,
 payload_sha256 BINARY(32) NOT NULL,
 reason VARCHAR(64) CHARACTER SET ascii NOT NULL,
 attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
 parked_at DATETIME(6) NOT NULL,
 UNIQUE KEY uq_business_message_parking (payload_sha256, reason),
 KEY idx_business_message_parking_time (parked_at, id),
 CONSTRAINT chk_business_message_parking_payload CHECK (OCTET_LENGTH(payload) <= 16384),
	 CONSTRAINT chk_business_message_parking_epoch CHECK (data_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 CONSTRAINT chk_business_message_parking_source_epoch CHECK (source_epoch IS NULL OR source_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
	 CONSTRAINT chk_business_message_parking_event CHECK (event_id IS NULL OR event_id REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
	 CONSTRAINT chk_business_message_parking_schema CHECK (schema_version <= 2),
	 CONSTRAINT chk_business_message_parking_attempts CHECK (attempt_count <= 1000)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS business_async_budget (
 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 open_requirements BIGINT UNSIGNED NOT NULL DEFAULT 0,
 retained_groups BIGINT UNSIGNED NOT NULL DEFAULT 0,
 retained_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
 parked_messages BIGINT UNSIGNED NOT NULL DEFAULT 0,
 parked_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	 CONSTRAINT chk_business_budget_epoch CHECK (data_epoch REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 CONSTRAINT chk_business_budget_open CHECK (open_requirements <= 10000),
 CONSTRAINT chk_business_budget_retained CHECK (retained_groups <= 100000 AND retained_bytes <= 1073741824),
 CONSTRAINT chk_business_budget_parked CHECK (parked_messages <= 10000 AND parked_bytes <= 167772160)
) ENGINE=InnoDB;

INSERT INTO business_async_budget (data_epoch)
SELECT data_epoch FROM business_environment
WHERE singleton = 1
  AND NOT EXISTS (SELECT 1 FROM business_async_budget);
