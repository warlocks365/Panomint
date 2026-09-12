package tags

// Applier 把「CLIP 零样本建议 + EXIF/GPS 启发式」落到 media_tags。
// 被清扫式 worker（cmd/taggen）与 API 手动触发共用，保证两条路径行为一致。

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
func (a *Applier) ApplyOne(ctx context.Context, m PendingMedia, vec []float32) (int, error) {
	n := 0
	if a.Clf != nil && len(vec) > 0 {
		for _, s := range a.Clf.Suggest(vec) {
			if err := a.Store.ApplySuggestion(ctx, m.ID, s.Tag, s.Confidence, "ai", a.ConfirmAI); err != nil {
				return n, err
			}
			n++
		}
	}
	for _, h := range Heuristics(MediaMeta{ID: m.ID, Place: m.Place, Is360: m.Is360, Type: m.Type, TakenAt: m.TakenAt}) {
		if err := a.Store.ApplySuggestion(ctx, m.ID, h.Tag, h.Confidence, "heuristic", true); err != nil {
			return n, err
		}
		n++
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
