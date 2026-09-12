package embed

// 媒体 embedding 的 DB 存取（pgvector）。
//
// media.embedding 为 VECTOR(512)，索引 idx_media_embedding 使用 ivfflat(vector_cosine_ops)，
// 因此检索用余弦距离运算符 `<=>`（越小越相似）。写入前向量已做 L2 归一化。

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 向量存取。
type Store struct {
	Pool *pgxpool.Pool
}

// MediaThumb 待编码媒体的缩略图信息。
type MediaThumb struct {
	ID       string
	Filename string
	Place    string
	ThumbMD  string
	Is360    bool
	Type     string
}

// VectorHit 余弦检索命中。
type VectorHit struct {
	ID       string  `json:"id"`
	Filename string  `json:"filename"`
	Place    string  `json:"place"`
	Type     string  `json:"type"`
	Is360    bool    `json:"is_360"`
	Distance float64 `json:"distance"` // 余弦距离（0=完全相同方向）
}

// ListPending 列出需要生成 embedding 的媒体；force 为真时忽略已有向量全部重算。
//
// 只返回**已有缩略图**的行：没有缩略图就没有图像输入，列出来只会白跑一遍
// （缩略图由 index worker 异步生成，晚到者由下一次清扫自然补上，见 embedgen -mode watch）。
func (s *Store) ListPending(ctx context.Context, force bool, limit int) ([]MediaThumb, error) {
	if limit <= 0 {
		limit = 1000
	}
	cond := "embedding IS NULL"
	if force {
		cond = "true"
	}
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, filename, COALESCE(place,''), COALESCE(thumbnail_md,''),
		       COALESCE(is_360,false), type::text
		FROM media
		WHERE deleted_at IS NULL AND COALESCE(thumbnail_md,'') <> '' AND %s
		ORDER BY taken_at DESC NULLS LAST, id
		LIMIT $1`, cond), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MediaThumb{}
	for rows.Next() {
		var m MediaThumb
		if err := rows.Scan(&m.ID, &m.Filename, &m.Place, &m.ThumbMD, &m.Is360, &m.Type); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountEmbedded 已生成向量的媒体数 / 总数。
func (s *Store) CountEmbedded(ctx context.Context) (done, total int, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE embedding IS NOT NULL), count(*)
		FROM media WHERE deleted_at IS NULL`).Scan(&done, &total)
	return
}

// SaveEmbedding 写入单个媒体的向量。
func (s *Store) SaveEmbedding(ctx context.Context, id string, vec []float32) error {
	if len(vec) != EmbeddingDim {
		return fmt.Errorf("向量维度应为 %d，实得 %d", EmbeddingDim, len(vec))
	}
	_, err := s.Pool.Exec(ctx,
		`UPDATE media SET embedding = $2::vector WHERE id = $1`, id, VectorLiteral(vec))
	return err
}

// SearchByVector 余弦距离检索 top-K（仅返回未删除且向量非空的媒体）。
func (s *Store) SearchByVector(ctx context.Context, vec []float32, limit int) ([]VectorHit, error) {
	if len(vec) != EmbeddingDim {
		return nil, fmt.Errorf("向量维度应为 %d，实得 %d", EmbeddingDim, len(vec))
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id::text, filename, COALESCE(place,''), type::text, COALESCE(is_360,false),
		       (embedding <=> $1::vector) AS distance
		FROM media
		WHERE deleted_at IS NULL AND embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2`, VectorLiteral(vec), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []VectorHit{}
	for rows.Next() {
		var h VectorHit
		if err := rows.Scan(&h.ID, &h.Filename, &h.Place, &h.Type, &h.Is360, &h.Distance); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// VectorLiteral 把 float32 切片转成 pgvector 字面量 "[v1,v2,...]"。
func VectorLiteral(v []float32) string {
	var sb strings.Builder
	sb.Grow(len(v) * 9)
	sb.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(x), 'f', 6, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}
