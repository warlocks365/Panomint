package transcode

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/queue"
)

// missingHLSQuery 查询「缺 HLS 且当前没有未完成转码任务」的视频（幂等：pending/running 的不重复排）。
const missingHLSQuery = `
SELECT m.id::text, COALESCE(m.width, 0), COALESCE(m.height, 0), m.path
FROM media m
WHERE m.deleted_at IS NULL
  AND m.type = 'video'
  AND COALESCE(m.hls_master, '') = ''
  AND NOT EXISTS (
      SELECT 1 FROM transcode_jobs j
      WHERE j.media_id = m.id AND j.status IN ('pending', 'running')
  )
ORDER BY m.imported_at, m.id`

// EnqueueResult 批量入队结果统计。
type EnqueueResult struct {
	Total    int // 待处理媒体数（缺 HLS 且无未完成任务）
	Enqueued int // 实际入队成功数
	Failed   int // 入队失败数
}

// EnqueueMissingHLS 把缺失 HLS 的视频批量排入转码队列。
// profileOverride 非空时强制使用该档位（1080p|2k|4k），否则按源分辨率用 ProfileForSource 自动选择。
// dryRun 为 true 时只打印清单，不写库不入队（q 可为 nil）。
func EnqueueMissingHLS(ctx context.Context, db *pgxpool.Pool, q *queue.Queue, dryRun bool, profileOverride string) (EnqueueResult, error) {
	if profileOverride != "" && !validProfile(profileOverride) {
		return EnqueueResult{}, fmt.Errorf("档位 %q 非法，需为 1080p|2k|4k", profileOverride)
	}
	if !dryRun && q == nil {
		return EnqueueResult{}, fmt.Errorf("非 dry-run 模式需要队列实例")
	}

	rows, err := db.Query(ctx, missingHLSQuery)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("查询缺失 HLS 的视频失败: %w", err)
	}
	defer rows.Close()

	var res EnqueueResult
	for rows.Next() {
		var id, path string
		var srcW, srcH int
		if err := rows.Scan(&id, &srcW, &srcH, &path); err != nil {
			return res, fmt.Errorf("读取媒体行失败: %w", err)
		}
		res.Total++

		profile := profileOverride
		if profile == "" {
			profile = ProfileForSource(srcW, srcH)
		}
		if dryRun {
			log.Printf("[dry-run] 待入队 media=%s profile=%s src=%dx%d path=%s", id, profile, srcW, srcH, path)
			continue
		}
		if _, err := enqueueOne(ctx, db, q, id, profile); err != nil {
			log.Printf("入队失败 media=%s: %v", id, err)
			res.Failed++
			continue
		}
		log.Printf("已入队 media=%s profile=%s src=%dx%d", id, profile, srcW, srcH)
		res.Enqueued++
	}
	if err := rows.Err(); err != nil {
		return res, fmt.Errorf("遍历媒体行失败: %w", err)
	}
	return res, nil
}

// enqueueOne 写一条 transcode_jobs 并入队，返回任务 id。
//
// **写行+入队、失败回滚删行**是「创建转码任务」的唯一语义（P1-02）：
// API（Handler.CreateJob）与 CLI（EnqueueMissingHLS）两路径共用本函数。
// 入队失败时回滚该行——否则留下的 status='pending' 僵尸行会让 missingHLSQuery
// 的 NOT EXISTS 把该媒体**永久**排除在自动补排之外，且 JobStatus 永远返回 pending。
func enqueueOne(ctx context.Context, db *pgxpool.Pool, q *queue.Queue, mediaID, profile string) (string, error) {
	var jobID string
	if err := db.QueryRow(ctx,
		`INSERT INTO transcode_jobs (media_id, kind, profile, status)
		 VALUES ($1, 'hls', $2, 'pending') RETURNING id`, mediaID, profile).Scan(&jobID); err != nil {
		return "", fmt.Errorf("写转码任务失败: %w", err)
	}
	if _, err := q.Enqueue(ctx, queue.Job{Kind: "transcode", Payload: map[string]string{
		"job_id":   jobID,
		"media_id": mediaID,
		"profile":  profile,
	}}); err != nil {
		_, _ = db.Exec(ctx, `DELETE FROM transcode_jobs WHERE id = $1`, jobID)
		return "", fmt.Errorf("入队失败: %w", err)
	}
	return jobID, nil
}
