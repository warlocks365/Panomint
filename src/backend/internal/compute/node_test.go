package compute

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptrStr(s string) *string    { return &s }
func ptrInt(v int) *int          { return &v }
func ptrStatus(s Status) *Status { return &s }
func ptrBool(b bool) *bool       { return &b }

// mustJSON 序列化辅助（失败即 Fatal）。
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(b)
}

// TestValidKind 节点类型取值域：三种一律合法，其它非法。
func TestValidKind(t *testing.T) {
	for _, k := range []Kind{KindLocalGPU, KindCloudGPU, KindLANAgent} {
		if !ValidKind(k) {
			t.Fatalf("%q 应合法", k)
		}
	}
	for _, k := range []Kind{"", "gpu", "LAN_AGENT", "local"} {
		if ValidKind(k) {
			t.Fatalf("%q 应非法", k)
		}
	}
}

// TestValidStatus 节点状态取值域。
func TestValidStatus(t *testing.T) {
	for _, s := range []Status{StatusOnline, StatusBusy, StatusOffline} {
		if !ValidStatus(s) {
			t.Fatalf("%q 应合法", s)
		}
	}
	for _, s := range []Status{"", "ONLINE", "idle", "down"} {
		if ValidStatus(s) {
			t.Fatalf("%q 应非法", s)
		}
	}
}

// TestNeedsAgentToken 只有 lan_agent 才需要接入令牌。
//
// 这条断言是安全边界：若 local_gpu/cloud_gpu 也发令牌，等于给"本不需要外部持钥"的
// 节点形态凭空多开一扇门。
func TestNeedsAgentToken(t *testing.T) {
	cases := map[Kind]bool{
		KindLANAgent: true,
		KindLocalGPU: false,
		KindCloudGPU: false,
	}
	for k, want := range cases {
		if got := (RegisterInput{Kind: k}).NeedsAgentToken(); got != want {
			t.Fatalf("kind=%q NeedsAgentToken() = %v，期望 %v", k, got, want)
		}
	}
}

// TestRegisterInputNormalize 登记输入校验与默认值补齐。
func TestRegisterInputNormalize(t *testing.T) {
	t.Run("合法输入：去空白 + 补齐默认", func(t *testing.T) {
		in := RegisterInput{Name: "  我的主机  ", Kind: KindLANAgent, Host: "  10.0.0.9 "}
		if err := in.Normalize(); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if in.Name != "我的主机" {
			t.Fatalf("name 未去空白: %q", in.Name)
		}
		if in.Host != "10.0.0.9" {
			t.Fatalf("host 未去空白: %q", in.Host)
		}
		if in.Codecs != DefaultCodecs {
			t.Fatalf("codecs 应补齐为 %q，实际 %q", DefaultCodecs, in.Codecs)
		}
		if in.Concurrency != 1 {
			t.Fatalf("concurrency 应补齐为 1，实际 %d", in.Concurrency)
		}
	})

	t.Run("显式值不被覆盖", func(t *testing.T) {
		in := RegisterInput{Name: "n", Kind: KindLANAgent, Codecs: "h264,hevc", Concurrency: 4}
		if err := in.Normalize(); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if in.Codecs != "h264,hevc" || in.Concurrency != 4 {
			t.Fatalf("显式值被覆盖: codecs=%q concurrency=%d", in.Codecs, in.Concurrency)
		}
	})

	t.Run("name 为空/纯空格 → 报错", func(t *testing.T) {
		for _, n := range []string{"", "   ", "\t"} {
			in := RegisterInput{Name: n, Kind: KindLANAgent}
			if err := in.Normalize(); err == nil {
				t.Fatalf("name=%q 应报错", n)
			}
		}
	})

	t.Run("kind 非法 → 报错", func(t *testing.T) {
		in := RegisterInput{Name: "n", Kind: "weird"}
		if err := in.Normalize(); err == nil {
			t.Fatal("非法 kind 应报错")
		}
	})

	t.Run("concurrency 越界", func(t *testing.T) {
		in := RegisterInput{Name: "n", Kind: KindLANAgent, Concurrency: -3}
		if err := in.Normalize(); err != nil || in.Concurrency != 1 {
			t.Fatalf("负数应归一为 1，实际 concurrency=%d err=%v", in.Concurrency, err)
		}
		in = RegisterInput{Name: "n", Kind: KindLANAgent, Concurrency: MaxConcurrency + 1}
		if err := in.Normalize(); err == nil {
			t.Fatalf("超过上限 %d 应报错", MaxConcurrency)
		}
		in = RegisterInput{Name: "n", Kind: KindLANAgent, Concurrency: MaxConcurrency}
		if err := in.Normalize(); err != nil {
			t.Fatalf("恰好等于上限不应报错: %v", err)
		}
	})

	t.Run("vram_mb 为负 → 报错（0 表示 CPU-only，合法）", func(t *testing.T) {
		in := RegisterInput{Name: "n", Kind: KindLANAgent, VRAMMB: ptrInt(-1)}
		if err := in.Normalize(); err == nil {
			t.Fatal("负显存应报错")
		}
		in = RegisterInput{Name: "n", Kind: KindLANAgent, VRAMMB: ptrInt(0)}
		if err := in.Normalize(); err != nil {
			t.Fatalf("0 显存（CPU 节点）应合法: %v", err)
		}
	})
}

// TestPatchInputValidate PATCH 部分更新的校验。
func TestPatchInputValidate(t *testing.T) {
	t.Run("全部为空 → Empty 为真", func(t *testing.T) {
		var in PatchInput
		if !in.Empty() {
			t.Fatal("空 PATCH 应被判为 Empty")
		}
		if err := in.Validate(); err != nil {
			t.Fatalf("空 PATCH 校验本身不应报错（由 Empty 拦截）: %v", err)
		}
	})

	t.Run("任一字段非 nil → Empty 为假", func(t *testing.T) {
		ins := []PatchInput{
			{Status: ptrStatus(StatusOffline)},
			{Codecs: ptrStr("h264")},
			{HasNVENC: ptrBool(true)},
			{VRAMMB: ptrInt(24576)},
			{Concurrency: ptrInt(2)},
			{Name: ptrStr("n")},
			{Host: ptrStr("h")},
			{RotateToken: true},
		}
		for i, in := range ins {
			if in.Empty() {
				t.Fatalf("第 %d 个 PATCH 不应判为 Empty", i)
			}
		}
	})

	t.Run("status 非法 → 报错", func(t *testing.T) {
		in := PatchInput{Status: ptrStatus("dead")}
		if err := in.Validate(); err == nil {
			t.Fatal("非法 status 应报错")
		}
	})

	t.Run("name 纯空格 → 报错；host 允许空串（语义为清空）", func(t *testing.T) {
		in := PatchInput{Name: ptrStr("  ")}
		if err := in.Validate(); err == nil {
			t.Fatal("空 name 应报错")
		}
		in = PatchInput{Host: ptrStr("   ")}
		if err := in.Validate(); err != nil {
			t.Fatalf("空 host 不应报错（表示清空）: %v", err)
		}
		if *in.Host != "" {
			t.Fatalf("host 应被去空白为空串，实际 %q", *in.Host)
		}
	})

	t.Run("codecs 空串 → 报错（防误清空）", func(t *testing.T) {
		in := PatchInput{Codecs: ptrStr(" ")}
		if err := in.Validate(); err == nil {
			t.Fatal("空 codecs 应报错")
		}
	})

	t.Run("concurrency 越界", func(t *testing.T) {
		for _, v := range []int{0, -1, MaxConcurrency + 1} {
			in := PatchInput{Concurrency: ptrInt(v)}
			if err := in.Validate(); err == nil {
				t.Fatalf("concurrency=%d 应报错", v)
			}
		}
		in := PatchInput{Concurrency: ptrInt(MaxConcurrency)}
		if err := in.Validate(); err != nil {
			t.Fatalf("concurrency=%d 不应报错: %v", MaxConcurrency, err)
		}
	})

	t.Run("归一化会写回指针（去空白）", func(t *testing.T) {
		in := PatchInput{Name: ptrStr("  新名字 "), Codecs: ptrStr(" h264,hevc ")}
		if err := in.Validate(); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if *in.Name != "新名字" || *in.Codecs != "h264,hevc" {
			t.Fatalf("未写回去空白后的值: name=%q codecs=%q", *in.Name, *in.Codecs)
		}
	})
}

// TestNodeJSONTags Node 的 JSON 字段名不得把令牌哈希之类敏感字段带出去。
//
// 这是"哈希不下发"的守门测试：Node 是直接序列化给管理端的结构体，
// 一旦有人把 agent_token_hash 加进来，这个测试会失败。
func TestNodeJSONTags(t *testing.T) {
	n := Node{ID: "id", Name: "n", Kind: KindLANAgent, Codecs: "h264", Status: StatusOnline, EffectiveStatus: StatusOffline}
	raw := mustJSON(t, n)
	for _, bad := range []string{"agent_token", "token_hash", "secret"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("Node 的 JSON 不应包含 %q：%s", bad, raw)
		}
	}
	for _, want := range []string{`"effective_status":"offline"`, `"status":"online"`, `"kind":"lan_agent"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("Node 的 JSON 缺少 %s：%s", want, raw)
		}
	}
}
