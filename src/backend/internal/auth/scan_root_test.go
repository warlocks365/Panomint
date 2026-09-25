package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"panoalbum/internal/dirscope"
)

// TestNormalizeScanRoot 穷举 normalizeScanRoot 的校验链（Job000123）。
// 这是"分配扫描根"的安全把关：穿越 / 非真实目录 / 保留区都必须被钉死。
func TestNormalizeScanRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "photos", "trip")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "afile.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reserved := filepath.Join(root, "_imports", "abc123")
	if err := os.MkdirAll(reserved, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedReserved := filepath.Join(root, "photos", "_imports")
	if err := os.MkdirAll(nestedReserved, 0o755); err != nil {
		t.Fatal(err)
	}
	lookalike := filepath.Join(root, "photos", "x_imports_y")
	if err := os.MkdirAll(lookalike, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr error // 非 nil 时校验错误类别（用 errors.Is 比对的哨兵）；特殊字符串用 errText
		errText string
	}{
		{name: "空串=媒体根本身", raw: "", want: ""},
		{name: "点=媒体根本身", raw: ".", want: ""},
		{name: "空白=媒体根本身", raw: "   ", want: ""},
		{name: "相对子目录", raw: "photos/trip", want: "photos/trip"},
		{name: "等价异形归一化", raw: "photos/../photos/trip", want: "photos/trip"},
		{name: "根内绝对路径", raw: sub, want: "photos/trip"},
		{name: "越界相对穿越", raw: "../etc", wantErr: dirscope.ErrEscapesScope},
		{name: "越界多层穿越", raw: "photos/../../..", wantErr: dirscope.ErrEscapesScope},
		{name: "不存在的目录", raw: "no/such/dir", errText: "扫描根目录必须是真实存在的物理目录"},
		{name: "文件而非目录", raw: "afile.txt", errText: "扫描根目录必须是目录而非文件"},
		{name: "保留段顶层", raw: "_imports", errText: "系统保留区"},
		{name: "保留段子目录", raw: "_imports/abc123", errText: "系统保留区"},
		{name: "保留段嵌套", raw: "photos/_imports", errText: "系统保留区"},
		{name: "形似保留段的正常目录", raw: "photos/x_imports_y", want: "photos/x_imports_y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeScanRoot(root, tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("期望 %v，得到 %v", tc.wantErr, err)
				}
				return
			}
			if tc.errText != "" {
				if err == nil || !contains(err.Error(), tc.errText) {
					t.Fatalf("期望含 %q 的错误，得到 %v", tc.errText, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if got != tc.want {
				t.Fatalf("期望 %q，得到 %q", tc.want, got)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
