package transcode

import "testing"

func TestLadderForProfile(t *testing.T) {
	cases := map[string]int{"1080p": 1, "2k": 2, "4k": 3, "": 1, "unknown": 1}
	for p, want := range cases {
		if got := len(LadderForProfile(p)); got != want {
			t.Fatalf("profile %q 档位数应为 %d，实际 %d", p, want, got)
		}
	}
	if !validProfile("2k") || validProfile("720p") {
		t.Fatal("validProfile 判定错误")
	}
}
