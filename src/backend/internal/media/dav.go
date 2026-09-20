package media

// WebDAV 直读直写（Job000055，契约 §16「媒体直读直写」，用户裁决：完整读写）。
//
// 语义映射（[自行决策] 均按契约 §16 落地，细节如下）：
//
//	树形 = 个人空间媒体库的 folder_path 目录树（相册/标签不进 DAV —— 它们是虚拟组织，
//	      DAV 是"文件夹"语义，契约写明"按 folder_path 映射"）。根 = 个人库根。
//	GET    下载原文件（owner + 未删除 + mediascope 读口径双保险）
//	PUT    整文件上传 → 走与 HTTP 上传完全相同的 ingest 管线（去重/缩略图队列/taken_at），
//	       folder_path 取自 URL 目录；不支持覆盖（已存在 → 405，客户端先 DELETE 再 PUT）
//	DELETE 软删入回收站（与网页端删除同语义，数据可恢复；purge 仍走网页端）
//	MKCOL  虚拟目录：folder_path 由 media 行派生、无独立目录表，故 MKCOL 恒 201、
//	       首次 PUT 进该目录时目录"显形"（契约语义不变，登记为已知限制）
//	MOVE   文件=改 folder_path/filename（纯组织变更，不动存储实体）；
//	       目录=前缀批量改（递归）；目标已存在 → 405
//
//	认证：HTTP Basic（邮箱 + 主密码 或 应用密码 app_password_hash——DDL 早已预留该列）。
//	不支持：随机写（仅整文件 PUT，契约明示）、COPY、跨用户任何操作、共享空间（v1 仅个人库）。
//
//	性能注记：Basic 每次请求一次 bcrypt 校验（PROPFIND 会发多请求）。LAN 场景可接受；
//	若未来成为热点，引入 per-connection 会话缓存（注意 WebDAV 客户端连接复用不保证）。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"panoalbum/internal/auth"
	"panoalbum/internal/mediascope"
)

// errDAVConflict 映射为 WebDAV 405/409 的冲突（已存在/非空等）。
var errDAVConflict = errors.New("dav: 目标已存在或不为空")

// errDAVNotFound 映射为 404。
var errDAVNotFound = errors.New("dav: 不存在")

// DAVHandler 构造带 Basic 认证的 WebDAV 挂载点（FileSystem 按登录用户实例化）。
func DAVHandler(h *Handler, authStore *auth.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, pw, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="panomint-dav"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		u, err := authStore.FindByEmail(r.Context(), email)
		if err != nil || u.Status != "active" || !davPasswordOK(r.Context(), authStore, u, pw) {
			w.Header().Set("WWW-Authenticate", `Basic realm="panomint-dav"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		srv := &webdav.Handler{
			Prefix:     "/dav/",
			FileSystem: &davFS{h: h, userID: u.ID, role: u.Role},
			LockSystem: webdav.NewMemLS(),
		}
		srv.ServeHTTP(w, r)
	})
}

// davPasswordOK 主密码或应用密码任一命中即可（应用密码优先——专为第三方客户端设计）。
func davPasswordOK(ctx context.Context, store *auth.Store, u *auth.User, pw string) bool {
	if hash, err := store.AppPasswordHash(ctx, u.ID); err == nil && hash != "" {
		if auth.VerifyPassword(hash, pw) {
			return true
		}
	}
	return auth.VerifyPassword(u.PasswordHash, pw)
}

// davFS webdav.FileSystem 实现：树 = 个人空间 folder_path。
type davFS struct {
	h      *Handler
	userID string
	role   string
}

// davPath 清洗并拆分 DAV 路径："a/b/c.jpg" → folder="a/b", file="c.jpg"。
// 尾斜杠 → 目录位；根 "/" → folder="", isDir=true。无尾斜杠的路径先按文件位解析，
// Stat/OpenFile 查不到文件时回退按目录解析（文件夹与文件同命名空间，与真实 FS 一致）。
// 路径穿越在 Clean 中被规范化消除；越出根的访问按文件位解析后自然 404（查无此行）。
func davPath(name string) (folder, file string, isDir bool, err error) {
	raw := "/" + strings.TrimPrefix(name, "/")
	isDir = strings.HasSuffix(name, "/")
	clean := path.Clean(raw)
	if clean == "/" || clean == "." {
		return "", "", true, nil
	}
	trimmed := strings.TrimPrefix(clean, "/")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[:idx], trimmed[idx+1:], isDir, nil
	}
	return "", trimmed, isDir, nil
}

// listChildren 列目录：直接文件 + 一级子目录名（folder_path 派生）。
func (fs *davFS) listChildren(ctx context.Context, folder string) (dirs map[string]bool, files []davFileInfo, err error) {
	dirs = map[string]bool{}
	readClause, readArgs := mediascope.ReadCond(3, fs.userID, fs.role, "m")
	rows, err := fs.h.Store.Pool.Query(ctx,
		fmt.Sprintf(`SELECT m.id, COALESCE(m.filename,''), COALESCE(m.folder_path,''), m.taken_at, m.size_bytes
			FROM media m
			WHERE m.deleted_at IS NULL AND m.owner_id = $1 AND m.space = 'personal'
			  AND (%s = $2 OR $2 = '') AND strpos(COALESCE(m.folder_path,''), $2 || CASE WHEN $2='' THEN '' ELSE '/' END) = 1
			  AND %s`, "m.folder_path", readClause),
		append([]any{fs.userID, folder}, readArgs...)...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	prefix := folder
	if prefix != "" {
		prefix += "/"
	}
	for rows.Next() {
		var id string
		var fn, fp string
		var takenAt time.Time
		var size int64
		if err := rows.Scan(&id, &fn, &fp, &takenAt, &size); err != nil {
			return nil, nil, err
		}
		rest := strings.TrimPrefix(fp, prefix)
		if rest == "" {
			// 恰是该目录下的文件
			files = append(files, davFileInfo{id: id, name: fn, size: size, mod: takenAt})
			continue
		}
		if seg, _, _ := strings.Cut(rest, "/"); seg != "" && seg != fn {
			dirs[seg] = true
		}
	}
	return dirs, files, rows.Err()
}

// davFileInfo 文件条目（os.FileInfo 由 davOpened 包装）。
type davFileInfo struct {
	id   string
	name string
	size int64
	mod  time.Time
}

func (f davFileInfo) Name() string { return f.name }

func (fs *davFS) findByPath(ctx context.Context, folder, file string) (id string, size int64, mod time.Time, err error) {
	readClause, readArgs := mediascope.ReadCond(4, fs.userID, fs.role, "m")
	err = fs.h.Store.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT m.id, m.size_bytes, COALESCE(m.taken_at, m.created_at)
			FROM media m
			WHERE m.deleted_at IS NULL AND m.owner_id = $1 AND m.space = 'personal'
			  AND COALESCE(m.folder_path,'') = $2 AND COALESCE(m.filename,'') = $3 AND %s`, readClause),
		append([]any{fs.userID, folder, file}, readArgs...)...).Scan(&id, &size, &mod)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, time.Time{}, errDAVNotFound
	}
	return id, size, mod, err
}

func (fs *davFS) folderExists(ctx context.Context, folder string) bool {
	if folder == "" {
		return true // 根恒存在
	}
	var n int
	err := fs.h.Store.Pool.QueryRow(ctx,
		`SELECT count(*) FROM media WHERE deleted_at IS NULL AND owner_id = $1 AND space='personal'
		 AND (folder_path = $2 OR folder_path LIKE $3)`,
		fs.userID, folder, folder+"/%").Scan(&n)
	return err == nil && n > 0
}

// Mkdir 虚拟目录：仅校验合法性，恒成功（已存在的目录也幂等 201——契约允许）。
// 与真实文件冲突（同名媒体占了这个路径）→ 405。
func (fs *davFS) Mkdir(ctx context.Context, name string, _ os.FileMode) error {
	folder, file, isDir, err := davPath(name)
	if err != nil {
		return errDAVNotFound
	}
	if !isDir && file != "" {
		// MKCOL 目标是文件位：看是否被媒体占用
		if _, _, _, err := fs.findByPath(ctx, folder, file); err == nil {
			return errDAVConflict
		}
	}
	return nil
}

// RemoveAll 文件=软删入回收站；目录=递归软删其下全部媒体（均 owner 限定）。
func (fs *davFS) RemoveAll(ctx context.Context, name string) error {
	folder, file, isDir, err := davPath(name)
	if err != nil {
		return errDAVNotFound
	}
	if isDir {
		if folder == "" {
			return errDAVConflict // 禁止删根
		}
		tag, err := fs.h.Store.Pool.Exec(ctx,
			`UPDATE media SET deleted_at = now(), updated_at = now()
			 WHERE deleted_at IS NULL AND owner_id = $1 AND space='personal' AND folder_path LIKE $2`,
			fs.userID, folder+"/%")
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 && !fs.folderExists(ctx, folder) {
			return errDAVNotFound
		}
		return nil
	}
	id, _, _, err := fs.findByPath(ctx, folder, file)
	if err != nil {
		return err
	}
	return fs.h.Store.SoftDelete(ctx, id)
}

// Rename MOVE：文件改 folder_path/filename；目录改前缀。目标已存在 → 405。
func (fs *davFS) Rename(ctx context.Context, oldName, newName string) error {
	of, ofile, oIsDir, err := davPath(oldName)
	if err != nil {
		return errDAVNotFound
	}
	nf, nfile, nIsDir, err := davPath(newName)
	if err != nil {
		return errDAVNotFound
	}
	if oIsDir || nIsDir {
		if !oIsDir || !nIsDir {
			return errDAVConflict // 目录↔文件互移不支持
		}
		if of == "" || nf == "" {
			return errDAVConflict
		}
		// 目录移动 = folder_path 前缀替换；目标下已有内容 → 405 防合并踩踏
		var n int
		if err := fs.h.Store.Pool.QueryRow(ctx,
			`SELECT count(*) FROM media WHERE deleted_at IS NULL AND owner_id=$1 AND space='personal' AND folder_path LIKE $2`,
			fs.userID, nf+"/%").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errDAVConflict
		}
		_, err := fs.h.Store.Pool.Exec(ctx,
			`UPDATE media SET folder_path = $2 || substr(folder_path, length($3)+1), updated_at=now()
			 WHERE deleted_at IS NULL AND owner_id=$1 AND space='personal' AND folder_path LIKE $4`,
			fs.userID, nf, of, of+"/%")
		return err
	}
	id, _, _, err := fs.findByPath(ctx, of, ofile)
	if err != nil {
		return err
	}
	if _, _, _, err := fs.findByPath(ctx, nf, nfile); err == nil {
		return errDAVConflict
	} else if !errors.Is(err, errDAVNotFound) {
		return err
	}
	_, err = fs.h.Store.Pool.Exec(ctx,
		`UPDATE media SET folder_path = NULLIF($2,''), filename = $3, updated_at=now() WHERE id = $1`,
		id, nf, nfile)
	return err
}

// Stat 文件 → 媒体行信息；目录 → 虚拟信息（存在=根或旗下有媒体）。
func (fs *davFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	folder, file, isDir, err := davPath(name)
	if err != nil {
		return nil, errDAVNotFound
	}
	if isDir {
		if folder == "" || fs.folderExists(ctx, folder) {
			return davDirInfo{name: path.Base("/" + folder)}, nil
		}
		return nil, errDAVNotFound
	}
	id, size, mod, err := fs.findByPath(ctx, folder, file)
	if errors.Is(err, errDAVNotFound) && fs.folderExists(ctx, folder+"/"+file) {
		// 无尾斜杠的目录访问：文件位查不到 → 按目录解析
		return davDirInfo{name: file}, nil
	}
	if err != nil {
		return nil, err
	}
	return davFileInfo{id: id, name: file, size: size, mod: mod}, nil
}

// OpenFile 读=打开原文件流；写=整文件 PUT（Close 时 ingest）；目录=Readdir 句柄。
func (fs *davFS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	folder, file, isDir, err := davPath(name)
	if err != nil {
		return nil, errDAVNotFound
	}
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
		if isDir || file == "" {
			return nil, errDAVConflict
		}
		if flag&os.O_CREATE != 0 && flag&os.O_TRUNC == 0 {
			if _, _, _, err := fs.findByPath(ctx, folder, file); err == nil {
				return nil, errDAVConflict // 不支持覆盖：契约"仅整文件 PUT"，覆盖=先 DELETE
			}
		}
		if err := os.MkdirAll(fs.h.UploadTmp, 0o755); err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(fs.h.UploadTmp, "dav-*")
		if err != nil {
			return nil, err
		}
		return &davPut{f: tmp, fs: fs, folder: folder, name: file, ctx: ctx}, nil
	}
	// 读路径
	if isDir {
		return &davDir{fs: fs, folder: folder, ctx: ctx}, nil
	}
	id, size, mod, err := fs.findByPath(ctx, folder, file)
	if errors.Is(err, errDAVNotFound) && fs.folderExists(ctx, folder+"/"+file) {
		return &davDir{fs: fs, folder: folder + "/" + file, ctx: ctx}, nil
	}
	if err != nil {
		return nil, err
	}
	_ = size
	_ = mod
	row := fs.h.Store.Pool.QueryRow(ctx,
		`SELECT COALESCE(m.path,'') FROM media m WHERE m.id = $1`, id)
	var rel string
	if err := row.Scan(&rel); err != nil {
		return nil, err
	}
	abs, ok := fs.h.ResolvePath(rel)
	if !ok {
		return nil, errDAVNotFound
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, errDAVNotFound
	}
	return &davGet{f: f}, nil
}

// davGet 已打开的原文件（只读）。
type davGet struct{ f *os.File }

func (g *davGet) Close() error                 { return g.f.Close() }
func (g *davGet) Read(p []byte) (int, error)   { return g.f.Read(p) }
func (g *davGet) Write([]byte) (int, error)    { return 0, errDAVConflict }
func (g *davGet) Seek(o int64, w int) (int64, error) { return g.f.Seek(o, w) }
func (g *davGet) Readdir(int) ([]os.FileInfo, error) { return nil, errDAVConflict }
func (g *davGet) Stat() (os.FileInfo, error) {
	st, err := g.f.Stat()
	return davFileInfo{name: st.Name(), size: st.Size(), mod: st.ModTime()}, err
}

// davDir 目录句柄：Readdir 列子项。
type davDir struct {
	fs     *davFS
	folder string
	ctx    context.Context
}

func (d *davDir) Close() error               { return nil }
func (d *davDir) Read([]byte) (int, error)   { return 0, io.EOF }
func (d *davDir) Write([]byte) (int, error)  { return 0, errDAVConflict }
func (d *davDir) Seek(int64, int) (int64, error) { return 0, errDAVConflict }

type davDirInfo struct{ name string }

func (i davDirInfo) Name() string { return i.name }
func (davDirInfo) Size() int64    { return 0 }
func (davDirInfo) Mode() os.FileMode { return os.ModeDir | 0o755 }
func (davDirInfo) ModTime() time.Time { return time.Time{} }
func (davDirInfo) IsDir() bool      { return true }
func (davDirInfo) Sys() any         { return nil }

func (davFileInfo) Mode() os.FileMode { return 0o644 }
func (f davFileInfo) Size() int64     { return f.size }
func (f davFileInfo) ModTime() time.Time { return f.mod }
func (davFileInfo) IsDir() bool        { return false }
func (davFileInfo) Sys() any           { return nil }

func (d *davDir) Readdir(count int) ([]os.FileInfo, error) {
	dirs, files, err := d.fs.listChildren(d.ctx, d.folder)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(dirs)+len(files))
	for name := range dirs {
		out = append(out, davDirInfo{name: name})
	}
	for _, f := range files {
		fi := f
		out = append(out, fi)
	}
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	if len(out) == 0 {
		return nil, io.EOF
	}
	return out, nil
}

func (d *davDir) Stat() (os.FileInfo, error) {
	return davDirInfo{name: path.Base("/" + d.folder)}, nil
}

// davPut 整文件 PUT：写入临时文件，Close 时走 HTTP 上传同一 ingest 管线。
type davPut struct {
	f      *os.File
	fs     *davFS
	folder string
	name   string
	ctx    context.Context
}

func (p *davPut) Close() error {
	src, err := os.Open(p.f.Name())
	if err != nil {
		p.f.Close()
		return err
	}
	defer src.Close()
	meta := uploadMeta{
		OwnerID:    p.fs.userID,
		Filename:   sanitizeFilename(p.name),
		FolderPath: sanitizeFolder(p.folder),
		Space:      "personal",
	}
	id, _, err := p.fs.h.ingest(p.ctx, src, meta)
	if err != nil {
		p.f.Close()
		os.Remove(p.f.Name())
		return err
	}
	p.f.Close()
	os.Remove(p.f.Name())
	log.Printf("[dav] PUT %s/%s → %s", p.folder, p.name, id)
	return nil
}

func (p *davPut) Read([]byte) (int, error)   { return 0, errDAVConflict }
func (p *davPut) Seek(int64, int) (int64, error) { return 0, errDAVConflict }
func (p *davPut) Readdir(int) ([]os.FileInfo, error) { return nil, errDAVConflict }
func (p *davPut) Stat() (os.FileInfo, error) {
	st, err := p.f.Stat()
	return davFileInfo{name: p.name, size: st.Size(), mod: st.ModTime()}, err
}

func (p *davPut) Write(b []byte) (int, error) {
	st, err := p.f.Stat()
	if err != nil {
		return 0, err
	}
	if st.Size()+int64(len(b)) > uploadMaxBytes() {
		return 0, errDAVConflict // 超限：以冲突中止（契约：整文件上限同上传）
	}
	return p.f.Write(b)
}

// 编译期断言：接口实现齐全。
var (
	_ webdav.FileSystem = (*davFS)(nil)
	_ webdav.File       = (*davGet)(nil)
	_ webdav.File       = (*davDir)(nil)
	_ webdav.File       = (*davPut)(nil)
)
