package albums

// P1-04（相册条目硬上限 + truncated 标记）与 P2-08（List 聚合查询 + first_media_id）
// 的回归网。与 ownership_test.go 同一理由：本仓库 handler 测试没有可用数据库，
// 用「纯函数单测 + 源码形状断言 + 坏版本自证」三层互补钉住。

import (
	"os"
	"strings"
	"testing"

	"panoalbum/internal/media"
)

// ---- P1-04：服务端硬上限 5000 + truncated ----

func TestCapItemsBoundary(t *testing.T) {
	mk := func(n int) []media.MediaRef {
		out := make([]media.MediaRef, n)
		for i := range out {
			out[i] = media.MediaRef{ID: "m"}
		}
		return out
	}
	cases := []struct {
		name      string
		n         int
		wantLen   int
		wantTrunc bool
	}{
		{"空", 0, 0, false},
		{"远未满", 100, 100, false},
		{"恰好满", albumItemsHardLimit, albumItemsHardLimit, false},
		{"超一条即截断", albumItemsHardLimit + 1, albumItemsHardLimit, true},
		{"远超", albumItemsHardLimit * 3, albumItemsHardLimit, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, trunc := capItems(mk(tc.n))
			if len(got) != tc.wantLen || trunc != tc.wantTrunc {
				t.Fatalf("capItems(n=%d) = (len=%d, truncated=%v)，want (%d, %v)",
					tc.n, len(got), trunc, tc.wantLen, tc.wantTrunc)
			}
		})
	}
}

// TestGetHasHardLimitAndTruncated 钉住 Get 的两个分支都带 LIMIT 且响应带 truncated。
func TestGetHasHardLimitAndTruncated(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	if !strings.Contains(src, "const albumItemsHardLimit = 5000") {
		t.Fatal("albumItemsHardLimit 必须是 5000（与审查采信的硬上限口径一致）")
	}
	// 两个分支（smart / manual）各一条带上限的 LIMIT。
	if n := strings.Count(src, "LIMIT `+strconv.Itoa(albumItemsHardLimit+1)"); n != 2 {
		t.Fatalf("Get 的 smart/manual 两分支应各有一条 硬上限+1 的 LIMIT，实际 %d 处", n)
	}
	// 截断判定必须经 capItems 落进 Detail.Truncated，且 Detail 带 json 标记下发。
	if n := strings.Count(src, "d.Items, d.Truncated = capItems(items)"); n != 2 {
		t.Fatalf("两个分支都应经 capItems 回填 d.Truncated，实际 %d 处", n)
	}
	if !strings.Contains(src, "`json:\"truncated\"`") {
		t.Fatal("Detail 缺 truncated 字段的 json 标记：前端无法感知结果被截断")
	}

	// 自证：修复前的坏版本（无 LIMIT 的全量查询）必须让断言落空。
	broken := "ORDER BY m.taken_at DESC, m.id DESC`"
	if strings.Count(broken, "LIMIT `+strconv.Itoa(albumItemsHardLimit+1)") != 0 {
		t.Fatal("守卫失效：无 LIMIT 的版本也能通过断言")
	}
}

// ---- P2-08：List 聚合查询 + first_media_id 下发 ----

func TestListUsesAggregateQueries(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	// manual 相册的计数 + 首图必须来自一条 GROUP BY 聚合查询，而不是逐相册串行。
	if !strings.Contains(src, "GROUP BY ai.album_id") {
		t.Fatal("List 缺 GROUP BY ai.album_id 聚合：manual 相册回到每册 2 次串行查询（N+1）")
	}
	if !strings.Contains(src, "array_agg(ai.media_id ORDER BY ai.sort_key ASC, m.taken_at DESC))[1]") {
		t.Fatal("聚合查询未取首图（sort_key ASC, taken_at DESC 与详情同序）")
	}
	// 逐相册的串行计数查询不得还在 manual 分支里（smart 的 count 来自 criteria，允许保留）。
	if strings.Contains(src, "count(*)::int FROM album_items") {
		t.Fatal("manual 分支仍有逐相册串行 count 查询（计数应并入 GROUP BY 聚合）")
	}
	// 封面缩略图必须批量取回。
	if !strings.Contains(src, "m.id = ANY($1::uuid[]) AND m.deleted_at IS NULL") {
		t.Fatal("封面缩略图未走 ANY($1::uuid[]) 批量查询")
	}
	// first_media_id 必须在列表响应里下发（前端 AlbumsView 据此消除 N+1）。
	if !strings.Contains(src, "`json:\"first_media_id,omitempty\"`") {
		t.Fatal("Summary 缺 first_media_id 字段")
	}

	// 自证：修复前的形状（无 GROUP BY）必须让断言落空。
	broken := "SELECT count(*)::int FROM album_items ai JOIN media m WHERE ai.album_id = $1"
	if strings.Contains(broken, "GROUP BY ai.album_id") {
		t.Fatal("守卫失效：逐相册串行查询的版本也能通过断言")
	}
}
