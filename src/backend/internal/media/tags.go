package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	// ⚠️ 取别名：本文件里有名为 cursor 的局部变量（分页游标字符串），
	// 直接 import 同名包会被变量遮蔽，调用点会编译不过。
	cursorcodec "panoalbum/internal/cursor"
)

// 手工标签（Job000005）：GET /tags 自动补全 + POST/DELETE /media/:id/tags 关联管理。
// tags 表全局无 owner，kind=user|ai，UNIQUE(name,kind)；media_tags 主键 (media_id,tag_id)。

// TagRef 标签引用（含使用计数，供前端自动补全）。
type TagRef struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Color      *string `json:"color,omitempty"`
	Confirmed  bool    `json:"confirmed"` // 粗粒度「已审阅」（AI 标签待确认为 false）
	UsageCount int     `json:"usage_count"`
}

// ErrInvalidTagName 标签名非法（空或超长）。
var ErrInvalidTagName = errors.New("标签名需为 1-128 字符")

// ErrMergeTargetNotFound 合并目标标签不存在。提前校验以替代 PG 外键报错，
// 避免把 `SQLSTATE 23503` 之类的数据库原文透给调用方。
var ErrMergeTargetNotFound = errors.New("目标标签不存在")

// GET /tags 列表数量上限：库内标签数已超过旧的硬编码 100，截断会使
// 使用次数较低的手工标签在页面上完全不可见。缺省与上限均为 500（可用 limit 参数下调）。
const (
	defaultTagListLimit = 500
	maxTagListLimit     = 500
)

// NormalizeTagName 清洗并校验标签名（去首尾空白；1-128 字符）。
func NormalizeTagName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return "", ErrInvalidTagName
	}
	return name, nil
}

// ListTags GET /tags?q=&kind=&limit=：子串过滤（ILIKE，pg_trgm GIN 索引加速）。
// kind 可选（user|ai）；limit<=0 时取 defaultTagListLimit，超过 maxTagListLimit 时收敛到上限。
//
// 排序语义：kind='user'（用户手工创建）恒排在 AI 标签之前，组内再按使用次数降序、名称升序。
// 理由：手工标签是用户显式创建的信息资产，使用次数天然低于批量生成的 AI 标签；
// 仅按 usage_count 排序会被 AI 标签整体挤出分页窗口，导致「建了却看不见」。
func (s *Store) ListTags(ctx context.Context, q, kind string, limit int) ([]TagRef, error) {
	if limit <= 0 {
		limit = defaultTagListLimit
	}
	if limit > maxTagListLimit {
		limit = maxTagListLimit
	}
	args := []any{}
	conds := []string{}
	if q = strings.TrimSpace(q); q != "" {
		args = append(args, q)
		conds = append(conds, fmt.Sprintf("t.name ILIKE '%%' || $%d || '%%'", len(args)))
	}
	if kind != "" {
		args = append(args, kind)
		conds = append(conds, fmt.Sprintf("t.kind = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.name, t.kind, t.color, t.confirmed, count(mt.media_id)::int
		FROM tags t LEFT JOIN media_tags mt ON mt.tag_id = t.id
		`+where+`
		GROUP BY t.id ORDER BY (t.kind = 'user') DESC, count(mt.media_id) DESC, t.name
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []TagRef{}
	for rows.Next() {
		var t TagRef
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Color, &t.Confirmed, &t.UsageCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// FindOrCreateUserTag 幂等取 user 标签：不存在则创建（UNIQUE(name,kind) 冲突时返回既有行）。
func (s *Store) FindOrCreateUserTag(ctx context.Context, name string) (*TagRef, error) {
	var t TagRef
	// DO UPDATE 空操作保证并发冲突下仍 RETURNING 既有行 id
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO tags (name, kind) VALUES ($1, 'user')
		ON CONFLICT (name, kind) DO UPDATE SET name = EXCLUDED.name
		RETURNING id, name, kind, color`, name).
		Scan(&t.ID, &t.Name, &t.Kind, &t.Color)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// AttachTag 关联媒体与标签（幂等：已关联不报错）。
// 手动关联记 origin='user' 并直接确认；若此前存在待确认的 AI 关联，人工添加即视为确认。
func (s *Store) AttachTag(ctx context.Context, mediaID, tagID string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO media_tags (media_id, tag_id, confirmed, origin) VALUES ($1, $2, true, 'user')
		 ON CONFLICT (media_id, tag_id) DO UPDATE SET confirmed = true, origin = 'user'`,
		mediaID, tagID)
	return err
}

// DetachTag 解除媒体标签关联；返回是否确有解除（未关联时 false，调用方据此 404 或幂等 200）。
func (s *Store) DetachTag(ctx context.Context, mediaID, tagID string) (bool, error) {
	ct, err := s.Pool.Exec(ctx,
		`DELETE FROM media_tags WHERE media_id = $1 AND tag_id = $2`, mediaID, tagID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// tagExists 校验标签存在（DELETE 路径区分 404 语义）。
func (s *Store) tagExists(ctx context.Context, tagID string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tags WHERE id = $1)`, tagID).Scan(&ok)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	return ok, nil
}

// ---- Phase 4：标签管理 + AI 确认 ----

// GetTag 读取单个标签（含使用计数）。
func (s *Store) GetTag(ctx context.Context, id string) (*TagRef, error) {
	var t TagRef
	err := s.Pool.QueryRow(ctx, `
		SELECT t.id, t.name, t.kind, t.color, t.confirmed,
		       (SELECT count(*)::int FROM media_tags mt WHERE mt.tag_id = t.id)
		FROM tags t WHERE t.id = $1`, id).
		Scan(&t.ID, &t.Name, &t.Kind, &t.Color, &t.Confirmed, &t.UsageCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTag 显式创建标签（kind 默认 user）；同名同 kind 已存在时返回既有行。
func (s *Store) CreateTag(ctx context.Context, name, kind string, color *string) (*TagRef, error) {
	if kind == "" {
		kind = "user"
	}
	var t TagRef
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO tags (name, kind, color) VALUES ($1, $2, $3)
		ON CONFLICT (name, kind) DO UPDATE SET color = COALESCE(EXCLUDED.color, tags.color)
		RETURNING id, name, kind, color, confirmed,
		          (SELECT count(*)::int FROM media_tags mt WHERE mt.tag_id = tags.id)`,
		name, kind, color).
		Scan(&t.ID, &t.Name, &t.Kind, &t.Color, &t.Confirmed, &t.UsageCount)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTag 改名/改色；name/color 为 nil 表示不改动该字段。
func (s *Store) UpdateTag(ctx context.Context, id string, name, color *string) (*TagRef, error) {
	var t TagRef
	err := s.Pool.QueryRow(ctx, `
		UPDATE tags SET name = COALESCE($2, name), color = COALESCE($3, color)
		WHERE id = $1
		RETURNING id, name, kind, color, confirmed,
		          (SELECT count(*)::int FROM media_tags mt WHERE mt.tag_id = tags.id)`,
		id, name, color).
		Scan(&t.ID, &t.Name, &t.Kind, &t.Color, &t.Confirmed, &t.UsageCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteTagOrMerge 删除标签。intoID 非空时先把全部关联迁到目标标签再删除源标签；
// 返回迁移或删除受影响的关联条数。整个操作为单事务。
func (s *Store) DeleteTagOrMerge(ctx context.Context, id, intoID string) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // 已提交时为无操作

	moved := 0
	if intoID != "" {
		if intoID == id {
			return 0, errors.New("不能合并到自身")
		}
		// 先校验目标标签存在：否则下面的 INSERT 会撞 media_tags.tag_id 外键，
		// 把 SQLSTATE 23503 原文透给调用方（应统一为 404 语义）。
		var targetExists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM tags WHERE id = $1)`, intoID).Scan(&targetExists); err != nil {
			return 0, err
		}
		if !targetExists {
			return 0, ErrMergeTargetNotFound
		}
		// 置信度取 latest-wins：来源（EXCLUDED）优先，仅当来源为 NULL 时保留目标旧值。
		// 之前用 COALESCE(旧, 新) 会让存量置信度永远刷不新（阈值调整后无法重算）。
		// confirmed 保持 OR 语义，用户确认状态永不被覆盖。
		ct, err := tx.Exec(ctx, `
			INSERT INTO media_tags (media_id, tag_id, confirmed, confidence, origin)
			SELECT media_id, $2, confirmed, confidence, origin
			FROM media_tags WHERE tag_id = $1
			ON CONFLICT (media_id, tag_id) DO UPDATE
			  SET confirmed = media_tags.confirmed OR EXCLUDED.confirmed,
			      confidence = COALESCE(EXCLUDED.confidence, media_tags.confidence)`,
			id, intoID)
		if err != nil {
			return 0, err
		}
		moved = int(ct.RowsAffected())
	}
	ct, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, id)
	if err != nil {
		return 0, err
	}
	if ct.RowsAffected() == 0 {
		return 0, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if moved == 0 && intoID == "" {
		return 0, nil
	}
	return moved, nil
}

// ConfirmTagForMedia 设置某媒体上的某标签关联的确认状态，返回是否确有更新。
//
// confirmed=true 时双写 tags.confirmed（粗粒度「已审阅」）；
// confirmed=false 时只改关联级 media_tags.confirmed，不回退 tags.confirmed——
// 标签级 confirmed 是「该标签曾被人工审阅过」的粗粒度历史标记，
// 取消单张图的确认不应连带撤销该标签在其他媒体上的审阅状态。
// 传入值即落库值，调用方回显不等于本次请求值。
func (s *Store) ConfirmTagForMedia(ctx context.Context, mediaID, tagID string, confirmed bool) (bool, error) {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media_tags SET confirmed = $3 WHERE media_id = $1 AND tag_id = $2`,
		mediaID, tagID, confirmed)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, nil
	}
	if confirmed {
		if _, err := s.Pool.Exec(ctx, `UPDATE tags SET confirmed = true WHERE id = $1`, tagID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// ConfirmAllForMedia 批量确认某媒体全部待确认关联（逐图批量接受）；返回确认条数。
func (s *Store) ConfirmAllForMedia(ctx context.Context, mediaID string) (int, error) {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media_tags SET confirmed = true WHERE media_id = $1 AND confirmed = false`, mediaID)
	if err != nil {
		return 0, err
	}
	n := int(ct.RowsAffected())
	if n > 0 {
		if _, err := s.Pool.Exec(ctx, `
			UPDATE tags SET confirmed = true
			WHERE id IN (SELECT tag_id FROM media_tags WHERE media_id = $1)`, mediaID); err != nil {
			return n, err
		}
	}
	return n, nil
}

// SetTagReviewed 设置标签名级「已审阅」标记（双向：confirmed 可置真也可置假）；返回是否确有更新。
// 原先只能置真，导致「取消确认」在标签级路径上无声失败。
func (s *Store) SetTagReviewed(ctx context.Context, id string, confirmed bool) (bool, error) {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE tags SET confirmed = $2 WHERE id = $1 AND confirmed <> $2`, id, confirmed)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// buildTagMediaWhere 组装 GET /tags/:id/media 的 WHERE 与参数。
//
// 可见性：tags 表**全局无 owner**（见文件头注释），所以不能按标签归属过滤，
// 必须按 **media 的可见性**过滤 —— 谓词直接复用 scopeConds（scope.go 的唯一真源），
// 与 GET /media 完全同口径：space 缺省 = personal；枚举外取值在 handler 层 400；
// 零值/未解析作用域收敛为恒假（fail-closed），后果是**空列表**而不是全库。
//
// 修复前这里只有 `deleted_at + tag_id + confirmed`，**没有任何可见性条件**，
// 实测 viewer 账号能拿到 owner 的媒体（2025-07-教会山-010.jpg）。
//
// ⚠️ 调用约定（正确性的一部分，不是风格问题）：作用域 args **必须最先追加**，
// 因为 scopeConds 的占位符从 $1 起编号；tag/cursor 条件一律按 len(args) 续编。
// 编号错了只会在**运行期**报 `expected N arguments`，编译期与静态阅读都看不出来。
func buildTagMediaWhere(scope MediaScope, tagID, cursor string) (string, []any, error) {
	conds := []string{"m.deleted_at IS NULL"}
	args := []any{}
	scopeWhere, scopeArgs := scopeConds(scope)
	args = append(args, scopeArgs...)
	conds = append(conds, scopeWhere...)

	args = append(args, tagID)
	conds = append(conds, fmt.Sprintf("mt.tag_id = $%d", len(args)))
	conds = append(conds, "mt.confirmed = true")

	if cursor != "" {
		t, id, err := cursorcodec.Decode(cursor)
		if err != nil {
			return "", nil, errors.New("无效游标")
		}
		args = append(args, t, id)
		conds = append(conds, fmt.Sprintf("(m.taken_at, m.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	return strings.Join(conds, " AND "), args, nil
}

// ListMediaByTag 按标签分页浏览媒体（仅已确认关联）；复合游标风格与时间轴一致。
// 可见性口径见 buildTagMediaWhere：scope 由 handler 解析后传入，零值（未解析）即空结果。
func (s *Store) ListMediaByTag(ctx context.Context, scope MediaScope, tagID, cursor string, limit int) (*ListResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where, args, err := buildTagMediaWhere(scope, tagID, cursor)
	if err != nil {
		return nil, err
	}
	var total int
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM media m JOIN media_tags mt ON mt.media_id = m.id WHERE `+where,
		args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit+1)
	rows, err := s.Pool.Query(ctx, `
		SELECT `+MediaRefColumns+`
		FROM media m JOIN media_tags mt ON mt.media_id = m.id
		WHERE `+where+`
		ORDER BY m.taken_at DESC, m.id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &ListResult{Items: []MediaRef{}, Buckets: []Bucket{}, Total: total}
	for rows.Next() {
		it, err := ScanMediaRef(rows)
		if err != nil {
			return nil, err
		}
		res.Items = append(res.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(res.Items) > limit {
		last := res.Items[limit-1]
		res.NextCursor = cursorcodec.Encode(last.TakenAt, last.ID)
		res.Items = res.Items[:limit]
	}
	return res, nil
}
