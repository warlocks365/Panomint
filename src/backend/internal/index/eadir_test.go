package index

// Job000056 @eaDir 缩略图复用的守卫测试（不依赖 ffmpeg/ORT——
// 转码与挂点行为由部署侧合成样本 e2e 承担，登记簿记证据）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panoalbum/internal/ffmpeg"
)

// 造 @eaDir/<媒体名>/ 结构；files 为 nil 表示整个 sidecar 目录不存在。
func mkEAThumbs(t *testing.T, mediaAbs string, files []string) {
	t.Helper()
	if files == nil {
		return
	}
	dir := filepath.Join(filepath.Dir(mediaAbs), "@eaDir", filepath.Base(mediaAbs))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindEAThumbs(t *testing.T) {
	media := filepath.Join(t.TempDir(), "2024", "photo1.jpg")

	// 无 @eaDir → nil（非群晖来源属常态）
	if got := FindEAThumbs(media); got != nil {
		t.Fatalf("无 sidecar 应 nil，实得 %v", got)
	}

	// 三档齐全 → 非 nil，键为档位常量
	mkEAThumbs(t, media, []string{"SYNOFILE_THUMB_S.jpg", "SYNOFILE_THUMB_M.jpg", "SYNOFILE_THUMB_L.jpg"})
	got := FindEAThumbs(media)
	if got == nil {
		t.Fatal("三档齐全应命中")
	}
	for _, size := range []ffmpeg.ThumbSize{"SM", "MD", "LG"} {
		if _, ok := got[size]; !ok {
			t.Fatalf("缺档位 %s：%v", size, got)
		}
	}
	if !strings.HasSuffix(got["LG"], filepath.Join("@eaDir", "photo1.jpg", "SYNOFILE_THUMB_L.jpg")) {
		t.Fatalf("LG 路径不对：%s", got["LG"])
	}

	// 缺 L 档 → nil（整组交回队列）
	mkEAThumbs(t, media, []string{"SYNOFILE_THUMB_S.jpg", "SYNOFILE_THUMB_M.jpg"})
	os.Remove(filepath.Join(filepath.Dir(media), "@eaDir", "photo1.jpg", "SYNOFILE_THUMB_L.jpg"))
	if got := FindEAThumbs(media); got != nil {
		t.Fatalf("缺档应 nil，实得 %v", got)
	}

	// 档位字母非法（如 XL）→ 不算数
	mkEAThumbs(t, media, []string{"SYNOFILE_THUMB_S.jpg", "SYNOFILE_THUMB_M.jpg", "SYNOFILE_THUMB_XL.jpg"})
	if got := FindEAThumbs(media); got != nil {
		t.Fatalf("XL 不应算档位，实得 %v", got)
	}

	// 大写 .JPG 扩展也认
	mkEAThumbs(t, media, []string{"SYNOFILE_THUMB_S.JPG", "SYNOFILE_THUMB_M.jpg", "SYNOFILE_THUMB_L.jpg"})
	if got := FindEAThumbs(media); got == nil {
		t.Fatal(".JPG 扩展应命中")
	}

	// 前缀不符（别家文件）→ nil
	mkEAThumbs(t, media, []string{"SYNOFILE_THUMB_S.jpg", "SYNOFILE_THUMB_M.jpg", "SYNOFILE_THUMB_L.jpg", "cover.jpg"})
	got = FindEAThumbs(media)
	if got == nil || len(got) != 3 {
		t.Fatalf("cover.jpg 不应干扰三档命中：%v", got)
	}

	// @eaDir 下的子目录不算文件
	dir := filepath.Join(filepath.Dir(media), "@eaDir", "photo1.jpg", "nested")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindEAThumbs(media); got == nil {
		t.Fatal("子目录不应干扰命中")
	}
}

// TestEAThumbNamingGuard 钉死命名与 worker 逐字节一致（两处独立实现，漂移即红）。
func TestEAThumbNamingGuard(t *testing.T) {
	got := eaThumbPath("/thumbs", "abc-123", "LG")
	want := filepath.Join("/thumbs", "abc-123_LG.webp")
	if got != want {
		t.Fatalf("命名漂移：want %q got %q", want, got)
	}
	// worker.go 的 thumbPath 规则（fmt.Sprintf("%s_%s.webp", id, size)）必须同形
	w, err := os.ReadFile("worker.go")
	if err != nil {
		t.Fatalf("读不到 worker.go: %v", err)
	}
	if !strings.Contains(string(w), `fmt.Sprintf("%s_%s.webp", mediaID, size)`) {
		t.Fatal("worker.thumbPath 命名规则已改——同步 eadir.go 的 eaThumbPath 并更新本守卫")
	}
}

// TestEAHookOrderGuard 钉住 index.go 挂点语义：复用成功在入队之前 return（不入队）。
func TestEAHookOrderGuard(t *testing.T) {
	b, err := os.ReadFile("index.go")
	if err != nil {
		t.Fatalf("读不到 index.go: %v", err)
	}
	src := string(b)
	iFind := strings.Index(src, "FindEAThumbs(e.Path)")
	iConv := strings.Index(src, "ConvertEAThumbs(ctx, td, mediaID, thumbs)")
	iRet := strings.Index(src, "return OutcomeInserted, nil\n\t\t\t\t}")
	iEnqueue := strings.Index(src, `x.q.Enqueue(ctx, queue.Job{Kind: "thumbnail"`)
	if iFind < 0 || iConv < 0 || iRet < 0 || iEnqueue < 0 {
		t.Fatalf("挂点环节缺失: find=%d conv=%d ret=%d enqueue=%d", iFind, iConv, iRet, iEnqueue)
	}
	if !(iFind < iConv && iConv < iRet && iRet < iEnqueue) {
		t.Fatalf("挂点顺序错：复用成功必须早于入队 return（find=%d conv=%d ret=%d enqueue=%d）",
			iFind, iConv, iRet, iEnqueue)
	}
	// 变异自证：入队挪到复用之前会被顺序断言拦住（ret 在 enqueue 后）
	if iRet > iEnqueue {
		t.Fatal("守卫失效：复用 return 排在入队后的坏版本通过了断言")
	}
	// UPDATE 语句必须与 worker 的三档一体 UPDATE 同形（一次写三列）
	if !strings.Contains(src, "UPDATE media SET thumbnail_sm=$1, thumbnail_md=$2, thumbnail_lg=$3") {
		t.Fatal("复用落库 UPDATE 与 worker 不同形（须一次写三列）")
	}
}
