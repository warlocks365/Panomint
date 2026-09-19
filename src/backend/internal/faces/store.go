package faces

// 人脸数据的 DB 存取（pgvector + PostGIS 风格的 BOX）。
//
// 依赖约定（DDL v1.1 已被 00014 迁移调整）：
//   faces.embedding  VECTOR(128)（SFace 输出维度），索引 idx_faces_embedding 为 HNSW + vector_cosine_ops
//   faces.bbox       BOX        人脸框
//   faces.cluster_id VARCHAR    临时聚类 ID（用户命名后落 people 行并回填 person_id）
//   faces.person_id  UUID       用户命名的人物（非空=已命名）；重扫时按框重叠迁移，见 match.go
//   media.faces_scanned_at TIMESTAMPTZ  人脸扫描标记（非空=已扫过，即使 0 张脸）
//
// 写入约定：某媒体的人脸是**整体替换**（先 DeleteFacesByMedia 再逐张 SaveFace），
// 不做逐行 upsert——这样重扫天然幂等，不会因人脸重复插入而堆积。

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
	// Path 是原图相对路径（相对 mediaRoot 解析）。可能为空或指向不存在的文件
	// （只导入过缩略图 / NAS 未挂载 / 源文件已被移除），故它只是**首选**而非必需 ——
	// 见 LoadScanImage 的回退链。
	Path string
}

// ListPending 列出待人脸扫描的媒体；force 为真时忽略扫描标记全量重算。
//
// 仍**只返回已有 LG 缩略图**的行：缩略图由 index worker 异步产出，晚到者由下一轮清扫自然补上
// （见 facesgen -mode watch），且它是原图不可用时的**回退源** —— 保留这个条件等于保证
// "每一行至少有一个可解码的图"，扫描才不会因为个别行缺原图而整体失败。
//
// 为什么还要带上 path（原图）：检测输入边长按源图长边自适应（resolveInputSize，上限 FACE_INPUT_MAX），
// 而 LG 缩略图宽固定 1280 —— 喂原图才能真正吃到"输入更大 → 更小的人脸也能检出"的收益。
func (s *Store) ListPending(ctx context.Context, force bool, limit int) ([]MediaItem, error) {
	if limit <= 0 {
		limit = 200
	}
	cond := "faces_scanned_at IS NULL"
	if force {
		cond = "true"
	}
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, COALESCE(filename,''), COALESCE(thumbnail_lg,''), COALESCE(path,'')
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
		if err := rows.Scan(&m.ID, &m.Filename, &m.ThumbLG, &m.Path); err != nil {
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

// FacesByMedia 列出某媒体已入库的人脸，供重扫时迁移用户命名关联（见 match.go）。
//
// 必须在 DeleteFacesByMedia **之前**调用：删除后这些行就再也读不回来了。
//
// bbox 是 Postgres 原生 box，写入时按 box(point(X,Y), point(X+W,Y+H)) 构造，
// 故规范化后 (bbox)[1] 恒为左下角（X,Y）、(bbox)[0] 恒为右上角（X+W,Y+H）。
// bbox 为 NULL 的行（理论上不该出现）用 COALESCE 退化成全 0 的零面积框，
// 因而绝不可能与任何新检出匹配上（BoxIoU 返回 0）——即「宁可丢掉命名，也不张冠李戴」。
func (s *Store) FacesByMedia(ctx context.Context, mediaID string) ([]FaceRef, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT COALESCE(person_id::text, ''),
		       is_pet,
		       COALESCE(cluster_id, ''),
		       COALESCE(((bbox)[1])[0]::float8, 0),
		       COALESCE(((bbox)[1])[1]::float8, 0),
		       COALESCE(((bbox)[0])[0]::float8, 0),
		       COALESCE(((bbox)[0])[1]::float8, 0)
		FROM faces
		WHERE media_id = $1::uuid`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FaceRef{}
	for rows.Next() {
		var r FaceRef
		var x2, y2 float64
		if err := rows.Scan(&r.PersonID, &r.IsPet, &r.ClusterID, &r.X, &r.Y, &x2, &y2); err != nil {
			return nil, err
		}
		r.W, r.H = x2-r.X, y2-r.Y
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveFace 写入一张人脸（向量已 L2 归一化；bbox 用 BOX 类型）。
//
// personID 非空表示这张脸已被用户命名（faces.person_id），isPet 随之落库；
// 二者由重扫时的命名迁移给出（见 match.go），新建人脸时传空串 / false。
func (s *Store) SaveFace(ctx context.Context, mediaID string, d Detection, emb []float32,
	clusterID, personID string, isPet bool) error {

	if len(emb) != EmbeddingDim {
		return fmt.Errorf("人脸向量维度应为 %d，实得 %d", EmbeddingDim, len(emb))
	}
	var cluster any
	if clusterID != "" {
		cluster = clusterID
	}
	var person any
	if personID != "" {
		person = personID
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO faces (media_id, cluster_id, person_id, bbox, confidence, embedding, is_pet)
		VALUES ($1,
		        $2,
		        $3::uuid,
		        box(point($4::float8, $5::float8), point($6::float8, $7::float8)),
		        $8,
		        $9::vector,
		        $10)`,
		mediaID, cluster, person,
		d.X, d.Y, d.X+d.W, d.Y+d.H,
		d.Score, vectorLiteral(emb), isPet)
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
