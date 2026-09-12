package embed

// BERT WordPiece 分词器（Chinese-CLIP 文本塔用）。
//
// 与 CLIP 的 BPE 完全不同：Chinese-CLIP 的文本塔是中文 BERT，tokenizer.json 声明
//   normalizer    : BertNormalizer(clean_text, handle_chinese_chars, lowercase)
//   pre_tokenizer : BertPreTokenizer（按空白与标点切分；CJK 逐字）
//   model         : WordPiece（vocab 21128，续接前缀 "##"）
//   post_processor: TemplateProcessing → [CLS] A [SEP]
//
// 正确性以 HF `tokenizers` 库输出为基准（见 bert_test.go 的对照用例）。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// BertContextLength Chinese-CLIP 文本侧默认最大长度（官方推理配方）。
const BertContextLength = 52

// maxCharsPerWord WordPiece 单字上限（tokenizer.json 的 max_input_chars_per_word）。
const maxCharsPerWord = 100

// BertTokenizer 中文 BERT 分词器。
type BertTokenizer struct {
	vocab map[string]int32
	clsID int32
	sepID int32
	padID int32
	unkID int32
}

type bertTokenizerFile struct {
	Model struct {
		Type                    string         `json:"type"`
		UnkToken                string         `json:"unk_token"`
		ContinuingSubwordPrefix string         `json:"continuing_subword_prefix"`
		MaxInputCharsPerWord    int            `json:"max_input_chars_per_word"`
		Vocab                   map[string]int `json:"vocab"`
	} `json:"model"`
	AddedTokens []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
	} `json:"added_tokens"`
}

// LoadBertTokenizer 从 tokenizer.json 载入。
func LoadBertTokenizer(path string) (*BertTokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 tokenizer 失败: %w", err)
	}
	var f bertTokenizerFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("解析 tokenizer.json 失败: %w", err)
	}
	if f.Model.Type != "WordPiece" {
		return nil, fmt.Errorf("非 WordPiece 分词器：%s", f.Model.Type)
	}
	t := &BertTokenizer{vocab: make(map[string]int32, len(f.Model.Vocab))}
	for k, v := range f.Model.Vocab {
		t.vocab[k] = int32(v)
	}
	lookup := func(s string) int32 {
		if id, ok := t.vocab[s]; ok {
			return id
		}
		return -1
	}
	t.clsID = lookup("[CLS]")
	t.sepID = lookup("[SEP]")
	t.padID = lookup("[PAD]")
	t.unkID = lookup("[UNK]")
	return t, nil
}

// VocabSize 便于诊断/测试。
func (t *BertTokenizer) VocabSize() int { return len(t.vocab) }

// PadID 补齐用的 token（[PAD]=0）。
func (t *BertTokenizer) PadID() int32 { return t.padID }

// Encode 文本 → 定长 BertContextLength 的 token ID 序列（含 [CLS]/[SEP]，不足补 [PAD]）。
func (t *BertTokenizer) Encode(text string) []int32 {
	ids := t.EncodeNoPad(text)
	out := make([]int32, BertContextLength)
	copy(out, ids)
	for i := len(ids); i < BertContextLength; i++ {
		out[i] = t.padID
	}
	return out
}

// EncodeNoPad 返回未补齐的 token 序列（含首尾特殊 token）。
func (t *BertTokenizer) EncodeNoPad(text string) []int32 {
	pieces := t.wordPiece(t.preTokenize(t.normalize(text)))
	ids := make([]int32, 0, BertContextLength)
	ids = append(ids, t.clsID)
	const maxBody = BertContextLength - 2
	for _, id := range pieces {
		if len(ids)-1 >= maxBody {
			break
		}
		ids = append(ids, id)
	}
	ids = append(ids, t.sepID)
	return ids
}

// AttentionMask 由 token 序列生成（[PAD] 位置为 0，其余为 1）。
func (t *BertTokenizer) AttentionMask(ids []int32) []int64 {
	m := make([]int64, len(ids))
	for i, id := range ids {
		if id != t.padID {
			m[i] = 1
		}
	}
	return m
}

// normalize 实现 BertNormalizer：
//
//	clean_text           去掉控制字符（保留 \t\n\r 并转为空格）
//	handle_chinese_chars 在 CJK 字符两侧补空格（使其逐字成 token）
//	lowercase            小写 + 去音标（strip_accents 跟随 lowercase）
func (t *BertTokenizer) normalize(s string) string {
	// clean_text + handle_chinese_chars
	var sb strings.Builder
	sb.Grow(len(s) + 16)
	for _, r := range s {
		if isBertControl(r) {
			continue
		}
		if isBertWhitespace(r) {
			sb.WriteRune(' ')
			continue
		}
		if isCJK(r) {
			sb.WriteRune(' ')
			sb.WriteRune(r)
			sb.WriteRune(' ')
			continue
		}
		sb.WriteRune(r)
	}
	out := sb.String()
	// lowercase + strip accents（NFD 后去掉 Mn 组合记号）
	out = strings.ToLower(out)
	out = stripAccents(out)
	return out
}

// stripAccents NFD 分解后移除 Mn（非间距组合记号）。
func stripAccents(s string) string {
	d := norm.NFD.String(s)
	var sb strings.Builder
	sb.Grow(len(d))
	for _, r := range d {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func isBertWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || unicode.IsSpace(r)
}

// isBertControl 控制字符（Cc/Cf），\t\n\r 除外（由 whitespace 分支处理）。
func isBertControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r)
}

// isCJK BERT 认为需要逐字切分的中日韩表意文字区间。
func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x20000 && r <= 0x2A6DF,
		r >= 0x2A700 && r <= 0x2B73F,
		r >= 0x2B740 && r <= 0x2B81F,
		r >= 0x2B820 && r <= 0x2CEAF,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0x2F800 && r <= 0x2FA1F:
		return true
	}
	return false
}

// isBertPunct 标点：ASCII 可见标点 ∪ Unicode P* 类别。
func isBertPunct(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// preTokenize BertPreTokenizer：按空白切分，再把标点单独成片。
func (t *BertTokenizer) preTokenize(s string) []string {
	out := []string{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if isBertWhitespace(r) {
			flush()
			continue
		}
		if isBertPunct(r) {
			flush()
			out = append(out, string(r))
			continue
		}
		cur.WriteRune(r)
	}
	flush()
	return out
}

// wordPiece 贪心最长匹配；非首片加 "##" 前缀；超过 maxCharsPerWord 或无法匹配则整体记为 [UNK]。
func (t *BertTokenizer) wordPiece(pieces []string) []int32 {
	ids := make([]int32, 0, len(pieces))
	for _, p := range pieces {
		ids = append(ids, t.wordPieceOne(p)...)
	}
	return ids
}

func (t *BertTokenizer) wordPieceOne(piece string) []int32 {
	runes := []rune(piece)
	if len(runes) > maxCharsPerWord {
		return []int32{t.unkID}
	}
	out := []int32{}
	start := 0
	for start < len(runes) {
		end := len(runes)
		cur := []int32{}
		for start < end {
			sub := string(runes[start:end])
			if start > 0 {
				sub = "##" + sub
			}
			if id, ok := t.vocab[sub]; ok {
				cur = []int32{id}
				break
			}
			end--
		}
		if len(cur) == 0 {
			// 任一片无法匹配 → 整词记 [UNK]（与 HF WordPiece 行为一致）
			return []int32{t.unkID}
		}
		out = append(out, cur...)
		start = end
	}
	return out
}
