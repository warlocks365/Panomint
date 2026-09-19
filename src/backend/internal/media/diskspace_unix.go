//go:build !windows

package media

import "golang.org/x/sys/unix"

// freeBytes 返回 path 所在文件系统的可用字节数（P1-05 落盘前空间检查）。
// 用 Bavail（非特权用户可用块）而不是 Bfree：root 保留块对服务进程同样不可用。
func freeBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
