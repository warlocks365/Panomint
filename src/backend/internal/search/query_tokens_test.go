package search

// 查询 token 化测试：英文停用词应被剔除（避免 in/the 之类宽泛命中目录名），
// 中文词与有意义英文词必须保留；全停用词时回退不剔除以免检索退化为无条件。

import (
	"reflect"
	"testing"
)

func TestQueryTokensDropsEnglishStopWords(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"aurora in the night sky", []string{"aurora", "night", "sky"}},
		{"city skyline at night", []string{"city", "skyline", "night"}},
		{"snow", []string{"snow"}},
		{"故宫", []string{"故宫"}},
		{"故宫 snow", []string{"故宫", "snow"}},
		{"  ", nil},
	}
	for _, c := range cases {
		got := queryTokens(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("queryTokens(%q) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

// 全为停用词时回退为原 token（否则 WHERE 只剩空间可见性条件，等于无条件检索）。
func TestQueryTokensAllStopWordsFallsBack(t *testing.T) {
	got := queryTokens("the of in")
	if len(got) != 3 {
		t.Fatalf("全停用词应回退保留原 token，实得 %v", got)
	}
}

// 中文 1~2 字词必须保留（不能因"短"被误判为停用词）。
func TestQueryTokensKeepsShortChinese(t *testing.T) {
	for _, q := range []string{"雪", "长城", "西湖"} {
		got := queryTokens(q)
		if len(got) != 1 || got[0] != q {
			t.Errorf("queryTokens(%q) 应保留原词，实得 %v", q, got)
		}
	}
}
