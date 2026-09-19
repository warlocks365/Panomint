// Package vecutil 向量小工具（审查 P2-06 收敛）：L2 归一化、余弦相似度、
// pgvector 字面量序列化 / 解析。
//
// 历史：L2Normalize/CosineSimilarity 曾在 embed/embed.go 与 faces/face.go 逐字重复两份；
// vectorLiteral/parseVector 在 embed/store.go、faces/face.go、tags/labelcache.go 各有一份，
// 精度策略不同是**有意的**（见 Literal 的 prec 参数），但可共享一个带精度参数的实现。
// 现统一收敛到本包，各调用方按自身精度策略传参。
package vecutil

import (
	"math"
	"strconv"
	"strings"
)

// L2Normalize 原地 L2 归一化（零向量原样返回）。
func L2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := math.Sqrt(sum)
	if n == 0 {
		return v
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

// CosineSimilarity 余弦相似度（输入已归一化时等价于点积；与模长无关）。
func CosineSimilarity(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Literal float32 切片 → pgvector 文本字面量 "[v1,v2,...]"。
//
// 用 'f' 定点格式（不产生科学计数法，pgvector 的输入解析必定接受）；
// prec 透传 strconv.FormatFloat，两种精度策略各有归宿：
//   - prec=6  ：图像向量（embed / faces）够用，字面量更短（原 embed.VectorLiteral 行为）；
//   - prec=-1 ：能精确回读该 float32 的最短十进制表示——tags 标签缓存的值必须与 ORT 真值
//     逐位相等（否则「命中缓存」与「现算」两条路径的相似度会有 <5e-7 偏差，
//     缓存路径的正确性就无法用 deepEqual 断言），故该路径追求无损回读。
func Literal(v []float32, prec int) string {
	var sb strings.Builder
	sb.Grow(len(v) * 12)
	sb.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(x), 'f', prec, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// Parse 解析 pgvector 文本表示 "[v1,v2,...]"；空串 / 空向量 / 任一元素非法时返回 nil。
func Parse(s string) []float32 {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil
		}
		out = append(out, float32(f))
	}
	return out
}
