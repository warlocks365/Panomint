// Package config 纯环境变量配置（12-factor；TDD §8.6：.env 开发 / Compose environment 生产）。
//
// ⚠️ 本文件的**默认值只适用于"在本机开发"**，且这是刻意的：本项目目标是通用相册系统，
// 需支持任意平台容器化部署与独立服务器安装，因此默认值里**不得**出现
//
//	· 任何具体主机的 IP / 主机名（曾默认指向某台内网机器 —— 忘设环境变量就连到别人的库）
//	· 任何口令（曾默认内置明文口令 —— 既是部署失败也是凭据泄露）
//	· 任何测试数据目录（曾把 MediaRoot 默认成 ./testdata/media，生产误用会把测试数据当媒体根）
//
// 为了不让"忘了设环境变量"在**生产**里静默连到 127.0.0.1 上的陌生库，
// Load 会在 APP_ENV=prod 且上述任一关键项**仍是默认值**时**直接返回错误**（启动即失败）。
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// 默认值集中定义：这样"生产环境是否仍在使用默认值"可以逐个比对，
// 而不是靠字符串散落在各处（第 3 处手写副本迟早会漂移）。
const (
	DefaultPGDSN      = "postgres://postgres@127.0.0.1:5432/pano_album"
	DefaultValkeyAddr = "127.0.0.1:6379"
	DefaultMediaRoot  = "./data/media"
	DefaultCORS       = "http://localhost:5173,http://localhost:8765,http://localhost:8088,http://127.0.0.1:8088"
)

// Config 服务配置。
type Config struct {
	Port         string // API 监听端口
	PGDSN        string // PostgreSQL 连接串
	ValkeyAddr   string // Valkey 地址
	ValkeyPass   string
	CORSOrigins  []string // 允许的跨域源
	Env          string   // dev|prod
	UploadDir    string   // 上传媒体存储根（./data/media）
	UploadTmp    string   // 分块上传临时目录（./data/uploads）
	HLSDir       string   // HLS 输出根目录（./data/hls）
	MediaRoot    string   // 既有索引媒体根（media.path 相对解析回退）
	AmapKey      string   // 高德逆地理编码 Key（空=不启用，入库时不自动填 place）
	AmapSecret   string   // 高德安全密钥（空=不带 sig 签名；Key 绑定安全密钥后必填）
	TileCacheDir string   // 瓦片磁盘缓存目录（Job000009；空=不缓存，高频率拖动易触发高德 429 限流）
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitOrigins 解析逗号分隔的允许源。
//
// ⚠️ 必须过滤空串：`strings.Split("", ",")` 返回 `[""]`（含一个空串）而不是空切片，
// 于是把 CORS_ORIGINS 显式设为空字符串时会多出一个空串条目 —— 而"显式置空"的正确语义是
// **不允许任何跨域源**，不是"允许一个空源"。同时容忍 `a, b` 这种带空格的写法。
func splitOrigins(s string) []string {
	out := make([]string, 0, 4)
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load 读取配置；可选加载 .env（已存在的环境变量优先，不覆盖）。
//
// 返回 error 的唯一情形是**生产环境仍在使用默认的关键配置**（见 validate）。
// 这样"忘设环境变量"的后果是**启动失败**，而不是连到别人的库、或连到本机的陌生库。
func Load() (Config, error) {
	loadDotEnv(".env")
	c := Config{
		Port:         env("API_PORT", "8080"),
		PGDSN:        env("PG_DSN", DefaultPGDSN),
		ValkeyAddr:   env("VALKEY_ADDR", DefaultValkeyAddr),
		ValkeyPass:   os.Getenv("VALKEY_PASSWORD"),
		CORSOrigins:  splitOrigins(env("CORS_ORIGINS", DefaultCORS)),
		Env:          env("APP_ENV", "dev"),
		UploadDir:    env("UPLOAD_DIR", "./data/media"),
		UploadTmp:    env("UPLOAD_TMP", "./data/uploads"),
		HLSDir:       env("HLS_DIR", "./data/hls"),
		MediaRoot:    env("MEDIA_ROOT", DefaultMediaRoot),
		AmapKey:      env("AMAP_KEY", ""),       // 空=不启用逆地理编码，入库时 place 留空
		AmapSecret:   env("AMAP_SECRET", ""),    // 空=请求不带 sig 签名
		TileCacheDir: env("TILE_CACHE_DIR", ""), // 空=瓦片不缓存
	}
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// validate 生产环境门禁。
//
// 只在 APP_ENV=prod 时生效 —— 开发环境（含测试）用默认值是**正常且必须**的，
// 不能因为加了门禁就把本地开发堵死。
//
// 错误信息里**不含任何值**（更不含口令），只报键名：错误信息本身也可能被日志采集。
func (c Config) validate() error {
	if c.Env != EnvProd {
		return nil
	}
	var stillDefault []string
	if c.PGDSN == DefaultPGDSN {
		stillDefault = append(stillDefault, "PG_DSN")
	}
	if c.ValkeyAddr == DefaultValkeyAddr {
		stillDefault = append(stillDefault, "VALKEY_ADDR")
	}
	if c.MediaRoot == DefaultMediaRoot {
		stillDefault = append(stillDefault, "MEDIA_ROOT")
	}
	if len(stillDefault) > 0 {
		return fmt.Errorf("APP_ENV=%s 但以下配置仍是仅适用于本机开发的默认值：%s；"+
			"请显式设置这些环境变量（或改用 APP_ENV=dev）", EnvProd, strings.Join(stillDefault, ", "))
	}
	return nil
}

// EnvProd 生产环境标识（APP_ENV 的取值）。
const EnvProd = "prod"

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
