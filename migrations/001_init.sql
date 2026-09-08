-- ============================================================
-- Job Aggregator — initial schema
-- Target: Neon Postgres (free tier)
-- ============================================================

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ------------------------------------------------------------
-- sources: daftar situs yang di-scrape
-- ------------------------------------------------------------
CREATE TABLE sources (
    id           SERIAL PRIMARY KEY,
    slug         TEXT NOT NULL UNIQUE,       -- 'dealls', 'kalibrr'
    name         TEXT NOT NULL,
    base_url     TEXT NOT NULL,
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    last_run_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ------------------------------------------------------------
-- companies: dinormalisasi biar dedup lintas sumber gampang
-- ------------------------------------------------------------
CREATE TABLE companies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    name_normalized TEXT NOT NULL,           -- lowercase, tanpa "PT", "Tbk", dll
    logo_url        TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_companies_normalized ON companies (name_normalized);
CREATE INDEX idx_companies_trgm ON companies USING GIN (name_normalized gin_trgm_ops);

-- ------------------------------------------------------------
-- jobs: tabel inti
-- ------------------------------------------------------------
CREATE TYPE salary_confidence AS ENUM ('exact', 'range', 'estimated', 'unknown');
CREATE TYPE work_mode        AS ENUM ('onsite', 'remote', 'hybrid', 'unknown');
CREATE TYPE experience_level AS ENUM ('intern', 'junior', 'mid', 'senior', 'lead', 'unknown');

CREATE TABLE jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- identitas
    title             TEXT NOT NULL,
    title_normalized  TEXT NOT NULL,          -- buat fuzzy dedup
    company_id        UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    description       TEXT,

    -- gaji (disimpan sebagai integer rupiah, BUKAN float)
    salary_min        BIGINT,
    salary_max        BIGINT,
    salary_currency   TEXT NOT NULL DEFAULT 'IDR',
    salary_conf       salary_confidence NOT NULL DEFAULT 'unknown',
    salary_raw        TEXT,                   -- simpen teks aslinya buat debug parser

    -- klasifikasi
    stack             TEXT[] NOT NULL DEFAULT '{}',
    location          TEXT,
    location_city     TEXT,
    mode              work_mode NOT NULL DEFAULT 'unknown',
    level             experience_level NOT NULL DEFAULT 'unknown',

    -- asal data
    source_id         INT NOT NULL REFERENCES sources(id),
    source_url        TEXT NOT NULL,
    source_job_id     TEXT,                   -- id asli di situs sumber

    -- dedup
    fingerprint       TEXT NOT NULL,          -- sha256(company_normalized + title_normalized)
    canonical_job_id  UUID REFERENCES jobs(id) ON DELETE SET NULL,  -- NULL = ini yang kanonik

    -- pencarian
    search_vector     TSVECTOR,

    -- waktu
    posted_at         TIMESTAMPTZ,
    first_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_active         BOOLEAN NOT NULL DEFAULT TRUE
);

-- satu loker per sumber gak boleh dobel
CREATE UNIQUE INDEX idx_jobs_source_unique ON jobs (source_id, source_url);

-- index buat filter utama
CREATE INDEX idx_jobs_active_posted   ON jobs (is_active, posted_at DESC) WHERE canonical_job_id IS NULL;
CREATE INDEX idx_jobs_stack           ON jobs USING GIN (stack);
CREATE INDEX idx_jobs_salary          ON jobs (salary_min, salary_max) WHERE salary_min IS NOT NULL;
CREATE INDEX idx_jobs_fingerprint     ON jobs (fingerprint);
CREATE INDEX idx_jobs_title_trgm      ON jobs USING GIN (title_normalized gin_trgm_ops);
CREATE INDEX idx_jobs_search          ON jobs USING GIN (search_vector);
CREATE INDEX idx_jobs_mode_level      ON jobs (mode, level) WHERE is_active = TRUE;

-- auto-update search_vector
CREATE OR REPLACE FUNCTION jobs_search_vector_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('simple', COALESCE(NEW.title, '')), 'A') ||
        setweight(to_tsvector('simple', COALESCE(array_to_string(NEW.stack, ' '), '')), 'B') ||
        setweight(to_tsvector('simple', COALESCE(NEW.description, '')), 'C');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_jobs_search_vector
    BEFORE INSERT OR UPDATE OF title, stack, description ON jobs
    FOR EACH ROW EXECUTE FUNCTION jobs_search_vector_update();

-- ------------------------------------------------------------
-- scrape_runs: audit trail tiap kali scraper jalan
-- ------------------------------------------------------------
CREATE TYPE run_status AS ENUM ('pending', 'running', 'success', 'failed');

CREATE TABLE scrape_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id      INT NOT NULL REFERENCES sources(id),
    status         run_status NOT NULL DEFAULT 'pending',
    jobs_found     INT NOT NULL DEFAULT 0,
    jobs_new       INT NOT NULL DEFAULT 0,
    jobs_duplicate INT NOT NULL DEFAULT 0,
    error_message  TEXT,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at    TIMESTAMPTZ
);

CREATE INDEX idx_scrape_runs_source ON scrape_runs (source_id, started_at DESC);

-- ------------------------------------------------------------
-- alerts: user langganan notifikasi
-- ------------------------------------------------------------
CREATE TABLE alerts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email            TEXT NOT NULL,
    filter_stack     TEXT[] DEFAULT '{}',
    filter_salary_min BIGINT,
    filter_mode      work_mode,
    filter_level     experience_level,
    filter_city      TEXT,
    is_verified      BOOLEAN NOT NULL DEFAULT FALSE,
    verify_token     TEXT,
    unsubscribe_token TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    last_notified_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alerts_active ON alerts (is_verified) WHERE is_verified = TRUE;
CREATE UNIQUE INDEX idx_alerts_email ON alerts (email);

-- ------------------------------------------------------------
-- seed sumber awal
-- ------------------------------------------------------------
INSERT INTO sources (slug, name, base_url) VALUES
    ('dealls',  'Dealls',  'https://dealls.com'),
    ('kalibrr', 'Kalibrr', 'https://www.kalibrr.com');
