// Package config 纯环境变量配置（12-factor；TDD §8.6：.env 开发 / Compose environment 生产）。
package config

import (
	"bufio"
	"os"
	"strings"
)

// Config 服务配置。
type Config struct {
	Port        string // API 监听端口
	PGDSN       string // PostgreSQL 连接串
	ValkeyAddr  string // Valkey 地址
	ValkeyPass  string
	CORSOrigins []string // 允许的跨域源
	Env         string   // dev|prod
	UploadDir   string   // 上传媒体存储根（./data/media）
	UploadTmp   string   // 分块上传临时目录（./data/uploads）
	HLSDir      string   // HLS 输出根目录（./data/hls）
	MediaRoot   string   // 既有索引媒体根（media.path 相对解析回退）
	AmapKey     string   // 高德逆地理编码 Key（空=不启用，入库时不自动填 place）
	AmapSecret  string   // 高德安全密钥（空=不带 sig 签名；Key 绑定安全密钥后必填）
	TileCacheDir string  // 瓦片磁盘缓存目录（Job000009；空=不缓存，高频率拖动易触发高德 429 限流）
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load 读取配置；可选加载 .env（已存在的环境变量优先，不覆盖）。
func Load() Config {
	loadDotEnv(".env")
	return Config{
		Port:        env("API_PORT", "8080"),
		PGDSN:       env("PG_DSN", "postgres://pano:PanoDev2026!@192.168.1.115:5432/pano_album"),
		ValkeyAddr:  env("VALKEY_ADDR", "192.168.1.115:6379"),
		ValkeyPass:  os.Getenv("VALKEY_PASSWORD"),
		CORSOrigins: strings.Split(env("CORS_ORIGINS", "http://localhost:5173,http://localhost:8765,https://192.168.1.117:8443"), ","),
		Env:         env("APP_ENV", "dev"),
		UploadDir:   env("UPLOAD_DIR", "./data/media"),
		UploadTmp:   env("UPLOAD_TMP", "./data/uploads"),
		HLSDir:      env("HLS_DIR", "./data/hls"),
		MediaRoot:   env("MEDIA_ROOT", "./testdata/media"),
		AmapKey:     env("AMAP_KEY", ""), // 空=不启用逆地理编码，入库时 place 留空
		AmapSecret:  env("AMAP_SECRET", ""), // 空=请求不带 sig 签名
		TileCacheDir: env("TILE_CACHE_DIR", ""), // 空=瓦片不缓存
	}
}

// loadDotEnv 极简 .env 解析（KEY=VALUE，# 注释，不覆盖已有环境变量）。
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
