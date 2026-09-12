package search

import (
	"context"
	"fmt"
	"strings"
)

// RecallHit 语义召回命中：媒体 ID + 相似度（0~1，越大越相似）。
type RecallHit struct {
	ID         string
	Similarity float64
}

// Recaller 召回管道插槽（Stage 4 语义召回接入点）。
// 返回的命中既并入候选集（WHERE 并集），也参与相关度打分（见 buildWhere 的语义项），
// 否则纯语义命中会因得分为 0 而被按时间排序埋没。
type Recaller interface {
	Recall(ctx context.Context, p SearchParams) ([]RecallHit, error)
}

// SemanticRecaller 未启用语义召回时的占位实现（恒空；保持 Stage 3 行为）。
type SemanticRecaller struct{}

// Recall 占位：无语义召回。
func (SemanticRecaller) Recall(context.Context, SearchParams) ([]RecallHit, error) { return nil, nil }

// semanticScoreWeight 语义相似度在总评分中的权重。
// 取 10 与「文件名完全匹配」同量级：文本强匹配（10~17 分）仍优先，
// 纯语义命中（相似度 0.70~0.85 → 1.5~3 分）排在其后，并按其相似度彼此排序。
const semanticScoreWeight = 10.0

// enStopWords 英文停用词：这些词在自然语言查询里高频出现，
// 若参与文本匹配会以 ILIKE 子串方式宽泛命中 folder_path 等字段
// （例如 "aurora in the night sky" 中的 in/the 把无关项顶到语义命中之前，
//
//	Job000010 实测踩到）。仅对纯 ASCII token 生效，中文词不受影响。
var enStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "in": true, "on": true,
	"at": true, "to": true, "for": true, "with": true, "and": true, "or": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "by": true,
	"from": true, "that": true, "this": true, "it": true, "as": true,
}

// isASCII 判定是否纯 ASCII（中文/日文等一律不当作停用词）。
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// queryTokens 拆出用于**文本匹配**的 token：剔除英文停用词。
// 若剔除后为空（如 q 全是停用词），回退保留原 token 以免检索退化为无条件。
// 注意：语义编码仍使用完整原始查询串，不做剔除（自然语言语义依赖完整上下文）。
func queryTokens(q string) []string {
	all := strings.Fields(q)
	if len(all) == 0 {
		return nil
	}
	kept := make([]string, 0, len(all))
	for _, t := range all {
		if isASCII(t) && enStopWords[strings.ToLower(t)] {
			continue
		}
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		return all
	}
	return kept
}

// trgmRecallThreshold trgm 补充召回相似度阈值（Job000005 裁决值）。
const trgmRecallThreshold = 0.15

// buildWhere 结构化过滤器层：由 SearchParams 组装参数化 WHERE（模式复用 internal/media/timeline.go）。
//   - hits：语义召回命中（ID + 相似度）。既并入候选集，也按相似度参与打分；空则不拼语义项
//   - geo：非 nil 时 place 条件由文本 ILIKE 替换为 ST_DWithin 半径检索（地理降级）
//
// 返回 scoreExpr：q 非空（或存在语义命中）时为相关度评分表达式；否则为空串
// （调用方保持 taken_at 排序原行为）。
// 返回 whereN：WHERE 实际引用的参数个数（args 中可能还含仅被评分表达式引用的参数，
// 例如语义召回的 ids/sims 数组；count(*) 查询必须只传 whereN 个）。
// 返回 textMatchExpr：文本 token 命中的布尔表达式（无 token 时为空串），
// 供调用方标记"仅语义命中"的结果。
func buildWhere(p SearchParams, hits []RecallHit, geo *GeoCenter) (where string, scoreExpr string, textMatchExpr string, args []any, whereN int) {
	conds := []string{"m.deleted_at IS NULL"}
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

	// q 关键词（Job000005）：多 token OR 召回（任一命中即中），单 token 命中条件 =
	// ILIKE 完全子串（文件名/地点/目录/标签）OR trgm similarity >= 0.15（文件名/地点/标签）。
	// 每个 token 只追加一次参数，WHERE 与 scoreExpr 复用同一占位符。
	// 英文停用词在文本匹配中被剔除（queryTokens），避免 in/the 之类宽泛命中目录名。
	var tokConds, tokScores []string
	for _, tok := range queryTokens(p.Q) {
		args = append(args, tok)
		n := len(args)
		tokConds = append(tokConds, fmt.Sprintf(`(m.filename ILIKE '%%%%' || $%[1]d || '%%%%'
			OR m.place ILIKE '%%%%' || $%[1]d || '%%%%'
			OR m.folder_path ILIKE '%%%%' || $%[1]d || '%%%%'
			OR similarity(m.filename, $%[1]d) >= %[2]g
			OR similarity(coalesce(m.place, ''), $%[1]d) >= %[2]g
			OR EXISTS(SELECT 1 FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
				WHERE mt.media_id = m.id AND (t.name ILIKE '%%%%' || $%[1]d || '%%%%'
					OR similarity(t.name, $%[1]d) >= %[2]g)))`, n, trgmRecallThreshold))
		// 评分：ILIKE 完全子串权重最高（文件名 10 / 地点 6 / 目录 4 / 标签 6），
		// trgm 相似度作连续分补充（×2 缩放，与 ILIKE 同量级但严格更低）。
		// ⚠️ place / folder_path 可空：必须先 COALESCE 再做 ILIKE，
		// 否则 NULL::int 会让**整个 score 变 NULL**，在 ORDER BY score DESC 下
		// 因 Postgres 默认 NULLS FIRST 被排到最前（Job000010 实测踩到）。
		tokScores = append(tokScores, fmt.Sprintf(`(10*(m.filename ILIKE '%%%%' || $%[1]d || '%%%%')::int
			+ 6*(COALESCE(m.place,'') ILIKE '%%%%' || $%[1]d || '%%%%')::int
			+ 4*(COALESCE(m.folder_path,'') ILIKE '%%%%' || $%[1]d || '%%%%')::int
			+ 6*(EXISTS(SELECT 1 FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
				WHERE mt.media_id = m.id AND t.name ILIKE '%%%%' || $%[1]d || '%%%%'))::int
			+ 2*similarity(m.filename, $%[1]d)
			+ 2*similarity(coalesce(m.place, ''), $%[1]d)
			+ 2*COALESCE((SELECT max(similarity(t.name, $%[1]d)) FROM media_tags mt
				JOIN tags t ON t.id = mt.tag_id WHERE mt.media_id = m.id), 0))`, n))
	}
	if len(tokConds) > 0 {
		joined := strings.Join(tokConds, "\n\tOR ")
		conds = append(conds, "("+joined+")")
		scoreExpr = strings.Join(tokScores, "\n\t+ ")
		// 文本命中的**布尔**表达式（与 WHERE 中同一组条件），用于标记"仅语义命中"。
		// 不能用 score>0 近似：trgm 相似度在未达阈值时仍可能为非零小值。
		textMatchExpr = "(" + joined + ")"
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
			conds = append(conds, fmt.Sprintf("m.place ILIKE '%%' || $%d || '%%'", len(args)))
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
	if len(hits) > 0 {
		// 语义召回：① 并入候选集（结构化条件 OR 召回命中）；
		// ② 以相似度参与打分——否则纯语义命中得分为 0，会被按时间排序埋没，
		//    出现「最佳语义命中排在几十条之后」的可用性问题（Job000010 实测踩到）。
		ids := make([]string, 0, len(hits))
		sims := make([]float64, 0, len(hits))
		for _, h := range hits {
			ids = append(ids, h.ID)
			sims = append(sims, h.Similarity)
		}
		args = append(args, ids)
		idsArg := len(args)
		args = append(args, sims)
		simsArg := len(args)

		semTerm := fmt.Sprintf(`%g*COALESCE(($%d::float8[])[array_position($%d::uuid[], m.id)], 0)`,
			semanticScoreWeight, simsArg, idsArg)
		if scoreExpr == "" {
			scoreExpr = semTerm
		} else {
			scoreExpr = scoreExpr + "\n\t+ " + semTerm
		}
		base := strings.Join(conds, " AND ")
		// 第 4 个返回值 = WHERE 实际引用的参数个数。
		// 语义打分用的两个数组参数（ids/sims）只被 SELECT/ORDER BY 引用，
		// 而 count(*) 查询只接受 WHERE 参数——不做区分会报 "expected N arguments"。
		return fmt.Sprintf("((%s) OR m.id = ANY($%d::uuid[]))", base, idsArg), scoreExpr, textMatchExpr, args, idsArg
	}
	return strings.Join(conds, " AND "), scoreExpr, textMatchExpr, args, len(args)
}
