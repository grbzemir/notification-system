DROP INDEX IF EXISTS idx_notifications_unqueued;
ALTER TABLE notifications DROP COLUMN IF EXISTS queued_at;
