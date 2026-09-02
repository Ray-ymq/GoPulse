CREATE TABLE notifications (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    source_event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    recipient_id BIGINT UNSIGNED NOT NULL,
    actor_id BIGINT UNSIGNED NOT NULL,
    post_id BIGINT UNSIGNED NOT NULL,
    comment_id BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL,
    read_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_notifications_source_event_id (source_event_id),
    KEY idx_notifications_recipient_created_id (recipient_id, created_at DESC, id DESC),
    KEY idx_notifications_actor_id (actor_id),
    KEY idx_notifications_post_id (post_id),
    KEY idx_notifications_comment_id (comment_id),
    CONSTRAINT fk_notifications_recipient
        FOREIGN KEY (recipient_id) REFERENCES users (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_notifications_actor
        FOREIGN KEY (actor_id) REFERENCES users (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_notifications_post
        FOREIGN KEY (post_id) REFERENCES posts (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_notifications_comment
        FOREIGN KEY (comment_id) REFERENCES comments (id)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_notifications_type CHECK (type IN ('comment.created', 'post.liked')),
    CONSTRAINT chk_notifications_comment_shape CHECK (
        (type = 'comment.created' AND comment_id IS NOT NULL)
        OR (type = 'post.liked' AND comment_id IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
