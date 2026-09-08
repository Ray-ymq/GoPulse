ALTER TABLE posts
    ADD COLUMN edited_at DATETIME(6) NULL,
    ADD COLUMN content_revision BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE business_outbox
    DROP CHECK chk_business_outbox_event_type,
    ADD CONSTRAINT chk_business_outbox_event_type
        CHECK (event_type IN ('comment.created', 'post.liked', 'post.created', 'user.followed', 'post.updated'));
