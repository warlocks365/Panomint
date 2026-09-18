// Package health 健康检查端点（TDD §8.5：/health liveness、/ready readiness、/metrics）。
package health

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"panoalbum/internal/queue"
)

// Handler 健康检查处理器。
type Handler struct {
	Pool  *pgxpool.Pool
	Queue *queue.Queue
	// DiskCheckDir 磁盘可写检查目录（媒体写入目标卷）
	DiskCheckDir string
}

// Live GET /health：进程存活即 200（liveness）。
func (h *Handler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready GET /ready：PG + Valkey + 磁盘可写，全部通过才 200（readiness）。
//
// ⚠️ 本端点**无鉴权**（与 /health、/metrics 同挂在根路由上），因此失败原因里绝不能带
// err.Error()：PG/Valkey 的原文会漏出主机、端口、用户名，os 错误会漏出文件系统路径。
// 客户端只拿 "fail"，完整错误写服务端日志供排障。
func (h *Handler) Ready(c *gin.Context) {
	checks := gin.H{}
	ok := true

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if err := h.Pool.Ping(ctx); err != nil {
		log.Printf("health: postgres 探测失败: %v", err)
		checks["postgres"] = "fail"
		ok = false
	} else {
		checks["postgres"] = "ok"
	}

	if err := h.Queue.Ping(ctx); err != nil {
		log.Printf("health: valkey 探测失败: %v", err)
		checks["valkey"] = "fail"
		ok = false
	} else {
		checks["valkey"] = "ok"
	}

	if err := checkDiskWritable(h.DiskCheckDir); err != nil {
		log.Printf("health: 磁盘可写探测失败 dir=%s: %v", h.DiskCheckDir, err)
		checks["disk"] = "fail"
		ok = false
	} else {
		checks["disk"] = "ok"
	}

	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "checks": checks})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "checks": checks})
}

// Metrics GET /metrics：Prometheus 指标暴露。
func (h *Handler) Metrics() gin.HandlerFunc {
	return gin.WrapH(promhttp.Handler())
}

func checkDiskWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(dir, ".ready_probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	return os.Remove(probe)
}
