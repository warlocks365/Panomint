package media

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// ---- 列清单的 NULL 保护（T-019；§二十一 收拢为单一真源后改名）----
//
// 背景：media.filename / folder_path / taken_at 在 DDL 中都可空，而 MediaRef 的
// Filename / FolderPath / TakenAt 是非指针字段。裸选可空列时，全库只要有一行 NULL，
// 整个端点就崩（实测 can't scan into dest[2] (col: filename): cannot scan NULL into *string）。
//
// 这里的断言对象从"时间轴那一份"变成了**唯一真源** MediaRefColumns（7 处调用共用）。
// "再出现第 N 份副本"这道防线由 mediaref_single_source_test.go 单独负责。
//
// 这三个测试是**离线**的 SQL 形状回归：真正的端到端证明用"三列全 NULL 的夹具"
// 对真实库跑过（改前 5 个端点崩 / 改后全部 200），见交付报告。

// TestListNullableColumnsGuarded 唯一真源的列清单不得裸选可空列 filename / folder_path。
func TestListNullableColumnsGuarded(t *testing.T) {
	norm := strings.Join(strings.Fields(MediaRefColumns), " ")

	// 命中"列表项位置上的裸可空列"：前面是开头或逗号，后面是逗号或结尾。
	// COALESCE(m.filename,'') 不会被命中——它前面是左括号，不是逗号。
	bare := regexp.MustCompile(`(^|,)\s*m\.(filename|folder_path)\s*(,|$)`)
	if m := bare.FindString(norm); m != "" {
		t.Fatalf("可空列裸出现在时间轴 SELECT 列表（命中 %q）："+
			"filename / folder_path 在 DDL 中可空而 MediaRef 同名字段非指针，"+
			"一行 NULL 就让整个 GET /media 返回 400，必须写成 COALESCE(m.<列>,'')。列表现值：%s", m, norm)
	}

	// 守卫自证：把**修复前**的列清单喂给同一个正则，必须命中。
	// 没有这一步，上面那条断言可能是"永远通过"的假测试（比如正则写错、或字段改名后失效）。
	const brokenList = `m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height`
	if bare.FindString(strings.Join(strings.Fields(brokenList), " ")) == "" {
		t.Fatalf("守卫失效：正则匹配不到修复前的裸选列清单（%s），该断言无法真正拦截回归", brokenList)
	}

	// 反向确认：不能靠"把列删掉"通过上一条。
	for _, col := range []string{"filename", "folder_path"} {
		if !strings.Contains(norm, "COALESCE(m."+col+",'')") {
			t.Fatalf("时间轴 SELECT 缺少 COALESCE(m.%s,'')。列表现值：%s", col, norm)
		}
	}

	// taken_at 是同一类可空列，但取法不同：在 List 的扫描边界用 *time.Time 承接
	// （MediaRef.TakenAt 是被 7 个扫描器与 JSON 契约共用的非指针 time.Time，
	// 改成指针会让 taken_at 变 null，属契约破坏）。
	// 因此它**应当**裸出现在这里——这条断言把该取舍钉住：谁在列清单里给它加 COALESCE，
	// 就必须回去同步 List 的 Scan 目标，不能悄悄制造第二种"缺失时间"语义。
	if strings.Contains(norm, "COALESCE(m.taken_at") {
		t.Fatalf("taken_at 的缺失语义由 List 扫描边界的 *time.Time 承接，不应在列清单里 COALESCE："+
			"此改动必须同步 List 的 Scan 目标。列表现值：%s", norm)
	}
	if !strings.Contains(norm, "m.taken_at") {
		t.Fatalf("时间轴 SELECT 应包含 m.taken_at。列表现值：%s", norm)
	}
}

// TestTimelineBucketSQLHandlesNullTakenAt 桶聚合必须能容纳 taken_at 为 NULL 的行。
//
// 只改 SELECT 列表不够：taken_at=NULL 的行走到 date_trunc 后扫回 time.Time 同样报错。
func TestTimelineBucketSQLHandlesNullTakenAt(t *testing.T) {
	sql := fmt.Sprintf(timelineBucketSQL, "month", "YYYY-MM", "m.deleted_at IS NULL")

	if !strings.Contains(sql, "COALESCE(to_char(date_trunc('month', m.taken_at), 'YYYY-MM'), 'unknown')") {
		t.Fatalf("桶聚合必须把可空的 taken_at 兜底为 'unknown'（与 /media/date-histogram 同口径）：%s", sql)
	}
	if !strings.Contains(sql, "GROUP BY 1") {
		t.Fatalf("桶聚合应按输出列分组：%s", sql)
	}

	// 排序：Postgres 只允许输出别名以**裸名**出现在 ORDER BY，`(b = 'unknown')` 这种
	// 把别名嵌进表达式的写法会报 column "b" does not exist（SQLSTATE 42703，已实测踩到）。
	if strings.Contains(sql, "(b = ") {
		t.Fatalf("ORDER BY 不得把输出别名 b 嵌进表达式（Postgres 报 column \"b\" does not exist）：%s", sql)
	}
	// 用 min(m.taken_at)：桶区间互不重叠故跨桶即时间新→旧；'unknown' 桶 min 为 NULL，
	// 配 NULLS LAST 落到末位（与 histogram.go 让 "unknown" 落末位的一致意图）。
	if !strings.Contains(sql, "ORDER BY min(m.taken_at) DESC NULLS LAST") {
		t.Fatalf("桶排序应为 min(m.taken_at) DESC NULLS LAST（时间新→旧，unknown 落末位）：%s", sql)
	}
}

// TestViewTruncWhitelist 桶粒度白名单完整性：year/month/day 三者齐全且带 to_char 格式。
func TestViewTruncWhitelist(t *testing.T) {
	for _, v := range []string{"year", "month", "day"} {
		g, ok := viewTrunc[v]
		if !ok {
			t.Fatalf("view %q 应在桶粒度白名单中", v)
		}
		if g.trunc == "" || g.toChar == "" {
			t.Fatalf("view %q 的 trunc / toChar 都不得为空，实际 %+v", v, g)
		}
	}
	// view=all 走"返回空桶数组"的分支，不应进白名单（否则会去聚合出一个无意义的桶）。
	if _, ok := viewTrunc["all"]; ok {
		t.Fatalf("view=all 应返回空桶数组，不应出现在桶粒度白名单中")
	}
}
