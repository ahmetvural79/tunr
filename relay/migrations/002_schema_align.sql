-- 002_schema_align.sql
--
-- Brings the tunnels table in line with what the relay (Go) and the
-- dashboard (Next.js) actually read/write. All operations are idempotent.
--
-- Background:
--   * relay/internal/db/db.go writes tunnel_short_id, connected_at, disconnected_at
--   * landing/app/app/api/tunnels/route.ts reads request_count + feature flags
--
-- 001_init.sql only created (id, user_id, subdomain, local_port, status, public_url,
-- created_at, closed_at) so both code paths silently failed (or relied on out-of-band
-- ALTERs in production). This migration consolidates the missing columns.

ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS tunnel_short_id TEXT;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS request_count    BIGINT      NOT NULL DEFAULT 0;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS connected_at     TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS disconnected_at  TIMESTAMPTZ;

-- local_port was NOT NULL in 001_init.sql but the relay's RecordTunnelConnect did not
-- supply it, so on fresh DBs every insert failed silently (the relay swallows the
-- error). Give it a default so older callers stay working while we transition to
-- including the port explicitly.
ALTER TABLE tunnels ALTER COLUMN local_port SET DEFAULT 0;
ALTER TABLE tunnels ALTER COLUMN local_port DROP NOT NULL;

-- Vibecoder + Pinggy feature flags surfaced as badges in the dashboard
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_qr            BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_auth_token    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_ip_whitelist  BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_header_mod    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_demo          BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_freeze        BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_widget        BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tunnels ADD COLUMN IF NOT EXISTS has_password      BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial unique index: enforce uniqueness only when tunnel_short_id is set so the
-- existing rows (NULL tunnel_short_id) don't collide.
CREATE UNIQUE INDEX IF NOT EXISTS idx_tunnels_short_id_uq
  ON tunnels (tunnel_short_id)
  WHERE tunnel_short_id IS NOT NULL;

-- Backfill timestamps for rows created before this migration so the dashboard
-- doesn't see NULLs after the schema change.
UPDATE tunnels SET connected_at    = created_at WHERE connected_at IS NULL;
UPDATE tunnels SET disconnected_at = closed_at  WHERE disconnected_at IS NULL AND closed_at IS NOT NULL;

-- Mark legacy rows that have a closed_at but still say status='active' as closed.
UPDATE tunnels SET status = 'closed' WHERE closed_at IS NOT NULL AND status = 'active';

-- Helpful index for "user's recent tunnels" queries used by the dashboard + relay.
CREATE INDEX IF NOT EXISTS idx_tunnels_user_connected_at
  ON tunnels (user_id, connected_at DESC);
