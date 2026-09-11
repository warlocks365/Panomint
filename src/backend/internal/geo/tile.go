package geo

// 底图瓦片反代（Job000009）：服务端代持高德 Key，前端只认本站 /tiles/amap/{z}/{x}/{y}。
//
// 为什么必须反代：
//  1. Key 不下发到浏览器——避免被抓包盗用、也便于集中限流与更换服务商；
//  2. 内网 HTTPS 场景下浏览器只信任本站证书，直连高德域名还要额外处理证书与跨域；
//  3. 后续若换底图（天地图/自建瓦片），前端 URL 不变，只改后端一处。

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// AmapTileProxy 高德栅格瓦片反代。
type AmapTileProxy struct {
	Key    string        // 可选：高德 Web 服务 Key（未配置则不附加 key 参数）
	Client *http.Client  // 未配置时用默认 10s 超时客户端
	// Style 高德瓦片样式：7=矢量无标注，8=矢量带标注（中文路网）。
	Style string
	// CacheDir 磁盘缓存目录（空=不缓存）。命中后不再请求上游，
	// 是高德 429 限流的主要对策（拖动地图反复命中常用 zoom 层）。
	CacheDir string
}

// cachePath 缓存文件路径：<dir>/<style>/<z>/<x>/<y>.png
func (p *AmapTileProxy) cachePath(style string, z, x, y int) string {
	return filepath.Join(p.CacheDir, style, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
}

// readCache 命中返回瓦片字节（文件存在且非空）。
func (p *AmapTileProxy) readCache(style string, z, x, y int) ([]byte, bool) {
	if p.CacheDir == "" {
		return nil, false
	}
	data, err := os.ReadFile(p.cachePath(style, z, x, y))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// writeCache 写缓存：先写临时文件再 rename，避免并发读到截断文件。
// 写失败仅忽略（缓存失败不影响主流程）。
func (p *AmapTileProxy) writeCache(style string, z, x, y int, data []byte) {
	if p.CacheDir == "" || len(data) == 0 {
		return
	}
	dst := p.cachePath(style, z, x, y)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, dst)
}

// amapHosts 高德瓦片 CDN 节点，按瓦片坐标分散负载。
var amapHosts = [4]string{"webrd01", "webrd02", "webrd03", "webrd04"}

// Serve 处理 GET /tiles/amap/:z/:x/:y。
func (p *AmapTileProxy) Serve(c *gin.Context) {
	z, err1 := strconv.Atoi(c.Param("z"))
	x, err2 := strconv.Atoi(c.Param("x"))
	y, err3 := strconv.Atoi(c.Param("y"))
	if err1 != nil || err2 != nil || err3 != nil || z < 0 || z > 22 || x < 0 || y < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_TILE", "message": "瓦片坐标非法"}})
		return
	}
	if y >= (1 << z) || x >= (1<<z) { // 该 zoom 下的瓦片索引上界
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_TILE", "message": "瓦片坐标越界"}})
		return
	}

	style := p.Style
	if style == "" {
		style = "8"
	}

	// 缓存命中直接返回（瓦片内容 immutable，无需校验时效）
	if data, hit := p.readCache(style, z, x, y); hit {
		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("Content-Type", "image/png")
		c.Header("X-Tile-Cache", "hit")
		c.Data(http.StatusOK, "image/png", data)
		return
	}

	host := amapHosts[(x+y)%len(amapHosts)]
	u := fmt.Sprintf("https://%s.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=%s&x=%d&y=%d&z=%d",
		host, style, x, y, z)
	if p.Key != "" {
		u += "&key=" + p.Key
	}

	cl := p.Client
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, u, nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "TILE_UPSTREAM", "message": err.Error()}})
		return
	}
	// 上游按 Referer 做防盗链判断，透传本站 Host 便于排查，不暴露内网细节
	req.Header.Set("User-Agent", "panoalbum-tile-proxy/1.0")

	resp, err := cl.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "TILE_UPSTREAM", "message": err.Error()}})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "TILE_UPSTREAM", "message": fmt.Sprintf("上游返回 %d", resp.StatusCode)}})
		return
	}

	// 读全量 body（瓦片 ≤ 数百 KB），写缓存后再下发；429/5xx 不缓存
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "TILE_UPSTREAM", "message": err.Error()}})
		return
	}
	p.writeCache(style, z, x, y, body)

	// 瓦片内容 immutable：允许浏览器/代理长缓存，显著降低重复拖动的上游请求
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("X-Tile-Cache", "miss")
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/png"
	}
	c.Data(http.StatusOK, ct, body)
}
