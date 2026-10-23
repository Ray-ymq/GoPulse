ALTER TABLE posts ADD INDEX idx_posts_author_created_id (author_id, created_at, id);
