package transcode

// Job000120「自动 HLS 转码开关」的源码形状守卫（配套 geo 侧 Normalize/COALESCE 单测）。
//
// 能证明什么：CreateJob 的用户级闸门在源码里真实存在、位置正确——
//   - 闸门查询（user_ui_prefs.auto_transcode）出现在 enqueueOne（写行+入队）**之前**：
//     关闭转码的用户发起请求时，绝不留任务行、绝不入队；
//   - 拒绝分支是 409 TRANSCODE_DISABLED，且同样排在 enqueueOne 之前；
//   - 缺行按开启处理（ErrNoRows 不进错误分支）——老账号行为不变。
//
// 不能证明什么：真实 HTTP 往返下「关 → 拒绝 / 开 → 正常入队」——本仓库测试环境无数据库，
// 归属查询（更早的一步）就已需要库；该端到端由部署后 verify 脚本覆盖。

import (
	"os"
	"strings"
	"testing"
)

func TestCreateJobGatedByAutoTranscodePref(t *testing.T) {
	b, err := os.ReadFile("transcode.go")
	if err != nil {
		t.Fatalf("读不到 transcode.go: %v", err)
	}
	body := methodBody(t, string(b), "CreateJob")

	gateAt := strings.Index(body, "auto_transcode FROM user_ui_prefs")
	if gateAt < 0 {
		t.Fatal("CreateJob 缺少 user_ui_prefs.auto_transcode 闸门查询")
	}
	enqueueAt := strings.Index(body, "enqueueOne(")
	if enqueueAt < 0 {
		t.Fatal("CreateJob 缺少 enqueueOne 调用（流程被改？）")
	}
	if gateAt > enqueueAt {
		t.Fatal("闸门必须排在 enqueueOne 之前（否则关了转码仍会写任务行/入队）")
	}

	denyAt := strings.Index(body, `errJSON(c, http.StatusConflict, "TRANSCODE_DISABLED"`)
	if denyAt < 0 {
		t.Fatal("缺少 409 TRANSCODE_DISABLED 拒绝分支")
	}
	if denyAt > enqueueAt {
		t.Fatal("拒绝分支必须排在 enqueueOne 之前")
	}

	// 缺行 = 默认开启：闸门段（查询→拒绝分支之间）必须把 ErrNoRows 排除在错误分支外。
	// 注意 QUERY_FAILED 在方法里出现多次（媒体归属查询在前），不能拿它当锚点。
	gateSeg := body[gateAt:enqueueAt]
	if !strings.Contains(gateSeg, "!errors.Is(err, pgx.ErrNoRows)") {
		t.Fatal("闸门查询失败分支必须把 pgx.ErrNoRows 排除在外（缺行=开启）")
	}
}
