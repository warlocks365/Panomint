package faces

// 人脸数据的 DB 存取（pgvector + PostGIS 风格的 BOX）。
//
// 依赖约定（DDL v1.1 已被 00014 迁移调整）：
//   faces.embedding  VECTOR(128)（SFace 输出维度），索引 idx_faces_embedding 为 HNSW + vector_cosine_ops
//   faces.bbox       BOX        人脸框
//   faces.cluster_id VARCHAR    临时聚类 ID（用户命名后落 people 行并回填 person_id）
//   media.faces_scanned_at TIMESTAMPTZ  人脸扫描标记（非空=已扫过，即使 0 张脸）

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 人脸存取（聚类亦经此访问库）。
type Store struct {
	Pool *pgxpool.Pool
}

// MediaItem 待扫描媒体。
type MediaItem struct {
	ID       string
	Filename string
	ThumbLG  string
}

// ListPending 列出待人脸扫描的媒体；force 为真时忽略扫描标记全量重算。
//
// 只返回**已有 LG 缩略图**的行：人脸检测需要足够分辨率（LG=宽 1280），
// 缩略图由 index worker 异步产出，晚到者由下一轮清扫自然补上（见 facesgen -mode watch）。
func (s *Store) ListPending(ctx context.Context, force bool, limit int) ([]MediaItem, error) {
	if limit <= 0 {
		limit = 200
	}
	cond := "faces_scanned_at IS NULL"
	if force {
		cond = "true"
	}
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, COALESCE(filename,''), COALESCE(thumbnail_lg,'')
		FROM media
		WHERE deleted_at IS NULL AND COALESCE(thumbnail_lg,'') <> '' AND %s
		ORDER BY taken_at DESC NULLS LAST, id
		LIMIT $1`, cond), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MediaItem{}
	for rows.Next() {
		var m MediaItem
		if err := rows.Scan(&m.ID, &m.Filename, &m.ThumbLG); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountStats 统计：(已扫描媒体数, 媒体总数, 人脸总数, 聚类数)。
func (s *Store) CountStats(ctx context.Context) (scanned, total, faceCount, clusterCount int, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE faces_scanned_at IS NOT NULL), count(*)
		FROM media WHERE deleted_at IS NULL`).Scan(&scanned, &total)
	if err != nil {
		return
	}
	err = s.Pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT cluster_id) FROM faces`).Scan(&faceCount, &clusterCount)
	return
}

// DeleteFacesByMedia 清除某媒体的旧人脸（重算前调用，避免残留过期框）。
func (s *Store) DeleteFacesByMedia(ctx context.Context, mediaID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM faces WHERE media_id = $1`, mediaID)
	return err
}

// SaveFace 写入一张人脸（向量已 L2 归一化；bbox 用 BOX 类型）。
func (s *Store) SaveFace(ctx context.Context, mediaID string, d Detection, emb []float32, clusterID string) error {
	if len(emb) != EmbeddingDim {
		return fmt.Errorf("人脸向量维度应为 %d，实得 %d", EmbeddingDim, len(emb))
	}
	var cluster any
	if clusterID != "" {
		cluster = clusterID
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO faces (media_id, cluster_id, bbox, confidence, embedding)
		VALUES ($1,
		        $2,
		        box(point($3::float8, $4::float8), point($5::float8, $6::float8)),
		        $7,
		        $8::vector)`,
		mediaID, cluster,
		d.X, d.Y, d.X+d.W, d.Y+d.H,
		d.Score, vectorLiteral(emb))
	return err
}

// MarkScanned 回填扫描标记（**即使该媒体 0 张脸也必须调用**，否则会被每轮重复扫）。
func (s *Store) MarkScanned(ctx context.Context, mediaID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE media SET faces_scanned_at = now() WHERE id = $1`, mediaID)
	return err
}

// NearestClusters 返回与 emb 余弦距离最近的 k 个**可并入的**已有簇（质心按 avg(embedding) 现算）。
//
// 关键不变量（由本查询唯一保证，调用方 PickCluster 直接信任传入的 refs）：
// **同一张媒体内的人脸最多归入同一个簇** —— 一张合影/群照里的不同人脸本就是不同的人，
// 故这里排除掉「已含有该 mediaID 某人脸的簇」。
// 依据：Job000011 实测 16 人合影被并成 1 个 11 脸大簇，11 张脸**全部来自同一张照片**；
// 单一相似度阈值无法分离（异人最高 0.525 > 真同人最低 0.4382），只能靠这条强先验兜底。
//
// 显式 ::text / ::float8 转换，避免依赖 pgvector 的 pgx 类型注册。
func (s *Store) NearestClusters(ctx context.Context, mediaID string, emb []float32, k int) ([]ClusterRef, error) {
	if len(emb) != EmbeddingDim {
		return nil, fmt.Errorf("人脸向量维度应为 %d，实得 %d", EmbeddingDim, len(emb))
	}
	if k <= 0 {
		k = 5
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT cluster_id, avg(embedding)::text AS centroid
		FROM faces
		WHERE cluster_id IS NOT NULL AND cluster_id <> ''
		  AND embedding IS NOT NULL
		  AND cluster_id NOT IN (
		      SELECT DISTINCT cluster_id FROM faces
		      WHERE media_id = $3::uuid AND cluster_id IS NOT NULL AND cluster_id <> ''
		  )
		GROUP BY cluster_id
		ORDER BY avg(embedding) <=> $1::vector
		LIMIT $2`, vectorLiteral(emb), k, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ClusterRef{}
	for rows.Next() {
		var id, centroid string
		if err := rows.Scan(&id, &centroid); err != nil {
			return nil, err
		}
		out = append(out, ClusterRef{ClusterID: id, Centroid: parseVector(centroid)})
	}
	return out, rows.Err()
}
