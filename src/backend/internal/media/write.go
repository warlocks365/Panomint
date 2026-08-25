package media

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// 媒体写操作：收藏 / 评级 / 软删 / 回收站（API v1.1 §3）。

// ownerOf 查询媒体属主（含回收站中的行）。
func (s *Store) ownerOf(ctx context.Context, id string) (ownerID string, deleted bool, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT owner_id, deleted_at IS NOT NULL FROM media WHERE id = $1`, id).
		Scan(&ownerID, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrNotFound
	}
	return ownerID, deleted, err
}

// SetFavorite 收藏切换：favorites 相册成员模型（DDL 无 is_favorite 字段，与 timeline 过滤一致）。
func (s *Store) SetFavorite(ctx context.Context, id, userID string, fav bool) error {
	// 找到或创建当前用户的收藏相册
	var albumID string
	err := s.Pool.QueryRow(ctx,
		`SELECT id FROM albums WHERE owner_id = $1 AND type = 'favorites' LIMIT 1`, userID).Scan(&albumID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.Pool.QueryRow(ctx,
			`INSERT INTO albums (name, type, owner_id) VALUES ('收藏', 'favorites', $1) RETURNING id`,
			userID).Scan(&albumID)
	}
	if err != nil {
		return err
	}
	if fav {
		_, err = s.Pool.Exec(ctx,
			`INSERT INTO album_items (album_id, media_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			albumID, id)
	} else {
		_, err = s.Pool.Exec(ctx,
			`DELETE FROM album_items WHERE album_id = $1 AND media_id = $2`, albumID, id)
	}
	return err
}

// SetRating 评级（0-5 星）。
func (s *Store) SetRating(ctx context.Context, id string, rating int) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET rating = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, rating, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDelete 软删（入回收站）。
func (s *Store) SoftDelete(ctx context.Context, id string) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore 从回收站恢复。
func (s *Store) Restore(ctx context.Context, id string) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Purge 永久删除（回收站中）；返回媒体 path 供调用方清理文件。
func (s *Store) Purge(ctx context.Context, id string) (string, error) {
	var path string
	err := s.Pool.QueryRow(ctx,
		`DELETE FROM media WHERE id = $1 AND deleted_at IS NOT NULL RETURNING path`, id).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return path, err
}

// ListTrash 回收站列表（按删除时间倒序，上限 500 条）。
func (s *Store) ListTrash(ctx context.Context, ownerID string) (*ListResult, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, m.type::text, m.filename, m.taken_at, m.width, m.height, m.duration,
		       m.codec, m.is_360, m.place, m.rating, m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg
		FROM media m
		WHERE m.deleted_at IS NOT NULL AND m.owner_id = $1
		ORDER BY m.deleted_at DESC LIMIT 500`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &ListResult{Items: []MediaRef{}, Buckets: []Bucket{}}
	for rows.Next() {
		var it MediaRef
		if err := rows.Scan(&it.ID, &it.Type, &it.Filename, &it.TakenAt, &it.Width, &it.Height,
			&it.Duration, &it.Codec, &it.Is360, &it.Place, &it.Rating,
			&it.ThumbnailSM, &it.ThumbnailMD, &it.ThumbnailLG); err != nil {
			return nil, err
		}
		res.Items = append(res.Items, it)
	}
	res.Total = len(res.Items)
	return res, rows.Err()
}
