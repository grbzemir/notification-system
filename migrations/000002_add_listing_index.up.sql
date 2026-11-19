-- Filtresiz listeleme ve cursor sayfalama (created_at DESC, id DESC sıralaması) için.
CREATE INDEX idx_notifications_created ON notifications (created_at DESC, id DESC);
