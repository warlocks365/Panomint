package media

import (
	"bytes"
	"context"
	"encoding/json"
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

// SetNotes 更新用户备注（仅 notes 字段；空串即清空）。
func (s *Store) SetNotes(ctx context.Context, id string, notes string) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET notes = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, notes, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CropRect 归一化裁剪框（相对原图，0..1）。
type CropRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Edits 非破坏式基本编辑参数（存 media.edits JSONB；原文件不变）。
// Rotate ∈ {0,90,180,270}；Crop 为 nil 表示未裁剪。
type Edits struct {
	Rotate int       `json:"rotate"`
	Crop   *CropRect `json:"crop,omitempty"`
}

// ErrBadEdits 编辑参数非法。
var ErrBadEdits = errors.New("编辑参数非法")

// NormalizeEdits 校验并规范化编辑参数（拒绝未知字段）。
// rotate 需为 0/90/180/270；crop 归一化且不得越界。
func NormalizeEdits(raw []byte) (*Edits, error) {
	var e Edits
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return nil, ErrBadEdits
	}
	switch e.Rotate {
	case 0, 90, 180, 270:
	default:
		return nil, ErrBadEdits
	}
	if e.Crop != nil {
		c := e.Crop
		if c.W <= 0 || c.H <= 0 || c.X < 0 || c.Y < 0 {
			return nil, ErrBadEdits
		}
		if c.X+c.W > 1.0001 || c.Y+c.H > 1.0001 {
			return nil, ErrBadEdits
		}
		// 规整到 [0,1]，消除浮点边界误差
		if c.X < 0 {
			c.X = 0
		}
		if c.Y < 0 {
			c.Y = 0
		}
		if c.X+c.W > 1 {
			c.W = 1 - c.X
		}
		if c.Y+c.H > 1 {
			c.H = 1 - c.Y
		}
	}
	return &e, nil
}

// SetEdits 写入非破坏式编辑参数（JSONB）；edits 为 nil 即清空（重置）。
func (s *Store) SetEdits(ctx context.Context, id string, edits *Edits) error {
	var raw any
	if edits != nil {
		b, err := json.Marshal(edits)
		if err != nil {
			return err
		}
		raw = b
	}
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET edits = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, raw, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetEdits 读取媒体当前的编辑参数。无编辑参数返回 (nil, nil)；媒体不存在/已删除返回 ErrNotFound。
// 列内容无法反序列化时按"无编辑"处理——脏数据不应让旋转接口整体 500。
func (s *Store) GetEdits(ctx context.Context, id string) (*Edits, error) {
	var raw []byte
	err := s.Pool.QueryRow(ctx,
		`SELECT edits FROM media WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var e Edits
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, nil
	}
	return &e, nil
}

// MergeRotateEdit 读-改-写合并：只更新 rotate，保留既有 crop。
// POST /media/:id/rotate 的 op=rotate 走此路径，避免整体覆盖抹掉用户的裁剪框。
// 纯函数，不触库（便于单测）。
func MergeRotateEdit(cur *Edits, angle int) *Edits {
	out := &Edits{Rotate: angle}
	if cur != nil {
		out.Crop = cur.Crop
	}
	return out
}

// MergeCropEdit 读-改-写合并：只更新 crop，保留既有 rotate。
// op=crop 走此路径，避免把旋转角度重置为 0。纯函数，不触库（便于单测）。
func MergeCropEdit(cur *Edits, crop *CropRect) *Edits {
	out := &Edits{Crop: crop}
	if cur != nil {
		out.Rotate = cur.Rotate
	}
	return out
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
