package shares

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func ogCtx(target string) *gin.Context {
	c, _ := newTestCtx(target)
	c.Params = gin.Params{{Key: "token", Value: "tok"}}
	return c
}

// TestOriginOf 三种访问入口（内网域名 / 局域网 IP:8088 / 隧道）都必须由请求头动态拼出，
// 且优先采信反向代理透传的 X-Forwarded-Proto（否则 https 页面会拿到 http 的图片地址）。
func TestOriginOf(t *testing.T) {
	c := ogCtx("http://panomint.warlocks.cn/public/shares/tok/og")
	c.Request.Host = "panomint.warlocks.cn"
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	if got := originOf(c); got != "https://panomint.warlocks.cn" {
		t.Fatalf("应采信 X-Forwarded-Proto，实际 %s", got)
	}

	c = ogCtx("http://192.168.1.115:8088/public/shares/tok/og")
	c.Request.Host = "192.168.1.115:8088"
	if got := originOf(c); got != "http://192.168.1.115:8088" {
		t.Fatalf("局域网 IP 直连应为 http，实际 %s", got)
	}

	c = ogCtx("http://abcd.trycloudflare.com/public/shares/tok/og")
	c.Request.Host = "abcd.trycloudflare.com"
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	if got := originOf(c); got != "https://abcd.trycloudflare.com" {
		t.Fatalf("隧道入口应为 https，实际 %s", got)
	}
}

// TestRenderOG 渲染结果的四项：完整卡片、中性卡片、转义、状态码。
func TestRenderOG(t *testing.T) {
	h := &Handler{}

	t.Run("完整卡片", func(t *testing.T) {
		c, rec := newTestCtx("/public/shares/tok/og")
		c.Params = gin.Params{{Key: "token", Value: "tok"}}
		h.renderOG(c, 200, ogData{
			Title:       "雷克雅未克·斯科加瀑布",
			Description: "1 项内容 · 含 360 全景 · 在线浏览",
			ImageURL:    "https://panomint.warlocks.cn/public/shares/tok/media/m-1/thumb?size=md",
			PageURL:     "https://panomint.warlocks.cn/share/tok",
		})
		html := rec.Body.String()
		if rec.Code != 200 {
			t.Fatalf("状态码应为 200，实际 %d", rec.Code)
		}
		for _, want := range []string{
			`property="og:title" content="雷克雅未克·斯科加瀑布"`,
			`property="og:description"`,
			`property="og:image" content="https://panomint.warlocks.cn/public/shares/tok/media/m-1/thumb?size=md"`,
			`property="og:url" content="https://panomint.warlocks.cn/share/tok"`,
			`property="og:type" content="website"`,
			`name="twitter:card" content="summary_large_image"`,
		} {
			if !strings.Contains(html, want) {
				t.Fatalf("HTML 缺少 %s\n%s", want, html)
			}
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type 应为 text/html，实际 %s", ct)
		}
	})

	t.Run("中性卡片无图片", func(t *testing.T) {
		// 受密码保护 / 已过期 / 不存在：不得泄露标题与缩略图
		c, rec := newTestCtx("/public/shares/tok/og")
		c.Params = gin.Params{{Key: "token", Value: "tok"}}
		h.renderOG(c, 200, ogData{
			Title:       "全景相册分享",
			Description: "该分享受访问密码保护，请在浏览器中打开并输入密码查看。",
			PageURL:     "https://panomint.warlocks.cn/share/tok",
		})
		html := rec.Body.String()
		if strings.Contains(html, "og:image") || strings.Contains(html, "twitter:image") {
			t.Fatalf("中性卡片不得输出 og:image/twitter:image\n%s", html)
		}
		if !strings.Contains(html, `name="twitter:card" content="summary"`) {
			t.Fatalf("中性卡片应回落到 summary 卡片\n%s", html)
		}
	})

	t.Run("标题转义防注入", func(t *testing.T) {
		c, rec := newTestCtx("/public/shares/tok/og")
		c.Params = gin.Params{{Key: "token", Value: "tok"}}
		h.renderOG(c, 200, ogData{
			Title:   `"><script>alert(1)</script>`,
			PageURL: "https://panomint.warlocks.cn/share/tok",
		})
		html := rec.Body.String()
		if strings.Contains(html, "<script>alert(1)</script>") || strings.Contains(html, `content=""><script`) {
			t.Fatalf("标题必须被 html/template 转义\n%s", html)
		}
		if !strings.Contains(html, "&lt;script&gt;") {
			t.Fatalf("转义结果应为实体，实际未转义\n%s", html)
		}
	})

	t.Run("PageURL 缺省时由 token 派生", func(t *testing.T) {
		c, rec := newTestCtx("http://192.168.1.115:8088/public/shares/abc/og")
		c.Params = gin.Params{{Key: "token", Value: "abc"}}
		c.Request.Host = "192.168.1.115:8088"
		h.renderOG(c, 200, ogData{Title: "x"})
		if !strings.Contains(rec.Body.String(), `content="http://192.168.1.115:8088/share/abc"`) {
			t.Fatalf("PageURL 应派生为 /share/abc\n%s", rec.Body.String())
		}
	})

	t.Run("404 页带 noindex", func(t *testing.T) {
		c, rec := newTestCtx("/public/shares/tok/og")
		c.Params = gin.Params{{Key: "token", Value: "tok"}}
		h.renderOG(c, 404, ogData{Title: "分享不存在", NoIndex: true})
		if rec.Code != 404 {
			t.Fatalf("状态码应为 404，实际 %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `<meta name="robots" content="noindex">`) {
			t.Fatal("失效分享应标记 noindex")
		}
	})
}
