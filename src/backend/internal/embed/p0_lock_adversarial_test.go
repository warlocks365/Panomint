package embed

// P0-2 ORT 推理锁的**对抗性源码审查**（CGO_ENABLED=0 可跑：只读源码，不触发 ORT 加载）。
//
// 攻击面：锁若只罩住 Run 而没罩住「写共享输入张量」或「拷输出」，两个并发请求会
// 互相污染输入/输出 —— A 的文本拿到 B 的向量（静默错配，比崩溃更糟）。
// 必须满足：Lock 在首次触碰共享张量（GetData/copy 写入）之前；
// Unlock（defer）在输出拷贝完成之后。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// funcBody 抠出指定方法的函数体（到下一个顶层 func 为止）。
func funcBody(t *testing.T, src, sig string) string {
	t.Helper()
	start := strings.Index(src, sig)
	if start < 0 {
		t.Fatalf("源码里找不到 %s —— 方法被改名或删除？", sig)
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

// assertLockCovers 钉住「Lock → 写输入 → Run → 拷输出 →（defer）Unlock」的全段覆盖。
func assertLockCovers(t *testing.T, body, lock, unlock, firstTouch, run string) {
	t.Helper()
	iLock := strings.Index(body, lock)
	iTouch := strings.Index(body, firstTouch)
	iRun := strings.Index(body, run)
	iUnlock := strings.Index(body, unlock)
	if iLock < 0 || iTouch < 0 || iRun < 0 || iUnlock < 0 {
		t.Fatalf("缺关键环节（lock/touch/run/unlock）:\n%s", body)
	}
	if iLock > iTouch {
		t.Fatalf("Lock 排在首次触碰共享张量之后 —— 写输入不在锁内，并发串台:\n%s", body)
	}
	if iRun < iTouch {
		t.Fatalf("Run 排在写输入之前？:\n%s", body)
	}
	// defer Unlock 出现在 Lock 之后即覆盖到函数返回（输出拷贝在 return 前完成）。
	if iUnlock < iLock {
		t.Fatalf("Unlock 出现在 Lock 之前？:\n%s", body)
	}
	// 输出拷贝（最后一个 GetData 之后还有 copy）必须在函数体内、return 之前：
	// defer 语义保证其在 Unlock 前完成，这里只需确认拷贝存在且位于 Run 之后。
	iLastCopy := strings.LastIndex(body, "copy(res,")
	if iLastCopy < 0 || iLastCopy < iRun {
		t.Fatalf("输出拷贝缺失或排在 Run 之前:\n%s", body)
	}
}

func TestAdversarialEncodeTextLockCoverage(t *testing.T) {
	b, err := os.ReadFile("clip_cgo.go")
	if err != nil {
		t.Fatalf("读不到 clip_cgo.go: %v", err)
	}
	src := string(b)
	body := funcBody(t, src, "func (e *Encoder) EncodeText(")
	assertLockCovers(t, body,
		"e.muText.Lock()", "e.muText.Unlock()",
		"e.textIn.GetData()", "e.text.Run()")

	// 自证：把 Lock 挪到 GetData 之后的坏版本必须被拦住。
	broken := strings.Replace(body, "e.muText.Lock()\n\tdefer e.muText.Unlock()\n\n\tdst := e.textIn.GetData()",
		"dst := e.textIn.GetData()\n\te.muText.Lock()\n\tdefer e.muText.Unlock()", 1)
	if !strings.Contains(broken, "dst := e.textIn.GetData()\n\te.muText.Lock()") {
		t.Fatal("自证替换未生效（源码形态变了，本测试需同步）")
	}
	// 对坏版本重跑次序断言：必须失败。
	if strings.Index(broken, "e.muText.Lock()") < strings.Index(broken, "e.textIn.GetData()") {
		t.Fatal("守卫失效：Lock 在 GetData 之后的版本也能通过断言")
	}
}

func TestAdversarialEncodePixelsLockCoverage(t *testing.T) {
	b, err := os.ReadFile("clip_cgo.go")
	if err != nil {
		t.Fatalf("读不到 clip_cgo.go: %v", err)
	}
	src := string(b)
	body := funcBody(t, src, "func (e *Encoder) EncodePixels(")
	assertLockCovers(t, body,
		"e.muVision.Lock()", "e.muVision.Unlock()",
		"e.visIn.GetData()", "e.vision.Run()")

	// 文本塔与视觉塔不得共用同一把锁（否则两塔互相串行，吞吐砍半——这是性能钉，
	// 顺带防止"合并成一把锁"的重构把 EncodeText 的 muText 删掉）。
	if strings.Contains(body, "muText") {
		t.Fatal("EncodePixels 误用 muText —— 两塔锁串味")
	}
}

// TestAdversarialNoUnlockedRunPath 全仓扫描：ORT 会话的 Run 调用点必须恰为
// 已知四处（文本/视觉/人脸特征/人脸检测），多一处即存在未审锁的旁路。
func TestAdversarialNoUnlockedRunPath(t *testing.T) {
	backend, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	err = filepath.Walk(backend, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") { // 测试自身含这些字面量
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		for _, pat := range []string{".text.Run(", ".vision.Run(", "sess.Run("} {
			if strings.Contains(src, pat) {
				sites = append(sites, filepath.Base(path)+" :: "+pat)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"clip_cgo.go :: .text.Run(":   1,
		"clip_cgo.go :: .vision.Run(": 1,
		"embed.go :: sess.Run(":       1, // internal/faces/embed.go
		"detect.go :: sess.Run(":      1, // internal/faces/detect.go
	}
	got := map[string]int{}
	for _, s := range sites {
		got[s]++
	}
	for k, n := range want {
		if got[k] != n {
			t.Fatalf("ORT Run 调用点 %q 期望 %d 处，实际 %d（全部站点: %v）", k, n, got[k], sites)
		}
	}
	if len(sites) != len(want) {
		t.Fatalf("ORT Run 调用点总数 = %d，期望 %d —— 出现未审锁的新推理路径: %v", len(sites), len(want), sites)
	}
}
