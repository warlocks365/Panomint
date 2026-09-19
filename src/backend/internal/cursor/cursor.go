// Package cursor 是分页游标编解码的单一真源。
//
// 为什么单独成包：本仓有三处**逐字节相同**的实现（`internal/audit` 的审计分页、
// `internal/media` 的时间轴、`internal/search` 的搜索结果），audit 的注释里甚至写着
// "与 internal/media 的时间轴游标同构" —— 也就是说作者知道它们重复，只是没有一个真源可引用。
// 三份同形实现的问题不在"多写了几行"，而在**改一处忘一处**：游标是**对外契约**，
// 一旦某处单独调了时间精度或分隔符，只有那一个端点的分页会悄悄错位（跨页丢行/重复行），
// 而这类错误在测试里极难暴露。故收敛到本包，并由 source_guard_test.go 防止再长出第二份。
//
// ⚠️ **线格式是对外契约，不要随手改**：
//
//	复合游标（v1，三处共用）：  base64url( RFC3339Nano(t.UTC()) + "|" + id )
//	评分游标（v3，仅 search）：  base64url( "v3|" + textMatched(0|1) + "|" + %g(score)
//	                                      + "|" + RFC3339Nano(t.UTC()) + "|" + id )
//
// 用 `base64.URLEncoding`（**带 `=` 填充**）而非 RawURLEncoding —— 这是既有三处的既定行为，
// 换成不带填充会改变每一个在途游标的字节。改动此处前请先读 cursor_test.go 的"线格式冻结"用例。
//
// 版本前缀（v1/v2/v3）的由来：游标携带的是**排序键**，排序键一旦加列（v2 加 score、
// v3 再加分组键 text_matched），旧游标就无法与新的 ORDER BY 对齐，跨页比较会出现
// 重复行或漏行。前缀让新旧格式可区分，从而在升级瞬间也能安全续页（见 DecodeScored 的 v2 回退）。
package cursor

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Encode 编码复合游标（时间 + id），对应 ORDER BY (<时间列> DESC, id DESC)。
//
// 时间统一转 UTC 后按 RFC3339Nano 格式化：不加这一步，同一时刻在不同时区会编出不同字节，
// 跨页比较就会错位（服务器时区与被编码时的时区可能不同）。
func Encode(t time.Time, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

// Decode 解析复合游标。id 原样返回（不做 UUID 校验 —— 校验属于调用方：
// 不同表的 id 类型不同，这里只保证"分隔与时间格式"这一层）。
//
// ⚠️ **空 id 一律判为格式错误**：`strings.Cut` 对 `"时间|"` 会成功返回 id=""，
// 若放行，调用方就会拼出 `WHERE (t, id) < ($1, ”)` 这类**恒不成立或恒成立**的比较，
// 表现为翻页静默错位（丢行/回环），而且不报任何错。三处调用方的 id 都来自
// UUID 或 int64（永不为空），故拒绝空 id 不会影响任何在途游标。
func Decode(s string) (time.Time, string, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", err
	}
	ts, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return time.Time{}, "", errors.New("游标格式错误")
	}
	if id == "" {
		return time.Time{}, "", errors.New("游标格式错误（id 为空）")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, "", err
	}
	return t, id, nil
}

// EncodeScored 编码 v3 评分游标：(是否文本命中, score, 时间, id)，
// 与 search 的 scoredOrderBy 一一对应。
//
// score 用 %g 格式化：既有实现如此，改 %.17g 之类会改变字节。
func EncodeScored(textMatched bool, score float64, t time.Time, id string) string {
	tm := "0"
	if textMatched {
		tm = "1"
	}
	return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("v3|%s|%g|%s|%s",
		tm, score, t.UTC().Format(time.RFC3339Nano), id)))
}

// DecodeScored 解析评分游标，返回 (是否文本命中, score, 时间, id)。
//
// 兼容 v2 三元组（score, 时间, id）：旧游标没带分组键，按 text_matched=true 处理。
// 仅在升级瞬间的跨版本续页可见，最坏情况是重复若干"仅语义"行 —— **不会漏行、不会报错**，
// 这正是版本前缀想要的性质（宁可重复，不可漏）。
func DecodeScored(s string) (bool, float64, time.Time, string, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return false, 0, time.Time{}, "", err
	}
	parts := strings.SplitN(string(b), "|", 5)
	switch {
	case len(parts) == 5 && parts[0] == "v3":
		sc, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标 score 非法")
		}
		t, err := time.Parse(time.RFC3339Nano, parts[3])
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标时间非法")
		}
		return parts[1] == "1", sc, t, parts[4], nil
	case len(parts) == 4 && parts[0] == "v2":
		sc, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标 score 非法")
		}
		t, err := time.Parse(time.RFC3339Nano, parts[2])
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标时间非法")
		}
		return true, sc, t, parts[3], nil
	}
	return false, 0, time.Time{}, "", errors.New("评分游标格式错误（需 v3 前缀四元组）")
}
