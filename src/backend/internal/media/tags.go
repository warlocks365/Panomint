package media

import (
	"context"
	"errors"
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
		SELECT t.id, t.name, t.kind, t.color, count(mt.media_id)::int
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
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Color, &t.UsageCount); err != nil {
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
func (s *Store) AttachTag(ctx context.Context, mediaID, tagID string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO media_tags (media_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
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
