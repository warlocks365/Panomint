package media

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StaleUploadTTL 分块上传会话的过期阈值（P2-04）：超过该时间未续传的
// .part/.json 视为中断残留（网络断、用户放弃、首块后不再续传）。
const StaleUploadTTL = 24 * time.Hour

// SweepStaleUploads 清扫 dir（UploadTmp）下 mtime 超过 maxAge 的分块会话残留
// （.part 数据与 .json 元数据）；返回删除的文件数。
//
// 接线方式（任务给定二选一，本实现选「导出函数、API 启动时装配」）：
// 由 cmd/api 在启动时调用一次（可再挂周期任务），装配点在 main.go，不在本包。
// 不选「ingest 路径惰性触发」的原因：惰性触发让清扫时机依赖上传流量 ——
// 低流量部署里残留照样长期存在，且给上传热路径挂上全目录遍历；
// 启动清扫语义直白、可用 t.TempDir 直接单测。
//
// 判定以文件 mtime 为准（与成功合并路径只删自己会话文件的既有行为一致）；
// meta json 里的 CreatedAt（uploadMeta.created_at，P2-04 补记）是辅助信息，
// 不作为删除依据 —— 旧版本会话没有该字段，mtime 对所有版本都成立。
func SweepStaleUploads(dir string, maxAge time.Duration, now time.Time) (removed int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // 还没有任何分块上传过，正常
		}
		return 0, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".part") && !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // Stat 失败（如正被并发删除）：跳过，下轮再扫
		}
		if now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			removed++
		}
	}
	return removed, nil
}
