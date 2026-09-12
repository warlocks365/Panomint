package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
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

// NormalizeTagName 清洗并校验标签名（去首尾空白；1-128 字符）。
func NormalizeTagName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return "", ErrInvalidTagName
	}
	return name, nil
}

// ListTags GET /tags?q=：子串过滤（ILIKE，pg_trgm GIN 索引加速），按使用计数降序。
func (s *Store) ListTags(ctx context.Context, q string) ([]TagRef, error) {
	where := ""
	args := []any{}
	if q = strings.TrimSpace(q); q != "" {
		args = append(args, q)
		where = "WHERE t.name ILIKE '%' || $1 || '%'"
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id, t.name, t.kind, t.color, t.confirmed, count(mt.media_id)::int
		FROM tags t LEFT JOIN media_tags mt ON mt.tag_id = t.id
		`+where+`
		GROUP BY t.id ORDER BY count(mt.media_id) DESC, t.name LIMIT 100`, args...)
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
		ct, err := tx.Exec(ctx, `
			INSERT INTO media_tags (media_id, tag_id, confirmed, confidence, origin)
			SELECT media_id, $2, confirmed, confidence, origin
			FROM media_tags WHERE tag_id = $1
			ON CONFLICT (media_id, tag_id) DO UPDATE
			  SET confirmed = media_tags.confirmed OR EXCLUDED.confirmed,
			      confidence = COALESCE(media_tags.confidence, EXCLUDED.confidence)`,
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

// ConfirmTagForMedia 确认某媒体上的某标签关联；双写 tags.confirmed（粗粒度审阅）。
// 返回是否确有更新。
func (s *Store) ConfirmTagForMedia(ctx context.Context, mediaID, tagID string) (bool, error) {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media_tags SET confirmed = true WHERE media_id = $1 AND tag_id = $2`,
		mediaID, tagID)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE tags SET confirmed = true WHERE id = $1`, tagID); err != nil {
		return false, err
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

// MarkTagReviewed 将标签名级「已审阅」标记置真（双写）；返回是否更新。
func (s *Store) MarkTagReviewed(ctx context.Context, id string) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `UPDATE tags SET confirmed = true WHERE id = $1 AND confirmed = false`, id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// ListMediaByTag 按标签分页浏览媒体（仅已确认关联）；复合游标风格与时间轴一致。
func (s *Store) ListMediaByTag(ctx context.Context, tagID, cursor string, limit int) (*ListResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where := "m.deleted_at IS NULL AND mt.tag_id = $1 AND mt.confirmed = true"
	args := []any{tagID}
	if cursor != "" {
		t, id, err := decodeCursor(cursor)
		if err != nil {
			return nil, errors.New("无效游标")
		}
		args = append(args, t, id)
		where += fmt.Sprintf(" AND (m.taken_at, m.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	var total int
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM media m JOIN media_tags mt ON mt.media_id = m.id WHERE `+where,
		args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit+1)
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,
		       m.codec, m.is_360, m.place, m.rating, m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg
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
		var it MediaRef
		if err := rows.Scan(&it.ID, &it.Type, &it.Filename, &it.FolderPath, &it.TakenAt, &it.Width, &it.Height,
			&it.Duration, &it.Codec, &it.Is360, &it.Place, &it.Rating,
			&it.ThumbnailSM, &it.ThumbnailMD, &it.ThumbnailLG); err != nil {
			return nil, err
		}
		res.Items = append(res.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(res.Items) > limit {
		last := res.Items[limit-1]
		res.NextCursor = encodeCursor(last.TakenAt, last.ID)
		res.Items = res.Items[:limit]
	}
	return res, nil
}
