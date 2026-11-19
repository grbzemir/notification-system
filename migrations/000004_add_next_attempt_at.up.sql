-- Yeniden deneme zamanı. NULL ise bildirim hemen kuyruğa alınabilir (ilk deneme).
-- Dolu ise relay yalnızca bu zaman gelince bildirimi yeniden yayınlar.
ALTER TABLE notifications ADD COLUMN next_attempt_at TIMESTAMPTZ;
