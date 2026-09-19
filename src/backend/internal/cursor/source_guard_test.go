package cursor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cursorEncodingMark 是"在做游标编解码"的内容特征。
//
// 用 `base64.URLEncoding`：本仓这个写法**只**被游标用到（Job000031 普查确认），
// 所以它是精确的判据。若将来出现与游标无关的 URL-safe base64 用途，
// 应当**显式**把它加进 allow 名单并在理由里写清 —— 而不是把守卫放宽到"随便用"。
const cursorEncodingMark = "base64.URLEncoding"

// allowedFiles 是允许出现游标编解码的文件（相对 src/backend）。
var allowedFiles = map[string]bool{
	"internal/cursor/cursor.go": true, // 唯一真源
}

// TestCursorCodecHasSingleImplementation 是守卫本体。
//
// 为什么需要它：本包之所以存在，是因为**三处逐字节相同的实现**（audit / media / search）
// 各自维护了一份游标编解码，而 audit 的注释里就写着"与 internal/media 同构" ——
// 即作者知道重复、却没有真源可引用。三份同形实现最危险的地方不是多几行代码，
// 而是**改一处忘一处**：游标是对外契约，某处单独调了时间精度或分隔符之后，
// 只有那一个端点的分页会静默错位（跨页丢行/重复行），测试极难暴露。
//
// 所以这里把"只有一份游标编解码"变成可执行的断言。收敛是一次性动作，守卫才是机制。
func TestCursorCodecHasSingleImplementation(t *testing.T) {
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
			if allowedFiles[rel] {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for i, ln := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(strings.TrimSpace(ln), "//") {
					continue // 注释里的提及不算（本项目曾因断言太宽而误报）
				}
				if strings.Contains(ln, cursorEncodingMark) {
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
		t.Errorf("游标编解码出现了第二份实现（%d 处）：\n  %s\n\n"+
			"唯一真源是 internal/cursor（Encode/Decode/EncodeScored/DecodeScored）。\n"+
			"⚠️ 游标是**对外契约**：别处自己再写一份，会导致某一天只改一处、\n"+
			"   而那个端点的分页静默错位（跨页丢行/重复行），测试极难发现。\n"+
			"请改为 import \"panoalbum/internal/cursor\" 并调用它。\n"+
			"若确属与游标无关的 URL-safe base64 用途，请把该文件加进本测试的 allowedFiles\n"+
			"并在注释里写明理由 —— 不要直接放宽判据。",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// backendRoot 由本测试文件位置反推 src/backend 根目录（不依赖 go test 的 cwd 约定）。
func backendRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败，无法定位仓库根")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
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
