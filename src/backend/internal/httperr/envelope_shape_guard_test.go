package httperr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 封套字面量只允许出现在本文件所在包内。
//
// 为什么需要这条守卫：项目在 Job000030 把散落在各包的 8 个 `errResp/fail/errJSON` 与
// 33 处内联 `c.JSON(status, gin.H{"error": gin.H{...}})` 收敛到了本包的 `Envelope`/`Abort`。
// 但**收敛是一次性的动作，不是一种机制** —— 只要有人下次图省事再写一行内联字面量，
// 封套形状就又有了第二份实现，而"形状漂移"正是当初让 `err.Error()` 回显在 8 个包里
// 各自蔓延的土壤（见 errtext_guard_test.go 的登记表）。
//
// 所以这里把"只有一份形状"变成**可执行的断言**：任何 `gin.H{"error"` 出现在本包之外即失败。
//
// 唯一允许的位置：internal/httperr/httperr.go 的 Envelope 实现本身。
var allowedEnvelopeFile = "internal/httperr/httperr.go"

// envelopeMark 是"构造错误封套"的内容特征。
//
// 用 `"error": gin.H{` 而不是 `gin.H{"error"` 是因为它同时覆盖两种书写顺序，
// 且不会被 `"error": <字符串变量>` 这类**不是构造封套**的写法误伤（那种没有 gin.H{）。
const envelopeMark = `"error": gin.H{`

// TestEnvelopeShapeHasSingleImplementation 是守卫本体。
func TestEnvelopeShapeHasSingleImplementation(t *testing.T) {
	root := backendRoot(t)
	var offenders []string

	for _, top := range []string{"internal", "cmd"} {
		dir := filepath.Join(root, top)
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if rel == allowedEnvelopeFile {
				return nil // 唯一真源
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for i, ln := range strings.Split(string(b), "\n") {
				// 注释里的提及不算（本项目曾因断言太宽而误报）。
				if strings.HasPrefix(strings.TrimSpace(ln), "//") {
					continue
				}
				if strings.Contains(ln, envelopeMark) {
					offenders = append(offenders, rel+":"+itoa(i+1)+"  "+strings.TrimSpace(ln))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", dir, err)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("错误封套出现了第二份实现（%d 处）：\n  %s\n\n"+
			"封套形状的唯一真源是 internal/httperr 的 Envelope/Abort。\n"+
			"请改为：\n"+
			"  · 普通 handler      → httperr.Envelope(c, status, \"CODE\", msg)\n"+
			"  · 来自本端之外的错误 → httperr.Fail(c, status, \"CODE\", \"固定文案\", err)（完整错误只进日志）\n"+
			"  · 中间件（需中断）   → httperr.Abort(c, status, \"CODE\", msg)\n"+
			"若确实需要新增形状，请改 internal/httperr/httperr.go 而不是在别处复制一份。",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// itoa 避免为一处拼字符串引入 strconv（本包刻意保持极小依赖）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
