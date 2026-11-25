-- 仅恢复表结构；已删除的故障摘要只能从备份恢复。
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
