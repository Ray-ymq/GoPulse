ALTER TABLE users MODIFY role ENUM('user','admin','super_admin') NOT NULL DEFAULT 'user';
UPDATE users SET role = 'super_admin' WHERE role = 'admin';
CREATE TABLE IF NOT EXISTS bootstrap_super_admin (
 singleton TINYINT UNSIGNED NOT NULL PRIMARY KEY,
 user_id BIGINT UNSIGNED NOT NULL UNIQUE,
 CONSTRAINT chk_bootstrap_singleton CHECK (singleton = 1),
 CONSTRAINT fk_bootstrap_user FOREIGN KEY (user_id) REFERENCES users(id) ON UPDATE RESTRICT ON DELETE RESTRICT
) ENGINE=InnoDB;
INSERT INTO bootstrap_super_admin (singleton,user_id)
 SELECT 1, MIN(id) FROM users WHERE role = 'super_admin'
 HAVING MIN(id) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM bootstrap_super_admin);
ALTER TABLE users MODIFY role ENUM('user','super_admin') NOT NULL DEFAULT 'user';
CREATE TABLE IF NOT EXISTS management_audit_events (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 operation_id CHAR(32) NOT NULL,
 occurred_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 actor_kind ENUM('user','system') NOT NULL,
 actor_user_id BIGINT UNSIGNED NULL,
 action VARCHAR(48) NOT NULL,
 resource_type VARCHAR(24) NOT NULL,
 resource_id VARCHAR(128) NOT NULL,
 phase ENUM('requested','completed') NOT NULL,
 outcome ENUM('succeeded','failed','unknown') NOT NULL,
 request_id VARCHAR(128) NOT NULL,
 details_json JSON NOT NULL,
 INDEX idx_audit_time (occurred_at,id)
) ENGINE=InnoDB;
