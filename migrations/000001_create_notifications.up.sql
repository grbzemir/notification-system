CREATE TABLE notifications (
    id                   UUID PRIMARY KEY,
    batch_id             UUID,
    idempotency_key      TEXT,
    recipient            TEXT        NOT NULL,
    channel              TEXT        NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    content              TEXT        NOT NULL,
    priority             TEXT        NOT NULL DEFAULT 'normal' CHECK (priority IN ('high', 'normal', 'low')),
    status               TEXT        NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending', 'scheduled', 'processing', 'sent', 'failed', 'cancelled')),
    attempts             INT         NOT NULL DEFAULT 0,
    last_error           TEXT,
    provider_message_id  TEXT,
    scheduled_at         TIMESTAMPTZ,
    sent_at              TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_notifications_idempotency_key
    ON notifications (idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_notifications_batch_id ON notifications (batch_id);
CREATE INDEX idx_notifications_filter ON notifications (status, channel, created_at DESC);
CREATE INDEX idx_notifications_scheduled ON notifications (scheduled_at) WHERE status = 'scheduled';
