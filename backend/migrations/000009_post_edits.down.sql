DELETE FROM business_outbox WHERE event_type = 'post.updated';
ALTER TABLE business_outbox
    DROP CHECK chk_business_outbox_event_type,
    ADD CONSTRAINT chk_business_outbox_event_type
        CHECK (event_type IN ('comment.created', 'post.liked', 'post.created', 'user.followed'));
ALTER TABLE posts DROP COLUMN edited_at, DROP COLUMN content_revision;
