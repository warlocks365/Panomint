package transcode

// Job000120-r2「自动 HLS 转码开关（系统级）」的源码形状守卫（配套 sysconfig_test.go）。
//
// 能证明什么：CreateJob 的系统级闸门在源码里真实存在、位置正确——
//   - 闸门（GetSystemAutoTranscode）出现在 enqueueOne（写行+入队）**之前**：
//     关闭转码时，绝不留任务行、绝不入队；
//   - 拒绝分支是 409 TRANSCODE_DISABLED，且同样排在 enqueueOne 之前；
//   - 闸门查询失败 fail-closed 500——拒绝服务也不在无把握时放行转码。
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

	gateAt := strings.Index(body, "GetSystemAutoTranscode(")
	if gateAt < 0 {
		t.Fatal("CreateJob 缺少系统级转码闸门（GetSystemAutoTranscode）")
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

	// 闸门查询失败必须 fail-closed：闸门段（查询→拒绝分支之间）存在 500 出口。
	// 「查询失败」文案在方法里出现多次（媒体归属查询在前），锚 httperr.Fail 形态即可。
	gateSeg := body[gateAt:denyAt]
	if !strings.Contains(gateSeg, "httperr.Fail(c, http.StatusInternalServerError") {
		t.Fatal("闸门查询失败必须 fail-closed 500（拒绝服务也不在无把握时放行转码）")
	}
}
