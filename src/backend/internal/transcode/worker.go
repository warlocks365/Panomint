package transcode

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/ffmpeg"
	"panoalbum/internal/queue"
)

// HLSWorker 转码 Worker：消费 kind=transcode 任务，ffmpeg HLS 多码率输出到 <hlsDir>/<media_id>/。
type HLSWorker struct {
	db        *pgxpool.Pool
	q         *queue.Queue
	hlsDir    string   // HLS 输出根目录
	mediaDirs []string // 媒体根目录候选（上传目录、索引根目录）
}

// NewHLSWorker 创建 Worker；mediaDirs 为 media.path 相对路径的解析根（按序探测）。
func NewHLSWorker(db *pgxpool.Pool, q *queue.Queue, hlsDir string, mediaDirs ...string) *HLSWorker {
	return &HLSWorker{db: db, q: q, hlsDir: hlsDir, mediaDirs: mediaDirs}
}

// resolveInput 按候选根目录解析 media.path 到磁盘文件。
func (w *HLSWorker) resolveInput(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		if _, err := os.Stat(rel); err == nil {
			return rel, nil
		}
		return "", fmt.Errorf("源文件不可达: %s", rel)
	}
	for _, root := range w.mediaDirs {
		if root == "" {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("源文件不可达: %s", rel)
}

// hasAudioStream 用 ffprobe 探测是否含音轨（决定 HLSArgs 的 withAudio）。
func hasAudioStream(ctx context.Context, input string) bool {
	bin, err := ffmpeg.LookProbePath()
	if err != nil {
		return false
	}
	out, err := exec.CommandContext(ctx, bin, "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=index", "-of", "csv=p=0", input).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// Handle 处理一个转码任务（queue.Handler 签名）。
func (w *HLSWorker) Handle(ctx context.Context, job queue.Job) error {
	if job.Kind != "transcode" {
		return nil // 非本 Worker 任务
	}
	jobID := job.Payload["job_id"]
	mediaID := job.Payload["media_id"]
	profile := job.Payload["profile"]
	if jobID == "" || mediaID == "" {
		return fmt.Errorf("%w: 任务缺少 job_id/media_id", queue.ErrStop)
	}
	if profile == "" {
		profile = "1080p"
	}

	var rel string
	if err := w.db.QueryRow(ctx,
		`SELECT path FROM media WHERE id = $1 AND deleted_at IS NULL`, mediaID).Scan(&rel); err != nil {
		// 媒体不存在：重试无意义，死信
		_ = w.setStatus(ctx, jobID, "failed", "")
		return fmt.Errorf("%w: 媒体查询失败: %v", queue.ErrStop, err)
	}
	input, err := w.resolveInput(rel)
	if err != nil {
		_ = w.setStatus(ctx, jobID, "failed", "")
		return fmt.Errorf("%w: %v", queue.ErrStop, err)
	}

	if err := w.setStatus(ctx, jobID, "running", ""); err != nil {
		return err
	}

	ladder := LadderForProfile(profile)
	outDir := filepath.Join(w.hlsDir, mediaID)
	// 重试场景：清掉半成品目录（幂等重转）
	_ = os.RemoveAll(outDir)
	for _, d := range append([]string{outDir}, ffmpeg.HLSDirs(outDir, ladder)...) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			_ = w.setStatus(ctx, jobID, "failed", "")
			return err
		}
	}

	durUs, _ := ffmpeg.ProbeDurationUs(ctx, input)
	args := append([]string{"-y"}, ffmpeg.HLSArgs(input, outDir, ladder, 4, hasAudioStream(ctx, input))...)
	task := ffmpeg.New(args, ffmpeg.WithExpectedDurationUs(durUs))
	if _, err := task.Run(ctx); err != nil {
		_ = w.setStatus(ctx, jobID, "failed", "")
		return fmt.Errorf("HLS 转码: %w", err) // 普通错误走退避重试
	}
	if _, err := os.Stat(filepath.Join(outDir, "master.m3u8")); err != nil {
		_ = w.setStatus(ctx, jobID, "failed", "")
		return fmt.Errorf("转码完成但 master.m3u8 缺失: %w", err)
	}

	// 回写：媒体 HLS 路径（URL 形态，契约 §13）+ 任务状态
	masterURL := "/transcode/hls/" + mediaID + "/master.m3u8"
	if _, err := w.db.Exec(ctx,
		`UPDATE media SET hls_master = $1, updated_at = now() WHERE id = $2`, masterURL, mediaID); err != nil {
		return err
	}
	return w.setStatus(ctx, jobID, "done", masterURL)
}

func (w *HLSWorker) setStatus(ctx context.Context, jobID, status, resultPath string) error {
	var rp any
	if resultPath != "" {
		rp = resultPath
	}
	_, err := w.db.Exec(ctx,
		`UPDATE transcode_jobs SET status = $1, result_path = COALESCE($2, result_path) WHERE id = $3`,
		status, rp, jobID)
	return err
}

// Run 消费循环；once=true 时处理一个任务（或阻塞超时）即返回，便于集成验证。
func (w *HLSWorker) Run(ctx context.Context, once bool) (int, error) {
	processed := 0
	for {
		got, err := w.q.ConsumeOnce(ctx, 5*time.Second, func(ctx context.Context, job queue.Job) error {
			if err := w.Handle(ctx, job); err != nil {
				log.Printf("转码任务失败 id=%s: %v", job.ID, err)
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
			continue
		}
		processed++
		if once {
			return processed, nil
		}
	}
}
