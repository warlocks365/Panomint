package audit

// PostgreSQL 持久化与审计写入器。
//
// 与 DDL 的对应（实测库结构，非文档）：
//
//	audit_log(id, user_id, action, detail jsonb, ip, at)          -- 00002 建表
//	+ target_type, target_id, user_agent, 三条查询索引            -- 00020 补列
//	audit_log.user_id 外键 → ON DELETE SET NULL                   -- 00025 放宽
//	+ actor_email（写入时快照的操作者邮箱）                        -- 00026 补列
//
// ⚠️ Entry.ActorUserID 写入 user_id 列：列名是历史遗留（DDL 与文档都叫 user_id），
// 但语义是「触发者」，故 Go 侧与对外 JSON 一律叫 actor_user_id。00020 刻意不改列名。
//
// ⚠️ actor_email 是 00025 的另一半：00025 让账号可删（user_id 置 NULL），
// 00026 的 actor_email 快照保证删账号后归因仍在（详见该迁移的文件头注释）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// ErrJobNotFound 任务不存在（既不在 index_jobs 也不在 transcode_jobs）。
// handler 据此返回 404：这是正常结果，不是服务故障。
var ErrJobNotFound = errors.New("audit: 任务不存在")

// ---------------------------------------------------------------------------
// 对外模型
// ---------------------------------------------------------------------------

// Page 审计分页结果。
type Page struct {
	Items      []Entry `json:"items"`
	Total      int     `json:"total"`
	Limit      int     `json:"limit"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// Stats GET /admin/stats 响应（契约 §14 的 4 个字段）。
type Stats struct {
	// MediaTotal 媒体总数（**不含**已软删 / 回收站中的记录）。
	MediaTotal int64 `json:"media_total"`
	// Users 账户总数。
	Users int64 `json:"users"`
	// StorageUsed 原文件占用字节数（sum(media.filesize)，仅未软删）。
	//
	// 口径说明：契约只给了字段名没给单位，本项目取「媒体原文件字节数之和」，
	// 单位为 byte（整数，不四舍五入成 MB —— 前端要显示什么单位由前端决定）。
	// 不含缩略图 / HLS 派生文件：库里没有它们的体积记录。
	StorageUsed int64 `json:"storage_used"`
	// IndexStatus 索引状态。
	IndexStatus IndexInfo `json:"index_status"`
}

// IndexInfo index_status 的结构。
//
// 契约 §14 只写了字段名 `index_status`，未定义结构。本项目的口径：
// 由 index_jobs 推导 —— 库里没有独立的「索引服务状态」表，
// index_jobs 是唯一的事实来源（不要凭空造一个内存态，重启即失真）。
type IndexInfo struct {
	// State idle | running | failed | unknown。
	// 由**最近一次**索引任务的状态映射：pending/running→running、done→idle、
	// failed→failed、无任务→unknown。刻意不把"有任务在跑"单独当 running：
	// 最新任务已 done 说明队列已清空，此时更早的 running 行是脏数据。
	State string `json:"state"`
	// Running 处于 pending/running 的索引任务数（供前端展示"有积压"）。
	Running int64 `json:"running"`
	// LastJob 最近一次索引任务；从未跑过则为 null。
	LastJob *Job `json:"last_job"`
}

// Job 索引 / 转码任务的统一视图（契约 §12 GET /admin/jobs）。
// 不适用本类型的字段为 null，由 JobType 判别。
type Job struct {
	JobType    string     `json:"job_type"` // index | transcode
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Total      *int       `json:"total"`
	Processed  *int       `json:"processed"`
	Progress   *float64   `json:"progress"` // processed/total，仅 index 有；total=0 时为 null
	UserID     *string    `json:"user_id"`
	MediaID    *string    `json:"media_id"`
	NodeID     *string    `json:"node_id"`
	Profile    *string    `json:"profile"`
	ResultPath *string    `json:"result_path"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
	CurrentFile *string   `json:"current_file"` // Job000132：index 任务正在处理的文件名（仅 index 有）
}

// ---------------------------------------------------------------------------
// 接口（拆出来是为了让「审计写入失败不得影响业务」可以用假实现直接断言）
// ---------------------------------------------------------------------------

// RecorderStore 审计写入的最小依赖。
type RecorderStore interface {
	Insert(ctx context.Context, e Entry) (int64, error)
}

// QueryStore 管理端只读查询的依赖（PGStore 实现）。
type QueryStore interface {
	Query(ctx context.Context, f Filter) (*Page, error)
	Stats(ctx context.Context) (*Stats, error)
	Jobs(ctx context.Context, q JobQuery) ([]Job, error)
	GetJob(ctx context.Context, id string) (*Job, error)
}

// PGStore PostgreSQL 实现（同时满足 RecorderStore 与 QueryStore）。
type PGStore struct {
	Pool *pgxpool.Pool
}

// ---------------------------------------------------------------------------
// 写入
// ---------------------------------------------------------------------------

// Insert 写入一条审计记录并返回自增 id。
//
// actor_email 快照（00026）：在同一条 INSERT 里用子查询 `(SELECT email FROM users WHERE id = $1)`
// 顺带把操作者邮箱写进去。这里**刻意用子查询而非第二次查询**：审计是高频低延迟路径
// （每个登录/写操作一条），多一次往返不划算；且子查询让「actor 不存在时快照为 NULL」
// 与「user_id 为 NULL」天然一致。子查询复用 $1，不新增参数位。
//
// 调用方通常**不应**直接调用它：请用 Recorder.Record（尽力而为，吞掉错误）。
// 直接调用会把审计故障升级成业务故障，正是本模块要避免的。
// 入参假定已经过 Entry.Normalize；未归一化的输入仍能写入，但可能落脏数据。
func (s *PGStore) Insert(ctx context.Context, e Entry) (int64, error) {
	// detail：nil → SQL NULL；空 map → JSON `{}`（两者语义不同，见 RedactDetail）。
	// 显式 ::jsonb 转换 + []byte 载体 —— 避免 pgx 把 []byte 当 bytea 编码。
	var detail any
	if e.Detail != nil {
		b, err := json.Marshal(e.Detail)
		if err != nil {
			return 0, fmt.Errorf("audit: detail 序列化失败: %w", err)
		}
		detail = b
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}

	var id int64
	err := s.Pool.QueryRow(ctx, insertAuditSQL, insertAuditArgs(e, detail, at)...).Scan(&id)
	return id, err
}

// insertAuditSQL 审计写入语句。抽成包级常量，好让 actor_email_test.go 直接钉死
// 「占位符个数 = 实参个数」—— 两者不一致只在运行期报 `expected N arguments`，
// 编译期完全看不见（本项目吃过这个亏）。
//
// actor_email 用**子查询**在同一条语句里取操作者邮箱做快照，而不是「先查 users 再插入」：
//
//	· 审计是高频低延迟路径（每个登录/写操作一条），多一次往返不划算；
//	· 子查询让「actor 不存在时快照为 NULL」与「user_id 为 NULL」天然一致 ——
//	  $1 为 NULL / 查不到时 `WHERE id = $1` 匹配不到行，子查询直接返回 NULL，
//	  不需要在 Go 侧再写一遍「空则跳过」的分支，也就不会出现两条路径口径不一致。
//
// 子查询**复用 $1**，不新增参数位：占位符仍是 $1..$8，个数与语义和引入 actor_email 之前一致
// （$1=user_id/actor，$2=action，$3=target_type，$4=target_id，$5=detail，$6=ip，$7=user_agent，$8=at）。
const insertAuditSQL = `
	INSERT INTO audit_log (user_id, actor_email, action, target_type, target_id, detail, ip, user_agent, at)
	VALUES ($1, (SELECT email FROM users WHERE id = $1), $2, $3, $4, $5::jsonb, $6, $7, $8)
	RETURNING id`

// insertAuditArgs 按 insertAuditSQL 的占位符顺序组装实参。
// 与 SQL 常量放在一起，是为了让「顺序/个数」两件事在同一个视野里，改一处就能看见另一处。
func insertAuditArgs(e Entry, detail any, at time.Time) []any {
	return []any{
		nullIfEmpty(e.ActorUserID), e.Action, nullIfEmpty(e.TargetType), nullIfEmpty(e.TargetID),
		detail, nullIfEmpty(e.IP), nullIfEmpty(e.UserAgent), at,
	}
}

// nullIfEmpty 把空串转成 SQL NULL。
// 审计列全部可空，空串与 NULL 在过滤语义上等价，但 NULL 更诚实（"没采到"≠"采到空值"）。
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// Query 按过滤条件分页查询审计日志（按 at DESC, id DESC）。
//
// total 与列表共用同一组过滤条件，但**不含游标**：total 表达的是"满足过滤条件的
// 全集有多大"，让游标参与计数会得到随翻页递减的无意义数字。
func (s *PGStore) Query(ctx context.Context, f Filter) (*Page, error) {
	f.Limit = clampAuditLimit(f.Limit)

	countFilter := f
	countFilter.CursorAt, countFilter.CursorID = nil, nil
	whereCount, argsCount := BuildWhere(countFilter)
	var total int
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log`+whereClause(whereCount), argsCount...).Scan(&total); err != nil {
		return nil, fmt.Errorf("audit: 统计审计记录数失败: %w", err)
	}

	where, args := BuildWhere(f)
	args = append(args, f.Limit+1) // 多取一行用于判断是否还有下一页
	rows, err := s.Pool.Query(ctx, `
		SELECT id, COALESCE(user_id::text, ''), action,
		       COALESCE(target_type, ''), COALESCE(target_id, ''),
		       COALESCE(ip, ''), COALESCE(user_agent, ''),
		       detail, at
		FROM audit_log`+whereClause(where)+`
		ORDER BY at DESC, id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("audit: 查询审计日志失败: %w", err)
	}
	defer rows.Close()

	res := &Page{Items: []Entry{}, Total: total, Limit: f.Limit}
	for rows.Next() {
		var (
			it  Entry
			raw []byte
			at  time.Time
		)
		if err := rows.Scan(&it.ID, &it.ActorUserID, &it.Action, &it.TargetType, &it.TargetID,
			&it.IP, &it.UserAgent, &raw, &at); err != nil {
			return nil, fmt.Errorf("audit: 扫描审计记录失败: %w", err)
		}
		it.At = at
		if len(raw) > 0 {
			// jsonb 保证是合法 JSON；顶层是 `null` 时 Unmarshal 得到 nil map 且不报错。
			if err := json.Unmarshal(raw, &it.Detail); err != nil {
				return nil, fmt.Errorf("audit: 解析 detail 失败: %w", err)
			}
		}
		res.Items = append(res.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(res.Items) > f.Limit {
		last := res.Items[f.Limit-1]
		res.NextCursor = encodeCursor(last.At, last.ID)
		res.Items = res.Items[:f.Limit]
	}
	return res, nil
}

// Stats 系统概览（契约 §14 GET /admin/stats）。
func (s *PGStore) Stats(ctx context.Context) (*Stats, error) {
	st := &Stats{}
	// 三条计数用独立子查询而非 JOIN：media 与 users 无关联，
	// JOIN 会得到笛卡尔积级的错误计数。
	if err := s.Pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM media WHERE deleted_at IS NULL),
		       (SELECT count(*) FROM users),
		       (SELECT COALESCE(sum(filesize), 0) FROM media WHERE deleted_at IS NULL)`).
		Scan(&st.MediaTotal, &st.Users, &st.StorageUsed); err != nil {
		return nil, fmt.Errorf("audit: 统计系统概览失败: %w", err)
	}
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM index_jobs WHERE status IN ('pending', 'running')`).
		Scan(&st.IndexStatus.Running); err != nil {
		return nil, fmt.Errorf("audit: 统计索引任务失败: %w", err)
	}

	// 最近一次索引任务（Jobs 已按 created_at DESC 排序）。查不到 → last_job=null、state=unknown。
	last, err := s.Jobs(ctx, JobQuery{JobType: "index", Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(last) > 0 {
		st.IndexStatus.LastJob = &last[0]
		st.IndexStatus.State = IndexState(last[0].Status)
	} else {
		st.IndexStatus.State = "unknown"
	}
	return st, nil
}

// IndexState 由最近一次索引任务的状态映射系统索引态。导出以便单测直接断言映射表。
func IndexState(lastStatus string) string {
	switch lastStatus {
	case "pending", "running":
		return "running"
	case "done":
		return "idle"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

// Jobs 索引 / 转码任务列表（契约 §12 GET /admin/jobs）。
func (s *PGStore) Jobs(ctx context.Context, q JobQuery) ([]Job, error) {
	sql, args := BuildJobsSQL(q)
	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: 查询任务列表失败: %w", err)
	}
	defer rows.Close()

	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// GetJob 按 id 取单条任务（契约 §12 GET /admin/jobs/:id）。
//
// 任务不在任何一张表里时返回 ErrJobNotFound —— handler 据此返回 404，
// 而不是 500（"查不到"是正常结果，不是服务故障）。
func (s *PGStore) GetJob(ctx context.Context, id string) (*Job, error) {
	row := s.Pool.QueryRow(ctx, JobByIDSQL(), id)
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// scanJob 扫描一行任务（列表与单条查询**共用**，避免两处列顺序漂移）。
func scanJob(row pgx.Row) (*Job, error) {
	var j Job
	if err := row.Scan(&j.JobType, &j.ID, &j.Kind, &j.Status, &j.UserID,
		&j.MediaID, &j.NodeID, &j.Profile, &j.ResultPath,
		&j.Total, &j.Processed, &j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.CurrentFile); err != nil {
		return nil, fmt.Errorf("audit: 扫描任务行失败: %w", err)
	}
	j.Progress = jobProgress(j.Total, j.Processed)
	return &j, nil
}

// jobProgress 计算进度（纯函数）。total 缺失或为 0 时返回 nil（不返回 0，
// 因为 0 会被读成"还没开始"，与"未知"是两回事）。
func jobProgress(total, processed *int) *float64 {
	if total == nil || processed == nil || *total <= 0 {
		return nil
	}
	p := float64(*processed) / float64(*total)
	return &p
}

// ---------------------------------------------------------------------------
// 审计写入器（尽力而为）
// ---------------------------------------------------------------------------

// auditTimeout 单条审计写入的等待上界。
//
// 有上界是刻意的：审计可以慢、可以丢，但不允许把业务请求拖死。
// 2s 远大于本机 PG 插入的耗时（单行 INSERT），只用来兜住"库不可达"这类异常。
const auditTimeout = 2 * time.Second

// Recorder 审计写入器。
//
// # 为什么是「显式调用」而不是「路径白名单中间件」
//
// 结论：本模块只提供显式调用能力（外加 FromGin 辅助函数），不提供中间件。
// 理由（按重要性排序）：
//
//  1. **中间件只能记录"尝试"，记录不了"事实"**。审计要回答的是
//     "谁把哪个对象改成了什么"，而 target_id/detail/结果只有 handler 内部知道。
//     中间件拿到的只有 method/path/status —— 无法区分「删除成功」与「403 被拒」，
//     两者都会被记成一条 share.delete。**记录错误的审计比不记录更危险**：
//     它会让人在追责时得出相反结论，而审计的全部价值就是可信。
//
//  2. **中间件无法区分成功与失败路径**。白名单能解决"记哪些路径"，
//     解决不了"这次请求到底改没改数据"（handler 在 return 前可能回滚、
//     可能因为归属校验失败而空转）。
//
//  3. **审计的用法是低频、按需、语义明确**的，不是每请求一次。
//     敏感操作在整套 API 里只有十余处（见 audit.go 的动作登记表），
//     显式调用点多但每个都精确；中间件要为这十余处引入一套匹配规则 +
//     参数提取逻辑，复杂度高于收益。
//
//  4. **范围约束**：本次改动不允许修改 internal/media、internal/shares 等既有
//     handler，故先交付能力 + 接入清单（见交付报告），不强行用中间件"糊"上覆盖。
//
// 若后续确实需要覆盖无法改动的既有 handler，可另加一个**白名单式**中间件
// （方法 + 路径模板 + 是否从路径参数提取 target），但必须同时接受上述第 1、2 条
// 缺陷，且不能与显式调用并存于同一路由（会重复记录）。
type Recorder struct {
	store RecorderStore
	log   *zap.Logger
}

// New 生产装配（pool → PGStore）。
func New(pool *pgxpool.Pool, logger *zap.Logger) *Recorder {
	return NewWithStore(&PGStore{Pool: pool}, logger)
}

// NewWithStore 注入自定义 store。测试用它传入「永远报错」或「会 panic」的假实现。
func NewWithStore(s RecorderStore, logger *zap.Logger) *Recorder {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Recorder{store: s, log: logger}
}

// Record 尽力而为地写一条审计记录。
//
// **硬保证**（由 recorder_test.go 断言，任何改动都必须让这两条测试继续通过）：
//
//   - 永不返回错误（没有返回值，也就无从外溢）；
//   - 永不 panic，包括 store 自身 panic 的情况；
//   - store 报错 / panics 时只打一条 warn 日志。
//
// 因此**任何业务路径都可以无脑调用它**：审计故障不会变成业务故障。
func (r *Recorder) Record(ctx context.Context, e Entry) {
	if r == nil || r.store == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			// 审计写入方的 panic 一律吞掉 —— 这是"不得成为故障点"的最后一道闸门。
			r.warn("审计写入 panic 已吞掉（审计不影响业务）",
				zap.Any("panic", rec), zap.String("action", e.Action))
		}
	}()
	if err := r.Log(ctx, e); err != nil {
		r.warn("审计写入失败（已忽略，不影响业务）",
			zap.Error(err), zap.String("action", e.Action))
	}
}

// Log 写入一条审计记录并把错误返回给调用方。
//
// 仅供**测试与诊断**使用：业务代码请用 Record。直接调用 Log 并处理其错误，
// 等于把审计可用性纳入业务成功条件 —— 正是本模块明确要避免的设计。
func (r *Recorder) Log(ctx context.Context, e Entry) error {
	if r == nil || r.store == nil {
		return errors.New("audit: 未装配 store")
	}
	ne, err := e.Normalize()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 与请求生命周期解耦：客户端提前断开不应让审计静默丢失；
	// 但也不能因此无限等待，故叠加 2s 上界。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditTimeout)
	defer cancel()

	_, err = r.store.Insert(ctx, ne)
	return err
}

func (r *Recorder) warn(msg string, fields ...zap.Field) {
	if r == nil || r.log == nil {
		return
	}
	r.log.Warn(msg, fields...)
}
