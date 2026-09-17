package media

import "time"

// 本文件是 **MediaRef 查询列清单与扫描器的唯一真源**（§二十一）。
//
// 为什么必须只有一份：在本次收拢之前，同一套 15 列的 SELECT 列表在仓库里存在 **7 份**：
//
//	1. media/timeline.go   listMediaCols      （GET /media，带防护）
//	2. media/duplicates.go                    （GET /media/duplicates，带防护）
//	3. media/tags.go                          （GET /tags/:id/media，裸选）
//	4. media/write.go      ListTrash          （GET /media/trash，裸选）
//	5. albums/store.go     mediaCols          （GET /albums/:id，裸选）
//	6. shares/store.go     mediaCols          （GET /public/shares/:token，裸选）
//	7. search/store.go                        （GET /search，裸选）
//
// 其中两处（albums / shares）的注释还写着"与 internal/media/timeline.go 保持一致"——
// **注释不是机制**：改 timeline.go 时它们不会跟着改。实测后果是同一个缺陷在 5 个端点上
// 各自独立存在：库里只要有一行 filename 或 folder_path 为 NULL，整个响应就 500/400
// （`can't scan into dest[2] (col: filename): cannot scan NULL into *string`）。
//
// 因此这里的规则是：**列清单与扫描器必须成对共用** —— 只共享字符串、各自写 Scan 目标
// 仍然会漂移（列加了一个而 Scan 没加就 panic）。两者绑在同一个定义里。

// MediaRefColumns 是 MediaRef 的**唯一**查询列清单，顺序即扫描顺序。
//
// filename / folder_path 在 DDL 中可空，而 MediaRef 的同名字段是非指针 string：
// 必须 COALESCE，否则全库只要有一行 NULL，整个端点就崩（见上）。**嵌套查询也一样**——
// 谓词、ORDER BY、游标都可以随便写，但 SELECT 列表必须是这一份。
//
// taken_at 同为可空列，但这里**故意不 COALESCE**：MediaRef.TakenAt 是被全部读路径与
// JSON 契约共用的非指针 time.Time，改成指针会让 taken_at 变 null（契约破坏）。
// 缺失语义由 MediaRefScanner 在扫描边界用 *time.Time 承接、NULL 时保持零值
// （"0001-01-01T00:00:00Z"），保留"确实没有拍摄时间"这一事实。
//
// 回归保护见 mediaref_single_source_test.go：再出现第 8 份副本会被测试直接拒绝。
const MediaRefColumns = `m.id, m.type, COALESCE(m.filename,''), COALESCE(m.folder_path,''), m.taken_at,
	m.width, m.height, m.duration, m.codec, m.is_360, m.place, m.rating,
	m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg`

// rowScanner 同时被 pgx.Row 与 pgx.Rows 满足（二者都是 Scan(dest ...any) error）。
// 用接口而不是 pgx.Row，是为了让单行（QueryRow）与多行（Query）走同一条扫描路径。
type rowScanner interface{ Scan(dest ...any) error }

// MediaRefScanner 累积一行的扫描目标，并在结束时回填可空的 taken_at。
//
// 用法（**列顺序必须与 MediaRefColumns 一致**；需要追加额外列时在 Dests() 之后续）：
//
//	sc := media.NewMediaRefScanner()
//	dests := append(sc.Dests(), &extra)   // 额外列必须排在 MediaRefColumns 之后
//	if err := rows.Scan(dests...); err != nil { ... }
//	it := sc.Finish()
type MediaRefScanner struct {
	Ref   MediaRef
	taken *time.Time
}

// NewMediaRefScanner 建一个空的扫描累加器。
func NewMediaRefScanner() *MediaRefScanner { return &MediaRefScanner{} }

// Dests 返回 MediaRefColumns 对应的扫描目标（15 个，顺序一一对应）。
//
// 返回的切片是新建的，调用方可以安全地 append 额外列的目标。
func (s *MediaRefScanner) Dests() []any {
	return []any{
		&s.Ref.ID, &s.Ref.Type, &s.Ref.Filename, &s.Ref.FolderPath, &s.taken,
		&s.Ref.Width, &s.Ref.Height, &s.Ref.Duration, &s.Ref.Codec, &s.Ref.Is360,
		&s.Ref.Place, &s.Ref.Rating,
		&s.Ref.ThumbnailSM, &s.Ref.ThumbnailMD, &s.Ref.ThumbnailLG,
	}
}

// Finish 回填 taken_at 后返回该行。NULL 时保持零值（见 MediaRefColumns 的说明）。
func (s *MediaRefScanner) Finish() MediaRef {
	if s.taken != nil {
		s.Ref.TakenAt = *s.taken
	}
	return s.Ref
}

// ScanMediaRef 扫描**恰好** MediaRefColumns 的一行（页面上最常见的用法）。
func ScanMediaRef(row rowScanner) (MediaRef, error) {
	s := NewMediaRefScanner()
	if err := row.Scan(s.Dests()...); err != nil {
		return MediaRef{}, err
	}
	return s.Finish(), nil
}
