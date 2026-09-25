package debug

// 命令实现（设计 §5.4 白名单 8 类）。底层绑定产品既有转码控制面
// （internal/transcode/control.go，Job000121 补出的包级函数），不平行造第二套。

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"panoalbum/internal/transcode"
	"panoalbum/internal/version"
)

// jobIDRe job_id 形态校验（UUID；防 PG 22P02 原文路径，设计 §6.6 参数强校验）。
var jobIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// exec 命令分发实现。返回 (应答载荷, 审计用 job_id, 错误)。
// 错误一律 *cmdError（机器可读 code）；底层故障收敛为 INTERNAL，不回显原文。
func (s *session) exec(env envelope) (map[string]any, string, error) {
	switch env.Type {
	case msgHello:
		// 重复 hello：无害，重发 ack。
		var hp helloPayload
		_ = jsonUnmarshal(env.Data, &hp)
		s.sayHelloAt(env.Seq, hp)
		return map[string]any{"ok": true}, "", nil

	case msgPing:
		return map[string]any{"server_time": time.Now().UTC().Format(time.RFC3339)}, "", nil

	case msgSnapshot:
		return s.cmdSnapshot()

	case msgQueueStats:
		return s.cmdQueueStats()

	case msgSubscribe:
		var p struct {
			Jobs any `json:"jobs"`
		}
		if err := jsonUnmarshal(env.Data, &p); err != nil || p.Jobs == nil {
			return nil, "", errf("INVALID_PARAMS", "payload.jobs 必填（\"*\" 或任务 id 数组）")
		}
		if star, ok := p.Jobs.(string); !ok || star != "*" {
			return nil, "", errf("INVALID_PARAMS", "v1 仅支持 jobs:\"*\"")
		}
		s.mu.Lock()
		s.subJobs = true
		s.mu.Unlock()
		return map[string]any{"subscribed": "jobs"}, "", nil

	case msgUnsubscribe:
		s.mu.Lock()
		s.subJobs = false
		s.mu.Unlock()
		return map[string]any{"unsubscribed": "jobs"}, "", nil

	case msgJobPause, msgJobResume, msgJobCancel:
		return s.cmdJobControl(env)

	case msgJobLogTail:
		return s.cmdJobLogTail(env)

	default:
		return nil, "", errf("UNKNOWN_COMMAND", "未知命令 %q", env.Type)
	}
}

// sayHelloAt 指定 seq 的 hello_ack（exec 内重复 hello 用）。
func (s *session) sayHelloAt(seq int, hp helloPayload) {
	s.respondResult(seq, map[string]any{
		"server":      version.String(),
		"proto_ver":   protoVer,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	})
}

// cmdSnapshot 全量转码现状（设计 §5.4：活动任务 + 队列深度）。
// worker 最近心跳：本地 transcodectl worker 无心跳表，以队列 processing 深度代替说明。
func (s *session) cmdSnapshot() (map[string]any, string, error) {
	jobs, err := transcode.ActiveJobs(context.Background(), s.h.Pool)
	if err != nil {
		return nil, "", errf("INTERNAL", "查询活动任务失败")
	}
	depth, err := transcode.Depth(context.Background(), s.h.TransQ)
	if err != nil {
		return nil, "", errf("INTERNAL", "查询队列深度失败")
	}
	return map[string]any{
		"jobs":        jobs,
		"queue":       depth,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	}, "", nil
}

func (s *session) cmdQueueStats() (map[string]any, string, error) {
	depth, err := transcode.Depth(context.Background(), s.h.TransQ)
	if err != nil {
		return nil, "", errf("INTERNAL", "查询队列深度失败")
	}
	return map[string]any{"queue": depth}, "", nil
}

// cmdJobControl pause/resume/cancel（设计 §5.4：绑定转码控制面，每条单独审计）。
func (s *session) cmdJobControl(env envelope) (map[string]any, string, error) {
	var p struct {
		JobID string `json:"job_id"`
	}
	if err := jsonUnmarshal(env.Data, &p); err != nil || !jobIDRe.MatchString(p.JobID) {
		return nil, "", errf("INVALID_PARAMS", "payload.job_id 需为 UUID")
	}
	ctx := context.Background()
	var err error
	var status string
	switch env.Type {
	case msgJobPause:
		err = transcode.PauseJob(ctx, s.h.Pool, s.h.TransQ, p.JobID)
		status = "paused"
	case msgJobResume:
		err = transcode.ResumeJob(ctx, s.h.Pool, s.h.TransQ, p.JobID)
		status = "pending"
	case msgJobCancel:
		err = transcode.CancelJob(ctx, s.h.Pool, s.h.TransQ, p.JobID)
		status = "canceled"
	}
	if err != nil {
		switch {
		case errors.Is(err, transcode.ErrJobNotFound):
			return nil, p.JobID, errf("JOB_NOT_FOUND", "任务不存在")
		case errors.Is(err, transcode.ErrJobInvalidState):
			return nil, p.JobID, errf("INVALID_STATE", "任务当前状态不允许该操作（running 任务请在终态后处置）")
		default:
			return nil, p.JobID, errf("INTERNAL", "控制操作失败")
		}
	}
	// 订阅推送 job.state（设计 §5.4 事件流）。
	s.mu.Lock()
	sub := s.subJobs
	s.mu.Unlock()
	if sub {
		s.pushEvent(map[string]any{"job_id": p.JobID, "status": status, "ts": time.Now().UTC().Format(time.RFC3339)})
	}
	return map[string]any{"job_id": p.JobID, "status": status}, p.JobID, nil
}

// cmdJobLogTail 有界任务档案（设计 §5.4：lines ≤ 500，不提供 follow）。
// 产品无 per-job 日志文件（worker 输出在容器 stdout），本命令返回该任务的结构化档案：
// 任务行 + 该任务相关的最近审计事件（debug.cmd 等 detail.job_id 命中的行）。
func (s *session) cmdJobLogTail(env envelope) (map[string]any, string, error) {
	var p struct {
		JobID string `json:"job_id"`
		Lines int    `json:"lines"`
	}
	if err := jsonUnmarshal(env.Data, &p); err != nil || !jobIDRe.MatchString(p.JobID) {
		return nil, "", errf("INVALID_PARAMS", "payload.job_id 需为 UUID")
	}
	if p.Lines <= 0 {
		p.Lines = 100
	}
	if p.Lines > maxLogTailLines {
		p.Lines = maxLogTailLines
	}
	ctx := context.Background()

	var job map[string]any
	var status, profile, resultPath, createdAt string
	err := s.h.Pool.QueryRow(ctx,
		`SELECT status, COALESCE(profile, ''), COALESCE(result_path, ''), created_at::text
		 FROM transcode_jobs WHERE id = $1`, p.JobID).
		Scan(&status, &profile, &resultPath, &createdAt)
	if err != nil {
		return nil, p.JobID, errf("JOB_NOT_FOUND", "任务不存在")
	}
	job = map[string]any{
		"id": p.JobID, "status": status, "created_at": createdAt,
	}
	if profile != "" {
		job["profile"] = profile
	}
	if resultPath != "" {
		job["result_path"] = resultPath
	}

	rows, err := s.h.Pool.Query(ctx,
		`SELECT action, created_at::text, detail::text
		 FROM audit_log WHERE detail->>'job_id' = $1
		 ORDER BY created_at DESC LIMIT $2`, p.JobID, p.Lines)
	if err != nil {
		return nil, p.JobID, errf("INTERNAL", "查询任务审计失败")
	}
	defer rows.Close()
	tail := make([]map[string]any, 0, p.Lines)
	for rows.Next() {
		var action, at, detailText string
		if err := rows.Scan(&action, &at, &detailText); err != nil {
			return nil, p.JobID, errf("INTERNAL", "读取任务审计失败")
		}
		var detail any
		if json.Unmarshal([]byte(detailText), &detail) != nil {
			detail = detailText
		}
		tail = append(tail, map[string]any{"action": action, "at": at, "detail": detail})
	}
	return map[string]any{"job": job, "audit_tail": tail}, p.JobID, nil
}
