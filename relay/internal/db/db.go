package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB — PostgreSQL bağlantı havuzu.
// pgx v5 kullanıyoruz çünkü:
//  - database/sql'den 2-3x daha hızlı
//  - Native PostgreSQL type desteği (UUID, JSONB, citext)
//  - Context-first API (timeout, cancel kolay)
//
// GÜVENLİK: Bağlantı string'i env'den alınır, hardcode asla.

// DB — pool wrapper
type DB struct {
	pool *pgxpool.Pool
}

// New — DB bağlantısı kur
// dsn format: postgres://user:password@host:5432/dbname?sslmode=require
//
// GÜVENLİK: sslmode=require — production'da plaintext Postgres bağlantısı yok
func New(ctx context.Context, dsn string) (*DB, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DB config parse hatası: %w", err)
	}

	// Bağlantı havuzu ayarları
	config.MaxConns = 20                       // max 20 eş zamanlı bağlantı
	config.MinConns = 2                        // her zaman 2 bağlantı hazır
	config.MaxConnLifetime = 30 * time.Minute  // bağlantıyı 30dk sonra yenile
	config.MaxConnIdleTime = 5 * time.Minute   // 5dk boş kalan bağlantıyı kapat
	config.HealthCheckPeriod = 1 * time.Minute // DB hala ayakta mı?

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("DB pool oluşturulamadı: %w", err)
	}

	// Bağlantıyı test et
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("DB ping başarısız: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Close — bağlantı havuzunu kapat
func (db *DB) Close() { db.pool.Close() }

// ─── KULLANICI İŞLEMLERİ ─────────────────────────────────────────────────────

// User — kullanıcı kaydı
type User struct {
	ID               string
	Email            string
	Plan             string
	PaddleCustomerID string
	CreatedAt        time.Time
}

// GetOrCreateUser — email ile kullanıcı bul veya oluştur (magic link sonrası çağrılır)
func (db *DB) GetOrCreateUser(ctx context.Context, email string) (*User, bool, error) {
	// Önce bul
	const selectQ = `SELECT id, email, plan, COALESCE(paddle_customer_id, '') FROM users WHERE email = $1`
	row := db.pool.QueryRow(ctx, selectQ, email)
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Plan, &u.PaddleCustomerID)
	if err == nil {
		return &u, false, nil // mevcut kullanıcı
	}

	// Yoksa oluştur
	const insertQ = `INSERT INTO users (email) VALUES ($1) RETURNING id, email, plan`
	row = db.pool.QueryRow(ctx, insertQ, email)
	if err := row.Scan(&u.ID, &u.Email, &u.Plan); err != nil {
		return nil, false, fmt.Errorf("kullanıcı oluşturulamadı: %w", err)
	}
	return &u, true, nil // yeni kullanıcı
}

// GetUserByID — ID ile kullanıcı bul
func (db *DB) GetUserByID(ctx context.Context, id string) (*User, error) {
	const q = `SELECT id, email, plan, COALESCE(paddle_customer_id, '') FROM users WHERE id = $1`
	var u User
	err := db.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.Email, &u.Plan, &u.PaddleCustomerID)
	if err != nil {
		return nil, fmt.Errorf("kullanıcı bulunamadı: %w", err)
	}
	return &u, nil
}

// UpdateUserPlan — Paddle webhook sonrası plan güncelle
func (db *DB) UpdateUserPlan(ctx context.Context, paddleCustomerID, newPlan string) error {
	const q = `UPDATE users SET plan = $1 WHERE paddle_customer_id = $2`
	_, err := db.pool.Exec(ctx, q, newPlan, paddleCustomerID)
	return err
}

// UpdateUserPlanByCustomerID updates plan by Paddle customer ID and returns whether a row changed.
func (db *DB) UpdateUserPlanByCustomerID(ctx context.Context, paddleCustomerID, newPlan string) (bool, error) {
	const q = `UPDATE users SET plan = $1 WHERE paddle_customer_id = $2`
	tag, err := db.pool.Exec(ctx, q, newPlan, paddleCustomerID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// LinkPaddleCustomerByEmail stores Paddle customer ID for an existing user.
func (db *DB) LinkPaddleCustomerByEmail(ctx context.Context, email, paddleCustomerID string) (bool, error) {
	const q = `
		UPDATE users
		SET paddle_customer_id = $1
		WHERE email = $2
		  AND (paddle_customer_id IS NULL OR paddle_customer_id = '' OR paddle_customer_id = $1)
	`
	tag, err := db.pool.Exec(ctx, q, paddleCustomerID, email)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─── MAGIC TOKEN İŞLEMLERİ ───────────────────────────────────────────────────

// SaveMagicToken — magic link token'ı kaydet
func (db *DB) SaveMagicToken(ctx context.Context, token, email string, expiresAt time.Time) error {
	const q = `INSERT INTO magic_tokens (token, email, expires_at) VALUES ($1, $2, $3)`
	_, err := db.pool.Exec(ctx, q, token, email, expiresAt)
	return err
}

// ConsumeMagicToken — token'ı doğrula ve tek seferlik kullanıma işaretle
// GÜVENLİK: FOR UPDATE ile transaction'da kullanılmalı (çift tıklama saldırısı önlenir)
func (db *DB) ConsumeMagicToken(ctx context.Context, token string) (string, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var email string
	var usedAt *time.Time
	var expiresAt time.Time
	const q = `SELECT email, expires_at, used_at FROM magic_tokens WHERE token = $1 FOR UPDATE`
	err = tx.QueryRow(ctx, q, token).Scan(&email, &expiresAt, &usedAt)
	if err != nil {
		return "", fmt.Errorf("geçersiz token")
	}

	// Süresi dolmuş mu?
	if time.Now().After(expiresAt) {
		return "", fmt.Errorf("token süresi dolmuş")
	}

	// Zaten kullanılmış mı?
	if usedAt != nil {
		return "", fmt.Errorf("token zaten kullanılmış")
	}

	// Kullanıldı olarak işaretle
	const markQ = `UPDATE magic_tokens SET used_at = now() WHERE token = $1`
	if _, err := tx.Exec(ctx, markQ, token); err != nil {
		return "", err
	}

	return email, tx.Commit(ctx)
}

// ─── TUNNEL KAYITLARI ────────────────────────────────────────────────────────

// RecordTunnelConnect — tunnel bağlantısını DB'ye kaydet
func (db *DB) RecordTunnelConnect(ctx context.Context, shortID, userID, subdomain string) error {
	const q = `
		INSERT INTO tunnels (tunnel_short_id, user_id, subdomain)
		VALUES ($1, $2::uuid, $3)
		ON CONFLICT (tunnel_short_id) DO NOTHING`
	_, err := db.pool.Exec(ctx, q, shortID, userID, subdomain)
	return err
}

// RecordTunnelDisconnect — tunnel kapanışını kaydet
func (db *DB) RecordTunnelDisconnect(ctx context.Context, shortID string) error {
	const q = `UPDATE tunnels SET disconnected_at = now() WHERE tunnel_short_id = $1`
	_, err := db.pool.Exec(ctx, q, shortID)
	return err
}

type TunnelRecord struct {
	ShortID        string
	Subdomain      string
	ConnectedAt    time.Time
	DisconnectedAt *time.Time
}

func (db *DB) ListUserTunnels(ctx context.Context, userID string, limit int) ([]TunnelRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	const q = `
		SELECT tunnel_short_id, subdomain, connected_at, disconnected_at
		FROM tunnels
		WHERE user_id = $1::uuid
		ORDER BY connected_at DESC
		LIMIT $2
	`
	rows, err := db.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]TunnelRecord, 0, limit)
	for rows.Next() {
		var record TunnelRecord
		if err := rows.Scan(&record.ShortID, &record.Subdomain, &record.ConnectedAt, &record.DisconnectedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

// CheckDailyQuota — günlük istek kotası aşıldı mı?
func (db *DB) CheckDailyQuota(ctx context.Context, userID string) (bool, error) {
	const q = `SELECT check_daily_quota($1::uuid)`
	var ok bool
	err := db.pool.QueryRow(ctx, q, userID).Scan(&ok)
	return ok, err
}

// ─── AUDIT LOG ───────────────────────────────────────────────────────────────

// AuditLog — önemli eylemi kaydet
func (db *DB) AuditLog(ctx context.Context, userID, action string, detail map[string]interface{}) {
	// Async kaydet — ana akışı yavaşlatmasın
	go func() {
		const q = `INSERT INTO audit_log (user_id, action, detail) VALUES ($1::uuid, $2, $3)`
		_, _ = db.pool.Exec(context.Background(), q, userID, action, detail)
	}()
}

// EnsureUserDomainsTable creates user_domains table used by dashboard + relay subdomain ownership checks.
func (db *DB) EnsureUserDomainsTable(ctx context.Context) error {
	const createTable = `
		CREATE TABLE IF NOT EXISTS user_domains (
			id BIGSERIAL PRIMARY KEY,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			type TEXT NOT NULL CHECK (type IN ('subdomain', 'custom')),
			domain TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (user_id, type, domain)
		)
	`
	if _, err := db.pool.Exec(ctx, createTable); err != nil {
		return err
	}
	const createIndex = `
		CREATE INDEX IF NOT EXISTS idx_user_domains_type_domain_updated
		ON user_domains (type, domain, updated_at DESC)
	`
	_, err := db.pool.Exec(ctx, createIndex)
	return err
}

// GetReservedSubdomainOwner returns owner user_id for a reserved subdomain if present.
func (db *DB) GetReservedSubdomainOwner(ctx context.Context, subdomain string) (string, bool, error) {
	if err := db.EnsureUserDomainsTable(ctx); err != nil {
		return "", false, err
	}

	const q = `
		SELECT user_id::text
		FROM user_domains
		WHERE type = 'subdomain' AND domain = $1
		ORDER BY updated_at DESC
		LIMIT 1
	`
	var ownerUserID string
	err := db.pool.QueryRow(ctx, q, subdomain).Scan(&ownerUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return ownerUserID, true, nil
}
