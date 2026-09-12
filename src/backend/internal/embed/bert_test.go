package embed

// Chinese-CLIP BERT 分词器对照测试。
// 期望值由 HF `tokenizers` 库对同一 tokenizer.json 编码得到：
//   Tokenizer.from_file('tokenizer.json').encode(s).ids

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// chineseClipDir 定位 Chinese-CLIP 模型目录（不入库，缺失时跳过）。
func chineseClipDir(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "assets", "models", "chinese-clip"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, "tokenizer.json")); err != nil {
		t.Skipf("Chinese-CLIP 目录不存在，跳过：%s", p)
	}
	return p
}

func TestBertTokenizerMatchesHuggingFace(t *testing.T) {
	tk, err := LoadBertTokenizer(filepath.Join(chineseClipDir(t), "tokenizer.json"))
	if err != nil {
		t.Fatalf("加载 BERT tokenizer 失败: %v", err)
	}
	t.Logf("vocab=%d cls=%d sep=%d pad=%d unk=%d", tk.VocabSize(), tk.clsID, tk.sepID, tk.padID, tk.unkID)

	cases := []struct {
		text string
		want []int32
	}{
		{"雪景", []int32{101, 7434, 3250, 102}},
		{"日落", []int32{101, 3189, 5862, 102}},
		{"故宫雪景", []int32{101, 3125, 2151, 7434, 3250, 102}},
		{"极光", []int32{101, 3353, 1045, 102}},
		{"a photo of a cat", []int32{101, 143, 9020, 8205, 143, 10165, 102}},
		{"长城 the Great Wall", []int32{101, 7270, 1814, 8174, 10512, 12284, 102}},
	}
	for _, c := range cases {
		got := tk.EncodeNoPad(c.text)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("EncodeNoPad(%q)\n got %v\nwant %v", c.text, got, c.want)
		}
	}
}

func TestBertEncodePadsAndMasks(t *testing.T) {
	tk, err := LoadBertTokenizer(filepath.Join(chineseClipDir(t), "tokenizer.json"))
	if err != nil {
		t.Fatalf("加载 BERT tokenizer 失败: %v", err)
	}
	ids := tk.Encode("雪景")
	if len(ids) != BertContextLength {
		t.Fatalf("长度应为 %d，实得 %d", BertContextLength, len(ids))
	}
	if ids[0] != 101 {
		t.Errorf("首 token 应为 [CLS]=101，实得 %d", ids[0])
	}
	if ids[1] != 7434 || ids[2] != 3250 || ids[3] != 102 {
		t.Errorf("有效区应为 [7434 3250 102]，实得 %v", ids[1:4])
	}
	for i := 4; i < BertContextLength; i++ {
		if ids[i] != 0 {
			t.Fatalf("位置 %d 应补 [PAD]=0，实得 %d", i, ids[i])
		}
	}
	mask := tk.AttentionMask(ids)
	if len(mask) != BertContextLength {
		t.Fatalf("mask 长度应为 %d", BertContextLength)
	}
	// 真实 token 计数：3（[CLS]+雪+景）+1（[SEP]）= 4 → mask 前 4 位为 1
	for i := 0; i < 4; i++ {
		if mask[i] != 1 {
			t.Errorf("位置 %d 的 mask 应为 1，实得 %d", i, mask[i])
		}
	}
	for i := 4; i < BertContextLength; i++ {
		if mask[i] != 0 {
			t.Errorf("PAD 位置 %d 的 mask 应为 0，实得 %d", i, mask[i])
		}
	}
}

func TestBertLowercaseAndAccents(t *testing.T) {
	tk, err := LoadBertTokenizer(filepath.Join(chineseClipDir(t), "tokenizer.json"))
	if err != nil {
		t.Fatalf("加载 BERT tokenizer 失败: %v", err)
	}
	// 大小写与音标应被归一化（Berlin/Bérlin 得到相同编码）
	a := tk.EncodeNoPad("Berlin")
	b := tk.EncodeNoPad("bérlin")
	if !reflect.DeepEqual(a, b) {
		t.Errorf("大小写/音标归一化后应一致：%v vs %v", a, b)
	}
}
