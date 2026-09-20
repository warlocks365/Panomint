package audit

// 纯逻辑单测：归一化 / 校验 / 脱敏 / 动作登记表。
// 全部不依赖数据库（本项目既有单测风格：需要真库的用例一律另设，见各 *_e2e）。

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNormalizeAction(t *testing.T) {
	ok := []struct{ in, want string }{
		{"admin.audit.read", "admin.audit.read"},
		{"  Admin.Audit.Read  ", "admin.audit.read"}, // 去空白 + 转小写
		{"AUTH.LOGIN", "auth.login"},
		{"share.create", "share.create"},
		{"a", "a"},
		{"a1.b2_c3.d4", "a1.b2_c3.d4"},
	}
	for _, tc := range ok {
		got, err := NormalizeAction(tc.in)
		if err != nil {
			t.Fatalf("NormalizeAction(%q) 意外报错: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeAction(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}

	bad := []string{
		"",                                  // 空
		"   ",                               // 全空白
		"1login",                            // 数字开头
		".login",                            // 点开头
		"admin/audit",                       // 斜杠
		"admin audit",                       // 空格
		"admin:audit",                       // 冒号（权限名风格，不是动作名）
		"登录",                                // 非 ASCII
		"admin\naudit",                      // 内嵌换行（首尾空白会被 TrimSpace 去掉，内嵌的不会）
		strings.Repeat("a", maxActionLen+1), // 超长
	}
	for _, in := range bad {
		if got, err := NormalizeAction(in); err == nil {
			t.Fatalf("NormalizeAction(%q) 应报错，实际返回 %q", in, got)
		}
	}

	// 边界：正好 maxActionLen 应通过（列宽 VARCHAR(128)）。
	if _, err := NormalizeAction(strings.Repeat("a", maxActionLen)); err != nil {
		t.Fatalf("长度 %d 应合法: %v", maxActionLen, err)
	}
}

func TestNormalizeActor(t *testing.T) {
	// 空串合法：系统任务（如索引重建）没有触发用户。
	if got, err := NormalizeActor("  "); err != nil || got != "" {
		t.Fatalf("空 actor 应合法且归一为空串，实际 %q,%v", got, err)
	}
	const u = "7C6B1B9C-CBA2-4394-B982-67D9038421F3"
	got, err := NormalizeActor(" " + u + " ")
	if err != nil {
		t.Fatalf("合法 UUID 报错: %v", err)
	}
	if got != strings.ToLower(u) {
		t.Fatalf("UUID 应归一为小写，实际 %q", got)
	}
	for _, bad := range []string{"not-a-uuid", "123e4567-e89b-12d3-a456-42661417400", "123e4567e89b12d3a456426614174000"} {
		if _, err := NormalizeActor(bad); err == nil {
			t.Fatalf("NormalizeActor(%q) 应报错（actor 是 users(id) 外键，非 UUID 必然外键冲突）", bad)
		}
	}
}

func TestNormalizeTargetType(t *testing.T) {
	if got, err := NormalizeTargetType(""); err != nil || got != "" {
		t.Fatalf("空 target_type 应合法，实际 %q,%v", got, err)
	}
	if got, err := NormalizeTargetType(" Compute_Node "); err != nil || got != "compute_node" {
		t.Fatalf("应归一为 compute_node，实际 %q,%v", got, err)
	}
	for _, bad := range []string{"9user", "user-x", "user x", strings.Repeat("a", maxTargetTypeLen+1)} {
		if _, err := NormalizeTargetType(bad); err == nil {
			t.Fatalf("NormalizeTargetType(%q) 应报错", bad)
		}
	}
}

func TestNormalizeTargetID(t *testing.T) {
	if got, err := NormalizeTargetID("   "); err != nil || got != "" {
		t.Fatalf("空 target_id 应合法，实际 %q,%v", got, err)
	}
	// target_id 允许非 UUID（分享 token / 设置键名），但不得含控制字符。
	if got, err := NormalizeTargetID(" 3c404f62-4ed4-4c7b-97c6-f7e2cf845c0e "); err != nil ||
		got != "3c404f62-4ed4-4c7b-97c6-f7e2cf845c0e" {
		t.Fatalf("UUID 型 target_id 应原样保留，实际 %q,%v", got, err)
	}
	if got, err := NormalizeTargetID("map.default_zoom"); err != nil || got != "map.default_zoom" {
		t.Fatalf("点分键名应合法，实际 %q,%v", got, err)
	}
	for _, bad := range []string{"a\nb", "a\rb", "a\tb", "a\x00b", strings.Repeat("a", maxTargetIDLen+1)} {
		if _, err := NormalizeTargetID(bad); err == nil {
			t.Fatalf("NormalizeTargetID(%q) 应报错（控制字符 / 超长）", bad)
		}
	}
}

func TestNormalizeIPAndUserAgent(t *testing.T) {
	if got := NormalizeIP(" 192.168.1.115 "); got != "192.168.1.115" {
		t.Fatalf("IP 归一失败: %q", got)
	}
	// IP 列宽 VARCHAR(64)：超长必须截断而不是让 INSERT 报错。
	if got := NormalizeIP(strings.Repeat("9", 200)); len(got) != maxIPLen {
		t.Fatalf("IP 应截断到 %d，实际长度 %d", maxIPLen, len(got))
	}

	ua := "Mozilla/5.0 (Windows NT 10.0)\r\nX-Injected: 1"
	got := NormalizeUserAgent(ua)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("UA 应剔除控制字符（防日志/列表换行注入），实际 %q", got)
	}
	// 按 rune 截断：多字节字符不应被劈成非法 UTF-8。
	long := strings.Repeat("测", maxUserAgentLen+50)
	got = NormalizeUserAgent(long)
	if len([]rune(got)) != maxUserAgentLen {
		t.Fatalf("UA 应按 rune 截断到 %d，实际 %d", maxUserAgentLen, len([]rune(got)))
	}
	if !utf8.ValidString(got) {
		t.Fatal("UA 截断后不是合法 UTF-8（按 byte 截断会劈坏多字节字符）")
	}
}

// TestEntryNormalizeInvariant target_id 非空时 target_type 必须非空。
//
// 理由：只有 id 没有类型，查询侧无法拼出「查某用户被改过什么」这类最有用的问句，
// 而记录看起来是"完整的"。宁可在写入时拒掉。
func TestEntryNormalizeInvariant(t *testing.T) {
	_, err := Entry{Action: ActionShareRevoke, TargetID: "abc"}.Normalize()
	if err == nil {
		t.Fatal("只给 target_id 不给 target_type 应被拒（否则记录不可查询）")
	}

	e, err := Entry{
		Action:      "  ADMIN.AUDIT.READ ",
		ActorUserID: "7C6B1B9C-CBA2-4394-B982-67D9038421F3",
		TargetType:  " Audit_Log ",
		TargetID:    "  ",
		IP:          "10.0.0.1",
	}.Normalize()
	if err != nil {
		t.Fatalf("合法 Entry 报错: %v", err)
	}
	if e.Action != ActionAuditRead || e.TargetType != TargetAuditLog || e.TargetID != "" {
		t.Fatalf("归一化结果不符: %+v", e)
	}
	if e.ActorUserID != "7c6b1b9c-cba2-4394-b982-67d9038421f3" {
		t.Fatalf("actor 应小写: %q", e.ActorUserID)
	}
	if e.At.IsZero() {
		t.Fatal("At 为零值时应补当前时间")
	}

	// 未指定 At 时不得改写调用方给的值。
	fixed := time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)
	e2, err := Entry{Action: "auth.login", At: fixed, TargetType: "user", TargetID: "x"}.Normalize()
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if !e2.At.Equal(fixed) {
		t.Fatalf("At 被改写: %v", e2.At)
	}
}

// TestRedactDetailRemovesSensitive 审计模块的红线：审计表不能成为密码/令牌的第二份副本。
func TestRedactDetailRemovesSensitive(t *testing.T) {
	in := map[string]any{
		"action":        "login",
		"password":      "hunter2",
		"refresh_token": "eyJhbGciOi...",
		"agent_token":   "pano_agent_xxx",
		"api_key":       "amap-key",
		"apiKey":        "camelCase 也要命中",
		"mfa_secret":    "JBSWY3DPEHPK3PXP",
		"password_hash": "$2a$10$abc",
		"ok_field":      "keep-me",
	}
	out := RedactDetail(in)

	for _, k := range []string{"password", "refresh_token", "agent_token", "api_key", "apiKey", "mfa_secret", "password_hash"} {
		if _, hit := out[k]; hit {
			t.Fatalf("敏感键 %q 未被剔除: %+v", k, out)
		}
	}
	if out["ok_field"] != "keep-me" {
		t.Fatalf("非敏感键应保留: %+v", out)
	}
	if out["action"] != "login" {
		t.Fatalf("非敏感键应保留: %+v", out)
	}
	// 入参不得被修改（副本语义）。
	if _, still := in["password"]; !still {
		t.Fatal("RedactDetail 不得修改入参")
	}
}

// TestRedactDetailNested 嵌套结构也必须脱敏 —— 凭据最常见的藏身处就是嵌套对象。
func TestRedactDetailNested(t *testing.T) {
	in := map[string]any{
		"before": map[string]any{"role": "viewer", "password": "x"},
		"after":  map[string]any{"role": "admin"},
		"items": []any{
			map[string]any{"id": "a", "token": "t"},
			map[string]any{"id": "b"},
			"plain",
		},
		"headers": map[string]string{"Authorization": "Bearer x", "X-Trace": "keep"},
	}
	out := RedactDetail(in)

	before := out["before"].(map[string]any)
	if before["role"] != "viewer" {
		t.Fatalf("嵌套非敏感键应保留: %+v", before)
	}
	if _, hit := before["password"]; hit {
		t.Fatalf("嵌套敏感键未剔除: %+v", before)
	}
	items := out["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("数组长度应保持 3，实际 %d", len(items))
	}
	if _, hit := items[0].(map[string]any)["token"]; hit {
		t.Fatalf("数组内对象未脱敏: %+v", items[0])
	}
	if items[2] != "plain" {
		t.Fatalf("标量元素应原样保留: %+v", items[2])
	}
	headers := out["headers"].(map[string]any)
	if _, hit := headers["Authorization"]; hit {
		t.Fatalf("map[string]string 分支未脱敏: %+v", headers)
	}
	if headers["X-Trace"] != "keep" {
		t.Fatalf("map[string]string 非敏感键应保留: %+v", headers)
	}
}

// TestRedactDetailEmptySemantics nil 与「全部被脱敏」必须可区分。
//
//   - nil  → 落库 NULL  → 读作「压根没记 detail」
//   - {}   → 落库 `{}`  → 读作「有 detail，但内容全属敏感」
//
// 追责时这两句话含义完全不同，故不能都塌成 NULL。
func TestRedactDetailEmptySemantics(t *testing.T) {
	if got := RedactDetail(nil); got != nil {
		t.Fatalf("nil 入参应返回 nil，实际 %#v", got)
	}
	if got := RedactDetail(map[string]any{}); got != nil {
		t.Fatalf("空 map 入参应返回 nil，实际 %#v", got)
	}
	got := RedactDetail(map[string]any{"password": "x"})
	if got == nil {
		t.Fatal("全部键被脱敏时应返回非 nil 空 map（与“没记 detail”区分）")
	}
	if len(got) != 0 {
		t.Fatalf("应返回空 map，实际 %+v", got)
	}
}

func TestIsSensitiveKey(t *testing.T) {
	for _, k := range []string{"password", "PASSWORD", "old_pwd", "app_password_hash", "authorization", "Cookie", "TOTP", "SecretKey", "agentToken", "refresh_token_hash", "jwt"} {
		if !IsSensitiveKey(k) {
			t.Fatalf("键 %q 应判为敏感", k)
		}
	}
	for _, k := range []string{"action", "role", "before", "after", "limit", "media_id", "actor", "target_type", "profile", "result_path", "paged", "path", "filename"} {
		if IsSensitiveKey(k) {
			t.Fatalf("键 %q 不应判为敏感（会丢失审计信息）", k)
		}
	}
}

// TestActionRegistryIsValid 登记表里的动作名必须都能通过写入侧归一化。
//
// 否则会出现「写的时候报错被吞掉、查的时候永远查不到」的静默失效 ——
// 这正是集中登记动作名的全部意义。
func TestActionRegistryIsValid(t *testing.T) {
	registry := map[string]string{
		"ActionAuditRead":     ActionAuditRead,
		"ActionLogin":         ActionLogin,
		"ActionSSOLogin":      ActionSSOLogin,
		"ActionLogout":        ActionLogout,
		"ActionTokenRotate":   ActionTokenRotate,
		"ActionMFASetup":      ActionMFASetup,
		"ActionMFAEnable":     ActionMFAEnable,
		"ActionMFADisable":    ActionMFADisable,
		"ActionUserCreate":    ActionUserCreate,
		"ActionUserUpdate":    ActionUserUpdate,
		"ActionUserDelete":    ActionUserDelete,
		"ActionRoleChange":    ActionRoleChange,
		"ActionShareCreate":   ActionShareCreate,
		"ActionShareRevoke":   ActionShareRevoke,
		"ActionShareDownload": ActionShareDownload,
		"ActionMediaDelete":   ActionMediaDelete,
		"ActionMediaPurge":    ActionMediaPurge,
		"ActionMediaRestore":  ActionMediaRestore,
		"ActionSettingsPatch": ActionSettingsPatch,
		"ActionIndexRebuild":  ActionIndexRebuild,
	}
	seen := map[string]string{}
	for name, v := range registry {
		got, err := NormalizeAction(v)
		if err != nil {
			t.Fatalf("%s = %q 无法通过 NormalizeAction: %v", name, v, err)
		}
		if got != v {
			t.Fatalf("%s = %q 不是规范形式（应已小写无空白），归一化得到 %q", name, v, got)
		}
		if prev, dup := seen[v]; dup {
			t.Fatalf("%s 与 %s 动作名重复：%q", name, prev, v)
		}
		seen[v] = name
	}
}

func TestTargetRegistryIsValid(t *testing.T) {
	for _, tt := range []string{TargetUser, TargetRole, TargetShare, TargetMedia, TargetSetting, TargetComputeNode, TargetAuditLog} {
		got, err := NormalizeTargetType(tt)
		if err != nil || got != tt {
			t.Fatalf("目标类型常量 %q 不合法（归一化得 %q，err=%v）", tt, got, err)
		}
	}
}

func TestIndexStateMapping(t *testing.T) {
	for in, want := range map[string]string{
		"pending": "running",
		"running": "running",
		"done":    "idle",
		"failed":  "failed",
		"queued":  "unknown", // 未知状态不得猜成 running/idle
		"":        "unknown",
	} {
		if got := IndexState(in); got != want {
			t.Fatalf("IndexState(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestJobProgress(t *testing.T) {
	i := func(n int) *int { return &n }
	if p := jobProgress(nil, i(1)); p != nil {
		t.Fatal("total 缺失应返回 nil（不是 0）")
	}
	if p := jobProgress(i(0), i(0)); p != nil {
		t.Fatal("total=0 应返回 nil（0 会被读成“还没开始”，与“未知”不同）")
	}
	p := jobProgress(i(105), i(72))
	if p == nil || *p < 0.685 || *p > 0.686 {
		t.Fatalf("progress 计算错误: %v", p)
	}
	if p := jobProgress(i(10), i(10)); p == nil || *p != 1.0 {
		t.Fatalf("完成应为 1.0，实际 %v", p)
	}
}
