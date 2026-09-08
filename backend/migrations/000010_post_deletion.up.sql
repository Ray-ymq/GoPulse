ALTER TABLE notifications
 DROP CHECK chk_notifications_comment_shape,
 DROP FOREIGN KEY fk_notifications_post,
 DROP FOREIGN KEY fk_notifications_comment,
 ADD CONSTRAINT fk_notifications_post_deleted FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE SET NULL,
 ADD CONSTRAINT fk_notifications_comment_deleted FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE SET NULL;
ALTER TABLE business_outbox DROP CHECK chk_business_outbox_event_type,
 ADD CONSTRAINT chk_business_outbox_event_type CHECK (event_type IN ('comment.created', 'post.liked', 'post.created', 'user.followed', 'post.updated', 'post.deleted'));
