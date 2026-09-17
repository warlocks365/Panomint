package audit

// 管理端只读 HTTP 端点。
//
// 本文件实现契约里的三条端点：
//
//	GET /admin/audit   契约 §2「管理端点（需 admin:users）」  → 审计日志（分页 + 过滤）
//	GET /admin/stats   契约 §14 管理后台                      → 系统概览
//	GET /admin/jobs    契约 §12                               → 索引/转码任务列表与进度
//
// **本文件不注册路由**，路由由 cmd/api/main.go 统一接线（装配样例见包文档与交付报告）。
//
// 鉴权：三条都由**人**（管理员界面）调用，走 JWT + RBAC，由 main.go 套
// auth.AuthRequired 与 auth.RequirePerm。权限点取自**库中实际存在的**种子权限，
// 不新造权限名（实测 seed 只有 admin:system / admin:users 两个 admin 前缀权限）：
//
//	/admin/audit → admin:users   （契约 §2 把 /admin/audit 列在"需 admin:users"表内）
//	/admin/stats → admin:system  （系统级概览，与 §15 节点管理同级）
//	/admin/jobs  → admin:system  （索引/转码运维面）
//
// 错误响应格式与 media / shares / compute 一致：{"error":{"code","message"}}。

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 错误码（与既有包同风格的大写下划线常量）。
const (
	CodeInvalidInput = "INVALID_INPUT"
	CodeInternal     = "INTERNAL"
	// CodeUnauthorized 未认证（本包唯一的用户侧端点 RestoreHistory 用它：
	// 拿不到 actor 时绝不能退化成"查全站"，见该文件头注释）。
	CodeUnauthorized = "UNAUTHORIZED"
)

// Handler 管理端只读端点。Store 不可为空。
type Handler struct {
	Store QueryStore
	// Recorder 可为 nil —— 为 nil 时不写审计（便于测试与灰度）。
	Recorder *Recorder
}

// fail 统一错误响应。
func fail(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}

// ---------------------------------------------------------------------------
// GET /admin/audit
// ---------------------------------------------------------------------------

// ListAudit 审计日志查询（分页 + 过滤）。
//
// 查询参数（全部可选）：
//
//	action      动作精确匹配（归一化后再匹配，见 NormalizeAction）
//	actor       触发者 user_id（UUID）
//	target_type 目标类型
//	target_id   目标 ID（给出时必须同时给 target_type）
//	from,to     时间范围，RFC3339，左闭右开 [from, to)
//	limit       默认 50，上限 200
//	cursor      上一页返回的 next_cursor
//
// 响应：{items, total, limit, next_cursor?}
func (h *Handler) ListAudit(c *gin.Context) {
	f, err := parseAuditFilter(c)
	if err != nil {
		fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())
		return
	}
	page, err := h.Store.Query(c.Request.Context(), f)
	if err != nil {
		log.Printf("audit: 查询审计日志失败: %v", err)
		fail(c, http.StatusInternalServerError, CodeInternal, "查询审计日志失败")
		return
	}
	// 先回响应、再写审计：本端点会自记录一条 admin.audit.read（见 recordAuditRead），
	// 把写库放在 c.JSON 之后可以保证审计的耗时不计入客户端等待。
	c.JSON(http.StatusOK, page)
	h.recordAuditRead(c, f)
}

// recordAuditRead 记录「谁读了审计日志」。
//
// 为什么审计查询本身要写审计：audit_log 含全站敏感操作史（谁删了谁、谁改了权限），
// 读取它本身就是一次有后果的敏感访问 —— 不记的话，内部人员翻完审计再去删自己的
// 记录就无从发现。这是本包**当前唯一的生产调用点**，其余接入清单见交付报告。
//
// detail 只记过滤条件，**不记返回内容**（返回内容已在审计表里，再记一遍会指数膨胀）。
func (h *Handler) recordAuditRead(c *gin.Context, f Filter) {
	if h.Recorder == nil {
		return
	}
	e := FromGin(c)
	e.Action = ActionAuditRead
	e.TargetType = TargetAuditLog

	d := map[string]any{"limit": f.Limit}
	if f.Action != "" {
		d["action"] = f.Action
	}
	if f.ActorUserID != "" {
		d["actor"] = f.ActorUserID
	}
	if f.TargetType != "" {
		d["target_type"] = f.TargetType
	}
	if f.TargetID != "" {
		d["target_id"] = f.TargetID
	}
	if f.From != nil {
		d["from"] = f.From.UTC().Format(time.RFC3339)
	}
	if f.To != nil {
		d["to"] = f.To.UTC().Format(time.RFC3339)
	}
	if f.CursorAt != nil {
		d["paged"] = true
	}
	e.Detail = d

	h.Recorder.Record(c.Request.Context(), e)
}

// FromGin 从 gin 上下文取出触发者与来源信息。
// user_id 由 auth.AuthRequired 注入；ip 受 X-Forwarded-For 影响，属不可信输入。
func FromGin(c *gin.Context) Entry {
	return Entry{
		ActorUserID: c.GetString("user_id"),
		IP:          c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	}
}

// parseAuditFilter 解析并校验查询参数。所有过滤值都走与写入侧相同的归一化函数，
// 保证"写进去的动作名"与"查得出来的动作名"必然一致。
func parseAuditFilter(c *gin.Context) (Filter, error) {
	var f Filter

	if v := strings.TrimSpace(c.Query("action")); v != "" {
		a, err := NormalizeAction(v)
		if err != nil {
			return f, err
		}
		f.Action = a
	}
	if v := strings.TrimSpace(c.Query("actor")); v != "" {
		a, err := NormalizeActor(v)
		if err != nil {
			return f, err
		}
		if a == "" {
			return f, errors.New("actor 必须是 UUID")
		}
		f.ActorUserID = a
	}
	if v := strings.TrimSpace(c.Query("target_type")); v != "" {
		tt, err := NormalizeTargetType(v)
		if err != nil {
			return f, err
		}
		if tt == "" {
			return f, errors.New("target_type 非法")
		}
		f.TargetType = tt
	}
	if v := strings.TrimSpace(c.Query("target_id")); v != "" {
		id, err := NormalizeTargetID(v)
		if err != nil {
			return f, err
		}
		if f.TargetType == "" {
			// 与写入侧同一条不变量：只给 id 不给 type，查询必然落空却看着像"没记录"。
			return f, errors.New("给出 target_id 时必须同时给出 target_type")
		}
		f.TargetID = id
	}
	from, err := parseTimeQuery(c, "from")
	if err != nil {
		return f, err
	}
	to, err := parseTimeQuery(c, "to")
	if err != nil {
		return f, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return f, errors.New("from 必须早于 to")
	}
	f.From, f.To = from, to

	if v := strings.TrimSpace(c.Query("cursor")); v != "" {
		at, id, err := decodeCursor(v)
		if err != nil {
			return f, errors.New("无效游标")
		}
		f.CursorAt, f.CursorID = &at, &id
	}
	f.Limit = clampAuditLimit(atoiDefault(c.Query("limit"), defaultAuditLimit))
	return f, nil
}

// parseTimeQuery 解析 RFC3339 / RFC3339Nano 时间参数。空值返回 nil。
func parseTimeQuery(c *gin.Context, key string) (*time.Time, error) {
	v := strings.TrimSpace(c.Query(key))
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, errors.New(key + " 必须是 RFC3339 时间（如 2026-09-15T00:00:00Z）")
	}
	u := t.UTC()
	return &u, nil
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

// ---------------------------------------------------------------------------
// GET /admin/stats
// ---------------------------------------------------------------------------

// Stats 系统概览（契约 §14）。
// 响应：{media_total, users, storage_used, index_status}
func (h *Handler) Stats(c *gin.Context) {
	st, err := h.Store.Stats(c.Request.Context())
	if err != nil {
		log.Printf("audit: 统计系统概览失败: %v", err)
		fail(c, http.StatusInternalServerError, CodeInternal, "统计系统概览失败")
		return
	}
	c.JSON(http.StatusOK, st)
}

// ---------------------------------------------------------------------------
// GET /admin/jobs
// ---------------------------------------------------------------------------

// jobsStatusRe 状态取值白名单字符集。
// 状态是各表自定义的 varchar（库里实测 index=done、transcode=done|failed，
// 但队列实现可能新增 'queued'/'canceled' 等），故不写死枚举，
// 只挡注入面与明显的脏输入。真正的枚举约束应在写入侧，不在查询侧。
var jobsStatusRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// Jobs 索引 / 转码任务列表与进度（契约 §12 查 index_jobs / transcode_jobs）。
//
// 查询参数（全部可选）：
//
//	type   index | transcode | all（默认 all）
//	status 状态精确匹配
//	limit  默认 50，上限 200
//
// 响应：{items:[...], limit}
func (h *Handler) Jobs(c *gin.Context) {
	q, err := parseJobQuery(c)
	if err != nil {
		fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())
		return
	}
	jobs, err := h.Store.Jobs(c.Request.Context(), q)
	if err != nil {
		log.Printf("audit: 查询任务列表失败: %v", err)
		fail(c, http.StatusInternalServerError, CodeInternal, "查询任务列表失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": jobs, "limit": clampJobsLimit(q.Limit)})
}

// GetJob GET /admin/jobs/:id（需 admin:system）→ 单条任务（契约 §12）。
//
// 与列表端点同权限：能看整张任务表的人，当然也能看其中一条。
//
// ⚠️ 先校验 UUID 格式：id 进 SQL 时带 `::uuid` 转换，格式不对会让 PG 抛类型错误
// → 落进 default 分支变成 **500**。那会把"调用方传错 id"伪装成"服务故障"。
func (h *Handler) GetJob(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if !uuidRe.MatchString(id) {
		fail(c, http.StatusBadRequest, CodeInvalidInput, "任务 id 必须是 UUID")
		return
	}
	j, err := h.Store.GetJob(c.Request.Context(), id)
	if errors.Is(err, ErrJobNotFound) {
		fail(c, http.StatusNotFound, "JOB_NOT_FOUND", "任务不存在")
		return
	}
	if err != nil {
		log.Printf("audit: 查询任务失败: %v", err)
		fail(c, http.StatusInternalServerError, CodeInternal, "查询任务失败")
		return
	}
	c.JSON(http.StatusOK, j)
}

// parseJobQuery 解析并校验 /admin/jobs 的查询参数。
func parseJobQuery(c *gin.Context) (JobQuery, error) {
	var q JobQuery
	switch t := strings.ToLower(strings.TrimSpace(c.Query("type"))); t {
	case "", "all":
		q.JobType = "" // 不限制；BuildJobsSQL 把 "" 当 all
	case "index", "transcode":
		q.JobType = t
	default:
		return q, errors.New("type 仅支持 index | transcode | all")
	}
	if s := strings.TrimSpace(c.Query("status")); s != "" {
		if !jobsStatusRe.MatchString(s) {
			return q, errors.New("status 非法")
		}
		q.Status = s
	}
	q.Limit = clampJobsLimit(atoiDefault(c.Query("limit"), defaultJobsLimit))
	return q, nil
}
