-- queued_at: bildirimin RabbitMQ'ya başarıyla bırakıldığı an. NULL ise henüz kuyrukta değildir.
ALTER TABLE notifications ADD COLUMN queued_at TIMESTAMPTZ;

-- Relay'in "kuyruğa bırakılmamış pending kayıtları bul" sorgusu için. Kısmi indeks olduğu için
-- sadece bekleyen az sayıdaki satırı kapsar, tablo büyüse de küçük kalır.
CREATE INDEX idx_notifications_unqueued ON notifications (created_at)
    WHERE status = 'pending' AND queued_at IS NULL;
