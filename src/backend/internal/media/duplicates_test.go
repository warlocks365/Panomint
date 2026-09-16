package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// PRD §6.16 工具箱「重复项目」单测：分组（并查集传递闭包）/ keeper 选择 / 阈值钳制 / 指纹十六进制。
// 全部覆盖不触库的纯逻辑——Store 为 nil 时校验必须发生在触库之前。

// dupReq 发一个 GET /media/duplicates 请求（Store 为 nil，只覆盖校验分支）。
func dupReq(h *Handler, query string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "u1"); c.Set("role", "member") })
	r.GET("/media/duplicates", h.Duplicates)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/duplicates"+query, nil))
	return rec
}

// cand 构造候选：id 默认当宽高/大小的载体，显式给全便于逐级验证 keeper 判据。
func cand(id string, h uint64, w, hgt int, size int64, taken time.Time) dupCandidate {
	return dupCandidate{
		Ref:      MediaRef{ID: id, Type: "photo", Width: &w, Height: &hgt, TakenAt: taken},
		PHash:    h,
		Filesize: size,
	}
}

// groupKey 把结果压成可比对的形式（组内成员顺序 + 组顺序），用于"顺序无关"断言。
func groupKey(groups []DuplicateGroup) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		ids := make([]string, 0, len(g.Items))
		for _, it := range g.Items {
			ids = append(ids, it.ID)
		}
		out = append(out, g.KeepID+":"+joinIDs(ids))
	}
	return out
}

func joinIDs(ids []string) string {
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += id
	}
	return s
}

// ---- 分组：并查集传递闭包 ----

func TestGroupDuplicatesTransitiveChain(t *testing.T) {
	// 位模式：a=0b0000、b=0b0011、c=0b1111。阈值 2 时 a≈b(2)、b≈c(2)，但 a 与 c 相距 4 ——
	// 只有做传递闭包才会合为一组，这正是本用例要钉死的性质。
	items := []dupCandidate{cand("a", 0b0000, 10, 10, 1, time.Time{}),
		cand("b", 0b0011, 10, 10, 1, time.Time{}),
		cand("c", 0b1111, 10, 10, 1, time.Time{})}
	groups := groupDuplicates(items, 2)
	if len(groups) != 1 {
		t.Fatalf("链式副本应合成 1 组，实际 %d 组", len(groups))
	}
	if len(groups[0].Items) != 3 {
		t.Fatalf("组内应有 3 个成员，实际 %d", len(groups[0].Items))
	}
	// keeper 是 id 最小的那个（三级判据全打平）
	if groups[0].KeepID != "a" {
		t.Fatalf("keeper 应为 a，实际 %s", groups[0].KeepID)
	}
}

func TestGroupDuplicatesDisjointClustersStaySeparate(t *testing.T) {
	items := []dupCandidate{
		cand("a", 0x0, 10, 10, 1, time.Time{}),
		cand("b", 0x1, 10, 10, 1, time.Time{}),    // d(a,b)=1
		cand("c", 0xFF00, 10, 10, 1, time.Time{}), // 与上一簇相距 ≥8 位
		cand("d", 0xFF01, 10, 10, 1, time.Time{}), // d(c,d)=1
	}
	groups := groupDuplicates(items, 1)
	if len(groups) != 2 {
		t.Fatalf("两个互不相交的簇应得 2 组，实际 %d 组", len(groups))
	}
	for _, g := range groups {
		if len(g.Items) != 2 {
			t.Fatalf("每组应恰好 2 个成员，实际 %v", groupKey(groups))
		}
	}
}

func TestGroupDuplicatesNeverEmitsSingleton(t *testing.T) {
	items := []dupCandidate{
		cand("a", 0x0, 10, 10, 1, time.Time{}),
		cand("b", 0x1, 10, 10, 1, time.Time{}),
		cand("lonely", 0xFFFFFFFFFFFFFFFF, 10, 10, 1, time.Time{}), // 与谁都不像
	}
	groups := groupDuplicates(items, 1)
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("单元素不应成组，实际 %v", groupKey(groups))
	}
	// 空/单元素输入直接给空数组（JSON 里是 []，不是 null）
	if g := groupDuplicates(nil, 10); len(g) != 0 {
		t.Fatalf("空输入应得空结果，实际 %d 组", len(g))
	}
	if g := groupDuplicates(items[:1], 10); len(g) != 0 {
		t.Fatalf("单元素输入应得空结果，实际 %d 组", len(g))
	}
}

func TestGroupDuplicatesOrderIndependence(t *testing.T) {
	base := []dupCandidate{
		cand("m5", 0x0000, 100, 100, 500, time.Time{}),
		cand("m1", 0x0001, 200, 200, 900, time.Time{}),
		cand("m9", 0x0003, 50, 50, 100, time.Time{}),
		cand("x7", 0xFFF0, 10, 10, 10, time.Time{}),
		cand("x2", 0xFFF1, 20, 20, 20, time.Time{}),
	}
	want := joinIDs(groupKey(groupDuplicates(base, 2)))
	if want == "" || !strings.Contains(want, ",") {
		t.Fatalf("断言用的键不含组内多成员，测试无效: %q", want)
	}
	// 打乱若干次，结果必须一模一样（否则 UI 的"删除其余"会飘）
	for _, perm := range [][]int{{4, 3, 2, 1, 0}, {2, 0, 4, 1, 3}, {1, 2, 3, 4, 0}} {
		shuffled := make([]dupCandidate, 0, len(base))
		for _, i := range perm {
			shuffled = append(shuffled, base[i])
		}
		got := joinIDs(groupKey(groupDuplicates(shuffled, 2)))
		if got != want {
			t.Fatalf("输入顺序不应改变分组结果：want %v, got %v", want, got)
		}
	}
}

// ---- keeper 选择：四级判据逐级验证 ----

func TestBetterKeeperPixelCountFirst(t *testing.T) {
	now := time.Now()
	big := cand("big", 0, 4000, 3000, 1, now)        // 1200 万像素，但文件极小、时间最晚
	small := cand("small", 0, 100, 100, 999999, now) // 像素最少
	if !betterKeeper(big, small) {
		t.Fatal("像素数最高者应为 keeper（即使 filesize 最小、taken_at 最晚）")
	}
	if betterKeeper(small, big) {
		t.Fatal("像素数低者不应胜过像素数高者")
	}
}

func TestBetterKeeperOrNilDimensions(t *testing.T) {
	a := dupCandidate{Ref: MediaRef{ID: "a"}} // width/height 为 NULL
	b := cand("b", 0, 10, 10, 1, time.Time{})
	if betterKeeper(a, b) {
		t.Fatal("缺失宽高的行像素数记 0，不应胜过有宽高的行")
	}
	if !betterKeeper(b, a) {
		t.Fatal("有宽高的行应胜过缺失宽高的行")
	}
}

func TestBetterKeeperFilesizeSecond(t *testing.T) {
	same := cand("a", 0, 100, 100, 500, time.Time{})
	bigger := cand("b", 0, 100, 100, 900, time.Time{})
	if !betterKeeper(bigger, same) {
		t.Fatal("同分辨率下 filesize 更大者应为 keeper")
	}
}

func TestBetterKeeperEarlierTakenAtThird(t *testing.T) {
	old := cand("a", 0, 100, 100, 500, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	newer := cand("b", 0, 100, 100, 500, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !betterKeeper(old, newer) {
		t.Fatal("taken_at 更早者应为 keeper")
	}
}

func TestBetterKeeperIDTieBreakIsTotal(t *testing.T) {
	// 前三级全打平 → 只能靠 id 兜底；无此兜底 keeper 会随输入顺序而变
	x := cand("m-zzz", 0, 100, 100, 500, time.Time{})
	y := cand("m-aaa", 0, 100, 100, 500, time.Time{})
	if !betterKeeper(y, x) {
		t.Fatal("全打平时 id 更小者应为 keeper")
	}
	if betterKeeper(x, y) {
		t.Fatal("id 更大者不应胜过 id 更小者（否则不是全序）")
	}
	// 组级断言：洗牌后 keeper 不变
	groups := groupDuplicates([]dupCandidate{x, y}, 0)
	if len(groups) != 1 || groups[0].KeepID != "m-aaa" {
		t.Fatalf("keeper 应为 m-aaa，实际 %v", groupKey(groups))
	}
	groups = groupDuplicates([]dupCandidate{y, x}, 0)
	if len(groups) != 1 || groups[0].KeepID != "m-aaa" {
		t.Fatalf("换输入顺序后 keeper 仍应为 m-aaa，实际 %v", groupKey(groups))
	}
}

func TestGroupDuplicatesKeeperIsFirstItem(t *testing.T) {
	items := []dupCandidate{
		cand("m3", 0x0, 100, 100, 500, time.Time{}),
		cand("m1", 0x1, 900, 900, 500, time.Time{}),
		cand("m2", 0x2, 100, 100, 500, time.Time{}),
	}
	g := groupDuplicates(items, 3)
	if len(g) != 1 {
		t.Fatalf("应得 1 组，实际 %d", len(g))
	}
	if g[0].KeepID != "m1" {
		t.Fatalf("keeper 应为像素最多的 m1，实际 %s", g[0].KeepID)
	}
	if g[0].Items[0].ID != g[0].KeepID {
		t.Fatalf("items[0] 应为 keeper，实际 %s", g[0].Items[0].ID)
	}
	// 组内其余成员按同一判据降序，顺序确定
	if g[0].Items[1].ID != "m2" || g[0].Items[2].ID != "m3" {
		t.Fatalf("组内顺序应为 keeper→m2→m3，实际 %v", groupKey(g))
	}
}

func TestGroupDuplicatesSortedBySizeThenKeepID(t *testing.T) {
	// b1=0xFF00、b2=0xFF03、b3=0xFF0F：b1≈b2(2)、b2≈b3(2)，但 b1 与 b3 相距 4 —— 又一处传递桥接
	items := []dupCandidate{
		cand("s1", 0x0, 10, 10, 1, time.Time{}),
		cand("s2", 0x1, 10, 10, 1, time.Time{}),
		cand("b1", 0xFF00, 10, 10, 1, time.Time{}),
		cand("b2", 0xFF03, 10, 10, 1, time.Time{}),
		cand("b3", 0xFF0F, 10, 10, 1, time.Time{}),
	}
	groups := groupDuplicates(items, 2)
	if len(groups) != 2 {
		t.Fatalf("应得 2 组，实际 %d", len(groups))
	}
	if len(groups[0].Items) != 3 || groups[0].KeepID != "b1" {
		t.Fatalf("大组（3 成员）应排在前，实际 %v", groupKey(groups))
	}
	if groups[1].KeepID != "s1" || len(groups[1].Items) != 2 {
		t.Fatalf("小组应排在后，实际 %v", groupKey(groups))
	}
}

// ---- 阈值 / limit 解析与钳制 ----

func TestParseDuplicateParams(t *testing.T) {
	cases := []struct {
		inT, inL string
		wantT    int
		wantL    int
		wantErr  bool
	}{
		{"", "", 10, 50, false},       // 缺省：T=10、limit=50
		{"0", "", 0, 50, false},       // 下界保留
		{"20", "", 20, 50, false},     // 上界保留
		{"99", "", 20, 50, false},     // 越界钳到上界
		{"-1", "", 0, 50, false},      // 越界钳到下界
		{"abc", "", 0, 0, true},       // 非数字：400
		{"", "abc", 0, 0, true},       // limit 非数字同样 400
		{"10", "0", 10, 1, false},     // limit 下界钳到 1
		{"10", "201", 10, 200, false}, // limit 越界钳到 200
		{" 7 ", " 3 ", 7, 3, false},   // 容忍空白（前端可能带空格）
	}
	for _, tc := range cases {
		gotT, gotL, err := ParseDuplicateParams(tc.inT, tc.inL)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("threshold=%q limit=%q 应报错", tc.inT, tc.inL)
			}
			continue
		}
		if err != nil {
			t.Fatalf("threshold=%q limit=%q 不应报错: %v", tc.inT, tc.inL, err)
		}
		if gotT != tc.wantT || gotL != tc.wantL {
			t.Fatalf("threshold=%q limit=%q: want (%d,%d), got (%d,%d)",
				tc.inT, tc.inL, tc.wantT, tc.wantL, gotT, gotL)
		}
	}
}

func TestDuplicatesHandlerInvalidParams(t *testing.T) {
	h := &Handler{} // Store 为 nil：非法参数必须在触库前 400
	for _, q := range []string{"?threshold=abc", "?limit=x"} {
		rec := dupReq(h, q)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d: %s", q, rec.Code, rec.Body.String())
		}
		var body struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s 响应不是错误封套: %v", q, err)
		}
		if body.Error.Code != "INVALID_PARAMS" {
			t.Fatalf("%s 错误码应为 INVALID_PARAMS，实际 %q", q, body.Error.Code)
		}
	}
}

// ---- 指纹十六进制（最高位置位的场景）----

func TestFormatPhashTopBitSet(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0x0000000000000000"},
		{0x8000000000000000, "0x8000000000000000"}, // bit 63：转 int64 会变负数，转十进制 JSON 会丢精度
		{0xFFFFFFFFFFFFFFFF, "0xffffffffffffffff"},
		{0x0000000000000001, "0x0000000000000001"},
	}
	for _, tc := range cases {
		got := formatPhash(tc.in)
		if got != tc.want {
			t.Fatalf("formatPhash(%#x): want %s, got %s", tc.in, tc.want, got)
		}
		// 必须能无损还原（前端/其它服务要靠它回算）
		back, err := strconv.ParseUint(got[2:], 16, 64)
		if err != nil || back != tc.in {
			t.Fatalf("formatPhash(%#x) 无法无损还原: %s, %v", tc.in, got, err)
		}
	}
}

func TestDuplicateResultJSONShape(t *testing.T) {
	groups := groupDuplicates([]dupCandidate{
		cand("m1", 0x8000000000000000, 100, 100, 1, time.Time{}),
		cand("m2", 0x8000000000000001, 50, 50, 1, time.Time{}),
	}, 1)
	res := &DuplicateResult{Groups: groups, Total: 1, Scanned: 2, Threshold: 1}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var out struct {
		Groups []struct {
			KeepID string `json:"keep_id"`
			Phash  string `json:"phash"`
			Items  []any  `json:"items"`
		} `json:"groups"`
		Total     int  `json:"total"`
		Scanned   int  `json:"scanned"`
		Threshold int  `json:"threshold"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if len(out.Groups) != 1 || out.Groups[0].KeepID != "m1" || out.Groups[0].Phash != "0x8000000000000000" {
		t.Fatalf("组字段不符: %s", b)
	}
	if len(out.Groups[0].Items) != 2 {
		t.Fatalf("items 应有 2 项: %s", b)
	}
	if out.Total != 1 || out.Scanned != 2 || out.Threshold != 1 || out.Truncated {
		t.Fatalf("顶层字段不符: %s", b)
	}
}
