package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// ErrRsyncNotFound rsync 不可用（未安装或不在 PATH）。
var ErrRsyncNotFound = errors.New("rsync 不可用：未在 PATH 找到 rsync 可执行文件")

// RsyncPath 定位 rsync；不可用时返回 ErrRsyncNotFound（含平台安装提示）。
func RsyncPath() (string, error) {
	p, err := exec.LookPath("rsync")
	if err != nil {
		return "", fmt.Errorf("%w（Linux: apt/yum install rsync；Windows: cwRsync 或 WSL；亦可去掉 -rsync 直接扫描本地/SMB 挂载目录）", ErrRsyncNotFound)
	}
	return p, nil
}

// Rsync 把远端/本地 src 同步到本地 staging 目录（subprocess 调用，与 ffmpeg 同模式，
// 不链接任何 rsync 代码，许可安全）。src 支持 rsync 远程语法 user@host:/path。
// logw 非 nil 时把 rsync 输出实时写入（进度可视化）。
func Rsync(ctx context.Context, src, staging string, extraArgs []string, logw io.Writer) error {
	bin, err := RsyncPath()
	if err != nil {
		return err
	}
	// -a 归档保留 mtime（断点判变依赖）--partial 断点续传 --timeout 防挂死
	args := []string{"-a", "--partial", "--timeout=120"}
	args = append(args, extraArgs...)
	args = append(args, src, staging)
	cmd := exec.CommandContext(ctx, bin, args...)
	if logw != nil {
		cmd.Stdout = logw
		cmd.Stderr = logw
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync %s -> %s 失败: %w", src, staging, err)
	}
	return nil
}
