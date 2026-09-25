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
