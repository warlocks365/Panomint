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
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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
func (h *Handler) ListPeople(c *gin.Context) {
	named, unnamed, err := h.Store.ListPeople(c.Request.Context())
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

// PersonMedia GET /people/:id/media?limit= → { person_id, total, items:[...] }
func (h *Handler) PersonMedia(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := h.Store.PersonMedia(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"person_id": c.Param("id"), "total": len(items), "items": items})
}

// TriggerScan POST /ai/faces ← { scope:"all" | "<media_id>" }
//
// 契约要求返回 { job_id }；本实现是"复位扫描标记 + 由 facesgen 清扫循环接手"，
// job_id 仅用于前端提示，不落任务表（详见 Store.ResetScanned 注释）。
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
