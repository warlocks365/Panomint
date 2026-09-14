package shares

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// newTestCtx 构造带 URL 的 gin 测试上下文（PROBE 逻辑均为纯函数，无需 DB）。
func newTestCtx(target string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", target, nil)
	return c, w
}

func TestClampInt(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 1, 10, 5},
		{0, 1, 10, 1},
		{-3, 1, 10, 1},
		{99, 1, 10, 10},
	}
	for _, tc := range cases {
		if got := clampInt(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Fatalf("clampInt(%d,%d,%d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

// TestKbpsOf 速率换算的边界：零字节回零、耗时下限防无穷、上封闭顶。
func TestKbpsOf(t *testing.T) {
	cases := []struct {
		name string
		n    int
		d    time.Duration
		want int
	}{
		{"零字节回零（不得报 1kbps 假数据）", 0, time.Second, 0},
		{"1MiB/1s = 8389kbps", 1 << 20, time.Second, 8389},
		{"耗时趋零按 5ms 下限计", 1000, 0, 1600},
		{"1MiB/1ms 仍受 5ms 下限压制，不报无穷大", 1 << 20, time.Millisecond, 1677722},
		{"慢链路不封底（256KiB/8s）", 256 << 10, 8 * time.Second, 262},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kbpsOf(tc.n, tc.d); got != tc.want {
				t.Fatalf("kbpsOf(%d,%v) = %d, want %d", tc.n, tc.d, got, tc.want)
			}
		})
	}
}

// TestProbeSize 探针字节数夹在 [64KiB,1MiB]，且 query 优先于环境变量。
func TestProbeSize(t *testing.T) {
	c, _ := newTestCtx("/probe?bytes=1")
	if got := probeSize(c); got != minProbeBytes {
		t.Fatalf("过小 bytes 应夹到 %d，实际 %d", minProbeBytes, got)
	}
	c, _ = newTestCtx("/probe?bytes=99999999")
	if got := probeSize(c); got != maxProbeBytes {
		t.Fatalf("过大 bytes 应夹到 %d，实际 %d", maxProbeBytes, got)
	}
	c, _ = newTestCtx("/probe?bytes=131072")
	if got := probeSize(c); got != 131072 {
		t.Fatalf("合法 bytes 应原样生效，实际 %d", got)
	}

	t.Setenv("BW_PROBE_BYTES", "65536")
	c, _ = newTestCtx("/probe")
	if got := probeSize(c); got != 65536 {
		t.Fatalf("环境变量应生效，实际 %d", got)
	}
	c, _ = newTestCtx("/probe?bytes=262144")
	if got := probeSize(c); got != 262144 {
		t.Fatalf("query 应优先于环境变量，实际 %d", got)
	}
}

// TestProbeDeadline 总耗时上限可由环境变量调整，非法值回落默认。
func TestProbeDeadline(t *testing.T) {
	if got := probeDeadline(); got != time.Duration(defaultProbeMaxMs)*time.Millisecond {
		t.Fatalf("默认上限应为 %dms，实际 %v", defaultProbeMaxMs, got)
	}
	t.Setenv("BW_PROBE_MAX_MS", "1500")
	if got := probeDeadline(); got != 1500*time.Millisecond {
		t.Fatalf("环境变量应生效，实际 %v", got)
	}
	t.Setenv("BW_PROBE_MAX_MS", "-1")
	if got := probeDeadline(); got != time.Duration(defaultProbeMaxMs)*time.Millisecond {
		t.Fatalf("非法值应回落默认，实际 %v", got)
	}
}

// TestProbeSampleMerge 下行探针先到、上行探针后到时不得互相清零；取出即消费。
func TestProbeSampleMerge(t *testing.T) {
	key := "t:merge:" + t.Name()
	putProbe(key, 0, 5000, 0) // 下行探针
	putProbe(key, 800, 0, 42) // 上行探针
	s, ok := takeProbe(key)
	if !ok {
		t.Fatal("探针结果应存在")
	}
	if s.down != 5000 || s.up != 800 || s.lat != 42 {
		t.Fatalf("合并结果 up=%d down=%d lat=%d，期望 800/5000/42", s.up, s.down, s.lat)
	}
	if _, ok := takeProbe(key); ok {
		t.Fatal("takeProbe 应一次性取出（避免重复落库）")
	}
}

// TestProbeSampleTTL 过期样本不得被复用。
func TestProbeSampleTTL(t *testing.T) {
	key := "t:stale:" + t.Name()
	probeSamples.Lock()
	probeSamples.m[key] = probeSample{down: 9999, at: time.Now().Add(-probeSampleTTL - time.Second)}
	probeSamples.Unlock()
	if _, ok := takeProbe(key); ok {
		t.Fatal("过期样本应被丢弃")
	}
}

// TestAggregateProbe 浏览器实测提示优先于服务端计时；上行只认服务端测量。
func TestAggregateProbe(t *testing.T) {
	key := "t:agg:" + t.Name()
	putProbe(key, 700, 3000, 30)
	c, _ := newTestCtx("/bandwidth/self-test?down_kbps=1234&latency_ms=77")
	up, down, lat := aggregateProbe(c, key)
	if up != 700 {
		t.Fatalf("上行应取服务端测量 700，实际 %d", up)
	}
	if down != 1234 {
		t.Fatalf("下行应被浏览器提示覆盖为 1234，实际 %d", down)
	}
	if lat != 77 {
		t.Fatalf("延迟应被浏览器提示覆盖为 77，实际 %d", lat)
	}

	// 无任何数据：全零（前端据此回落 hls.js 默认 ABR）
	c, _ = newTestCtx("/bandwidth/self-test")
	if up, down, lat = aggregateProbe(c, "t:agg-missing"); up != 0 || down != 0 || lat != 0 {
		t.Fatalf("无数据应全零，实际 up=%d down=%d lat=%d", up, down, lat)
	}

	// 非法/非正提示不得污染结果
	key2 := "t:agg2:" + t.Name()
	putProbe(key2, 0, 4000, 0)
	c, _ = newTestCtx("/bandwidth/self-test?down_kbps=abc&latency_ms=-5")
	if _, down, _ = aggregateProbe(c, key2); down != 4000 {
		t.Fatalf("非法提示应被忽略，保留服务端测量 4000，实际 %d", down)
	}

	// 离谱的浏览器提示必须被封顶 5Gbps（既防前端 bug 也防脏数据入库）
	key3 := "t:agg3:" + t.Name()
	putProbe(key3, 0, 4000, 0)
	c, _ = newTestCtx("/bandwidth/self-test?down_kbps=999999999")
	if _, down, _ = aggregateProbe(c, key3); down != maxReportedKbps {
		t.Fatalf("超限提示应封顶到 %d，实际 %d", maxReportedKbps, down)
	}
}

func TestScopeFilter(t *testing.T) {
	q, args := scopeFilter(bandwidthScope{Scope: "global"})
	if q != "scope = $1 AND ref_id IS NULL" || len(args) != 1 {
		t.Fatalf("global 过滤错误: %q %v", q, args)
	}
	id := "b7cc6683-e0d0-4e6b-a9b1-4f53f6722b4b"
	q, args = scopeFilter(bandwidthScope{Scope: "share_token", RefID: &id})
	if q != "scope = $1 AND ref_id = $2" || len(args) != 2 || args[1] != id {
		t.Fatalf("share_token 过滤错误: %q %v", q, args)
	}
}

// TestScopesFor 查询顺序为「具体 scope → global」，global 本身不重复。
func TestScopesFor(t *testing.T) {
	id := "b7cc6683-e0d0-4e6b-a9b1-4f53f6722b4b"
	got := scopesFor(bandwidthScope{Scope: "share_token", RefID: &id})
	if len(got) != 2 || got[0].Scope != "share_token" || got[1].Scope != "global" || got[1].RefID != nil {
		t.Fatalf("share_token 查询顺序错误: %+v", got)
	}
	got = scopesFor(bandwidthScope{Scope: "global"})
	if len(got) != 1 || got[0].Scope != "global" {
		t.Fatalf("global 应只有一项: %+v", got)
	}
}

func TestUUIDRe(t *testing.T) {
	if !uuidRe.MatchString("b7cc6683-e0d0-4e6b-a9b1-4f53f6722b4b") {
		t.Fatal("合法 uuid 应通过")
	}
	for _, bad := range []string{"", "not-a-uuid", "b7cc6683-e0d0-4e6b-a9b1", "'; DROP TABLE share_links;--"} {
		if uuidRe.MatchString(bad) {
			t.Fatalf("非法 ref_id 应被拒: %q", bad)
		}
	}
}

func TestProbeKeysIsolated(t *testing.T) {
	// 不同分享/不同 IP/不同用户必须互不串味（否则会读到别人的测量值）
	if shareProbeKey("a", "1.1.1.1") == shareProbeKey("b", "1.1.1.1") ||
		shareProbeKey("a", "1.1.1.1") == shareProbeKey("a", "2.2.2.2") ||
		shareProbeKey("a", "1.1.1.1") == userProbeKey("a", "1.1.1.1") {
		t.Fatal("探针缓存 key 必须按 (维度, id, ip) 隔离")
	}
}
