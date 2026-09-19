package cursor

import (
	"testing"
	"time"
)

// TestWireFormatIsFrozen 是"线格式冻结"用例：下面这些期望值**不是本包生成的**，
// 而是用 Python 的 base64.urlsafe_b64encode 对既有三处实现（`audit/query.go`、
// `media/timeline.go`、`search/store.go`）的载荷**独立算出来**的。
//
// 为什么必须这样测：游标是对外契约，收敛到本包的正确性判据是"**字节不变**"，而不是"能往返"。
// 能往返的两份实现照样可以互不兼容（例如一处用 RFC3339、一处用 Nano），
// 而那种不一致只会表现为"升级后翻页丢行或重复行"，在测试里几乎不会自己冒出来。
// 一旦本用例失败，说明线格式被改了 —— 所有在途游标会同时失效，必须先想清楚再动。
func TestWireFormatIsFrozen(t *testing.T) {
	t0 := time.Date(2026, 9, 19, 6, 30, 0, 0, time.UTC)
	t1 := time.Date(2026, 9, 19, 6, 30, 0, 123456789, time.UTC)

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"复合游标(时间|字母id)", Encode(t0, "abc-123"), "MjAyNi0wOS0xOVQwNjozMDowMFp8YWJjLTEyMw=="},
		{"复合游标(时间|数字id)", Encode(t0, "42"), "MjAyNi0wOS0xOVQwNjozMDowMFp8NDI="},
		{"复合游标(纳秒精度)", Encode(t1, "x"), "MjAyNi0wOS0xOVQwNjozMDowMC4xMjM0NTY3ODlafHg="},
		{"v3评分游标(文本命中)", EncodeScored(true, 0.5, t0, "abc"), "djN8MXwwLjV8MjAyNi0wOS0xOVQwNjozMDowMFp8YWJj"},
		{"v3评分游标(仅语义)", EncodeScored(false, 1.25, t0, "z"), "djN8MHwxLjI1fDIwMjYtMDktMTlUMDY6MzA6MDBafHo="},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：编码结果与既有实现不一致\n  实际 %q\n  期望 %q", c.name, c.got, c.want)
		}
	}

	// 反向钉住 UTC 归一：上面几条都是 UTC 输入，若有人改成"保留原时区"它们仍会通过。
	// 这条用非 UTC 输入来暴露 —— 同一时刻必须编出**完全相同**的字节。
	loc := time.FixedZone("CST", 8*3600)
	if a, b := Encode(t0.In(loc), "abc-123"), Encode(t0, "abc-123"); a != b {
		t.Errorf("非 UTC 时刻应先归一到 UTC 再编码：%q != %q", a, b)
	}
}

func TestRoundTrip(t *testing.T) {
	t0 := time.Date(2026, 9, 19, 6, 30, 0, 0, time.UTC)

	gotT, gotID, err := Decode(Encode(t0, "abc-123"))
	if err != nil {
		t.Fatalf("Decode 失败：%v", err)
	}
	if !gotT.Equal(t0) || gotID != "abc-123" {
		t.Errorf("往返不一致：t=%v id=%q", gotT, gotID)
	}

	tm, sc, t2, id, err := DecodeScored(EncodeScored(true, 0.5, t0, "abc"))
	if err != nil {
		t.Fatalf("DecodeScored 失败：%v", err)
	}
	if !tm || sc != 0.5 || !t2.Equal(t0) || id != "abc" {
		t.Errorf("评分游标往返不一致：tm=%v score=%v t=%v id=%q", tm, sc, t2, id)
	}
}

// TestDecodeScoredAcceptsV2 钉住升级语义："**宁可重复、不可漏行**"。
// v2 三元组没有分组键，必须按 text_matched=true 解出来，而不是报错。
func TestDecodeScoredAcceptsV2(t *testing.T) {
	// base64url("v2|0.75|2026-09-19T06:30:00Z|abc") —— 由 Python 独立算出
	const v2 = "djJ8MC43NXwyMDI2LTA5LTE5VDA2OjMwOjAwWnxhYmM="
	tm, sc, t0, id, err := DecodeScored(v2)
	if err != nil {
		t.Fatalf("v2 游标应可解析（兼容旧游标），实际报错：%v", err)
	}
	if !tm {
		t.Errorf("v2 游标（无分组键）必须按 text_matched=true 处理，实际 %v", tm)
	}
	if sc != 0.75 || id != "abc" || !t0.Equal(time.Date(2026, 9, 19, 6, 30, 0, 0, time.UTC)) {
		t.Errorf("v2 解析结果不符：score=%v t=%v id=%q", sc, t0, id)
	}
}

// TestMalformedInputs 畸形输入必须**返回错误**，既不 panic 也不静默给零值 ——
// 游标直接来自查询串，是攻击面之一。
//
// 每个样例都注明它想覆盖的分支，避免"因为 base64 解不开而报错"被误当成"格式校验生效"。
func TestMalformedInputs(t *testing.T) {
	bad := []struct{ name, s string }{
		{"空串", ""},                               // strings.Cut 无分隔符
		{"非法 base64", "!!!not-base64!!!"},        // base64 解码失败
		{"无分隔符", "YWJj"},                         // base64("abc")
		{"缺 id", "MjAyNi0wOS0xOVQwNjozMDowMFp8"}, // base64("2026-09-19T06:30:00Z|")
		{"时间非法", "bm90LWEtdGltZXxhYmM="},         // base64("not-a-time|abc")
	}
	for _, c := range bad {
		if t0, id, err := Decode(c.s); err == nil {
			t.Errorf("[%s] 畸形游标 %q 应报错，实际返回 t=%v id=%q", c.name, c.s, t0, id)
		}
	}

	// 评分游标另有两个只属于它的失败分支：score 非法、前缀不符。
	badScored := []struct{ name, s string }{
		{"v3 前缀但 score 非数字", "djN8MXxhYmN8MjAyNi0wOS0xOVQwNjozMDowMFp8eA=="}, // base64("v3|1|abc|2026-09-19T06:30:00Z|x")
		{"前缀不符", "YWJj"}, // base64("abc")
	}
	for _, c := range badScored {
		if _, _, _, _, err := DecodeScored(c.s); err == nil {
			t.Errorf("[%s] 畸形评分游标 %q 应报错", c.name, c.s)
		}
	}
}
