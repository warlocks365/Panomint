package albums

// Job000101 个人空间按相册分组（GET /albums/groups）。
//
// 语义裁决 [自行决策]：
//   - 分组范围 = 本人 personal 空间内 type IN ('manual','favorites') 且有可见成员的相册；
//     smart 相册不入分组——其成员由 criteria 动态计算、无 album_items 行，纳入需逐册
//     实时求值，成本高且语义上是「动态视图」而非「显式归档」。
//   - 未分组桶 = 不在「本人拥有的任何相册」里的 personal 媒体（album=none 同口径）；
//     仅被 smart 相册 criteria 命中的媒体仍算未分组。
//   - 每组只回前 groupItemsLimit 项（截断标记 truncated），完整视图仍走相册详情页；
//     未分组桶回前 ungroupedItemsLimit 项 + has_more，翻页走 /media?album=none 游标。
//
// 可见性：全部媒体行过 mediascope.VisibleCondFor（调用者可见集，与 List 同真源）；
// 相册行限 owner_id=调用者（不存在「全库」档，fail-closed）。

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/cursor"
	"panoalbum/internal/httperr"
	"panoalbum/internal/media"
	"panoalbum/internal/mediascope"
)

const (
	// groupItemsLimit 每组首屏项数（超出走相册详情页看全部）。
	groupItemsLimit = 8
	// ungroupedItemsLimit 未分组桶首屏项数（+1 探测 has_more，实际回前 N 条）。
	ungroupedItemsLimit = 24
)

// GroupSummary 一个相册分组（相册头 + 前 N 项 + 截断标记）。
type GroupSummary struct {
	AlbumID   string           `json:"album_id"`
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Count     int              `json:"count"`
	Items     []media.MediaRef `json:"items"`
	Truncated bool             `json:"truncated"`
}

// UngroupedBucket 未分组桶（不在任何本人相册里的 personal 媒体）。
// NextCursor 仅 has_more 时给出，续翻走 /media?album=none&cursor=…（同族游标契约）。
type UngroupedBucket struct {
	Count      int              `json:"count"`
	Items      []media.MediaRef `json:"items"`
	HasMore    bool             `json:"has_more"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// GroupsResult GET /albums/groups 响应。
type GroupsResult struct {
	Groups    []GroupSummary  `json:"groups"`
	Ungrouped UngroupedBucket `json:"ungrouped"`
}

// listGroupAlbumsSQL 候选相册：本人 personal 空间的 manual/favorites 相册。
// smart 排除（无 album_items，见文件头裁决）；排序与 List 一致（favorites 置顶）。
const listGroupAlbumsSQL = `
	SELECT a.id, a.name, a.type FROM albums a
	WHERE a.owner_id = $1 AND a.space = 'personal' AND a.type IN ('manual','favorites')
	ORDER BY a.type = 'favorites' DESC, a.updated_at DESC`

// groupCountsQueries 计数聚合（可见成员口径与 List 的聚合段逐字一致：谓词主体=调用者可见集）。
func groupCountsQueries(userID string, ids []string) (string, []any) {
	vis, visArgs := mediascope.VisibleCondFor(2, userID, "m")
	return `
		SELECT ai.album_id, count(*)::int
		FROM album_items ai JOIN media m ON m.id = ai.media_id
		WHERE ai.album_id = ANY($1::uuid[]) AND m.deleted_at IS NULL AND ` + vis + `
		GROUP BY ai.album_id`, append([]any{ids}, visArgs...)
}

// groupItemsQueries 每组前 N 项：窗口函数单查询；SELECT 列表 = MediaRefColumns +
// 末尾追加 album_id（追加列走 MediaRefScanner.Dests() 之后续的既定扩展位，见 mediaref.go）。
func groupItemsQueries(userID string, ids []string, limit int) (string, []any) {
	vis, visArgs := mediascope.VisibleCondFor(2, userID, "m")
	return `
		SELECT ` + media.MediaRefColumns + `, m.album_id
		FROM (
			SELECT ai.album_id,
			       ROW_NUMBER() OVER (PARTITION BY ai.album_id
			           ORDER BY ai.sort_key ASC, m.taken_at DESC NULLS LAST, m.id DESC) AS rn,
			       m.*
			FROM album_items ai JOIN media m ON m.id = ai.media_id
			WHERE ai.album_id = ANY($1::uuid[]) AND m.deleted_at IS NULL AND ` + vis + `
		) m
		WHERE m.rn <= $3
		ORDER BY m.album_id, m.rn`, append(append([]any{ids}, visArgs...), limit)
}

// ungroupedQueries 未分组桶 count + items（$1/$2 同绑调用者——可见谓词与
// 「不属于本人任何相册」谓词同主体；items 多取 1 条探测 has_more）。
func ungroupedQueries(userID string, limit int) (countSQL, itemsSQL string, args []any) {
	vis, visArgs := mediascope.VisibleCondFor(1, userID, "m")
	notIn := `NOT EXISTS(SELECT 1 FROM album_items ai JOIN albums a ON a.id = ai.album_id
		WHERE ai.media_id = m.id AND a.owner_id = $2)`
	base := ` FROM media m WHERE m.deleted_at IS NULL AND ` + vis + ` AND ` + notIn
	return `SELECT count(*)::int` + base,
		`SELECT ` + media.MediaRefColumns + base + `
		ORDER BY m.taken_at DESC NULLS LAST, m.id DESC
		LIMIT $3`,
		append(append(visArgs, userID), limit+1)
}

// Groups 相册分组聚合（查询次数 4，不随相册数增长）。
func (s *Store) Groups(ctx context.Context, userID string) (*GroupsResult, error) {
	res := &GroupsResult{Groups: []GroupSummary{}, Ungrouped: UngroupedBucket{Items: []media.MediaRef{}}}

	// ① 候选相册（id/name/type）
	type cand struct {
		id, name, typ string
	}
	rows, err := s.Pool.Query(ctx, listGroupAlbumsSQL, userID)
	if err != nil {
		return nil, err
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.name, &c.typ); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return res, s.fillUngrouped(ctx, userID, res)
	}

	// ② 计数
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.id)
	}
	counts := map[string]int{}
	countSQL, countArgs := groupCountsQueries(userID, ids)
	aggRows, err := s.Pool.Query(ctx, countSQL, countArgs...)
	if err != nil {
		return nil, err
	}
	for aggRows.Next() {
		var aid string
		var n int
		if err := aggRows.Scan(&aid, &n); err != nil {
			aggRows.Close()
			return nil, err
		}
		counts[aid] = n
	}
	aggRows.Close()
	if err := aggRows.Err(); err != nil {
		return nil, err
	}

	// ③ 每组前 N 项（单查询窗口函数；空相册在 ② 后剔除）
	nonEmpty := make([]string, 0, len(cands))
	byID := map[string]cand{}
	for _, c := range cands {
		if counts[c.id] > 0 {
			nonEmpty = append(nonEmpty, c.id)
			byID[c.id] = c
		}
	}
	itemsByAlbum := map[string][]media.MediaRef{}
	if len(nonEmpty) > 0 {
		itemSQL, itemArgs := groupItemsQueries(userID, nonEmpty, groupItemsLimit)
		itemRows, err := s.Pool.Query(ctx, itemSQL, itemArgs...)
		if err != nil {
			return nil, err
		}
		for itemRows.Next() {
			sc := media.NewMediaRefScanner()
			var aid string
			if err := itemRows.Scan(append(sc.Dests(), &aid)...); err != nil {
				itemRows.Close()
				return nil, err
			}
			itemsByAlbum[aid] = append(itemsByAlbum[aid], sc.Finish())
		}
		itemRows.Close()
		if err := itemRows.Err(); err != nil {
			return nil, err
		}
	}

	for _, id := range nonEmpty {
		c := byID[id]
		items := itemsByAlbum[id]
		res.Groups = append(res.Groups, GroupSummary{
			AlbumID:   c.id,
			Name:      c.name,
			Kind:      typeToKind(c.typ),
			Count:     counts[id],
			Items:     items,
			Truncated: counts[id] > len(items),
		})
	}

	return res, s.fillUngrouped(ctx, userID, res)
}

// fillUngrouped 未分组桶：count + 前 N 项 + has_more。
func (s *Store) fillUngrouped(ctx context.Context, userID string, res *GroupsResult) error {
	countSQL, itemsSQL, args := ungroupedQueries(userID, ungroupedItemsLimit)
	if err := s.Pool.QueryRow(ctx, countSQL, args[:2]...).Scan(&res.Ungrouped.Count); err != nil {
		return err
	}
	rows, err := s.Pool.Query(ctx, itemsSQL, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		it, err := media.ScanMediaRef(rows)
		if err != nil {
			return err
		}
		res.Ungrouped.Items = append(res.Ungrouped.Items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(res.Ungrouped.Items) > ungroupedItemsLimit {
		res.Ungrouped.HasMore = true
		res.Ungrouped.Items = res.Ungrouped.Items[:ungroupedItemsLimit]
	}
	if res.Ungrouped.HasMore && len(res.Ungrouped.Items) > 0 {
		last := res.Ungrouped.Items[len(res.Ungrouped.Items)-1]
		res.Ungrouped.NextCursor = cursor.Encode(last.TakenAt, last.ID)
	}
	return nil
}

// Groups GET /albums/groups → 200 GroupsResult（个人空间按相册分组视图数据源）。
func (h *Handler) Groups(c *gin.Context) {
	res, err := h.Store.Groups(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, res)
}
