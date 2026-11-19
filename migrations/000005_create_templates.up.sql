-- Bildirim şablonları. Gövde {{degisken}} biçiminde yer tutucular içerir; bildirim oluşturulurken doldurulur.
CREATE TABLE templates (
    id         UUID PRIMARY KEY,
    name       TEXT        NOT NULL,
    body       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_templates_name ON templates (name);
