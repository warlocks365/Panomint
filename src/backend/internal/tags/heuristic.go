package tags

// EXIF/GPS 启发式兜底标签：来源为可信元数据（GPS/时间/360 标志），
// 与 CLIP 零样本互补 —— 这些事实 CLIP 未必能识别，但成本几乎为零且准确率高。
//
// 与 AI（CLIP）建议的区别：启发式标签直接 confirmed=true、origin='heuristic'，
// 不进入人工确认队列（GPS 城市名、是否 360 无需用户逐张点头）。

import (
	"strings"
	"time"
)

// MediaMeta 启发式所需的媒体元数据（来自 media 行）。
type MediaMeta struct {
	ID      string
	Place   string    // media.place（已由地理编码填充的城市/地点名）
	Is360   bool      // media.is_360
	Type    string    // media.type：photo|video
	TakenAt time.Time // media.taken_at（零值 = 未知）
}

// HeuristicTag 启发式产出。
type HeuristicTag struct {
	Tag        string
	Group      string
	Confidence float64
	Rule       string // 规则名（日志/调试用）
}

// Heuristics 计算启发式标签（顺序即优先级，同组只保留首个）。
func Heuristics(m MediaMeta) []HeuristicTag {
	var out []HeuristicTag
	if m.Is360 {
		out = append(out, HeuristicTag{Tag: "全景", Confidence: 1.0, Rule: "is_360"})
	}
	if m.Type == "video" {
		out = append(out, HeuristicTag{Tag: "视频", Confidence: 1.0, Rule: "type=video"})
	}
	if city := cityOf(m.Place); city != "" {
		out = append(out, HeuristicTag{Tag: city, Confidence: 0.9, Rule: "gps_city"})
	}
	if s := seasonOf(m.TakenAt); s != "" {
		out = append(out, HeuristicTag{Tag: s, Group: "季节", Confidence: 1.0, Rule: "season"})
	}
	return out
}

// cityOf 从 place 串提取城市名：取首个 '市' 之前（含），或首个逗号/空格前的片段。
//
// 例："北京市朝阳区" → "北京市"；"杭州市" → "杭州市"；"Kyoto, Japan" → "Kyoto"。
func cityOf(place string) string {
	p := strings.TrimSpace(place)
	if p == "" {
		return ""
	}
	if i := strings.Index(p, ","); i > 0 {
		p = p[:i]
	}
	if i := strings.IndexAny(p, " \t"); i > 0 {
		p = p[:i]
	}
	if i := strings.Index(p, "市"); i >= 0 {
		p = p[:i+len("市")]
	}
	p = strings.TrimSpace(p)
	if len([]rune(p)) == 0 || len([]rune(p)) > 12 {
		return ""
	}
	return p
}

// seasonOf 由拍摄月份推季节（北半球约定）。
func seasonOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch t.Month() {
	case time.March, time.April, time.May:
		return "春天"
	case time.June, time.July, time.August:
		return "夏天"
	case time.September, time.October, time.November:
		return "秋天"
	default:
		return "冬天"
	}
}
