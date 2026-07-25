-- 004_nodes.sql
--
-- Çok-node borcu, Faz A: küme şeması.
--
-- Bugün tek node var ve `RUNNER_URL` tek bir string. Bu migration, o tek node'u
-- "kümenin tek üyesi" olarak modelliyor. Amaç kapasite eklemek değil — bugün
-- hiçbir davranış değişmiyor — **şema migration'ını ucuzken yapmak**. Altı ay
-- sonra 3 node ve binlerce app varken aynı şemayı eklemek, bir günlük iş değil
-- bir haftalık migration projesi olur.
--
-- Yerleştirme modeli: **shard değil, havuz.** App'in kalıcı bir "evi" yok;
-- `current_node_id` app'in ŞU AN nerede çalıştığını söyler, nereye ait olduğunu
-- değil. Scale-to-zero sayesinde app'lerin çoğu her an 0 RAM'de ve sadece bir
-- dosya; uyanırken hangi node'da uyanacağına o an karar verilebilir. Bu yüzden
-- alan NULL olabilir (= şu an hiçbir yerde çalışmıyor, uyuyor).
--
-- Hepsi idempotent (IF NOT EXISTS).

-- ── nodes: kümenin üyeleri ──
CREATE TABLE IF NOT EXISTS nodes (
    id            TEXT PRIMARY KEY,                       -- "n_fsn1_01" — insan okunur, sabit
    role          TEXT NOT NULL DEFAULT 'all',            -- all | edge | worker | builder | data
    region        TEXT NOT NULL DEFAULT 'ams',            -- ams | sea | sin
    -- Scheduler'ın çağıracağı adres. Bugün RUNNER_URL ile aynı değer.
    url           TEXT NOT NULL,                          -- http://tunr-runner:9091
    status        TEXT NOT NULL DEFAULT 'ready',          -- ready | cordoned | draining | down
    -- Kapasite beyanı: scheduler bin-packing kararını buradan verir.
    cpu_cores     INT,
    memory_mb     INT,
    disk_gb       INT,
    -- gVisor checkpoint/restore'un ön koşulu: bir snapshot, ancak snapshot'ın
    -- alındığı makinedeki TÜM CPU özelliklerine sahip bir makinede restore
    -- edilebilir. Node katılırken beyan ettiği taban seti burada tutulur;
    -- uyumsuz node'a checkpoint'li app yerleştirilmez. (Modal bunu AWS'de zor
    -- yoldan öğrendi: bir instance tipi pclmulqdq desteklemiyordu → Invalid Opcode.)
    cpu_baseline  TEXT NOT NULL DEFAULT 'x86-64-v2',
    labels        JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at  TIMESTAMPTZ,                            -- sağlık: heartbeat
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_nodes_role_status ON nodes (role, status);

-- ── apps: yerleştirme alanları ──
-- current_node_id: app ŞU AN nerede koşuyor (NULL = uyuyor, hiçbir yerde).
--   ON DELETE SET NULL: node ölünce app'ler silinmez — sadece "evsiz" kalırlar
--   ve bir sonraki istekte scheduler onları başka bir node'a yerleştirir. Havuz
--   modelinin şemadaki karşılığı tam olarak bu satır.
-- home_region: coğrafi yerleşim tercihi; bölgeler arası backhaul kararı buradan.
ALTER TABLE apps ADD COLUMN IF NOT EXISTS current_node_id TEXT
    REFERENCES nodes(id) ON DELETE SET NULL;
ALTER TABLE apps ADD COLUMN IF NOT EXISTS home_region TEXT NOT NULL DEFAULT 'ams';

CREATE INDEX IF NOT EXISTS idx_apps_node ON apps (current_node_id);

-- ── node_metrics: kapasite zaman serisi ──
-- Node ekleme kararı "hissiyat"la değil metrik eşiğiyle verilmeli; bu tablo o
-- eşiklerin (mem.hot_utilization > %70, build.queue_wait_p95 > 60sn, ...)
-- dayanağı. Runner'ın /v1/host + /v1/stats çıktısı periyodik olarak buraya akar.
CREATE TABLE IF NOT EXISTS node_metrics (
    node_id          TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    ts               TIMESTAMPTZ NOT NULL DEFAULT now(),
    mem_total_bytes  BIGINT,
    mem_avail_bytes  BIGINT,
    -- App'lerin zram sonrası GERÇEK maliyeti. Kapasite modelinin tek gerçek
    -- girdisi bu: charged - swapped.
    mem_effective_bytes BIGINT,
    swap_total_bytes BIGINT,
    swap_used_bytes  BIGINT,
    -- PSI "some avg10", yüzde. Doygunluğun erken sinyali.
    mem_pressure     REAL,
    cpu_pressure     REAL,
    io_pressure      REAL,
    disk_used_gb     INT,
    apps_hot         INT,
    apps_warm        INT,
    apps_stopped     INT,
    build_queue_wait_p95_s REAL,
    PRIMARY KEY (node_id, ts)
);

-- Zaman serisi sorguları hep "son N saat" şeklinde; tersten index doğru olan.
CREATE INDEX IF NOT EXISTS idx_node_metrics_ts ON node_metrics (ts DESC);

-- ── Bu kutuyu kümenin ilk üyesi olarak kaydet ──
-- Tek node'da scheduler her zaman bunu döner. RUNNER_URL ile aynı adres;
-- gerçek değer relay tarafından ilk açılışta güncellenir (upsert).
INSERT INTO nodes (id, role, region, url, status, cpu_baseline)
VALUES ('n_local_01', 'all', 'ams', 'http://tunr-runner:9091', 'ready', 'x86-64-v2')
ON CONFLICT (id) DO NOTHING;

-- Mevcut app'leri o node'a bağla (bugün hepsi zaten orada koşuyor).
UPDATE apps SET current_node_id = 'n_local_01' WHERE current_node_id IS NULL;
