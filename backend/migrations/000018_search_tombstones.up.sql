CREATE TABLE post_search_tombstones (
 data_epoch CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 post_id BIGINT UNSIGNED NOT NULL,
 content_revision BIGINT UNSIGNED NOT NULL,
 deleted_at DATETIME(6) NOT NULL,
 PRIMARY KEY (data_epoch, post_id)
) ENGINE=InnoDB;
