package dirscope

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "photos", "trip")
	if err := mkdirAll(sub); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		dir     string
		want    string // 期望的绝对路径（"" 表示期望报错）
		wantErr error
	}{
		{name: "空=根本身", dir: "", want: root},
		{name: "点=根本身", dir: ".", want: root},
		{name: "空白=根本身", dir: "   ", want: root},
		{name: "相对子目录", dir: "photos/trip", want: sub},
		{name: "相对带子目录穿越", dir: "photos/../photos/trip", want: sub},
		{name: "根内绝对路径", dir: sub, want: sub},
		{name: "越界相对穿越", dir: "../etc", wantErr: ErrEscapesScope},
		{name: "越界多层穿越", dir: "photos/../../..", wantErr: ErrEscapesScope},
		{name: "越界绝对路径", dir: volumeRoot(t), wantErr: ErrEscapesScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(root, tc.dir)
			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("期望 %v，得到 %v", tc.wantErr, err)
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

func TestWithin(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "photos")
	if err := mkdirAll(sub); err != nil {
		t.Fatal(err)
	}
	if !Within(root, sub) {
		t.Fatal("子目录应判定为在 root 内")
	}
	if !Within(root, root) {
		t.Fatal("root 本身应判定为在 root 内（闭区间）")
	}
	if Within(root, filepath.Join(root, "..")) {
		t.Fatal("父目录应判定为越界")
	}
	if Within(root, volumeRoot(t)) {
		t.Fatal("跨盘符/卷根应判定为越界")
	}
}

func TestRelDisplay(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "photos", "trip")
	if err := mkdirAll(sub); err != nil {
		t.Fatal(err)
	}
	if got := RelDisplay(root, root); got != "." {
		t.Fatalf("根本身应显示 \".\"，得到 %q", got)
	}
	if got := RelDisplay(root, sub); got != "photos/trip" {
		t.Fatalf("期望 photos/trip，得到 %q", got)
	}
}

func TestHasReserved(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{"", false},
		{".", false},
		{"photos/trip", false},
		{"_imports", true},
		{"photos/_imports", true},
		{"photos/_imports/abcdef12", true},
		{"photos/trip/x_imports_y", false}, // 仅整段匹配，子串不算
		{"imports/正常目录", false},
	}
	for _, tc := range cases {
		if got := HasReserved(tc.rel); got != tc.want {
			t.Fatalf("HasReserved(%q) = %v，期望 %v", tc.rel, got, tc.want)
		}
	}
}

func mkdirAll(p string) error { return os.MkdirAll(p, 0o755) }

func volumeRoot(t *testing.T) string {
	t.Helper()
	vol := filepath.VolumeName(mustAbs(t))
	return vol + string(filepath.Separator)
}

func mustAbs(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
