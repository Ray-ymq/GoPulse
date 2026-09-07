DELETE FROM business_outbox WHERE event_type = 'user.followed';
ALTER TABLE business_outbox DROP CHECK chk_business_outbox_event_type, ADD CONSTRAINT chk_business_outbox_event_type CHECK (event_type IN ('comment.created', 'post.liked', 'post.created'));
DELETE FROM notifications WHERE type = 'user.followed';
ALTER TABLE notifications DROP CHECK chk_notifications_type, DROP CHECK chk_notifications_comment_shape,
    MODIFY post_id BIGINT UNSIGNED NOT NULL,
    ADD CONSTRAINT chk_notifications_type CHECK (type IN ('comment.created', 'post.liked')),
    ADD CONSTRAINT chk_notifications_comment_shape CHECK (
        (type = 'comment.created' AND comment_id IS NOT NULL)
        OR (type = 'post.liked' AND comment_id IS NULL));
DROP TABLE user_follows;
