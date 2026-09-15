package transcode

import "testing"

func TestLadderForSourceProfile(t *testing.T) {
	cases := []struct {
		profile   string
		w, h      int
		wantNames []string
	}{
		{"1080p", 1280, 720, []string{"720p", "480p", "360p"}},           // 720p 源：绝不上采样到 1080p
		{"1080p", 1920, 1080, []string{"1080p", "720p", "480p", "360p"}}, // 1080p 源：到 1080p 为止
		{"4k", 3840, 2160, []string{"2160p", "1440p", "1080p", "720p"}},  // 4K：最高 4 档
		{"4k", 3840, 1920, []string{"2160p", "1440p", "1080p", "720p"}},  // 2:1 全景按宽命中 2160p
		{"1080p", 3840, 1920, []string{"1080p", "720p"}},                 // 档位限高
		{"2k", 2560, 1440, []string{"1440p", "1080p", "720p", "480p"}},
	}
	for _, c := range cases {
		ladder := LadderForSourceProfile(c.profile, c.w, c.h)
		if len(ladder) != len(c.wantNames) {
			t.Fatalf("profile=%s src=%dx%d: 期望 %v，实际 %v", c.profile, c.w, c.h, c.wantNames, ladder)
		}
		for i, name := range c.wantNames {
			if ladder[i].Name != name {
				t.Fatalf("profile=%s src=%dx%d: 第 %d 档应为 %s，实际 %s",
					c.profile, c.w, c.h, i, name, ladder[i].Name)
			}
		}
	}
}

func TestLadderForSourceProfileUnknownSize(t *testing.T) {
	if got := LadderForSourceProfile("1080p", 0, 0); got != nil {
		t.Fatalf("分辨率未知时应返回 nil 以便调用方兜底，实际 %v", got)
	}
}

func TestProfileForSource(t *testing.T) {
	cases := []struct {
		w, h  int
		want  string
		valid bool
	}{
		{1280, 720, "1080p", true},
		{1920, 1080, "1080p", true},
		{2560, 1440, "2k", true},
		{3840, 2160, "4k", true},
		{3840, 1920, "4k", true}, // 2:1 全景
		{0, 0, "1080p", true},    // 未知分辨率保守档
	}
	for _, c := range cases {
		got := ProfileForSource(c.w, c.h)
		if got != c.want {
			t.Fatalf("src=%dx%d: 档位应为 %s，实际 %s", c.w, c.h, c.want, got)
		}
		if validProfile(got) != c.valid {
			t.Fatalf("src=%dx%d: 档位 %s 未通过 validProfile", c.w, c.h, got)
		}
	}
}
