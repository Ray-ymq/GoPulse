ALTER TABLE users MODIFY role ENUM('user','admin','super_admin') NOT NULL DEFAULT 'user';
UPDATE users SET role = 'admin' WHERE role = 'super_admin';
DROP TABLE IF EXISTS management_audit_events;
DROP TABLE IF EXISTS bootstrap_super_admin;
ALTER TABLE users MODIFY role ENUM('user','admin') NOT NULL DEFAULT 'user';
