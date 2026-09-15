package audit

// SQL 拼装的纯函数单测。
//
// 本文件存在的唯一理由：把「占位符与 args 下标错位」钉死在测试里。
// 该类错误在本项目已多次出现（拼接时 $n 与 len(args) 各算一次，改一处漏一处），
// 且**运行时不一定报错** —— 参数类型恰好兼容时 SQL 会静默返回错结果，
// 比直接报错危险得多。故所有断言都围绕一条不变量：
//
//	SQL 里出现的占位符集合必须恰好是 {$1, $2, ..., $len(args)}，一个不多一个不少。
//
// 「一个不少」抓的是条件被拼上但参数忘 append（或反之）；
// 「一个不多」抓的是最大编号超过 args 长度 —— 即经典的错位。

import (
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var placeholderRe = regexp.MustCompile(`\$(\d+)`)

// assertPlaceholdersAligned 断言 SQL 的占位符集合恰好是 1..wantArgs。
//
// 允许同一个编号**重复出现**（BuildJobsSQL 的 UNION 两分支刻意共用 status 占位符），
// 但不允许出现空洞（$2 在其中而 $1 不存在）或越界（$3 存在而只有 2 个参数）。
func assertPlaceholdersAligned(t *testing.T, sql string, wantArgs int) {
	t.Helper()
	seen := map[int]bool{}
	maxN := 0
	for _, m := range placeholderRe.FindAllStringSubmatch(sql, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("SQL 含非法占位符 %q：\n%s", m[1], sql)
		}
		seen[n] = true
		if n > maxN {
			maxN = n
		}
	}
	if maxN != wantArgs {
		t.Fatalf("最大占位符编号 $%d ≠ args 长度 %d（占位符与参数错位）：\n%s", maxN, wantArgs, sql)
	}
	for n := 1; n <= wantArgs; n++ {
		if !seen[n] {
			t.Fatalf("占位符 $%d 缺失（args 长度 %d）：\n%s", n, wantArgs, sql)
		}
	}
}

// assertSQLHasNoWhereKeyword 防呆：BuildWhere 的契约是**不含** WHERE 关键字，
// 由 whereClause 补。若两者都补就会拼出 "WHERE WHERE"。
func assertSQLHasNoWhereKeyword(t *testing.T, cond string) {
	t.Helper()
	if strings.Contains(strings.ToUpper(cond), "WHERE") {
		t.Fatalf("BuildWhere 不得包含 WHERE 关键字（由 whereClause 补）：%s", cond)
	}
}

func TestBuildWhereEmpty(t *testing.T) {
	cond, args := BuildWhere(Filter{})
	if cond != "" {
		t.Fatalf("无条件时应返回空串，实际 %q", cond)
	}
	if args != nil {
		t.Fatalf("无条件时应返回 nil args（pgx 对 nil 与空切片处理不同），实际 %#v", args)
	}
	if whereClause("") != "" {
		t.Fatal("空条件不应产生 WHERE 关键字")
	}
}

// TestBuildWherePlaceholderAlignment 逐个叠加过滤条件，验证编号与参数一一对应。
//
// 这是本文件的核心用例：每加一个条件就重新断言一次，一旦有人插入条件却忘了
// args、或改了 append 顺序，都会在最小的叠加步数上炸出来。
func TestBuildWherePlaceholderAlignment(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	curAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	var curID int64 = 42

	steps := []struct {
		name   string
		filter Filter
		want   []any // 期望 args，顺序即占位符顺序
	}{
		{"action", Filter{Action: "auth.login"}, []any{"auth.login"}},
		{"actor", Filter{ActorUserID: "u-1"}, []any{"u-1"}},
		{"target_type", Filter{TargetType: "user"}, []any{"user"}},
		{"target_id", Filter{TargetType: "user", TargetID: "x"}, []any{"user", "x"}},
		{"from", Filter{From: &from}, []any{from}},
		{"to", Filter{From: &from, To: &to}, []any{from, to}},
		{"cursor", Filter{CursorAt: &curAt, CursorID: &curID}, []any{curAt, curID}},
		{
			"全部叠加",
			Filter{
				Action: "share.create", ActorUserID: "u-1",
				TargetType: "share", TargetID: "s-1",
				From: &from, To: &to,
				CursorAt: &curAt, CursorID: &curID,
			},
			[]any{"share.create", "u-1", "share", "s-1", from, to, curAt, curID},
		},
	}

	for _, tc := range steps {
		cond, args := BuildWhere(tc.filter)
		assertSQLHasNoWhereKeyword(t, cond)
		if len(args) != len(tc.want) {
			t.Fatalf("%s: args 长度 %d，期望 %d（%#v）", tc.name, len(args), len(tc.want), args)
		}
		for i := range tc.want {
			if args[i] != tc.want[i] {
				t.Fatalf("%s: args[%d] = %#v，期望 %#v（占位符 $%d 会取到错值）",
					tc.name, i, args[i], tc.want[i], i+1)
			}
		}
		assertPlaceholdersAligned(t, cond, len(args))
	}
}

// TestBuildWhereCursorIsLast 游标条件必须排在最后且使用两个连续占位符。
//
// 顺序敏感：Store.Query 依赖「游标条件在末尾」来构造 (at, id) 行比较，
// 且 total 查询靠「清空游标后重算」复用同一个 BuildWhere。
func TestBuildWhereCursorIsLast(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	curAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	var curID int64 = 7

	cond, args := BuildWhere(Filter{Action: "media.delete", From: &from, CursorAt: &curAt, CursorID: &curID})
	if len(args) != 4 {
		t.Fatalf("args 应为 4 个，实际 %d: %#v", len(args), args)
	}
	// 行比较必须用 (at, id) 两列 —— at 可能重复（同一秒多条），只按 at 会漏行/重复。
	if !strings.Contains(cond, "(at, id) < ($3, $4)") {
		t.Fatalf("游标条件应为 (at, id) < ($3, $4)，实际 %q", cond)
	}
	if !strings.HasSuffix(cond, "(at, id) < ($3, $4)") {
		t.Fatalf("游标条件必须在末尾（total 查询依赖清空游标后复用同一函数），实际 %q", cond)
	}
	if !strings.HasPrefix(cond, "action = $1 AND at >= $2 AND ") {
		t.Fatalf("前置条件顺序不符：%q", cond)
	}

	// 只有 CursorID 没有 CursorAt（半个游标）时不得拼出残缺条件。
	half, halfArgs := BuildWhere(Filter{CursorID: &curID})
	if strings.Contains(half, "at, id") {
		t.Fatalf("半个游标不应产生游标条件：%q（args=%#v）", half, halfArgs)
	}
	if half != "" || halfArgs != nil {
		t.Fatalf("半个游标应等价于无条件，实际 %q / %#v", half, halfArgs)
	}
}

func TestClampLimits(t *testing.T) {
	// <=0 回落默认值；超上限夹到上限（不是回落默认值 —— 见 clampAuditLimit 注释）。
	for in, want := range map[int]int{
		0: defaultAuditLimit, -5: defaultAuditLimit,
		1: 1, 199: 199, 200: maxAuditLimit, 201: maxAuditLimit, 100000: maxAuditLimit,
	} {
		if got := clampAuditLimit(in); got != want {
			t.Fatalf("clampAuditLimit(%d) = %d，期望 %d", in, got, want)
		}
	}
	for in, want := range map[int]int{
		0: defaultJobsLimit, -1: defaultJobsLimit,
		1: 1, 200: maxJobsLimit, 500: maxJobsLimit,
	} {
		if got := clampJobsLimit(in); got != want {
			t.Fatalf("clampJobsLimit(%d) = %d，期望 %d", in, got, want)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 9, 15, 12, 34, 56, 789012345, time.UTC)
	gotT, gotID, err := decodeCursor(encodeCursor(ts, 9876543210))
	if err != nil {
		t.Fatalf("游标解码失败: %v", err)
	}
	if !gotT.Equal(ts) {
		t.Fatalf("时间不往返：%v ≠ %v", gotT, ts)
	}
	if gotID != 9876543210 {
		t.Fatalf("id 不往返：%d", gotID)
	}

	// 合法 base64、含分隔符、但 id 段不是数字（确定性构造，不靠手写 base64 字面量）。
	invalidID := base64.URLEncoding.EncodeToString([]byte(ts.Format(time.RFC3339Nano) + "|notan"))

	for _, bad := range []string{
		"!!!not-base64!!!",        // 非 base64
		"aGVsbG8",                 // "hello"，无分隔符
		encodeCursor(ts, 1) + "=", // 破坏填充
		invalidID,                 // id 段非数字
	} {
		if _, _, err := decodeCursor(bad); err == nil {
			t.Fatalf("非法游标 %q 应报错", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /admin/jobs 的 UNION 查询
// ---------------------------------------------------------------------------

func TestBuildJobsSQLAlignment(t *testing.T) {
	cases := []struct {
		name     string
		q        JobQuery
		wantArgs int
	}{
		{"默认(无过滤)", JobQuery{}, 1},                          // 仅 limit
		{"两类+状态", JobQuery{JobType: "", Status: "done"}, 2}, // status + limit
		{"仅index", JobQuery{JobType: "index"}, 1},
		{"仅index+状态", JobQuery{JobType: "index", Status: "done"}, 2},
		{"仅transcode+状态", JobQuery{JobType: "transcode", Status: "failed"}, 2},
	}
	for _, tc := range cases {
		sql, args := BuildJobsSQL(tc.q)
		if len(args) != tc.wantArgs {
			t.Fatalf("%s: args 长度 %d，期望 %d（%#v）", tc.name, len(args), tc.wantArgs, args)
		}
		assertPlaceholdersAligned(t, sql, len(args))

		// limit 必须是最后一个参数且类型为 int（编号最大者）。
		if _, ok := args[len(args)-1].(int); !ok {
			t.Fatalf("%s: 最后一个参数应为 limit(int)，实际 %T", tc.name, args[len(args)-1])
		}
	}
}

// TestBuildJobsSQLStatusSharedBetweenBranches 两个 UNION 分支必须共用同一个 status 占位符。
//
// 若各给一个编号（$1 与 $2 都塞 "done"），不仅白传一个参数，
// 更糟的是以后有人只改一个分支的条件时，args 长度与占位符编号会静默错位。
func TestBuildJobsSQLStatusSharedBetweenBranches(t *testing.T) {
	sql, args := BuildJobsSQL(JobQuery{JobType: "", Status: "done"})
	if len(args) != 2 {
		t.Fatalf("status 共用时 args 应为 2 个（status + limit），实际 %d: %#v", len(args), args)
	}
	if strings.Count(sql, "WHERE status = $1") != 2 {
		t.Fatalf("两个分支应各出现一次 WHERE status = $1：\n%s", sql)
	}
	if !strings.Contains(sql, "LIMIT $2") {
		t.Fatalf("limit 应为 $2：\n%s", sql)
	}
}

func TestBuildJobsSQLBranchSelection(t *testing.T) {
	both, _ := BuildJobsSQL(JobQuery{JobType: ""})
	if !strings.Contains(both, "FROM index_jobs") || !strings.Contains(both, "FROM transcode_jobs") {
		t.Fatalf("空 JobType 应同时查两张表：\n%s", both)
	}
	if strings.Count(both, "UNION ALL") != 1 {
		t.Fatalf("两支之间应恰好一个 UNION ALL：\n%s", both)
	}

	all, _ := BuildJobsSQL(JobQuery{JobType: "all"})
	if all != both {
		t.Fatal("JobType=all 与空值应生成相同 SQL")
	}

	onlyIdx, _ := BuildJobsSQL(JobQuery{JobType: "index"})
	if !strings.Contains(onlyIdx, "FROM index_jobs") || strings.Contains(onlyIdx, "transcode_jobs") {
		t.Fatalf("index 只应查 index_jobs：\n%s", onlyIdx)
	}
	if strings.Contains(onlyIdx, "UNION ALL") {
		t.Fatalf("单表不应有 UNION ALL：\n%s", onlyIdx)
	}

	onlyTr, _ := BuildJobsSQL(JobQuery{JobType: "transcode"})
	if !strings.Contains(onlyTr, "FROM transcode_jobs") || strings.Contains(onlyTr, "index_jobs") {
		t.Fatalf("transcode 只应查 transcode_jobs：\n%s", onlyTr)
	}
}

// TestBuildJobsSQLShapesMatch 两支的列数与顺序必须完全一致，否则 UNION 会报错。
//
// 这里直接数 SELECT 列表：UNION ALL 要求各分支列数与类型可对齐，
// 写错一列会在运行时（而不是编译时）炸。
func TestBuildJobsSQLShapesMatch(t *testing.T) {
	sql, _ := BuildJobsSQL(JobQuery{})
	// 剥掉外层 SELECT * FROM ( ... ) j 包装，只看 UNION 的两个分支。
	inner := sql[strings.Index(sql, "(")+1 : strings.LastIndex(sql, ") j")]
	for _, branch := range strings.Split(inner, "UNION ALL") {
		sel := branch[strings.Index(branch, "SELECT"):strings.Index(branch, "FROM")]
		// 顶层逗号数 + 1 = 列数；忽略 NULL::text 里的 :: 与函数调用（本查询没有）。
		cols := strings.Count(sel, ",") + 1
		if cols != 14 {
			t.Fatalf("分支列数应为 14，实际 %d：\n%s", cols, sel)
		}
	}
	// 判别列必须存在。
	if !strings.Contains(sql, "'index'::text AS job_type") || !strings.Contains(sql, "'transcode'::text AS job_type") {
		t.Fatalf("两个分支都必须有 job_type 判别列：\n%s", sql)
	}
	// 所有 uuid 列显式 ::text，避免驱动层对 NULL::uuid → *string 的扫描歧义。
	for _, col := range []string{"id::text", "user_id::text", "media_id::text", "node_id::text"} {
		if !strings.Contains(sql, col) {
			t.Fatalf("缺失 ::text 转换 %q（uuid 列必须转 text，否则 NULL 扫进 *string 行为依驱动而定）：\n%s", col, sql)
		}
	}
	// 排序键必须与游标/分页假设一致。
	if !strings.Contains(sql, "ORDER BY created_at DESC, id DESC") {
		t.Fatalf("排序应为 created_at DESC, id DESC：\n%s", sql)
	}
}
