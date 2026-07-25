package db

// nodes.go — küme üyeleri ve kapasite zaman serisi (Çok-node planı, Faz A).
//
// Bu dosyanın amacı bugün kapasite eklemek değil. Amaç, node ekleme kararının
// **hissiyatla değil metrikle** verilebilmesi için gereken veriyi biriktirmeye
// bugünden başlamak. Plandaki tetikleyici tablosu (mem.hot_utilization > %70 →
// WORKER ekle, build.queue_wait_p95 > 60sn → BUILDER ekle) ancak arkasında
// 7 günlük p95 varsa işe yarar; o yüzden toplama, ihtiyaçtan önce başlamalı.

import (
	"context"
	"time"
)

// Node — kümenin bir üyesi.
type Node struct {
	ID          string
	Role        string // all | edge | worker | builder | data
	Region      string
	URL         string
	Status      string // ready | cordoned | draining | down
	CPUCores    int
	MemoryMB    int
	DiskGB      int
	CPUBaseline string
}

// NodeMetricSample — tek bir node'un tek bir andaki kapasite görüntüsü.
type NodeMetricSample struct {
	NodeID string

	MemTotalBytes uint64
	MemAvailBytes uint64
	// MemEffectiveBytes: app popülasyonunun host'a GERÇEK maliyeti — resident
	// (memory.current) + zram'e taşınmış sayfaların SIKIŞTIRILMIŞ boyutu.
	//
	// Dikkat: cgroup v2'de `memory.current` swap'e çıkmış sayfaları ZATEN
	// saymaz. Bu yüzden "charged - swapped" yanlıştır; uyuyan bir app'i sıfır
	// maliyetli gösterir ve kapasite modeli tam da bu sayıya güvenerek
	// oversubscribe edip OOM'a gider. Doğru hesap runner tarafında
	// CgroupStats.RealBytes(zramRatio) ile yapılır.
	MemEffectiveBytes uint64
	SwapTotalBytes    uint64
	SwapUsedBytes     uint64

	MemPressure float64
	CPUPressure float64
	IOPressure  float64

	AppsHot     int
	AppsWarm    int
	AppsStopped int

	BuildQueueWaitP95 float64
}

// UpsertNode — node kaydını ekler veya günceller. Node katılımı idempotent
// olmalı: bootstrap script'i tekrar çalıştığında yeni satır değil, güncelleme.
func (db *DB) UpsertNode(ctx context.Context, n Node) error {
	const q = `
		INSERT INTO nodes (id, role, region, url, status, cpu_cores, memory_mb, disk_gb, cpu_baseline, last_seen_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
		ON CONFLICT (id) DO UPDATE SET
			role         = EXCLUDED.role,
			region       = EXCLUDED.region,
			url          = EXCLUDED.url,
			status       = EXCLUDED.status,
			cpu_cores    = COALESCE(EXCLUDED.cpu_cores, nodes.cpu_cores),
			memory_mb    = COALESCE(EXCLUDED.memory_mb, nodes.memory_mb),
			disk_gb      = COALESCE(EXCLUDED.disk_gb, nodes.disk_gb),
			cpu_baseline = EXCLUDED.cpu_baseline,
			last_seen_at = now(),
			updated_at   = now()`
	_, err := db.pool.Exec(ctx, q, n.ID, n.Role, n.Region, n.URL, n.Status,
		nullIfZero(n.CPUCores), nullIfZero(n.MemoryMB), nullIfZero(n.DiskGB), n.CPUBaseline)
	return err
}

// Heartbeat — node'un hâlâ ayakta olduğunu işaretler.
func (db *DB) Heartbeat(ctx context.Context, nodeID string) error {
	_, err := db.pool.Exec(ctx, `UPDATE nodes SET last_seen_at = now() WHERE id = $1`, nodeID)
	return err
}

// ListNodes — scheduler'ın yerleştirme yapabileceği node'lar.
func (db *DB) ListNodes(ctx context.Context) ([]Node, error) {
	const q = `
		SELECT id, role, region, url, status,
		       COALESCE(cpu_cores, 0), COALESCE(memory_mb, 0), COALESCE(disk_gb, 0), cpu_baseline
		  FROM nodes
		 WHERE status IN ('ready', 'cordoned')
		 ORDER BY id`
	rows, err := db.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Role, &n.Region, &n.URL, &n.Status,
			&n.CPUCores, &n.MemoryMB, &n.DiskGB, &n.CPUBaseline); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// RecordNodeMetrics — bir örneği zaman serisine yazar.
func (db *DB) RecordNodeMetrics(ctx context.Context, s NodeMetricSample) error {
	const q = `
		INSERT INTO node_metrics (
			node_id, ts,
			mem_total_bytes, mem_avail_bytes, mem_effective_bytes,
			swap_total_bytes, swap_used_bytes,
			mem_pressure, cpu_pressure, io_pressure,
			apps_hot, apps_warm, apps_stopped, build_queue_wait_p95_s
		) VALUES ($1, now(), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (node_id, ts) DO NOTHING`
	_, err := db.pool.Exec(ctx, q, s.NodeID,
		int64(s.MemTotalBytes), int64(s.MemAvailBytes), int64(s.MemEffectiveBytes),
		int64(s.SwapTotalBytes), int64(s.SwapUsedBytes),
		s.MemPressure, s.CPUPressure, s.IOPressure,
		s.AppsHot, s.AppsWarm, s.AppsStopped, s.BuildQueueWaitP95)
	return err
}

// PruneNodeMetrics — retention'dan eski örnekleri siler.
//
// Bu tablo dakikada bir satır alıyor; budanmazsa yılda ~500k satır eder. Karar
// için gereken pencere 7–30 gün, o yüzden fazlasını tutmak sadece disk yiyor.
func (db *DB) PruneNodeMetrics(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := db.pool.Exec(ctx,
		`DELETE FROM node_metrics WHERE ts < now() - $1::interval`,
		olderThan.String())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// nullIfZero — 0 değerini SQL NULL'a çevirir, böylece "beyan edilmemiş" ile
// "gerçekten sıfır" karışmaz ve UPSERT'teki COALESCE mevcut değeri korur.
func nullIfZero(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
