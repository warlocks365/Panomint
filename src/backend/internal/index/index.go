package index

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/queue"
)

// 种子 owner 用户（开发环境；bcrypt 哈希对应密码 "password"）。
const (
	seedEmail        = "owner@pano.local"
	seedPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
)

// ScanStats 一次扫描的统计。
type ScanStats struct {
	JobID     string // index_jobs.id
	Total     int    // 发现的媒体文件数
	Inserted  int    // 新写入 media 的行数
	Duplicate int    // 同 hash 跳过数
	Failed    int    // 元数据/入库失败数
}

// Indexer 媒体索引器。
type Indexer struct {
	db *pgxpool.Pool
	q  *queue.Queue
}

// New 创建索引器。
func New(db *pgxpool.Pool, q *queue.Queue) *Indexer {
	return &Indexer{db: db, q: q}
}

// EnsureSeedUser 保证存在 owner 种子用户，返回其 id（幂等）。
func EnsureSeedUser(ctx context.Context, db *pgxpool.Pool) (string, error) {
	var id string
	err := db.QueryRow(ctx,
		`INSERT INTO users (email, display_name, password_hash, role_id)
		 SELECT $1, 'Owner', $2, r.id FROM roles r WHERE r.name='owner'
		 ON CONFLICT (email) DO UPDATE SET email=EXCLUDED.email
		 RETURNING id`, seedEmail, seedPasswordHash).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("种子用户: %w", err)
	}
	return id, nil
}

// findByHash 查重：同 hash 且非副本的 media 是否已存在。
func (x *Indexer) findByHash(ctx context.Context, hash string) (string, error) {
	var id string
	err := x.db.QueryRow(ctx,
		`SELECT id FROM media WHERE hash=$1 AND duplicate_of IS NULL AND deleted_at IS NULL LIMIT 1`,
		hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// insertMedia 写入一行 media，返回新 id。
func (x *Indexer) insertMedia(ctx context.Context, ownerID string, e FileEntry, m *Meta) (string, error) {
	takenAt := m.TakenAt
	if takenAt == nil {
		takenAt = &e.ModTime // 无 EXIF/creation_time 回退 mtime
	}
	var duration any
	if m.DurationSec != nil {
		duration = *m.DurationSec
	}
	var id string
	err := x.db.QueryRow(ctx, `
		INSERT INTO media (
			type, space, owner_id, path, folder_path, filename,
			taken_at, width, height, duration, codec, fps,
			gps, hash, filesize, camera_make, camera_model
		) VALUES (
			$1, 'personal', $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11,
			CASE WHEN $12::float8 IS NOT NULL AND $13::float8 IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint($13, $12), 4326) END,
			$14, $15, $16, $17
		) RETURNING id`,
		string(e.Kind), ownerID, e.Rel, e.Folder, e.Filename,
		*takenAt, nullInt(m.Width), nullInt(m.Height), duration, nullStr(m.Codec), nullFloat(m.FPS),
		m.Lat, m.Lng,
		e.Hash, e.Size, nullStr(m.CameraMake), nullStr(m.CameraModel),
	).Scan(&id)
	return id, err
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

// Scan 扫描 root 目录：建 index_jobs 任务行，逐文件提取元数据、hash 去重入库、派发缩略图任务。
func (x *Indexer) Scan(ctx context.Context, root string) (*ScanStats, error) {
	ownerID, err := EnsureSeedUser(ctx, x.db)
	if err != nil {
		return nil, err
	}

	// 建任务行（先 running，拿到总数后更新 total）
	var jobID string
	if err := x.db.QueryRow(ctx,
		`INSERT INTO index_jobs (kind, user_id, status, started_at)
		 VALUES ('full', $1, 'running', now()) RETURNING id`, ownerID).Scan(&jobID); err != nil {
		return nil, fmt.Errorf("建 index_jobs: %w", err)
	}
	st := &ScanStats{JobID: jobID}

	fail := func(err error) (*ScanStats, error) {
		_, _ = x.db.Exec(context.Background(),
			`UPDATE index_jobs SET status='failed', finished_at=now() WHERE id=$1`, jobID)
		return st, err
	}

	entries, err := ScanDir(ctx, root)
	if err != nil {
		return fail(fmt.Errorf("扫描目录: %w", err))
	}
	st.Total = len(entries)
	if _, err := x.db.Exec(ctx,
		`UPDATE index_jobs SET total=$1 WHERE id=$2`, st.Total, jobID); err != nil {
		return fail(err)
	}

	for i, e := range entries {
		if err := x.indexOne(ctx, ownerID, e, st); err != nil {
			log.Printf("索引失败 %s: %v", e.Rel, err)
			st.Failed++
		}
		// 每 10 个刷一次进度，最后一次由收尾统一更新
		if (i+1)%10 == 0 {
			_, _ = x.db.Exec(ctx, `UPDATE index_jobs SET processed=$1 WHERE id=$2`, i+1, jobID)
		}
	}

	_, err = x.db.Exec(ctx,
		`UPDATE index_jobs SET status='done', processed=$1, finished_at=now() WHERE id=$2`,
		st.Total, jobID)
	if err != nil {
		return fail(err)
	}
	return st, nil
}

// indexOne 处理单个文件：去重判定 → 元数据 → 入库 → 缩略图任务入队。
func (x *Indexer) indexOne(ctx context.Context, ownerID string, e FileEntry, st *ScanStats) error {
	dupID, err := x.findByHash(ctx, e.Hash)
	if err != nil {
		return err
	}
	if dupID != "" {
		st.Duplicate++
		return nil
	}

	var m *Meta
	if e.Kind == KindVideo {
		m, err = ExtractVideoMeta(ctx, e.Path)
	} else {
		m, err = ExtractPhotoMeta(e.Path)
	}
	if err != nil {
		return fmt.Errorf("元数据: %w", err)
	}

	mediaID, err := x.insertMedia(ctx, ownerID, e, m)
	if err != nil {
		return fmt.Errorf("写 media: %w", err)
	}
	st.Inserted++

	// 缩略图任务入队（Worker 异步生成三档 WebP）
	payload := map[string]string{
		"media_id": mediaID,
		"path":     e.Path,
		"kind":     string(e.Kind),
	}
	if m.DurationSec != nil {
		payload["duration_sec"] = fmt.Sprint(*m.DurationSec)
	}
	_, err = x.q.Enqueue(ctx, queue.Job{Kind: "thumbnail", Payload: payload})
	if err != nil {
		return fmt.Errorf("缩略图入队: %w", err)
	}
	return nil
}
