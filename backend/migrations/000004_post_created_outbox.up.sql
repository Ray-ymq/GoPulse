ALTER TABLE business_outbox
    DROP CHECK chk_business_outbox_event_type,
    ADD CONSTRAINT chk_business_outbox_event_type
        CHECK (event_type IN ('comment.created', 'post.liked', 'post.created'));
