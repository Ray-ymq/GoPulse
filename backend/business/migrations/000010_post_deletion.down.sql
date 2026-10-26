-- Permanent deletion cannot be reversed. Preserve notification tombstones on rollback.
ALTER TABLE notifications
 DROP FOREIGN KEY fk_notifications_post_deleted,
 DROP FOREIGN KEY fk_notifications_comment_deleted,
 ADD CONSTRAINT fk_notifications_post FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE RESTRICT,
 ADD CONSTRAINT fk_notifications_comment FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE RESTRICT;
