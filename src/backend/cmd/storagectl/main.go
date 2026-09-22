// storagectl 网络挂载执行器（Job000070 F4）。
//
// 常驻轮询 storage_mounts 表，对每条挂载：
//   - 未挂 → 解密凭据 → rclone mount（webdav/smb，只读）或内核 mount（nfs，只读）
//     → 验证挂载点可读 → 回写 status=online + mount_path → 触发索引导入（媒体归挂载属主）
//   - 已挂 → 健康检查（读目录，EIO/超时=断连）→ 失败回写 offline 并重挂
//   - 表里没有但盘上挂着的 → 孤儿卸载（防删除后残留）
//
// 与 indexctl 同容器运行（entrypoint 双进程），共享 mount 命名空间——
// FUSE 挂载点只有同容器进程可见，跨容器需挂载传播更脆。
//
// 安全红线：
//   - 凭据解密后只写 0600 临时配置文件，进程内不留副本，用完即删；
//   - 挂载只读（--read-only / -o ro）：导入单向，绝不往远程写；
//   - last_error 截断 300 字符，不吞敏感串。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/config"
	"panoalbum/internal/index"
	"panoalbum/internal/queue"
	"panoalbum/internal/storage"
)

const (
	mountRoot   = "/mnt/storage"
	pollEvery   = 10 * time.Second
	healthEvery = 30 * time.Second // 每个挂载的健康检查最小间隔
	syncEvery   = 5 * time.Minute  // 每个挂载的增量同步最小间隔
	errMaxLen   = 300
)

// 可替换别名（单测钉行为，不真 exec/不真 sleep）。
var (
	jsonUnmarshal = json.Unmarshal
	execCommand   = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
	timeSleep = time.Sleep
)

const timeMillisecond = time.Millisecond

// mountRec 轮询行。
type mountRec struct {
	id       string
	name     string
	typ      string
	connJSON string
	credsEnc string
	ownerID  string
}

func main() {
	log.SetPrefix("[storagectl] ")
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "用法: storagectl run")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	pollFlag := fs.Duration("poll", pollEvery, "轮询间隔")
	_ = fs.Parse(os.Args[2:])

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("配置加载: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), cfg.PGDSN)
	if err != nil {
		log.Fatalf("连接 PG: %v", err)
	}
	defer pool.Close()
	q := queue.New("media", queue.Config{Addr: cfg.ValkeyAddr, Password: cfg.ValkeyPass})
	defer q.Close()
	idx := index.New(pool, q)

	log.Printf("启动：poll=%v", *pollFlag)
	if !storage.CipherKeyConfigured() {
		log.Printf("警告：STORAGE_CIPHER_KEY 未配置——带凭据挂载将无法解密（无凭据挂载不受影响）")
	}
	lastHealth := map[string]time.Time{}
	lastSync := map[string]time.Time{}
	for {
		if err := reconcile(context.Background(), pool, idx, lastHealth, lastSync); err != nil {
			log.Printf("轮询失败: %v", err)
		}
		time.Sleep(*pollFlag)
	}
}

// reconcile 一轮对账。
func reconcile(ctx context.Context, pool *pgxpool.Pool, idx *index.Indexer, lastHealth, lastSync map[string]time.Time) error {
	rows, err := pool.Query(ctx,
		`SELECT id::text, name, type, conn::text, creds_enc, owner_id::text FROM storage_mounts`)
	if err != nil {
		return err
	}
	var want []mountRec
	for rows.Next() {
		var m mountRec
		if err := rows.Scan(&m.id, &m.name, &m.typ, &m.connJSON, &m.credsEnc, &m.ownerID); err != nil {
			rows.Close()
			return err
		}
		want = append(want, m)
	}
	rows.Close()
	return reconcileWith(ctx, pool, idx, lastHealth, lastSync, want)
}

// reconcileWith 纯对账逻辑（可测）：want=表内期望集。
func reconcileWith(ctx context.Context, pool *pgxpool.Pool, idx *index.Indexer, lastHealth, lastSync map[string]time.Time, want []mountRec) error {
	wantIDs := map[string]bool{}
	for _, m := range want {
		wantIDs[mountKey(m.id)] = true
	}

	// 孤儿卸载：盘上挂着但表里没有。
	active, err := listActiveMounts()
	if err != nil {
		log.Printf("列挂载点失败: %v", err)
	} else {
		for key, mp := range active {
			if !wantIDs[key] {
				log.Printf("孤儿卸载 %s（表内无此挂载）", mp)
				if err := umount(mp); err != nil {
					log.Printf("孤儿卸载失败 %s: %v", mp, err)
				}
			}
		}
	}

	// 期望集：未挂的挂载，已挂的健康检查 + 到期同步。
	for _, m := range want {
		mp := mountPath(m.id)
		if _, ok := active[mountKey(m.id)]; ok {
			if time.Since(lastHealth[m.id]) >= healthEvery {
				lastHealth[m.id] = time.Now()
				if err := healthCheck(mp); err != nil {
					log.Printf("健康检查失败 %s(%s): %v，重挂", m.name, mp, err)
					setStatus(ctx, pool, m.id, "offline", "断连: "+truncate(err.Error(), errMaxLen-4))
					_ = umount(mp)
					if err := mountOne(m, mp); err != nil {
						setStatus(ctx, pool, m.id, "error", truncate(err.Error(), errMaxLen))
						continue
					}
					setStatus(ctx, pool, m.id, "online", "")
					lastSync[m.id] = time.Time{} // 重挂后立即同步
				}
			}
			if err := syncAndIndex(ctx, pool, idx, m, mp, lastSync); err != nil {
				log.Printf("同步/索引失败 %s: %v", m.name, err)
				setStatus(ctx, pool, m.id, "error", truncate("同步: "+err.Error(), errMaxLen))
			}
			continue
		}
		// 未挂：执行挂载 → 同步落地 → 索引导入。
		if err := mountOne(m, mp); err != nil {
			log.Printf("挂载失败 %s: %v", m.name, err)
			setStatus(ctx, pool, m.id, "error", truncate(err.Error(), errMaxLen))
			continue
		}
		setStatus(ctx, pool, m.id, "online", "")
		log.Printf("已挂载 %s → %s", m.name, mp)
		if err := syncAndIndex(ctx, pool, idx, m, mp, lastSync); err != nil {
			log.Printf("同步/索引失败 %s: %v", m.name, err)
			setStatus(ctx, pool, m.id, "error", truncate("同步: "+err.Error(), errMaxLen))
		}
	}
	return nil
}

// syncAndIndex 增量同步到本地落地目录 + 索引导入（hash 去重幂等，可反复执行）。
func syncAndIndex(ctx context.Context, pool *pgxpool.Pool, idx *index.Indexer, m mountRec, mp string, lastSync map[string]time.Time) error {
	if time.Since(lastSync[m.id]) < syncEvery {
		return nil
	}
	lastSync[m.id] = time.Now()
	local, err := syncMount(m, mp)
	if err != nil {
		return err
	}
	st, err := idx.ScanAs(ctx, local, m.ownerID)
	if err != nil {
		return err
	}
	log.Printf("同步+索引完成 %s：共%d 新%d 重%d 失败%d（落地 %s）",
		m.name, st.Total, st.Inserted, st.Duplicate, st.Failed, local)
	return nil
}

// mountKey 挂载点键（id 前 8 位，目录名与 id 的关联键）。
func mountKey(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// mountPath 挂载点路径。
func mountPath(id string) string {
	return path.Join(mountRoot, mountKey(id))
}

// truncate 错误信息截断。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// setStatus 回写状态（best-effort，失败仅记日志）。
// ⚠️ status 传两次（$2 写列、$5 供 CASE 比较）：同一占位符混用 varchar/text 上下文会
// 触发 42P08 inconsistent types（与 mediascope $n::text 同族教训），拆开各推各的。
func setStatus(ctx context.Context, pool *pgxpool.Pool, id, status, lastErr string) {
	var le any
	if lastErr != "" {
		le = lastErr
	}
	_, err := pool.Exec(ctx,
		`UPDATE storage_mounts SET status=$1, last_error=$2,
		 mount_path=CASE WHEN $3='online' THEN $4 ELSE mount_path END, updated_at=now()
		 WHERE id=$5`,
		status, le, status, mountPath(id), id)
	if err != nil {
		log.Printf("状态回写失败 %s: %v", id, err)
	}
}

// listActiveMounts 读 /proc/mounts 找 /mnt/storage/* 的挂载。
func listActiveMounts() (map[string]string, error) {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.HasPrefix(f[1], mountRoot+"/") {
			out[strings.TrimPrefix(f[1], mountRoot+"/")] = f[1]
		}
	}
	return out, nil
}

// healthCheck 读挂载点首项（EIO=断连）。FUSE 层自带请求超时，rclone 崩溃后
// readdir 立即 EIO，不会长阻塞；故不用 ctx（os.File 也不支持 ctx 读）。
func healthCheck(mp string) error {
	f, err := os.Open(mp)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	return err
}

// umount 卸载（fusermount3 优先——rclone v1.68+ 用 fuse3；内核 umount 兜底）。
// ⚠️ 必须带超时：stale FUSE 挂载点的 fusermount/umount 可能无限阻塞
//（实测：手动测试残留的半死挂载把 reconcile 循环卡死，无任何日志）。
func umount(mp string) error {
	try := func(name string, args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).Run()
	}
	if err := try("fusermount3", "-u", mp); err == nil {
		return nil
	}
	if err := try("fusermount", "-u", mp); err == nil {
		return nil
	}
	return try("umount", mp)
}

// obscure 用 rclone 的混淆格式处理密码（rclone config 要求）。
func obscure(pass string) (string, error) {
	out, err := exec.Command("rclone", "obscure", pass).Output()
	if err != nil {
		return "", fmt.Errorf("rclone obscure: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
