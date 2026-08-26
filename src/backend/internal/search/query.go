package search

import (
	"context"
	"fmt"
	"strings"
)

// Recaller 召回管道插槽（Stage 4 语义召回接入点；返回补充召回的媒体 ID，与结构化过滤取并集）。
type Recaller interface {
	Recall(ctx context.Context, p SearchParams) ([]string, error)
}

// SemanticRecaller pgvector 语义召回占位实现（本期不实现，恒返回 nil；Stage 4 替换为真实实现，
// API 契约与调用方代码不变）。
type SemanticRecaller struct{}

// Recall 占位：无语义召回。
func (SemanticRecaller) Recall(context.Context, SearchParams) ([]string, error) { return nil, nil }

// buildWhere 结构化过滤器层：由 SearchParams 组装参数化 WHERE（模式复用 internal/media/timeline.go）。
//   - extraIDs：召回管道补充的媒体 ID（语义召回并集；MVP 恒空）
//   - geo：非 nil 时 place 条件由文本 trgm 替换为 ST_DWithin 半径检索（地理降级）
func buildWhere(p SearchParams, extraIDs []string, geo *GeoCenter) (string, []any) {
	conds := []string{"m.deleted_at IS NULL"}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	// 空间可见性：个人空间仅本人媒体；共享空间限共享空间成员
	// （media 无 space_id 列，成员身份即可见共享媒体，与 timeline.go 注释的并集模型一致）
	args = append(args, p.UserID)
	conds = append(conds, fmt.Sprintf(`((m.space = 'personal' AND m.owner_id = $%[1]d)
		OR (m.space = 'shared' AND EXISTS(
			SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $%[1]d)))`, len(args)))

	// q 关键词：空白分词，逐 token 匹配 文件名/地点（trgm）+ 目录（ILIKE）+ 标签（trgm EXISTS）
	for _, tok := range strings.Fields(p.Q) {
		args = append(args, tok)
		n := len(args)
		conds = append(conds, fmt.Sprintf(`(m.filename %% $%[1]d OR m.place %% $%[1]d
			OR m.folder_path ILIKE '%%%%' || $%[1]d || '%%%%'
			OR EXISTS(SELECT 1 FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
				WHERE mt.media_id = m.id AND t.name %% $%[1]d))`, n))
	}
	if p.Tag != "" {
		add(`EXISTS(SELECT 1 FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
			WHERE mt.media_id = m.id AND t.name ILIKE '%%' || $%d || '%%')`, p.Tag)
	}
	if p.HasAfter {
		add("m.taken_at >= $%d", p.DateAfter)
	}
	if p.HasBefore {
		add("m.taken_at < $%d", p.DateBefore)
	}
	if p.Place != "" {
		if geo != nil {
			// 地理降级：解析坐标后 5km 半径检索（WGS-84）
			args = append(args, geo.Lon, geo.Lat)
			conds = append(conds, fmt.Sprintf(
				"ST_DWithin(m.gps::geography, ST_SetSRID(ST_MakePoint($%d, $%d), 4326)::geography, %d)",
				len(args)-1, len(args), geoRadiusM))
		} else {
			args = append(args, p.Place)
			conds = append(conds, fmt.Sprintf("m.place %% $%d", len(args)))
		}
	}
	switch p.Type {
	case "photo", "video":
		add("m.type = $%d AND m.is_360 = false", p.Type)
	case "360":
		conds = append(conds, "m.is_360 = true")
	}
	if p.Favorites {
		// 收藏模型：favorites 相册成员（与 timeline.go / albums.criteria.go 一致）
		conds = append(conds, `EXISTS(SELECT 1 FROM album_items ai JOIN albums a ON a.id = ai.album_id
			WHERE ai.media_id = m.id AND a.type = 'favorites')`)
	}
	if len(extraIDs) > 0 {
		args = append(args, extraIDs)
		// 语义召回并集：结构化条件 OR 召回命中（Stage 4 生效；MVP extraIDs 恒空不拼接）
		base := strings.Join(conds, " AND ")
		return fmt.Sprintf("((%s) OR m.id = ANY($%d::uuid[]))", base, len(args)), args
	}
	return strings.Join(conds, " AND "), args
}
