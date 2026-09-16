package phash

// 感知哈希的 DB 存取（media.phash / media.phash_scanned_at，见迁移 00024）。
//
// 依赖约定：
//
//	media.phash             BIGINT       64 位指纹；位序见 phash.go 包注释
//	media.phash_scanned_at  TIMESTAMPTZ  扫描标记（非空=已尝试过，即使解不开也不再重试）
//	media.thumbnail_md      TEXT         **只存文件名**（如 <media_id>_MD.webp），相对 THUMB_DIR
//
// 为什么用 MD（宽 640）而不是 LG（宽 1280）：pHash 最终只把图缩到 32×32，
// 640 已经远超所需；MD 由 index worker 更早产出，命中更快。
//
// 增量为何是「清扫式」而非队列：与 embedgen / facesgen 同理——队列消费方对未知 kind
// 会直接 Ack 吞掉任务，而清扫天然幂等、能自动重试历史失败项。

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 感知哈希存取。
type Store struct {
	Pool *pgxpool.Pool
}

// Candidate 待计算感知哈希的媒体。
type Candidate struct {
	ID       string
	Filename string
	ThumbMD  string
}

// ListPending 列出待计算感知哈希的媒体；force 为真时忽略扫描标记全量重算。
//
// force 是刻意保留的参数（而非单独再造一个 ListAll）：faces.Store.ListPending(ctx, force, limit)
// 就是这个形状，-force 必须能忽略标记重算，对齐既有先例比另起一套更不容易出错。
//
// 只返回**已有 MD 缩略图**的行：缩略图由 index worker 异步产出，
// 晚到者由下一轮清扫自然补上（见 phashgen -mode watch）。
func (s *Store) ListPending(ctx context.Context, force bool, limit int) ([]Candidate, error) {
	if limit <= 0 {
		limit = 200
	}
	cond := "phash_scanned_at IS NULL"
	if force {
		cond = "true"
	}
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, COALESCE(filename,''), COALESCE(thumbnail_md,'')
		FROM media
		WHERE deleted_at IS NULL AND COALESCE(thumbnail_md,'') <> '' AND %s
		ORDER BY taken_at DESC NULLS LAST, id
		LIMIT $1`, cond), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Candidate{}
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Filename, &c.ThumbMD); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Save 写入指纹并同时回填扫描标记（两者必须一起写，否则会被下轮重复计入待扫）。
//
// phash 列是 BIGINT（**有符号**），而哈希是 64 位无符号、最高位可能为 1：
// 直接传 uint64 会被 pgx 拒绝或溢出，故显式 int64(h)。
// 这**不丢位**——只是同一串比特换个解释（已在测试库实测 0x8000000000000001 往返一致），
// 读取时再 uint64(...) 转回来即可。SQL 侧算汉明距离要写
// `bit_count((a # b)::bit(64))`：bit_count 只接受 bit 类型，加 ::bit(64) 才能喂 BIGINT。
func (s *Store) Save(ctx context.Context, mediaID string, h uint64) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE media SET phash = $2, phash_scanned_at = now() WHERE id = $1`,
		mediaID, int64(h))
	return err
}

// MarkScanned 只回填扫描标记，用于「缩略图存在但无法解码」的情况。
//
// 没有这一步，坏缩略图（截断/格式不支持）会被每轮清扫无限重试，永远占着队列头部。
func (s *Store) MarkScanned(ctx context.Context, mediaID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE media SET phash_scanned_at = now() WHERE id = $1`, mediaID)
	return err
}

// Stats 统计：(媒体总数, 已算出指纹数, 待扫描数, 失败数)。
//
// 恒等式 total = hashed + pending + failed 由四个 FILTER 的定义**结构性**保证
// （它们是对同一集合按 phash / phash_scanned_at 是否为空做的四象限划分），
// 故 status 输出可以直接当作「还剩多少」来读：
//
//	hashed  已算好，可用于分组
//	pending 还没试过（缩略图未就绪或刚导入）
//	failed  试过但没算出（缩略图缺失或解不开）——只有 -force 才会再碰
func (s *Store) Stats(ctx context.Context) (total, hashed, pending, failed int, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE phash IS NOT NULL),
		       count(*) FILTER (WHERE phash IS NULL AND phash_scanned_at IS NULL),
		       count(*) FILTER (WHERE phash IS NULL AND phash_scanned_at IS NOT NULL)
		FROM media WHERE deleted_at IS NULL`).Scan(&total, &hashed, &pending, &failed)
	return
}
