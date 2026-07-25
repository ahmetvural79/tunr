-- ================================================================
-- tunr.sh — Supabase Row Level Security (RLS) Policy Setup
-- ================================================================
-- Bu SQL'i Supabase Dashboard → SQL Editor'da çalıştırın.
-- GÜVENLİK: RLS olmadan anon key ile herkes herkese erişebilir!
-- ================================================================

-- ── Tabloları oluştur ────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id          UUID PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'pro', 'team')),
    paddle_customer_id TEXT,
    paddle_subscription_id TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tunnels (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subdomain   TEXT NOT NULL UNIQUE,
    local_port  INT,
    connected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disconnected_at TIMESTAMPTZ,
    requests_count BIGINT DEFAULT 0,
    bytes_transferred BIGINT DEFAULT 0
);

CREATE TABLE IF NOT EXISTS usage_log (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period      DATE NOT NULL DEFAULT CURRENT_DATE,
    requests    BIGINT DEFAULT 0,
    bandwidth_bytes BIGINT DEFAULT 0,
    UNIQUE (user_id, period)
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE, -- SHA-256 hash, plain-text asla saklanmaz
    name        TEXT DEFAULT 'Default',
    last_used   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS feedback (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    email       TEXT,
    type        TEXT NOT NULL CHECK (type IN ('bug', 'feature', 'general')),
    message     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'wont_fix')),
    admin_reply TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── RLS'yi Etkinleştir ───────────────────────────────────────────

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE tunnels ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback ENABLE ROW LEVEL SECURITY;

-- ── users politikaları ──────────────────────────────────────────

-- Kullanıcı sadece kendi profilini okuyabilir
CREATE POLICY "users_select_own" ON users
    FOR SELECT USING (auth.uid() = id);

-- Kullanıcı sadece kendi profilini güncelleyebilir
-- plan alanını SADECE backend service_role key ile güncelleyebilir
CREATE POLICY "users_update_own" ON users
    FOR UPDATE USING (auth.uid() = id)
    WITH CHECK (auth.uid() = id);

-- Yeni kayıt (auth trigger ile otomatik oluşur, aşağıya bakın)
CREATE POLICY "users_insert_own" ON users
    FOR INSERT WITH CHECK (auth.uid() = id);

-- ── tunnels politikaları ─────────────────────────────────────────

CREATE POLICY "tunnels_select_own" ON tunnels
    FOR SELECT USING (auth.uid() = user_id);

CREATE POLICY "tunnels_insert_own" ON tunnels
    FOR INSERT WITH CHECK (auth.uid() = user_id);

CREATE POLICY "tunnels_delete_own" ON tunnels
    FOR DELETE USING (auth.uid() = user_id);

-- ── usage_log politikaları ───────────────────────────────────────

CREATE POLICY "usage_select_own" ON usage_log
    FOR SELECT USING (auth.uid() = user_id);

-- Insert/Update sadece backend (service_role) yapabilir
-- Frontend'den kullanım verisi değiştirilmemeli

-- ── api_tokens politikaları ──────────────────────────────────────

CREATE POLICY "tokens_select_own" ON api_tokens
    FOR SELECT USING (auth.uid() = user_id);

CREATE POLICY "tokens_delete_own" ON api_tokens
    FOR DELETE USING (auth.uid() = user_id);

-- ── feedback politikaları ────────────────────────────────────────

-- Herkes (anonim dahil) feedback gönderebilir
CREATE POLICY "feedback_insert_any" ON feedback
    FOR INSERT WITH CHECK (true);

-- Kullanıcı sadece kendi feedback'ini okuyabilir
CREATE POLICY "feedback_select_own" ON feedback
    FOR SELECT USING (
        auth.uid() IS NULL -- anonim görüntüleme için; gerçekte kısıtla
        OR auth.uid() = user_id
    );

-- ── Auth Trigger: Yeni kullanıcı kaydında users tablosu oluştur ──

CREATE OR REPLACE FUNCTION public.handle_new_user()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER SET search_path = public
AS $$
BEGIN
    INSERT INTO public.users (id, email, plan)
    VALUES (
        NEW.id,
        NEW.email,
        'free'
    )
    ON CONFLICT (id) DO NOTHING;
    RETURN NEW;
END;
$$;

-- Trigger: auth.users'a yeni satır eklenince users tablosuna da ekle
DROP TRIGGER IF EXISTS on_auth_user_created ON auth.users;
CREATE TRIGGER on_auth_user_created
    AFTER INSERT ON auth.users
    FOR EACH ROW EXECUTE FUNCTION public.handle_new_user();

-- ── Plan güncelleme (sadece service_role) ───────────────────────
-- Bu fonksiyonu relay backend, Paddle webhook geldiğinde çağırır.
-- İmza: update_user_plan(user_id UUID, new_plan TEXT)

CREATE OR REPLACE FUNCTION update_user_plan(
    p_user_id UUID,
    p_plan TEXT,
    p_paddle_sub_id TEXT DEFAULT NULL,
    p_paddle_cus_id TEXT DEFAULT NULL
)
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    UPDATE public.users
    SET
        plan = p_plan,
        paddle_subscription_id = COALESCE(p_paddle_sub_id, paddle_subscription_id),
        paddle_customer_id = COALESCE(p_paddle_cus_id, paddle_customer_id),
        updated_at = NOW()
    WHERE id = p_user_id;
END;
$$;

-- ── Kullanım sayacı ─────────────────────────────────────────────
-- Relay backend bu fonksiyonu her request sonrası çağırır.

CREATE OR REPLACE FUNCTION increment_usage(
    p_user_id UUID,
    p_bytes BIGINT DEFAULT 0
)
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    INSERT INTO public.usage_log (user_id, period, requests, bandwidth_bytes)
    VALUES (p_user_id, CURRENT_DATE, 1, p_bytes)
    ON CONFLICT (user_id, period)
    DO UPDATE SET
        requests = usage_log.requests + 1,
        bandwidth_bytes = usage_log.bandwidth_bytes + p_bytes;
END;
$$;

-- ── NOTLAR ──────────────────────────────────────────────────────
-- 1. service_role key'i ASLA frontend'de kullanmayın
-- 2. update_user_plan() fonksiyonu yalnızca relay backend'den çağrılır
-- 3. RLS, anon key güvenliğinin temelidir — devre dışı bırakmayın
-- 4. Admin paneli için: Supabase Dashboard → Table Editor kullanın
--    veya ayrı bir admin Supabase projesi kullanın
