CREATE TABLE business_outbox (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    event_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    schema_version SMALLINT UNSIGNED NOT NULL,
    payload JSON NOT NULL,
    status ENUM('pending', 'leased', 'published') NOT NULL DEFAULT 'pending',
    available_at DATETIME(6) NOT NULL,
    attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
    lease_owner VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    lease_expires_at DATETIME(6) NULL,
    published_at DATETIME(6) NULL,
    last_error VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_business_outbox_event_id (event_id),
    KEY idx_business_outbox_pending (status, available_at, id),
    KEY idx_business_outbox_lease_recovery (status, lease_expires_at, id),
    KEY idx_business_outbox_published_cleanup (status, published_at, id),
    CONSTRAINT chk_business_outbox_event_type CHECK (event_type IN ('comment.created', 'post.liked')),
    CONSTRAINT chk_business_outbox_schema_version CHECK (schema_version = 1),
    CONSTRAINT chk_business_outbox_state CHECK (
        (status = 'pending' AND lease_owner IS NULL AND lease_expires_at IS NULL AND published_at IS NULL)
        OR (status = 'leased' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL AND published_at IS NULL)
        OR (status = 'published' AND lease_owner IS NULL AND lease_expires_at IS NULL AND published_at IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
