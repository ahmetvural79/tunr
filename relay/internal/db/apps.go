package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// apps.go — pivot Faz 0 data access for apps / routes (cloud upstream).
// Schema lives in migrations/003_apps.sql. The dashboard (Next.js/pg) reads the
// same tables; keep column names stable.

// App is a persistent application (stable across deployments).
type App struct {
	ID           string
	UserID       string
	Name         string // == subdomain
	Region       string
	InternalPort int
	EdgeSecret   string
	Status       string
}

// CloudRoute is a resolved subdomain -> cloud upstream row (routes JOIN apps).
type CloudRoute struct {
	Subdomain   string
	AppID       string
	CloudURL    string
	WakeTimeout int
	EdgeSecret  string
}

// CreateApp inserts a new app. user_id must be a UUID.
func (db *DB) CreateApp(ctx context.Context, a App) error {
	const q = `
		INSERT INTO apps (id, user_id, name, region, internal_port, edge_secret, status)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)`
	region := a.Region
	if region == "" {
		region = "ams"
	}
	port := a.InternalPort
	if port == 0 {
		port = 8080
	}
	status := a.Status
	if status == "" {
		status = "created"
	}
	_, err := db.pool.Exec(ctx, q, a.ID, a.UserID, a.Name, region, port, a.EdgeSecret, status)
	return err
}

// GetAppByName returns an app by its (unique) name/subdomain.
func (db *DB) GetAppByName(ctx context.Context, name string) (*App, bool, error) {
	const q = `
		SELECT id, user_id::text, name, region, internal_port, edge_secret, status
		FROM apps WHERE name = $1`
	var a App
	err := db.pool.QueryRow(ctx, q, name).Scan(
		&a.ID, &a.UserID, &a.Name, &a.Region, &a.InternalPort, &a.EdgeSecret, &a.Status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &a, true, nil
}

// GetOrCreateAppByName returns the app for name (must be owned by userID),
// creating it with the given id/edge_secret if it doesn't exist yet.
func (db *DB) GetOrCreateAppByName(ctx context.Context, userID, name, id, edgeSecret string, internalPort int) (*App, bool, error) {
	existing, ok, err := db.GetAppByName(ctx, name)
	if err != nil {
		return nil, false, err
	}
	if ok {
		if existing.UserID != userID {
			return nil, false, fmt.Errorf("name %q is taken", name)
		}
		return existing, false, nil
	}
	app := App{ID: id, UserID: userID, Name: name, Region: "ams", InternalPort: internalPort, EdgeSecret: edgeSecret, Status: "created"}
	if err := db.CreateApp(ctx, app); err != nil {
		return nil, false, err
	}
	return &app, true, nil
}

// NextDeploymentSeq returns the next per-app deployment sequence number.
func (db *DB) NextDeploymentSeq(ctx context.Context, appID string) (int, error) {
	const q = `SELECT COALESCE(MAX(seq), 0) + 1 FROM deployments WHERE app_id = $1`
	var seq int
	err := db.pool.QueryRow(ctx, q, appID).Scan(&seq)
	return seq, err
}

// InsertDeployment creates a deployment row.
func (db *DB) InsertDeployment(ctx context.Context, id, appID string, seq int, status string) error {
	const q = `INSERT INTO deployments (id, app_id, seq, status) VALUES ($1, $2, $3, $4)`
	_, err := db.pool.Exec(ctx, q, id, appID, seq, status)
	return err
}

// UpdateDeployment sets a deployment's status (+ optional image_ref / error).
func (db *DB) UpdateDeployment(ctx context.Context, id, status, imageRef, errMsg string) error {
	const q = `UPDATE deployments SET status = $1,
	              image_ref = COALESCE(NULLIF($2, ''), image_ref),
	              error     = NULLIF($3, '')
	           WHERE id = $4`
	_, err := db.pool.Exec(ctx, q, status, imageRef, errMsg, id)
	return err
}

// SetAppStatus updates an app's lifecycle status.
func (db *DB) SetAppStatus(ctx context.Context, appID, status string) error {
	const q = `UPDATE apps SET status = $1, updated_at = now() WHERE id = $2`
	_, err := db.pool.Exec(ctx, q, status, appID)
	return err
}

// DeleteApp removes an app (routes + deployments cascade via FK) by ID.
func (db *DB) DeleteApp(ctx context.Context, appID string) error {
	const q = `DELETE FROM apps WHERE id = $1`
	_, err := db.pool.Exec(ctx, q, appID)
	return err
}

// AppListRow is a compact app row for CLI/MCP listings.
type AppListRow struct {
	ID     string
	Name   string
	Region string
	Status string
}

// ListAppsByUser returns the user's apps, newest first.
func (db *DB) ListAppsByUser(ctx context.Context, userID string) ([]AppListRow, error) {
	const q = `
		SELECT id, name, region, status
		FROM apps
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 100`
	rows, err := db.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppListRow
	for rows.Next() {
		var a AppListRow
		if err := rows.Scan(&a.ID, &a.Name, &a.Region, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertCloudRoute maps a subdomain to a cloud upstream (fires NOTIFY routes_changed).
func (db *DB) UpsertCloudRoute(ctx context.Context, subdomain, appID, cloudURL string, wakeTimeout int) error {
	if wakeTimeout <= 0 {
		wakeTimeout = 30
	}
	const q = `
		INSERT INTO routes (subdomain, kind, app_id, cloud_url, wake_timeout, updated_at)
		VALUES ($1, 'cloud', $2, $3, $4, now())
		ON CONFLICT (subdomain) DO UPDATE
		  SET kind = 'cloud', app_id = EXCLUDED.app_id, cloud_url = EXCLUDED.cloud_url,
		      wake_timeout = EXCLUDED.wake_timeout, updated_at = now()`
	_, err := db.pool.Exec(ctx, q, subdomain, appID, cloudURL, wakeTimeout)
	return err
}

// DeleteRoute removes a subdomain route (fires NOTIFY routes_changed).
func (db *DB) DeleteRoute(ctx context.Context, subdomain string) error {
	const q = `DELETE FROM routes WHERE subdomain = $1`
	_, err := db.pool.Exec(ctx, q, subdomain)
	return err
}

// LoadCloudRoutes returns all kind='cloud' routes joined with their app secret.
func (db *DB) LoadCloudRoutes(ctx context.Context) ([]CloudRoute, error) {
	const q = `
		SELECT r.subdomain, COALESCE(r.app_id, ''), COALESCE(r.cloud_url, ''),
		       r.wake_timeout, COALESCE(a.edge_secret, '')
		FROM routes r
		LEFT JOIN apps a ON a.id = r.app_id
		WHERE r.kind = 'cloud'`
	rows, err := db.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CloudRoute
	for rows.Next() {
		var cr CloudRoute
		if err := rows.Scan(&cr.Subdomain, &cr.AppID, &cr.CloudURL, &cr.WakeTimeout, &cr.EdgeSecret); err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

// GetCloudRoute returns a single cloud route by subdomain.
func (db *DB) GetCloudRoute(ctx context.Context, subdomain string) (CloudRoute, bool, error) {
	const q = `
		SELECT r.subdomain, COALESCE(r.app_id, ''), COALESCE(r.cloud_url, ''),
		       r.wake_timeout, COALESCE(a.edge_secret, '')
		FROM routes r
		LEFT JOIN apps a ON a.id = r.app_id
		WHERE r.subdomain = $1 AND r.kind = 'cloud'`
	var cr CloudRoute
	err := db.pool.QueryRow(ctx, q, subdomain).Scan(
		&cr.Subdomain, &cr.AppID, &cr.CloudURL, &cr.WakeTimeout, &cr.EdgeSecret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CloudRoute{}, false, nil
		}
		return CloudRoute{}, false, err
	}
	return cr, true, nil
}

// ListenRoutes holds a dedicated connection and calls onChange(subdomain) for
// every NOTIFY routes_changed. Blocks until ctx is cancelled or the connection
// drops (caller reconnects + does a full reload as fallback).
func (db *DB) ListenRoutes(ctx context.Context, onChange func(subdomain string)) error {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN routes_changed"); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait notification: %w", err)
		}
		onChange(n.Payload)
	}
}
