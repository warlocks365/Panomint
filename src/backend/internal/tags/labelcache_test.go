package tags

// 标签向量持久缓存的纯逻辑单测：**不依赖 DB / 不依赖 ORT / 不依赖网络**。
//
// 覆盖（对应缺陷 1 的验收标准）：
//   - 空缓存 → 编码 N×模板数 次并写回缓存
//   - 命中缓存 → 编码 **0** 次
//   - 条数 / 维度 / 标签名 / 分类 / 互斥组 任一不符 → 视为未命中并重算
//   - cache_key 对「族 / 模型目录 / 词表 / 模板集合」四类变化各自敏感，且同输入稳定
//   - Cache == nil → 与改动前一致（照常编码）
//   - 缓存 Load/Save 报错 → 降级编码、不阻断、不 panic
//   - 缓存命中路径与现算路径的标签向量、Suggest 结果完全一致

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/embed"
)

// stubEncoder 计数型假文本塔：任意输入都返回同一个 512 维单位向量（e0）。
// 所有标签向量因此相同，Suggest 的结果由「降序 + 同组互斥 + 上限」决定，便于断言确定性。
type stubEncoder struct {
	fam   embed.ModelFamily
	calls int
}

func (s *stubEncoder) Family() embed.ModelFamily { return s.fam }

func (s *stubEncoder) EncodeText(context.Context, string) ([]float32, error) {
	s.calls++
	return unit(0), nil
}

// memLabelCache 内存版 LabelVectorCache：可注入读写故障、记录调用情况。
type memLabelCache struct {
	store   map[string][]CachedLabelVector
	loadErr error
	saveErr error
	loads   int
	saves   int
	lastKey string
	lastLen int
}

func newMemLabelCache() *memLabelCache {
	return &memLabelCache{store: map[string][]CachedLabelVector{}}
}

func (m *memLabelCache) Load(_ context.Context, key string) ([]CachedLabelVector, error) {
	m.loads++
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return m.store[key], nil // 未命中返回 (nil, nil)
}

func (m *memLabelCache) Save(_ context.Context, key string, items []CachedLabelVector) error {
	m.saves++
	if m.saveErr != nil {
		return m.saveErr
	}
	cp := make([]CachedLabelVector, len(items))
	copy(cp, items)
	m.store[key] = cp
	m.lastKey = key
	m.lastLen = len(items)
	return nil
}

// testCacheVocab 小型词表（3 个标签 / 2 个分类 / 2 个互斥组），断言编码次数时最直观。
func testCacheVocab() *Vocab {
	return &Vocab{Classes: []Class{
		{Name: "场景", Tags: []TagDef{
			{Label: "晴天", EN: "clear sky", Group: "天气"},
			{Label: "雨天", EN: "rain", Group: "天气"},
		}},
		{Name: "物体", Tags: []TagDef{
			{Label: "猫", EN: "cat", Group: "动物"},
		}},
	}}
}

// wantEncodeCalls 现算路径应有的编码次数 = 标签数 × 该族模板数。
func wantEncodeCalls(v *Vocab, fam embed.ModelFamily) int {
	return v.LabelCount() * len(templatesFor(fam))
}

// freshLabels 不经缓存跑一次编码，得到「真值」标签向量（供缓存填充/比对）。
func freshLabels(t *testing.T, v *Vocab) []labelVec {
	t.Helper()
	enc := &stubEncoder{fam: embed.FamilyChineseCLIP}
	clf, err := NewClassifier(context.Background(), enc, v, ClassifyConfig{})
	if err != nil {
		t.Fatalf("现算路径构建分类器失败: %v", err)
	}
	return clf.labels
}

// 1. 空缓存：照常编码全部提示词，并把结果写回缓存。
func TestNewClassifierEmptyCacheEncodesAndSaves(t *testing.T) {
	v := testCacheVocab()
	cache := newMemLabelCache()
	enc := &stubEncoder{fam: embed.FamilyChineseCLIP}

	clf, err := NewClassifier(context.Background(), enc, v, ClassifyConfig{Cache: cache})
	if err != nil {
		t.Fatalf("NewClassifier 失败: %v", err)
	}
	if want := wantEncodeCalls(v, embed.FamilyChineseCLIP); enc.calls != want {
		t.Errorf("空缓存应编码 %d 次，实得 %d", want, enc.calls)
	}
	if clf.LabelCount() != v.LabelCount() {
		t.Errorf("标签数应为 %d，实得 %d", v.LabelCount(), clf.LabelCount())
	}
	if cache.saves != 1 {
		t.Fatalf("应写回缓存 1 次，实得 %d", cache.saves)
	}
	if want := labelCacheKey(embed.FamilyChineseCLIP, "", v); cache.lastKey != want {
		t.Errorf("写入的 cache_key 应为 %q，实得 %q", want, cache.lastKey)
	}
	if cache.lastLen != v.LabelCount() {
		t.Errorf("写入条目数应为 %d，实得 %d", v.LabelCount(), cache.lastLen)
	}
}

// 2. 命中缓存：**编码 0 次**，且标签向量与现算路径完全一致。
func TestNewClassifierCacheHitSkipsEncoding(t *testing.T) {
	v := testCacheVocab()
	cache := newMemLabelCache()
	first := &stubEncoder{fam: embed.FamilyChineseCLIP}
	clf1, err := NewClassifier(context.Background(), first, v, ClassifyConfig{Cache: cache})
	if err != nil {
		t.Fatalf("首次构建失败: %v", err)
	}

	second := &stubEncoder{fam: embed.FamilyChineseCLIP}
	clf2, err := NewClassifier(context.Background(), second, v, ClassifyConfig{Cache: cache})
	if err != nil {
		t.Fatalf("二次构建失败: %v", err)
	}
	if second.calls != 0 {
		t.Fatalf("命中缓存时不得调用 EncodeText，实得 %d 次", second.calls)
	}
	if cache.saves != 1 {
		t.Errorf("命中缓存不应重复写回，Save 次数应仍为 1，实得 %d", cache.saves)
	}
	if !reflect.DeepEqual(clf1.labels, clf2.labels) {
		t.Errorf("缓存命中路径的标签向量与现算路径不一致\n现算: %+v\n缓存: %+v", clf1.labels, clf2.labels)
	}
	if clf2.LabelCount() != v.LabelCount() {
		t.Errorf("标签数应为 %d，实得 %d", v.LabelCount(), clf2.LabelCount())
	}
}

// 3. 缓存内容与当前词表不一致 → 视为未命中并重算重写。
func TestNewClassifierCacheMismatchReencodes(t *testing.T) {
	v := testCacheVocab()
	key := labelCacheKey(embed.FamilyChineseCLIP, "", v)

	cases := []struct {
		name   string
		mutate func([]CachedLabelVector) []CachedLabelVector
	}{
		{"条数不符（少一条）", func(x []CachedLabelVector) []CachedLabelVector { return x[:len(x)-1] }},
		{"条数不符（多一条）", func(x []CachedLabelVector) []CachedLabelVector {
			return append(x, CachedLabelVector{Label: "多余", Vec: unit(0)})
		}},
		{"维度不符", func(x []CachedLabelVector) []CachedLabelVector {
			x[0].Vec = make([]float32, 8)
			return x
		}},
		{"标签名不符", func(x []CachedLabelVector) []CachedLabelVector {
			x[0].Label = "词表里没有的标签"
			return x
		}},
		{"分类不符", func(x []CachedLabelVector) []CachedLabelVector {
			x[0].Class = "事件"
			return x
		}},
		{"互斥组不符", func(x []CachedLabelVector) []CachedLabelVector {
			x[0].Group = "别的组"
			return x
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := newMemLabelCache()
			cache.store[key] = tc.mutate(toCachedLabels(freshLabels(t, v)))
			enc := &stubEncoder{fam: embed.FamilyChineseCLIP}

			clf, err := NewClassifier(context.Background(), enc, v, ClassifyConfig{Cache: cache})
			if err != nil {
				t.Fatalf("NewClassifier 失败: %v", err)
			}
			if enc.calls == 0 {
				t.Errorf("缓存与词表不一致时必须重算，实得编码 0 次")
			}
			if cache.saves != 1 {
				t.Errorf("重算后应写回缓存 1 次，实得 %d", cache.saves)
			}
			if !reflect.DeepEqual(clf.labels, freshLabels(t, v)) {
				t.Errorf("重算结果应等于真值")
			}
		})
	}
}

// 4. cache_key 对各类影响因素敏感，且同输入稳定、格式固定。
func TestLabelCacheKeySensitivityAndStability(t *testing.T) {
	dir := "/models/chinese-clip"
	base := labelCacheKey(embed.FamilyChineseCLIP, dir, testCacheVocab())

	if len(base) != cacheKeyLen || strings.Trim(base, "0123456789abcdef") != "" {
		t.Fatalf("cache_key 应为 %d 位小写十六进制，实得 %q（len=%d）", cacheKeyLen, base, len(base))
	}
	if got := labelCacheKey(embed.FamilyChineseCLIP, dir, testCacheVocab()); got != base {
		t.Errorf("同输入必须得到同一 cache_key：%q != %q", got, base)
	}
	// 模型目录两侧空白规范化后应视为同一套（避免同一目录写法不同导致无谓重算）
	if got := labelCacheKey(embed.FamilyChineseCLIP, "  "+dir+"  ", testCacheVocab()); got != base {
		t.Errorf("模型目录首尾空白应被规范化：%q != %q", got, base)
	}

	// (a) 模型族
	if got := labelCacheKey(embed.FamilyCLIP, dir, testCacheVocab()); got == base {
		t.Errorf("模型族变化必须改变 cache_key（换塔后向量空间不同）")
	}
	// (b) 模型目录
	if got := labelCacheKey(embed.FamilyChineseCLIP, "/models/other-clip", testCacheVocab()); got == base {
		t.Errorf("模型目录变化必须改变 cache_key（换权重文件即换向量）")
	}
	// (c) 词表 —— 逐一验证 Label / EN / Group / Class 名 / 增删条目
	vocabCases := []struct {
		name   string
		mutate func(*Vocab)
	}{
		{"改 EN", func(v *Vocab) { v.Classes[0].Tags[0].EN = "sunny" }},
		{"改 Label", func(v *Vocab) { v.Classes[0].Tags[0].Label = "晴朗" }},
		{"改 Group", func(v *Vocab) { v.Classes[0].Tags[0].Group = "别的组" }},
		{"改 Class 名", func(v *Vocab) { v.Classes[1].Name = "事件" }},
		{"删一个标签", func(v *Vocab) { v.Classes[0].Tags = v.Classes[0].Tags[:1] }},
		{"增一个标签", func(v *Vocab) {
			v.Classes[1].Tags = append(v.Classes[1].Tags, TagDef{Label: "狗", EN: "dog", Group: "动物"})
		}},
	}
	for _, tc := range vocabCases {
		v := testCacheVocab()
		tc.mutate(v)
		if got := labelCacheKey(embed.FamilyChineseCLIP, dir, v); got == base {
			t.Errorf("词表变化（%s）必须改变 cache_key", tc.name)
		}
	}
	// (d) 提示词模板集合
	zhTmpls := templatesFor(embed.FamilyChineseCLIP)
	modified := append([]string{"另一条 %s 的提示词"}, zhTmpls...)
	if labelCacheKeyFor(embed.FamilyChineseCLIP, dir, testCacheVocab(), modified) ==
		labelCacheKeyFor(embed.FamilyChineseCLIP, dir, testCacheVocab(), zhTmpls) {
		t.Errorf("模板集合变化必须改变 cache_key")
	}
	// 中/英两族用的是不同模板集合，其 key 天然不同（与族变化一致，双重保险）
	if labelCacheKeyFor(embed.FamilyCLIP, dir, testCacheVocab(), clipCLIPTemplates) ==
		labelCacheKeyFor(embed.FamilyChineseCLIP, dir, testCacheVocab(), zhTmpls) {
		t.Errorf("不同族的模板集合必须得到不同 cache_key")
	}
}

// 5. Cache == nil：行为与改动前一致（照常编码）。
func TestNewClassifierNilCacheEncodes(t *testing.T) {
	v := testCacheVocab()
	enc := &stubEncoder{fam: embed.FamilyChineseCLIP}
	clf, err := NewClassifier(context.Background(), enc, v, ClassifyConfig{})
	if err != nil {
		t.Fatalf("NewClassifier 失败: %v", err)
	}
	if want := wantEncodeCalls(v, embed.FamilyChineseCLIP); enc.calls != want {
		t.Errorf("无缓存时应编码 %d 次，实得 %d", want, enc.calls)
	}
	if clf.LabelCount() != v.LabelCount() {
		t.Errorf("标签数应为 %d，实得 %d", v.LabelCount(), clf.LabelCount())
	}
}

// 6. 缓存读取失败（DB 挂 / 表不存在）→ 降级编码，启动照常成功。
func TestNewClassifierCacheLoadErrorDegrades(t *testing.T) {
	v := testCacheVocab()
	cache := newMemLabelCache()
	cache.loadErr = errors.New("模拟 DB 故障：relation \"tag_label_vectors\" does not exist")
	enc := &stubEncoder{fam: embed.FamilyChineseCLIP}

	clf, err := NewClassifier(context.Background(), enc, v, ClassifyConfig{Cache: cache})
	if err != nil {
		t.Fatalf("缓存读取失败绝不得阻断启动，实得 error: %v", err)
	}
	if enc.calls == 0 {
		t.Errorf("读缓存失败时应降级为编码，实得编码 0 次")
	}
	if clf.LabelCount() != v.LabelCount() {
		t.Errorf("降级后标签数应为 %d，实得 %d", v.LabelCount(), clf.LabelCount())
	}
	if cache.saves != 1 {
		t.Errorf("读失败但编码成功，仍应尝试写回缓存，实得 Save %d 次", cache.saves)
	}
}

// 7. 缓存写失败 → 只记警告，不阻断、不返回 error。
func TestNewClassifierCacheSaveErrorDoesNotFail(t *testing.T) {
	v := testCacheVocab()
	cache := newMemLabelCache()
	cache.saveErr = errors.New("模拟写失败：权限不足")
	enc := &stubEncoder{fam: embed.FamilyChineseCLIP}

	clf, err := NewClassifier(context.Background(), enc, v, ClassifyConfig{Cache: cache})
	if err != nil {
		t.Fatalf("缓存写入失败绝不得阻断启动，实得 error: %v", err)
	}
	if cache.saves != 1 {
		t.Errorf("应尝试写缓存 1 次，实得 %d", cache.saves)
	}
	if clf.LabelCount() != v.LabelCount() {
		t.Errorf("标签数应为 %d，实得 %d", v.LabelCount(), clf.LabelCount())
	}
}

// 8. nil 接收者 / nil 连接池：返回 error，绝不 panic。
func TestPGLabelVectorCacheNilSafety(t *testing.T) {
	ctx := context.Background()
	item := CachedLabelVector{Label: "x", Vec: unit(0)}

	var nilRecv *PGLabelVectorCache
	if _, err := nilRecv.Load(ctx, "k"); err == nil {
		t.Errorf("nil 接收者 Load 应返回 error")
	}
	if err := nilRecv.Save(ctx, "k", []CachedLabelVector{item}); err == nil {
		t.Errorf("nil 接收者 Save 应返回 error")
	}

	empty := &PGLabelVectorCache{}
	if _, err := empty.Load(ctx, "k"); err == nil {
		t.Errorf("nil Pool 的 Load 应返回 error")
	}
	if err := empty.Save(ctx, "k", []CachedLabelVector{item}); err == nil {
		t.Errorf("nil Pool 的 Save 应返回 error")
	}
}

// 8b. Save 的入参守卫：空集合是 no-op（不触碰 DB），维度不符必须被拒绝。
// 两者都发生在任何 DB 调用之前，故用「惰性连接池」（指向不存在的服务）即可验证。
func TestPGLabelVectorCacheSaveGuards(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Skipf("无法构造连接池，跳过: %v", err)
	}
	defer pool.Close()
	pc := &PGLabelVectorCache{Pool: pool}

	if err := pc.Save(context.Background(), "k", nil); err != nil {
		t.Errorf("空集合应为 no-op（不触碰 DB），实得 %v", err)
	}
	err = pc.Save(context.Background(), "k", []CachedLabelVector{{Label: "x", Vec: make([]float32, 8)}})
	if err == nil || !strings.Contains(err.Error(), "维度") {
		t.Errorf("维度不符应被拒绝写入，实得 err=%v", err)
	}
}

// 8c. 向量字面量序列化必须能被 ParseVectorLiteral 精确回读，且不出现科学计数法。
func TestVectorLiteralRoundTrip(t *testing.T) {
	in := []float32{0, 1, -0.5, 1e-8, 3.4e38, 1.2345678e-30, -0.99999994}
	s := vectorLiteral(in)
	if strings.ContainsAny(s, "eE") {
		t.Errorf("字面量不得出现科学计数法（pgvector 输入最稳妥的形式是定点）：%s", s)
	}
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		t.Errorf("字面量应被方括号包裹：%s", s)
	}
	got, err := ParseVectorLiteral(s)
	if err != nil {
		t.Fatalf("回读失败: %v（字面量 %s）", err, s)
	}
	if len(got) != len(in) {
		t.Fatalf("维度应为 %d，实得 %d", len(in), len(got))
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("第 %d 维未精确回读：写入 %v，读出 %v", i, in[i], got[i])
		}
	}
}

// 9. 缓存命中路径与现算路径「等价」：标签向量与 Suggest 结果都必须逐字节一致。
func TestCacheHitEquivalentToFreshEncoding(t *testing.T) {
	v := testCacheVocab()
	ctx := context.Background()
	cache := newMemLabelCache()

	fresh := &stubEncoder{fam: embed.FamilyChineseCLIP}
	clfFresh, err := NewClassifier(ctx, fresh, v, ClassifyConfig{})
	if err != nil {
		t.Fatalf("现算构建失败: %v", err)
	}
	if err := cache.Save(ctx, labelCacheKey(embed.FamilyChineseCLIP, "", v), toCachedLabels(clfFresh.labels)); err != nil {
		t.Fatalf("填充缓存失败: %v", err)
	}

	enc := &stubEncoder{fam: embed.FamilyChineseCLIP}
	clfCached, err := NewClassifier(ctx, enc, v, ClassifyConfig{Cache: cache})
	if err != nil {
		t.Fatalf("缓存命中构建失败: %v", err)
	}
	if enc.calls != 0 {
		t.Fatalf("命中缓存时不得编码，实得 %d 次", enc.calls)
	}
	if !reflect.DeepEqual(clfFresh.labels, clfCached.labels) {
		t.Fatalf("标签向量不一致\n现算: %+v\n缓存: %+v", clfFresh.labels, clfCached.labels)
	}

	probe := unit(0)
	gotFresh := clfFresh.Suggest(probe)
	gotCached := clfCached.Suggest(probe)
	if !reflect.DeepEqual(gotFresh, gotCached) {
		t.Fatalf("Suggest 结果不一致\n现算: %+v\n缓存: %+v", gotFresh, gotCached)
	}
	if len(gotFresh) == 0 {
		t.Fatalf("测试前提不成立：探测向量应至少命中 1 个标签")
	}
	// 顺带固化「同组互斥 + 上限」在该词表上的期望：晴天/雨天同属「天气」只留晴天，另有「猫」
	if len(gotFresh) != 2 || gotFresh[0].Tag != "晴天" || gotFresh[1].Tag != "猫" {
		t.Errorf("期望命中 [晴天, 猫]，实得 %+v", gotFresh)
	}
}

// 10. 在**真实词表规模**上验证缺陷 1 的验收标准：首次编码 570 次，二次启动 0 次。
// 这正是回归现场：114 标签 × 5 模板 = 570 次 CPU 编码 ≈ 110 秒启动时长。
func TestDefaultVocabCacheHitZeroEncodes(t *testing.T) {
	v := DefaultVocab()
	want := v.LabelCount() * len(chineseCLIPTemplates)
	t.Logf("真实词表规模：%d 标签 × %d 模板 = %d 次文本编码", v.LabelCount(), len(chineseCLIPTemplates), want)
	if want < 500 {
		t.Fatalf("回归现场应约 570 次编码，实得 %d —— 词表或模板数被改动，请复核本测试的前提", want)
	}

	cache := newMemLabelCache()
	first := &stubEncoder{fam: embed.FamilyChineseCLIP}
	if _, err := NewClassifier(context.Background(), first, v, ClassifyConfig{Cache: cache}); err != nil {
		t.Fatalf("首次构建失败: %v", err)
	}
	if first.calls != want {
		t.Fatalf("首次（空缓存）应编码 %d 次，实得 %d", want, first.calls)
	}

	second := &stubEncoder{fam: embed.FamilyChineseCLIP}
	clf, err := NewClassifier(context.Background(), second, v, ClassifyConfig{Cache: cache})
	if err != nil {
		t.Fatalf("二次构建失败: %v", err)
	}
	if second.calls != 0 {
		t.Fatalf("二次启动（缓存命中）必须 0 次编码，实得 %d 次", second.calls)
	}
	if clf.LabelCount() != v.LabelCount() {
		t.Errorf("标签数应为 %d，实得 %d", v.LabelCount(), clf.LabelCount())
	}
}
