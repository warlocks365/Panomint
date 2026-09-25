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
	if !strings.Contains(seg, "COALESCE($1::boolean, $3::boolean)") {
		t.Fatal("INSERT 分支 COALESCE 必须显式 ::boolean 锚定——nil *bool 经 pgx 以 unknown 传入时" +
			" COALESCE($1,$3) 被解析成 text，落布尔列 42804（Job000124 e2e 首跑实测踩出）")
	}
	if !strings.Contains(seg, "PutSystemConfig: 至少需提供一个待更新字段") {
		t.Fatal("两字段全 nil 必须报错（调用方缺陷的第二道守卫）")
	}
}
