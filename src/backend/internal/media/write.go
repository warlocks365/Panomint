package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"panoalbum/internal/mediascope"
)

// 媒体写操作：收藏 / 评级 / 软删 / 回收站（API v1.1 §3）。

// ownerOf 查询媒体属主（含回收站中的行）。
func (s *Store) ownerOf(ctx context.Context, id string) (ownerID string, deleted bool, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT owner_id, deleted_at IS NOT NULL FROM media WHERE id = $1`, id).
		Scan(&ownerID, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrNotFound
	}
	return ownerID, deleted, err
}

// readAccessOf 单条媒体「读」访问判定原语（Detail/Thumb/Download 共用；P2-01）。
//
// 与 ownerOf 的分工：ownerOf 只回答「属主是谁」，是**写路径**的判定输入
// （写仍仅限属主/owner/admin）；读访问的口径是
// 「属主 ∪（media.space='shared' 且调用者是共享空间成员或属主）∪ owner/admin 角色」，
// 与 GET /media?space=shared 的列表口径一致 —— 谓词出自 mediascope.ReadCond
// （**单一真源**，本包不再手写成员判定）。
//
// 媒体不存在 → ErrNotFound（与 ownerOf 同形，畸形 id 的 22P02 原样透出由上层映射）；
// 无权 → allowed=false（403 还是 404 由调用方按端点的存在性预言机策略决定）。
func (s *Store) readAccessOf(ctx context.Context, id, userID, role string) (ownerID string, deleted, allowed bool, err error) {
	cond, args := mediascope.ReadCond(2, userID, role, "")
	err = s.Pool.QueryRow(ctx,
		`SELECT owner_id, deleted_at IS NOT NULL, `+cond+` FROM media WHERE id = $1`,
		append([]any{id}, args...)...).Scan(&ownerID, &deleted, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, false, ErrNotFound
	}
	return ownerID, deleted, allowed, err
}

// readAllowed 读访问的布尔形态（缩略图判定用：无权与不存在都必须收敛为同一个 404）。
func (s *Store) readAllowed(ctx context.Context, id, userID, role string) (bool, error) {
	_, _, allowed, err := s.readAccessOf(ctx, id, userID, role)
	return allowed, err
}

// SetFavorite 收藏切换：favorites 相册成员模型（DDL 无 is_favorite 字段，与 timeline 过滤一致）。
func (s *Store) SetFavorite(ctx context.Context, id, userID string, fav bool) error {
	// 找到或创建当前用户的收藏相册
	var albumID string
	err := s.Pool.QueryRow(ctx,
		`SELECT id FROM albums WHERE owner_id = $1 AND type = 'favorites' LIMIT 1`, userID).Scan(&albumID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.Pool.QueryRow(ctx,
			`INSERT INTO albums (name, type, owner_id) VALUES ('收藏', 'favorites', $1) RETURNING id`,
			userID).Scan(&albumID)
	}
	if err != nil {
		return err
	}
	if fav {
		_, err = s.Pool.Exec(ctx,
			`INSERT INTO album_items (album_id, media_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			albumID, id)
	} else {
		_, err = s.Pool.Exec(ctx,
			`DELETE FROM album_items WHERE album_id = $1 AND media_id = $2`, albumID, id)
	}
	return err
}

// SetRating 评级（0-5 星）。
func (s *Store) SetRating(ctx context.Context, id string, rating int) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET rating = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, rating, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNotes 更新用户备注（仅 notes 字段；空串即清空）。
func (s *Store) SetNotes(ctx context.Context, id string, notes string) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET notes = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, notes, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CropRect 归一化裁剪框（相对原图，0..1）。
type CropRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Edits 非破坏式基本编辑参数（存 media.edits JSONB；原文件不变）。
// Rotate ∈ {0,90,180,270}；Crop 为 nil 表示未裁剪。
type Edits struct {
	Rotate int       `json:"rotate"`
	Crop   *CropRect `json:"crop,omitempty"`
}

// ErrBadEdits 编辑参数非法。
var ErrBadEdits = errors.New("编辑参数非法")

// NormalizeEdits 校验并规范化编辑参数（拒绝未知字段）。
// rotate 需为 0/90/180/270；crop 归一化且不得越界。
func NormalizeEdits(raw []byte) (*Edits, error) {
	var e Edits
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return nil, ErrBadEdits
	}
	switch e.Rotate {
	case 0, 90, 180, 270:
	default:
		return nil, ErrBadEdits
	}
	if e.Crop != nil {
		c := e.Crop
		if c.W <= 0 || c.H <= 0 || c.X < 0 || c.Y < 0 {
			return nil, ErrBadEdits
		}
		if c.X+c.W > 1.0001 || c.Y+c.H > 1.0001 {
			return nil, ErrBadEdits
		}
		// 规整到 [0,1]，消除浮点边界误差
		if c.X < 0 {
			c.X = 0
		}
		if c.Y < 0 {
			c.Y = 0
		}
		if c.X+c.W > 1 {
			c.W = 1 - c.X
		}
		if c.Y+c.H > 1 {
			c.H = 1 - c.Y
		}
	}
	return &e, nil
}

// SetEdits 写入非破坏式编辑参数（JSONB）；edits 为 nil 即清空（重置）。
func (s *Store) SetEdits(ctx context.Context, id string, edits *Edits) error {
	var raw any
	if edits != nil {
		b, err := json.Marshal(edits)
		if err != nil {
			return err
		}
		raw = b
	}
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET edits = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, raw, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetEdits 读取媒体当前的编辑参数。无编辑参数返回 (nil, nil)；媒体不存在/已删除返回 ErrNotFound。
// 列内容无法反序列化时按"无编辑"处理——脏数据不应让旋转接口整体 500。
func (s *Store) GetEdits(ctx context.Context, id string) (*Edits, error) {
	var raw []byte
	err := s.Pool.QueryRow(ctx,
		`SELECT edits FROM media WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var e Edits
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, nil
	}
	return &e, nil
}

// MergeRotateEdit 读-改-写合并：只更新 rotate，保留既有 crop。
// POST /media/:id/rotate 的 op=rotate 走此路径，避免整体覆盖抹掉用户的裁剪框。
// 纯函数，不触库（便于单测）。
func MergeRotateEdit(cur *Edits, angle int) *Edits {
	out := &Edits{Rotate: angle}
	if cur != nil {
		out.Crop = cur.Crop
	}
	return out
}

// MergeCropEdit 读-改-写合并：只更新 crop，保留既有 rotate。
// op=crop 走此路径，避免把旋转角度重置为 0。纯函数，不触库（便于单测）。
func MergeCropEdit(cur *Edits, crop *CropRect) *Edits {
	out := &Edits{Crop: crop}
	if cur != nil {
		out.Rotate = cur.Rotate
	}
	return out
}

// SoftDelete 软删（入回收站）。
func (s *Store) SoftDelete(ctx context.Context, id string) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE media SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore 从回收站恢复；返回该媒体的 path，供调用方在审计 detail 里留可解析线索。
//
// 与 Purge 同形（都用 RETURNING path），理由见 §二十四.7：恢复**本身**不销毁任何东西，
// 但恢复之后这行**仍可能被 purge** —— 那一刻 `target_id` 就再也查不回内容了
// （GetDetail 只会说「不存在或已删除」）。path 是这条媒体在库里最后的可解析线索，
// 而**只有在恢复的当口顺手取出来才拿得到**：删掉的行读不回来，事后无法补记。
// 键名 path 不含任何敏感子串，不会被 RedactDetail 剔除。
func (s *Store) Restore(ctx context.Context, id string) (string, error) {
	var path string
	err := s.Pool.QueryRow(ctx,
		`UPDATE media SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL RETURNING path`,
		id).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return path, err
}

// Purge 永久删除（回收站中）；返回媒体 path（供调用方清理文件与审计）
// 与顺带解除的引用数（供审计 detail 记「顺带解除 N 处引用」）。
//
// 外键背景（P1-02）：albums.cover_media_id / people.cover_media_id /
// media.duplicate_of / media.live_photo_pair_id 四处曾是无 ON DELETE 规则的
// NO ACTION 外键，被引用的媒体 DELETE 必撞 23503（回收站「删不掉」）。
// 迁移 00027 已把四处改为 ON DELETE SET NULL；这里仍在**同一事务**里显式
// UPDATE 置 NULL，原因有二：
//  1. 解除的引用计数要写进审计 detail，DB 级联拿不到计数；
//  2. 对迁移尚未应用的部署兜底 —— 显式解引用后 DELETE 不再依赖 FK 规则。
func (s *Store) Purge(ctx context.Context, id string) (path string, refsCleared int, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(ctx) // 提交成功后 Rollback 是 no-op（pgx 约定）
	for _, q := range []string{
		`UPDATE albums SET cover_media_id = NULL WHERE cover_media_id = $1`,
		`UPDATE people SET cover_media_id = NULL WHERE cover_media_id = $1`,
		`UPDATE media SET duplicate_of = NULL WHERE duplicate_of = $1`,
		`UPDATE media SET live_photo_pair_id = NULL WHERE live_photo_pair_id = $1`,
	} {
		ct, err := tx.Exec(ctx, q, id)
		if err != nil {
			return "", 0, err
		}
		refsCleared += int(ct.RowsAffected())
	}
	err = tx.QueryRow(ctx,
		`DELETE FROM media WHERE id = $1 AND deleted_at IS NOT NULL RETURNING path`, id).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return path, refsCleared, nil
}

// ListTrash 回收站列表（按删除时间倒序，上限 500 条）。
//
// 列与扫描器走 mediaref.go 的唯一真源。**行为变化（有意）**：本函数原先只选 13 列、
// **没有 folder_path**，于是回收站的 folder_path 恒为 ""（与列表页不一致，属历史漂移）。
// 收拢后回收站也带上真实 folder_path —— 这是"同一份清单"的必然结果，
// 若保留旧的 13 列，就又是一份私有副本。
//
// ⚠️ WHERE 口径说明（**有意保持现状，勿为「与 scopeConds 一致」而改**）：
// 本查询是 `deleted_at IS NOT NULL AND owner_id = $1`，**已绑定调用者**（$1 = 当前 user_id），
// 不返回任何他人媒体，不存在越权泄漏。它与 scopeConds（/media 系列）相比确实少了
// `space = 'personal'` 一臂，但那不是遗漏：
//   - 回收站的语义是「**我的**已删媒体」，不是「某个空间的已删媒体」；
//   - 若补上 space = 'personal'，我在共享空间里删掉的照片会从回收站里**消失**，
//     而恢复接口按 id 走、用户已看不到它，于是**再也无法恢复** —— 这是实打实的功能回退；
//   - 该改动换不来任何安全收益（查询本就 owner-bound），纯负收益。
//
// 因此这里保留现状。将来若要改，必须先有「共享空间回收站」的产品语义，而不是"一致性"。
func (s *Store) ListTrash(ctx context.Context, ownerID string) (*ListResult, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT `+MediaRefColumns+`
		FROM media m
		WHERE m.deleted_at IS NOT NULL AND m.owner_id = $1
		ORDER BY m.deleted_at DESC LIMIT 500`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &ListResult{Items: []MediaRef{}, Buckets: []Bucket{}}
	for rows.Next() {
		it, err := ScanMediaRef(rows)
		if err != nil {
			return nil, err
		}
		res.Items = append(res.Items, it)
	}
	res.Total = len(res.Items)
	return res, rows.Err()
}
