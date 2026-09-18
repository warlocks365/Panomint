// migrateplan 数据迁移扫描器（Job000010 后追加；TDD §8.8 / PRD §11.2 的 P1 切片）。
//
// 作用：在**不写库**的前提下，扫描一个既有媒体库（群晖 Photos 导出、旧相册目录等），
// 与当前库比对，产出「导入计划」——新增/重复/路径冲突各多少，供人工确认后再执行导入。
//
// 用法：
//
//	migrateplan -dir <源媒体根> [-meta] [-limit N] [-json plan.json]
//
//	  -dir    源媒体根目录（必填）
//	  -meta   额外解析 EXIF（taken_at / GPS / 360 全景），较慢，默认关闭
//	  -limit  只处理前 N 个文件（大库先抽样看看）
//	  -json   把完整计划写入 JSON 文件
//
// ⚠️ 路径基准（重要，对应实测坑位）：`media.path` 存的是**相对扫描根**的路径。
// 因此源库必须以「媒体根」为 -dir 扫描，不要对某个叶子子目录扫描——
// 否则导入后 worker 按 MEDIA_ROOT 解析会找不到源文件（"源文件不可达"）。
// 本工具会在报告中明确给出扫描根，便于人工核对。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/config"
	"panoalbum/internal/index"
)

// PlanItem 单个文件的计划条目。
type PlanItem struct {
	Rel    string `json:"rel"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	Hash   string `json:"hash"`
	Action string `json:"action"` // new | duplicate | conflict
	Note   string `json:"note,omitempty"`
}

// Plan 完整导入计划。
type Plan struct {
	SourceRoot string         `json:"source_root"`
	ScannedAt  string         `json:"scanned_at"`
	Total      int            `json:"total"`
	New        int            `json:"new"`
	Duplicate  int            `json:"duplicate"`
	Conflict   int            `json:"conflict"`
	TotalBytes int64          `json:"total_bytes"`
	ByKind     map[string]int `json:"by_kind"`
	Meta       *MetaStats     `json:"meta,omitempty"`
	Items      []PlanItem     `json:"items"`
}

// MetaStats EXIF 解析统计（仅 -meta 时填充）。
type MetaStats struct {
	Parsed   int `json:"parsed"`
	Failed   int `json:"failed"`
	WithGPS  int `json:"with_gps"`
	Panorama int `json:"panorama"` // 360 全景
}

func main() {
	dir := flag.String("dir", "", "源媒体根目录（必填）")
	meta := flag.Bool("meta", false, "解析 EXIF（较慢）")
	limit := flag.Int("limit", 0, "只处理前 N 个文件（0=全部）")
	jsonOut := flag.String("json", "", "把完整计划写入该 JSON 文件")
	flag.Parse()

	if *dir == "" {
		log.Fatal("migrateplan 需要 -dir <源媒体根目录>")
	}
	info, err := os.Stat(*dir)
	if err != nil || !info.IsDir() {
		log.Fatalf("源目录不可用: %s (%v)", *dir, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	fmt.Printf("扫描源目录：%s\n", *dir)
	start := time.Now()
	entries, err := index.ScanDir(ctx, *dir)
	if err != nil {
		log.Fatalf("扫描失败: %v", err)
	}
	if *limit > 0 && len(entries) > *limit {
		entries = entries[:*limit]
		fmt.Printf("（已按 -limit 截断到前 %d 个）\n", *limit)
	}
	fmt.Printf("发现媒体文件 %d 个，用时 %s\n", len(entries), time.Since(start).Round(time.Millisecond))

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		log.Fatalf("配置错误: %v", cfgErr)
	}
	pool, err := pgxpool.New(ctx, cfg.PGDSN)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	// 拉取库内已有 hash 与 path（一次性，避免逐文件查询）
	existingByHash, existingByPath, err := loadExisting(ctx, pool)
	if err != nil {
		log.Fatalf("读取库内既有媒体失败: %v", err)
	}
	fmt.Printf("库内既有媒体：%d 条\n", len(existingByPath))

	plan := &Plan{
		SourceRoot: *dir,
		ScannedAt:  time.Now().Format(time.RFC3339),
		ByKind:     map[string]int{},
	}
	if *meta {
		plan.Meta = &MetaStats{}
	}

	for _, e := range entries {
		plan.Total++
		plan.TotalBytes += e.Size
		plan.ByKind[string(e.Kind)]++

		item := PlanItem{Rel: e.Rel, Kind: string(e.Kind), Size: e.Size, Hash: e.Hash}
		switch {
		case existingByHash[e.Hash] != "":
			// 内容已在库中（可能是同一文件的不同路径）→ 去重跳过
			item.Action = "duplicate"
			item.Note = "内容相同，库内已存在：" + existingByHash[e.Hash]
			plan.Duplicate++
		case existingByPath[e.Rel] != "":
			// 路径被占用但内容不同 → 冲突，需人工决定（改名或覆盖）
			item.Action = "conflict"
			item.Note = "路径已被占用且内容不同"
			plan.Conflict++
		default:
			item.Action = "new"
			plan.New++
		}
		plan.Items = append(plan.Items, item)

		if *meta && e.Kind == index.KindPhoto {
			m, err := index.ExtractPhotoMeta(e.Path)
			if err != nil {
				plan.Meta.Failed++
			} else {
				plan.Meta.Parsed++
				if m.Lat != nil && m.Lng != nil {
					plan.Meta.WithGPS++
				}
				if m.Is360 {
					plan.Meta.Panorama++
				}
			}
		}
	}

	sort.Slice(plan.Items, func(i, j int) bool {
		if plan.Items[i].Action != plan.Items[j].Action {
			return plan.Items[i].Action < plan.Items[j].Action
		}
		return plan.Items[i].Rel < plan.Items[j].Rel
	})

	printReport(plan)

	if *jsonOut != "" {
		b, _ := json.MarshalIndent(plan, "", "  ")
		if err := os.WriteFile(*jsonOut, b, 0o644); err != nil {
			log.Fatalf("写入 JSON 失败: %v", err)
		}
		fmt.Printf("\n完整计划已写入：%s\n", *jsonOut)
	}
}

// loadExisting 读取库内既有媒体：hash → path、path → id。
func loadExisting(ctx context.Context, pool *pgxpool.Pool) (map[string]string, map[string]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT COALESCE(hash,''), COALESCE(path,''), id::text FROM media WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	byHash := map[string]string{}
	byPath := map[string]string{}
	for rows.Next() {
		var h, p, id string
		if err := rows.Scan(&h, &p, &id); err != nil {
			return nil, nil, err
		}
		if h != "" {
			if _, ok := byHash[h]; !ok {
				byHash[h] = p
			}
		}
		if p != "" {
			byPath[p] = id
		}
	}
	return byHash, byPath, rows.Err()
}

// printReport 打印人类可读报告。
func printReport(p *Plan) {
	fmt.Println()
	fmt.Println("────────────── 导入计划（dry-run，未写库）──────────────")
	fmt.Printf("扫描根        : %s\n", p.SourceRoot)
	fmt.Printf("文件总数      : %d（%.1f MB）\n", p.Total, float64(p.TotalBytes)/1024/1024)
	kinds := make([]string, 0, len(p.ByKind))
	for k := range p.ByKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s %d", k, p.ByKind[k]))
	}
	fmt.Printf("类型分布      : %s\n", strings.Join(parts, " · "))
	fmt.Println()
	fmt.Printf("  新增 (new)       : %d\n", p.New)
	fmt.Printf("  重复 (duplicate) : %d  ← 内容 sha256 已在库中，导入会跳过\n", p.Duplicate)
	fmt.Printf("  冲突 (conflict)  : %d  ← 路径已占用但内容不同，需人工决定\n", p.Conflict)

	if p.Meta != nil {
		fmt.Println()
		fmt.Printf("EXIF 解析     : 成功 %d / 失败 %d\n", p.Meta.Parsed, p.Meta.Failed)
		fmt.Printf("含 GPS        : %d\n", p.Meta.WithGPS)
		fmt.Printf("360 全景      : %d\n", p.Meta.Panorama)
	}

	// 冲突明细（最多 20 条，需人工处理）
	if p.Conflict > 0 {
		fmt.Println()
		fmt.Println("冲突明细（前 20 条）：")
		n := 0
		for _, it := range p.Items {
			if it.Action != "conflict" {
				continue
			}
			fmt.Printf("  - %s\n", it.Rel)
			if n++; n >= 20 {
				break
			}
		}
	}
	fmt.Println()
	fmt.Println("提示：本工具只读不写。确认无误后，用 indexctl scan -dir <同一源根> 执行实际导入。")
	fmt.Println("⚠️ 必须对**媒体根**扫描（不要对叶子子目录），否则 media.path 会存成文件名，worker 解析源文件将失败。")
}
