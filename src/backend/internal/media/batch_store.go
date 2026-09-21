package media

// Job000066 批量操作存储层：归属一次收敛 + 各 op 的批量 SQL。
// 所有语句限定 id=ANY($1) 且调用前已经 batchOwnedIDs 过滤——此处不再重复 owner 谓词。

import (
	"context"
	"fmt"
	"strings"
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
	id          string
	typ         string
	filename    string
	folder      string
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

// insertCopiedRow 写入副本行（新 id；path/filename 用新值；副本继承缩略图引用与 hash——
// 缩略图文件同名引用对新行同样有效（同 id 命名？缩略图名含旧 id）→ 缩略图置空重生成，保证一致）。
func (s *Store) insertCopiedRow(ctx context.Context, r *copiedRow, ownerID, newRel, storedName string) (string, error) {
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
		r.typ, ownerID, newRel, r.folder, storedName, r.id).Scan(&newID)
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
