package embed

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// modelDir 定位本地模型目录（模型不入库，缺失时跳过）。
func modelDir(t *testing.T) string {
	t.Helper()
	// internal/embed → 上溯 4 级到仓库根 → assets/models/clip
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "assets", "models", "clip"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, "tokenizer.json")); err != nil {
		t.Skipf("模型目录不存在，跳过：%s", p)
	}
	return p
}

// referenceCases 由 HF `tokenizers` 库对同一 tokenizer.json 编码得到，作为实现基准：
//
//	Tokenizer.from_file('tokenizer.json').encode(s).ids
func TestTokenizerMatchesHuggingFace(t *testing.T) {
	tk, err := LoadTokenizer(filepath.Join(modelDir(t), "tokenizer.json"))
	if err != nil {
		t.Fatalf("加载 tokenizer 失败: %v", err)
	}
	t.Logf("vocab=%d merges=%d cls=%d sep=%d", tk.vocabSize(), len(tk.ranks), tk.clsID, tk.sepID)

	cases := []struct {
		text string
		want []int32
	}{
		{"a photo of a cat", []int32{49406, 320, 1125, 539, 320, 2368, 49407}},
		{"sunset", []int32{49406, 3424, 49407}},
		{"日落", []int32{49406, 39121, 164, 238, 377, 49407}},
		{"a dog's toy", []int32{49406, 320, 1929, 568, 5988, 49407}},
		{"Hello World", []int32{49406, 3306, 1002, 49407}},
	}
	for _, c := range cases {
		got := tk.EncodeNoPad(c.text)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("EncodeNoPad(%q)\n got %v\nwant %v", c.text, got, c.want)
		}
	}
}

func TestEncodePadsToContextLength(t *testing.T) {
	tk, err := LoadTokenizer(filepath.Join(modelDir(t), "tokenizer.json"))
	if err != nil {
		t.Fatalf("加载 tokenizer 失败: %v", err)
	}
	ids := tk.Encode("sunset")
	if len(ids) != ContextLength {
		t.Fatalf("长度应为 %d，实得 %d", ContextLength, len(ids))
	}
	if ids[0] != 49406 {
		t.Errorf("首 token 应为 <|startoftext|>=49406，实得 %d", ids[0])
	}
	if ids[1] != 3424 || ids[2] != 49407 {
		t.Errorf("有效区应为 [3424, 49407]，实得 %v", ids[1:4])
	}
	for i := 3; i < ContextLength; i++ {
		if ids[i] != 49407 {
			t.Fatalf("位置 %d 应补 pad=49407，实得 %d", i, ids[i])
		}
	}
}

func TestEncodeTruncatesLongText(t *testing.T) {
	tk, err := LoadTokenizer(filepath.Join(modelDir(t), "tokenizer.json"))
	if err != nil {
		t.Fatalf("加载 tokenizer 失败: %v", err)
	}
	long := ""
	for i := 0; i < 300; i++ {
		long += "word "
	}
	ids := tk.Encode(long)
	if len(ids) != ContextLength {
		t.Fatalf("超长文本应截断到 %d，实得 %d", ContextLength, len(ids))
	}
	if ids[0] != 49406 || ids[ContextLength-1] != 49407 {
		t.Errorf("截断后仍应保留首尾特殊 token：首=%d 尾=%d", ids[0], ids[ContextLength-1])
	}
}
