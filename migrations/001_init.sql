CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'USER' CHECK (role IN ('USER', 'ADMIN')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS events (
    id VARCHAR(64) PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    venue VARCHAR(255) NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    total_seats INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS seats (
    id VARCHAR(64) PRIMARY KEY,
    event_id VARCHAR(64) NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    seat_number VARCHAR(16) NOT NULL,
    price_cents BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'AVAILABLE' CHECK (status IN ('AVAILABLE', 'HELD', 'SOLD')),
    held_by_user_id VARCHAR(64),
    hold_id VARCHAR(64),
    hold_expires_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    UNIQUE (event_id, seat_number)
);

CREATE INDEX IF NOT EXISTS idx_seats_event_status ON seats (event_id, status);
CREATE INDEX IF NOT EXISTS idx_seats_hold_expires ON seats (status, hold_expires_at);

CREATE TABLE IF NOT EXISTS orders (
    id VARCHAR(64) PRIMARY KEY,
    hold_id VARCHAR(64) UNIQUE NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    event_id VARCHAR(64) NOT NULL,
    seat_id VARCHAR(64) NOT NULL,
    amount_cents BIGINT NOT NULL,
    idempotency_key VARCHAR(128) UNIQUE NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'CONFIRMED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
