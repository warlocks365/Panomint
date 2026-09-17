package audit

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// GET /media/restore-history —— 用户侧只读端点：**调用者自己**执行过的「从回收站恢复」历史。
//
// # 它存在的理由
//
// 工具箱页（`/toolbox`）的「已恢复」标签此前只能是引导区，因为「谁在什么时候恢复了什么」
// 在库里没有任何记录（`media.delete` / `media.purge` 已审计，恢复没有）。`media.restore`
// 补上后数据有了，但读它的唯一出口 `GET /admin/audit` 挂在 `admin:users` 之下
// ⇒ **普通成员看不到自己的恢复历史**。本端点补的就是这半。
//
// # 为什么它落在 audit 包，而不是 media 包
//
// 它读的是 `audit_log`，复用的是本包既有的 `Filter` / `Page` / 游标编解码 / limit 钳制。
// 若搬到 media 包，就得把这套约定**再实现一遍**，而「同一份约定抄两处 + 注释承诺同步」
// 正是 §二十一 那个必然漂移的失效模式（列清单曾 7 份副本、5 份各漂各的）。
// 本包已有同型先例：`GET /admin/stats`、`GET /admin/jobs` 也是 audit 包实现、
// 挂在**别人的路径前缀**之下 —— 路径命名空间与包边界本来就不必一一对应。
//
// # 为什么读它**不写** ActionAuditRead（与 ListAudit 刻意相反）
//
// `ListAudit` 自记录（`recordAuditRead`），立论前提是：`/admin/audit` 暴露**全站**
// 敏感操作史，读它本身就是一次有后果的敏感访问。
// 本端点只返回**调用者自己**的行 —— 那些行本就是他自己做的动作，**读它不产生任何新的
// 知情面**。前提不同，故不照搬先例；且每开一次工具箱就写一条审计，只会把账本淹没
// （§二十四.1 明确要避免「噪音掩盖真线索」）。
// **这条决定有测试钉住**：TestRestoreHistoryDoesNotWriteAuditRead。若将来有人"顺手"
// 加上自记录，那个测试会红，逼他先来改这条注释说明理由。
//
// # 两条已知限制（已写进契约，不是疏漏）
//
//  1. `detail` 里只有 `path` 与 `owner_id`，**没有 filename**；且该媒体若此后被 purge，
//     `target_id` 已无法解析（行被整行删除）。⇒ 本端点**按 path 展示、不承诺稳定文件名**。
//     要稳定文件名就得另存一份恢复记录表 —— 那是升级路径，不在本次范围。
//  2. 行是按 **actor** 存的 ⇒ 只含本人**执行**的恢复。管理员代某成员恢复时，那行的
//     `user_id` 是**管理员**，故该成员在这里**看不到**这一条。
//     这是**可见性缺口，不是越权泄漏**（过滤 `user_id = 调用者` 不会多返回任何一行）——
//     但不写进契约的话，这个列表会**看起来完整而其实不然**。
func (h *Handler) RestoreHistory(c *gin.Context) {
	// ⚠️ actor **只**取自认证上下文，绝不接受查询参数（`?actor=`）覆盖 ——
	// 否则任何人都能读别人的恢复历史。
	//
	// 而"非空"这一步是**必需的安全检查，不是防御性冗余**：
	// BuildWhere 对空 ActorUserID 会**跳过** user_id 条件，这次查询就退化成
	// 「返回全站所有人的恢复历史」。只要认证中间件有一次没注入 user_id，
	// 一个小小的疏忽就变成一次越权。故此处宁可 401。
	actor, err := NormalizeActor(c.GetString("user_id"))
	if err != nil || actor == "" {
		fail(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return
	}

	f := Filter{Action: ActionMediaRestore, ActorUserID: actor}
	if v := strings.TrimSpace(c.Query("cursor")); v != "" {
		at, id, err := decodeCursor(v)
		if err != nil {
			fail(c, http.StatusBadRequest, CodeInvalidInput, "无效游标")
			return
		}
		f.CursorAt, f.CursorID = &at, &id
	}
	// limit 钳制语义与 /admin/audit 完全一致（超上限夹到上限，而不是回落默认值）：
	// 同一套约定只应有一处实现，故直接复用 clampAuditLimit。
	f.Limit = clampAuditLimit(atoiDefault(c.Query("limit"), defaultAuditLimit))

	page, err := h.Store.Query(c.Request.Context(), f)
	if err != nil {
		log.Printf("audit: 查询恢复历史失败: %v", err)
		fail(c, http.StatusInternalServerError, CodeInternal, "查询恢复历史失败")
		return
	}
	// 刻意**不**调 h.Recorder（见文件头注释）。
	c.JSON(http.StatusOK, page)
}
