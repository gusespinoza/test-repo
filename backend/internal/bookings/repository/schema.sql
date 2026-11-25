ALTER TABLE events
    ADD COLUMN IF NOT EXISTS sold INTEGER NOT NULL DEFAULT 0;

ALTER TABLE events
    DROP CONSTRAINT IF EXISTS events_sold_nonnegative;

ALTER TABLE events
    ADD CONSTRAINT events_sold_nonnegative CHECK (sold >= 0);

CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'confirmed', 'cancelled')),
    idempotency_key TEXT NOT NULL CHECK (length(trim(idempotency_key)) BETWEEN 1 AND 200),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS orders_user_created_at_idx
    ON orders (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS orders_event_status_idx
    ON orders (event_id, status);
