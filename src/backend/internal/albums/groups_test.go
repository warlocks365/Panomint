package albums

// Job000101 分组查询的 SQL 文本/参数流钉住测试（纯逻辑，不触库）。
// 钉三件事：① 可见性谓词（调用者可见集）嵌进每条媒体查询；
// ② 参数占位符自洽（$1=ids/$2=userID/$3=limit 续编无错位）；
// ③ SELECT 列表 = MediaRefColumns 唯一真源 + 末尾追加 album_id 扩展列。

import (
	"strings"
	"testing"

	"panoalbum/internal/media"
	"panoalbum/internal/mediascope"
)

// visOf 测试内重算期望谓词（与产品同调 mediascope，避免手写副本漂移）。
func visOf(t *testing.T, start int, userID string) string {
	t.Helper()
	vis, args := mediascope.VisibleCondFor(start, userID, "m")
	if len(args) != 1 || args[0] != userID {
		t.Fatalf("VisibleCondFor args = %v, want [%s]", args, userID)
	}
	return vis
}

func TestGroupCountsQueries_ScopedAndParameterized(t *testing.T) {
	uid := "u-1"
	ids := []string{"a1", "a2"}
	sql, args := groupCountsQueries(uid, ids)
	vis := visOf(t, 2, uid)
	if !strings.Contains(sql, vis) {
		t.Fatalf("counts SQL 未嵌可见性谓词:\n%s", sql)
	}
	if !strings.Contains(sql, "ai.album_id = ANY($1::uuid[])") {
		t.Fatalf("counts SQL 缺 album_id ANY 绑定:\n%s", sql)
	}
	if len(args) != 2 || args[0].([]string)[0] != "a1" || args[1] != uid {
		t.Fatalf("counts args = %v", args)
	}
}

func TestGroupItemsQueries_WindowAndColumns(t *testing.T) {
	uid := "u-1"
	ids := []string{"a1"}
	sql, args := groupItemsQueries(uid, ids, groupItemsLimit)
	vis := visOf(t, 2, uid)
	for _, want := range []string{
		vis,
		"ROW_NUMBER() OVER (PARTITION BY ai.album_id",
		"WHERE m.rn <= $3",
		media.MediaRefColumns,
		"m.album_id",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("items SQL 缺 %q:\n%s", want, sql)
		}
	}
	// SELECT 列表必须以 MediaRefColumns 起首、album_id 追加在末尾（扫描器扩展位约定）。
	head := strings.TrimPrefix(sql, "\n\t\tSELECT ")
	if !strings.HasPrefix(head, media.MediaRefColumns+", m.album_id") {
		t.Fatalf("SELECT 列表形态违例（须 MediaRefColumns + 末尾 album_id）:\n%s", head[:120])
	}
	if len(args) != 3 || args[2] != groupItemsLimit {
		t.Fatalf("items args = %v（$3 应=limit）", args)
	}
}

func TestUngroupedQueries_NotInAnyOwnAlbum(t *testing.T) {
	uid := "u-1"
	countSQL, itemsSQL, args := ungroupedQueries(uid, ungroupedItemsLimit)
	vis := visOf(t, 1, uid)
	notIn := "NOT EXISTS(SELECT 1 FROM album_items ai JOIN albums a ON a.id = ai.album_id"
	for name, q := range map[string]string{"count": countSQL, "items": itemsSQL} {
		if !strings.Contains(q, vis) {
			t.Fatalf("%s SQL 未嵌可见性谓词:\n%s", name, q)
		}
		if !strings.Contains(q, notIn) || !strings.Contains(q, "a.owner_id = $2") {
			t.Fatalf("%s SQL 缺未分组谓词:\n%s", name, q)
		}
	}
	if !strings.Contains(itemsSQL, "LIMIT $3") || !strings.Contains(countSQL, "count(*)") {
		t.Fatalf("items/count SQL 形态异常:\ncount=%s\nitems=%s", countSQL, itemsSQL)
	}
	// args = [userID(vis $1), userID(owner $2), limit+1($3)]
	if len(args) != 3 || args[0] != uid || args[1] != uid || args[2] != ungroupedItemsLimit+1 {
		t.Fatalf("ungrouped args = %v", args)
	}
}

// 候选相册查询的 fail-closed 口径：只认本人 personal 空间的 manual/favorites。
func TestListGroupAlbumsSQL_FailClosed(t *testing.T) {
	for _, want := range []string{"a.owner_id = $1", "a.space = 'personal'", "a.type IN ('manual','favorites')"} {
		if !strings.Contains(listGroupAlbumsSQL, want) {
			t.Fatalf("listGroupAlbumsSQL 缺 %q", want)
		}
	}
	if strings.Contains(listGroupAlbumsSQL, "smart") {
		t.Fatalf("listGroupAlbumsSQL 不应含 smart（动态成员无 album_items，见文件头裁决）")
	}
}
