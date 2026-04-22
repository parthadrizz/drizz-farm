-- D1 schema for drizz-farm lead capture.
-- Run with: wrangler d1 execute drizz-farm-leads --file schema.sql --remote

-- One row per install. install_id is the client-generated UUID.
-- email is mandatory in the client, but we store NOT NULL to trust-verify.
CREATE TABLE IF NOT EXISTS installs (
    install_id    TEXT PRIMARY KEY,
    email         TEXT NOT NULL,
    org_name      TEXT,
    hostname      TEXT,
    os            TEXT,
    arch          TEXT,
    version       TEXT,
    ip            TEXT,                 -- captured from CF-Connecting-IP header
    country       TEXT,                 -- CF-IPCountry
    user_agent    TEXT,
    verified_at   TEXT,                 -- populated when the welcome email link is clicked
    first_seen    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_installs_email ON installs(email);
CREATE INDEX IF NOT EXISTS idx_installs_last_seen ON installs(last_seen);

-- Each heartbeat event appended. Lets us see activity over time.
-- Trim old rows periodically — we only need last ~30 days for analytics.
CREATE TABLE IF NOT EXISTS heartbeats (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    install_id      TEXT NOT NULL,
    version         TEXT,
    os              TEXT,
    arch            TEXT,
    node_count      INTEGER,
    sessions_today  INTEGER,
    emulators_today INTEGER,
    uptime_seconds  INTEGER,
    received_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    -- no FK to installs: heartbeats from unknown installs (e.g. after a DB
    -- reset, or if signup failed) still get captured. We join on install_id
    -- at analytics time; orphans are easy to spot.
);

CREATE INDEX IF NOT EXISTS idx_heartbeats_install ON heartbeats(install_id);
CREATE INDEX IF NOT EXISTS idx_heartbeats_received ON heartbeats(received_at);

-- One-shot email verification tokens. Emitted at signup, redeemed when
-- the user clicks the link in the welcome email. Token is opaque
-- (32 hex chars from crypto.getRandomValues) so unguessable.
CREATE TABLE IF NOT EXISTS verify_tokens (
    token       TEXT PRIMARY KEY,
    install_id  TEXT NOT NULL,
    issued_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    used_at     TEXT
);

CREATE INDEX IF NOT EXISTS idx_verify_install ON verify_tokens(install_id);
