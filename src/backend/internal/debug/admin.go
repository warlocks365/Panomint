package debug

// 管理端点四件（设计 §10.2，全部 RequirePerm("admin:system") 由路由层把关）：
//
//	GET  /admin/debug/status   → 通道状态（无明钥）
//	POST /admin/debug/enable   → 开启并签发凭据（201，key 唯一出口）
//	POST /admin/debug/rotate   → 重置密钥（踢旧连 4004）
//	POST /admin/debug/disable  → 关闭（幂等 204，踢连 4003）
//
// 审计（设计 §7.1）：enable/disable(manual)/rotate 在此写；expired 由 reaper/握书写；
// agent 事件在 ws.go。detail 键名全部走 §7.3 白名单（channel_fp/ttl_hours/expires_at/reason）。

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/audit"
	"panoalbum/internal/httperr"
	"panoalbum/internal/queue"
)

// Handler 调试通道 HTTP 端点（管理面 + 握手面共用装配）。
type Handler struct {
	Pool   *pgxpool.Pool // 命令面查库（活动任务/任务档案）
	Store  *Store
	Audit  *audit.Recorder // 可为 nil（测试跳过）
	Hub    *Hub            // 进程内连接槽（ws.go）
	Locker *FailLocker     // 失败锁定（ratelimit.go）
	RL     *queue.RateLimiter // 握手专用桶（ratelimit.go）
	TransQ *queue.Queue   // 转码队列（命令面绑定用）

	// AllowInsecureWS = DEBUG_ALLOW_INSECURE_WS（默认关；仅开发，且仍限 127.0.0.1）。
	AllowInsecureWS bool
}

// recordAdmin 管理动作审计（actor=管理员，来自 gin 上下文）。
func (h *Handler) recordAdmin(c *gin.Context, action string, detail map[string]any) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = action
	e.TargetType = audit.TargetDebugChannel
	e.TargetID = strOr(detail["channel_fp"], "-")
	e.Detail = detail
	h.Audit.Record(c.Request.Context(), e)
}

func strOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// wsURL 拼出接入 URL（wss://host/debug/channel/<id>，设计 §3.3）。
// 恒 wss：调试通道强制 TLS（明文 ws 握手被 426 DEBUG_TLS_REQUIRED 拒绝），
// 若按请求 scheme 回退拼 ws:// 会给出一根必死链（e2e D 段实证：直连 8088 拿到
// ws:// URL 根本不可用）——展示给用户的凭据只给可用形态。
func wsURL(c *gin.Context, channelID string) string {
	return "wss://" + c.Request.Host + "/debug/channel/" + channelID
}

// Status GET /admin/debug/status（设计 §10.2 首行）。
func (h *Handler) Status(c *gin.Context) {
	ch, err := h.Store.Status(c.Request.Context())
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	resp := gin.H{"enabled": false, "connected": false}
	if ch != nil && ch.Enabled {
		resp["enabled"] = true
		resp["connected"] = h.Hub.Connected()
		resp["channel_id"] = ch.ChannelID
		resp["created_at"] = ch.CreatedAt.UTC().Format(time.RFC3339)
		resp["expires_at"] = ch.ExpiresAt.UTC().Format(time.RFC3339)
		if ch.LastConnectAt != nil {
			resp["last_connect_at"] = ch.LastConnectAt.UTC().Format(time.RFC3339)
		}
		if ch.LastConnectIP != "" {
			resp["last_connect_ip"] = ch.LastConnectIP
		}
	}
	c.JSON(http.StatusOK, resp)
}

// Enable POST /admin/debug/enable {ttl_hours?} → 201 {url, key, expires_at}。
// 已开启 → 409 DEBUG_ALREADY_ENABLED（不轮换正在使用的密钥，设计 §8.1）。
func (h *Handler) Enable(c *gin.Context) {
	var req struct {
		TTLHours *int `json:"ttl_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 JSON 对象")
		return
	}
	ttl := DefaultTTLHours
	if req.TTLHours != nil {
		if !ValidTTL(*req.TTLHours) {
			httperr.Envelope(c, http.StatusBadRequest, "BAD_TTL", "ttl_hours 需为 1|8|24|72")
			return
		}
		ttl = *req.TTLHours
	}

	channelID, plainKey, expiresAt, err := h.Store.Enable(c.Request.Context(), c.GetString("user_id"), ttl)
	if err != nil {
		switch {
		case errors.Is(err, ErrAlreadyEnabled):
			httperr.Envelope(c, http.StatusConflict, "DEBUG_ALREADY_ENABLED", "调试通道已开启，如需换密钥请用重置")
		case errors.Is(err, ErrBadTTL):
			httperr.Envelope(c, http.StatusBadRequest, "BAD_TTL", "ttl_hours 需为 1|8|24|72")
		default:
			httperr.Fail(c, http.StatusInternalServerError, "ENABLE_FAILED", "开启失败", err)
		}
		return
	}
	h.recordAdmin(c, audit.ActionDebugEnable, map[string]any{
		"ttl_hours":  ttl,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"channel_fp": Fingerprint(channelID),
	})
	c.JSON(http.StatusCreated, gin.H{
		"url":        wsURL(c, channelID),
		"key":        plainKey,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

// Rotate POST /admin/debug/rotate → 200 {url, key, expires_at}。
// 未开启 → 409；踢旧连 4004（设计 §6.3）。TTL：请求体可带 ttl_hours，缺省沿用原档位。
func (h *Handler) Rotate(c *gin.Context) {
	var req struct {
		TTLHours *int `json:"ttl_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 JSON 对象")
		return
	}
	ttl := DefaultTTLHours
	if req.TTLHours != nil {
		if !ValidTTL(*req.TTLHours) {
			httperr.Envelope(c, http.StatusBadRequest, "BAD_TTL", "ttl_hours 需为 1|8|24|72")
			return
		}
		ttl = *req.TTLHours
	} else if ch, err := h.Store.Status(c.Request.Context()); err == nil && ch != nil && ch.Enabled {
		// 沿用原档位（created_at→expires_at 跨度）；异常值回落默认（不可能出现，保险丝）。
		if span := int(ch.ExpiresAt.Sub(ch.CreatedAt).Hours()); ValidTTL(span) {
			ttl = span
		}
	}

	channelID, plainKey, expiresAt, oldFP, err := h.Store.Rotate(c.Request.Context(), c.GetString("user_id"), ttl)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotEnabled):
			httperr.Envelope(c, http.StatusConflict, "DEBUG_NOT_ENABLED", "调试通道未开启")
		case errors.Is(err, ErrBadTTL):
			httperr.Envelope(c, http.StatusBadRequest, "BAD_TTL", "ttl_hours 需为 1|8|24|72")
		default:
			httperr.Fail(c, http.StatusInternalServerError, "ROTATE_FAILED", "重置失败", err)
		}
		return
	}
	h.Hub.Kick(CloseKeyRotated, "密钥已轮换")
	h.recordAdmin(c, audit.ActionDebugRotate, map[string]any{
		"old_channel_fp": oldFP,
		"new_channel_fp": Fingerprint(channelID),
	})
	c.JSON(http.StatusOK, gin.H{
		"url":        wsURL(c, channelID),
		"key":        plainKey,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

// Disable POST /admin/debug/disable → 204（幂等；未开启也 204，设计 §6.4）。
func (h *Handler) Disable(c *gin.Context) {
	wasEnabled, fp, err := h.Store.Disable(c.Request.Context())
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DISABLE_FAILED", "关闭失败", err)
		return
	}
	if wasEnabled {
		h.Hub.Kick(CloseManualOff, "调试通道已关闭")
		h.recordAdmin(c, audit.ActionDebugDisable, map[string]any{
			"channel_fp": fp,
			"reason":     "manual",
		})
	}
	c.Status(http.StatusNoContent)
}
