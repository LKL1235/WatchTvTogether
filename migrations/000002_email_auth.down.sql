DROP INDEX IF EXISTS idx_users_email_lower;
ALTER TABLE users DROP COLUMN IF EXISTS email;
