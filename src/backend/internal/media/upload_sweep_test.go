package media

// P2-04 分块上传会话 TTL 清扫的回归网。
//
// 修复前：中断的上传（网络断、用户放弃、首块后不再续传）在 UploadTmp 留下
// .part/.json 永久残留，无任何 TTL/启动清扫。SweepStaleUploads 是清扫本体
// （接线：cmd/api 启动时调用，装配点在 main.go，不在本包 —— 见函数注释）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepStaleUploads(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := now.Add(-25 * time.Hour) // 超过 24h 阈值
	fresh := now.Add(-1 * time.Hour)

	mk := func(name string, mtime time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	mk("aaa111.part", stale) // 残留数据 → 删
	mk("aaa111.json", stale) // 残留元数据 → 删
	mk("bbb222.part", fresh) // 活跃会话 → 留
	mk("bbb222.json", fresh) // 活跃会话 → 留
	mk("README.txt", stale)  // 非会话文件 → 留（只认 .part/.json）
	mk("ccc333.bin", stale)  // 非会话文件 → 留

	removed, err := SweepStaleUploads(dir, StaleUploadTTL, now)
	if err != nil {
		t.Fatalf("清扫失败: %v", err)
	}
	if removed != 2 {
		t.Fatalf("应删除 2 个残留文件，实际 %d", removed)
	}
	for _, gone := range []string{"aaa111.part", "aaa111.json"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s 应被删除", gone)
		}
	}
	for _, keep := range []string{"bbb222.part", "bbb222.json", "README.txt", "ccc333.bin"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("%s 应保留: %v", keep, err)
		}
	}
}

func TestSweepStaleUploadsMissingDirIsNoop(t *testing.T) {
	// UploadTmp 还不存在（从未有过分块上传）是正常形态，不是错误。
	removed, err := SweepStaleUploads(filepath.Join(t.TempDir(), "nonexistent"), StaleUploadTTL, time.Now())
	if err != nil || removed != 0 {
		t.Fatalf("目录不存在应为 (0, nil)，实际 (%d, %v)", removed, err)
	}
}

func TestUploadMetaRecordsCreatedAt(t *testing.T) {
	// P2-04 配套：分块 meta json 补记创建时间（TTL 判定的辅助信息）。
	// 首块初始化路径会写 CreatedAt —— 通过 HTTP 层走一遍首块，读回 json 验证。
	h := newTestHandler(t)
	chunk := []byte("aaaaaaaaaa")
	rec := doUpload(h, newChunkReq(t, chunk, 0, 9, 100, ""))
	if rec.Code != 200 {
		t.Fatalf("首块应 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		UploadID string `json:"upload_id"`
	}
	mustJSON(t, rec, &resp)
	data, err := os.ReadFile(filepath.Join(h.UploadTmp, resp.UploadID+".json"))
	if err != nil {
		t.Fatalf("读会话元数据失败: %v", err)
	}
	var meta uploadMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("会话元数据损坏: %v", err)
	}
	if meta.CreatedAt == "" {
		t.Fatal("分块 meta json 缺少 created_at（P2-04 要求补记创建时间）")
	}
	if _, err := time.Parse(time.RFC3339, meta.CreatedAt); err != nil {
		t.Fatalf("created_at 应为 RFC3339，实际 %q: %v", meta.CreatedAt, err)
	}
}
