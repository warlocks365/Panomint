package geo

// ShortPlaceName 的行为锁定测试。
//
// 这是**纯字符串函数**（用户裁决：不调地图 API，纯本地解析取尾部地标），
// 所以可以穷举 —— 每条用例都是一次真实的判定，不依赖网络。
//
// 判定纪律：本测试的期望值是**产品决策**（取尾部地标，不取头部行政区名，
// 见 2026-10-03 用户裁决），不是「代码现在就这样」。改动期望值前先想清楚
// 是否要改产品语义。

import "testing"

func TestShortPlaceName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		comment string
	}{
		// ---- 核心场景：取尾部具体地标 ----
		{
			name:    "北京景山前街",
			in:      "北京市东城区景山前街 4 号",
			want:    "景山前街",
			comment: "头部行政区剥掉，尾部路名保留，门牌号剥离",
		},
		{
			name:    "杭州龙井路",
			in:      "浙江省杭州市西湖区龙井路 1 号",
			want:    "龙井路",
			comment: "多层前缀（省+市+区）逐层剥掉",
		},
		{
			name:    "深圳科技园南路",
			in:      "深圳市南山区科技园南路 18 号",
			want:    "科技园南路",
			comment: "「南路」是双字后缀，优先于单字「路」匹配",
		},
		{
			name:    "上海南京东路",
			in:      "上海市黄浦区南京东路 300 号",
			want:    "南京东路",
			comment: "「东路」双字后缀优先",
		},
		{
			name:    "广州中山大道",
			in:      "广东省广州市天河区中山大道西 5 号",
			want:    "中山大道",
			comment: "「大道」命中后即切分，尾部「西」不并入",
		},

		// ---- 无路/街结构：原样返回（不退化失败）----
		{
			name:    "景点名原样",
			in:      "故宫博物院",
			want:    "故宫博物院",
			comment: "无路/街后缀，且长度合规 → 原样返回",
		},
		{
			name:    "已是短地名",
			in:      "西湖",
			want:    "西湖",
			comment: "两字短名不触发任何剥离规则",
		},

		// ---- 门牌号剥离 ----
		{
			name:    "无空格门牌",
			in:      "北京市朝阳区建国路88号",
			want:    "建国路",
			comment: "「88号」紧贴路名也要剥",
		},
		{
			name:    "带连字符门牌",
			in:      "上海市浦东新区张杨路 123-5 号",
			want:    "张杨路",
			comment: "「123-5号」整体剥离",
		},

		// ---- 提取失败：返回空串交用户手填（不猜）----
		{
			name:    "空串",
			in:      "",
			want:    "",
			comment: "空输入 → 空输出",
		},
		{
			name:    "纯空白",
			in:      "   ",
			want:    "",
			comment: "全空白 → 空输出",
		},
		{
			name:    "过长无结构地址",
			in:      "北京市东城区某某某某某某某某某某某某某某某某某某某某某某某某某某某某某",
			want:    "",
			comment: ">24 字且无路/街后缀 → 判为提取失败，交用户手填",
		},

		// ---- 特殊：地名本身含「号」但不是门牌 ----
		{
			name:    "含号但非门牌",
			in:      "金三里 3 号院",
			want:    "金三里 3 号院",
			comment: "「号」前是中文（不是数字），不视为门牌，不剥离",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShortPlaceName(tc.in)
			if got != tc.want {
				t.Errorf("ShortPlaceName(%q)\n  得 %q\n  期望 %q\n  （%s）",
					tc.in, got, tc.want, tc.comment)
			}
		})
	}
}

// TestShortPlaceNameIdempotent 短地名不应被二次提取（幂等性）。
//
// 这条很重要：前端拿到 place 后若又把它当 address 传回来提取，不应得到
// 「景山前街」→「空」这种丢失。
func TestShortPlaceNameIdempotent(t *testing.T) {
	places := []string{
		"景山前街",
		"科技园南路",
		"故宫博物院",
		"西湖",
	}
	for _, p := range places {
		once := ShortPlaceName(p)
		twice := ShortPlaceName(once)
		if once != twice {
			t.Errorf("非幂等：ShortPlaceName(%q)=%q，再取一次得 %q", p, once, twice)
		}
	}
}

// TestShortPlaceNameNoCrossContamination 确认「尾部提取」而非「头部提取」。
//
// 这是用户裁决的核心（2026-10-03：取尾部具体地标，不取头部行政区名）。
// 显式断言：**不得**返回任何行政区名。
func TestShortPlaceNameNoCrossContamination(t *testing.T) {
	const (
		addr    = "北京市东城区景山前街 4 号"
		forbid  = "北京市"
		forbid2 = "东城区"
		admin   = "北京市"
	)
	got := ShortPlaceName(addr)
	for _, bad := range []string{forbid, forbid2, admin} {
		if got == bad {
			t.Errorf("返回了行政区名 %q —— 裁决要求取尾部地标（%q）", bad, addr)
		}
	}
	if got != "景山前街" {
		t.Errorf("期望尾部地标「景山前街」，得 %q", got)
	}
}
