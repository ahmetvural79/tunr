-- 003_apps.sql
--
-- Pivot Faz 0: "cloud" upstream (kalıcı app'ler) için şema.
--
-- Bugün relay yalnız subdomain -> canlı WS tünel oturumu biliyordu. Bu migration
-- üç tablo ekler: apps (kalıcı uygulama), deployments (her build/release), routes
-- (subdomain -> upstream eşlemesi). routes tablosu relay'in baktığı tek tablodur;
-- kind='cloud' satırları CloudUpstream'e, kind='tunnel' tünele, kind='hybrid' ise
-- (ileride, detach) tünel varsa tünele yoksa cloud'a düşer.
--
-- Hepsi idempotent (IF NOT EXISTS). Dashboard (Next.js/pg) ve relay (Go/pgx) aynı
-- Postgres'i paylaştığı için bu tablolar her ikisi tarafından da okunabilir.

-- ── apps: kalıcı uygulama ──
CREATE TABLE IF NOT EXISTS apps (
    id            TEXT PRIMARY KEY,                         -- "a_x7k2…" kısa ulid; container adında kullanılır
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL UNIQUE,                     -- subdomain ile aynı
    region        TEXT NOT NULL DEFAULT 'ams',
    internal_port INT  NOT NULL DEFAULT 8080,               -- app'in dinlediği port (PORT env)
    edge_secret   TEXT NOT NULL,                            -- tunr-shim HMAC anahtarı (X-Tunr-Edge)
    status        TEXT NOT NULL DEFAULT 'created',          -- created | live | deleted
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_apps_user ON apps (user_id, created_at DESC);

-- ── deployments: app içinde artan her build/release ──
CREATE TABLE IF NOT EXISTS deployments (
    id         TEXT PRIMARY KEY,                            -- "dep_…"
    app_id     TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    seq        INT  NOT NULL,                               -- app içinde artan; rollback bununla
    image_ref  TEXT,                                        -- lokal image tag (own-server) veya registry ref
    status     TEXT NOT NULL DEFAULT 'queued',              -- queued|building|pushing|releasing|healthy|failed
    error      TEXT,
    env_enc    BYTEA,                                       -- env snapshot, CP anahtarıyla mühürlü (sealed box)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_deployments_app ON deployments (app_id, seq DESC);

-- ── routes: subdomain -> upstream. Relay'in baktığı tek tablo. ──
CREATE TABLE IF NOT EXISTS routes (
    subdomain    TEXT PRIMARY KEY,
    kind         TEXT NOT NULL DEFAULT 'cloud',             -- 'tunnel' | 'cloud' | 'hybrid'
    app_id       TEXT REFERENCES apps(id) ON DELETE CASCADE,-- cloud/hybrid için
    cloud_url    TEXT,                                      -- http://tunr-app-{id}:8080 (own-server container)
    wake_timeout INT  NOT NULL DEFAULT 30,                  -- sn; ilk bayt için uyanma payı
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_routes_kind ON routes (kind);

-- ── LISTEN/NOTIFY: routes değişince relay in-process cache'ini geçersiz kılar ──
-- Payload = değişen subdomain. Relay bir dedicated pgx bağlantısıyla dinler;
-- bağlantı koparsa 10 sn'lik TTL poll'a düşer.
CREATE OR REPLACE FUNCTION notify_routes_changed() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('routes_changed', COALESCE(NEW.subdomain, OLD.subdomain));
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS routes_changed_trg ON routes;
CREATE TRIGGER routes_changed_trg
    AFTER INSERT OR UPDATE OR DELETE ON routes
    FOR EACH ROW EXECUTE FUNCTION notify_routes_changed();
