package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- MediaRef 列清单的「唯一真源」守卫（§二十一）----
//
// 背景：收拢之前，同一套 15 列的 SELECT 列表在仓库里有 **7 份**副本；其中两处的注释还
// 写着「与 internal/media/timeline.go 保持一致」——**注释不是机制**，改一份不会带动其余。
// 后果是同一个缺陷在 5 个端点上各自独立存在（任一 filename/folder_path 为 NULL 即整响应崩）。
//
// 本文件负责的是"**不许再出现第 8 份**"这一条。它不是靠断言"某个函数被调用了"，
// 而是靠**在源码里搜列清单的特征串**——因为漂移的形态就是"有人又抄了一份"。
//
// ⚠️ 本文件自己也在被扫描的范围内（这很重要）：它需要引用特征串来做"守卫自证"，
// 因此**所有出现特征串的地方都必须用字符串拼接写**，不能写成连续字面量。
// 第一版就是直接写成字面量的，结果守卫把自己当成了第 8 份副本并报红 —— 那次报红
// 反过来证明了守卫是活的。修法是"让本文件真的不含该串"，而不是把自己加进允许清单：
// 一旦允许清单里出现"测试文件"这种条目，真正的副本就有地方藏了。

// lastThreeMediaRefCols 拼出列清单的特征串（15 列的尾部三列）。
//
// 必须拼接而不能写成字面量，见文件头说明。
func lastThreeMediaRefCols() string {
	return "m.thumbnail_sm, " + "m.thumbnail_md, " + "m.thumbnail_lg"
}

// refColumnMarker 是一份 MediaRef 列清单的特征串。
//
// 选尾部三列是因为：全部 7 份副本**都**含这个子串，而不含列清单的 SQL 不会含它
// （谓词 / ORDER BY 里出现的是单个列名，如目录过滤用 `m.folder_path = $n`）。
var refColumnMarker = lastThreeMediaRefCols()

// sharedListFile 唯一真源所在文件（相对 src/backend）。
var sharedListFile = filepath.Join("internal", "media", "mediaref.go")

// allowedForeignLists 允许含该特征串、但**不是** MediaRef 列清单的文件。
//
// 每一项都必须写明"为什么它不是 MediaRef 列清单"。允许清单是必要之恶：
// 没有它就只能"只扫已知的 7 个文件"，那样新增包里的第 8 份副本会被漏掉。
// 也正因为它有软化守卫的风险，条目必须少而精确——**不要**把测试文件塞进来。
var allowedForeignLists = map[string]string{
	filepath.Join("internal", "media", "detail.go"): "MediaDetail 的列清单（另一个响应结构体，尾部还有 hls_master 等额外列）",
}

// detectsRefColumnList 判定这段源码里是否含一份 MediaRef 列清单。
func detectsRefColumnList(src string) bool {
	return strings.Contains(src, refColumnMarker)
}

// repoRoot 从 internal/media 回到 src/backend。
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("解析仓库根失败: %v", err)
	}
	return root
}

// TestMediaRefColumnListHasSingleSource 全仓只允许存在一份 MediaRef 列清单。
//
// 失败意味着：有人又抄了一份。请改用 media.MediaRefColumns（列）
// + media.ScanMediaRef / media.NewMediaRefScanner（扫描器）。
// 两者必须成对共用——只共享列字符串、各自写 Scan 目标仍会漂移（列加了而 Scan 没加就 panic）。
func TestMediaRefColumnListHasSingleSource(t *testing.T) {
	root := repoRoot(t)
	found := map[string]bool{}

	for _, sub := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, sub), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !detectsRefColumnList(string(b)) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found[rel] = true
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", sub, err)
		}
	}

	// 前提自证：唯一真源必须真的被扫到，否则下面的计数毫无意义
	// （比如 marker 写错、或真源被挪到别处）。
	if !found[sharedListFile] {
		t.Fatalf("唯一真源 %s 里找不到列清单特征串 —— 真源被挪走、改名，或特征串构造被改坏了。"+
			"本守卫的前提失效，先修它。", sharedListFile)
	}

	for f := range found {
		if f == sharedListFile {
			continue
		}
		if _, ok := allowedForeignLists[f]; ok {
			continue
		}
		t.Fatalf("%s 里出现了 MediaRef 列清单特征串 —— 这是**又一份副本**。\n"+
			"§二十一 的教训：同一份列清单曾有 7 份，其中两处的注释还写着「与 xx 保持一致」，"+
			"而注释不会让任何东西保持同步；后果是同一个 NULL 缺陷在 5 个端点各自独立存在。\n"+
			"请改用 media.MediaRefColumns（列）+ media.ScanMediaRef / media.NewMediaRefScanner（扫描器）。\n"+
			"若这里确实是**另一个结构体**的列清单，请把它加进 allowedForeignLists 并写明理由。", f)
	}

	// 反向确认：数量必须正好是「真源 1 + 允许清单 N」。
	// 少了说明真源或 detail.go 被判漏了，同样要看见。
	if want := 1 + len(allowedForeignLists); len(found) != want {
		t.Fatalf("含列清单特征串的文件应为 %d 个（真源 1 + 允许清单 %d），实际 %d 个：%v",
			want, len(allowedForeignLists), len(found), found)
	}
}

// TestGuardDetectsPreFixCopies 守卫自证：把**收拢前**的那 5 份清单喂给同一判定函数，必须命中。
//
// 没有这一步，上面的断言可能是"永远通过"的假测试（特征串写错、或列清单改个排版就认不出）。
// 下面每一段都是从收拢前的源码逐字抄下来的，只是把尾部三列换成了 lastThreeMediaRefCols()
// 的拼接结果——否则本文件自己就会含特征串（见文件头）。
func TestGuardDetectsPreFixCopies(t *testing.T) {
	tail := lastThreeMediaRefCols()
	old := map[string]string{
		"albums/store.go 的 mediaCols（裸选）": "m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,\n" +
			"\tm.codec, m.is_360, m.place, m.rating, " + tail,
		"shares/store.go 的 mediaCols（裸选）": "m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,\n" +
			"\tm.codec, m.is_360, m.place, m.rating, " + tail,
		"search/store.go 的内联清单（裸选）": "SELECT m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,\n" +
			"\t       m.codec, m.is_360, m.place, m.rating, " + tail + ",",
		"media/tags.go 的内联清单（裸选）": "SELECT m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,\n" +
			"\t       m.codec, m.is_360, m.place, m.rating, " + tail,
		"media/write.go ListTrash（裸选，且没有 folder_path）": "SELECT m.id, m.type::text, m.filename, m.taken_at, m.width, m.height, m.duration,\n" +
			"\t       m.codec, m.is_360, m.place, m.rating, " + tail,
	}
	if len(old) != 5 {
		t.Fatalf("自证样本应为收拢前那 5 份无防护副本，实际 %d 份", len(old))
	}
	for name, src := range old {
		if !detectsRefColumnList(src) {
			t.Fatalf("守卫失效：认不出收拢前的副本（%s）—— 上面的断言无法真正拦截回归", name)
		}
	}

	// 反向：**不含**列清单的 SQL 不得被误判。否则守卫会把正常代码也拦下，
	// 逼着后来者把允许清单塞满，最后没人再维护它。
	notLists := map[string]string{
		"index/worker.go 的 UPDATE 三列赋值": "UPDATE media SET thumbnail_sm=$1, thumbnail_md=$2, thumbnail_lg=$3, updated_at=now()",
		"media/timeline.go 的目录过滤谓词":     "(m.folder_path = $1 OR m.folder_path LIKE $1 || '/%')",
		"phash/store.go 的两列清单（worker）":  "SELECT id::text, COALESCE(filename,''), COALESCE(thumbnail_md,'')",
	}
	for name, src := range notLists {
		if detectsRefColumnList(src) {
			t.Fatalf("守卫误报：把非 MediaRef 列清单判成了副本（%s）：%s", name, src)
		}
	}
}

// TestEveryCallSiteUsesSharedDefinition 7 处调用点都必须引用共享定义。
//
// 与上一条互补：上一条抓"多出一份副本"，这一条抓"某处不再用共享定义"，
// 报错更直指位置。
func TestEveryCallSiteUsesSharedDefinition(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		file, symbol, endpoint string
	}{
		{filepath.Join("internal", "media", "timeline.go"), "MediaRefColumns", "GET /media"},
		{filepath.Join("internal", "media", "duplicates.go"), "MediaRefColumns", "GET /media/duplicates"},
		{filepath.Join("internal", "media", "tags.go"), "MediaRefColumns", "GET /tags/:id/media"},
		{filepath.Join("internal", "media", "write.go"), "MediaRefColumns", "GET /media/trash"},
		{filepath.Join("internal", "albums", "store.go"), "media.MediaRefColumns", "GET /albums/:id"},
		{filepath.Join("internal", "shares", "store.go"), "media.MediaRefColumns", "GET /public/shares/:token"},
		{filepath.Join("internal", "search", "store.go"), "media.MediaRefColumns", "GET /search"},
	}
	for _, tc := range cases {
		b, err := os.ReadFile(filepath.Join(root, tc.file))
		if err != nil {
			t.Fatalf("读不到 %s：%v", tc.file, err)
		}
		if !strings.Contains(string(b), tc.symbol) {
			t.Fatalf("%s（%s）没有引用共享列清单 %q —— "+
				"该端点会退回自己维护列清单，也就是 §二十一 那个已被修复的漂移。",
				tc.file, tc.endpoint, tc.symbol)
		}
	}
}
