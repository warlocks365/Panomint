package debug

// 命令实现（设计 §5.4 白名单 8 类）。Job000140 Phase 1 起实现体抽入 commands_core.go
// （包级函数、依赖显式注入），WSS 与 HTTP（POST /admin/agent/cmd）两个命令面共用；
// 本文件保留会话层包装：订阅推送（job.state）等 WS 会话态逻辑。

import (
	"context"
	"regexp"
	"time"

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
		return cmdPingCore()

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
	return cmdSnapshotCore(context.Background(), s.h.Pool, s.h.TransQ)
}

func (s *session) cmdQueueStats() (map[string]any, string, error) {
	return cmdQueueStatsCore(context.Background(), s.h.TransQ)
}

// cmdJobControl pause/resume/cancel（设计 §5.4：绑定转码控制面，每条单独审计）。
// 实现在 cmdJobControlCore（与 HTTP 命令面共用）；本层只做订阅事件推送。
func (s *session) cmdJobControl(env envelope) (map[string]any, string, error) {
	payload, jobID, err := cmdJobControlCore(context.Background(), s.h.Pool, s.h.TransQ, env.Type, env.Data)
	if err != nil {
		return payload, jobID, err
	}
	// 订阅推送 job.state（设计 §5.4 事件流）。
	s.mu.Lock()
	sub := s.subJobs
	s.mu.Unlock()
	if sub {
		s.pushEvent(map[string]any{"job_id": jobID, "status": payload["status"], "ts": time.Now().UTC().Format(time.RFC3339)})
	}
	return payload, jobID, nil
}

// cmdJobLogTail 有界任务档案（设计 §5.4：lines ≤ 500，不提供 follow）。
// 实现在 cmdJobLogTailCore（与 HTTP 命令面共用）：任务行 + 该任务相关的最近审计事件。
func (s *session) cmdJobLogTail(env envelope) (map[string]any, string, error) {
	return cmdJobLogTailCore(context.Background(), s.h.Pool, env.Data)
}
