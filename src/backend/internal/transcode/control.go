package transcode

// 转码控制面包级函数（Job000121 调试通道绑定，设计 §1.1）：
// pause/resume/cancel 此前仅"队列语义+任务状态机"隐含存在，本文件把它们补成
// 显式包级函数，供调试通道（internal/debug）复用——**不平行造第二套控制面**。
//
// 状态机扩展（transcode_jobs.status 新增 'paused' / 'canceled'，VARCHAR(16) 容得下；
// 读侧 admin/jobs 查询本就不写死枚举，见 internal/audit/handlers.go 的说明）：
//
//	pending --(worker 领取)--> running --(完成)--> done
//	   |  ^                       |
//	   |  +------ resume ---------+
//	   v                          v
//	paused --(cancel)--> canceled
//	pending --(cancel)--> canceled
//
// v1 边界（刻意收窄，fail-closed）：running 一律不接受 pause/resume/cancel——
// ffmpeg 在 worker 进程内，跨进程强杀不在本期范围；调试通道可先 pause 队列消费
// 或等任务终态后再处置。running 任务返回 ErrJobInvalidState。

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/queue"
)

// 控制面哨兵（调试命令层按 errors.Is 映射 error code）。
var (
	ErrJobNotFound      = errors.New("transcode: 任务不存在")
	ErrJobInvalidState  = errors.New("transcode: 任务当前状态不允许该操作")
)

// jobRow 控制面读视图。
type jobRow struct {
	ID       string
	MediaID  string
	Profile  string
	Status   string
}

func loadJobRow(ctx context.Context, db *pgxpool.Pool, jobID string) (jobRow, error) {
	var r jobRow
	err := db.QueryRow(ctx,
		`SELECT id::text, media_id::text, COALESCE(profile, ''), status FROM transcode_jobs WHERE id = $1`,
		jobID).Scan(&r.ID, &r.MediaID, &r.Profile, &r.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrJobNotFound
	}
	if err != nil {
		return r, err
	}
	return r, nil
}

// PauseJob 暂停待执行任务：置 paused（条件 UPDATE 原子占坑）+ 移出 waiting。
// 已被 worker 领走（LREM 未命中）→ 回滚状态并返回 ErrJobInvalidState（fail-closed）。
// 次序刻意"状态先行"：DB 状态迁移是原子闸门，把所有并发竞态收敛到 LREM 一步。
func PauseJob(ctx context.Context, db *pgxpool.Pool, q *queue.Queue, jobID string) error {
	row, err := loadJobRow(ctx, db, jobID)
	if err != nil {
		return err
	}
	if row.Status != "pending" {
		return ErrJobInvalidState
	}
	tag, err := db.Exec(ctx,
		`UPDATE transcode_jobs SET status = 'paused' WHERE id = $1 AND status = 'pending'`, jobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrJobInvalidState
	}
	removed, err := q.RemoveFromWaiting(ctx, "job_id", jobID)
	if err != nil {
		_, _ = db.Exec(ctx, `UPDATE transcode_jobs SET status = 'pending' WHERE id = $1 AND status = 'paused'`, jobID)
		return err
	}
	if !removed {
		// 领取竞态：worker 恰在 UPDATE 前 pop 走。回滚占坑，交由正常流程跑完。
		_, _ = db.Exec(ctx, `UPDATE transcode_jobs SET status = 'pending' WHERE id = $1 AND status = 'paused'`, jobID)
		return ErrJobInvalidState
	}
	return nil
}

// ResumeJob 恢复已暂停任务：置 pending + 重新入队（载荷按行重建）。
// 入队失败回滚状态为 paused——不留"pending 但未入队"的悬挂行。
func ResumeJob(ctx context.Context, db *pgxpool.Pool, q *queue.Queue, jobID string) error {
	row, err := loadJobRow(ctx, db, jobID)
	if err != nil {
		return err
	}
	if row.Status != "paused" {
		return ErrJobInvalidState
	}
	tag, err := db.Exec(ctx,
		`UPDATE transcode_jobs SET status = 'pending' WHERE id = $1 AND status = 'paused'`, jobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrJobInvalidState
	}
	if _, err := q.Enqueue(ctx, queue.Job{Kind: "transcode", Payload: map[string]string{
		"job_id": jobID, "media_id": row.MediaID, "profile": row.Profile,
	}}); err != nil {
		_, _ = db.Exec(ctx,
			`UPDATE transcode_jobs SET status = 'paused' WHERE id = $1 AND status = 'pending'`, jobID)
		return err
	}
	return nil
}

// CancelJob 取消待执行/已暂停任务：置 canceled（条件 UPDATE 原子占坑）+ 移出 waiting（若原 pending）。
// LREM 未命中（领取竞态）→ 回滚状态并返回 ErrJobInvalidState。
func CancelJob(ctx context.Context, db *pgxpool.Pool, q *queue.Queue, jobID string) error {
	row, err := loadJobRow(ctx, db, jobID)
	if err != nil {
		return err
	}
	if row.Status != "pending" && row.Status != "paused" {
		return ErrJobInvalidState
	}
	tag, err := db.Exec(ctx,
		`UPDATE transcode_jobs SET status = 'canceled' WHERE id = $1 AND status IN ('pending','paused')`, jobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrJobInvalidState
	}
	if row.Status == "pending" {
		removed, err := q.RemoveFromWaiting(ctx, "job_id", jobID)
		if err != nil {
			_, _ = db.Exec(ctx, `UPDATE transcode_jobs SET status = 'pending' WHERE id = $1 AND status = 'canceled'`, jobID)
			return err
		}
		if !removed {
			_, _ = db.Exec(ctx, `UPDATE transcode_jobs SET status = 'pending' WHERE id = $1 AND status = 'canceled'`, jobID)
			return ErrJobInvalidState
		}
	}
	return nil
}

// ControlJobRow 调试通道可见的任务行（snapshot/推送用）。
type ControlJobRow struct {
	ID         string  `json:"id"`
	MediaID    string  `json:"media_id"`
	Status     string  `json:"status"`
	Profile    string  `json:"profile,omitempty"`
	ResultPath *string `json:"result_path,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// QueueDepth 队列深度四元组（queue.Len 的 JSON 化视图）。
type QueueDepth struct {
	Waiting    int64 `json:"waiting"`
	Processing int64 `json:"processing"`
	Delayed    int64 `json:"delayed"`
	Failed     int64 `json:"failed"`
}

// ActiveJobs 活动任务清单（pending/running/paused，最近 50 条，新→旧）。
func ActiveJobs(ctx context.Context, db *pgxpool.Pool) ([]ControlJobRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, media_id::text, status, COALESCE(profile, ''), result_path, created_at::text
		 FROM transcode_jobs
		 WHERE status IN ('pending', 'running', 'paused')
		 ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ControlJobRow, 0, 16)
	for rows.Next() {
		var r ControlJobRow
		if err := rows.Scan(&r.ID, &r.MediaID, &r.Status, &r.Profile, &r.ResultPath, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Depth 队列深度（Valkey 侧；不可用返回 error，调用方如实上报）。
func Depth(ctx context.Context, q *queue.Queue) (QueueDepth, error) {
	w, p, d, f, err := q.Len(ctx)
	return QueueDepth{Waiting: w, Processing: p, Delayed: d, Failed: f}, err
}
