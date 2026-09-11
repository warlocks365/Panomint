package geo

// 地图模式 HTTP 层（Job000009）：聚合簇 / 条目列表 / 时间直方图。
//
// 端点（均需 media:read 权限，见 cmd/api/main.go 装配）：
//   GET /geo/clusters  bbox+zoom 网格聚合（时间轴 → 地图）
//   GET /geo/items     bbox 内媒体条目（点击簇展开）
//   GET /geo/histogram bbox 内时间分布（地图 → 时间轴）

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Handler 地图模式 HTTP 处理器。
type Handler struct {
	Media  *MediaStore
	Tiles  *AmapTileProxy
	// Provider 默认坐标系输出（amap=GCJ-02，osm/其他=WGS-84）。
	Provider string
}

// defaultMapIcon 默认图标（红色圆形，对齐需求"默认红点"）。
func defaultMapIcon() *MapIconPref {
	return &MapIconPref{Shape: "circle", Color: "#ef4444"}
}

// validShape 合法图标形状集合。
func validShape(s string) bool {
	switch s {
	case "circle", "triangle", "diamond", "star", "pin", "inverted", "custom":
		return true
	}
	return false
}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// validColor 校验十六进制颜色。
func validColor(c string) bool {
	return hexColorRe.MatchString(c)
}

func (h *Handler) provider(c *gin.Context) string {
	if v := c.Query("provider"); v != "" {
		return v
	}
	if h.Provider != "" {
		return h.Provider
	}
	return "amap"
}

// parseBBox 解析并校验 bbox 参数（min_lng,min_lat,max_lng,max_lat）。
func parseBBox(c *gin.Context) (BBox, bool) {
	f := func(k string) (float64, bool) {
		v, err := strconv.ParseFloat(c.Query(k), 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	minLng, ok1 := f("min_lng")
	minLat, ok2 := f("min_lat")
	maxLng, ok3 := f("max_lng")
	maxLat, ok4 := f("max_lat")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": "bbox 参数缺失或非法（min_lng/min_lat/max_lng/max_lat）"}})
		return BBox{}, false
	}
	b := BBox{MinLng: minLng, MinLat: minLat, MaxLng: maxLng, MaxLat: maxLat}
	if !b.Valid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": "bbox 越界或顺序非法"}})
		return BBox{}, false
	}
	return b, true
}

// bboxFor 解析 bbox 并按 provider 还原为库内坐标系（WGS-84）。
// 前端 map.getBounds() 给的是地图显示坐标系（高德底图 = GCJ-02），需反变换后才能查库。
func (h *Handler) bboxFor(c *gin.Context) (BBox, bool) {
	b, ok := parseBBox(c)
	if !ok {
		return BBox{}, false
	}
	if h.provider(c) == "amap" {
		return BBoxFromGCJ02(b), true
	}
	return b, true
}

// Clusters GET /geo/clusters?min_lng=&min_lat=&max_lng=&max_lat=&zoom=&provider=&from=&to=
func (h *Handler) Clusters(c *gin.Context) {
	b, ok := h.bboxFor(c)
	if !ok {
		return
	}
	zoom, _ := strconv.Atoi(c.DefaultQuery("zoom", "4"))
	if zoom < 0 || zoom > 22 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": "zoom 需在 0~22"}})
		return
	}
	from, err := ParseTime(c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	to, err := ParseTime(c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}

	clusters, err := h.Media.Clusters(c.Request.Context(), b, zoom, h.provider(c), from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"clusters": clusters})
}

// Items GET /geo/items?min_lng=&...&from=&to=&limit=
// 返回 bbox 内媒体条目（含缩略图所需 id），供点击簇/框选后展开网格。
func (h *Handler) Items(c *gin.Context) {
	b, ok := h.bboxFor(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	from, err := ParseTime(c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	to, err := ParseTime(c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}

	items, err := h.Media.Items(c.Request.Context(), b, h.provider(c), from, to, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// Histogram GET /geo/histogram?min_lng=&...&granularity=year|month|day
// 地图 viewport 变化 → 重算时间轴分布（双向联动的"地图→时间轴"方向）。
// Job000009：每桶返回四类媒体分类计数。
func (h *Handler) Histogram(c *gin.Context) {
	b, ok := h.bboxFor(c)
	if !ok {
		return
	}
	granularity := c.DefaultQuery("granularity", "month")
	buckets, err := h.Media.Histogram(c.Request.Context(), b, granularity)
	if err != nil {
		code := "QUERY_FAILED"
		status := http.StatusInternalServerError
		if err == ErrInvalidGranularity {
			code = "INVALID_PARAMS"
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"buckets": buckets})
}

// GetMapIconPref GET /preferences/map 读取当前用户地图图标偏好。
// 无记录返回 200 + 默认值（前端据此用默认红点，无需额外处理 404）。
func (h *Handler) GetMapIconPref(c *gin.Context) {
	userID := c.GetString("user_id")
	p, err := h.Media.GetMapIcon(c.Request.Context(), userID)
	if err == ErrPrefNotFound {
		c.JSON(http.StatusOK, gin.H{"pref": defaultMapIcon()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pref": p})
}

// PutMapIconPref PUT /preferences/map 写入当前用户地图图标偏好。
func (h *Handler) PutMapIconPref(c *gin.Context) {
	userID := c.GetString("user_id")
	var p MapIconPref
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	// 基础校验：shape 必须为已知值，颜色必须为合法十六进制色值
	if !validShape(p.Shape) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": "shape 非法"}})
		return
	}
	if !validColor(p.Color) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": "color 非法"}})
		return
	}
	if err := h.Media.PutMapIcon(c.Request.Context(), userID, &p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pref": &p})
}
