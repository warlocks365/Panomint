//go:build windows

package media

import "golang.org/x/sys/windows"

// freeBytes 返回 path 所在卷的可用字节数（P1-05 落盘前空间检查）。
// 项目部署目标是 Linux，本实现只为保证 Windows 上 go build / 本地开发可用。
func freeBytes(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}
