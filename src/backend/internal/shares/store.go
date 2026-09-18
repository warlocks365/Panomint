// Package shares 分享链接（Stage 2 微信 H5 分享后端）。
// token 即凭证：公开端点无鉴权，靠 64 位 hex 随机 token 防枚举。
package shares

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"panoalbum/internal/albums"
	"panoalbum/internal/media"
	"panoalbum/internal/mediascope"
)

// Store 分享数据访问。
type Store struct {
	Pool   *pgxpool.Pool
	Albums *albums.Store // 相册条目复用（normal/smart 统一）
}

var (
	ErrNotFound   = errors.New("分享不存在")
	ErrTargetLost = errors.New("分享目标不存在")
)

// Share 分享链接记录（对应 share_links 表；吊销 = 删除行）。
type Share struct {
	ID            string     `json:"id"`
	Token         string     `json:"token"`
	Kind          string     `json:"kind"` // album|media
	TargetID      string     `json:"target_id"`
	OwnerID       string     `json:"owner_id"`
	Title         *string    `json:"title,omitempty"`
	ExpireAt      *time.Time `json:"expire_at,omitempty"`
	PasswordHash  *string    `json:"-"`
	AllowDownload bool       `json:"allow_download"`
	IsWechat      bool       `json:"is_wechat"`
	MaxViews      *int       `json:"max_views,omitempty"`
	AccessCount   int        `json:"access_count"`
	CreatedAt     time.Time  `json:"created_at"`
}

// genToken crypto/rand 32 字节 hex（64 字符），防枚举。
func genToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Status 分享状态（列表展示用）：active|expired|exhausted。
func (s *Share) Status(now time.Time) string {
	if s.ExpireAt != nil && !now.Before(*s.ExpireAt) {
		return "expired"
	}
	if s.MaxViews != nil && s.AccessCount >= *s.MaxViews {
		return "exhausted"
	}
	return "active"
}

// checkAccess 公开访问纯逻辑校验：过期 → 超量 → 密码。
// 返回错误码（"" 表示通过），HTTP 一律 403。
func checkAccess(sh *Share, password string, now time.Time) string {
	if sh.ExpireAt != nil && !now.Before(*sh.ExpireAt) {
		return "EXPIRED"
	}
	if sh.MaxViews != nil && sh.AccessCount >= *sh.MaxViews {
		return "MAX_VIEWS"
	}
	if sh.PasswordHash != nil && *sh.PasswordHash != "" {
		if password == "" {
			return "PASSWORD_REQUIRED"
		}
		if bcrypt.CompareHashAndPassword([]byte(*sh.PasswordHash), []byte(password)) != nil {
			return "WRONG_PASSWORD"
		}
	}
	return ""
}

const shareCols = `id, token, kind::text, target_id, owner_id, title, expire_at, password_hash,
	allow_download, is_wechat, max_views, access_count, created_at`

// scanShare 扫描一行分享。
func scanShare(row pgx.Row) (*Share, error) {
	var s Share
	err := row.Scan(&s.ID, &s.Token, &s.Kind, &s.TargetID, &s.OwnerID, &s.Title, &s.ExpireAt,
		&s.PasswordHash, &s.AllowDownload, &s.IsWechat, &s.MaxViews, &s.AccessCount, &s.CreatedAt)
	return &s, err
}

// CreateInput 创建参数。
type CreateInput struct {
	Kind          string
	TargetID      string
	Title         *string
	ExpireAt      *time.Time
	PasswordHash  *string // 已 bcrypt；空密码为 nil
	AllowDownload bool
	IsWechat      bool
	MaxViews      *int
}

// Create 新建分享，返回 id 与 token。
func (s *Store) Create(ctx context.Context, ownerID string, in CreateInput) (string, string, error) {
	token, err := genToken()
	if err != nil {
		return "", "", err
	}
	var id string
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO share_links (token, kind, target_id, owner_id, title, expire_at, password_hash,
			allow_download, is_wechat, max_views)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		token, in.Kind, in.TargetID, ownerID, in.Title, in.ExpireAt, in.PasswordHash,
		in.AllowDownload, in.IsWechat, in.MaxViews).Scan(&id)
	return id, token, err
}

// ListByOwner 我的分享列表（按创建时间倒序）。
func (s *Store) ListByOwner(ctx context.Context, ownerID string) ([]Share, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+shareCols+`
		FROM share_links WHERE owner_id = $1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Share{}
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sh)
	}
	return out, rows.Err()
}

// GetByID 按 id 查（吊销归属判定用）。
func (s *Store) GetByID(ctx context.Context, id string) (*Share, error) {
	sh, err := scanShare(s.Pool.QueryRow(ctx, `SELECT `+shareCols+` FROM share_links WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sh, err
}

// GetByToken 按 token 查（公开端点凭证校验）。
func (s *Store) GetByToken(ctx context.Context, token string) (*Share, error) {
	sh, err := scanShare(s.Pool.QueryRow(ctx, `SELECT `+shareCols+` FROM share_links WHERE token = $1`, token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sh, err
}

// Delete 吊销（删除行；表无 revoked 字段，删除即吊销）。
func (s *Store) Delete(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM share_links WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordAccess 访问审计：写 share_access_log + access_count 原子自增（同事务）。
func (s *Store) RecordAccess(ctx context.Context, shareID, ip, ua string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO share_access_log (share_id, ip, user_agent) VALUES ($1, $2, $3)`,
		shareID, ip, ua); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE share_links SET access_count = access_count + 1 WHERE id = $1`, shareID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TargetOwnedBy 分享目标归属校验（media.owner_id / albums.owner_id）。
func (s *Store) TargetOwnedBy(ctx context.Context, kind, targetID, userID string) (bool, error) {
	var owner string
	var err error
	switch kind {
	case "media":
		err = s.Pool.QueryRow(ctx,
			`SELECT owner_id FROM media WHERE id = $1 AND deleted_at IS NULL`, targetID).Scan(&owner)
	case "album":
		err = s.Pool.QueryRow(ctx,
			`SELECT owner_id FROM albums WHERE id = $1`, targetID).Scan(&owner)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrTargetLost
	}
	if err != nil {
		return false, err
	}
	return owner == userID, nil
}

// MediaRef 的列清单与扫描器**不再在本包维护** —— 唯一真源在 internal/media/mediaref.go。
// 这里原先的 mediaCols 注释写着"与 internal/albums/store.go 保持一致"，而两份都是裸选可空列：
// 注释互相引用，却没有一处是权威。实测后果是 GET /public/shares/:token（kind=media）
// 在任意一行 filename/folder_path 为 NULL 时整响应 500。

// ListItems 分享内容：media 单条 / album 全量（smart 由 albums.Store 实时计算）。
func (s *Store) ListItems(ctx context.Context, sh *Share) ([]media.MediaRef, error) {
	if sh.Kind == "album" {
		d, err := s.Albums.Get(ctx, sh.TargetID)
		if errors.Is(err, albums.ErrNotFound) {
			return nil, ErrTargetLost
		}
		if err != nil {
			return nil, err
		}
		return d.Items, nil
	}
	// kind = media：单条（已删除则空列表）
	it, err := media.ScanMediaRef(s.Pool.QueryRow(ctx, `SELECT `+media.MediaRefColumns+`
		FROM media m WHERE m.id = $1 AND m.deleted_at IS NULL`, sh.TargetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return []media.MediaRef{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []media.MediaRef{it}, nil
}

// MediaInShare 媒体归属校验：该媒体是否属于此分享目标（缩略图/HLS 越权防护）。
func (s *Store) MediaInShare(ctx context.Context, sh *Share, mediaID string) (bool, error) {
	if sh.Kind == "media" {
		return sh.TargetID == mediaID, nil
	}
	// album：normal 走 album_items；smart 实时计算条目集合
	var typ, albumOwner string
	err := s.Pool.QueryRow(ctx, `SELECT type, owner_id FROM albums WHERE id = $1`, sh.TargetID).Scan(&typ, &albumOwner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrTargetLost
	}
	if err != nil {
		return false, err
	}
	if typ != "smart" {
		// 与读路径同一口径：EXISTS 里也要按**相册属主**的可见集过滤。
		//
		// 否则会出现"列表里看不到、但仍能按 media id 直接取字节"的侧门：某条历史脏行
		// （AddItems 修复前塞进来的他人 media）虽已被 ListItems/Get 过滤掉，
		// 却仍然通过本判定 → 匿名访问者可用它换到 PublicThumb 的缩略图字节（实测 200/40KB）。
		//
		// 主体用 albumOwner：匿名分享链路没有调用者身份，且分享是**有意**让匿名可见的；
		// 用相册属主既收紧了历史脏行，又不会把正常分享打死（属主自己的媒体仍在集合内）。
		vis, visArgs := mediascope.VisibleCondFor(3, albumOwner, "m")
		args := append([]any{sh.TargetID, mediaID}, visArgs...)
		var ok bool
		err := s.Pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM album_items ai JOIN media m ON m.id = ai.media_id
				WHERE ai.album_id = $1 AND ai.media_id = $2 AND m.deleted_at IS NULL AND `+vis+`)`,
			args...).Scan(&ok)
		return ok, err
	}
	items, err := s.ListItems(ctx, sh)
	if err != nil {
		return false, err
	}
	for _, it := range items {
		if it.ID == mediaID {
			return true, nil
		}
	}
	return false, nil
}

// ThumbFile 读媒体缩略图文件名（按尺寸列；媒体已删除或缺图为 nil）。
func (s *Store) ThumbFile(ctx context.Context, mediaID, size string) (*string, error) {
	col := map[string]string{"sm": "thumbnail_sm", "md": "thumbnail_md", "lg": "thumbnail_lg"}[size]
	if col == "" {
		return nil, errors.New("size 仅支持 sm|md|lg")
	}
	var name *string
	err := s.Pool.QueryRow(ctx, `SELECT `+col+` FROM media WHERE id = $1 AND deleted_at IS NULL`,
		mediaID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return name, err
}

// HasHLS 媒体是否已有 HLS 产物（hls_master 非空）。
func (s *Store) HasHLS(ctx context.Context, mediaID string) (bool, error) {
	var master *string
	err := s.Pool.QueryRow(ctx,
		`SELECT hls_master FROM media WHERE id = $1 AND deleted_at IS NULL`, mediaID).Scan(&master)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return master != nil && *master != "", nil
}
