package shares

// 公开分享链路（匿名可达）的 media 归属判定回归网。
//
// 背景（2026-09-18 真机复现）：某条 album_items 脏行（他人 media 被塞进相册）虽已被
// ListItems/Get 的可见性过滤挡在列表之外，匿名 GET /public/shares/:token/media/:id/thumb
// 仍返回 200 + 40716 字节 —— 因为 MediaInShare 的 EXISTS 只判 album_items + deleted_at。
// 即"列表看不到，但按 media id 仍能换到字节"的侧门。

import (
	"os"
	"strings"
	"testing"
)

// TestMediaInShareScopedToAlbumOwner 缩略图/HLS 的归属判定必须按**相册属主**过滤。
func TestMediaInShareScopedToAlbumOwner(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	if !strings.Contains(src, `mediascope.VisibleCondFor(3, albumOwner, "m")`) {
		t.Fatal("MediaInShare 的 EXISTS 未按相册属主收窄可见集（或谓词占位符起点不是 $3）—— " +
			"历史脏行会留下「列表看不到、仍能按 id 取字节」的匿名侧门")
	}
	if !strings.Contains(src, "SELECT type, owner_id FROM albums") {
		t.Fatal("MediaInShare 必须同时取相册属主 owner_id 作为谓词主体")
	}
	// 匿名分享没有调用者身份；若这里出现 gin 上下文/调用者取值，说明主体被换错了。
	if strings.Contains(src, "gin.Context") || strings.Contains(src, "c.GetString(") {
		t.Fatal("Store 层不得取调用者身份：匿名分享链路没有主体，改用它会把分享整条 fail-closed 打死")
	}
}
