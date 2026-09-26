package transcode

// Job000124「播放时自动转码」auto 分支的源码形状守卫（配套 autotc_gate_test.go）。
//
// 能证明什么：CreateJob 的 auto 分支在源码里真实存在且次序正确——
//   - attach 查询（复用 pending/running 任务）排在 enqueueOne 之前：
//     自动触发绝不重复写任务行/入队（幂等，多端并发同一媒体共享一个活动任务）；
//   - 服务端选档 ProfileForSource 出现在 attach 之后、enqueueOne 之前：
//     无活动任务时按源分辨率自动选档（与 CLI 补排同口径），请求 profile 被忽略；
//   - 复用分支的响应带 reused=true，调用方可区分「新建」与「attach」。
//
// 不能证明什么：真实 HTTP 往返下 attach/新建的行为——本仓库测试环境无数据库，
// 该端到端由部署后 verify 脚本覆盖（设计方案 §8）。

import (
	"os"
	"strings"
	"testing"
)

func TestCreateJobAutoBranchShape(t *testing.T) {
	b, err := os.ReadFile("transcode.go")
	if err != nil {
		t.Fatalf("读不到 transcode.go: %v", err)
	}
	body := methodBody(t, string(b), "CreateJob")

	attachAt := strings.Index(body, `'pending', 'running'`)
	if attachAt < 0 {
		t.Fatal("CreateJob 缺少 auto 分支的 attach 查询（pending/running 复用语义）")
	}
	profileAt := strings.Index(body, "ProfileForSource(")
	if profileAt < 0 {
		t.Fatal("CreateJob 缺少 auto 分支的服务端选档（ProfileForSource）")
	}
	enqueueAt := strings.Index(body, "enqueueOne(")
	if enqueueAt < 0 {
		t.Fatal("CreateJob 缺少 enqueueOne 调用（流程被改？）")
	}
	if !(attachAt < profileAt && profileAt < enqueueAt) {
		t.Fatal("次序必须是 attach 查询 → ProfileForSource 选档 → enqueueOne")
	}
	if !strings.Contains(body, `"reused"`) {
		t.Fatal("attach 分支响应缺少 reused 标记")
	}
	// auto 分支不得绕过总闸门：闸门查询仍须先于 attach。
	gateAt := strings.Index(body, "GetSystemAutoTranscode(")
	if gateAt < 0 || gateAt > attachAt {
		t.Fatal("总闸门必须存在于 auto 分支之前（auto 不得绕过 409 闸门）")
	}
}

func TestPutConfigPartialUpdateShape(t *testing.T) {
	b, err := os.ReadFile("transcode.go")
	if err != nil {
		t.Fatalf("读不到 transcode.go: %v", err)
	}
	body := methodBody(t, string(b), "PutConfig")

	// 部分更新三要件：map 先取一层、字段独立解指针、全缺 400。
	// Job000125 更新：字段从两个扩到五个；「全缺 400」由 PutSystemConfigFields 的
	// ErrValidation 哨兵承担（Handler isValidationErr 映射），守卫断言随形状同步。
	if !strings.Contains(body, "map[string]json.RawMessage") {
		t.Fatal("PutConfig 需用 map[string]json.RawMessage 先取一层（缺失不改语义）")
	}
	for _, key := range []string{`raw["auto_transcode"]`, `raw["realtime_transcode"]`} {
		if !strings.Contains(body, key) {
			t.Fatalf("PutConfig 缺少字段解析：%s", key)
		}
	}
	for _, key := range []string{`raw["hls_seg_seconds"]`, `raw["hls_cache_profile"]`, `raw["stream_base_url"]`} {
		if !strings.Contains(body, key) {
			t.Fatalf("PutConfig 缺少 Job000125 字段解析：%s", key)
		}
	}
	if !strings.Contains(body, "isValidationErr(err)") {
		t.Fatal("PutConfig 缺少 isValidationErr 分类（校验错 400 与库错 500 的分界）")
	}
	putAt := strings.Index(body, "PutSystemConfigFields(")
	if putAt < 0 {
		t.Fatal("PutConfig 缺少 PutSystemConfigFields 调用")
	}
}
