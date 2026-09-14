package tags

// 标签文本向量的**持久缓存**（缺陷 1 修复）：
//
// 起点：NewClassifier 原先在每次进程启动时把词表逐标签编码为文本向量。
// 提示词模板从 2 条扩到 5 条后，编码次数 114×5=570，实测 CPU 下 API 绑定端口前要等 110 秒
// （期间 nginx 一直 502）。自托管场景每次重启/部署都吃这个成本，不可接受。
//
// 做法：把编码结果落到 tag_label_vectors 表，二次启动直接载入、一次 EncodeText 都不调。
//
// 三条硬约束（设计时不可让步）：
//  1. **缓存失效必须正确**：任一影响向量值的因素变化都要换 cache_key，绝不能读到过期向量。
//  2. **缓存失败绝不阻断启动**：DB 不可用 / 表不存在 / 读写失败一律降级为「照常编码」，
//     只记警告日志。缓存是优化，不能引入新的启动失败模式。
//  3. **不引入 ORT/CGO 依赖**：缓存层只用 pgx + 标准库，无 CGO 构建下同样可编译。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/embed"
)

// cacheKeyLen 取 SHA-256 十六进制的前 N 位作为 cache_key（碰撞概率对本场景可忽略）。
const cacheKeyLen = 16

// CachedLabelVector 一条标签文本向量（缓存读写的最小单元）。
// Class/Group 一并持久化：它们参与「缓存是否仍然有效」的校验（词表改了分组就应重算），
// 也让表内容自解释、便于人工排查。
type CachedLabelVector struct {
	Label string
	Class string
	Group string
	Vec   []float32
}

// LabelVectorCache 标签向量持久缓存。
//
// 抽象成接口而非直接用 *pgxpool.Pool 的原因：
//   - internal/tags 的纯逻辑单测可以用内存实现，**不需要 DB**；
//   - cmd/api 与 cmd/taggen 各自注入基于 pgxpool 的实现；
//   - ClassifyConfig.Cache 为 nil 时完全不使用缓存，行为与改动前一致。
type LabelVectorCache interface {
	// Load 按 cache_key 取该套缓存的**全部**标签向量。
	// 命中与否不由本方法决定：返回条数不符 / 维度不符由调用方校验并视为未命中。
	Load(ctx context.Context, key string) ([]CachedLabelVector, error)
	// Save 覆盖写入该 cache_key 下的标签向量集合（upsert）。
	Save(ctx context.Context, key string, items []CachedLabelVector) error
}

// labelCacheKey 计算当前「族 + 模型目录 + 词表 + 模板」组合的缓存键。
//
// 为什么这四类因素都要进 key —— 任一项变化都会让同一个标签编码出**不同的向量**，
// 复用旧向量等于把错误的语义方向带进生产：
//   - 模型族（chinese-clip / clip）：决定用哪套文本塔权重，也决定提示词用中文还是英文
//     （中文塔不认英文提示词、英文塔不认中文），换族后向量空间都不同；
//   - 模型目录（EMBED_MODEL_DIR）：决定实际加载的权重文件，换目录/换模型版本即换向量；
//   - 词表内容（Label/EN/Class/Group 的稳定哈希）：标签名或英文名变了，提示词文本就变了；
//     Group 变化虽不改变提示词，但会改变落库语义与互斥判定，一并纳入以「宁可多算不可错用」；
//   - 提示词模板集合（模板文本的稳定哈希）：模板直接决定编码出的句子，进而决定向量。
//
// 实现：各部分规范化后以 NUL 分隔拼接，取 SHA-256 前 16 位十六进制。
func labelCacheKey(fam embed.ModelFamily, modelDir string, v *Vocab) string {
	return labelCacheKeyFor(fam, modelDir, v, templatesFor(fam))
}

// labelCacheKeyFor 与 labelCacheKey 相同，但显式传入模板集合 —— 便于单测断言
// 「模板集合变化必然改变 cache_key」，也便于将来按族定制模板而无需碰 hash 逻辑。
func labelCacheKeyFor(fam embed.ModelFamily, modelDir string, v *Vocab, tmpls []string) string {
	var b strings.Builder
	b.WriteString("fam=")
	b.WriteString(string(fam))
	b.WriteByte(0)
	b.WriteString("dir=")
	b.WriteString(strings.TrimSpace(modelDir))
	b.WriteByte(0)
	b.WriteString("vocab=")
	b.WriteString(vocabDigest(v))
	b.WriteByte(0)
	b.WriteString("tmpl=")
	b.WriteString(digestStrings(tmpls))

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:cacheKeyLen]
}

// vocabDigest 词表内容的稳定哈希：按 Classes/Tags 的**原始顺序**串接
// Class.Name 与每个 TagDef 的 Label/EN/Group（字段间 \x1f、条目间 \x1e，避免拼接歧义）。
func vocabDigest(v *Vocab) string {
	if v == nil {
		v = DefaultVocab()
	}
	var b strings.Builder
	for _, cls := range v.Classes {
		b.WriteString(cls.Name)
		b.WriteByte(0x1e)
		for _, td := range cls.Tags {
			b.WriteString(td.Label)
			b.WriteByte(0x1f)
			b.WriteString(td.EN)
			b.WriteByte(0x1f)
			b.WriteString(td.Group)
			b.WriteByte(0x1e)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// digestStrings 字符串切片的稳定哈希（顺序敏感：模板顺序会改变平均方向？不会，
// 但顺序变化说明模板集合被改动过，按「宁可重算」处理）。
func digestStrings(xs []string) string {
	sum := sha256.Sum256([]byte(strings.Join(xs, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// toCachedLabels 把内存态标签向量转成缓存条目。
func toCachedLabels(labels []labelVec) []CachedLabelVector {
	out := make([]CachedLabelVector, 0, len(labels))
	for _, l := range labels {
		out = append(out, CachedLabelVector{Label: l.label, Class: l.class, Group: l.group, Vec: l.vec})
	}
	return out
}

// matchCachedLabels 判定缓存内容能否直接使用：要求与当前词表**完全一致**
// （条数相同、每个标签都存在且 Class/Group 相同），且每条向量维度必须等于
// embed.EmbeddingDim（维度不符一律视为未命中，绝不把残缺向量喂给 Suggest）。
// 命中时按词表顺序返回 labelVec，顺序与现算路径完全一致。
func matchCachedLabels(items []CachedLabelVector, v *Vocab) ([]labelVec, bool) {
	if v == nil {
		v = DefaultVocab()
	}
	want := v.LabelCount()
	if len(items) != want {
		return nil, false
	}
	byLabel := make(map[string]CachedLabelVector, len(items))
	for _, it := range items {
		byLabel[it.Label] = it
	}
	out := make([]labelVec, 0, want)
	for _, cls := range v.Classes {
		for _, td := range cls.Tags {
			it, ok := byLabel[td.Label]
			if !ok || it.Class != cls.Name || it.Group != td.Group {
				return nil, false
			}
			if len(it.Vec) != embed.EmbeddingDim {
				return nil, false
			}
			out = append(out, labelVec{label: td.Label, class: cls.Name, group: td.Group, vec: it.Vec})
		}
	}
	return out, true
}

// PGLabelVectorCache 基于 pgx 的 LabelVectorCache（表 tag_label_vectors）。
type PGLabelVectorCache struct {
	Pool *pgxpool.Pool
}

// Load 读取该 cache_key 下的全部标签向量。表不存在 / DB 不可用都会返回 error，
// 由 NewClassifier 降级为重新编码（只记警告，不阻断启动）。
func (p *PGLabelVectorCache) Load(ctx context.Context, key string) ([]CachedLabelVector, error) {
	if p == nil || p.Pool == nil {
		return nil, errors.New("标签向量缓存未注入连接池")
	}
	rows, err := p.Pool.Query(ctx, `
		SELECT label, class, grp, embedding::text
		FROM tag_label_vectors
		WHERE cache_key = $1`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CachedLabelVector, 0, 128)
	for rows.Next() {
		var it CachedLabelVector
		var vecText string
		if err := rows.Scan(&it.Label, &it.Class, &it.Group, &vecText); err != nil {
			return nil, err
		}
		v, err := ParseVectorLiteral(vecText)
		if err != nil {
			return nil, fmt.Errorf("解析标签 %q 的缓存向量失败: %w", it.Label, err)
		}
		it.Vec = v
		out = append(out, it)
	}
	return out, rows.Err()
}

// Save 覆盖写入该 cache_key 下的标签向量集合。
//
// 步骤与顺序（顺序有原因，勿随意调整）：
//  1. 多行 upsert：同名标签直接覆盖（置信来源永远是「刚算出来的」）；
//  2. 删除该 cache_key 下**已不在当前标签集**里的陈行。
//
// 为什么必须先写后删：若只写不删，陈旧的额外行会让 matchCachedLabels 的
// 「条数完全一致」校验永远失败 → 每次启动都重算 → 缓存等于失效。
// 而先写后删的失败窗口只可能留下「超集」（仍会重算），不会留下缺行（那才会读到残缺向量）。
func (p *PGLabelVectorCache) Save(ctx context.Context, key string, items []CachedLabelVector) error {
	if p == nil || p.Pool == nil {
		return errors.New("标签向量缓存未注入连接池")
	}
	if len(items) == 0 {
		return nil
	}
	args := make([]any, 0, len(items)*5)
	holders := make([]string, 0, len(items))
	labels := make([]string, 0, len(items))
	for i, it := range items {
		if len(it.Vec) != embed.EmbeddingDim {
			return fmt.Errorf("标签 %q 的向量维度为 %d，应为 %d，拒绝写入缓存",
				it.Label, len(it.Vec), embed.EmbeddingDim)
		}
		n := i * 5
		holders = append(holders, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d::vector)", n+1, n+2, n+3, n+4, n+5))
		args = append(args, key, it.Label, it.Class, it.Group, vectorLiteral(it.Vec))
		labels = append(labels, it.Label)
	}
	if _, err := p.Pool.Exec(ctx, `
		INSERT INTO tag_label_vectors (cache_key, label, class, grp, embedding)
		VALUES `+strings.Join(holders, ",")+`
		ON CONFLICT (cache_key, label) DO UPDATE
		  SET class = EXCLUDED.class,
		      grp = EXCLUDED.grp,
		      embedding = EXCLUDED.embedding,
		      created_at = now()`, args...); err != nil {
		return err
	}
	if _, err := p.Pool.Exec(ctx,
		`DELETE FROM tag_label_vectors WHERE cache_key = $1 AND label <> ALL($2::text[])`,
		key, labels); err != nil {
		return err
	}
	return nil
}

// vectorLiteral 把向量序列化为 pgvector 文本字面量 "[v1,v2,...]"。
//
// 用 'f' 定点格式（不产生科学计数法，pgvector 的输入解析必定接受）；
// prec=-1 取能精确回读该 float32 的最短十进制表示，保证「存进去 = 读出来」。
//
// 为什么不复用 embed.VectorLiteral：后者用 'f' 定点但**只保留 6 位小数**，对图像向量够用，
// 而这里的缓存值必须与 ORT 真值**逐位相等**（否则「命中缓存」与「现算」两条路径的相似度
// 会有 <5e-7 的偏差，缓存路径的正确性就无法用 deepEqual 断言了）。故此处追求无损回读。
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 12)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
