package faces

// 人物与聚类的读写（API 契约 v1.1 §6 人物）。
//
// 语义划分：
//   people      用户已命名的人物（可隐藏、可标记宠物）
//   cluster_id  扫描时自动产生的临时聚类 ID
//   person_id   为空 ⇒ 该脸仍属"未命名聚类"，出现在 GET /people 的 unnamed 里
//   命名 / 合并  建 people 行 → 把所选 cluster_id 下的 faces.person_id 回填
//
// 封面规则：簇/人物的封面取**簇内 confidence 最高的那张脸所属的媒体**；
// 人物若已显式设置 people.cover_media_id 则优先使用（允许用户后续改封面）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PersonRow 已命名人物（GET /people 的 named 元素）。
type PersonRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Hidden       bool   `json:"hidden"`
	IsPet        bool   `json:"is_pet"`
	FaceCount    int    `json:"face_count"`
	CoverMediaID string `json:"cover_media_id,omitempty"`
}

// ClusterRow 未命名聚类（GET /people 的 unnamed 元素）。
type ClusterRow struct {
	ClusterID string `json:"cluster_id"`
	Count     int    `json:"count"`
	Cover     string `json:"cover,omitempty"`
}

// PersonMediaItem 人物媒体条目（GET /people/:id/media）。
type PersonMediaItem struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	TakenAt  string `json:"taken_at,omitempty"`
}

// ErrPersonNotFound 指定人物不存在（handler 据此回 404）。
var ErrPersonNotFound = errors.New("人物不存在")

// ErrNothingToUpdate PATCH 未携带任何可更新字段。
var ErrNothingToUpdate = errors.New("没有可更新的字段")

// ErrEmptyName 姓名不能为空。
var ErrEmptyName = errors.New("姓名不能为空")

// ListPeople 返回 (已命名人物, 未命名聚类)。
//
// 一次查询各取一遍，避免 N+1；封面用 array_agg + 下标取最高置信度那张脸。
func (s *Store) ListPeople(ctx context.Context) ([]PersonRow, []ClusterRow, error) {
	prows, err := s.Pool.Query(ctx, `
		SELECT p.id::text,
		       COALESCE(p.name, ''),
		       p.hidden,
		       p.is_pet,
		       count(f.id),
		       COALESCE(p.cover_media_id::text,
		                (array_agg(f.media_id::text ORDER BY f.confidence DESC NULLS LAST))[1],
		                '')
		FROM people p
		LEFT JOIN faces f ON f.person_id = p.id
		GROUP BY p.id
		ORDER BY p.hidden, p.name NULLS LAST, p.created_at`)
	if err != nil {
		return nil, nil, err
	}
	defer prows.Close()

	named := []PersonRow{}
	for prows.Next() {
		var r PersonRow
		if err := prows.Scan(&r.ID, &r.Name, &r.Hidden, &r.IsPet, &r.FaceCount, &r.CoverMediaID); err != nil {
			return nil, nil, err
		}
		named = append(named, r)
	}
	if err := prows.Err(); err != nil {
		return nil, nil, err
	}

	crows, err := s.Pool.Query(ctx, `
		SELECT f.cluster_id,
		       count(DISTINCT f.media_id),
		       COALESCE((array_agg(f.media_id::text ORDER BY f.confidence DESC NULLS LAST))[1], '')
		FROM faces f
		WHERE f.person_id IS NULL
		  AND f.cluster_id IS NOT NULL
		  AND f.cluster_id <> ''
		GROUP BY f.cluster_id
		ORDER BY 2 DESC, f.cluster_id`)
	if err != nil {
		return nil, nil, err
	}
	defer crows.Close()

	unnamed := []ClusterRow{}
	for crows.Next() {
		var r ClusterRow
		if err := crows.Scan(&r.ClusterID, &r.Count, &r.Cover); err != nil {
			return nil, nil, err
		}
		unnamed = append(unnamed, r)
	}
	return named, unnamed, crows.Err()
}

// CreatePerson 命名并合并：新建 people 行，把 clusterIDs 指向的人脸回填 person_id。
//
// 事务内完成「建人 → 迁徙人脸 → 回填封面」，中途失败不会留下半合并状态。
func (s *Store) CreatePerson(ctx context.Context, name string, isPet bool, clusterIDs []string) (PersonRow, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return PersonRow{}, ErrEmptyName
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return PersonRow{}, err
	}
	defer tx.Rollback(ctx) // Commit 成功后 Rollback 为 no-op

	var out PersonRow
	if err := tx.QueryRow(ctx, `
		INSERT INTO people (name, is_pet) VALUES ($1, $2)
		RETURNING id::text, COALESCE(name, ''), hidden, is_pet`,
		name, isPet).Scan(&out.ID, &out.Name, &out.Hidden, &out.IsPet); err != nil {
		return PersonRow{}, err
	}

	// 空串 / 重复的 cluster_id 直接过滤，避免 ANY() 里出现无效元素
	ids := make([]string, 0, len(clusterIDs))
	for _, id := range clusterIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		tag, err := tx.Exec(ctx, `
			UPDATE faces SET person_id = $1, is_pet = $2
			WHERE cluster_id = ANY($3)`, out.ID, isPet, ids)
		if err != nil {
			return PersonRow{}, err
		}
		out.FaceCount = int(tag.RowsAffected())
	}

	var cover string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE((array_agg(media_id::text ORDER BY confidence DESC NULLS LAST))[1], '')
		FROM faces WHERE person_id = $1`, out.ID).Scan(&cover); err != nil {
		return PersonRow{}, err
	}
	if cover != "" {
		if _, err := tx.Exec(ctx, `UPDATE people SET cover_media_id = $2::uuid WHERE id = $1`, out.ID, cover); err != nil {
			return PersonRow{}, err
		}
		out.CoverMediaID = cover
	}

	if err := tx.Commit(ctx); err != nil {
		return PersonRow{}, err
	}
	return out, nil
}

// UpdatePerson 改名 / 隐藏（二者可选其一或同时）。nil 表示不改该字段。
func (s *Store) UpdatePerson(ctx context.Context, id string, name *string, hidden *bool) (PersonRow, error) {
	sets := make([]string, 0, 2)
	args := []any{id}
	if name != nil {
		nm := strings.TrimSpace(*name)
		if nm == "" {
			return PersonRow{}, ErrEmptyName
		}
		args = append(args, nm)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if hidden != nil {
		args = append(args, *hidden)
		sets = append(sets, fmt.Sprintf("hidden = $%d", len(args)))
	}
	if len(sets) == 0 {
		return PersonRow{}, ErrNothingToUpdate
	}

	var out PersonRow
	err := s.Pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE people SET %s WHERE id = $1::uuid
		RETURNING id::text, COALESCE(name, ''), hidden, is_pet,
		          (SELECT count(*) FROM faces WHERE person_id = $1::uuid),
		          COALESCE(cover_media_id::text, '')`, strings.Join(sets, ", ")), args...).
		Scan(&out.ID, &out.Name, &out.Hidden, &out.IsPet, &out.FaceCount, &out.CoverMediaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PersonRow{}, ErrPersonNotFound
	}
	if err != nil {
		return PersonRow{}, err
	}
	return out, nil
}

// PersonMedia 该人物出现过的全部媒体（去重，按拍摄时间倒序）。
func (s *Store) PersonMedia(ctx context.Context, personID string, limit int) ([]PersonMediaItem, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id::text, COALESCE(m.filename, ''), m.taken_at
		FROM faces f
		JOIN media m ON m.id = f.media_id
		WHERE f.person_id = $1::uuid AND m.deleted_at IS NULL
		GROUP BY m.id, m.filename, m.taken_at
		ORDER BY m.taken_at DESC NULLS LAST, m.id
		LIMIT $2`, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PersonMediaItem{}
	for rows.Next() {
		var it PersonMediaItem
		var taken *time.Time
		if err := rows.Scan(&it.ID, &it.Filename, &taken); err != nil {
			return nil, err
		}
		if taken != nil {
			it.TakenAt = taken.UTC().Format(time.RFC3339)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ResetScanned 触发重算：把扫描标记清空，让 facesgen 的清扫循环下一轮重新处理。
//
// 契约 §12 的 POST /ai/faces 只要求返回 job_id；因为人脸扫描是**清扫式**增量
// （见 facesgen -mode watch），这里不做入队，而是复位标记——等价且不会产生
// 「队列消费者不认识的消息类型」问题。
//
// **本函数只复位标记，不删 faces 行——这是刻意的**：
//   - 替换旧人脸是**扫描方**的职责，scanOne 已经是「先删后整体重写」的幂等操作
//     （见 cmd/facesgen/main.go）。若这里也删一次，等于把同一件事做两遍。
//   - 复位是**可逆**的（下一轮清扫会把数据算回来），删除是**不可逆**的。若在此处删，
//     一旦 faces-worker 未在运行或模型加载失败，用户会立刻看到人脸全空且长期不恢复；
//     而只复位标记时，读接口在整个窗口期内仍返回上一轮结果，不会出现"空档"。
//   - 只在这里删也修不掉重复累积：重复的根因是插入方没有先删，删在触发方不改变插入语义。
//
// 返回值 = 被复位的**媒体条数**（scope=all 时为"有 LG 缩略图且未删除"的媒体数），
// 注意它既不是人脸数、也不是本次重扫会产出的人脸数（见 api.go TriggerScan 的字段命名说明）。
//
// mediaID 为空或 "all" 表示全量。
func (s *Store) ResetScanned(ctx context.Context, mediaID string) (int64, error) {
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" || mediaID == "all" {
		tag, err := s.Pool.Exec(ctx, `
			UPDATE media SET faces_scanned_at = NULL
			WHERE deleted_at IS NULL AND COALESCE(thumbnail_lg, '') <> ''`)
		if err != nil {
			return 0, err
		}
		return tag.RowsAffected(), nil
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE media SET faces_scanned_at = NULL WHERE id = $1::uuid`, mediaID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
