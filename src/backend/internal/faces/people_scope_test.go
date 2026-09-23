package faces

// GET /people 与 GET /people/:id/media 的「media 内容按调用者可见性收窄」守卫。
//
// 背景：这两个端点的 SQL 原先**没有任何属主/空间条件**。`GET /people/:id/media`
// 只要知道人物 id，任一带 media:read 的账号（含 viewer 角色）就能拿到他人的
// 全部媒体 ID 与文件名 —— 已用真实第二个账号实测复现（200 + owner 的 3 条媒体）。
// 同类问题也存在于 `GET /people` 的 face_count / cover_media_id / unnamed.count / cover。
//
// 本文件的三道守卫（都自带**前提自证**，防空转）：
//
//  1. 行为侧：生成的 SQL 文本必须**逐字节包含** mediascope 产出的可见性谓词
//     （即同一段代码产出，不是"看起来调用了函数"）；谓词的两个臂（personal / shared，
//     且 shared 含成员 ∪ 属主）都要在，漏臂的方向是**多返回数据且不报错**；
//  2. 编号侧：占位符编号与 args 个数自洽。编号错位只会在**运行期**被 pgx 以
//     "expected N arguments" 拒绝，编译期毫无提示，故必须单独钉住；
//  3. 形状侧：无身份时恒假（fail-closed）；people 行不按属主过滤（未决建模问题，
//     见 ListPeople 注释）；faces 包里不得再出现手写的可见性谓词。
//
// ⚠️ 本文件**不覆盖** SQL 的真实可执行性（无 DB 集成测试可用）：括号配平、
// 谓词存在、编号自洽都能测，但 `EXISTS (...) AND false` 之类在 PostgreSQL 上
// 能否按预期执行，必须由带真实数据库的端到端验证补上。

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"panoalbum/internal/mediascope"
)

// placeholders 解析 SQL 文本里出现过的全部占位符编号 $n（本项目 SQL 的字符串
// 字面量里没有 $，故无需词法分析）。
func placeholders(sql string) []int {
	var out []int
	for i := 0; i < len(sql); i++ {
		if sql[i] != '$' {
			continue
		}
		j := i + 1
		for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
			j++
		}
		if j == i+1 {
			continue // 裸 '$' 不是占位符
		}
		n, err := strconv.Atoi(sql[i+1 : j])
		if err != nil {
			continue
		}
		out = append(out, n)
		i = j - 1
	}
	return out
}

// assertQueryArgsConsistent 钉住「占位符编号与 args 个数自洽」：
// 已有编号必须从 $1 连续到 $len(args)（pgx 按位置绑定，缺号/超号只在运行期报错）；
// 并顺带做一次括号配平，拦住 Sprintf 拼接时漏写括号这类手误。
func assertQueryArgsConsistent(t *testing.T, name, sql string, args []any) {
	t.Helper()
	ns := placeholders(sql)
	seen := map[int]bool{}
	max := 0
	for _, n := range ns {
		seen[n] = true
		if n > max {
			max = n
		}
	}
	if max != len(args) {
		t.Fatalf("%s：占位符最高编号 $%d 与 args 个数 %d 不一致（运行期会被 pgx 以 "+
			"expected N arguments 拒绝）:\n%s", name, max, len(args), sql)
	}
	for n := 1; n <= max; n++ {
		if !seen[n] {
			t.Fatalf("%s：占位符编号不连续，缺 $%d（已用 %v，args 个数 %d）:\n%s",
				name, n, ns, len(args), sql)
		}
	}
	if o, c := strings.Count(sql, "("), strings.Count(sql, ")"); o != c {
		t.Fatalf("%s：括号不配平（%d 个 '(' vs %d 个 ')'），拼接 SQL 时漏写了括号:\n%s",
			name, o, c, sql)
	}
}

// TestPersonMediaQueryScopedToCaller 是本次越权修复的**靶心**：
// PersonMedia 生成的 WHERE 必须带上调用者可见性谓词（而不是"看起来调用了函数"）。
func TestPersonMediaQueryScopedToCaller(t *testing.T) {
	const pid = "11111111-1111-1111-1111-111111111111"
	sql, args := personMediaQuery(pid, "u1", 25)

	want, vargs := mediascope.VisibleCondFor(2, "u1", "m")
	if len(vargs) != 1 || vargs[0] != "u1" {
		t.Fatalf("前提不成立：mediascope 谓词应恰好占 1 个参数位，实得 %v", vargs)
	}
	if !strings.Contains(sql, want) {
		t.Fatalf("PersonMedia 的 WHERE 必须**逐字节**包含 mediascope 产出的可见性谓词"+
			"（同一段代码产出，不得手写副本）。\n缺少: %q\n实际 SQL:\n%s", want, sql)
	}
	// 谓词的各臂逐一在场：只判 personal 会漏掉共享空间，只判 shared 会漏掉本人个人空间，
	// 而 shared 只判 members 会漏掉 shared_space.owner_id —— 每一次漏臂都是**多返回数据**。
	for _, arm := range []string{
		"m.space = 'personal' AND (m.owner_id = $2 OR",
		"shared_space_members sm WHERE sm.user_id = $2",
		"shared_space ss WHERE ss.owner_id = $2",
	} {
		if !strings.Contains(sql, arm) {
			t.Errorf("可见性谓词缺少臂 %q（漏臂不报错、只多返回数据）:\n%s", arm, sql)
		}
	}
	if !strings.Contains(sql, "f.person_id = $1::uuid") {
		t.Errorf("原有参数 $1 = person_id 被改动了:\n%s", sql)
	}
	if !strings.Contains(sql, "m.deleted_at IS NULL") {
		t.Errorf("软删过滤被摘掉了:\n%s", sql)
	}

	assertQueryArgsConsistent(t, "personMediaQuery(userID=u1)", sql, args)
	if len(args) != 3 || args[0] != pid || args[1] != "u1" || args[2] != 25 {
		t.Fatalf("args 布局应为 [$1=personID, $2=userID, $3=limit]，实得 %v", args)
	}

	// 前提自证：改造前（有缺陷）的 SQL 文本**不含**该谓词，否则上面的"包含"断言是空转的。
	const legacy = `
		SELECT m.id::text, COALESCE(m.filename, ''), m.taken_at
		FROM faces f
		JOIN media m ON m.id = f.media_id
		WHERE f.person_id = $1::uuid AND m.deleted_at IS NULL
		GROUP BY m.id, m.filename, m.taken_at
		ORDER BY m.taken_at DESC NULLS LAST, m.id
		LIMIT $2`
	if !strings.Contains(legacy, "f.person_id = $1::uuid AND m.deleted_at IS NULL") {
		t.Fatal("守卫失效：自证文本与改造前的实现不一致，本用例的前提不成立")
	}
	if strings.Contains(legacy, want) {
		t.Fatal("守卫失效：有缺陷的旧 SQL 也包含该谓词，本断言拦不住回归")
	}
}

// TestPersonMediaQueryFailClosedWithoutIdentity 靶心之二：
// 拿不到可信身份时，唯一安全的输出是「谁也看不见」，而不是退化成
// 「只少了属主条件、只剩 person_id」的查询 —— 后者与本次事故完全同形。
func TestPersonMediaQueryFailClosedWithoutIdentity(t *testing.T) {
	const pid = "22222222-2222-2222-2222-222222222222"
	sql, args := personMediaQuery(pid, "", 500)

	if vis, vargs := mediascope.VisibleCondFor(2, "", "m"); vis != mediascope.FailClosed || vargs != nil {
		t.Fatalf("前提不成立：mediascope 在无身份时应恒假且不带参数，实得 (%q, %v)", vis, vargs)
	}
	if !strings.Contains(sql, mediascope.FailClosed) {
		t.Fatalf("无身份时必须恒假收窄（WHERE 里出现 %q）:\n%s", mediascope.FailClosed, sql)
	}
	if strings.Contains(sql, "owner_id") || strings.Contains(sql, "shared_space") {
		t.Fatalf("无身份时不得出现任何属主/空间条件（只允许恒假）:\n%s", sql)
	}

	// fail-closed 时谓词不占参数位，LIMIT 编号应自动前移，仍与 args 自洽。
	assertQueryArgsConsistent(t, `personMediaQuery(userID="")`, sql, args)
	if len(args) != 2 || args[0] != pid || args[1] != 500 {
		t.Fatalf("fail-closed 时 args 应为 [$1=personID, $2=limit]，实得 %v", args)
	}
}

// TestListPeopleQueriesScopedToCaller 钉住 GET /people 只收窄 media 派生字段。
func TestListPeopleQueriesScopedToCaller(t *testing.T) {
	namedSQL, namedArgs, unnamedSQL, unnamedArgs := listPeopleQueries("u1")

	visMedia, _ := mediascope.VisibleCondFor(1, "u1", "m")
	visCover, _ := mediascope.VisibleCondFor(1, "u1", "mc")

	if !strings.Contains(namedSQL, visMedia) {
		t.Fatalf("named：人脸所属媒体侧缺少可见性谓词。\n缺少: %q\n实际 SQL:\n%s", visMedia, namedSQL)
	}
	if !strings.Contains(namedSQL, visCover) {
		t.Fatalf("named：**封面媒体**侧缺少可见性谓词 —— p.cover_media_id 是指向他人媒体的 UUID，"+
			"同样必须收窄。\n缺少: %q\n实际 SQL:\n%s", visCover, namedSQL)
	}
	if !strings.Contains(unnamedSQL, visMedia) {
		t.Fatalf("unnamed：缺少可见性谓词。\n缺少: %q\n实际 SQL:\n%s", visMedia, unnamedSQL)
	}

	// 谓词必须挂在 faces 的 LEFT JOIN 条件里：这样 count(f.id) 与 array_agg(f.media_id)
	// 无需改动即自然只覆盖可见人脸，同时「可见媒体为 0 的人物」仍留在名单里（计数 0、封面空），
	// 不会被 INNER JOIN 从人物列表里抹掉。
	if !strings.Contains(namedSQL, "LEFT JOIN faces f") {
		t.Errorf("named 的 faces 必须是 LEFT JOIN，否则可见媒体为 0 的人物会从名单里消失:\n%s", namedSQL)
	}
	if !strings.Contains(namedSQL, "count(f.id)") {
		t.Errorf("named 的计数口径被改动了（应为 count(f.id)）:\n%s", namedSQL)
	}
	if !strings.Contains(unnamedSQL, "count(DISTINCT f.media_id)") {
		t.Errorf("unnamed 的计数口径被改动了（应为 count(DISTINCT f.media_id)）:\n%s", unnamedSQL)
	}

	// 决策登记（本轮刻意不做的事）：people 表**没有 owner 列**，
	// 「人物库是全站共享元数据，还是每人一套」仍是**未决建模问题，需用户裁决**。
	// 因此这里只收窄 media 派生字段，不给 people 行加属主过滤。
	// 若有人把它误当成已解决而加上 p.owner_id，本断言会失败 —— 那需要 schema 变更 +
	// 存量数据迁移 + 用户裁决，不该悄悄混进这次越权修复。
	if !strings.Contains(namedSQL, "FROM people p") {
		t.Fatalf("named 必须以 people 为全量驱动表:\n%s", namedSQL)
	}
	if strings.Contains(namedSQL, "p.owner_id") || strings.Contains(unnamedSQL, "p.owner_id") {
		t.Fatalf("people 行不应按属主过滤（见 ListPeople 注释里的未决建模问题）:\n%s\n%s",
			namedSQL, unnamedSQL)
	}

	assertQueryArgsConsistent(t, "listPeopleQueries(userID=u1)/named", namedSQL, namedArgs)
	assertQueryArgsConsistent(t, "listPeopleQueries(userID=u1)/unnamed", unnamedSQL, unnamedArgs)
	if len(namedArgs) != 1 || namedArgs[0] != "u1" ||
		len(unnamedArgs) != 1 || unnamedArgs[0] != "u1" {
		t.Fatalf("两条查询都应恰好绑 1 个身份参数，实得 named=%v unnamed=%v", namedArgs, unnamedArgs)
	}
}

// TestListPeopleQueriesFailClosedWithoutIdentity 覆盖 GET /people 的无身份路径。
//
// 语义：无身份时人物名册（纯元数据）仍在，但计数全为 0、封面全为空、unnamed 为空。
func TestListPeopleQueriesFailClosedWithoutIdentity(t *testing.T) {
	namedSQL, namedArgs, unnamedSQL, unnamedArgs := listPeopleQueries("")

	for _, c := range []struct {
		name string
		sql  string
		args []any
	}{
		{"named", namedSQL, namedArgs},
		{"unnamed", unnamedSQL, unnamedArgs},
	} {
		if !strings.Contains(c.sql, mediascope.FailClosed) {
			t.Errorf("%s：无身份时必须恒假收窄（缺 %q）:\n%s", c.name, mediascope.FailClosed, c.sql)
		}
		if strings.Contains(c.sql, "owner_id") {
			t.Errorf("%s：无身份时不得出现属主条件:\n%s", c.name, c.sql)
		}
		assertQueryArgsConsistent(t, "listPeopleQueries(userID=\"\")/"+c.name, c.sql, c.args)
		if len(c.args) != 0 {
			t.Errorf("%s：fail-closed 时谓词不占参数位，args 应为空，实得 %v", c.name, c.args)
		}
	}
}

// TestFacesHandlerPassesCallerIdentity 形状侧守卫（与 internal/search/query_visible_test.go
// 同一手法：读源码文本做断言）——两个端点都必须把上下文里的 user_id 一路传进 Store。
// 传参被摘掉时 SQL 仍能编译通过、测试也全绿，只有真实请求才会泄漏，因此必须单独钉住。
func TestFacesHandlerPassesCallerIdentity(t *testing.T) {
	b, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatalf("读不到 api.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	if !strings.Contains(src, `h.Store.PersonMedia(c.Request.Context(), c.Param("id"), c.GetString("user_id"), limit)`) {
		t.Fatal("PersonMedia handler 未把 c.GetString(\"user_id\") 传给 Store：" +
			"身份没传下去，SQL 里的谓词就绑不上调用者（越权原样复现）")
	}
	if !strings.Contains(src, `h.Store.ListPeople(c.Request.Context(), c.GetString("user_id"))`) {
		t.Fatal("ListPeople handler 未把 c.GetString(\"user_id\") 传给 Store")
	}
	// 前提自证：改造前的两处调用不含身份参数，否则上面的断言是空转的。
	for _, legacy := range []string{
		`h.Store.PersonMedia(c.Request.Context(), c.Param("id"), limit)`,
		`h.Store.ListPeople(c.Request.Context())`,
	} {
		if strings.Contains(src, legacy) {
			t.Fatalf("守卫失效或接线被回退：api.go 里仍存在无身份的调用 %q", legacy)
		}
	}
}

// TestFacesPeopleSQLHasNoHandWrittenVisibilityPredicate 防止 faces 包再"手写一份"谓词。
//
// 手写副本的漂移方向通常不是报错而是**放宽**（漏掉某一臂 = 多返回数据，SQL 不报错），
// 本项目已为此付出过一次越权的代价，故谓词只允许来自 internal/mediascope。
func TestFacesPeopleSQLHasNoHandWrittenVisibilityPredicate(t *testing.T) {
	b, err := os.ReadFile("people.go")
	if err != nil {
		t.Fatalf("读不到 people.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	if n := strings.Count(src, "shared_space_members"); n != 0 {
		t.Fatalf("faces/people.go 里仍有 %d 处 shared_space_members —— 可见性谓词必须来自 "+
			"mediascope 单一真源，不得手写副本", n)
	}
	if n := strings.Count(src, "mediascope.VisibleCondFor("); n != 3 {
		t.Fatalf("people.go 里 mediascope.VisibleCondFor 出现 %d 次，期望 3 次"+
			"（PersonMedia / named 媒体侧+封面侧 / unnamed）—— 少一处即该查询不再收窄", n)
	}
	// 前提自证：用于自证的旧文本确实含目标串，否则上面的 count==0 是空转。
	legacy := "EXISTS(SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $%[1]d)"
	if strings.Count(legacy, "shared_space_members") != 1 {
		t.Fatal("守卫失效：自证文本不含目标字符串，断言空转")
	}
}
