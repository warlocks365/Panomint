package main

// rclone 配置生成 + 各类型挂载命令组装（与 main.go 同包，纯逻辑可单测）。

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"panoalbum/internal/storage"
)

// connConf 非敏感连接配置（与 storage 包 handler 的 conn 同形；此处独立副本避免
// 跨包依赖 handler 内部类型——storagectl 只读表，不引 handler）。
type connConf struct {
	URL    string `json:"url"`
	Host   string `json:"host"`
	Share  string `json:"share"`
	Export string `json:"export"`
	Port   int    `json:"port,omitempty"`
}

// mountOne 按类型执行挂载：webdav/smb=rclone（只读），nfs=内核 mount（只读）。
func mountOne(m mountRec, mp string) error {
	if err := os.MkdirAll(mp, 0o755); err != nil {
		return fmt.Errorf("建挂载点: %w", err)
	}
	var cc connConf
	if err := jsonUnmarshal([]byte(m.connJSON), &cc); err != nil {
		return fmt.Errorf("连接配置解析: %w", err)
	}
	creds, err := storage.DecryptCreds(m.credsEnc)
	if err != nil {
		return fmt.Errorf("凭据解密: %w", err)
	}
	switch m.typ {
	case "webdav", "smb":
		return rcloneMount(m, cc, creds, mp)
	case "nfs":
		return nfsMount(cc, mp)
	}
	return fmt.Errorf("未知类型 %q", m.typ)
}

// rcloneConfig 生成 rclone 配置文本。密码用 obscure 格式（rclone 约定，非加密仅混淆防窥）。
func rcloneConfig(typ string, cc connConf, creds *storage.Creds, obscuredPass string) string {
	var b strings.Builder
	b.WriteString("[dst]\n")
	b.WriteString("type = " + typ + "\n")
	if typ == "webdav" {
		fmt.Fprintf(&b, "url = %s\n", cc.URL)
	} else { // smb
		fmt.Fprintf(&b, "host = %s\nshare = %s\n", cc.Host, cc.Share)
		if cc.Port > 0 {
			fmt.Fprintf(&b, "port = %d\n", cc.Port)
		}
	}
	if creds != nil && creds.User != "" {
		fmt.Fprintf(&b, "user = %s\n", creds.User)
	}
	if obscuredPass != "" {
		fmt.Fprintf(&b, "pass = %s\n", obscuredPass)
	}
	if creds != nil && creds.Domain != "" {
		fmt.Fprintf(&b, "domain = %s\n", creds.Domain)
	}
	return b.String()
}

// rcloneMount rclone 挂载（--read-only 单向导入；--daemon 后台化）。
func rcloneMount(m mountRec, cc connConf, creds *storage.Creds, mp string) error {
	obp := ""
	if creds != nil && creds.Pass != "" {
		var err error
		obp, err = obscure(creds.Pass)
		if err != nil {
			return err
		}
	}
	cfgPath := path.Join("/tmp", "rclone-"+mountKey(m.id)+".conf")
	if err := os.WriteFile(cfgPath, []byte(rcloneConfig(m.typ, cc, creds, obp)), 0o600); err != nil {
		return fmt.Errorf("写 rclone 配置: %w", err)
	}
	defer os.Remove(cfgPath)
	// ⚠️ 绝不能用 CombinedOutput/Output 捕获 rclone mount 的输出：
	// --daemon 模式主进程 fork 后退出，但 **daemon 子进程继承 stdout/stderr pipe**，
	// CombinedOutput 等 pipe EOF 会永久阻塞（实测卡死 reconcile 循环，进程假活零日志）。
	// 输出一律走 --log-file；这里只判 daemon 启动退出码。
	logPath := path.Join("/tmp", "rclone-"+mountKey(m.id)+".log")
	_ = os.Remove(logPath)
	args := []string{
		"mount", "dst:", mp,
		"--config", cfgPath,
		"--read-only",
		"--vfs-cache-mode", "off",
		"--dir-cache-time", "30s",
		"--daemon",
		"--log-level", "ERROR",
		"--log-file", logPath,
	}
	if err := exec.Command("rclone", args...).Run(); err != nil {
		tail := readLogTail(logPath, 200)
		return fmt.Errorf("rclone mount: %w (%s)", err, tail)
	}
	// --daemon 立即返回；等 FUSE 就绪后轮询验证。
	for i := 0; i < 10; i++ {
		if healthCheck(mp) == nil {
			return nil
		}
		timeSleep(300 * timeMillisecond)
	}
	return fmt.Errorf("挂载未就绪（见 %s）", logPath)
}

// readLogTail 读日志文件末尾 n 字节（错误上下文补充；文件不存在返回空串）。
func readLogTail(p string, n int) string {
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return ""
	}
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return truncate(string(b), n)
}

// nfsMount 内核 NFS 只读挂载。
func nfsMount(cc connConf, mp string) error {
	target := cc.Host + ":" + cc.Export
	args := []string{"-t", "nfs", "-o", "ro,timeo=50,retrans=2,nolock", target, mp}
	if out, err := execCommand("mount", args...); err != nil {
		return fmt.Errorf("mount nfs: %v (%s)", err, truncate(string(out), 200))
	}
	return nil
}

// syncLocalDir 挂载媒体的本地落地目录（主存储内，普通目录语义，用户可管理）。
// 导入式语义：远程文件复制进 /data/media（断连后已导入内容仍可读可用；
// hash 去重保证重复导入幂等）。不沿用"扫描挂载点引用路径"——umount 后文件不可读。
func syncLocalDir(id string) string {
	return path.Join(os.Getenv("UPLOAD_DIR"), "_imports", mountKey(id))
}

// syncMount 增量同步挂载内容到本地落地目录：
// webdav/smb 走 rclone copy（按 size+mtime 跳过，无变更秒过）；nfs 走 cp -ru。
// cfg 内容在 rcloneMount 里已删——同步复用挂载点视图（FUSE 读），nfs 直接读挂载点。
func syncMount(m mountRec, mp string) (string, error) {
	local := syncLocalDir(m.id)
	if err := os.MkdirAll(local, 0o755); err != nil {
		return "", fmt.Errorf("建落地目录: %w", err)
	}
	switch m.typ {
	case "webdav", "smb":
		// 经挂载点读（FUSE 本地视图→本地拷贝，省去配置重生，天然只读）。
		if out, err := execCommand("rclone", "copy", mp, local,
			"--transfers", "2", "--checkers", "4",
			"--log-level", "ERROR"); err != nil {
			return "", fmt.Errorf("rclone copy: %v (%s)", err, truncate(string(out), 200))
		}
	case "nfs":
		if out, err := execCommand("cp", "-ru", mp+"/.", local+"/"); err != nil {
			return "", fmt.Errorf("cp 同步: %v (%s)", err, truncate(string(out), 200))
		}
	}
	return local, nil
}
