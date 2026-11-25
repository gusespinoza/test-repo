CREATE TABLE IF NOT EXISTS events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT NOT NULL DEFAULT '',
    venue TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    capacity INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE events DROP CONSTRAINT IF EXISTS events_capacity_check;
ALTER TABLE events ADD CONSTRAINT events_capacity_check CHECK (capacity > 0);

CREATE INDEX IF NOT EXISTS events_starts_at_id_idx
    ON events (starts_at, id);
