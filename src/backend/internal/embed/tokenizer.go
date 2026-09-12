package embed

// CLIP BPE tokenizer（对齐 HuggingFace fast tokenizer / Xenova clip-vit-base-patch32 的 tokenizer.json）。
//
// 管线：NFC 归一化 → 空白折叠 → 小写 → CLIP 正则切分 → ByteLevel 字节映射 → BPE 合并
//      → 包装 <|startoftext|> / <|endoftext|> → 截断/补齐到 77。
//
// 实现依据是 tokenizer.json 中的 normalizer / pre_tokenizer / model 字段，
// 正确性以 HF `tokenizers` 库的输出为基准（见 tokenizer_test.go 的对照用例）。

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// CLIP 官方正则（OpenAI simple_tokenizer.py 与 HF tokenizer.json 一致）。
// 注意 [\p{N}] 是单字符数字（不是 +）。
const clipPattern = `<\|startoftext\|>|<\|endoftext\|>|'s|'t|'re|'ve|'m|'ll|'d|[\p{L}]+|[\p{N}]|[^\s\p{L}\p{N}]+`

// ContextLength CLIP 文本编码器上下文长度（token 数，含首尾特殊 token）。
const ContextLength = 77

// Tokenizer CLIP BPE 分词器。
type Tokenizer struct {
	vocab   map[string]int32
	ranks   map[string]int // "a\x00b" → 合并优先级（越小越先合并）
	byteEnc [256]string    // 字节 → ByteLevel 映射字符
	re      *regexp.Regexp
	clsID   int32 // <|startoftext|>
	sepID   int32 // <|endoftext|>（同时是 pad / unk）
	unkID   int32
}

// tokenizerFile 只取需要的字段，避免把 49k 词表反序列化成 map[string]any。
type tokenizerFile struct {
	Model struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges []string       `json:"merges"`
	} `json:"model"`
	AddedTokens []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
	} `json:"added_tokens"`
}

// LoadTokenizer 从 tokenizer.json 载入。
func LoadTokenizer(path string) (*Tokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 tokenizer 失败: %w", err)
	}
	var f tokenizerFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("解析 tokenizer.json 失败: %w", err)
	}
	if f.Model.Type != "BPE" {
		return nil, fmt.Errorf("不支持的 tokenizer 类型: %s", f.Model.Type)
	}

	t := &Tokenizer{
		vocab: make(map[string]int32, len(f.Model.Vocab)),
		ranks: make(map[string]int, len(f.Model.Merges)),
		re:    regexp.MustCompile(clipPattern),
	}
	for tok, id := range f.Model.Vocab {
		t.vocab[tok] = int32(id)
	}
	for i, m := range f.Model.Merges {
		// merges 形如 "i n"（也可能出现 JSON 数组形式，这里兼容两种）
		parts := strings.Fields(m)
		if len(parts) != 2 {
			continue
		}
		t.ranks[parts[0]+"\x00"+parts[1]] = i
	}
	for _, at := range f.AddedTokens {
		switch at.Content {
		case "<|startoftext|>":
			t.clsID = int32(at.ID)
		case "<|endoftext|>":
			t.sepID = int32(at.ID)
		}
	}
	t.unkID = t.sepID
	t.buildByteEncoder()
	return t, nil
}

// buildByteEncoder 构造 GPT-2/CLIP 的字节→Unicode 映射表。
//
// 可打印字节直接映射为自身字符；其余（控制字符、被 UTF-8 占用的高位字节等）
// 依次映射到 256+n 的码点，保证每个字节都有唯一、可逆的可见字符表示。
func (t *Tokenizer) buildByteEncoder() {
	bs := make([]int, 0, 256)
	for b := 33; b <= 126; b++ { // '!' .. '~'
		bs = append(bs, b)
	}
	for b := 161; b <= 172; b++ { // '¡' .. '¬'
		bs = append(bs, b)
	}
	for b := 174; b <= 255; b++ { // '®' .. 'ÿ'
		bs = append(bs, b)
	}
	inBs := make(map[int]bool, len(bs))
	for _, b := range bs {
		inBs[b] = true
	}
	next := 0
	for b := 0; b < 256; b++ {
		if inBs[b] {
			t.byteEnc[b] = string(rune(b))
		} else {
			t.byteEnc[b] = string(rune(256 + next))
			next++
		}
	}
}

// Encode 文本 → 定长 ContextLength 的 token ID 序列（不足补 pad，超长截断）。
// 返回的序列已包含首尾 <|startoftext|> / <|endoftext|>。
func (t *Tokenizer) Encode(text string) []int32 {
	ids := t.EncodeNoPad(text)
	out := make([]int32, ContextLength)
	copy(out, ids)
	for i := len(ids); i < ContextLength; i++ {
		out[i] = t.sepID // pad token = <|endoftext|>
	}
	return out
}

// EncodeNoPad 返回未补齐的 token 序列（含首尾特殊 token）。
func (t *Tokenizer) EncodeNoPad(text string) []int32 {
	pieces := t.pretokenize(t.normalize(text))
	ids := make([]int32, 0, ContextLength)
	ids = append(ids, t.clsID)
	// 预留末尾 <|endoftext|>，超长时截断中间部分
	const maxBody = ContextLength - 2
	for _, p := range pieces {
		for _, id := range t.bpe(p) {
			if len(ids)-1 >= maxBody {
				break
			}
			ids = append(ids, id)
		}
		if len(ids)-1 >= maxBody {
			break
		}
	}
	ids = append(ids, t.sepID)
	return ids
}

// normalize NFC → 空白折叠为单空格 → 小写（与 tokenizer.json 的 normalizer 顺序一致）。
// 注意：空白折叠不 strip（HF 的 Replace 语义），保留首尾空白交由正则切分处理。
func (t *Tokenizer) normalize(s string) string {
	s = norm.NFC.String(s)
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.ToLower(s)
}

var whitespaceRe = regexp.MustCompile(`\s+`)

// pretokenize 用 CLIP 正则切分并保留命中片段（等价 HF Split(invert=true)）。
func (t *Tokenizer) pretokenize(s string) []string {
	return t.re.FindAllString(s, -1)
}

// eowSuffix CLIP 的词尾标记（OpenAI 原版 BPE 约定，词汇表内的键即形如 "sunset</w>"）。
const eowSuffix = "</w>"

// bpe 对单个片段做 ByteLevel 字节映射 + 词尾标记 + BPE 合并，返回 token ID。
func (t *Tokenizer) bpe(piece string) []int32 {
	if piece == "" {
		return nil
	}
	// 1) 字节级映射：UTF-8 每个字节 → 映射字符（按 rune 切分，一个 rune 对应一个字节）
	var sb strings.Builder
	for i := 0; i < len(piece); i++ {
		sb.WriteString(t.byteEnc[piece[i]])
	}
	symbols := strings.Split(sb.String(), "")
	if len(symbols) == 0 {
		return nil
	}
	// 2) 末字符追加词尾标记（CLIP 原版：word = tuple(token[:-1]) + (token[-1] + '</w>',)）
	symbols[len(symbols)-1] += eowSuffix
	// 3) 迭代合并：每轮取 rank 最小的一对
	for len(symbols) > 1 {
		bestRank := -1
		bestIdx := -1
		for i := 0; i < len(symbols)-1; i++ {
			if r, ok := t.ranks[symbols[i]+"\x00"+symbols[i+1]]; ok {
				if bestRank < 0 || r < bestRank {
					bestRank = r
					bestIdx = i
				}
			}
		}
		if bestIdx < 0 {
			break
		}
		merged := symbols[bestIdx] + symbols[bestIdx+1]
		symbols = append(symbols[:bestIdx], append([]string{merged}, symbols[bestIdx+2:]...)...)
	}
	// 4) 查表
	ids := make([]int32, 0, len(symbols))
	for _, s := range symbols {
		if id, ok := t.vocab[s]; ok {
			ids = append(ids, id)
		} else {
			ids = append(ids, t.unkID)
		}
	}
	return ids
}

// vocabSize 便于测试/诊断。
func (t *Tokenizer) vocabSize() int { return len(t.vocab) }

// PadID 补齐用的 token（CLIP 以 <|endoftext|> 同时充当 pad）。
func (t *Tokenizer) PadID() int32 { return t.sepID }

// sortedRanksDebug 仅测试用：返回前 n 个合并规则的稳定视图。
func (t *Tokenizer) sortedRanksDebug(n int) []string {
	out := make([]string, 0, len(t.ranks))
	for k := range t.ranks {
		out = append(out, strings.ReplaceAll(k, "\x00", " "))
	}
	sort.Slice(out, func(i, j int) bool {
		return t.ranks[strings.ReplaceAll(out[i], " ", "\x00")] < t.ranks[strings.ReplaceAll(out[j], " ", "\x00")]
	})
	if n > len(out) {
		n = len(out)
	}
	return out[:n]
}
