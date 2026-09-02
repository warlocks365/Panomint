package ffmpeg

import "testing"

// scaledSize 复刻 HLSArgs 中 scale=w=..:h=..:force_original_aspect_ratio=decrease 的输出尺寸。
func scaledSize(srcW, srcH, boxW, boxH int) (int, int) {
	s := float64(boxW) / float64(srcW)
	if sh := float64(boxH) / float64(srcH); sh < s {
		s = sh
	}
	return int(float64(srcW)*s + 1e-9), int(float64(srcH)*s + 1e-9)
}

func TestLadderForSourceNoUpscale(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"720p", 1280, 720},
		{"1080p", 1920, 1080},
		{"1440p", 2560, 1440},
		{"4K", 3840, 2160},
		{"2:1全景4K", 3840, 1920},
		{"2:1全景5.7K", 5760, 2880},
		{"小源", 320, 240},
		{"竖屏", 720, 1280},
		{"超宽", 1000, 400},
	}
	for _, c := range cases {
		ladder := LadderForSource(c.w, c.h)
		if len(ladder) == 0 {
			t.Fatalf("%s(%dx%d): 阶梯不应为空", c.name, c.w, c.h)
		}
		if len(ladder) > maxRenditions {
			t.Fatalf("%s: 档位数 %d 超过上限 %d", c.name, len(ladder), maxRenditions)
		}
		for i, r := range ladder {
			if i > 0 && ladder[i-1].Height <= r.Height {
				t.Fatalf("%s: 阶梯未严格降序: %v", c.name, ladder)
			}
			outW, outH := scaledSize(c.w, c.h, r.Width, r.Height)
			if outW > c.w || outH > c.h {
				t.Fatalf("%s(%dx%d): 档位 %s(%dx%d) 实际输出 %dx%d — 发生上采样",
					c.name, c.w, c.h, r.Name, r.Width, r.Height, outW, outH)
			}
		}
	}
}

func TestLadderForSourceTopRung(t *testing.T) {
	cases := []struct {
		name    string
		w, h    int
		topName string
		wantOut [2]int // 顶档实际输出宽高
	}{
		{"720p源只到720p", 1280, 720, "720p", [2]int{1280, 720}},
		{"1080p源只到1080p", 1920, 1080, "1080p", [2]int{1920, 1080}},
		{"4K_16:9到2160p", 3840, 2160, "2160p", [2]int{3840, 2160}},
		{"2:1全景按宽命中2160p", 3840, 1920, "2160p", [2]int{3840, 1920}},
	}
	for _, c := range cases {
		ladder := LadderForSource(c.w, c.h)
		if ladder[0].Name != c.topName {
			t.Fatalf("%s: 顶档应为 %s，实际 %s（%v）", c.name, c.topName, ladder[0].Name, ladder)
		}
		gotW, gotH := scaledSize(c.w, c.h, ladder[0].Width, ladder[0].Height)
		if gotW != c.wantOut[0] || gotH != c.wantOut[1] {
			t.Fatalf("%s: 顶档输出应为 %dx%d，实际 %dx%d", c.name, c.wantOut[0], c.wantOut[1], gotW, gotH)
		}
	}
}

func TestLadderForSource720pNoHighRungs(t *testing.T) {
	ladder := LadderForSource(1280, 720)
	want := []string{"720p", "480p", "360p"}
	if len(ladder) != len(want) {
		t.Fatalf("720p 源档位数应为 %d，实际 %d: %v", len(want), len(ladder), ladder)
	}
	for i, name := range want {
		if ladder[i].Name != name {
			t.Fatalf("720p 源第 %d 档应为 %s，实际 %s", i, name, ladder[i].Name)
		}
	}
}

func TestLadderForSourceUnknownOrTiny(t *testing.T) {
	if got := LadderForSource(0, 0); got != nil {
		t.Fatalf("分辨率未知时应返回 nil，实际 %v", got)
	}
	ladder := LadderForSource(321, 241) // 比 360p 还小且为奇数
	if len(ladder) != 1 {
		t.Fatalf("小源应退化为单档，实际 %v", ladder)
	}
	r := ladder[0]
	if r.Width > 321 || r.Height > 241 {
		t.Fatalf("小源单档不得上采样: %v", r)
	}
	if r.Width%2 != 0 || r.Height%2 != 0 {
		t.Fatalf("小源单档边长须为偶数: %v", r)
	}
}
