package albums

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/media"
)

// Store 相册数据访问。
type Store struct {
	Pool *pgxpool.Pool
}

var (
	ErrNotFound      = errors.New("相册不存在")
	ErrCommentNoRows = errors.New("评论不存在")
	ErrForbidden     = errors.New("无权操作该相册")
	ErrFavoritesLock = errors.New("收藏相册禁止删除")
	ErrSmartReadOnly = errors.New("智能相册禁止手动管理条目")
	ErrThirdLevel    = errors.New("仅支持两级评论")
	ErrParentMissing = errors.New("父评论不存在")
)

// Summary GET /albums 列表项。
type Summary struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	Description     string    `json:"description"`
	CoverMediaID    *string   `json:"cover_media_id"`
	CoverMediaThumb *string   `json:"cover_media_thumb,omitempty"`
	MediaCount      int       `json:"media_count"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Detail GET /albums/:id 响应。
type Detail struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Description  string           `json:"description"`
	CoverMediaID *string          `json:"cover_media_id"`
	Criteria     *Criteria        `json:"criteria"`
	Items        []media.MediaRef `json:"items"`
	OwnerID      string           `json:"-"`
	Type         string           `json:"-"`
}

// Comment 相册评论（扁平返回）。
type Comment struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	ParentID  *string   `json:"parent_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// mediaCols MediaRef 查询列（与 internal/media/timeline.go 保持一致）。
const mediaCols = `m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,
	m.codec, m.is_360, m.place, m.rating, m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg`

// scanMediaRef 扫描一行媒体为 MediaRef。
func scanMediaRef(row pgx.Row) (*media.MediaRef, error) {
	var it media.MediaRef
	err := row.Scan(&it.ID, &it.Type, &it.Filename, &it.FolderPath, &it.TakenAt, &it.Width, &it.Height,
		&it.Duration, &it.Codec, &it.Is360, &it.Place, &it.Rating,
		&it.ThumbnailSM, &it.ThumbnailMD, &it.ThumbnailLG)
	return &it, err
}

// scanMediaRows 扫描多行媒体。
func scanMediaRows(rows pgx.Rows) ([]media.MediaRef, error) {
	defer rows.Close()
	out := []media.MediaRef{}
	for rows.Next() {
		it, err := scanMediaRef(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// Create 新建相册，返回 id。
func (s *Store) Create(ctx context.Context, ownerID, name, description, kind string, coverMediaID *string, c *Criteria) (string, error) {
	var queryJSON []byte
	if c != nil {
		b, err := json.Marshal(c)
		if err != nil {
			return "", err
		}
		queryJSON = b
	}
	var id string
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO albums (name, description, type, owner_id, cover_media_id, query)
		VALUES ($1, NULLIF($2,''), $3, $4, $5, $6)
		RETURNING id`,
		name, description, kindToType(kind), ownerID, coverMediaID, queryJSON).Scan(&id)
	return id, err
}

// getAlbumMeta 读取相册元信息（含 owner/type，供权限判定）。
func (s *Store) getAlbumMeta(ctx context.Context, id string) (ownerID, typ string, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT owner_id, type FROM albums WHERE id = $1`, id).Scan(&ownerID, &typ)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// List 相册列表：本人相册 + favorites 相册（如存在）；含首图回填与媒体计数。
func (s *Store) List(ctx context.Context, userID string) ([]Summary, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT a.id, a.name, a.type, COALESCE(a.description,''), a.cover_media_id, a.updated_at
		FROM albums a
		WHERE a.owner_id = $1 OR a.type = 'favorites'
		ORDER BY a.type = 'favorites' DESC, a.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawAlbum struct {
		Summary
		typ string
	}
	var raws []rawAlbum
	for rows.Next() {
		var r rawAlbum
		if err := rows.Scan(&r.ID, &r.Name, &r.typ, &r.Description, &r.CoverMediaID, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Kind = typeToKind(r.typ)
		raws = append(raws, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Summary, 0, len(raws))
	for _, r := range raws {
		sum := r.Summary
		var firstID *string
		if r.typ == "smart" {
			// 智能相册：条件实时计数 + 取首项（与详情同序 taken_at DESC）
			c, err := s.getCriteria(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			where, args := buildCriteriaWhere(c)
			if err := s.Pool.QueryRow(ctx, `SELECT count(*)::int FROM media m WHERE `+where, args...).Scan(&sum.MediaCount); err != nil {
				return nil, err
			}
			_ = s.Pool.QueryRow(ctx, `SELECT m.id FROM media m WHERE `+where+`
				ORDER BY m.taken_at DESC, m.id DESC LIMIT 1`, args...).Scan(&firstID)
		} else {
			if err := s.Pool.QueryRow(ctx,
				`SELECT count(*)::int FROM album_items WHERE album_id = $1`, r.ID).Scan(&sum.MediaCount); err != nil {
				return nil, err
			}
			_ = s.Pool.QueryRow(ctx, `
				SELECT ai.media_id FROM album_items ai JOIN media m ON m.id = ai.media_id
				WHERE ai.album_id = $1 AND m.deleted_at IS NULL
				ORDER BY ai.sort_key ASC, m.taken_at DESC LIMIT 1`, r.ID).Scan(&firstID)
		}
		sum.CoverMediaID = effectiveCover(sum.CoverMediaID, firstID)
		if sum.CoverMediaID != nil {
			_ = s.Pool.QueryRow(ctx,
				`SELECT thumbnail_sm FROM media WHERE id = $1`, *sum.CoverMediaID).Scan(&sum.CoverMediaThumb)
		}
		out = append(out, sum)
	}
	return out, nil
}

// getCriteria 读取智能相册条件。
func (s *Store) getCriteria(ctx context.Context, id string) (*Criteria, error) {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT query FROM albums WHERE id = $1`, id).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var c Criteria
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Get 相册详情：normal 取 album_items（按加入顺序），smart 由 criteria 实时计算。
func (s *Store) Get(ctx context.Context, id string) (*Detail, error) {
	var d Detail
	var raw []byte
	err := s.Pool.QueryRow(ctx, `
		SELECT id, name, type, COALESCE(description,''), cover_media_id, query, owner_id
		FROM albums WHERE id = $1`, id).
		Scan(&d.ID, &d.Name, &d.Type, &d.Description, &d.CoverMediaID, &raw, &d.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Kind = typeToKind(d.Type)
	if len(raw) > 0 {
		var c Criteria
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		d.Criteria = &c
	}

	if d.Type == "smart" {
		where, args := buildCriteriaWhere(d.Criteria)
		rows, err := s.Pool.Query(ctx, `SELECT `+mediaCols+` FROM media m WHERE `+where+`
			ORDER BY m.taken_at DESC, m.id DESC`, args...)
		if err != nil {
			return nil, err
		}
		d.Items, err = scanMediaRows(rows)
		if err != nil {
			return nil, err
		}
	} else {
		rows, err := s.Pool.Query(ctx, `
			SELECT `+mediaCols+`
			FROM album_items ai JOIN media m ON m.id = ai.media_id
			WHERE ai.album_id = $1 AND m.deleted_at IS NULL
			ORDER BY ai.sort_key ASC, m.taken_at DESC`, id)
		if err != nil {
			return nil, err
		}
		d.Items, err = scanMediaRows(rows)
		if err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// Patch 局部更新相册字段（nil 表示不更新）。
func (s *Store) Patch(ctx context.Context, id string, name, description *string, coverMediaID *string, c *Criteria) error {
	var queryJSON []byte
	if c != nil {
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		queryJSON = b
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE albums SET
			name           = COALESCE($2, name),
			description    = COALESCE($3, description),
			cover_media_id = COALESCE($4, cover_media_id),
			query          = COALESCE($5, query)
		WHERE id = $1`, id, name, description, coverMediaID, queryJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 删除相册（album_items / album_comments 由外键级联清理）。
func (s *Store) Delete(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM albums WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddItems 批量加入媒体（去重；返回实际新增数）。
func (s *Store) AddItems(ctx context.Context, albumID string, mediaIDs []string) (int64, error) {
	// 去重保序
	seen := make(map[string]struct{}, len(mediaIDs))
	uniq := make([]string, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return 0, nil
	}
	// sort_key 续排（按加入顺序）；已存在条目 ON CONFLICT 跳过
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO album_items (album_id, media_id, sort_key)
		SELECT $1, mid,
		       COALESCE((SELECT max(sort_key) FROM album_items WHERE album_id = $1), 0)
		       + ROW_NUMBER() OVER (ORDER BY ord)::int
		FROM unnest($2::uuid[]) WITH ORDINALITY AS u(mid, ord)
		ON CONFLICT (album_id, media_id) DO NOTHING`,
		albumID, uniq)
	if err != nil {
		return 0, fmt.Errorf("加入媒体失败（媒体 id 可能不存在）: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RemoveItem 移除单个媒体。
func (s *Store) RemoveItem(ctx context.Context, albumID, mediaID string) error {
	tag, err := s.Pool.Exec(ctx,
		`DELETE FROM album_items WHERE album_id = $1 AND media_id = $2`, albumID, mediaID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListComments 评论列表（按时间升序扁平返回）。
func (s *Store) ListComments(ctx context.Context, albumID string) ([]Comment, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.user_id, COALESCE(NULLIF(u.display_name,''), u.email),
		       c.parent_id, c.content, c.created_at
		FROM album_comments c JOIN users u ON u.id = c.user_id
		WHERE c.album_id = $1
		ORDER BY c.created_at ASC`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var cm Comment
		if err := rows.Scan(&cm.ID, &cm.UserID, &cm.UserName, &cm.ParentID, &cm.Content, &cm.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, cm)
	}
	return out, rows.Err()
}

// AddComment 发表评论；两级树约束：parent 自身若为回复则拒绝。
func (s *Store) AddComment(ctx context.Context, albumID, userID, content string, parentID *string) (string, error) {
	if parentID != nil {
		var pp *string
		err := s.Pool.QueryRow(ctx,
			`SELECT parent_id FROM album_comments WHERE id = $1 AND album_id = $2`,
			*parentID, albumID).Scan(&pp)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrParentMissing
		}
		if err != nil {
			return "", err
		}
		if !checkReplyAllowed(pp != nil) {
			return "", ErrThirdLevel
		}
	}
	var id string
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO album_comments (album_id, user_id, content, parent_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		albumID, userID, content, parentID).Scan(&id)
	return id, err
}

// GetCommentAuthor 查询评论作者（删除权限判定用）。
func (s *Store) GetCommentAuthor(ctx context.Context, albumID, commentID string) (string, error) {
	var uid string
	err := s.Pool.QueryRow(ctx,
		`SELECT user_id FROM album_comments WHERE id = $1 AND album_id = $2`,
		commentID, albumID).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCommentNoRows
	}
	return uid, err
}

// DeleteComment 删除评论。
func (s *Store) DeleteComment(ctx context.Context, commentID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM album_comments WHERE id = $1`, commentID)
	return err
}
