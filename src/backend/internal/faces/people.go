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

	"panoalbum/internal/mediascope"
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

// listPeopleQueries 组装 GET /people 的两条 SQL 与参数（抽出为纯函数以便单测直接断言
// 「生成的 SQL 文本里确实有可见性谓词」与「占位符最高编号 == len(args)」）。
//
// 为什么要抽出来测：可见性谓词的占位符编号错位**只会在运行期**被 pgx 以
// "expected N arguments" 拒绝（编译期毫无提示）；而谓词整段被漏掉时 SQL 不报错、
// 只是多返回数据 —— 这正是本项目已发生过的越权形态。故用文本断言钉住。
//
// 收窄范围（只收窄 media 派生字段，人物行本身全量返回，理由见 ListPeople 的注释）：
//   - named.face_count       : 只统计「人脸所属媒体对调用者可见」的人脸
//   - named.cover_media_id   : 封面媒体不可见时不再返回，退化为该人物可见人脸里置信度最高者
//   - unnamed.count / cover  : 聚类只由「可见媒体」上的人脸聚合而来
//
// ⚠️ 顺带收窄（刻意，非疏漏）：两条查询都加了 `m.deleted_at IS NULL`。
// 改造前 named 完全不看 deleted_at，unnamed 也不看，于是回收站里的照片仍计入
// face_count / count，而 GET /people/:id/media 早就排除了软删媒体 —— 同一张脸在两处
// 说法不一致。现在两处口径统一：软删媒体不算「调用者可见」。
//
// 占位符约定：谓词实例一律绑 **同一个 $1 = userID**（personal 臂与 shared 臂、
// 媒体侧与封面侧共用），因此 args 恰好 1 个元素；userID 为空时谓词是恒假 "false"、
// args 为空。两条 SQL 的 args 因此可以共用同一个切片 —— 调用方只读不写。
func listPeopleQueries(userID string) (namedSQL string, namedArgs []any, unnamedSQL string, unnamedArgs []any) {
	// 同一身份的两个实例：m = 人脸所属媒体，mc = 人物显式封面媒体。
	visMedia, args := mediascope.VisibleCondFor(1, userID, "m")
	visCover, _ := mediascope.VisibleCondFor(1, userID, "mc")

	// 可见性挂在 faces 的 LEFT JOIN 条件里（而不是 media 的 JOIN 上）：
	// 不可见的媒体会让**人脸行**整体不参与聚合，于是 count(f.id) 与
	// array_agg(f.media_id) 无需改动就自然只覆盖可见媒体；而因为仍是 LEFT JOIN，
	// 「可见媒体为 0 的人物」依然会出现在名单里（计数 0、封面空），不会凭空消失。
	namedSQL = fmt.Sprintf(`
		SELECT p.id::text,
		       COALESCE(p.name, ''),
		       p.hidden,
		       p.is_pet,
		       count(f.id),
		       COALESCE((SELECT p.cover_media_id::text
		                 FROM media mc
		                 WHERE mc.id = p.cover_media_id
		                   AND mc.deleted_at IS NULL
		                   AND %[2]s),
		                (array_agg(f.media_id::text ORDER BY f.confidence DESC NULLS LAST))[1],
		                '')
		FROM people p
		LEFT JOIN faces f
		       ON f.person_id = p.id
		      AND EXISTS (SELECT 1
		                  FROM media m
		                  WHERE m.id = f.media_id
		                    AND m.deleted_at IS NULL
		                    AND %[1]s)
		GROUP BY p.id
		ORDER BY p.hidden, p.name NULLS LAST, p.created_at`, visMedia, visCover)

	// 未命名聚类：聚类本身就是从人脸聚合出来的，不可见媒体上的人脸整体不参与，
	// 因此这里用 INNER JOIN（一条可见人脸都没有的聚类直接不出现，这是期望行为）。
	unnamedSQL = fmt.Sprintf(`
		SELECT f.cluster_id,
		       count(DISTINCT f.media_id),
		       COALESCE((array_agg(f.media_id::text ORDER BY f.confidence DESC NULLS LAST))[1], '')
		FROM faces f
		JOIN media m ON m.id = f.media_id
		            AND m.deleted_at IS NULL
		            AND %[1]s
		WHERE f.person_id IS NULL
		  AND f.cluster_id IS NOT NULL
		  AND f.cluster_id <> ''
		GROUP BY f.cluster_id
		ORDER BY 2 DESC, f.cluster_id`, visMedia)

	return namedSQL, args, unnamedSQL, args
}

// ListPeople 返回 (已命名人物, 未命名聚类)。
//
// 一次查询各取一遍，避免 N+1；封面用 array_agg + 下标取最高置信度那张脸。
//
// ⚠️ 遗留建模问题（**未决，需用户裁决**；不是这里的遗漏）：
// people 表**没有 owner / space 列**（见 migrations/00005_ddl_part.sql），因此
// 「人物库是全站共享的一套元数据，还是每个用户各有一套」在数据模型上没有答案。
// 因此本函数（以及 listPeopleQueries）**刻意不按属主过滤 people 行**，
// 只把**暴露 media 内容的字段**收窄到调用者可见的媒体集合：
//
//	named.face_count / named.cover_media_id / unnamed.count / unnamed.cover
//
// 即：调用者仍能看到「别人命名过的人物名」（纯元数据），但看不到任何指向他人媒体的
// 计数与媒体 ID。若日后裁决为"每人一套人物库"，需要的是给 people 加 owner 列并迁移
// 存量数据 —— 那是**另一次 schema 变更**，不要误以为本轮已经处理完了。
//
// userID 为调用者身份（handler 取自上下文 user_id）。为空时谓词退化为恒假（fail-closed），
// 于是人物名册仍在、但计数全为 0、封面全为空、unnamed 为空 —— 宁可空结果也不放宽。
func (s *Store) ListPeople(ctx context.Context, userID string) ([]PersonRow, []ClusterRow, error) {
	namedSQL, namedArgs, unnamedSQL, unnamedArgs := listPeopleQueries(userID)

	prows, err := s.Pool.Query(ctx, namedSQL, namedArgs...)
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

	crows, err := s.Pool.Query(ctx, unnamedSQL, unnamedArgs...)
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

// personMediaQuery 组装 PersonMedia 的 SQL 与参数（抽出为纯函数以便单测断言
// 「WHERE 里确实有可见性谓词」与「占位符最高编号 == len(args)」）。
//
// 占位符布局：$1 = personID（查询原本就有的参数），可见性谓词从 **$2** 起
// （personal 臂与 shared 臂共用 $2），LIMIT 取紧随其后的编号。
// ⚠️ 编号错位只会在运行期被 pgx 报 "expected N arguments"，编译期发现不了，
// 故 LIMIT 的编号按 len(args) 动态算，userID 为空（fail-closed，谓词是 "false"
// 且不占参数位）时 LIMIT 自然前移为 $2，始终与 args 个数自洽。
func personMediaQuery(personID, userID string, limit int) (string, []any) {
	vis, vargs := mediascope.VisibleCondFor(2, userID, "m")
	args := append([]any{personID}, vargs...)
	args = append(args, limit)
	sql := fmt.Sprintf(`
		SELECT m.id::text, COALESCE(m.filename, ''), m.taken_at
		FROM faces f
		JOIN media m ON m.id = f.media_id
		WHERE f.person_id = $1::uuid AND m.deleted_at IS NULL AND %s
		GROUP BY m.id, m.filename, m.taken_at
		ORDER BY m.taken_at DESC NULLS LAST, m.id
		LIMIT $%d`, vis, len(args))
	return sql, args
}

// PersonMedia 该人物出现过的**调用者可见**媒体（去重，按拍摄时间倒序）。
//
// userID 为调用者身份（handler 取自上下文 user_id）。可见性条件来自
// internal/mediascope（唯一真源），因此只持 media:read 的 viewer 也**只能**看到
// 自己（以及自己所属共享空间）的媒体 —— 这正是本函数此前的越权点：
// 改造前的 WHERE 只有 `f.person_id = $1 AND m.deleted_at IS NULL`，
// 没有任何属主/空间条件，任一带 media:read 的账号只要知道人物 id 就能拿到
// 他人的全部媒体 ID 与文件名（已用真实第二账号实测复现）。
func (s *Store) PersonMedia(ctx context.Context, personID, userID string, limit int) ([]PersonMediaItem, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	sql, args := personMediaQuery(personID, userID, limit)
	rows, err := s.Pool.Query(ctx, sql, args...)
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
