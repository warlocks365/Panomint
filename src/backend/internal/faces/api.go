package faces

// HTTP 端点（API 契约 v1.1 §6 人物 + §12 POST /ai/faces）。
//
// 只依赖 Store（纯 SQL，无 CGO），因此 CGO_ENABLED=0 构建下也能提供人物页读接口；
// 仅"扫描"需要 CGO（见 detect.go / embed.go 的构建标签）。
//
// 装配（cmd/api/main.go，属主为集成方）：
//
//	facesH := &faces.Handler{Store: &faces.Store{Pool: pool}}
//	authed.GET("/people", permRead, facesH.ListPeople)
//	authed.POST("/people", permWrite, facesH.CreatePerson)
//	authed.PATCH("/people/:id", permWrite, facesH.PatchPerson)
//	authed.GET("/people/:id/media", permRead, facesH.PersonMedia)
//	authed.POST("/ai/faces", permWrite, facesH.TriggerScan)

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/pgxutil"
)

// Handler 人物 / 人脸相关端点。Store 不可为空。
type Handler struct {
	Store *Store
}

// fail 统一错误响应 {"error":{"code","message"}}，与 media 包保持一致。
func fail(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}

// ListPeople GET /people → { named:[...], unnamed:[{cluster_id,count,cover}] }
//
// 调用者身份取自上下文 user_id（鉴权中间件注入），用于把**暴露 media 内容的字段**
// （face_count / cover_media_id / count / cover）收窄到调用者可见的媒体集合。
// 身份缺失时不放宽、而是恒假收窄（见 Store.ListPeople）。
func (h *Handler) ListPeople(c *gin.Context) {
	named, unnamed, err := h.Store.ListPeople(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		fail(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"named": named, "unnamed": unnamed})
}

// CreatePerson POST /people ← { cluster_ids:[...], name, is_pet }
func (h *Handler) CreatePerson(c *gin.Context) {
	var req struct {
		ClusterIDs []string `json:"cluster_ids"`
		Name       string   `json:"name"`
		IsPet      bool     `json:"is_pet"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "INVALID_PARAMS", "请求体格式错误")
		return
	}
	p, err := h.Store.CreatePerson(c.Request.Context(), req.Name, req.IsPet, req.ClusterIDs)
	if errors.Is(err, ErrEmptyName) {
		fail(c, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusCreated, p)
}

// PatchPerson PATCH /people/:id ← { name } 或 { hidden }
func (h *Handler) PatchPerson(c *gin.Context) {
	var req struct {
		Name   *string `json:"name"`
		Hidden *bool   `json:"hidden"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "INVALID_PARAMS", "请求体格式错误")
		return
	}
	p, err := h.Store.UpdatePerson(c.Request.Context(), c.Param("id"), req.Name, req.Hidden)
	switch {
	case errors.Is(err, ErrPersonNotFound):
		fail(c, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	case errors.Is(err, ErrEmptyName), errors.Is(err, ErrNothingToUpdate):
		fail(c, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
		return
	case err != nil:
		fail(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, p)
}

// writePersonMediaError 把 GET /people/:id/media 的查询失败映射为响应。
//
// 人物 id 是 UUID 列（faces.person_id → people.id），畸形 id 会让 PG 抛 22P02。
// 与「人物不存在」同形 404（同一 code 与 message，文案取自 ErrPersonNotFound）：
// 对调用方而言"id 语法非法"与"没有这个人物"是同一件事；若回 500，既伪造服务故障，
// 又会把 `invalid input syntax for type uuid` / SQLSTATE 这类内部细节吐出去。
//
// 其余 DB 故障仍 500，但 message 固定为「查询失败」——完整错误只进服务端日志。
func writePersonMediaError(c *gin.Context, err error) {
	if pgxutil.IsMalformedID(err) {
		fail(c, http.StatusNotFound, "NOT_FOUND", ErrPersonNotFound.Error())
		return
	}
	log.Printf("[faces] 人物媒体查询失败: %v", err)
	fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
}

// PersonMedia GET /people/:id/media?limit= → { person_id, total, items:[...] }
//
// 调用者身份取自上下文 user_id：只返回该调用者可见的媒体（谓词来自
// internal/mediascope 单一真源）。此前本端点没有任何属主条件，任一带 media:read
// 的账号都能拿到他人的媒体 ID / 文件名（已实测复现）。
func (h *Handler) PersonMedia(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := h.Store.PersonMedia(c.Request.Context(), c.Param("id"), c.GetString("user_id"), limit)
	if err != nil {
		writePersonMediaError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"person_id": c.Param("id"), "total": len(items), "items": items})
}

// TriggerScan POST /ai/faces ← { scope:"all" | "<media_id>" }
//
// 契约要求返回 { job_id }；本实现是"复位扫描标记 + 由 facesgen 清扫循环接手"，
// job_id 仅用于前端提示，不落任务表（详见 Store.ResetScanned 注释）。
//
// 响应字段说明（契约只承诺 job_id，scope/queued 是本实现的附加信息）：
//
//	job_id  前端提示用的伪任务号（NewClusterID 生成，无对应任务表记录）
//	scope   回显请求范围
//	queued  **被复位扫描标记的媒体条数**——不是任务数、也不是人脸数
//	        （实测 = 有 LG 缩略图且未删除的媒体数，当前语料为 93）
//
// ⚠️ `queued` 这个名字容易读成"入队任务数"，语义不够自明；但既有验证脚本
// （.workbuddy/face-corpus/verify_people_api.sh:113 期望 queued=可扫描媒体数）
// 已按此含义断言，且契约 §12 只承诺 job_id，故本轮不改键名以免破坏兼容，
// 在此以注释固化其定义。若日后要改名，建议 `media_reset`。
func (h *Handler) TriggerScan(c *gin.Context) {
	var req struct {
		Scope string `json:"scope"`
	}
	_ = c.ShouldBindJSON(&req) // scope 可省略，缺省 all
	if req.Scope == "" {
		req.Scope = "all"
	}
	n, err := h.Store.ResetScanned(c.Request.Context(), req.Scope)
	if err != nil {
		fail(c, http.StatusInternalServerError, "RESET_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"job_id": NewClusterID(), "scope": req.Scope, "queued": n})
}
