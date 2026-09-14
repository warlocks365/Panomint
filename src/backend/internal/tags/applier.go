package tags

// Applier 把「CLIP 零样本建议 + EXIF/GPS 启发式」落到 media_tags。
// 被清扫式 worker（cmd/taggen）与 API 手动触发共用，保证两条路径行为一致。
//
// 优先级：启发式来自可信元数据（EXIF/GPS/媒体类型），判定为**事实**；
// AI 建议是**猜测**。当两者落入同一个互斥组时（Group 非空），事实优先。
// 否则同一张照片会同时挂上互相矛盾的标签 —— 实测：
//
//	2026-06 拍摄的「龙井-茶园-008」同时拿到 夏天（EXIF 月份，conf 1.0）
//	与 秋天（CLIP，conf 0.4304），用户在时间线上会看到自相矛盾的季节。
//
// 注意：只对**非空 Group** 生效。Group 为空表示该标签不参与互斥，任何来源都可并存
// （例如 GPS 城市名与 AI 的「城市」含义不同，不应互相压制）。

import (
	"context"
	"log"
)

// Applier 打标执行器。
type Applier struct {
	Store *Store
	Clf   *Classifier
	// ConfirmAI 为真时 AI 建议直接标记已确认（默认 false：需人工确认）。
	ConfirmAI bool
	// Logger 可空。
	Logger *log.Logger
}

// ApplyOne 处理一条已知向量的媒体，返回写入的标签条数。
//
// 成功走完（AI 建议 + 启发式两个循环都无错返回）后会把 media.tags_scanned_at 置为 now()，
// 标记「该图已完成一轮打标」——**即使 Clf 为 nil、Suggest 返回空、或建议被
// filterAISuggestions 全部丢弃，也要置位**。因为「算过但没有建议」正是让
// ListPendingAI 收敛的合法终态；漏置位会让该图每分钟被重选重打标（缺陷 2）。
// 置位失败返回 error，让调用方按「本轮未完成」处理并在下一轮重试，不静默吞掉。
func (a *Applier) ApplyOne(ctx context.Context, m PendingMedia, vec []float32) (int, error) {
	n := 0
	// 启发式先算出来（纯内存，无 IO），用于压制同互斥组的 AI 建议。
	hs := Heuristics(MediaMeta{ID: m.ID, Place: m.Place, Is360: m.Is360, Type: m.Type, TakenAt: m.TakenAt})
	if a.Clf != nil && len(vec) > 0 {
		for _, s := range filterAISuggestions(a.Clf.Suggest(vec), hs) {
			if err := a.Store.ApplySuggestion(ctx, m.ID, s.Tag, s.Confidence, "ai", a.ConfirmAI); err != nil {
				return n, err
			}
			n++
		}
	}
	for _, h := range hs {
		if err := a.Store.ApplySuggestion(ctx, m.ID, h.Tag, h.Confidence, "heuristic", true); err != nil {
			return n, err
		}
		n++
	}
	if err := a.Store.MarkTagsScanned(ctx, m.ID); err != nil {
		return n, err
	}
	return n, nil
}

// ApplyByID 处理单条媒体（自行加载元数据与向量）。id 不存在或无向量时返回 0, nil。
func (a *Applier) ApplyByID(ctx context.Context, id string) (int, error) {
	list, err := a.Store.ListPendingAI(ctx, 1, id)
	if err != nil {
		return 0, err
	}
	if len(list) == 0 {
		return 0, nil
	}
	vec, err := a.Store.LoadEmbedding(ctx, id)
	if err != nil {
		return 0, err
	}
	return a.ApplyOne(ctx, list[0], vec)
}

// filterAISuggestions 丢弃与启发式已占用的互斥组冲突的 AI 建议。
//
// 目前词表里启发式唯一会产出的互斥组是「季节」（见 heuristic.go 的 seasonOf）；
// 城市/全景/视频都在 Group 为空的标签上，不受影响。
func filterAISuggestions(sugg []Suggestion, hs []HeuristicTag) []Suggestion {
	if len(sugg) == 0 || len(hs) == 0 {
		return sugg
	}
	occupied := map[string]bool{}
	for _, h := range hs {
		if h.Group != "" {
			occupied[h.Group] = true
		}
	}
	if len(occupied) == 0 {
		return sugg
	}
	out := make([]Suggestion, 0, len(sugg))
	for _, s := range sugg {
		if s.Group != "" && occupied[s.Group] {
			continue
		}
		out = append(out, s)
	}
	return out
}
