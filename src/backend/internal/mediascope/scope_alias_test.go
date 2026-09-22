package mediascope

import (
	"strings"
	"testing"
)

// TestAliasParamKeepsLegacyTextByteIdentical 是本次「别名参数化」改造的**兼容性闸门**。
//
// 改造前的 Conds / VisibleCond 把 `m.` 硬编码进谓词文本。如果为了支持别的表别名
// 而顺手改了这两个函数的输出，所有既有调用点（internal/media 三个端点、
// internal/search）的 SQL 都会在**运行期**才炸或行为漂移 —— 这类问题编译期看不见。
//
// 因此这里用**硬编码期望文本**（而不是"再调一次产品函数比较"）钉住旧签名输出。
// ⚠️ 为什么必须硬编码：本包内的早期守卫 TestVisibleCondSharesSharedArmWithConds
// 在真源自己写错时会"两边一起错、一起绿"，对真源回归是失明的。
// 硬编码期望文本是唯一能发现"真源被改坏"的写法。
func TestAliasParamKeepsLegacyTextByteIdentical(t *testing.T) {
	uid := "u-1"

	// --- Conds（space 档位版）---
	conds, args := Conds(Scope{Space: "personal", OwnerID: uid})
	if len(conds) != 2 || len(args) != 1 {
		t.Fatalf("personal: 期望 2 conds / 1 args，实际 %d / %d", len(conds), len(args))
	}
	if conds[0] != "m.space = 'personal'" {
		t.Errorf("personal conds[0] 漂移：%q", conds[0])
	}
	if conds[1] != "(m.owner_id = $1 OR "+wantFolderGrant(1, "m")+")" {
		t.Errorf("personal conds[1] 漂移：%q", conds[1])
	}

	conds, args = Conds(Scope{Space: "shared", MemberID: uid})
	if len(conds) != 2 || len(args) != 1 {
		t.Fatalf("shared: 期望 2 conds / 1 args，实际 %d / %d", len(conds), len(args))
	}
	if conds[0] != "m.space = 'shared'" {
		t.Errorf("shared conds[0] 漂移：%q", conds[0])
	}
	wantShared := "(EXISTS(SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $1)\n" +
		"\t\t\t\tOR EXISTS(SELECT 1 FROM shared_space ss WHERE ss.owner_id = $1))"
	if conds[1] != wantShared {
		t.Errorf("shared conds[1] 漂移：\n got=%q\nwant=%q", conds[1], wantShared)
	}

	// --- VisibleCond（并集版）---
	got, args := VisibleCond(1, uid)
	want := "((m.space = 'personal' AND m.owner_id = $1)\n" +
		"\t\tOR (m.space = 'shared' AND " + wantShared + ")\n" +
		"\t\tOR (" + wantFolderGrant(1, "m") + "))"
	if got != want {
		t.Errorf("VisibleCond 文本漂移：\n got=%q\nwant=%q", got, want)
	}
	if len(args) != 1 || args[0] != uid {
		t.Errorf("VisibleCond args 漂移：%v", args)
	}
}

// TestAliasEmptyProducesBareColumns 覆盖 alias="" 这一新档位（geo 等未加别名的查询用）。
func TestAliasEmptyProducesBareColumns(t *testing.T) {
	conds, args := CondsFor(Scope{Space: "personal", OwnerID: "u"}, "")
	if len(conds) != 2 || len(args) != 1 {
		t.Fatalf("期望 2 conds / 1 args，实际 %d / %d", len(conds), len(args))
	}
	if conds[0] != "space = 'personal'" {
		t.Errorf("裸列名 conds[0] = %q，期望不带前缀", conds[0])
	}
	if conds[1] != "(owner_id = $1 OR "+wantFolderGrant(1, "")+")" {
		t.Errorf("裸列名 conds[1] = %q，期望 owner+目录臂", conds[1])
	}
	if strings.Contains(conds[1], "m.owner_id") {
		t.Errorf("alias=\"\" 时不应出现 m. 前缀：%q", conds[1])
	}

	// shared 档：sharedVerdict 不引用 media 列，故与别名无关 —— 这条断言把它钉住，
	// 避免以后有人在 sharedVerdict 里加 `m.space` 之类的前缀而让别名为空的调用点炸掉。
	cEmpty, _ := CondsFor(Scope{Space: "shared", MemberID: "u"}, "")
	cM, _ := CondsFor(Scope{Space: "shared", MemberID: "u"}, "m")
	if cEmpty[1] != cM[1] {
		t.Errorf("sharedVerdict 不应随别名变化：\n empty=%q\n     m=%q", cEmpty[1], cM[1])
	}
	if cEmpty[0] != "space = 'shared'" {
		t.Errorf("裸列名 shared conds[0] = %q", cEmpty[0])
	}

	got, args := VisibleCondFor(5, "u", "")
	if !strings.Contains(got, "space = 'personal' AND owner_id = $5") {
		t.Errorf("VisibleCondFor(alias=\"\") 占位符/列名不对：%q", got)
	}
	// ⚠️ 这里必须断言 `m.space` / `m.owner_id` 而不是裸的 "m."：
	// sharedVerdict 里合法地存在 `sm.user_id` 与 `ss.owner_id` 两个**别的表**的别名，
	// 用 `strings.Contains(got, "m.")` 会把 `sm.` 误判成命中（本测试第一次就跑出了这个假失败）。
	if strings.Contains(got, "m.space") || strings.Contains(got, "m.owner_id") {
		t.Errorf("alias=\"\" 时不应出现 m. 前缀：%q", got)
	}
	if len(args) != 1 {
		t.Errorf("args 期望恰好 1 个，实际 %d", len(args))
	}
}

// TestAliasParamPlaceholderNumbering 对抗输入：三处占位符必须全部等于 start。
//
// 只改一处（例如 personal 臂用了 $start 而 shared 臂仍写死 $1）会导致 SQL 在
// **走到特定分支时**才报 "expected N arguments" —— 编译期与静态阅读都发现不了。
func TestAliasParamPlaceholderNumbering(t *testing.T) {
	for _, alias := range []string{"m", "", "media_alias"} {
		for _, start := range []int{1, 7, 99} {
			got, args := VisibleCondFor(start, "u-1", alias)
			if len(args) != 1 {
				t.Fatalf("alias=%q start=%d: args 期望 1 个，实际 %d", alias, start, len(args))
			}
			prefix := ""
			if alias != "" {
				prefix = alias + "."
			}
			wantN := "$" + itoa(start)
			// 并集谓词里应恰好出现 3 次该占位符：personal owner_id、成员 EXISTS、属主 EXISTS。
			if n := strings.Count(got, wantN); n != 4 {
				t.Errorf("alias=%q start=%d: %s 出现 %d 次，期望 4 次\n%s", alias, start, wantN, n, got)
			}
			// 不该出现任何别的 $n（否则占位符编号不一致）。
			for _, other := range []string{"$1", "$2", "$3", "$7", "$99"} {
				if other == wantN {
					continue
				}
				if strings.Contains(got, other) {
					t.Errorf("alias=%q start=%d: 意外出现占位符 %s\n%s", alias, start, other, got)
				}
			}
			if prefix != "" && !strings.Contains(got, prefix+"owner_id = "+wantN) {
				t.Errorf("alias=%q start=%d: 缺少带别名的 owner_id 占位符\n%s", alias, start, got)
			}
			if prefix == "" && !strings.Contains(got, "owner_id = "+wantN) {
				t.Errorf("alias=%q start=%d: 缺少裸列 owner_id 占位符\n%s", alias, start, got)
			}
		}
	}
}

// TestAliasParamFailClosed 无身份时必须恒假，且别名参数不得让它变成"放行"。
func TestAliasParamFailClosed(t *testing.T) {
	for _, alias := range []string{"m", "", "x"} {
		got, args := VisibleCondFor(1, "", alias)
		if got != FailClosed {
			t.Errorf("alias=%q: userID 为空时必须是 %q，实际 %q", alias, FailClosed, got)
		}
		if args != nil {
			t.Errorf("alias=%q: 恒假时不得带参数，实际 %v", alias, args)
		}
		conds, args := CondsFor(Scope{Space: "personal", OwnerID: ""}, alias)
		if len(conds) != 1 || conds[0] != FailClosed || args != nil {
			t.Errorf("alias=%q: personal 无主体必须恒假，实际 %v / %v", alias, conds, args)
		}
	}
}

// wantFolderGrant 与生产 folderGrantArm 逐字节一致的期望值（双写钉死形态）。
func wantFolderGrant(n int, alias string) string {
	fp := alias + ".folder_path"
	if alias == "" {
		fp = "folder_path"
	}
	return "EXISTS(SELECT 1 FROM folder_dirs gf, jsonb_array_elements(gf.grants) gfge" +
		" WHERE (" + fp + " = gf.path OR " + fp + " LIKE gf.path || '/%')" +
		" AND gfge->>'user_id' = $" + itoa(n) + " AND (gfge->>'read')::boolean)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
