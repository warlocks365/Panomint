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
	// ⚠️ 不用 rclone --daemon：实测容器内 daemon 化后 FUSE 初始化卡死
	//（挂载点半挂，任何访问挂起，docker exec 都进不去）。改为前台 rclone +
	// Start 不 Wait（进程脱离由容器 init 接管）；挂载是否就绪由下方 healthCheck
	// 轮询判定。另：绝不能用 CombinedOutput——子进程继承 pipe 会永久阻塞等待方。
	logPath := path.Join("/tmp", "rclone-"+mountKey(m.id)+".log")
	_ = os.Remove(logPath)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("开日志: %w", err)
	}
	args := []string{
		"mount", "dst:", mp,
		"--config", cfgPath,
		"--read-only",
		"--vfs-cache-mode", "off",
		"--dir-cache-time", "30s",
		"--log-level", "ERROR",
		// 后端不可达时快速失败而非挂起（默认无连接超时，FUSE 请求会无限等）。
		"--contimeout", "15s",
		"--timeout", "30s",
		"--low-level-retries", "2",
		"--retries", "1",
	}
	cmd := exec.Command("rclone", args...)
	cmd.Stdout = lf
	cmd.Stderr = lf
	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("rclone mount 启动: %w", err)
	}
	lf.Close()
	// 就绪判定只看 /proc/mounts（mount(2) 完成即注册，读取永不阻塞）；
	// 绝不对挂载点 readdir 判就绪——后端不可达时 FUSE 请求挂起且无 ctx 可救。
	for i := 0; i < 20; i++ {
		if mnt, err := listActiveMounts(); err == nil && mnt[mountKey(m.id)] != "" {
			return nil
		}
		timeSleep(500 * timeMillisecond)
	}
	return fmt.Errorf("挂载 10s 内未注册到 /proc/mounts（见 %s）", logPath)
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
		// 超时参数防后端不可达时 copy 无限挂起（CombinedOutput 等子进程退出）。
		if out, err := execCommand("rclone", "copy", mp, local,
			"--transfers", "2", "--checkers", "4",
			"--contimeout", "15s", "--timeout", "60s",
			"--low-level-retries", "2", "--retries", "2",
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
