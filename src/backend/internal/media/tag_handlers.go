package media

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 手工标签 HTTP 处理器（Job000005）。读 media:read；写 media:write + 媒体归属校验。

// ListTags GET /tags?q=（自动补全：子串过滤 + 使用计数）
func (h *Handler) ListTags(c *gin.Context) {
	tags, err := h.Store.ListTags(c.Request.Context(), c.Query("q"))
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// AddTag POST /media/:id/tags {name}（幂等：不存在则以 kind=user 创建，已关联不报错）
func (h *Handler) AddTag(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"name\": \"标签名\"}")
		return
	}
	name, err := NormalizeTagName(req.Name)
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	tag, err := h.Store.FindOrCreateUserTag(c.Request.Context(), name)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "TAG_CREATE_FAILED", err.Error())
		return
	}
	if err := h.Store.AttachTag(c.Request.Context(), id, tag.ID); err != nil {
		errResp(c, http.StatusInternalServerError, "TAG_ATTACH_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "tag": tag})
}

// RemoveTag DELETE /media/:id/tags/:tag_id（解除关联；未关联按幂等 200 处理，标签不存在 404）
func (h *Handler) RemoveTag(c *gin.Context) {
	id := c.Param("id")
	tagID := c.Param("tag_id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	exists, err := h.Store.tagExists(c.Request.Context(), tagID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	if !exists {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
		return
	}
	removed, err := h.Store.DetachTag(c.Request.Context(), id, tagID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "tag_id": tagID, "removed": removed})
}
