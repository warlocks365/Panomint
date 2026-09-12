package index

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/ffmpeg"
	"panoalbum/internal/queue"
)

// ThumbWorker 缩略图 Worker：消费 kind=thumbnail 任务，生成 SM/MD/LG 三档 WebP。
type ThumbWorker struct {
	db       *pgxpool.Pool
	q        *queue.Queue
	thumbDir string // 缩略图输出根目录
}

// NewThumbWorker 创建 Worker。
func NewThumbWorker(db *pgxpool.Pool, q *queue.Queue, thumbDir string) *ThumbWorker {
	return &ThumbWorker{db: db, q: q, thumbDir: thumbDir}
}

// thumbPath 缩略图文件路径：<thumbDir>/<media_id>_<档>.webp。
func (w *ThumbWorker) thumbPath(mediaID string, size ffmpeg.ThumbSize) string {
	return filepath.Join(w.thumbDir, fmt.Sprintf("%s_%s.webp", mediaID, size))
}

// editFilter 读取 media.edits 并生成 ffmpeg 滤镜链；无编辑时返回空串。
//
// 顺序与前端一致：**先裁剪（按未旋转方向）后旋转**（详见 ffmpeg.EditFilter）。
// 解析失败**不视为任务失败**——缩略图仍按原图生成，编辑参数异常不应阻断索引。
func (w *ThumbWorker) editFilter(ctx context.Context, mediaID string) string {
	var raw []byte
	if err := w.db.QueryRow(ctx, `SELECT edits FROM media WHERE id = $1`, mediaID).Scan(&raw); err != nil || len(raw) == 0 {
		return ""
	}
	var ed struct {
		Rotate int `json:"rotate"`
		Crop   *struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
			W float64 `json:"w"`
			H float64 `json:"h"`
		} `json:"crop"`
	}
	if err := json.Unmarshal(raw, &ed); err != nil {
		log.Printf("media.edits 解析失败 media=%s: %v（按原图生成缩略图）", mediaID, err)
		return ""
	}
	if ed.Crop != nil {
		return ffmpeg.EditFilter(ed.Rotate, true, ed.Crop.X, ed.Crop.Y, ed.Crop.W, ed.Crop.H)
	}
	return ffmpeg.EditFilter(ed.Rotate, false, 0, 0, 0, 0)
}

// Handle 处理一个缩略图任务（queue.Handler 签名）。
func (w *ThumbWorker) Handle(ctx context.Context, job queue.Job) error {
	if job.Kind != "thumbnail" {
		return nil // 非本 Worker 任务，直接 Ack（其他 Worker 负责）
	}
	mediaID := job.Payload["media_id"]
	input := job.Payload["path"]
	if mediaID == "" || input == "" {
		return fmt.Errorf("%w: 任务缺少 media_id/path", queue.ErrStop)
	}
	if _, err := os.Stat(input); err != nil {
		return fmt.Errorf("%w: 源文件不可达: %v", queue.ErrStop, err)
	}
	if err := os.MkdirAll(w.thumbDir, 0o755); err != nil {
		return err
	}

	// 视频取 1 秒处预览帧（短视频取首帧）
	var seekUs int64
	if job.Payload["kind"] == string(KindVideo) {
		if d, _ := strconv.Atoi(job.Payload["duration_sec"]); d > 2 {
			seekUs = 1_000_000
		}
	}

	// 非破坏式编辑（media.edits）：缩略图必须与查看器呈现一致，
	// 否则用户旋转/裁剪后缩略图不变，看起来像"没保存"。
	editFilter := w.editFilter(ctx, mediaID)

	for _, size := range []ffmpeg.ThumbSize{ffmpeg.ThumbSM, ffmpeg.ThumbMD, ffmpeg.ThumbLG} {
		out := w.thumbPath(mediaID, size)
		args := ffmpeg.ThumbnailArgsEdited(input, out, size, seekUs, editFilter)
		if _, err := ffmpeg.New(args).Run(ctx); err != nil {
			return fmt.Errorf("生成 %s 档缩略图: %w", size, err) // 普通错误走退避重试
		}
	}

	// 回写缩略图路径（相对 thumbDir 的文件名，前端拼接）
	_, err := w.db.Exec(ctx,
		`UPDATE media SET thumbnail_sm=$1, thumbnail_md=$2, thumbnail_lg=$3, updated_at=now()
		 WHERE id=$4`,
		filepath.Base(w.thumbPath(mediaID, ffmpeg.ThumbSM)),
		filepath.Base(w.thumbPath(mediaID, ffmpeg.ThumbMD)),
		filepath.Base(w.thumbPath(mediaID, ffmpeg.ThumbLG)),
		mediaID)
	return err
}

// Run 消费循环；once=true 时处理完一个任务（或阻塞超时）即返回，便于集成验证。
// 返回已处理任务数。
func (w *ThumbWorker) Run(ctx context.Context, once bool) (int, error) {
	processed := 0
	for {
		got, err := w.q.ConsumeOnce(ctx, 5*time.Second, func(ctx context.Context, job queue.Job) error {
			if err := w.Handle(ctx, job); err != nil {
				log.Printf("缩略图任务失败 id=%s: %v", job.ID, err)
				return err
			}
			return nil
		})
		if err != nil {
			return processed, err
		}
		if !got {
			if once {
				return processed, nil
			}
			continue // 空闲，继续阻塞拉取
		}
		processed++
		if once {
			return processed, nil
		}
	}
}
