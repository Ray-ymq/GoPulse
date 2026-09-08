-- Restore the exact v10 schema and preserve all tombstones. v10 intentionally has
-- no resource-shape CHECK because its SET NULL foreign keys are incompatible.
ALTER TABLE notifications
 DROP CHECK chk_notifications_resource_shape,
 DROP FOREIGN KEY fk_notifications_post_shape,
 DROP FOREIGN KEY fk_notifications_comment_shape,
 ADD CONSTRAINT fk_notifications_post_deleted FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE SET NULL,
 ADD CONSTRAINT fk_notifications_comment_deleted FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE SET NULL;
