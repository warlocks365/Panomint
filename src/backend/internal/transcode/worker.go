package transcode

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/ffmpeg"
	"panoalbum/internal/queue"
)

// HLSWorker 转码 Worker：消费 kind=transcode 任务，ffmpeg HLS 多码率输出到 <hlsDir>/<media_id>/。
//
// ⚠️ 本文件只负责**控制端的那一半**：查库拿媒体元数据、回写任务状态与 media.hls_master、
// 以及消费 Valkey 队列。真正的 ffmpeg 管线在 hls.go 的 TranscodeHLS 里 —— 它与算力节点
// agent 共用同一份实现，避免两处 ffmpeg 参数各自漂移（见 hls.go 的文件头说明）。
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
// 解析规则本身在 ResolveMediaPath（hls.go），此处只是绑定本 Worker 的候选根。
func (w *HLSWorker) resolveInput(rel string) (string, error) {
	return ResolveMediaPath(rel, w.mediaDirs...)
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
//
// 错误语义刻意分两档（直接影响是否死信）：
//   - **确定性失败**（媒体不存在、源文件不可达）→ 包 queue.ErrStop 进死信，
//     因为退避重试一万次也不会有别的结果，只会刷满日志；
//   - **可重试失败**（转码/IO 出错）→ 原样返回，交给队列退避重试。
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

	var rel string
	var srcW, srcH int
	if err := w.db.QueryRow(ctx,
		`SELECT path, COALESCE(width, 0), COALESCE(height, 0) FROM media WHERE id = $1 AND deleted_at IS NULL`,
		mediaID).Scan(&rel, &srcW, &srcH); err != nil {
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

	masterURL, err := TranscodeHLS(ctx, HLSTranscodeRequest{
		MediaID:   mediaID,
		Input:     input,
		HLSDir:    w.hlsDir,
		Profile:   profile,
		SrcWidth:  srcW,
		SrcHeight: srcH,
	})
	if err != nil {
		// 与历史行为一致：转码失败标记 failed 但**不**进死信，留给队列退避重试。
		_ = w.setStatus(ctx, jobID, "failed", "")
		return err
	}

	// 回写：媒体 HLS 路径（URL 形态，契约 §13）+ 任务状态
	if _, err := w.db.Exec(ctx,
		`UPDATE media SET hls_master = $1, updated_at = now() WHERE id = $2`, masterURL, mediaID); err != nil {
		return err
	}
	return w.setStatus(ctx, jobID, "done", masterURL)
}

// ladderNames 档位名列表（日志用）。
func ladderNames(ladder []ffmpeg.HLSRendition) string {
	names := make([]string, 0, len(ladder))
	for _, r := range ladder {
		names = append(names, r.Name)
	}
	return strings.Join(names, ",")
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
