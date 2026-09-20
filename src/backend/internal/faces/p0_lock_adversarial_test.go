package faces

// P0-2 ORT 推理锁的**对抗性源码审查**（faces 侧：EmbedAligned + Detect）。
// CGO_ENABLED=0 可跑：只读源码，不触发 ORT 加载。
//
// 攻击面同 embed 侧：锁必须覆盖「写共享输入张量 → Run → 拷输出」全段；
// Detect 用的是手工 Lock/Unlock（非 defer），每条错误路径都必须显式 Unlock，
// 漏一条 = 第一次推理失败后整个 Detector 永久死锁。

import (
	"os"
	"strings"
	"testing"
)

func lockFuncBody(t *testing.T, file, sig string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("读不到 %s: %v", file, err)
	}
	src := string(b)
	start := strings.Index(src, sig)
	if start < 0 {
		t.Fatalf("%s 里找不到 %s —— 方法被改名或删除？", file, sig)
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

// TestAdversarialEmbedAlignedLockCoverage 钉住 r.mu 全段覆盖。
func TestAdversarialEmbedAlignedLockCoverage(t *testing.T) {
	body := lockFuncBody(t, "embed.go", "func (r *Recognizer) EmbedAligned(")

	iLock := strings.Index(body, "r.mu.Lock()")
	iDefer := strings.Index(body, "defer r.mu.Unlock()")
	iTouch := strings.Index(body, "r.in.GetData()")
	iRun := strings.Index(body, "r.sess.Run()")
	iCopy := strings.LastIndex(body, "copy(res,")
	switch {
	case iLock < 0 || iDefer < 0 || iTouch < 0 || iRun < 0 || iCopy < 0:
		t.Fatalf("缺关键环节:\n%s", body)
	case iLock > iTouch:
		t.Fatalf("Lock 排在首次触碰共享输入张量之后 —— 写输入不在锁内:\n%s", body)
	case iDefer != iLock+len("r.mu.Lock()\n\t"):
		t.Fatalf("Unlock 不是紧跟 Lock 的 defer —— 中途错误路径可能持锁返回:\n%s", body)
	case iCopy < iRun:
		t.Fatalf("输出拷贝排在 Run 之前？:\n%s", body)
	}

	// 自证：Lock 挪到 GetData 之后的坏版本必须让次序断言落空。
	broken := strings.Replace(body,
		"r.mu.Lock()\n\tdefer r.mu.Unlock()\n\n\tdst := r.in.GetData()",
		"dst := r.in.GetData()\n\tr.mu.Lock()\n\tdefer r.mu.Unlock()", 1)
	if strings.Index(broken, "r.mu.Lock()") < strings.Index(broken, "r.in.GetData()") {
		t.Fatal("守卫失效：Lock 在 GetData 之后的版本也能通过断言")
	}
}

// checkDetectErrorPathsUnlock 逐行检查：首个 Lock 与最终 Unlock 之间的每条 return
// 前一行必须是 d.mu.Unlock()。返回发现违规的行号（0 = 无违规）。
func checkDetectErrorPathsUnlock(body string) int {
	lines := strings.Split(body, "\n")
	// 最终 Unlock 的行号（之后的 return 在锁外，不受约束）。
	lastUnlock := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "d.mu.Unlock()" {
			lastUnlock = i
		}
	}
	seenLock := false
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "d.mu.Lock()" {
			seenLock = true
			continue
		}
		if !seenLock || i >= lastUnlock {
			continue
		}
		if strings.HasPrefix(trim, "return ") && strings.TrimSpace(lines[i-1]) != "d.mu.Unlock()" {
			return i + 1
		}
	}
	return 0
}

// TestAdversarialDetectLockCoverage 钉住 d.mu（手工 Lock/Unlock）：
//  1. Lock 在 setFor（共享张量组）之前；
//  2. 正常路径 Unlock 在输出头拷贝循环**之后**；
//  3. Lock 与最终 Unlock 之间的每条 return 都必须先 Unlock（漏一条即死锁）。
func TestAdversarialDetectLockCoverage(t *testing.T) {
	body := lockFuncBody(t, "detect.go", "func (d *Detector) Detect(")

	iLock := strings.Index(body, "d.mu.Lock()")
	iSetFor := strings.Index(body, "d.setFor(size)")
	iRun := strings.Index(body, "d.sess.Run(")
	iHeads := strings.LastIndex(body, "YunetHeadData{Kind:")
	iUnlockFinal := strings.LastIndex(body, "d.mu.Unlock()")
	switch {
	case iLock < 0 || iSetFor < 0 || iRun < 0 || iHeads < 0 || iUnlockFinal < 0:
		t.Fatalf("缺关键环节:\n%s", body)
	case iLock > iSetFor:
		t.Fatalf("Lock 排在 setFor 之后 —— 张量组缓存（map+FIFO 淘汰 Destroy）不在锁内:\n%s", body)
	case iUnlockFinal < iHeads:
		t.Fatalf("最终 Unlock 排在输出头拷贝之前 —— 拷输出不在锁内，并发串台:\n%s", body)
	}

	if ln := checkDetectErrorPathsUnlock(body); ln != 0 {
		t.Fatalf("Detect 第 %d 行的 return 前没有 d.mu.Unlock() —— 该错误路径会让 Detector 永久死锁:\n%s", ln, body)
	}

	// 自证：删掉一条错误路径 Unlock 的坏版本必须被逐行检查拦住。
	broken := strings.Replace(body, "\t\td.mu.Unlock()\n\t\treturn nil, err\n", "\t\treturn nil, err\n", 1)
	if broken == body {
		t.Fatal("自证替换未生效（源码形态变了，本测试需同步）")
	}
	if checkDetectErrorPathsUnlock(broken) == 0 {
		t.Fatal("守卫失效：删掉一条错误路径 Unlock 也没被逐行检查拦住")
	}
}
