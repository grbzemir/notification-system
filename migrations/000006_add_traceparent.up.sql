-- W3C trace context (traceparent). Bildirim oluşturulurken kaydedilir; kuyruğa sonradan bırakan relay ve yeniden denemeler
-- aynı izi (trace) sürdürebilsin diye. Boş string: izleme kapalıydı.
ALTER TABLE notifications ADD COLUMN traceparent TEXT NOT NULL DEFAULT '';
