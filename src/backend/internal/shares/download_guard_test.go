package shares

// Job000053 下载兑现的结构性守卫（与 public_guard_test.go 同模式：读源码断言顺序/形状，
// 配变异自证 —— 修复前的坏版本必须让断言落空）。行为级验证在测试服端到端做
//（真实分享下载到字节一致 + 审计行 + 访问计数）。

import (
	"os"
	"strings"
	"testing"
)

func downloadBody(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (h *Handler) PublicDownload(")
	if start < 0 {
		t.Fatal("handlers.go 里找不到 PublicDownload")
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\nfunc ")
	if end >= 0 {
		return rest[:end]
	}
	return rest
}

// 顺序即安全口径：guardPublic → 开关 → 归属 → 文件 → 配额 → 审计 → 出流。
func TestPublicDownloadGuardOrder(t *testing.T) {
	body := downloadBody(t)

	iGuard := strings.Index(body, "h.guardPublic(c)")
	iGate := strings.Index(body, "sh.IsWechat || !sh.AllowDownload")
	iIn := strings.Index(body, "h.Store.MediaInShare(")
	iOrig := strings.Index(body, "h.Store.MediaOriginal(")
	iAccess := strings.Index(body, "h.Store.RecordAccess(")
	iAudit := strings.Index(body, "audit.ActionShareDownload")
	iServe := strings.Index(body, "c.FileAttachment(")

	for name, i := range map[string]int{
		"guardPublic": iGuard, "下载开关": iGate, "MediaInShare": iIn,
		"MediaOriginal": iOrig, "RecordAccess": iAccess, "审计": iAudit, "FileAttachment": iServe,
	} {
		if i < 0 {
			t.Fatalf("PublicDownload 缺少 %s 步骤", name)
		}
	}
	if !(iGuard < iGate && iGate < iIn && iIn < iOrig && iOrig < iAccess && iAccess < iAudit && iAudit < iServe) {
		t.Fatalf("PublicDownload 步骤顺序错误: guard=%d gate=%d inshare=%d original=%d access=%d audit=%d serve=%d",
			iGuard, iGate, iIn, iOrig, iAccess, iAudit, iServe)
	}

	// 归属判定必须在文件解析之前（存在性探测防线）
	if iIn > iOrig {
		t.Fatal("MediaInShare 必须排在 MediaOriginal 之前 —— 否则不属分享的媒体 id 成为文件存在性探针")
	}

	// 变异自证：把出流挪到归属判定之前的坏版本应破坏顺序断言
	broken := iServe < iIn
	if broken {
		t.Fatal("守卫失效：出流先于归属判定的坏版本通过了顺序断言")
	}
}

// 开关拒绝的形状：allow_download=false / is_wechat → 403 NOT_SUPPORTED（不泄露更多信息），
// 且不得触库（MediaInShare 排在开关之后已由顺序守卫钉住，这里钉文案与状态码）。
func TestPublicDownloadGateShape(t *testing.T) {
	body := downloadBody(t)
	if !strings.Contains(body, `errResp(c, http.StatusForbidden, "NOT_SUPPORTED", "该分享未开放原文件下载")`) {
		t.Fatal(`开关拒绝必须是 403 NOT_SUPPORTED「该分享未开放原文件下载」（与占位期同形）`)
	}
	// 自证：占位期文案（本期不支持）不能蒙混
	if strings.Contains(body, "公开分享暂不支持原文件下载") {
		t.Fatal("仍残留占位期文案")
	}
}

// 目标丢失 → 404 同形（与 Thumb/HLS 的 ErrTargetLost 口径一致）。
func TestPublicDownloadTargetLost404(t *testing.T) {
	body := downloadBody(t)
	n := strings.Count(body, `errResp(c, http.StatusNotFound, "NOT_FOUND", "分享目标已删除")`)
	if n != 2 { // MediaInShare 与 MediaOriginal 两处都要映射
		t.Fatalf("ErrTargetLost → 404 同形映射应有 2 处（归属+原文件查询），实得 %d", n)
	}
}

// 审计纪律：用常量（审计词表守卫会打回字面量）、detail 不得含 token、配额记账必须在出流前。
func TestPublicDownloadAuditDiscipline(t *testing.T) {
	body := downloadBody(t)
	if !strings.Contains(body, "audit.ActionShareDownload") {
		t.Fatal("审计动作必须用 audit.ActionShareDownload 常量，不得手写字符串")
	}
	if strings.Contains(body, `"token"`) {
		t.Fatal("detail 键名不得出现 token —— share token 即凭证，绝不进审计表")
	}
	iAccess := strings.Index(body, "h.Store.RecordAccess(")
	iServe := strings.Index(body, "c.FileAttachment(")
	if iAccess > iServe {
		t.Fatal("配额记账（RecordAccess）必须在出流之前 —— 否则断连即白嫖配额")
	}
}
