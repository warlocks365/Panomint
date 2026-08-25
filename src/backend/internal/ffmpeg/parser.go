package ffmpeg

import (
	"strconv"
	"strings"
)

// progressAcc 累积 -progress 输出的键值对；每个块以 progress=continue|end 结束。
type progressAcc struct {
	cur Progress
}

// feed 喂入一行 key=value；块结束时返回 (快照, true)。
func (a *progressAcc) feed(line string) (Progress, bool) {
	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return Progress{}, false
	}
	switch strings.TrimSpace(k) {
	case "frame":
		a.cur.Frame = parseI64(v)
	case "fps":
		a.cur.FPS = parseF64(v)
	case "bitrate":
		a.cur.BitrateKbps = parseBitrate(v)
	case "out_time_us":
		a.cur.OutTimeUs = parseI64(v)
	case "speed":
		a.cur.Speed = parseF64(strings.TrimSuffix(strings.TrimSpace(v), "x"))
	case "progress":
		a.cur.Done = strings.TrimSpace(v) == "end"
		p := a.cur
		a.cur = Progress{}
		return p, true
	}
	return Progress{}, false
}

func parseI64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n // "N/A" 等解析失败时为 0
}

func parseF64(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// parseBitrate 解析 "1234.5kbits/s" → 1234.5；"N/A" → 0。
func parseBitrate(s string) float64 {
	return parseF64(strings.TrimSuffix(strings.TrimSpace(s), "kbits/s"))
}
