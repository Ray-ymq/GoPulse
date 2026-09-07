CREATE TABLE user_follows (
    follower_id BIGINT UNSIGNED NOT NULL,
    followed_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (follower_id, followed_id),
    KEY idx_follows_following (follower_id, created_at DESC, followed_id DESC),
    KEY idx_follows_followers (followed_id, created_at DESC, follower_id DESC),
    CONSTRAINT fk_follows_follower FOREIGN KEY (follower_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_follows_followed FOREIGN KEY (followed_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT chk_follows_not_self CHECK (follower_id <> followed_id)
) ENGINE=InnoDB;
ALTER TABLE notifications DROP CHECK chk_notifications_type, DROP CHECK chk_notifications_comment_shape,
    MODIFY post_id BIGINT UNSIGNED NULL,
    ADD CONSTRAINT chk_notifications_type CHECK (type IN ('comment.created', 'post.liked', 'user.followed')),
    ADD CONSTRAINT chk_notifications_comment_shape CHECK (
        (type = 'comment.created' AND post_id IS NOT NULL AND comment_id IS NOT NULL)
        OR (type = 'post.liked' AND post_id IS NOT NULL AND comment_id IS NULL)
        OR (type = 'user.followed' AND post_id IS NULL AND comment_id IS NULL));
ALTER TABLE business_outbox DROP CHECK chk_business_outbox_event_type,
 ADD CONSTRAINT chk_business_outbox_event_type CHECK (event_type IN ('comment.created', 'post.liked', 'post.created', 'user.followed'));
