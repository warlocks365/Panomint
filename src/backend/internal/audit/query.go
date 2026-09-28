package audit

// 查询条件与 SQL 拼装。
//
// 本文件里的函数**全部是纯函数**（除游标编解码外无副作用、不碰 DB），
// 目的是把「占位符与 args 下标错位」这类错误钉死在单元测试里。
// 本项目已多次踩过该类错误（拼接时用 len(args) 与 $n 各算一次，改一处漏一处），
// 因此 BuildWhere / BuildJobsSQL 都遵守同一条不变量：
//
//	**第 k 个条件使用的占位符一定是 $k，且 args[k-1] 一定是它的值。**
//
// 实现上统一走 `add` 闭包：先 append 到 args，再用 len(args) 当占位符编号 ——
// 编号与下标由同一处产生，不存在两处计算漂移的可能。

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"panoalbum/internal/cursor"
)

// Filter 审计查询过滤条件。零值 = 不过滤。
type Filter struct {
	// Action 动作精确匹配（应已 NormalizeAction）。
	Action string
	// ActorUserID 触发者 UUID 精确匹配。
	ActorUserID string
	// TargetType / TargetID 目标精确匹配。
	TargetType string
	TargetID   string
	// From / To 时间范围，左闭右开 [From, To)。
	From *time.Time
	To   *time.Time

	// CursorAt / CursorID 已解码的游标。由 HTTP 层经 decodeCursor 填充，
	// 不暴露给调用方直接拼装 —— 让「游标是编码过的字符串」这层抽象留在边界上。
	CursorAt *time.Time
	CursorID *int64

	// Limit 单页条数；<=0 或超上限时由调用方（PGStore.Query）夹紧。
	Limit int
}

// BuildWhere 把 Filter 拼成 **不含 WHERE 关键字** 的条件体与参数列表。
//
// 返回：
//   - conds 形如 `action = $1 AND at >= $2`；无条件时为空串 "";
//   - args 与占位符严格同序；无条件时为 nil。
//
// 游标条件走复合行比较 `(at, id) < ($n, $m)`：audit_log 的排序键是
// (at DESC, id DESC)，at 可能重复（同一秒多条），只按 at 分页会漏行或重复，
// 必须带上唯一的 id 才能得到稳定窗口。
func BuildWhere(f Filter) (string, []any) {
	var conds []string
	var args []any

	add := func(format string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(format, len(args)))
	}

	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.ActorUserID != "" {
		add("user_id = $%d", f.ActorUserID)
	}
	if f.TargetType != "" {
		add("target_type = $%d", f.TargetType)
	}
	if f.TargetID != "" {
		add("target_id = $%d", f.TargetID)
	}
	if f.From != nil {
		add("at >= $%d", *f.From)
	}
	if f.To != nil {
		add("at < $%d", *f.To)
	}
	if f.CursorAt != nil && f.CursorID != nil {
		args = append(args, *f.CursorAt, *f.CursorID)
		conds = append(conds, fmt.Sprintf("(at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}

	if len(conds) == 0 {
		return "", nil
	}
	return strings.Join(conds, " AND "), args
}

// whereClause 把 BuildWhere 的条件体补上 WHERE 关键字。
func whereClause(cond string) string {
	if cond == "" {
		return ""
	}
	return " WHERE " + cond
}

// clampAuditLimit 夹紧分页条数：<=0 回落默认值，超上限夹到上限。
//
// 超上限刻意**夹到上限**而不是回落默认值：客户端明确要 500 条时给它 200 条
// （=「你要的太多，最多给这么多」）比静默只给 50 条更符合预期，
// 后者会让调用方以为"审计只有 50 条"。
func clampAuditLimit(n int) int {
	if n <= 0 {
		return defaultAuditLimit
	}
	if n > maxAuditLimit {
		return maxAuditLimit
	}
	return n
}

// clampJobsLimit 夹紧任务列表条数，语义同 clampAuditLimit。
func clampJobsLimit(n int) int {
	if n <= 0 {
		return defaultJobsLimit
	}
	if n > maxJobsLimit {
		return maxJobsLimit
	}
	return n
}

// ---------------------------------------------------------------------------
// 游标：编解码见 internal/cursor（唯一真源，与 media/search 共用同一线格式）。
//
// 这里刻意保留 int64 签名做**薄适配**：审计表的 id 是 bigint，而真源用 string id
// （因为它要与 UUID 型的 media/search 共用）。适配只做十进制转换，不改变任何字节 ——
// 十进制字符串就是原实现写进游标的形态。
// ---------------------------------------------------------------------------

func encodeCursor(at time.Time, id int64) string {
	return cursor.Encode(at, strconv.FormatInt(id, 10))
}

func decodeCursor(s string) (time.Time, int64, error) {
	t, rawID, err := cursor.Decode(s)
	if err != nil {
		return time.Time{}, 0, err
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return time.Time{}, 0, err
	}
	return t, id, nil
}

// ---------------------------------------------------------------------------
// 任务列表 SQL（契约 §12 GET /admin/jobs）
// ---------------------------------------------------------------------------

// JobQuery `GET /admin/jobs` 的查询条件。
type JobQuery struct {
	// JobType "" / "all" = 不限制；"index" / "transcode" = 只看一类。
	// 由 HTTP 层 parseJobQuery 校验，BuildJobsSQL 只处理这三种取值。
	JobType string
	// Status 任务状态精确匹配（各表状态取值不同，故意不做统一枚举：
	// 契约未定义状态机，强行归一化会把"库里到底是什么"这件事掩盖掉）。
	Status string
	// Limit 条数；<=0 或超上限时夹紧。
	Limit int
}

// 两个 UNION 分支的列形状 —— **单一事实来源**。
//
// 列表查询（BuildJobsSQL）与单条查询（JobByIDSQL）共用这两个常量：
// 14 列的 UNION 写错一列或错一个类型，只会在**运行时**报错（不是编译期），
// 因此刻意只维护一份，避免两处漂移。
const (
	indexJobBranch = `SELECT 'index'::text AS job_type, id::text AS id, kind, status, user_id::text AS user_id,
       NULL::text AS media_id, NULL::text AS node_id, NULL::text AS profile, NULL::text AS result_path,
       total, processed, started_at, finished_at, created_at, current_file
FROM index_jobs`

	transcodeJobBranch = `SELECT 'transcode'::text AS job_type, id::text AS id, kind, status, NULL::text AS user_id,
       media_id::text AS media_id, node_id::text AS node_id, profile, result_path,
       NULL::int AS total, NULL::int AS processed, NULL::timestamptz AS started_at, NULL::timestamptz AS finished_at, created_at,
       NULL::text AS current_file
FROM transcode_jobs`
)

// JobByIDSQL 按 id 取**单条**任务（契约 §12 的 GET /admin/jobs/:id）。
//
// 两张表的 id 各自独立生成（uuid_generate_v4），理论上可能撞号，故 UNION ALL 后 LIMIT 1；
// 撞号概率可忽略，且真撞上也只是取到其中一条而非报错。
func JobByIDSQL() string {
	return "SELECT * FROM (" + indexJobBranch + " WHERE id = $1::uuid" +
		" UNION ALL " + transcodeJobBranch + " WHERE id = $1::uuid" + ") j LIMIT 1"
}

// BuildJobsSQL 生成 index_jobs / transcode_jobs 的 UNION ALL 视图查询。
//
// 两张表的列集不同（index 有 total/processed 与触发者 user_id；transcode 有
// media_id/node_id/profile/result_path），故用 NULL 补齐成同一形状，
// 由 job_type 做判别列。刻意**不在 SQL 里算 progress**（留给 Go 侧算，
// 便于纯逻辑单测）。
//
// 占位符约定：两个 UNION 分支**共用**同一个 status 占位符（同一个参数值只传一次），
// LIMIT 的占位符追加在最后。所有 uuid 列显式 ::text 转换 —— 上层一律按字符串处理，
// 避免 uuid / text 的驱动层扫描歧义。
func BuildJobsSQL(q JobQuery) (string, []any) {
	var args []any
	statusCond := ""
	if q.Status != "" {
		args = append(args, q.Status)
		statusCond = fmt.Sprintf(" WHERE status = $%d", len(args))
	}

	var branches []string
	switch q.JobType {
	case "index":
		branches = []string{indexJobBranch + statusCond}
	case "transcode":
		branches = []string{transcodeJobBranch + statusCond}
	default: // "" / "all"
		branches = []string{indexJobBranch + statusCond, transcodeJobBranch + statusCond}
	}

	args = append(args, clampJobsLimit(q.Limit))
	return "SELECT * FROM (" + strings.Join(branches, " UNION ALL ") + ") j" +
		" ORDER BY created_at DESC, id DESC LIMIT $" + strconv.Itoa(len(args)), args
}
