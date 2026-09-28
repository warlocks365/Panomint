package index

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/ffmpeg"
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

// Geocoder 逆地理编码能力抽象（实现见 internal/geo.AmapGeocoder）。
// 实现必须容忍失败：无 Key / 配额耗尽 / 网络不通时返回错误，
// 由索引器降级跳过、仅记日志，绝不阻塞媒体入库。
type Geocoder interface {
	ReverseGeocode(ctx context.Context, lat, lng float64) (string, error)
}

// Indexer 媒体索引器。
type Indexer struct {
	db       *pgxpool.Pool
	q        *queue.Queue
	geocoder Geocoder // 可为 nil，表示不启用逆地理编码
}

// New 创建索引器（不启用逆地理编码）。
func New(db *pgxpool.Pool, q *queue.Queue) *Indexer {
	return &Indexer{db: db, q: q}
}

// NewWithGeocoder 创建带逆地理编码能力的索引器：入库时若取到 GPS，
// 会调用 g 反查地名写入 media.place。g 为 nil 时行为等价于 New。
func NewWithGeocoder(db *pgxpool.Pool, q *queue.Queue, g Geocoder) *Indexer {
	return &Indexer{db: db, q: q, geocoder: g}
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

// pathIndexed 同一相对路径（media.path 唯一对应一次导入）是否已有未删除记录。
func (x *Indexer) pathIndexed(ctx context.Context, rel string) (bool, error) {
	var one bool
	err := x.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM media WHERE path = $1 AND deleted_at IS NULL)`, rel).Scan(&one)
	return one, err
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
			gps, hash, filesize, camera_make, camera_model,
			is_360, projection, place
		) VALUES (
			$1, 'personal', $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11,
			CASE WHEN $12::float8 IS NOT NULL AND $13::float8 IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint($13, $12), 4326) END,
			$14, $15, $16, $17,
			$18, $19, $20
		) RETURNING id`,
		string(e.Kind), ownerID, e.Rel, e.Folder, e.Filename,
		*takenAt, nullInt(m.Width), nullInt(m.Height), duration, nullStr(m.Codec), nullFloat(m.FPS),
		m.Lat, m.Lng,
		e.Hash, e.Size, nullStr(m.CameraMake), nullStr(m.CameraModel),
		m.Is360, nullStr(m.Projection), nullStr(m.Place),
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

// EntryOutcome 单条索引结果（IndexEntries 回调用）。
type EntryOutcome string

const (
	OutcomeInserted  EntryOutcome = "inserted"
	OutcomeDuplicate EntryOutcome = "duplicate"
	OutcomeFailed    EntryOutcome = "failed"
)

// Scan 扫描 root 目录：建 index_jobs 任务行，逐文件提取元数据、hash 去重入库、派发缩略图任务。
// 入库媒体归属 seed user（本地目录导入的历史语义）。
func (x *Indexer) Scan(ctx context.Context, root string) (*ScanStats, error) {
	ownerID, err := EnsureSeedUser(ctx, x.db)
	if err != nil {
		return nil, err
	}
	return x.ScanAs(ctx, root, ownerID)
}

// ScanAs 与 Scan 同，但入库媒体归属调用方指定的 userID（Job000070 挂载导入：
// 远程挂载点的媒体归挂载属主，保证 mediascope 可见性谓词语义一致）。
func (x *Indexer) ScanAs(ctx context.Context, root, ownerID string) (*ScanStats, error) {
	return x.ScanAsPrefixed(ctx, root, ownerID, "")
}

// ScanAsPrefixed 与 ScanAs 同，但 media.path/folder_path 加前缀（相对 MEDIA_ROOT）。
// 挂载导入用：扫描根是落地子目录（/data/media/_imports/<id8>），而库里 path 必须相对
// MEDIA_ROOT，否则按 folder_path 浏览/拼路径时全链错位（文件在 _imports/<id8>/x，
// 库里记成 x——浏览按 /data/media/x 找文件找不到）。prefix 形态如 "_imports/<id8>"。
func (x *Indexer) ScanAsPrefixed(ctx context.Context, root, ownerID, prefix string) (*ScanStats, error) {
	jobID, err := x.createScanJob(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return x.scanWithJob(ctx, root, ownerID, prefix, jobID)
}

// ScanAsync 异步扫描（Job000113 管理端扫描导入 / R1-c 启动自扫共用）：先同步建
// index_jobs 任务行并返回 jobID（调用方据此立即响应/记日志），扫描本体在后台
// goroutine 执行，完成（含失败）经 onDone 回调通知（可为 nil）。入库归属 ownerID。
//
// 后台刻意改用 context.Background()：扫描是长任务，不得随触发它的 HTTP 请求取消
// 而中断半截（进度/终态由 scanWithJob 写回 index_jobs，可断点观察）。
func (x *Indexer) ScanAsync(ctx context.Context, root, ownerID string, onDone func(error)) (string, error) {
	jobID, err := x.createScanJob(ctx, ownerID)
	if err != nil {
		return "", err
	}
	go func() {
		_, serr := x.scanWithJob(context.Background(), root, ownerID, "", jobID)
		if serr != nil {
			log.Printf("异步扫描失败 job=%s root=%s: %v", jobID, root, serr)
		}
		if onDone != nil {
			onDone(serr)
		}
	}()
	return jobID, nil
}

// createScanJob 建 index_jobs 任务行（running），返回任务 id（建行的同步/异步路径共用）。
func (x *Indexer) createScanJob(ctx context.Context, ownerID string) (string, error) {
	var jobID string
	if err := x.db.QueryRow(ctx,
		`INSERT INTO index_jobs (kind, user_id, status, started_at)
		 VALUES ('full', $1, 'running', now()) RETURNING id`, ownerID).Scan(&jobID); err != nil {
		return "", fmt.Errorf("建 index_jobs: %w", err)
	}
	return jobID, nil
}

// scanWithJob 在既有任务行上执行扫描：逐文件提取元数据、hash 去重入库、派发缩略图任务，
// 并把 total/processed/终态写回 index_jobs。失败会把任务行置 failed（fail 闭包）。
func (x *Indexer) scanWithJob(ctx context.Context, root, ownerID, prefix, jobID string) (*ScanStats, error) {
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
	// 前缀修正：Rel/Folder 相对扫描根，加前缀后相对 MEDIA_ROOT（挂载导入语义）。
	if prefix != "" {
		for i := range entries {
			entries[i].Rel = prefix + "/" + entries[i].Rel
			if entries[i].Folder != "" {
				entries[i].Folder = prefix + "/" + entries[i].Folder
			} else {
				entries[i].Folder = prefix
			}
		}
	}
	st.Total = len(entries)
	if _, err := x.db.Exec(ctx,
		`UPDATE index_jobs SET total=$1 WHERE id=$2`, st.Total, jobID); err != nil {
		return fail(err)
	}

	for i, e := range entries {
		// Job000132：正在处理的文件实时可见（管理后台「N/M · 正在处理 xxx」）
		_, _ = x.db.Exec(ctx,
			`UPDATE index_jobs SET current_file=$1 WHERE id=$2`, e.Filename, jobID)
		outcome, err := x.indexOne(ctx, ownerID, e)
		switch {
		case err != nil:
			log.Printf("索引失败 %s: %v", e.Rel, err)
			st.Failed++
		case outcome == OutcomeDuplicate:
			st.Duplicate++
		default:
			st.Inserted++
		}
		// 每个文件处理后即刷进度（0/0 假死的根源是旧版每 10 个才刷 + 遍历阶段无进度）
		if _, err := x.db.Exec(ctx, `UPDATE index_jobs SET processed=$1 WHERE id=$2`, i+1, jobID); err != nil {
			log.Printf("进度更新失败 job=%s: %v", jobID, err)
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

// IndexEntries 增量入库（P1 迁移工具 watchctl 复用）：对已收集的 entries 逐条
// 去重/提取元数据/写 media/入队缩略图；jobKind 写入 index_jobs.kind（full|incremental）。
// onResult 非 nil 时每条处理后回调（供调用方做断点续扫状态落盘）。
// 中断（ctx 取消）时返回已处理统计与 ctx.Err()，未处理条目留待下次续扫。
func (x *Indexer) IndexEntries(ctx context.Context, jobKind string, entries []FileEntry,
	onResult func(e FileEntry, outcome EntryOutcome)) (*ScanStats, error) {
	ownerID, err := EnsureSeedUser(ctx, x.db)
	if err != nil {
		return nil, err
	}

	var jobID string
	if err := x.db.QueryRow(ctx,
		`INSERT INTO index_jobs (kind, user_id, status, started_at)
		 VALUES ($1, $2, 'running', now()) RETURNING id`, jobKind, ownerID).Scan(&jobID); err != nil {
		return nil, fmt.Errorf("建 index_jobs: %w", err)
	}
	st := &ScanStats{JobID: jobID, Total: len(entries)}
	if _, err := x.db.Exec(ctx,
		`UPDATE index_jobs SET total=$1 WHERE id=$2`, st.Total, jobID); err != nil {
		return st, err
	}

	processed := 0
	for _, e := range entries {
		if ctx.Err() != nil {
			break // 中断：保留进度，交由调用方续扫
		}
		outcome, err := x.indexOne(ctx, ownerID, e)
		if err != nil {
			log.Printf("索引失败 %s: %v", e.Rel, err)
			st.Failed++
			outcome = OutcomeFailed
		} else if outcome == OutcomeDuplicate {
			st.Duplicate++
		} else {
			st.Inserted++
		}
		processed++
		if onResult != nil {
			onResult(e, outcome)
		}
		if processed%10 == 0 {
			_, _ = x.db.Exec(ctx, `UPDATE index_jobs SET processed=$1 WHERE id=$2`, processed, jobID)
		}
	}

	status := "done"
	retErr := error(nil)
	if ctx.Err() != nil {
		status = "failed" // 中断视为未完成任务
		retErr = ctx.Err()
	}
	if _, err := x.db.Exec(context.Background(),
		`UPDATE index_jobs SET status=$1, processed=$2, finished_at=now() WHERE id=$3`,
		status, processed, jobID); err != nil && retErr == nil {
		retErr = err
	}
	return st, retErr
}

// indexOne 处理单个文件：去重判定 → 元数据 → 入库 → 缩略图任务入队。
// 返回细分结果（inserted/duplicate）；error 非空即 failed。
func (x *Indexer) indexOne(ctx context.Context, ownerID string, e FileEntry) (EntryOutcome, error) {
	// Job000132 path 预检：同路径已入库（未删除）→ 直接跳过，**不读文件**。
	// 重复扫描从「重读全部文件算哈希」（GB 级视频 × N = 小时级）降为「stat 全部文件」（秒级）；
	// 内容变更的场景由调用方先删后扫。跨路径同内容仍走下方 hash 去重（正确性不变）。
	exists, err := x.pathIndexed(ctx, e.Rel)
	if err != nil {
		return OutcomeFailed, err
	}
	if exists {
		return OutcomeDuplicate, nil
	}

	hash, err := HashFile(e.Path)
	if err != nil {
		return OutcomeFailed, fmt.Errorf("哈希 %s: %w", e.Path, err)
	}
	e.Hash = hash

	dupID, err := x.findByHash(ctx, e.Hash)
	if err != nil {
		return OutcomeFailed, err
	}
	if dupID != "" {
		return OutcomeDuplicate, nil
	}

	var m *Meta
	if e.Kind == KindVideo {
		m, err = ExtractVideoMeta(ctx, e.Path)
	} else {
		m, err = ExtractPhotoMeta(e.Path)
	}
	if err != nil {
		return OutcomeFailed, fmt.Errorf("元数据: %w", err)
	}

	// 逆地理编码：有 GPS 且配置了 Geocoder 时回填 place。
	// 失败（无 Key / 配额耗尽 / 网络不通 / 境外坐标）只记日志，绝不阻塞入库。
	if m.Place == "" && m.Lat != nil && m.Lng != nil && x.geocoder != nil {
		place, gerr := x.geocoder.ReverseGeocode(ctx, *m.Lat, *m.Lng)
		if gerr != nil {
			log.Printf("逆地理编码跳过: %s (%v,%v): %v", e.Rel, *m.Lat, *m.Lng, gerr)
		} else {
			m.Place = place
		}
	}

	mediaID, err := x.insertMedia(ctx, ownerID, e, m)
	if err != nil {
		return OutcomeFailed, fmt.Errorf("写 media: %w", err)
	}

	// Job000056：群晖 @eaDir sidecar 缩略图复用（仅新插入走到这里；duplicate 已 return）。
	// 三档齐全 → 本地 ffmpeg 转 WebP 落位 + 与 worker 同一 UPDATE 落库 → 不入队；
	// 缺档/未配 THUMB_DIR/转码或落库失败 → 保守回落下方队列生成（不留半成品）。
	if td := eaThumbDir(); td != "" {
		if thumbs := FindEAThumbs(e.Path); thumbs != nil {
			if cerr := ConvertEAThumbs(ctx, td, mediaID, thumbs); cerr == nil {
				_, uerr := x.db.Exec(ctx,
					`UPDATE media SET thumbnail_sm=$1, thumbnail_md=$2, thumbnail_lg=$3, updated_at=now()
					 WHERE id=$4`,
					filepath.Base(eaThumbPath(td, mediaID, ffmpeg.ThumbSM)),
					filepath.Base(eaThumbPath(td, mediaID, ffmpeg.ThumbMD)),
					filepath.Base(eaThumbPath(td, mediaID, ffmpeg.ThumbLG)),
					mediaID)
				if uerr == nil {
					return OutcomeInserted, nil
				}
				// 落库失败：已落盘的 WebP 会被 worker 整组覆盖，无需清理；回落入队。
			}
		}
	}

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
		return OutcomeFailed, fmt.Errorf("缩略图入队: %w", err)
	}
	return OutcomeInserted, nil
}
