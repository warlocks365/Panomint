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
	"panoalbum/internal/mediascope"
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
	// ErrMediaNotAccessible 请求的 media_ids 里含相册属主不可见的媒体（他人个人空间的媒体、
	// 或不存在/已删除的 id）。整笔拒绝，绝不部分插入。
	ErrMediaNotAccessible = errors.New("含不可访问的媒体")
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

// MediaRef 的列清单与扫描器**不再在本包维护** —— 唯一真源在 internal/media/mediaref.go。
// 这里原先的 mediaCols 注释写着"与 internal/media/timeline.go 保持一致"，而它其实是裸选
// 可空列的那一份：注释承诺同步，机制上却没有任何东西保证它。实测后果是 GET /albums/:id
// 在任意一行 filename/folder_path 为 NULL 时整响应 500。

// scanMediaRows 扫描多行媒体为 MediaRef（扫描器见 media.ScanMediaRef）。
func scanMediaRows(rows pgx.Rows) ([]media.MediaRef, error) {
	defer rows.Close()
	out := []media.MediaRef{}
	for rows.Next() {
		it, err := media.ScanMediaRef(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
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

// listAlbumsSQL 相册列表：**仅本人**的相册。
//
// favorites 是**每人**的系统相册（media.write 按 owner_id 建/取，见 media/write.go:29），
// **不是**全站共享相册。旧写法 `WHERE a.owner_id = $1 OR a.type = 'favorites'` 的后半段
// 会命中全站所有人的收藏相册，把他人相册的名称/描述/封面/计数一起列出来 —— 等于全站台账。
// 这里收窄为仅本人；ORDER BY 仍把 favorites 置顶（列表内排序，与可见性无关）。
const listAlbumsSQL = `
	SELECT a.id, a.name, a.type, COALESCE(a.description,''), a.cover_media_id, a.updated_at, a.owner_id
	FROM albums a
	WHERE a.owner_id = $1
	ORDER BY a.type = 'favorites' DESC, a.updated_at DESC`

// List 相册列表：本人相册；含首图回填与媒体计数。
func (s *Store) List(ctx context.Context, userID string) ([]Summary, error) {
	rows, err := s.Pool.Query(ctx, listAlbumsSQL, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawAlbum struct {
		Summary
		typ     string
		ownerID string
	}
	var raws []rawAlbum
	for rows.Next() {
		var r rawAlbum
		if err := rows.Scan(&r.ID, &r.Name, &r.typ, &r.Description, &r.CoverMediaID, &r.UpdatedAt, &r.ownerID); err != nil {
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
			// 智能相册：条件实时计数 + 取首项（与详情同序 taken_at DESC）。
			// 属主传**相册属主**（r.ownerID），不是调用者 —— 本函数虽然只列本人相册，
			// 但口径必须与 Get/分享链路一致，不能依赖"调用者==属主"这个巧合。
			c, err := s.getCriteria(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			where, args := buildCriteriaWhere(c, r.ownerID)
			if err := s.Pool.QueryRow(ctx, `SELECT count(*)::int FROM media m WHERE `+where, args...).Scan(&sum.MediaCount); err != nil {
				return nil, err
			}
			_ = s.Pool.QueryRow(ctx, `SELECT m.id FROM media m WHERE `+where+`
				ORDER BY m.taken_at DESC, m.id DESC LIMIT 1`, args...).Scan(&firstID)
		} else {
			// 读路径与写入端同一口径：按**相册属主**的可见集过滤。
			// AddItems 的修复只保证"以后不会再塞进来"，挡不住库里已存在的历史行
			// （相册里的他人 media）—— 读路径过滤是兜住历史数据的唯一办法。
			vis, visArgs := mediascope.VisibleCondFor(2, r.ownerID, "m")
			args := append([]any{r.ID}, visArgs...)
			if err := s.Pool.QueryRow(ctx,
				`SELECT count(*)::int FROM album_items ai JOIN media m ON m.id = ai.media_id
				 WHERE ai.album_id = $1 AND m.deleted_at IS NULL AND `+vis, args...).Scan(&sum.MediaCount); err != nil {
				return nil, err
			}
			_ = s.Pool.QueryRow(ctx, `
				SELECT ai.media_id FROM album_items ai JOIN media m ON m.id = ai.media_id
				WHERE ai.album_id = $1 AND m.deleted_at IS NULL AND `+vis+`
				ORDER BY ai.sort_key ASC, m.taken_at DESC LIMIT 1`, args...).Scan(&firstID)
		}
		sum.CoverMediaID = effectiveCover(sum.CoverMediaID, firstID)
		if sum.CoverMediaID != nil {
			// cover_media_id 可被 PATCH 设成任意 media id：取缩略图前也要过可见集，
			// 否则 List 会把他人 media 的 thumbnail_sm 直接交给调用者。
			thumbVis, thumbVisArgs := mediascope.VisibleCondFor(2, r.ownerID, "m")
			thumbArgs := append([]any{*sum.CoverMediaID}, thumbVisArgs...)
			_ = s.Pool.QueryRow(ctx,
				`SELECT thumbnail_sm FROM media m WHERE m.id = $1 AND m.deleted_at IS NULL AND `+thumbVis,
				thumbArgs...).Scan(&sum.CoverMediaThumb)
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
		// 属主传相册属主（d.OwnerID）：公开分享链路（shares.ListItems）在这里是**匿名**调用，
		// 绝不能改用调用者身份，否则匿名分享会被 fail-closed 打死。
		where, args := buildCriteriaWhere(d.Criteria, d.OwnerID)
		rows, err := s.Pool.Query(ctx, `SELECT `+media.MediaRefColumns+` FROM media m WHERE `+where+`
			ORDER BY m.taken_at DESC, m.id DESC`, args...)
		if err != nil {
			return nil, err
		}
		d.Items, err = scanMediaRows(rows)
		if err != nil {
			return nil, err
		}
	} else {
		// 读路径按**相册属主** d.OwnerID 收窄可见集，兜住历史脏行（AddItems 修复前的越权插入）。
		//
		// ⚠️ 主体必须是相册属主而不是调用者：本函数同时服务公开分享链路
		// （shares.Store.ListItems → 本函数），那条链路是**匿名**的、根本没有调用者身份，
		// 且分享是**有意**让匿名可见的。用调用者身份会把匿名分享整条 fail-closed 打死。
		vis, visArgs := mediascope.VisibleCondFor(2, d.OwnerID, "m")
		args := append([]any{id}, visArgs...)
		rows, err := s.Pool.Query(ctx, `
			SELECT `+media.MediaRefColumns+`
			FROM album_items ai JOIN media m ON m.id = ai.media_id
			WHERE ai.album_id = $1 AND m.deleted_at IS NULL AND `+vis+`
			ORDER BY ai.sort_key ASC, m.taken_at DESC`, args...)
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

// addItemsQueries 生成 AddItems 的两条语句（抽成纯函数以便被单测钉住）。
//
// 为什么需要"先校验、再插入"两条语句，而不是靠 INSERT 的 RowsAffected：
//   - `ON CONFLICT DO NOTHING` 会把"这条媒体已经在相册里"也计成 0 行，
//     与"这条媒体不可访问"无法区分 —— 用行数判断会把正常的重复添加误判成越权；
//   - 反过来，若能区分却只插入了一部分，调用方收到的 {added:N<请求数} 会被误读为"全成功"。
//
// 因此：先用 checkSQL 数出"请求的 id 中落在相册属主可见集内的个数"，与请求数不等就整笔拒绝；
// 相等才执行 insertSQL。两条语句在**同一个事务**里（见 AddItems）。
//
// 占位符约定（与 mediascope 的调用约定一致，argv 个数必须自洽）：
//   - checkSQL ：$1 = media id 数组；可见性谓词从 $2 起编号；
//   - insertSQL：$1 = 相册 id，$2 = media id 数组；可见性谓词从 $3 起编号；
//     insertSQL 里那个 `max(sort_key) WHERE album_id = $1` 的 $1 就是相册 id，不另占位。
//
// ⚠️ 谓词主体是**相册属主 albumOwnerID**，不是调用者：
//   - canManage 允许 owner/admin 管理他人相册，调用者未必是相册属主；
//   - 读路径（Get/List/匿名分享）就是以相册属主为口径 —— 写入集合必须等于读回集合；
//   - 匿名分享链路没有调用者身份，若写入端用调用者口径，读回集合会与之不一致。
//
// ⚠️ albumOwnerID=="" 时 mediascope 返回恒假且**不占参数位**：此时 checkSQL 恒数出 0，
// AddItems 必然整笔拒绝（不会写出任何行）。
func addItemsQueries(albumID, albumOwnerID string, uniq []string) (checkSQL string, checkArgs []any, insertSQL string, insertArgs []any) {
	visCheck, visCheckArgs := mediascope.VisibleCondFor(2, albumOwnerID, "m")
	checkSQL = `
		SELECT count(*)::int
		FROM unnest($1::uuid[]) AS u(mid)
		JOIN media m ON m.id = u.mid
		WHERE m.deleted_at IS NULL AND ` + visCheck
	checkArgs = append([]any{uniq}, visCheckArgs...)

	visInsert, visInsertArgs := mediascope.VisibleCondFor(3, albumOwnerID, "m")
	insertSQL = `
		INSERT INTO album_items (album_id, media_id, sort_key)
		SELECT $1, m.id,
		       COALESCE((SELECT max(sort_key) FROM album_items WHERE album_id = $1), 0)
		       + ROW_NUMBER() OVER (ORDER BY u.ord)::int
		FROM unnest($2::uuid[]) WITH ORDINALITY AS u(mid, ord)
		JOIN media m ON m.id = u.mid
		WHERE m.deleted_at IS NULL AND ` + visInsert + `
		ON CONFLICT (album_id, media_id) DO NOTHING`
	insertArgs = append([]any{albumID, uniq}, visInsertArgs...)
	return checkSQL, checkArgs, insertSQL, insertArgs
}

// AddItems 批量加入媒体（去重；返回实际新增数）。
//
// albumOwnerID 由调用点（handler 从 getAlbumMeta 拿到的相册属主）显式传入，**不**从请求上下文取：
// Store 层没有 gin 上下文，而这一层也正因此不可能"偷偷改用调用者身份"
// （与 internal/albums/criteria.go 的 buildCriteriaWhere 同一风格、同一理由：属主显式入参）。
// 注：这里刻意不写带括号的 buildCriteriaWhere 调用形态 —— 同包的
// TestCriteriaCallSitesPassAlbumOwner 用它统计真实调用点，注释里出现该形态会造成假命中。
//
// 语义：请求里只要有一个 media id 不在相册属主的可见集内，就**整笔拒绝**（ErrMediaNotAccessible），
// 不插入任何一行 —— 修复前这里只校验相册归属，随后把任意 media_id 塞进 album_items，
// 使他人 media 可经 GET /albums/:id 与**匿名公开分享**读回 filename/thumbnail/place/taken_at。
func (s *Store) AddItems(ctx context.Context, albumID, albumOwnerID string, mediaIDs []string) (int64, error) {
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

	checkSQL, checkArgs, insertSQL, insertArgs := addItemsQueries(albumID, albumOwnerID, uniq)

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 可见性校验必须在插入**之前**：只要有一项不可访问就整笔不做。
	var accessible int
	if err := tx.QueryRow(ctx, checkSQL, checkArgs...).Scan(&accessible); err != nil {
		return 0, fmt.Errorf("校验媒体可见性失败: %w", err)
	}
	if accessible != len(uniq) {
		return 0, fmt.Errorf("%w：请求 %d 个，其中 %d 个不在该相册属主的可见范围内",
			ErrMediaNotAccessible, len(uniq), len(uniq)-accessible)
	}

	// sort_key 续排（按加入顺序）；已存在条目 ON CONFLICT 跳过。
	tag, err := tx.Exec(ctx, insertSQL, insertArgs...)
	if err != nil {
		return 0, fmt.Errorf("加入媒体失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
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
