-- MySQL forbids CHECK columns with SET NULL referential actions (error 3823).
-- Deletion must atomically clear both references before deleting resources.
-- Existing malformed rows deliberately fail migration rather than losing data.
ALTER TABLE notifications
 DROP FOREIGN KEY fk_notifications_post_deleted,
 DROP FOREIGN KEY fk_notifications_comment_deleted,
 ADD CONSTRAINT fk_notifications_post_shape FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE RESTRICT,
 ADD CONSTRAINT fk_notifications_comment_shape FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE RESTRICT,
 ADD CONSTRAINT chk_notifications_resource_shape CHECK (
  (type = 'comment.created' AND ((post_id IS NOT NULL AND comment_id IS NOT NULL) OR (post_id IS NULL AND comment_id IS NULL)))
  OR (type = 'post.liked' AND comment_id IS NULL)
  OR (type = 'user.followed' AND post_id IS NULL AND comment_id IS NULL)
 );
