package media

// Job000066 批量操作存储层：归属一次收敛 + 各 op 的批量 SQL。
// 所有语句限定 id=ANY($1) 且调用前已经 batchOwnedIDs 过滤——此处不再重复 owner 谓词。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"panoalbum/internal/mediascope"
)

// batchOwnedIDs 一次查询收敛归属：返回有权且未删的 id 集 + 无权/不存在/已删的 failed 列表。
// 无权与不存在同形（不泄露存在性），reason 用统一文案。
func (s *Store) batchOwnedIDs(ctx context.Context, ids []string, userID string) (allowed []string, failed []map[string]any) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id::text FROM media WHERE id = ANY($1) AND owner_id = $2 AND deleted_at IS NULL`, ids, userID)
	if err != nil {
		return nil, []map[string]any{{"id": "*", "reason": "查询失败"}}
	}
	ok := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ok[id] = true
			allowed = append(allowed, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, []map[string]any{{"id": "*", "reason": "查询失败"}}
	}
	for _, id := range ids {
		if !ok[id] {
			failed = append(failed, map[string]any{"id": id, "reason": "不存在或无权操作"})
		}
	}
	return allowed, failed
}

// BatchSoftDelete 软删（回收站语义，幂等可重试）。
func (s *Store) BatchSoftDelete(ctx context.Context, ids []string) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		`UPDATE media SET deleted_at = now(), updated_at = now()
		 WHERE id = ANY($1) AND deleted_at IS NULL`, ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// BatchSetFolder 移动到目标目录（空串=根目录）。
func (s *Store) BatchSetFolder(ctx context.Context, ids []string, folder string) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		`UPDATE media SET folder_path = $1, updated_at = now() WHERE id = ANY($2)`, folder, ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// BatchSetTags 批量附加/移除标签（ON CONFLICT 幂等）。
func (s *Store) BatchSetTags(ctx context.Context, ids []string, tagIDs []string, add bool) (int, error) {
	var opErr error
	count := 0
	if add {
		_, opErr = s.Pool.Exec(ctx,
			`INSERT INTO media_tags (media_id, tag_id, confirmed, origin)
			 SELECT m.id, t.id, true, 'user' FROM unnest($1::uuid[]) m(id), unnest($2::uuid[]) t(id)
			 ON CONFLICT (media_id, tag_id) DO NOTHING`, ids, tagIDs)
		if opErr == nil {
			count = len(ids)
		}
	} else {
		_, opErr = s.Pool.Exec(ctx,
			`DELETE FROM media_tags WHERE media_id = ANY($1) AND tag_id = ANY($2)`, ids, tagIDs)
		if opErr == nil {
			count = len(ids)
		}
	}
	return count, opErr
}

// BatchSetMeta 批量改拍摄时间/地点（setTakenAt/setPlace 为 false 表示该字段不动；值 nil=清空）。
func (s *Store) BatchSetMeta(ctx context.Context, ids []string, takenAt any, setTakenAt bool, place any, setPlace bool) (int, error) {
	sets := []string{"updated_at = now()"}
	args := []any{ids}
	if setTakenAt {
		args = append(args, takenAt)
		sets = append(sets, fmt.Sprintf("taken_at = $%d", len(args)))
	}
	if setPlace {
		args = append(args, place)
		sets = append(sets, fmt.Sprintf("place = $%d", len(args)))
	}
	if len(sets) == 1 {
		return 0, nil // 无字段可改
	}
	tag, err := s.Pool.Exec(ctx,
		fmt.Sprintf(`UPDATE media SET %s WHERE id = ANY($1)`, strings.Join(sets, ", ")), args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// copiedRow 复制源行（copySourceRow 的载体）。
type copiedRow struct {
	id       string
	typ      string
	filename string
	folder   string
}

// copySourceRow 读复制源行（含相对 path）。
func (s *Store) copySourceRow(ctx context.Context, id string) (*copiedRow, string, error) {
	r := &copiedRow{}
	var rel string
	err := s.Pool.QueryRow(ctx,
		`SELECT id::text, type::text, filename, COALESCE(folder_path,''), path
		 FROM media WHERE id = $1`, id).Scan(&r.id, &r.typ, &r.filename, &r.folder, &rel)
	if err != nil {
		return nil, "", err
	}
	return r, rel, nil
}

// insertCopiedRow 写入副本行（新 id；path/filename 用新值；folder 为副本目标目录
// （Job000100：copy 带 folder_path 目标时由调用方传入目标而非源目录）；副本继承缩略图引用与 hash——
// 缩略图文件同名引用对新行同样有效（同 id 命名？缩略图名含旧 id）→ 缩略图置空重生成，保证一致）。
func (s *Store) insertCopiedRow(ctx context.Context, r *copiedRow, ownerID, newRel, storedName, folder string) (string, error) {
	var newID string
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO media (type, space, owner_id, path, folder_path, filename,
			taken_at, width, height, duration, codec, fps,
			gps, hash, filesize, camera_make, camera_model, is_360, projection, place)
		 SELECT $1, 'personal', $2, $3, $4, $5,
		        taken_at, width, height, duration, codec, fps,
		        gps, hash, filesize, camera_make, camera_model, is_360, projection, place
		 FROM media WHERE id = $6
		 RETURNING id`,
		r.typ, ownerID, newRel, folder, storedName, r.id).Scan(&newID)
	return newID, err
}

// BatchSetSpace 个人空间 → 共享空间（共享给家人协作；反向迁移走单条管理）。
func (s *Store) BatchSetSpace(ctx context.Context, ids []string) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		"UPDATE media SET space = 'shared', updated_at = now() WHERE id = ANY($1) AND space = 'personal'", ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ---- Job000100 相册目标（move/copy → album）----

// albumTargetInfo 相册目标守卫的产出。属主显式入参给可见性谓词——
// 与 internal/albums 同一理由：谓词主体必须是**相册属主**而不是调用者，
// 读路径（含匿名分享）按相册属主的可见集过滤，写入端不对齐就会产生读不出/越权脏行。
type albumTargetInfo struct {
	ownerID string
	typ     string
}

// getAlbumTarget 读相册属主/类型（守卫用；不存在返回 found=false，同形 404 由调用方决定）。
func (s *Store) getAlbumTarget(ctx context.Context, albumID string) (albumTargetInfo, bool, error) {
	var info albumTargetInfo
	err := s.Pool.QueryRow(ctx,
		`SELECT owner_id::text, type::text FROM albums WHERE id = $1`, albumID).Scan(&info.ownerID, &info.typ)
	if errors.Is(err, pgx.ErrNoRows) {
		return albumTargetInfo{}, false, nil
	}
	if err != nil {
		return albumTargetInfo{}, false, err
	}
	return info, true, nil
}

// errBatchMediaNotAccessible 整笔可见性校验失败的统一哨兵（调用方映射 403）。
var errBatchMediaNotAccessible = errors.New("存在不在该相册属主可见范围内的媒体")

// batchAddToAlbumQueries 构造「整笔可见性校验 + 插入」两段语句（与 internal/albums 的
// addItemsQueries 同一形制）：先校验任何一项不在相册属主可见范围则整笔不写。
// 占位符编号：check 从 $2 起、insert 从 $3 起（VisibleCondFor 恰 1 个 userID 参数，前提由测试自证）。
func batchAddToAlbumQueries(albumID, albumOwnerID string, ids []string) (checkSQL string, checkArgs []any, insertSQL string, insertArgs []any) {
	visCheck, checkVisArgs := mediascope.VisibleCondFor(2, albumOwnerID, "m")
	checkSQL = `SELECT count(*)::int
		FROM unnest($1::uuid[]) AS u(mid)
		JOIN media m ON m.id = u.mid
		WHERE m.deleted_at IS NULL AND ` + visCheck
	checkArgs = append([]any{ids}, checkVisArgs...)

	visInsert, insertVisArgs := mediascope.VisibleCondFor(3, albumOwnerID, "m")
	insertSQL = `INSERT INTO album_items (album_id, media_id, sort_key)
		SELECT $1, m.id,
		       COALESCE((SELECT max(sort_key) FROM album_items WHERE album_id = $1), 0)
		       + ROW_NUMBER() OVER (ORDER BY u.ord)::int
		FROM unnest($2::uuid[]) WITH ORDINALITY AS u(mid, ord)
		JOIN media m ON m.id = u.mid
		WHERE m.deleted_at IS NULL AND ` + visInsert + `
		ON CONFLICT (album_id, media_id) DO NOTHING`
	insertArgs = append([]any{albumID, ids}, insertVisArgs...)
	return checkSQL, checkArgs, insertSQL, insertArgs
}

// BatchAddToAlbum move→相册 / copy→相册的入册执行：整笔可见性校验后插入 album_items。
// 返回实际新增数（ON CONFLICT 跳过已存在条目，幂等可重试）。
func (s *Store) BatchAddToAlbum(ctx context.Context, albumID, albumOwnerID string, ids []string) (int, error) {
	checkSQL, checkArgs, insertSQL, insertArgs := batchAddToAlbumQueries(albumID, albumOwnerID, ids)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var accessible int
	if err := tx.QueryRow(ctx, checkSQL, checkArgs...).Scan(&accessible); err != nil {
		return 0, fmt.Errorf("校验媒体可见性失败: %w", err)
	}
	if accessible != len(ids) {
		return 0, fmt.Errorf("%w：请求 %d 个，其中 %d 个不可见", errBatchMediaNotAccessible, len(ids), len(ids)-accessible)
	}

	tag, err := tx.Exec(ctx, insertSQL, insertArgs...)
	if err != nil {
		return 0, fmt.Errorf("加入相册失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
