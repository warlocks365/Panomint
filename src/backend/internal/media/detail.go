package media

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// 媒体详情（API v1.1 §3 GET /media/:id 契约结构）。

// Exif EXIF 元数据子结构。
type Exif struct {
	CameraMake   *string  `json:"camera_make,omitempty"`
	CameraModel  *string  `json:"camera_model,omitempty"`
	LensModel    *string  `json:"lens_model,omitempty"`
	FocalLength  *float64 `json:"focal_length,omitempty"`
	Aperture     *float64 `json:"aperture,omitempty"`
	ISO          *int     `json:"iso,omitempty"`
	ShutterSpeed *string  `json:"shutter_speed,omitempty"`
	ExposureBias *float64 `json:"exposure_bias,omitempty"`
}

// VideoMeta 视频技术元数据子结构。
type VideoMeta struct {
	FPS        *float64 `json:"fps,omitempty"`
	Bitrate    *int     `json:"bitrate,omitempty"`
	Duration   *int     `json:"duration,omitempty"`
	HDR        bool     `json:"hdr"`
	ColorSpace *string  `json:"color_space,omitempty"`
}

// Geo 经纬度。
type Geo struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// NamedRef 带名称的关联引用（tags/people/albums）。
type NamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Detail 媒体详情响应。
type Detail struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"`
	Space          string     `json:"space"`
	OwnerID        string     `json:"owner_id"`
	Path           string     `json:"path"`
	FolderPath     *string    `json:"folder_path,omitempty"`
	Filename       *string    `json:"filename,omitempty"`
	TakenAt        *time.Time `json:"taken_at,omitempty"`
	Width          *int       `json:"width,omitempty"`
	Height         *int       `json:"height,omitempty"`
	Codec          *string    `json:"codec,omitempty"`
	GPS            *Geo       `json:"gps,omitempty"`
	Place          *string    `json:"place,omitempty"`
	Is360          bool       `json:"is_360"`
	Projection     *string    `json:"projection,omitempty"`
	ThumbnailSM    *string    `json:"thumbnail_sm,omitempty"`
	ThumbnailMD    *string    `json:"thumbnail_md,omitempty"`
	ThumbnailLG    *string    `json:"thumbnail_lg,omitempty"`
	HLSMaster      *string    `json:"hls_master,omitempty"`
	Filesize       *int64     `json:"filesize,omitempty"`
	Rating         int        `json:"rating"`
	Favorite       bool       `json:"favorite"`
	LivePhotoPair  *string    `json:"live_photo_pair_id,omitempty"`
	Tags           []NamedRef `json:"tags"`
	People         []NamedRef `json:"people"`
	Albums         []NamedRef `json:"albums"`
	Exif           Exif       `json:"exif"`
	Video          *VideoMeta `json:"video,omitempty"`
}

// ErrNotFound 媒体不存在或无权限。
var ErrNotFound = errors.New("媒体不存在")

// canAccess 访问控制：本人，或 owner/admin 角色。
func canAccess(userID, role, ownerID string) bool {
	return userID == ownerID || role == "owner" || role == "admin"
}

// GetDetail 查询媒体详情（含标签/人物/相册关联，关联均可为空数组）。
func (s *Store) GetDetail(ctx context.Context, id string) (*Detail, error) {
	var d Detail
	var lat, lng *float64
	var vm VideoMeta
	err := s.Pool.QueryRow(ctx, `
		SELECT m.id, m.type::text, m.space::text, m.owner_id, m.path, m.folder_path, m.filename,
		       m.taken_at, m.width, m.height, m.codec,
		       CASE WHEN m.gps IS NOT NULL THEN ST_Y(m.gps) END,
		       CASE WHEN m.gps IS NOT NULL THEN ST_X(m.gps) END,
		       m.place, m.is_360, m.projection,
		       m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg, m.hls_master,
		       m.filesize, m.rating, m.live_photo_pair_id,
		       m.camera_make, m.camera_model, m.lens_model, m.focal_length, m.aperture,
		       m.iso, m.shutter_speed, m.exposure_bias,
		       m.fps, m.bitrate, m.duration, m.hdr, m.color_space,
		       EXISTS(SELECT 1 FROM album_items ai JOIN albums a ON a.id = ai.album_id
		              WHERE ai.media_id = m.id AND a.type = 'favorites')
		FROM media m WHERE m.id = $1 AND m.deleted_at IS NULL`, id,
	).Scan(&d.ID, &d.Type, &d.Space, &d.OwnerID, &d.Path, &d.FolderPath, &d.Filename,
		&d.TakenAt, &d.Width, &d.Height, &d.Codec, &lat, &lng,
		&d.Place, &d.Is360, &d.Projection,
		&d.ThumbnailSM, &d.ThumbnailMD, &d.ThumbnailLG, &d.HLSMaster,
		&d.Filesize, &d.Rating, &d.LivePhotoPair,
		&d.Exif.CameraMake, &d.Exif.CameraModel, &d.Exif.LensModel, &d.Exif.FocalLength,
		&d.Exif.Aperture, &d.Exif.ISO, &d.Exif.ShutterSpeed, &d.Exif.ExposureBias,
		&vm.FPS, &vm.Bitrate, &vm.Duration, &vm.HDR, &vm.ColorSpace,
		&d.Favorite)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if lat != nil && lng != nil {
		d.GPS = &Geo{Lat: *lat, Lng: *lng}
	}
	// 视频技术元数据（仅视频挂载）
	if d.Type == "video" {
		d.Video = &vm
	}

	// 关联：标签 / 人物 / 相册（空结果给空数组）
	d.Tags = []NamedRef{}
	d.People = []NamedRef{}
	d.Albums = []NamedRef{}
	if err := s.loadRefs(ctx, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// loadRefs 装载 tags/people/albums 关联数组。
func (s *Store) loadRefs(ctx context.Context, d *Detail) error {
	queries := []struct {
		sql  string
		dest *[]NamedRef
	}{
		{`SELECT t.id, t.name FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
			WHERE mt.media_id = $1 ORDER BY t.name`, &d.Tags},
		{`SELECT DISTINCT pe.id, COALESCE(pe.name,'') FROM faces f JOIN people pe ON pe.id = f.person_id
			WHERE f.media_id = $1 ORDER BY 2`, &d.People},
		{`SELECT a.id, a.name FROM album_items ai JOIN albums a ON a.id = ai.album_id
			WHERE ai.media_id = $1 ORDER BY a.name`, &d.Albums},
	}
	for _, q := range queries {
		rows, err := s.Pool.Query(ctx, q.sql, d.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r NamedRef
			if err := rows.Scan(&r.ID, &r.Name); err != nil {
				rows.Close()
				return err
			}
			*q.dest = append(*q.dest, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}
