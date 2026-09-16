package media

// =============================================================================
// 本文件钉住一条**已知且刻意接受**的行为。它不是缺陷。
//
// 背景：2026-09-16 有人把它当 bug 上报 —— 极光-延时-001.mp4 与 西湖-游船-延时-001.mp4
// 在 GET /media/duplicates 的结果里被并在同一组，且 d=0（指纹完全相同）。
//
// 查证结论（**不是**管线写错帧）：
//   · 三层缩略图（sm / md / lg）逐层 sha256 相同；
//   · 但源文件明显不同：2170533 B vs 1080574 B，时长 12s vs 6s；
//   · 两条源视频**抽取出的首帧字节相同**。
// 即两个视频的**开头本来就是同一画面**（语料属性），而缩略图取首帧 ⇒ 单帧 pHash 必然相同。
// 于是这是"单帧 pHash 对视频是**弱标识**"的固有局限，不是阈值/门控能修掉的东西。
//
// 曾尝试的修法：给重复检测加一道 `detail`（AC 能量）门，即迁移 00025 的
// `Hamming ≤ T AND (双方 detail ≥ D_min OR sha256 相同)`。**实测不可行**：
// 这一对恰恰是全库细节最丰富的一档（三种候选度量 mean|AC| / RMS(AC) / meanAbsAC/|DC|
// 全部如此），任何能把它分开的 D_min 都会连带排除 62/93 = 67% 的媒体。
// 迁移**未创建**，门控**未实施**。
//
// 因此 team-lead 做出产品决策（2026-09-16）：**保持现状的合并行为，不收紧视频的身份标识**。
//  1. 收紧（排除视频，或要求 sha256 相同）会一并砍掉"同一场景的 360 照片 ↔ 360 视频"
//     这一对（实测 d=2，是**真重复**）—— 等于拿一个真阳性去换掉两个（其中一个还只是语料复用）。
//  2. 本端点**只读且只作建议**：自己不删任何东西，实际清理必经用户确认并走软删（回收站可恢复）。
//     over-inclusive 的代价是"用户多看一眼"，不是丢数据。
//
// ⚠️ 下面三条用例断言的是**意图**，不是现象。如果你正在读这段是因为它们红了，
// 那么你大概率正准备"修掉"上面这个行为。请先读完上文的取舍：把这一对拆开需要推翻
// 一条产品决策，而不是换个阈值或加个 type 判据。删改本文件前请先回答：
// "同场景 360 照片 + 360 视频那一对（TestCrossMediumSameScenePairMustKeepMerging）怎么办？"
// 改完还要同步 文档/API详细契约_v1.1.md 的「已知限制三」。
// =============================================================================

import (
	"testing"
	"time"

	"panoalbum/internal/phash"
)

// 夹具指纹。都满足真实 pHash 的结构恒等式（popcount == 32、bit0 恒为 1，见 internal/phash），
// 免得将来有人拿"这不像一个真指纹"来质疑夹具。
const (
	// 极光-延时-001.mp4 与 西湖-游船-延时-001.mp4 共用：两条视频首帧相同。
	phAuroraTimelapse uint64 = 0x2c6bdc211befc305
	// 普通照片（及其重编码副本共用）。
	phBeachPhoto uint64 = 0x36be4a0fc7484cab
	// 同一场景的 360 视频与其 360 照片：实测 d=2。
	phScogaVideo uint64 = 0xd41aa8d047c367cf
	phScogaPhoto uint64 = 0xd41aa8d047c367dd
	// 无关照片，用来说明"不是把什么都并在一起"。
	phFarPhoto uint64 = 0xe2335d91f1076659
)

// vid 构造视频候选。duplicates_test.go 里的 cand() 把 type 固定成 photo，
// 而本文件的用例恰恰要区分照片/视频两种形态（要证明分组**不看** type）。
func vid(id string, h uint64, w, hgt int, secs float64, size int64) dupCandidate {
	return dupCandidate{
		Ref: MediaRef{
			ID: id, Type: "video",
			Width: &w, Height: &hgt, Duration: &secs,
		},
		PHash:    h,
		Filesize: size,
	}
}

// photo 构造照片候选（与 cand() 等价，只是名字在本文件里更对称）。is360 单独给。
func photo(id string, h uint64, w, hgt int, size int64, is360 bool) dupCandidate {
	return dupCandidate{
		Ref:      MediaRef{ID: id, Type: "photo", Width: &w, Height: &hgt, Is360: is360},
		PHash:    h,
		Filesize: size,
	}
}

// TestVideoPairSharingFirstFrameMergesByDesign 钉住：两条首帧相同的视频**会**被并在同一组。
//
// 这是刻意的、已接受的行为。红了先读文件头的取舍说明，别直接改判定规则。
func TestVideoPairSharingFirstFrameMergesByDesign(t *testing.T) {
	// 真实语料：文件大小 2170533 / 1080574 B、时长 12s / 6s 都不同，但首帧字节相同。
	aurora := vid("m-aurora-timelapse", phAuroraTimelapse, 1920, 1080, 12, 2170533)
	xihu := vid("m-xihu-boat-timelapse", phAuroraTimelapse, 1920, 1080, 6, 1080574)

	// 前提自证：整条用例建立在"两条视频指纹完全相同"之上。若夹具被改动导致不再相同，
	// 下面的断言会退化成空转（永远看不到分组），所以必须在这里先失败。
	if d := phash.Hamming(aurora.PHash, xihu.PHash); d != 0 {
		t.Fatalf("夹具失效：首帧相同的两条视频 ⇒ 指纹应完全一致（d=0），实际 d=%d。"+
			"本用例的全部结论以 d=0 为前提，前提破了就必须在这里失败，而不是让断言悄悄变成空转。", d)
	}

	// 负对照：无关照片不得被顺带并进来（防止有人"修"成放宽阈值/去掉判据）。
	far := photo("m-faraway", phFarPhoto, 1920, 1080, 500000, false)
	if d := phash.Hamming(aurora.PHash, far.PHash); d <= DuplicateThresholdDefault {
		t.Fatalf("夹具失效：负对照与视频对的距离应远大于阈值 %d，实际 d=%d", DuplicateThresholdDefault, d)
	}

	groups := groupDuplicates([]dupCandidate{aurora, xihu, far}, DuplicateThresholdDefault)
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf(`行为被改动了 —— 但请先读完再动手：这**不是** bug，是刻意的产品取舍。

  现象：两条视频（%s / %s）首帧相同 ⇒ 单帧 pHash 完全相同 ⇒ 被并在同一组。
  事实：不是管线写错帧。三层缩略图逐层 sha256 相同，而源文件差异明显
        （%d B vs %d B，%.0fs vs %.0fs）；两条源视频抽取出的首帧**字节相同**。
  结论：单帧 pHash 对视频是**弱标识** —— 两个不同视频只要开头画面相同就必然合并，
        且**无法**用指纹把它们区分开（任何纯指纹规则都会一起放过或一起拦下）。

  为什么不收紧（team-lead 产品决策 2026-09-16）：
    · 收紧（排除视频 / 要求 sha256 相同）会一并砍掉"同一场景的 360 照片 ↔ 360 视频"
      那一对（实测 d=2，是**真重复**）—— 等于拿一个真阳性去换掉两个；
    · 本端点**只读且只作建议**，自己不删任何东西，实际清理必经用户确认并走软删（回收站可恢复）。
      over-inclusive 的代价是"用户多看一眼"，不是丢数据。

  要改这个行为，你推翻的是一条产品决策而不是修一个 bug。动手前请先回答：
    "同场景 360 照片 + 360 视频那一对（TestCrossMediumSameScenePairMustKeepMerging）怎么办？"
  改完请同步 文档/API详细契约_v1.1.md 的「已知限制三」。`,
			aurora.Ref.ID, xihu.Ref.ID,
			aurora.Filesize, xihu.Filesize,
			*aurora.Ref.Duration, *xihu.Ref.Duration)
	}

	// 关键性质：连**最严的合法阈值**（T=0，即 DuplicateThresholdMin）都合得上，
	// 而不只是默认 T=10。这说明"调 threshold 参数"根本拆不开这一对 ——
	// 能拆开它的只有判定规则本身（排除视频 / 要求元数据相同），那正是产品决策的范畴。
	if g := groupDuplicates([]dupCandidate{aurora, xihu}, DuplicateThresholdMin); len(g) != 1 || len(g[0].Items) != 2 {
		t.Fatalf("夹具/行为与预期不符：d=0 的一对在**最严阈值** T=%d 下应仍合并（因为 0 ≤ %d），实际 %v。"+
			"若这里失败，说明 d 已不为 0，请先检查上面的前提自证。",
			DuplicateThresholdMin, DuplicateThresholdMin, groupKey(g))
	}
}

// TestCrossMediumSameScenePairMustKeepMerging 是上一条的**对偶**：不收紧的理由就在这一对。
//
// 同一场景的 360 照片 ↔ 360 视频跨越媒介、字节不同（实测 d=2），但是**真重复**。
// 任何"排除视频""要求 sha256 相同""要求 filesize/duration 相等"的收紧都会把它砍掉 ——
// 所以本用例和第二、三条用例一起，把"保持 over-inclusive"钉成有据可依的取舍而非疏漏。
func TestCrossMediumSameScenePairMustKeepMerging(t *testing.T) {
	p := photo("m-scoga-360-photo", phScogaPhoto, 5760, 2880, 4200000, true)
	v := vid("m-scoga-360-video", phScogaVideo, 3840, 1920, 18, 3100000)
	v.Ref.Is360 = true

	d := phash.Hamming(p.PHash, v.PHash)
	// 前提自证：跨媒介 ⇒ 不可能是同一份字节，故 d 必须 > 0；又必须落在默认阈值内。
	if d == 0 {
		t.Fatalf("夹具失效：跨媒介（照片 vs 视频）的同一场景不应 d=0，实际 d=0 —— 那它就退化成了另一条用例。")
	}
	if d > DuplicateThresholdDefault {
		t.Fatalf("夹具失效：实测 d=2 应落在默认阈值 %d 内，实际 d=%d", DuplicateThresholdDefault, d)
	}

	groups := groupDuplicates([]dupCandidate{p, v}, DuplicateThresholdDefault)
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf(`同场景的 360 照片与 360 视频没有被并在同一组 —— 这通常意味着有人收紧了身份标识。

  这一对是**真重复**（同一场景、跨媒介，实测 d=%d），而本端点的价值恰恰在于"接住观感相同"：
  重编码、缩放、跨媒介同场景，都是它应当命中的目标，也正是 T=%d 按真实数据标定的意图
  （同图重编码/缩放实测 d=0，无关图对中位数 32）。

  收紧的代价已经从另一头证明过：任何能拆开"两条首帧相同的视频"的规则，都会先拆掉这一对。
  一个真阳性换两个（其中一个仅因语料首帧复用而误合并）—— 不划算，故保持 over-inclusive。
  见 文档/API详细契约_v1.1.md「已知限制三」与 TestVideoPairSharingFirstFrameMergesByDesign。`,
			d, DuplicateThresholdDefault)
	}

	// keeper 判据**不看 type**（见契约文档同小节）：像素多的照片胜出，尽管它是"照片"。
	// 这是刻意不引入"视频优先/照片优先"这类没人能解释的隐含规则。
	if groups[0].KeepID != p.Ref.ID {
		t.Fatalf("keeper 应为像素数更高的照片 %s（判据不看 type），实际 %s", p.Ref.ID, groups[0].KeepID)
	}
}

// TestDuplicateGroupingIgnoresMetadata 钉住第二条被否掉的收紧思路：
// "要求 filesize / duration 相等再算重复"。这会从根上废掉本端点的主要用途。
//
// 依据是阈值标定的实测（见 duplicates.go 的 DuplicateThresholdDefault 注释）：
// 同一张图 PNG / JPEG q90 / q60 / q30 与 75% / 60% / 40% 缩放，汉明距离**全为 0**，
// 而这些副本的文件大小本来就不同 —— 若把"大小相等"作为前置，重编码/缩放副本将全部漏检。
func TestDuplicateGroupingIgnoresMetadata(t *testing.T) {
	orig := photo("m-beach-original", phBeachPhoto, 1600, 1200, 260578, false)
	reenc := photo("m-beach-reencoded", phBeachPhoto, 1600, 1200, 41207, false) // 同一张图重编码后更小
	reenc.Ref.TakenAt = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)             // 导入时间也不同

	// 前提自证：除指纹外，这两个候选的**每一项**元数据都不同（大小、文件名、拍摄时间）。
	if orig.Filesize == reenc.Filesize || orig.Ref.ID == reenc.Ref.ID || orig.Ref.TakenAt.Equal(reenc.Ref.TakenAt) {
		t.Fatalf("夹具失效：本用例要求两个候选在元数据上全部不同（大小/文件名/时间），否则挡不住" +
			"以元数据相等为前置的收紧。")
	}

	groups := groupDuplicates([]dupCandidate{orig, reenc}, DuplicateThresholdDefault)
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf(`重编码副本没有被识别为重复（期望 1 组，实际 %d 组）—— 有人把元数据相等当成了分组前置条件。

  分组判据**只看指纹**（PHash 的汉明距离）；filesize / width / height / taken_at 只用于
  挑 keeper（betterKeeper），**不参与**"是否算重复"的判断。这是刻意的：
  重复检测要接住"观感相同"的副本，而重编码（PNG → JPEG q30）与缩放（75%%/60%%/40%%）
  恰恰是观感相同、字节与大小都不同 —— 实测这些副本的汉明距离全为 0。

  加"大小/时长相等"这类前置会把本端点的主要用途（清理重编码/缩放副本）整个废掉，
  而它想解决的"两条首帧相同的视频"问题也不会因此好转（那两个文件大小本来就不同）。`,
			len(groups))
	}
}
