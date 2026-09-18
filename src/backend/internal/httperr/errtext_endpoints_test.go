package httperr_test

// 端到端行为守卫：**真实的 pgconn.PgError** 打到真实 HTTP 处理器上，响应体不得回显原文。
//
// 为什么用"假 PG"而不是真库：本机/CI 都没有 PostgreSQL（5432 未监听），而"库连不上"
// 只能得到 *pgconn.ConnectError（host/port/dial），**拿不到带 SQLSTATE 的 PgError**。
// 于是这里起一个只做一件事的 TCP 服务：读完启动包后回一个 PostgreSQL **ErrorResponse**
// 帧（'E'）。pgx 会把它解析成真正的 *pgconn.PgError 交给 handler —— 这正是生产里
// "畸形 id → 22P02"、"表不存在 → 42P01"、"外键冲突 → 23503" 的那类错误。
//
// 断言的是**响应体**（客户端能看到的东西），不是错误类型：只要 handler 还在回显
// err.Error()，body 里就会出现 22P02 / SQLSTATE / invalid input / 表名，本用例必红。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/albums"
	"panoalbum/internal/folders"
	"panoalbum/internal/geo"
	"panoalbum/internal/media"
	"panoalbum/internal/search"
	"panoalbum/internal/shares"
	"panoalbum/internal/spaces"
)

const testUserID = "11111111-1111-1111-1111-111111111111"

// fakePGErrors 三种真实 PG 报错形状（SQLSTATE 与文案都按 Postgres 实际输出构造）。
var fakePGErrors = []struct {
	code    string
	message string
}{
	{"22P02", `invalid input syntax for type uuid: "not-a-uuid"`},
	{"42P01", `relation "media" does not exist`},
	{"23503", `insert or update on table "share_links" violates foreign key constraint "share_links_target_id_fkey"`},
}

// leakMarkers 回显原文时必然出现的标记（大小写不敏感）。
var leakMarkers = []string{
	"22p02", "42p01", "23503", "sqlstate",
	"invalid input", "uuid", "relation", "does not exist",
	"foreign key", "constraint", "share_links", "media",
	"pq:", "pgx",
}

// startFakePG 起一个"读完启动包就回 ErrorResponse"的假 PostgreSQL，返回其 DSN。
func startFakePG(t *testing.T, code, message string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				// 1) 读启动包：int32 长度 + 其余负载
				var hdr [4]byte
				if _, err := io.ReadFull(c, hdr[:]); err != nil {
					return
				}
				n := int(binary.BigEndian.Uint32(hdr[:]))
				if n < 4 || n > 1<<20 {
					return
				}
				if _, err := io.ReadFull(c, make([]byte, n-4)); err != nil {
					return
				}
				// 2) 回 ErrorResponse 帧
				var body bytes.Buffer
				field := func(k byte, v string) {
					body.WriteByte(k)
					body.WriteString(v)
					body.WriteByte(0)
				}
				field('S', "ERROR")
				field('V', "ERROR")
				field('C', code)
				field('M', message)
				field('F', "executor.c")
				field('L', "1234")
				field('R', "exec_simple_query")
				body.WriteByte(0) // 字段终止符

				var out bytes.Buffer
				out.WriteByte('E')
				var lnb [4]byte
				binary.BigEndian.PutUint32(lnb[:], uint32(body.Len()+4))
				out.Write(lnb[:])
				out.Write(body.Bytes())
				_, _ = c.Write(out.Bytes())
			}(conn)
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("解析假 PG 地址失败: %v", err)
	}
	// sslmode=disable：不先发 SSLRequest，直接进启动包。
	return "postgres://u:p@127.0.0.1:" + port + "/none?sslmode=disable"
}

func newFakePool(t *testing.T, code, message string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), startFakePG(t, code, message))
	if err != nil {
		t.Fatalf("构造 pool 失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// endpoint 一个待验证的端点。
type endpoint struct {
	name     string
	path     string
	register func(r *gin.Engine, pool *pgxpool.Pool)
}

func endpoints() []endpoint {
	return []endpoint{
		{
			name: "GET /spaces",
			path: "/spaces",
			register: func(r *gin.Engine, pool *pgxpool.Pool) {
				h := &spaces.Handler{Pool: pool}
				r.GET("/spaces", h.Get)
			},
		},
		{
			name: "GET /folders/tree",
			path: "/folders/tree",
			register: func(r *gin.Engine, pool *pgxpool.Pool) {
				h := &folders.Handler{Pool: pool}
				r.GET("/folders/tree", h.Tree)
			},
		},
		{
			name: "GET /albums",
			path: "/albums",
			register: func(r *gin.Engine, pool *pgxpool.Pool) {
				h := &albums.Handler{Store: &albums.Store{Pool: pool}}
				r.GET("/albums", h.List)
			},
		},
		{
			name: "GET /shares",
			path: "/shares",
			register: func(r *gin.Engine, pool *pgxpool.Pool) {
				h := &shares.Handler{Store: &shares.Store{Pool: pool}}
				r.GET("/shares", h.List)
			},
		},
		{
			name: "GET /media/trash",
			path: "/media/trash",
			register: func(r *gin.Engine, pool *pgxpool.Pool) {
				h := &media.Handler{Store: &media.Store{Pool: pool}}
				r.GET("/media/trash", h.Trash)
			},
		},
		{
			name: "GET /geo/clusters",
			path: "/geo/clusters?min_lng=0&min_lat=0&max_lng=1&max_lat=1&zoom=4&kind=all",
			register: func(r *gin.Engine, pool *pgxpool.Pool) {
				h := &geo.Handler{Media: &geo.MediaStore{Pool: pool}}
				r.GET("/geo/clusters", h.Clusters)
			},
		},
	}
}

func TestEndpointsDoNotEchoPgError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, ep := range endpoints() {
		for _, fe := range fakePGErrors {
			t.Run(ep.name+"/"+fe.code, func(t *testing.T) {
				pool := newFakePool(t, fe.code, fe.message)
				r := gin.New()
				r.Use(func(c *gin.Context) {
					c.Set("user_id", testUserID)
					c.Set("role", "owner")
					c.Next()
				})
				ep.register(r, pool)

				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ep.path, nil))

				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("真故障（%s）应为 500，实际 %d: %s", fe.code, rec.Code, rec.Body.String())
				}
				var body struct {
					Error struct{ Code, Message string } `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("响应不是错误封套: %v (%s)", err, rec.Body.String())
				}
				if body.Error.Code != "QUERY_FAILED" || body.Error.Message != "查询失败" {
					t.Fatalf("应为 QUERY_FAILED/查询失败，实际 %q/%q", body.Error.Code, body.Error.Message)
				}
				low := strings.ToLower(rec.Body.String())
				for _, m := range leakMarkers {
					if strings.Contains(low, m) {
						t.Fatalf("%s：响应体回显了 DB 原文标记 %q —— 客户端不该看到实现细节：%s",
							ep.name, m, rec.Body.String())
					}
				}
			})
		}
	}
}

// TestFakePGReturnsRealPgError 是 TestEndpointsDoNotEchoPgError 的**前提自证**：
// 若假 PG 没能让 pgx 产出带 SQLSTATE 的真错误，那"响应体不含标记"就可能是因为
// 压根没有标记可漏 —— 本用例直接检查 pgx 返回的错误原文里确实带这些标记。
func TestFakePGReturnsRealPgError(t *testing.T) {
	pool := newFakePool(t, "22P02", `invalid input syntax for type uuid: "not-a-uuid"`)
	var s string
	err := pool.QueryRow(context.Background(), `SELECT 1`).Scan(&s)
	if err == nil {
		t.Fatal("假 PG 应当让查询失败")
	}
	msg := strings.ToLower(err.Error())
	for _, m := range []string{"22p02", "sqlstate", "invalid input", "uuid"} {
		if !strings.Contains(msg, m) {
			t.Fatalf("前提失效：假 PG 返回的错误原文里没有 %q，无法证明端点确实有原文可漏：%v", m, err)
		}
	}
	t.Logf("假 PG 产生的真实驱动错误原文：%v", err)
}

// unreachablePool 指向必然连不上的地址；只用于「验证 DB 之前就该返回」的分支。
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("构造 pool 失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestGeoHistogramInvalidGranularityStill400 钉住**被保留下来的可区分语义**：
// 粒度非法是本端校验文案，必须继续是 400/INVALID_PARAMS + 原文案，
// 不能被"统一成 500/查询失败"（那会把用户参数写错伪装成服务故障）。
func TestGeoHistogramInvalidGranularityStill400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 这个分支在触库之前就返回，pool 只用它"能构造"这一性质。
	h := &geo.Handler{Media: &geo.MediaStore{Pool: unreachablePool(t)}}
	r := gin.New()
	r.GET("/geo/histogram", h.Histogram)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/geo/histogram?min_lng=0&min_lat=0&max_lng=1&max_lat=1&granularity=hour", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("粒度非法应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是错误封套: %v (%s)", err, rec.Body.String())
	}
	if body.Error.Code != "INVALID_PARAMS" || body.Error.Message != "granularity 仅支持 year|month|day" {
		t.Fatalf("粒度非法应保留本端文案 INVALID_PARAMS/granularity 仅支持 year|month|day，实际 %q/%q",
			body.Error.Code, body.Error.Message)
	}
}

// TestSearchInvalidCursorStill400 钉住**被保留下来的可区分语义**：
// 非法游标是本端校验文案，必须继续是 400/INVALID_CURSOR + 原文案。
// 游标在触库之前解析（store.go 的 query），故用不可达 pool 即可走到该分支。
func TestSearchInvalidCursorStill400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &search.Handler{Store: &search.Store{Pool: unreachablePool(t)}}
	r := gin.New()
	r.GET("/search", h.Search)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/search?cursor=garbage", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法游标应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是错误封套: %v (%s)", err, rec.Body.String())
	}
	if body.Error.Code != "INVALID_CURSOR" || body.Error.Message != "无效游标" {
		t.Fatalf("非法游标应保留本端文案 INVALID_CURSOR/无效游标，实际 %q/%q",
			body.Error.Code, body.Error.Message)
	}
}
