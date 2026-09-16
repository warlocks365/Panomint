package geo

// 地图模式 HTTP 层（Job000009）：聚合簇 / 条目列表 / 时间直方图。
//
// 端点（均需 media:read 权限，见 cmd/api/main.go 装配）：
//   GET /geo/clusters  bbox+zoom 网格聚合（时间轴 → 地图）
//   GET /geo/items     bbox 内媒体条目（点击簇展开）
//   GET /geo/histogram bbox 内时间分布（地图 → 时间轴）

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
)

// Handler 地图模式 HTTP 处理器。
type Handler struct {
	Media *MediaStore
	Tiles *AmapTileProxy
	// Provider 默认坐标系输出（amap=GCJ-02，osm/其他=WGS-84）。
	Provider string

	// MapCfg 系统地图配置读取器（GET/PUT /admin/map-config 用）。
	MapCfg *MapConfigStore
	// AmapKeyEnv 环境变量提供的高德 Key。**只用于判断"是否已配置"，绝不回显**。
	AmapKeyEnv string
	// Audit 审计写入器；可为 nil（测试/灰度时跳过）。仅用于系统配置变更留痕。
	Audit *audit.Recorder
}

// record 写一条审计（尽力而为；Audit 为 nil 时跳过）。
func (h *Handler) record(c *gin.Context, action, targetType, targetID string, detail map[string]any) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = action
	e.TargetType = targetType
	e.TargetID = targetID
	e.Detail = detail
	h.Audit.Record(c.Request.Context(), e)
}

// mapKeyState 归因高德 Key 的可用性与来源（纯函数，便于穷举单测）。
//
// 优先级：数据库里可解密的 Key > 环境变量 AMAP_KEY > 其余（密文不可解 / 读取失败 / 未配置）。
// 之所以要区分来源：运维看到"source=env"就知道要改 .env 并重启，看到"source=db"就知道改这里。
// 数据库有值但**解密失败**时不回落环境变量 —— 那说明配置本身坏了，
// 静默用 env 顶上会让"界面上显示可用、实际走的是另一个 Key"这种问题永远查不出来。
func mapKeyState(envKey, dbKey, dbState string) (string, string) {
	if dbState == ChinaKeyOK && strings.TrimSpace(dbKey) != "" {
		return ChinaKeyOK, "db"
	}
	if dbState == ChinaKeyUndecryptable {
		return ChinaKeyUndecryptable, "db"
	}
	if strings.TrimSpace(envKey) != "" {
		return ChinaKeyOK, "env"
	}
	if dbState == ChinaKeyLoadFailed {
		return ChinaKeyLoadFailed, "none"
	}
	return ChinaKeyAbsent, "none"
}

// keyState 读取当前生效的高德 Key 状态与来源。
func (h *Handler) keyState(ctx context.Context) (string, string) {
	cfg := MapConfig{}
	if h.MapCfg != nil {
		cfg = h.MapCfg.Load(ctx)
	}
	return mapKeyState(h.AmapKeyEnv, cfg.ChinaAPIKey, cfg.ChinaKeyState)
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

// parseKind 解析并校验分类过滤参数（空串 = all）。
// 非法取值一律 400 而不是静默当成 all —— 静默降级会让"筛选没生效"看起来像"这些媒体本来就没有"。
func parseKind(c *gin.Context) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(c.Query("kind")))
	if v == "" {
		return KindAll, true
	}
	if !ValidKind(v) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code": "INVALID_PARAMS", "message": "kind 仅支持 all|photo|video|pano"}})
		return "", false
	}
	return v, true
}

// Clusters GET /geo/clusters?min_lng=&min_lat=&max_lng=&max_lat=&zoom=&provider=&kind=&from=&to=
func (h *Handler) Clusters(c *gin.Context) {
	b, ok := h.bboxFor(c)
	if !ok {
		return
	}
	kind, ok := parseKind(c)
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

	clusters, err := h.Media.Clusters(c.Request.Context(), b, zoom, h.provider(c), kind, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"clusters": clusters})
}

// Items GET /geo/items?min_lng=&...&kind=&from=&to=&limit=
// 返回 bbox 内媒体条目（含缩略图所需 id），供点击簇/框选后展开网格。
func (h *Handler) Items(c *gin.Context) {
	b, ok := h.bboxFor(c)
	if !ok {
		return
	}
	kind, ok := parseKind(c)
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

	items, err := h.Media.Items(c.Request.Context(), b, h.provider(c), kind, from, to, limit)
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

// Places GET /geo/places?min_lng=&...&from=&to=&limit=
// 返回 bbox 内去重地名列表（含计数），供地图底部"地理位置罗列"横向滑动展示。
func (h *Handler) Places(c *gin.Context) {
	b, ok := h.bboxFor(c)
	if !ok {
		return
	}
	kind, ok := parseKind(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
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
	places, err := h.Media.Places(c.Request.Context(), b, h.provider(c), kind, from, to, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"places": places})
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

// GetUIPrefs GET /user/ui-prefs 读取当前用户地图 UI 偏好（契约 §7）。
//
// 无记录返回 200 + 默认值（**不返回 404**，见 uiprefs.go 的说明）：
// "没设置过"与"设置成默认"对使用者没有区别，让前端为首次访问单独写分支没有收益。
func (h *Handler) GetUIPrefs(c *gin.Context) {
	p, err := h.Media.GetUIPrefs(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, p)
}

// PutUIPrefs PUT /user/ui-prefs 写入当前用户地图 UI 偏好（契约 §7）。
//
// 非法取值一律 400：这四个值会被直接喂给地图初始化，存进脏值的故障现场是
// "地图打不开"，而根因在几百行之外。
func (h *Handler) PutUIPrefs(c *gin.Context) {
	var in UIPrefs
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	norm, err := NormalizeUIPrefs(in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	if err := h.Media.PutUIPrefs(c.Request.Context(), c.GetString("user_id"), norm); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, norm)
}

// GetMapConfig GET /admin/map-config 读取系统地图配置（契约 §7，需 admin:system）。
//
// ⚠️ 响应里**不含密钥或密文**，只有可用性状态与来源。
func (h *Handler) GetMapConfig(c *gin.Context) {
	ctx := c.Request.Context()
	state, source := h.keyState(ctx)
	v, err := h.Media.GetSystemMapConfig(ctx, state, source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, v)
}

// PutMapConfig PUT /admin/map-config 更新系统地图配置（契约 §7，需 admin:system）。
//
// ⚠️ **不接受密钥字段**（见 PutSystemMapConfig 的详细说明）：列名 `china_api_key_enc` 声明的是
// 密文，而本仓库没有密钥管理设施 —— 把明文写进去等于列名说谎。
// 若请求体里带了 `china_api_key`，这里**显式 400 并说明原因**，
// 而不是静默忽略（静默忽略会让调用方以为"设置成功了"，实际 Key 没换）。
func (h *Handler) PutMapConfig(c *gin.Context) {
	var body struct {
		SystemMapConfigInput
		// 显式接收以便"存在即报错"：只是忽略未知字段的话，调用方会以为设置生效了。
		ChinaAPIKey *string `json:"china_api_key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	if body.ChinaAPIKey != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code": "API_KEY_NOT_SUPPORTED_HERE",
			"message": "本端点暂不支持写入高德 Key：数据库该列按 DDL 语义为加密存储，而当前没有密钥管理设施，" +
				"写入明文会让列名与内容不符。请改用 AMAP_KEY 环境变量配置（管理界面会显示其可用状态）。",
		}})
		return
	}
	if body.SystemMapConfigInput.Empty() {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": "没有任何待更新字段"}})
		return
	}
	ctx := c.Request.Context()
	state, source := h.keyState(ctx)
	v, err := h.Media.PutSystemMapConfig(ctx, body.SystemMapConfigInput, state, source)
	if errors.Is(err, ErrInvalidConfig) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	if err != nil {
		log.Printf("geo: 更新地图配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	// 系统配置变更写审计（audit.ActionSettingsPatch 早已登记但一直无人写入，本端点首次真正落库）。
	// detail 只记**非敏感**的枚举值；Key 相关只记状态，不记内容。
	h.record(c, audit.ActionSettingsPatch, audit.TargetSetting, "map-config", map[string]any{
		"china_provider": v.ChinaProvider,
		"intl_provider":  v.IntlProvider,
	})
	c.JSON(http.StatusOK, v)
}
