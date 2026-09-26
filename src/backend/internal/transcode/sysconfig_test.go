package transcode

// system_transcode_config 读写的源码形状守卫（系统级开关语义钉）。
//
// 能证明什么：sysconfig.go 里三条不可漂移的语义——
//   - 缺行 = 默认开启（ErrNoRows 走 DefaultAutoTranscode 分支，不报错）；
//   - 查询真错误 fail-closed（return false, err，由 CreateJob 映射 500）；
//   - 写入走 singleton 原子 upsert（单行硬约束，迁移 00038）。
//
// 不能证明什么：真实库上「无行 → true / false 行 → false / upsert 幂等」——
// 测试环境无数据库，行为级断言由部署后 verify 脚本覆盖。

import (
	"os"
	"strings"
	"testing"
)

func TestGetSystemAutoTranscodeSemantics(t *testing.T) {
	b, err := os.ReadFile("sysconfig.go")
	if err != nil {
		t.Fatalf("读不到 sysconfig.go: %v", err)
	}
	seg := string(b)

	if !strings.Contains(seg, "errors.Is(err, pgx.ErrNoRows)") {
		t.Fatal("GetSystemAutoTranscode 必须把 ErrNoRows 排除在错误分支外（缺行=开启）")
	}
	if !strings.Contains(seg, "return DefaultAutoTranscode, nil") {
		t.Fatal("缺行时必须返回 DefaultAutoTranscode（true），存量部署行为不变")
	}
	if !strings.Contains(seg, "return false, err") {
		t.Fatal("查询真错误必须 fail-closed（false, err），由调用方 500 拒绝服务")
	}
	if DefaultAutoTranscode != true {
		t.Fatal("DefaultAutoTranscode 必须恒为 true")
	}
}

func TestPutSystemAutoTranscodeUpsertSingleton(t *testing.T) {
	b, err := os.ReadFile("sysconfig.go")
	if err != nil {
		t.Fatalf("读不到 sysconfig.go: %v", err)
	}
	if !strings.Contains(string(b), "ON CONFLICT (singleton) DO UPDATE") {
		t.Fatal("写入必须走 singleton 原子 upsert（单行硬约束，00023/00038 同款）")
	}
}

// Job000124 增量：realtime_transcode 缺省必须恒为 false（存量部署升级后行为逐字节不变），
// 且部分更新 upsert 必须用 COALESCE 保列（nil = 该列不动，缺失不改语义）。
// Job000125 更新：实现委托 PutSystemConfigFields（9 占位符版本），断言随新 SQL 形状同步。
func TestRealtimeTranscodeDefaultsAndPartialUpsert(t *testing.T) {
	b, err := os.ReadFile("sysconfig.go")
	if err != nil {
		t.Fatalf("读不到 sysconfig.go: %v", err)
	}
	seg := string(b)
	if DefaultRealtimeTranscode != false {
		t.Fatal("DefaultRealtimeTranscode 必须恒为 false（Job000124 之前无此能力）")
	}
	if !strings.Contains(seg, "realtime_transcode = COALESCE($2") {
		t.Fatal("部分更新 upsert 必须用 COALESCE($2, ...) 保 realtime 列（缺失不改）")
	}
	if !strings.Contains(seg, "COALESCE($1::boolean, $6::boolean)") {
		t.Fatal("INSERT 分支 COALESCE 必须显式 ::boolean 锚定——nil *bool 经 pgx 以 unknown 传入时" +
			" COALESCE 被解析成 text，落布尔列 42804（Job000124 e2e 首跑实测踩出；Job000125 扩到 9 占位符）")
	}
	if !strings.Contains(seg, "PutSystemConfigFields 至少需提供一个待更新字段") {
		t.Fatal("全字段 nil 必须报错（ErrValidation 哨兵包裹，Handler 映射 400）")
	}
	if !strings.Contains(seg, "var ErrValidation") || !strings.Contains(seg, "func isValidationErr") {
		t.Fatal("校验错误必须走 ErrValidation 哨兵分类（400 与存储故障 500 的分界）")
	}
}

// Job000125 增量：HLS 三字段（分片时长/缓存档/外部地址）的写入校验钉。
// 行为级断言（真库 upsert 幂等、存量行默认值）由部署后 verify 脚本覆盖——与本文件其余守卫同口径。
func TestHLSFieldsValidation(t *testing.T) {
	if DefaultHLSCacheProfile != "balanced" {
		t.Fatal("DefaultHLSCacheProfile 必须为 balanced（= 历史响应头行为，存量不变）")
	}
	if DefaultSegSeconds != 4 {
		t.Fatal("DefaultSegSeconds 必须为 4（hls.go 唯一真源，迁移 DEFAULT 与之一致）")
	}

	// seg 范围
	if err := validateHLSUpdate(SystemConfigUpdate{SegSeconds: intPtr(1)}); err == nil {
		t.Fatal("seg=1 必须被拒（下界 2）")
	}
	if err := validateHLSUpdate(SystemConfigUpdate{SegSeconds: intPtr(21)}); err == nil {
		t.Fatal("seg=21 必须被拒（上界 20）")
	}
	if err := validateHLSUpdate(SystemConfigUpdate{SegSeconds: intPtr(6)}); err != nil {
		t.Fatalf("seg=6 必须合法: %v", err)
	}

	// 缓存档枚举
	if err := validateHLSUpdate(SystemConfigUpdate{CacheProfile: strPtr("rude")}); err == nil {
		t.Fatal("非法缓存档必须被拒")
	}
	for _, p := range []string{"no_cache", "balanced", "aggressive"} {
		if err := validateHLSUpdate(SystemConfigUpdate{CacheProfile: strPtr(p)}); err != nil {
			t.Fatalf("缓存档 %q 必须合法: %v", p, err)
		}
	}

	// 外部地址形态
	if err := validateHLSUpdate(SystemConfigUpdate{StreamBaseURL: strPtr("")}); err != nil {
		t.Fatalf("空地址（站内相对路径）必须合法: %v", err)
	}
	for _, bad := range []string{
		"media.example.com",          // 缺 scheme
		"ftp://media.example.com",    // 非 http(s)
		"https://m.example.com/",     // 尾斜杠（拼接会造成 //）
		"https://m.example.com?a=b",  // 查询串
		"https://m.example.com#frag", // 片段
		"https://m.example.com/x y",  // 空白
	} {
		if err := validateHLSUpdate(SystemConfigUpdate{StreamBaseURL: strPtr(bad)}); err == nil {
			t.Fatalf("stream_base_url=%q 必须被拒", bad)
		}
	}
	if err := validateHLSUpdate(SystemConfigUpdate{StreamBaseURL: strPtr("https://media.example.com")}); err != nil {
		t.Fatalf("合法外部地址必须通过: %v", err)
	}

	// 全 nil 仍须报错（两道守卫中的第二道）
	if err := validateHLSUpdate(SystemConfigUpdate{}); err != nil {
		t.Fatalf("空更新集在 validate 层应放行（空集拦截在 PutSystemConfigFields）: %v", err)
	}
}

func intPtr(v int) *int    { return &v }
func strPtr(v string) *string { return &v }
