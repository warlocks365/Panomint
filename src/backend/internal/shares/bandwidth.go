package shares

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"panoalbum/internal/httperr"
)

// Phase 4 P1：分享带宽自测 + ABR 初档（契约 §「分享页带宽自测」/§「带宽自测」）。
//
// 为什么是 3 个请求（探针下行、探针上行、汇总落库）：
// 契约的 POST /…/bandwidth-test 是**无 body** 且返回 JSON {up,down,latency} 的汇总端点。
// 而「上行容量」本质上必须由客户端把字节送上来才能测——与「无 body」不可兼得。
// 因此拆成：下行探针（服务端写、服务端计时）→ 上行探针（客户端写、服务端读计时）
// → 汇总端点（无 body，读两次探针的缓存结果并落库）。总请求数 3，仍属「探针请求数要少」。
//
// 探针在带宽上界内**自限**：单方向 ≤1MiB，单次 ≤BW_PROBE_MAX_MS（默认 8s）提前结束。

const (
	defaultProbeBytes = 256 << 10 // 单方向默认字节数（256KiB）
	minProbeBytes     = 64 << 10  // 64KiB 以下测不出量级，无意义
	maxProbeBytes     = 1 << 20   // 单方向上界 1MiB：防长挂与流量浪费
	defaultProbeMaxMs = 8000      // 单次探针总耗时上限
	probeFloorMs      = 5         // 计时下限，防「耗时趋零 → 速率无穷大」
	maxReportedKbps   = 5_000_000 // 上报上限 5Gbps：反代/环回缓冲会让结果虚高，需封顶
	probeSampleTTL    = 2 * time.Minute
)

// probeBuf 固定大小的随机字节（进程内生成一次）。下行探针用它做载荷，
// 避免每次请求都跑 crypto/rand，也保证内容不可压缩（真测链路而非测压缩率）。
var probeBuf = func() []byte {
	b := make([]byte, maxProbeBytes)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i*31 + 7)
		}
	}
	return b
}()

// ---- 探针参数（环境变量可调，模式与 handlers.go 里 THUMB_DIR 一致） ----

// probeSize 本次探针字节数：?bytes= 优先，其次 BW_PROBE_BYTES，最后默认 256KiB。
// 一律夹在 [64KiB, 1MiB]。
func probeSize(c *gin.Context) int {
	if v := c.Query("bytes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return clampInt(n, minProbeBytes, maxProbeBytes)
		}
	}
	if v := os.Getenv("BW_PROBE_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return clampInt(n, minProbeBytes, maxProbeBytes)
		}
	}
	return defaultProbeBytes
}

// probeDeadline 探针总耗时上限（BW_PROBE_MAX_MS，默认 8s）；到点即提前结束，只统计已传字节。
func probeDeadline() time.Duration {
	if v := os.Getenv("BW_PROBE_MAX_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultProbeMaxMs * time.Millisecond
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// kbpsOf 字节数 / 耗时 → kbps，夹在 [1, 5Gbps]。
func kbpsOf(n int, d time.Duration) int {
	if n <= 0 {
		return 0
	}
	if floor := time.Duration(probeFloorMs) * time.Millisecond; d < floor {
		d = floor
	}
	return clampInt(int(math.Round(float64(n)*8/d.Seconds()/1000)), 1, maxReportedKbps)
}

// ---- 探针结果的进程内短时缓存 ----
//
// 作用只有一个：把「探针请求」与之后的「汇总请求」串起来（后者无 body，无法自己传字节）。
// TTL 极短、不持久化、不跨进程；本部署为单副本容器（docker-compose 的 api 服务）。
type probeSample struct {
	up, down, lat int
	at            time.Time
}

var probeSamples = struct {
	sync.Mutex
	m map[string]probeSample
}{m: make(map[string]probeSample)}

// putProbe 合并写入（只覆盖非零分量）：
// 下行探针只带 down、上行探针只带 up，先到的不该被后到的清零。
func putProbe(key string, up, down, lat int) {
	probeSamples.Lock()
	defer probeSamples.Unlock()
	now := time.Now()
	prev := probeSamples.m[key]
	if prev.at.IsZero() || now.Sub(prev.at) > probeSampleTTL {
		prev = probeSample{}
	}
	if up > 0 {
		prev.up = up
	}
	if down > 0 {
		prev.down = down
	}
	if lat > 0 {
		prev.lat = lat
	}
	prev.at = now
	for k, v := range probeSamples.m { // 惰性清理，避免无界增长
		if now.Sub(v.at) > probeSampleTTL {
			delete(probeSamples.m, k)
		}
	}
	probeSamples.m[key] = prev
}

func takeProbe(key string) (probeSample, bool) {
	probeSamples.Lock()
	defer probeSamples.Unlock()
	s, ok := probeSamples.m[key]
	if !ok || time.Since(s.at) > probeSampleTTL {
		return probeSample{}, false
	}
	delete(probeSamples.m, key)
	return s, true
}

func shareProbeKey(shareID, ip string) string { return "s:" + shareID + ":" + ip }
func userProbeKey(userID, ip string) string   { return "u:" + userID + ":" + ip }

// ---- 探针端点 ----

// PublicProbeDown GET /public/shares/:token/bandwidth-probe?bytes=N
// 下行探针：服务端向访客连写 N 字节随机数据并计时（token 鉴权 + 密码/有效期约束）。
func (h *Handler) PublicProbeDown(c *gin.Context) {
	sh := h.guardPublic(c)
	if sh == nil {
		return
	}
	h.probeDown(c, shareProbeKey(sh.ID, c.ClientIP()))
}

// ProbeDown GET /bandwidth/probe?bytes=N（登录态；与分享版共用同一实现）
func (h *Handler) ProbeDown(c *gin.Context) {
	h.probeDown(c, userProbeKey(c.GetString("user_id"), c.ClientIP()))
}

// probeDown 下行探针实现。
//
// X-Accel-Buffering: no 是关键：nginx 默认缓冲上游响应，若不关，这里测到的只是
// api→nginx 的环回速度而非访客下行。该响应头令 nginx 边收边转（与 SSE 同款机制），
// 于是写操作被访客侧 TCP 接收窗口压住，计时才反映真实下行。
func (h *Handler) probeDown(c *gin.Context, key string) {
	n := probeSize(c)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Header("Content-Length", strconv.Itoa(n))
	c.Status(http.StatusOK)

	start := time.Now()
	deadline := start.Add(probeDeadline())
	src := bytes.NewReader(probeBuf[:n])
	buf := make([]byte, 32<<10)
	written := 0
	for written < n && time.Now().Before(deadline) {
		chunk := buf
		if rem := n - written; rem < len(chunk) {
			chunk = chunk[:rem]
		}
		m, rerr := src.Read(chunk)
		if m > 0 {
			w, werr := c.Writer.Write(chunk[:m])
			written += w
			if werr != nil {
				break
			}
		}
		if rerr != nil {
			break
		}
	}
	c.Writer.Flush()
	putProbe(key, 0, kbpsOf(written, time.Since(start)), 0)
}

// PublicProbeUp POST /public/shares/:token/bandwidth-probe?bytes=N
// 上行探针：读并丢弃访客上传的随机字节并计时（token 鉴权 + 密码/有效期约束）。
func (h *Handler) PublicProbeUp(c *gin.Context) {
	sh := h.guardPublic(c)
	if sh == nil {
		return
	}
	h.probeUp(c, shareProbeKey(sh.ID, c.ClientIP()))
}

// ProbeUp POST /bandwidth/probe?bytes=N（登录态；与分享版共用同一实现）
func (h *Handler) ProbeUp(c *gin.Context) {
	h.probeUp(c, userProbeKey(c.GetString("user_id"), c.ClientIP()))
}

// probeUp 上行探针实现：读请求体计时，首字节到达时刻作为 RTT 近似。
//
// 注意 nginx 的 proxy_request_buffering 默认开启：它会把整个请求体先缓冲再转给 api，
// 此时这里的计时会虚高（值会被 maxReportedKbps 封顶）。生产应给探针 location 关掉该缓冲
// （见交付说明的 nginx 规则）；即使未关也不影响功能——ABR 只用下行。
func (h *Handler) probeUp(c *gin.Context, key string) {
	n := probeSize(c)
	start := time.Now()
	deadline := start.Add(probeDeadline())
	buf := make([]byte, 32<<10)
	got := 0
	var firstByte time.Duration
	sawFirst := false
	for got < n && time.Now().Before(deadline) {
		chunk := buf
		if rem := n - got; rem < len(chunk) {
			chunk = chunk[:rem]
		}
		r, err := c.Request.Body.Read(chunk)
		if r > 0 {
			if !sawFirst {
				firstByte, sawFirst = time.Since(start), true
			}
			got += r
		}
		if err != nil {
			break
		}
	}
	lat := 0
	if sawFirst {
		lat = clampInt(int(firstByte/time.Millisecond), 0, 60_000)
	}
	up := kbpsOf(got, time.Since(start))
	putProbe(key, up, 0, lat)
	c.JSON(http.StatusOK, gin.H{"received_bytes": got, "up_kbps": up, "latency_ms": lat})
}

// ---- 汇总端点 ----

// aggregateProbe 汇总一次自测：探针缓存为主，浏览器实测提示（?down_kbps=&latency_ms=）优先。
//
// 为何让浏览器提示优先：下行容量与 RTT 的真实端点只有访客自己知道；反向代理链上任何一层
// 缓冲（nginx / cloudflared）都会让服务端计时偏乐观。服务端的自测值作为无提示时的兜底。
// 上行不接受浏览器的值——只有服务端读请求体才能测。
func aggregateProbe(c *gin.Context, key string) (up, down, lat int) {
	if s, ok := takeProbe(key); ok {
		up, down, lat = s.up, s.down, s.lat
	}
	if v, err := strconv.Atoi(c.Query("down_kbps")); err == nil && v > 0 {
		down = clampInt(v, 1, maxReportedKbps)
	}
	if v, err := strconv.Atoi(c.Query("latency_ms")); err == nil && v >= 0 {
		lat = clampInt(v, 0, 60_000)
	}
	return up, down, lat
}

// PublicBandwidthTest POST /public/shares/:token/bandwidth-test
//
// 契约的 /s/:token/bandwidth-test：分享场景专用带宽自测，以 token 鉴权，遵守密码与有效期。
// 请求无 body；响应 {up_kbps, down_kbps, latency_ms}；结果写入 bandwidth_profiles
// （scope=share_token, ref_id=share_links.id, source=self_test）并追加 bandwidth_tests。
// 360 播放页据此在 HLS 多档中选取初始档。
func (h *Handler) PublicBandwidthTest(c *gin.Context) {
	sh := h.guardPublic(c)
	if sh == nil {
		return
	}
	up, down, lat := aggregateProbe(c, shareProbeKey(sh.ID, c.ClientIP()))
	// 一次有效测量都没有（探针全失败且浏览器也没给提示）：不落脏数据，直接回零，
	// 前端据此回落 hls.js 默认 ABR。
	if up == 0 && down == 0 && lat == 0 {
		c.JSON(http.StatusOK, gin.H{"up_kbps": 0, "down_kbps": 0, "latency_ms": 0})
		return
	}
	refID := sh.ID
	if err := h.Store.SaveSelfTest(c.Request.Context(),
		bandwidthScope{Scope: "share_token", RefID: &refID}, up, down, lat); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "SAVE_FAILED", "保存失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"up_kbps": up, "down_kbps": down, "latency_ms": lat})
}

// SelfTest POST /bandwidth/self-test（需登录）
// 登录态带宽自测：与分享版**共用**探针端点（/bandwidth/probe）与落库实现，
// 写入 scope=global 的 self_test profile + bandwidth_tests。
func (h *Handler) SelfTest(c *gin.Context) {
	up, down, lat := aggregateProbe(c, userProbeKey(c.GetString("user_id"), c.ClientIP()))
	if up == 0 && down == 0 && lat == 0 {
		c.JSON(http.StatusOK, gin.H{"up_kbps": 0, "down_kbps": 0, "latency_ms": 0})
		return
	}
	if err := h.Store.SaveSelfTest(c.Request.Context(), bandwidthScope{Scope: "global"}, up, down, lat); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "SAVE_FAILED", "保存失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"up_kbps": up, "down_kbps": down, "latency_ms": lat})
}

// GetBandwidth GET /bandwidth（需登录，media:read）
// 当前生效带宽：手动指定优先，否则最近一次自测；都无则 source=none（前端回落默认 ABR）。
func (h *Handler) GetBandwidth(c *gin.Context) {
	up, down, source, err := h.Store.EffectiveBandwidth(c.Request.Context(), bandwidthScope{Scope: "global"})
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"up_kbps": up, "down_kbps": down, "source": source})
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// PatchBandwidth PATCH /bandwidth（需登录，写权限）
// 手动指定带宽 → bandwidth_profiles(source=manual)。360 播放页据此向
// /transcode/hls/:id/master.m3u8 选取档位。up_kbps/down_kbps 至少给一个。
// scope=share_token 时必须给 ref_id（share_links.id），且仅分享创建者或 owner/admin 可写。
func (h *Handler) PatchBandwidth(c *gin.Context) {
	var req struct {
		UpKbps   *int    `json:"up_kbps"`
		DownKbps *int    `json:"down_kbps"`
		Scope    string  `json:"scope"`
		RefID    *string `json:"ref_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	if req.Scope == "" {
		req.Scope = "global"
	}
	if req.Scope != "global" && req.Scope != "share_token" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "scope 仅支持 global|share_token")
		return
	}
	if req.UpKbps == nil && req.DownKbps == nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "至少提供 up_kbps 或 down_kbps")
		return
	}
	if (req.UpKbps != nil && (*req.UpKbps <= 0 || *req.UpKbps > maxReportedKbps)) ||
		(req.DownKbps != nil && (*req.DownKbps <= 0 || *req.DownKbps > maxReportedKbps)) {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "带宽必须为 1.."+strconv.Itoa(maxReportedKbps)+" 的整数 kbps")
		return
	}

	sc := bandwidthScope{Scope: req.Scope}
	if req.Scope == "share_token" {
		if req.RefID == nil || !uuidRe.MatchString(*req.RefID) {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "scope=share_token 时必须提供合法 ref_id（share_links.id）")
			return
		}
		sh, err := h.Store.GetByID(c.Request.Context(), *req.RefID)
		if errors.Is(err, ErrNotFound) {
			errResp(c, http.StatusNotFound, "NOT_FOUND", "分享不存在")
			return
		}
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		role := c.GetString("role")
		if c.GetString("user_id") != sh.OwnerID && role != "owner" && role != "admin" {
			errResp(c, http.StatusForbidden, "FORBIDDEN", "仅分享创建者或管理员可指定该分享的带宽")
			return
		}
		sc.RefID = req.RefID
	}

	if err := h.Store.SaveManual(c.Request.Context(), sc, req.UpKbps, req.DownKbps); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "SAVE_FAILED", "保存失败", err)
		return
	}
	up, down, source, err := h.Store.EffectiveBandwidth(c.Request.Context(), sc)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"scope": sc.Scope, "ref_id": sc.RefID,
		"up_kbps": up, "down_kbps": down, "source": source,
	})
}

// ---- 存储层 ----

// bandwidthScope 带宽配置的归属：(scope, ref_id)。ref_id 仅在 scope=share_token 时非空。
type bandwidthScope struct {
	Scope string  // global|share_token
	RefID *string // share_links.id
}

// scopeFilter 生成 (scope, ref_id) 的 WHERE 片段与参数。
// ref_id 可空，故 NULL 走 IS NULL 分支，避免无类型参数导致 PG 推断失败。
func scopeFilter(sc bandwidthScope) (string, []any) {
	if sc.RefID == nil {
		return "scope = $1 AND ref_id IS NULL", []any{sc.Scope}
	}
	return "scope = $1 AND ref_id = $2", []any{sc.Scope, *sc.RefID}
}

// SaveSelfTest 写一次自测：upsert self_test profile + 追加一条 bandwidth_tests（同事务）。
// 表结构无 (scope,ref_id,source) 唯一约束，故先查后写；source 参与匹配，
// 使 manual 行与 self_test 行互不覆盖（同一 scope 最多两行）。
func (s *Store) SaveSelfTest(ctx context.Context, sc bandwidthScope, upKbps, downKbps, latencyMs int) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	where, args := scopeFilter(sc)
	args = append(args, "self_test")
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM bandwidth_profiles WHERE `+where+
		fmt.Sprintf(" AND source = $%d ORDER BY updated_at DESC LIMIT 1", len(args)), args...).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		err = tx.QueryRow(ctx, `INSERT INTO bandwidth_profiles (scope, ref_id, up_kbps, down_kbps, source)
			VALUES ($1, $2::uuid, $3, $4, 'self_test') RETURNING id`,
			sc.Scope, sc.RefID, upKbps, downKbps).Scan(&id)
	case err != nil:
		return err
	default:
		_, err = tx.Exec(ctx, `UPDATE bandwidth_profiles SET up_kbps = $2, down_kbps = $3, source = 'self_test'
			WHERE id = $1`, id, upKbps, downKbps)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bandwidth_tests (profile_id, up_kbps, down_kbps, latency_ms)
		VALUES ($1, $2, $3, $4)`, id, upKbps, downKbps, latencyMs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SaveManual 手动指定带宽（source=manual）。nil 表示该方向不改动。
func (s *Store) SaveManual(ctx context.Context, sc bandwidthScope, upKbps, downKbps *int) error {
	if upKbps == nil && downKbps == nil {
		return nil
	}
	where, args := scopeFilter(sc)
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id FROM bandwidth_profiles WHERE `+where+
		" AND source = 'manual' ORDER BY updated_at DESC LIMIT 1", args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = s.Pool.Exec(ctx, `INSERT INTO bandwidth_profiles (scope, ref_id, up_kbps, down_kbps, source)
			VALUES ($1, $2::uuid, $3::int, $4::int, 'manual')`, sc.Scope, sc.RefID, upKbps, downKbps)
		return err
	}
	if err != nil {
		return err
	}
	// 只更新显式给出的方向（动态拼 SET，避免 COALESCE($n, col) 的参数类型推断问题）
	sets := []string{}
	uargs := []any{id}
	if upKbps != nil {
		sets = append(sets, fmt.Sprintf("up_kbps = $%d", len(uargs)+1))
		uargs = append(uargs, *upKbps)
	}
	if downKbps != nil {
		sets = append(sets, fmt.Sprintf("down_kbps = $%d", len(uargs)+1))
		uargs = append(uargs, *downKbps)
	}
	_, err = s.Pool.Exec(ctx, "UPDATE bandwidth_profiles SET "+strings.Join(sets, ", ")+" WHERE id = $1", uargs...)
	return err
}

// EffectiveBandwidth 当前生效带宽：手动指定优先，否则最近一次自测。
// 先查该 scope（如某条分享），无则回落 global；全无则 source="none"、数值为 0。
func (s *Store) EffectiveBandwidth(ctx context.Context, sc bandwidthScope) (up, down int, source string, err error) {
	for _, s2 := range scopesFor(sc) {
		u, d, ok, e := s.profileBandwidth(ctx, s2, "manual")
		if e != nil {
			return 0, 0, "", e
		}
		if ok {
			return u, d, "manual", nil
		}
	}
	for _, s2 := range scopesFor(sc) {
		u, d, ok, e := s.profileBandwidth(ctx, s2, "self_test")
		if e != nil {
			return 0, 0, "", e
		}
		if ok {
			return u, d, "self_test", nil
		}
	}
	return 0, 0, "none", nil
}

// scopesFor 查询顺序：具体 scope → global（scope 本身为 global 时只有一项）。
func scopesFor(sc bandwidthScope) []bandwidthScope {
	if sc.Scope == "global" || sc.Scope == "" {
		return []bandwidthScope{{Scope: "global"}}
	}
	return []bandwidthScope{sc, {Scope: "global"}}
}

// profileBandwidth 读某 scope 下指定 source 的 profile 上下行（列可空，缺失按 0）。
func (s *Store) profileBandwidth(ctx context.Context, sc bandwidthScope, source string) (up, down int, ok bool, err error) {
	where, args := scopeFilter(sc)
	args = append(args, source)
	var u, d *int
	err = s.Pool.QueryRow(ctx, `SELECT up_kbps, down_kbps FROM bandwidth_profiles WHERE `+where+
		fmt.Sprintf(" AND source = $%d ORDER BY updated_at DESC LIMIT 1", len(args)), args...).Scan(&u, &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if u != nil {
		up = *u
	}
	if d != nil {
		down = *d
	}
	return up, down, true, nil
}
