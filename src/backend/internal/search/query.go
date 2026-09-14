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

// personNameScoreWeight 人物姓名命中的评分权重。
//
// 必须严格 > semanticScoreWeight（10），推导：
//   - 语义项得分 = semanticScoreWeight × 余弦相似度，相似度 ∈ [0,1] → 语义项上界恰为 10；
//   - CLIP 文本塔只能表达"这是人像"这一层级，无法区分具体人名，人像类向量彼此高度相近
//     （实测 einstein-1 与 lincoln-1 相似度 0.759、与极光风景照 0.574，均高于 1-0.63=0.37 的召回门槛）；
//   - 若人物名得分 ≤ 10，"搜爱因斯坦"时林肯等其他人像会以同分或更高分挤到同一位次，
//     出现"结果混进林肯"的错配（本次修复的原始缺陷）。
//
// 取 15：高于语义上界 10，又低于"文件名命中(10) + 地点(6) + 目录(4)"这类多字段叠加，
// 保证人物名命中必然压过任何纯语义命中，同时不喧宾夺主。
const personNameScoreWeight = 15.0

// personNameMatch 生成「媒体 m 存在某个已命名人物，其姓名命中 token $n」的布尔 SQL。
//
// 人物名是**文本事实**（用户显式命名），不是语义近似，因此它同时用于：
//   - 文本召回条件（tokConds）：让"搜人名"能命中该人物的照片；
//   - 相关度评分（tokScores）：以 personNameScoreWeight 计分，压过纯语义命中。
//
// 姓名比较前先 COALESCE 兜底：people.name 在 DDL 中可空，不兜会让 ILIKE 结果为 NULL。
func personNameMatch(n int) string {
	return fmt.Sprintf(`EXISTS(SELECT 1 FROM faces f JOIN people p ON p.id = f.person_id
				WHERE f.media_id = m.id AND COALESCE(p.name, '') <> ''
					AND (p.name ILIKE '%%' || $%[1]d || '%%' OR similarity(p.name, $%[1]d) >= %[2]g))`,
		n, trgmRecallThreshold)
}

// personIntentGate 生成「库内是否存在姓名命中 token $n 的人物」的布尔 SQL（与具体媒体行无关，
// 对同一查询是不相关子查询，Postgres 只会求值一次）。
//
// 用途见 buildWhere 的语义并集：语义召回的粒度是"人像"，一旦查询命中了某些已命名人物，
// 其他人物的人像就纯属语义噪声（搜"爱因斯坦"召回林肯），需要整体抑制。
// personGateMinSim 人物意图闸门的相似度下限。
//
// ⚠️ 必须比 trgmRecallThreshold(0.15) 严得多：闸门一旦命中就会**整体抑制语义并集**，
// 而实测 similarity('雪','雪景')=0.25、similarity('林','林肯')=0.25 —— 若沿用 0.15，
// 一个名字叫「雪」或「小林」的人物会让 q=雪景 的语义召回被全部杀掉（把语义分支改死）。
// 取 0.45：只认「姓名与查询 token 高度重合」的情形（姓名"爱因斯坦" vs token "爱因斯坦" 相似度 1.0）。
const personGateMinSim = 0.45

// personGateMinNameLen 闸门要求姓名至少这么长：单字姓名极易与常见查询词误撞（见上）。
const personGateMinNameLen = 2

// personIntentGate 生成「库内是否存在姓名命中 token $n 的人物」的布尔 SQL（与具体媒体行无关，
// 对同一查询是不相关子查询，Postgres 只会求值一次）。
//
// 用途见 buildWhere 的语义并集：语义召回的粒度是"人像"，一旦查询命中了某些已命名人物，
// 其他人物的人像就纯属语义噪声（搜"爱因斯坦"召回林肯），需要整体抑制。
func personIntentGate(n int) string {
	return fmt.Sprintf(`EXISTS(SELECT 1 FROM people pg
			WHERE COALESCE(pg.name, '') <> ''
				AND char_length(pg.name) >= %[3]d
				AND (pg.name ILIKE '%%' || $%[1]d || '%%' OR similarity(pg.name, $%[1]d) >= %[2]g))`,
		n, personGateMinSim, personGateMinNameLen)
}

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

// isUUID 判定字符串是否为 UUID 文本（36 位、4 个连字符、其余为十六进制）。
// 用于区分 person 参数是"人物 ID"还是"人物姓名"——只有前者能安全地做 `$n::uuid` 转换，
// 否则 Postgres 会因非法 UUID 文本直接报错（22P02）。
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
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
//   - q 的每个 token 同时匹配**人物姓名**（people.name）；命中任一人物名时语义并集被闸门抑制
//   - p.Person（§8 person 参数）：UUID → 按 faces.person_id 过滤；非 UUID → 按人物姓名模糊匹配
//
// 返回 scoreExpr：q 非空（或存在语义命中）时为相关度评分表达式；否则为空串
// （调用方保持 taken_at 排序原行为）。
// 返回 whereN：WHERE 实际引用的参数个数（args 中可能还含仅被评分表达式引用的参数，
// 例如语义召回的 ids/sims 数组；count(*) 查询必须只传 whereN 个）。
// 返回 textMatchExpr：文本 token 命中的布尔表达式（无 token 时为空串），
// 供调用方标记"仅语义命中"的结果。
func buildWhere(p SearchParams, hits []RecallHit, geo *GeoCenter) (where string, scoreExpr string, textMatchExpr string, args []any, whereN int) {
	deletedCond := "m.deleted_at IS NULL"
	conds := []string{deletedCond}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	// 空间可见性：个人空间仅本人媒体；共享空间限共享空间成员
	// （media 无 space_id 列，成员身份即可见共享媒体，与 timeline.go 注释的并集模型一致）
	//
	// ⚠️ 具名保存并**在语义并集分支复用**：并集若是裸的 `m.id = ANY(...)`，
	// 就会绕过授权与软删——实测「按可见性条件可见媒体数为 0 的用户」仍能搜到他人个人空间的照片。
	// 这里把可见性抽成变量，就是为了让并集分支无法"忘记"带上它。
	args = append(args, p.UserID)
	visibleCond := fmt.Sprintf(`((m.space = 'personal' AND m.owner_id = $%[1]d)
		OR (m.space = 'shared' AND EXISTS(
			SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $%[1]d)))`, len(args))
	conds = append(conds, visibleCond)

	// q 关键词（Job000005）：多 token OR 召回（任一命中即中），单 token 命中条件 =
	// ILIKE 完全子串（文件名/地点/目录/标签/人物姓名）OR trgm similarity >= 0.15（同上）。
	// 人物姓名（people.name）纳入同一关键词通路：用户在搜索框里直接敲人名即可命中该人物的照片
	// （此前人物维度完全不参与检索，导致"搜爱因斯坦"只剩语义召回，混入同为人像的其他人）。
	// 每个 token 只追加一次参数，WHERE 与 scoreExpr 复用同一占位符。
	// 英文停用词在文本匹配中被剔除（queryTokens），避免 in/the 之类宽泛命中目录名。
	// ⚠️ filename / place / folder_path 三个列在 DDL 中**均可空**
	// （media.filename VARCHAR(512) 无 NOT NULL，见 migrations/00004_ddl_part.sql），
	// 这里必须全部先 COALESCE 再 ILIKE：这段条件同时充当 textMatchExpr——它现在是**排序键**
	// （text_matched DESC），任一裸列比较为 NULL 都会让整组 OR 求值为 NULL，被
	// Postgres 的 DESC 默认 NULLS FIRST 排到最前，恰好把"仅语义"结果顶到文本命中之前
	// （实测回归场景踩到）。游标行比较同样无法表达 NULLS LAST，只能从源头保证非 NULL。
	var tokConds, tokScores, personGates []string
	for _, tok := range queryTokens(p.Q) {
		args = append(args, tok)
		n := len(args)
		personHit := personNameMatch(n)
		// 该 token 是否命中"库内某个人物名"（查询级判断，用于语义闸门；见下方 hits 分支）
		personGates = append(personGates, personIntentGate(n))
		tokConds = append(tokConds, fmt.Sprintf(`(COALESCE(m.filename,'') ILIKE '%%%%' || $%[1]d || '%%%%'
			OR COALESCE(m.place,'') ILIKE '%%%%' || $%[1]d || '%%%%'
			OR COALESCE(m.folder_path,'') ILIKE '%%%%' || $%[1]d || '%%%%'
			OR similarity(COALESCE(m.filename,''), $%[1]d) >= %[2]g
			OR similarity(coalesce(m.place, ''), $%[1]d) >= %[2]g
			OR EXISTS(SELECT 1 FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
				WHERE mt.media_id = m.id AND (t.name ILIKE '%%%%' || $%[1]d || '%%%%'
					OR similarity(t.name, $%[1]d) >= %[2]g))
			OR %[3]s)`, n, trgmRecallThreshold, personHit))
		// 评分：ILIKE 完全子串权重最高（文件名 10 / 地点 6 / 目录 4 / 标签 6），
		// 人物姓名 15（必须高于 semanticScoreWeight，见其注释），
		// trgm 相似度作连续分补充（×2 缩放，与 ILIKE 同量级但严格更低）。
		// ⚠️ filename / place / folder_path 可空：必须先 COALESCE 再做 ILIKE，
		// 否则 NULL::int 会让**整个 score 变 NULL**，在 ORDER BY score DESC 下
		// 因 Postgres 默认 NULLS FIRST 被排到最前（Job000010 实测踩到）。
		tokScores = append(tokScores, fmt.Sprintf(`(10*(COALESCE(m.filename,'') ILIKE '%%%%' || $%[1]d || '%%%%')::int
			+ 6*(COALESCE(m.place,'') ILIKE '%%%%' || $%[1]d || '%%%%')::int
			+ 4*(COALESCE(m.folder_path,'') ILIKE '%%%%' || $%[1]d || '%%%%')::int
			+ 6*(EXISTS(SELECT 1 FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
				WHERE mt.media_id = m.id AND t.name ILIKE '%%%%' || $%[1]d || '%%%%'))::int
			+ %[2]g*(%[3]s)::int
			+ 2*similarity(COALESCE(m.filename,''), $%[1]d)
			+ 2*similarity(coalesce(m.place, ''), $%[1]d)
			+ 2*COALESCE((SELECT max(similarity(t.name, $%[1]d)) FROM media_tags mt
				JOIN tags t ON t.id = mt.tag_id WHERE mt.media_id = m.id), 0))`,
			n, personNameScoreWeight, personHit))
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
	if p.Person != "" {
		// §8 person 参数：契约与前端（PeopleView → /search?person=<people.id>）传的是人物 UUID。
		// 非 UUID 时按人物姓名模糊匹配兜底（搜索框直敲人名只需 q，此分支用于兼容按名传参）。
		// people / faces 为空时 EXISTS 恒为 false，不会报错。
		if isUUID(p.Person) {
			add(`EXISTS(SELECT 1 FROM faces f WHERE f.media_id = m.id AND f.person_id = $%d::uuid)`, p.Person)
		} else {
			args = append(args, p.Person)
			conds = append(conds, fmt.Sprintf(`EXISTS(SELECT 1 FROM faces f JOIN people pn ON pn.id = f.person_id
				WHERE f.media_id = m.id AND COALESCE(pn.name, '') <> ''
					AND (pn.name ILIKE '%%' || $%[1]d || '%%' OR similarity(pn.name, $%[1]d) >= %[2]g))`,
				len(args), trgmRecallThreshold))
		}
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
		// 语义闸门（人物意图）：查询命中库内任一人物的姓名时，视为"找人"，把**仅语义**命中的结果
		// 整体剔除——语义只能表达"这是人像"，无法区分具体人名，其他人的人像纯属噪声。
		// 注意闸门只与语义并集 AND（`id = ANY(...) AND NOT(人物意图)`），不作用于 base：
		// 否则"爱因斯坦 夜景"里靠文本命中的夜景也会被一并杀掉。
		gate := "TRUE"
		if len(personGates) > 0 {
			gate = "NOT (" + strings.Join(personGates, "\n\t\t\tOR ") + ")"
		}
		// 第 4 个返回值 = WHERE 实际引用的参数个数。
		// 语义打分用的两个数组参数（ids/sims）只被 SELECT/ORDER BY 引用，
		// 而 count(*) 查询只接受 WHERE 参数——不做区分会报 "expected N arguments"。
		// （闸门只复用已计入 whereN 的 token 参数，不引入新参数。）
		//
		// ⚠️ 并集分支必须同时带上 deletedCond 与 visibleCond：
		// 语义召回来自 embed.Store.SearchByVector，那条查询只过滤 deleted_at/embedding，
		// **不做 owner/space 限定**，即 TopK 可命中全库任何人的媒体。
		// 若并集只写 `m.id = ANY(...)`，授权与软删就被完全绕过（历史缺陷）。
		// 复用同名占位符不新增参数，whereN 仍为 idsArg。
		return fmt.Sprintf("((%s) OR (%s AND %s AND m.id = ANY($%d::uuid[]) AND %s))",
				base, deletedCond, visibleCond, idsArg, gate),
			scoreExpr, textMatchExpr, args, idsArg
	}
	return strings.Join(conds, " AND "), scoreExpr, textMatchExpr, args, len(args)
}
