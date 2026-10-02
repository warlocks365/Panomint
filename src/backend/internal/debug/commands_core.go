package debug

// 命令核心（Job000140 Phase 1，Agent 语义接口设计 §5）：把 WSS 白名单命令的实现抽为
// **不依赖 WS 会话**的包级函数——WSS（debugctl）与 HTTP（POST /admin/agent/cmd）两个命令面
// 共用同一实现，保证行为逐字节等价（同输入 → 同输出 → 同错误码）。
//
// 依赖显式注入（Pool/TransQ），订阅推送等会话态逻辑留在 session 层（commands.go）。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/queue"
	"panoalbum/internal/transcode"
	"panoalbum/internal/version"
)

// jobIDRe / errf / cmdError / maxLogTailLines / msg* 命令名常量 定义在 commands.go 与
// session.go（同包共享），此处不重复声明。

// cmdSnapshotCore 全量转码现状（活动任务 + 队列深度）。返回 (载荷, job_id, 错误)，
// job_id 恒空——与 WSS 会话版返回形状一致，命令面无需区分。
func cmdSnapshotCore(ctx context.Context, pool *pgxpool.Pool, transQ *queue.Queue) (map[string]any, string, error) {
	jobs, err := transcode.ActiveJobs(ctx, pool)
	if err != nil {
		return nil, "", errf("INTERNAL", "查询活动任务失败")
	}
	depth, err := transcode.Depth(ctx, transQ)
	if err != nil {
		return nil, "", errf("INTERNAL", "查询队列深度失败")
	}
	return map[string]any{
		"jobs":        jobs,
		"queue":       depth,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	}, "", nil
}

// cmdQueueStatsCore 队列深度。
func cmdQueueStatsCore(ctx context.Context, transQ *queue.Queue) (map[string]any, string, error) {
	depth, err := transcode.Depth(ctx, transQ)
	if err != nil {
		return nil, "", errf("INTERNAL", "查询队列深度失败")
	}
	return map[string]any{"queue": depth}, "", nil
}

// cmdJobControlCore job.pause / job.resume / job.cancel（绑定 transcode 控制面）。
// cmd 为命令名（msgJobPause/Resume/Cancel），data 为原始 payload。
func cmdJobControlCore(ctx context.Context, pool *pgxpool.Pool, transQ *queue.Queue, cmd string, data json.RawMessage) (map[string]any, string, error) {
	var p struct {
		JobID string `json:"job_id"`
	}
	if err := jsonUnmarshal(data, &p); err != nil || !jobIDRe.MatchString(p.JobID) {
		return nil, "", errf("INVALID_PARAMS", "payload.job_id 需为 UUID")
	}
	var err error
	var status string
	switch cmd {
	case msgJobPause:
		err = transcode.PauseJob(ctx, pool, transQ, p.JobID)
		status = "paused"
	case msgJobResume:
		err = transcode.ResumeJob(ctx, pool, transQ, p.JobID)
		status = "pending"
	case msgJobCancel:
		err = transcode.CancelJob(ctx, pool, transQ, p.JobID)
		status = "canceled"
	default:
		return nil, "", errf("UNKNOWN_COMMAND", "未知命令 %q", cmd)
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
	return map[string]any{"job_id": p.JobID, "status": status}, p.JobID, nil
}

// cmdJobLogTailCore 有界任务档案（lines ≤ 500，不提供 follow）：任务行 + 该任务相关的
// 最近审计事件（debug.cmd 等 detail.job_id 命中的行）。
func cmdJobLogTailCore(ctx context.Context, pool *pgxpool.Pool, data json.RawMessage) (map[string]any, string, error) {
	var p struct {
		JobID string `json:"job_id"`
		Lines int    `json:"lines"`
	}
	if err := jsonUnmarshal(data, &p); err != nil || !jobIDRe.MatchString(p.JobID) {
		return nil, "", errf("INVALID_PARAMS", "payload.job_id 需为 UUID")
	}
	if p.Lines <= 0 {
		p.Lines = 100
	}
	if p.Lines > maxLogTailLines {
		p.Lines = maxLogTailLines
	}

	var status, profile, resultPath, createdAt string
	err := pool.QueryRow(ctx,
		`SELECT status, COALESCE(profile, ''), COALESCE(result_path, ''), created_at::text
		 FROM transcode_jobs WHERE id = $1`, p.JobID).
		Scan(&status, &profile, &resultPath, &createdAt)
	if err != nil {
		return nil, "", errf("JOB_NOT_FOUND", "任务不存在")
	}
	job := map[string]any{
		"id": p.JobID, "status": status, "created_at": createdAt,
	}
	if profile != "" {
		job["profile"] = profile
	}
	if resultPath != "" {
		job["result_path"] = resultPath
	}

	rows, err := pool.Query(ctx,
		`SELECT action, created_at::text, detail::text
		 FROM audit_log WHERE detail->>'job_id' = $1
		 ORDER BY created_at DESC LIMIT $2`, p.JobID, p.Lines)
	if err != nil {
		return nil, "", errf("INTERNAL", "查询任务审计失败")
	}
	defer rows.Close()
	tail := make([]map[string]any, 0, p.Lines)
	for rows.Next() {
		var action, at, detailText string
		if err := rows.Scan(&action, &at, &detailText); err != nil {
			return nil, "", errf("INTERNAL", "读取任务审计失败")
		}
		var detail any
		if json.Unmarshal([]byte(detailText), &detail) != nil {
			detail = detailText
		}
		tail = append(tail, map[string]any{"action": action, "at": at, "detail": detail})
	}
	if err := rows.Err(); err != nil {
		return nil, "", errf("INTERNAL", "读取任务审计失败")
	}
	return map[string]any{"job": job, "audit_tail": tail}, p.JobID, nil
}

// cmdPingCore 存活探针（HTTP 面复用：返回服务端标识与时间）。
func cmdPingCore() (map[string]any, string, error) {
	return map[string]any{
		"server":      version.String(),
		"server_time": time.Now().UTC().Format(time.RFC3339),
	}, "", nil
}

// agentHTTPCommands HTTP 命令面白名单（Agent 语义接口 v1）。
// 与 WSS 白名单的关系：WSS 的 hello（连接层）/subscribe/unsubscribe（会话订阅态）
// 在无持久连接的 HTTP 面无意义，故不收录；其余与 WSS 同源同实现。
var agentHTTPCommands = map[string]struct{}{
	msgPing:       {},
	msgSnapshot:   {},
	msgQueueStats: {},
	msgJobPause:   {},
	msgJobResume:  {},
	msgJobCancel:  {},
	msgJobLogTail: {},
}
