package tags

// 标签 AI 打标的 DB 存取（清扫式 worker 与 API 预览共用）。
//
// 落库约定（双写，见迁移 00015）：
//   - media_tags.confirmed/confidence/origin 表达**逐图关联级**状态；
//   - tags.confirmed 表达**标签名级**粗粒度「已审阅」；由本文件在确认时同步置真。

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 标签打标的数据库访问。
type Store struct {
	Pool *pgxpool.Pool
}

// PendingMedia 待 AI 打标的媒体。
type PendingMedia struct {
	ID       string
	Filename string
	Place    string
	Type     string
	Is360    bool
	TakenAt  time.Time
}

// EmbeddedMedia 携带已缓存向量的媒体（标定/打标复用）。
type EmbeddedMedia struct {
	PendingMedia
	Vec []float32
}

// ListPendingAI 列出待 AI 打标的媒体：未删除、有向量、有缩略图、且尚无 origin='ai' 关联。
//
// 说明：若某图既无 CLIP 建议也无启发式标签，它会在每轮清扫中被重新计算——
// 但该计算只是缓存标签向量与图像向量的**点积**（无推理），成本可忽略。
// forceMediaID 非空时只返回该媒体且忽略「是否已打标」（供手动单图触发）。
func (s *Store) ListPendingAI(ctx context.Context, limit int, forceMediaID string) ([]PendingMedia, error) {
	if limit <= 0 {
		limit = 200
	}
	if forceMediaID != "" {
		rows, err := s.Pool.Query(ctx, `
			SELECT id::text, COALESCE(filename,''), COALESCE(place,''), type::text,
			       COALESCE(is_360,false), COALESCE(taken_at, 'epoch'::timestamptz)
			FROM media
			WHERE id = $1 AND deleted_at IS NULL AND embedding IS NOT NULL`, forceMediaID)
		if err != nil {
			return nil, err
		}
		return scanPending(rows)
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id::text, COALESCE(m.filename,''), COALESCE(m.place,''), m.type::text,
		       COALESCE(m.is_360,false), COALESCE(m.taken_at, 'epoch'::timestamptz)
		FROM media m
		WHERE m.deleted_at IS NULL
		  AND m.embedding IS NOT NULL
		  AND COALESCE(m.thumbnail_md,'') <> ''
		  AND NOT EXISTS (
		      SELECT 1 FROM media_tags mt
		      WHERE mt.media_id = m.id AND mt.origin = 'ai')
		ORDER BY m.taken_at DESC NULLS LAST, m.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return scanPending(rows)
}

func scanPending(rows pgx.Rows) ([]PendingMedia, error) {
	defer rows.Close()
	out := []PendingMedia{}
	for rows.Next() {
		var m PendingMedia
		if err := rows.Scan(&m.ID, &m.Filename, &m.Place, &m.Type, &m.Is360, &m.TakenAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListEmbedded 返回最多 limit 条「有向量」的媒体及其向量（标定用；仅读取）。
func (s *Store) ListEmbedded(ctx context.Context, limit int) ([]EmbeddedMedia, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id::text, COALESCE(m.filename,''), COALESCE(m.place,''), m.type::text,
		       COALESCE(m.is_360,false), COALESCE(m.taken_at, 'epoch'::timestamptz),
		       m.embedding::text
		FROM media m
		WHERE m.deleted_at IS NULL AND m.embedding IS NOT NULL
		ORDER BY m.taken_at DESC NULLS LAST, m.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmbeddedMedia{}
	for rows.Next() {
		var m EmbeddedMedia
		var vecText string
		if err := rows.Scan(&m.ID, &m.Filename, &m.Place, &m.Type, &m.Is360, &m.TakenAt, &vecText); err != nil {
			return nil, err
		}
		v, err := ParseVectorLiteral(vecText)
		if err != nil {
			return nil, fmt.Errorf("解析 %s 的向量失败: %w", m.Filename, err)
		}
		m.Vec = v
		out = append(out, m)
	}
	return out, rows.Err()
}

// LoadEmbedding 读取单条媒体的向量（预览接口用）。
func (s *Store) LoadEmbedding(ctx context.Context, id string) ([]float32, error) {
	var vecText string
	err := s.Pool.QueryRow(ctx,
		`SELECT embedding::text FROM media WHERE id = $1 AND embedding IS NOT NULL`, id).Scan(&vecText)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseVectorLiteral(vecText)
}

// ApplySuggestion 落库一条打标结果（幂等）。
//
// origin 取值：'ai'（CLIP 建议，默认待确认）| 'heuristic'（元数据兜底，直接确认）。
// 同名标签已存在时复用（含用户手工创建的 user 标签 → 视为已确认，不降级）。
func (s *Store) ApplySuggestion(ctx context.Context, mediaID, tagName string, confidence float64, origin string, confirmed bool) error {
	if origin == "" {
		origin = "ai"
	}
	tagID, kind, err := s.ensureTag(ctx, tagName, confirmed)
	if err != nil {
		return err
	}
	if kind == "user" {
		confirmed = true // 用户已手工建立该标签 → 关联视为已确认
	}
	if _, err := s.Pool.Exec(ctx, `
		INSERT INTO media_tags (media_id, tag_id, confirmed, confidence, origin)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (media_id, tag_id) DO UPDATE
		  SET origin = EXCLUDED.origin,
		      -- 置信度 latest-wins：新算出的置信度必须能覆盖旧值，否则调阈值后存量标签永远刷不新。
		      -- 仅当新值为 NULL 时才保留旧值（避免手工关联把已有 AI 置信度抹掉）。
		      confidence = COALESCE(EXCLUDED.confidence, media_tags.confidence),
		      confirmed = media_tags.confirmed OR EXCLUDED.confirmed`,
		mediaID, tagID, confirmed, confidence, origin); err != nil {
		return err
	}
	if confirmed {
		return s.markTagReviewed(ctx, tagID)
	}
	return nil
}

// ensureTag 按名取标签（优先 user）；不存在则创建 kind='ai'。
func (s *Store) ensureTag(ctx context.Context, name string, confirmed bool) (id, kind string, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT id::text, kind FROM tags WHERE name = $1
		ORDER BY (kind = 'user') DESC, (kind = 'ai') DESC
		LIMIT 1`, name).Scan(&id, &kind)
	if err == nil {
		return id, kind, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", "", err
	}
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO tags (name, kind, confirmed) VALUES ($1, 'ai', $2)
		ON CONFLICT (name, kind) DO UPDATE SET confirmed = tags.confirmed OR EXCLUDED.confirmed
		RETURNING id::text`, name, confirmed).Scan(&id)
	return id, "ai", err
}

// markTagReviewed 双写：标签名级粗粒度审阅标记置真。
func (s *Store) markTagReviewed(ctx context.Context, tagID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE tags SET confirmed = true WHERE id = $1`, tagID)
	return err
}

// Counts 统计：总数 / 已向量化 / 已被 AI 打标过 / 待 AI 打标。
func (s *Store) Counts(ctx context.Context) (total, embedded, aiTagged, pending int, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE embedding IS NOT NULL),
		       count(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM media_tags mt WHERE mt.media_id = m.id AND mt.origin = 'ai')),
		       count(*) FILTER (WHERE embedding IS NOT NULL
		           AND COALESCE(thumbnail_md,'') <> ''
		           AND NOT EXISTS (
		               SELECT 1 FROM media_tags mt WHERE mt.media_id = m.id AND mt.origin = 'ai'))
		FROM media m WHERE deleted_at IS NULL`).Scan(&total, &embedded, &aiTagged, &pending)
	return
}

// PendingAITags 列出某媒体尚未确认的 AI 建议（预览/前端待确认区）。
type PendingAITag struct {
	TagID      string  `json:"tag_id"`
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
	Origin     string  `json:"origin"`
}

// ListPendingAITags 列出某媒体未确认的 AI/启发式待确认标签。
func (s *Store) ListPendingAITags(ctx context.Context, mediaID string) ([]PendingAITag, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT t.id::text, t.name, COALESCE(mt.confidence, 0), COALESCE(mt.origin,'')
		FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
		WHERE mt.media_id = $1 AND mt.confirmed = false
		ORDER BY mt.confidence DESC NULLS LAST, t.name`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingAITag{}
	for rows.Next() {
		var p PendingAITag
		if err := rows.Scan(&p.TagID, &p.Name, &p.Confidence, &p.Origin); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ParseVectorLiteral 解析 pgvector 文本 "[v1,v2,...]" → []float32。
func ParseVectorLiteral(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil, errors.New("空向量")
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, err
		}
		out = append(out, float32(f))
	}
	return out, nil
}
