package transcode

// 转码链路（JobStatus / ServeHLS）的归属校验回归网。
//
// ⚠️ 先说清本组用例**能**证明什么、**不能**证明什么（避免把静态断言当成端到端保证）：
//   - 不能：用真实 HTTP 往返证明"别人的 job/分片取不到" —— 本仓库测试环境没有数据库，
//     Handler.Pool 是 *pgxpool.Pool 具体类型、没有接口可替身，任何需要读库的分支都跑不到。
//   - 能：1) 归属谓词本身（纯函数）的完整真值表；
//     2) 源码形状：两个 handler 都必须调用该谓词、且拒绝分支是 404 且排在吐字节之前；
//     3) 行为：库不可达（鉴权无法完成）时**绝不**把磁盘上的分片内容吐出去（fail-closed）；
//     4) 路径穿越防护没有被这次改动削掉 —— 那是另一条安全边界。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ownerUUID  = "11111111-1111-1111-1111-111111111111"
	otherUUID  = "22222222-2222-2222-2222-222222222222"
	mediaUUID  = "3f3d7bad-6795-4e48-a039-79c44b206c67"
	jobUUID    = "9a7b6c5d-4e3f-4a2b-9c8d-7e6f5a4b3c2d"
	hlsRelPath = "/transcode/hls/:id/*file"
)

func transCtx(userID, role string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", userID)
	c.Set("role", role)
	return c
}

func transReq(h *Handler, userID, role, method, path string, register func(*gin.Engine)) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("role", role)
	})
	register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// unreachablePool 指向一个必然连不上的地址（端口 1）：用来断言"鉴权无法完成时不放行"。
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("构造 pool 失败（本用例只用它连不上这一性质）: %v", err)
	}
	return pool
}

// ---- 1. 谓词本身 ----

func TestCanAccessMediaOnlyOwnerOrAdminRole(t *testing.T) {
	cases := []struct {
		name, user, role string
		want             bool
	}{
		{"本人", ownerUUID, "viewer", true},
		{"owner 角色（非本人）", otherUUID, "owner", true},
		{"admin 角色（非本人）", otherUUID, "admin", true},
		{"他人 viewer", otherUUID, "viewer", false},
		{"他人 member", otherUUID, "member", false},
		{"无主体", "", "viewer", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canAccessMedia(transCtx(tc.user, tc.role), ownerUUID); got != tc.want {
				t.Fatalf("canAccessMedia(user=%q role=%q, owner=%q) = %v，want %v",
					tc.user, tc.role, ownerUUID, got, tc.want)
			}
		})
	}
}

// ---- 2. 源码形状 ----

// methodBody 抠出某个 Handler 方法的函数体（到下一个顶层 func 为止）。
func methodBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func (h *Handler) "+name+"(")
	if start < 0 {
		t.Fatalf("源码里找不到 func (h *Handler) %s —— 方法被改名或删除？", name)
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

func TestJobStatusAndServeHLSScopedToMediaOwner(t *testing.T) {
	b, err := os.ReadFile("transcode.go")
	if err != nil {
		t.Fatalf("读不到 transcode.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	// 前提自证：CreateJob 是本文件里唯一**既有**的归属校验，先证明抠函数体有效。
	create := methodBody(t, src, "CreateJob")
	if !strings.Contains(create, "canAccessMedia(c, ownerID)") ||
		!strings.Contains(create, "FROM media WHERE id = $1") {
		t.Fatal("前提失效：CreateJob 里找不到既有的媒体归属校验（它是本次对齐的基准）")
	}
	// CreateJob 的对外行为不变：仍返回 403（本次只收读取侧）。
	if !strings.Contains(create, "http.StatusForbidden") {
		t.Fatal("CreateJob 的 403 行为被改动：本次只收紧 JobStatus/ServeHLS，不应顺手改写入侧")
	}

	deny404 := regexp.MustCompile(`errJSON\(c,\s*http\.StatusNotFound,\s*"NOT_FOUND"`)

	// --- JobStatus ---
	js := methodBody(t, src, "JobStatus")
	if !strings.Contains(js, "JOIN media") {
		t.Fatalf("JobStatus 必须 JOIN media 取属主（transcode_jobs 没有 owner 列）:\n%s", js)
	}
	if !strings.Contains(js, "canAccessMedia(c, ownerID)") {
		t.Fatalf("JobStatus 缺少归属校验 canAccessMedia(c, ownerID):\n%s", js)
	}
	iManage := strings.Index(js, "if !canAccessMedia(c, ownerID)")
	iDeny := deny404.FindStringIndex(js[iManage:])
	if iDeny == nil {
		t.Fatalf("JobStatus 的归属拒绝分支不是 404 NOT_FOUND（无权不得给出 403 探测判据）:\n%s", js)
	}
	if iOK := strings.Index(js, "c.JSON(http.StatusOK, j)"); iOK < 0 || iManage+iDeny[0] > iOK {
		t.Fatalf("JobStatus 的拒绝分支必须排在 200 响应之前:\n%s", js)
	}

	// --- ServeHLS ---
	hls := methodBody(t, src, "ServeHLS")
	if !strings.Contains(hls, "SELECT owner_id FROM media WHERE id = $1") {
		t.Fatalf("ServeHLS 必须按媒体 id 查 media.owner_id（与 CreateJob/JobStatus 同口径）:\n%s", hls)
	}
	if !strings.Contains(hls, "canAccessMedia(c, ownerID)") {
		t.Fatalf("ServeHLS 缺少归属校验 canAccessMedia(c, ownerID):\n%s", hls)
	}
	iOwn := strings.Index(hls, "canAccessMedia(c, ownerID)")
	iFile := strings.Index(hls, "c.File(full)")
	if iOwn < 0 || iFile < 0 || iOwn > iFile {
		t.Fatalf("归属校验必须排在吐出字节（c.File）之前:\n%s", hls)
	}
	if !strings.Contains(hls, `errJSON(c, http.StatusNotFound, "NOT_FOUND", "HLS 文件不存在")`) {
		t.Fatalf("ServeHLS 的无权分支必须与\"文件不存在\"同形状（404 NOT_FOUND）:\n%s", hls)
	}

	// 路径穿越防护是**另一条**安全边界，不能被这次改动削弱或合并。
	for _, want := range []string{
		`!uuidRe.MatchString(id)`,
		`strings.HasPrefix(rel, "..")`,
		`base, _ := filepath.Abs(filepath.Join(h.HLSDir, id))`,
		`!strings.HasPrefix(full, base+string(os.PathSeparator))`,
	} {
		if !strings.Contains(hls, want) {
			t.Fatalf("ServeHLS 的路径穿越防护被削弱：找不到 %q", want)
		}
	}

	// 自证：坏版本（无鉴权直接 File）必须让上面的断言落空。
	broken := methodBody(t, "func (h *Handler) ServeHLS(c *gin.Context) {\n\tc.File(\"x\")\n}\n", "ServeHLS")
	if strings.Contains(broken, "canAccessMedia") || strings.Contains(broken, "SELECT owner_id FROM media") {
		t.Fatal("守卫失效：本断言在\"无鉴权\"的版本上也会通过，拦不住回归")
	}
}

// ---- 3. 行为：鉴权无法完成时绝不吐字节 ----

// TestServeHLSNeverServesWithoutOwnershipCheck 磁盘上真的有分片，但库不可达 ⇒ 鉴权无法完成。
// 此时绝不允许 200，更不允许吐出分片内容（fail-closed）。
func TestServeHLSNeverServesWithoutOwnershipCheck(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, mediaUUID), 0o755); err != nil {
		t.Fatalf("造 HLS 目录失败: %v", err)
	}
	const secret = "SECRET-MANIFEST-BYTES"
	if err := os.WriteFile(filepath.Join(dir, mediaUUID, "master.m3u8"), []byte(secret), 0o644); err != nil {
		t.Fatalf("造分片失败: %v", err)
	}

	pool := unreachablePool(t)
	defer pool.Close()
	h := &Handler{Pool: pool, HLSDir: dir}

	rec := transReq(h, otherUUID, "viewer", http.MethodGet,
		"/transcode/hls/"+mediaUUID+"/master.m3u8",
		func(r *gin.Engine) { r.GET(hlsRelPath, h.ServeHLS) })

	if rec.Code == http.StatusOK {
		t.Fatalf("库不可达时鉴权无法完成，绝不能放行（实际 %d）", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("鉴权失败路径吐出了分片内容 —— 这正是本次要堵的越权读取")
	}
}

// TestJobStatusNeverReturns200WithoutOwnershipCheck 同一 fail-closed 性质：库不可达时不得 200。
func TestJobStatusNeverReturns200WithoutOwnershipCheck(t *testing.T) {
	pool := unreachablePool(t)
	defer pool.Close()
	h := &Handler{Pool: pool}

	rec := transReq(h, otherUUID, "viewer", http.MethodGet, "/transcode/job/"+jobUUID,
		func(r *gin.Engine) { r.GET("/transcode/job/:id", h.JobStatus) })
	if rec.Code == http.StatusOK {
		t.Fatalf("库不可达时不得返回 200，实际 %d", rec.Code)
	}
}
