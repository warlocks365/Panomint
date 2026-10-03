package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"panoalbum/internal/audit"
	"panoalbum/internal/geocoord"
	"panoalbum/internal/httperr"
	"panoalbum/internal/pgxutil"
	"panoalbum/internal/queue"
)

// 媒体写操作 HTTP 处理器（详情/收藏/评级/软删/回收站）。

// errResp 统一错误响应 {"error":{"code","message"}}。
// 形状委托给 internal/httperr（单一真源），本包不再各写一份 c.JSON(gin.H{...})。
func errResp(c *gin.Context, status int, code, msg string) {
	httperr.Envelope(c, status, code, msg)
}

// writeLookupError 把「取媒体元信息/归属」失败的 DB 错误映射为响应。
//
// ★ 安全不变量：**畸形 id 与「记录不存在」必须逐字节同形**（同一个 404 NOT_FOUND /
// 同一个 message）。media.id 是 UUID 列，`GET /media/not-a-uuid` 会让 PG 抛 22P02；
// 若把它当 500，既伪造了"服务故障"，又会把 PG 原文（invalid input syntax for type uuid、
// SQLSTATE、列类型）回给客户端。把两条分支收敛到同一个响应，是因为对调用方而言
// "id 语法非法"与"这条不存在"是同一件事；任何可区分的响应都会变成探测判据。
//
// 其余 DB 错误（连不上库、超时、语法正确的 UUID 查询失败）仍是 500，但 message 固定：
// 服务端 log 保留完整错误供排障，客户端不该看到内部实现细节。
func writeLookupError(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) || pgxutil.IsMalformedID(err) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在")
		return
	}
	log.Printf("[media] 媒体归属/存在性查询失败: %v", err)
	errResp(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
}

// checkAccess 校验媒体归属；resp 为 false 时已写出错误响应。
//
// 错误映射统一走 writeLookupError —— 本函数的调用方遍布 /media/** 的读写端点
// （Detail/Favorite/Rating/Delete/Restore/Purge/…），故畸形 id 的 404 口径与
// 「不回显 PG 原文」在这里一次性生效，不需要每个端点各判一遍（单一真源）。
//
// 口径：**写路径**（仅属主/owner/admin，见 canAccess）。读路径（Detail/Thumb/Download）
// 用 checkReadAccess —— 共享空间成员对 space='shared' 的媒体有读权限（P2-01）。
func (h *Handler) checkAccess(c *gin.Context, id string) (ownerID string, ok bool) {
	ownerID, _, err := h.Store.ownerOf(c.Request.Context(), id)
	if err != nil {
		writeLookupError(c, err)
		return "", false
	}
	if !canAccess(c.GetString("user_id"), c.GetString("role"), ownerID) {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "无权访问该媒体")
		return "", false
	}
	return ownerID, true
}

// checkReadAccess 校验单条媒体的「读」访问（Detail/Download 共用；P2-01）。
//
// 与 checkAccess 的差别：读路径的口径是
// 「属主 ∪（media.space='shared' 且调用者是共享空间成员）∪ owner/admin」，
// 与 GET /media?space=shared 的列表口径一致（判定收敛在 mediascope.ReadCond，
// 本包不另写一份成员判定）。缩略图走 thumbAccess（无权收敛为 404 同形，
// 与本函数的 403 不同 —— 二进制资源不产出存在性预言机，见 thumb.go 注释）。
func (h *Handler) checkReadAccess(c *gin.Context, id string) (ownerID string, ok bool) {
	ownerID, err := readAccessCheck(c.Request.Context(), h.Store.readAccessOf,
		c.GetString("user_id"), c.GetString("role"), id)
	if err != nil {
		writeReadAccessError(c, err)
		return "", false
	}
	return ownerID, true
}

// readAccessLookup Detail/Download 读访问判定所需的唯一 DB 能力。
// 签名与 Store.readAccessOf 逐字相同（方法值可直接传入）；抽成函数类型是为了让
// 「无权 → ErrForbidden / 不存在 → ErrNotFound」的判定口径能被无库单测直接钉住。
type readAccessLookup func(ctx context.Context, id, userID, role string) (ownerID string, deleted, allowed bool, err error)

// readAccessCheck 读访问判定（纯逻辑，不碰 gin）：放行返回 ownerID；
// 媒体不存在 → ErrNotFound；无权 → ErrForbidden；DB 错误原样透出（不得折叠成 404/403，
// 否则故障会被伪装成「不存在」或「无权」）。
func readAccessCheck(ctx context.Context, lookup readAccessLookup, userID, role, id string) (string, error) {
	ownerID, _, allowed, err := lookup(ctx, id, userID, role)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", ErrForbidden
	}
	return ownerID, nil
}

// writeReadAccessError 读访问失败的响应映射：ErrForbidden → 403（读详情是元数据端点，
// 与列表的 403 口径一致）；其余（ErrNotFound / 畸形 id / DB 错误）走 writeLookupError。
func writeReadAccessError(c *gin.Context, err error) {
	if errors.Is(err, ErrForbidden) {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "无权访问该媒体")
		return
	}
	writeLookupError(c, err)
}

// Detail GET /media/:id
func (h *Handler) Detail(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkReadAccess(c, id); !ok {
		return
	}
	d, err := h.Store.GetDetail(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	}
	if err != nil {
		log.Printf("[media] 媒体详情查询失败 id=%s: %v", id, err)
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
		return
	}
	c.JSON(http.StatusOK, d)
}

// Favorite POST /media/:id/favorite {favorite:bool}
func (h *Handler) Favorite(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"favorite\": true|false}")
		return
	}
	if err := h.Store.SetFavorite(c.Request.Context(), id, c.GetString("user_id"), req.Favorite); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "favorite": req.Favorite})
}

// Rate POST /media/:id/rate {rating:0-5}
func (h *Handler) Rate(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	var req struct {
		Rating int `json:"rating"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 0 || req.Rating > 5 {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "rating 需为 0-5 整数")
		return
	}
	if err := h.Store.SetRating(c.Request.Context(), id, req.Rating); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "rating": req.Rating})
}

// Patch PATCH /media/:id {notes?, edits?}
// Job000005：仅允许更新 notes / edits 字段，其他字段拒收 400。
// 注意语义差异：PATCH 的 edits 是**整体提交**（整对象替换；显式 null 即清空），
// 不做 MergeRotateEdit/MergeCropEdit 的单维度合并——前端整体提交契约不变。
func (h *Handler) Patch(c *gin.Context) {
	var req struct {
		Notes *string         `json:"notes"`
		Edits json.RawMessage `json:"edits"`
		// Job000143：地理与时间元数据。指针语义 = 「字段缺席则不动」；
		// 清空靠传空串 / 坐标传 0（见 normalizeMetadataRequest 的三态归一）。
		TakenAt *string  `json:"taken_at"` // RFC3339，如 2026-08-15T14:30:00+08:00
		Place   *string  `json:"place"`    // 拍摄地短地名
		Address *string  `json:"address"`  // 详细地址
		Lat     *float64 `json:"lat"`      // ⚠️ 坐标系见 normalizeMetadataRequest
		Lng     *float64 `json:"lng"`
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields() // 设计裁决：非白名单字段一律拒收（显式优于静默忽略）
	if err := dec.Decode(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST",
			"仅支持更新 notes / edits / taken_at / place / address / lat / lng 字段")
		return
	}
	if req.Notes == nil && req.Edits == nil &&
		req.TakenAt == nil && req.Place == nil && req.Address == nil &&
		req.Lat == nil && req.Lng == nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST",
			"缺少 notes / edits / taken_at / place / address / lat / lng 中的至少一项")
		return
	}
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	resp := gin.H{"id": id}
	if req.Notes != nil {
		if err := h.Store.SetNotes(c.Request.Context(), id, *req.Notes); errors.Is(err, ErrNotFound) {
			errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
			return
		} else if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
		resp["notes"] = *req.Notes
	}
	if req.Edits != nil {
		// 显式 null → 清空编辑（重置）
		var edits *Edits
		if string(req.Edits) != "null" {
			e, err := NormalizeEdits(req.Edits)
			if err != nil {
				errResp(c, http.StatusBadRequest, "BAD_REQUEST", "edits 非法：rotate 需 0/90/180/270，crop 需归一化 {x,y,w,h}")
				return
			}
			edits = e
		}
		if err := h.Store.SetEdits(c.Request.Context(), id, edits); errors.Is(err, ErrNotFound) {
			errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
			return
		} else if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
		if edits != nil {
			resp["edits"] = edits
		} else {
			resp["edits"] = nil
		}
		h.enqueueThumbRegen(c, id)
	}

	// ---- Job000143：地理与时间元数据 ----
	// 与 notes/edits 分开处理：前两者是「内容」改动，本段是「定位/时间」改动，
	// 校验规则（三态、坐标成对、范围）完全不同，混在一起会让错误提示无法定位。
	meta, fields, mErr := h.normalizeMetadataRequest(c, req.TakenAt, req.Place, req.Address, req.Lat, req.Lng)
	if mErr != nil {
		errResp(c, http.StatusBadRequest, "BAD_METADATA", mErr.Error())
		return
	}
	if !meta.Empty() {
		if err := h.Store.SetMetadata(c.Request.Context(), id, meta); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
		// 回显归一化后的值（坐标已转 WGS-84，与库里存的以及 GET 详情返回的一致）。
		if meta.SetTakenAt {
			if meta.TakenAt.IsZero() {
				resp["taken_at"] = nil
			} else {
				resp["taken_at"] = meta.TakenAt
			}
		}
		if meta.SetPlace {
			if meta.Place == "" {
				resp["place"] = nil
			} else {
				resp["place"] = meta.Place
			}
		}
		if meta.SetAddress {
			if meta.Address == "" {
				resp["address"] = nil
			} else {
				resp["address"] = meta.Address
			}
		}
		if meta.SetGPS {
			if meta.Lat == 0 && meta.Lng == 0 {
				resp["gps"] = nil
			} else {
				resp["gps"] = Geo{Lat: meta.Lat, Lng: meta.Lng}
			}
		}
		// 审计：只记「改了哪些字段」，**不记坐标明文**（见 ActionMediaMetadataEdit 注释）。
		h.record(c, audit.ActionMediaMetadataEdit, audit.TargetMedia, id,
			map[string]any{"fields": fields})
	}
	c.JSON(http.StatusOK, resp)
}

// normalizeMetadataRequest 把 PATCH 请求的元数据字段归一化成 MetadataUpdate。
//
// 职责：
//  1. 三态判定（缺席=不动 / 空串或 0=清空 / 有值=更新）；
//  2. **坐标系统一**：库内 media.gps 是 geometry(Point,4326)=WGS-84，
//     而高德地图的搜索候选与选点坐标是 **GCJ-02**。前端不做转换（它不知道
//     底图用的是哪套坐标），由本函数按来源标记转换后入库 —— 这是**唯一收口点**，
//     漏一处就会让地图页 geo/clusters 聚合错位 300~500 米。
//  3. 委托 NormalizeMetadata 做范围/长度校验。
//
// coordSource 坐标系来源标记。⚠️ 缺省（前端未声明）时**假定已是 WGS-84**：
// 宁可让用户手动纠偏，也不能把已经是 WGS-84 的坐标（EXIF、GPS 设备）
// 再转一次 —— 那是不可逆的精度损失。
// coordSourceKey 坐标系声明头。
//
// ⚠️ 必须是 **连字符** X-Coord-Source —— 这与前端 api/media.js 里
// `headers: { 'X-Coord-Source': coordSource }` 以及契约文档一致。
// 原先这里写成 "coord_source"（下划线），于是前端发的头在 net/http 的
// canonical 规则下落到 "Coord-Source"，而代码查的是 "coord_source" →
// **永远读不到 → GCJ-02 坐标从未被转换**（表现为地图上整体偏 300~500 米，
// 但接口 200、无任何报错）。这类 bug 只有端到端断言才抓得到。
const coordSourceKey = "X-Coord-Source"

// c 仅用于读 X-Coord-Source 头（判坐标系来源），除此之外不碰请求体。
func (h *Handler) normalizeMetadataRequest(
	c *gin.Context, takenAt, place, address *string, lat, lng *float64) (MetadataUpdate, []string, error) {
	var m MetadataUpdate
	var fields []string

	if takenAt != nil {
		m.SetTakenAt = true
		fields = append(fields, "taken_at")
		s := strings.TrimSpace(*takenAt)
		if s == "" {
			m.TakenAt = time.Time{} // 清空
		} else {
			// 接受 RFC3339（带时区）。不带时区的字符串一律拒：
			// 猜时区会把时间写错 8 小时，且用户无从察觉。
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return m, nil, fmt.Errorf("拍摄时间格式非法：需 RFC3339（如 2026-08-15T14:30:00+08:00）")
			}
			m.TakenAt = t
		}
	}
	if place != nil {
		m.SetPlace = true
		m.Place = *place
		if strings.TrimSpace(m.Place) != "" {
			fields = append(fields, "place")
		}
	}
	if address != nil {
		m.SetAddress = true
		m.Address = *address
		if strings.TrimSpace(m.Address) != "" {
			fields = append(fields, "address")
		}
	}
	if lat != nil || lng != nil {
		if lat == nil || lng == nil {
			// 明确区分这个错误：半截坐标写入后用户完全无感（另一半保持旧值），
			// 且下次地图选点会把它当成合法值继续用。
			return m, nil, fmt.Errorf("经纬度必须成对提供：lat 与 lng 须同时给或同时不给")
		}
		m.SetGPS = true
		m.Lat, m.Lng = *lat, *lng
		// **坐标系收口**：前端传的若为 GCJ-02（高德底图），转成 WGS-84 落库。
		// 依据是请求头 X-Coord-Source: gcj02（见前端 GeoPicker/搜索调用点）。
		if strings.EqualFold(strings.TrimSpace(c.GetHeader(coordSourceKey)), "gcj02") {
			if geocoord.OutOfChina(m.Lng, m.Lat) {
				m.Lng, m.Lat = geocoord.GCJ02ToWGS84(m.Lng, m.Lat)
			}
			// 境外坐标 GCJ-02 与 WGS-84 等价，不转换（转换公式只对国内有效）
		}
		if m.Lat != 0 || m.Lng != 0 {
			fields = append(fields, "gps")
		}
	}

	norm, err := NormalizeMetadata(m)
	if err != nil {
		if errors.Is(err, ErrHalfCoords) {
			return m, nil, fmt.Errorf("经纬度必须成对提供：lat 与 lng 须同时给或同时不给")
		}
		return m, nil, err
	}
	return norm, fields, nil
}

// Rotate POST /media/:id/rotate {op:"rotate|crop|auto", angle?, rect?}（契约 §3，非破坏 sidecar）
//
// Phase 4 修复：rotate/crop 改为「读-改-写」合并语义——原实现是整对象覆盖，
// 先 PATCH 写入 {rotate,crop} 后再 POST op=rotate 会抹掉 crop，再 POST op=crop 会把 rotate 重置为 0。
// op:"auto" 维持「清空编辑参数」的既有语义（本实现不含自动增强算法，原文件始终不变）。
//
// 注意：PATCH /media/:id 走的是整体提交（"{edits:{...}}" 或显式 null 清空），
// 语义与这里不同，源码见下方 Patch，未做改动。
func (h *Handler) Rotate(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	var req struct {
		Op    string    `json:"op"`
		Angle int       `json:"angle"`
		Rect  *CropRect `json:"rect"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {op, angle?, rect?}")
		return
	}

	// 1) 入参校验（不触库，先失败先返回）
	var crop *CropRect
	switch req.Op {
	case "rotate":
		switch req.Angle {
		case 90, 180, 270:
		default:
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "angle 需为 90/180/270")
			return
		}
	case "crop":
		if req.Rect == nil {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "缺少 rect")
			return
		}
		raw, _ := json.Marshal(Edits{Crop: req.Rect})
		e, err := NormalizeEdits(raw)
		if err != nil {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "rect 需为归一化 {x,y,w,h} 且不越界")
			return
		}
		crop = e.Crop
	case "auto":
		// 无自动增强算法：按「重置编辑参数」处理（rotate 与 crop 一并清空）
	default:
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "op 需为 rotate|crop|auto")
		return
	}

	// 2) 读现有编辑参数
	cur, err := h.Store.GetEdits(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}

	// 3) 合并：单维度 op 只改自己那一段，保留另一段
	var edits *Edits
	switch req.Op {
	case "rotate":
		edits = MergeRotateEdit(cur, req.Angle)
	case "crop":
		edits = MergeCropEdit(cur, crop)
	case "auto":
		edits = nil // 清空编辑参数
	}

	// 4) 落库
	if err := h.Store.SetEdits(c.Request.Context(), id, edits); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	h.enqueueThumbRegen(c, id)
	c.JSON(http.StatusOK, gin.H{"id": id, "edits": edits})
}

// enqueueThumbRegen 尽力而为：编辑变更后重排缩略图任务。
// worker 侧（internal/index/worker.go 的 editFilter）会读取 media.edits 并套用
// 「先裁剪后旋转」滤镜链，使缩略图与查看器呈现一致；此处仅负责投递 "thumbnail" 任务，
// 投递失败不影响编辑参数已持久化。
func (h *Handler) enqueueThumbRegen(c *gin.Context, id string) {
	if h.Q == nil {
		return
	}
	d, err := h.Store.GetDetail(c.Request.Context(), id)
	if err != nil || d == nil {
		return
	}
	abs, ok := h.ResolvePath(d.Path)
	if !ok {
		return
	}
	payload := map[string]string{"media_id": id, "path": abs, "kind": d.Type}
	if _, err := h.Q.Enqueue(c.Request.Context(), queue.Job{Kind: "thumbnail", Payload: payload}); err != nil {
		fmt.Printf("缩略图重排失败 media=%s: %v\n", id, err)
	}
}

// Delete DELETE /media/:id（软删入回收站）
func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")
	ownerID, ok := h.checkAccess(c, id)
	if !ok {
		return
	}
	if err := h.Store.SoftDelete(c.Request.Context(), id); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已在回收站")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	// 审计在**成功落库之后**：软删失败（404/500）不写，否则审计里会出现
	// "谁删了 X" 而 X 其实还在 —— 记录错误的审计比不记录更危险（见 audit.Recorder 的注释）。
	// detail 只记 owner_id（中性键名，不带任何敏感子串），用于按"某人名下被删的媒体"追查。
	h.record(c, audit.ActionMediaDelete, audit.TargetMedia, id, map[string]any{"owner_id": ownerID})
	c.JSON(http.StatusOK, gin.H{"id": id, "deleted": true})
}

// Trash GET /media/trash（回收站列表，仅本人）
func (h *Handler) Trash(c *gin.Context) {
	res, err := h.Store.ListTrash(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Restore POST /media/trash/:id/restore
func (h *Handler) Restore(c *gin.Context) {
	id := c.Param("id")
	ownerID, ok := h.checkAccess(c, id)
	if !ok {
		return
	}
	path, err := h.Store.Restore(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或不在回收站")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	// 审计在**成功落库之后**：对**不在回收站**的 id（未软删或不存在）返回 404 且**不写**
	// —— 记一条「已恢复」而它其实从未被删，与记一条假的「已删除」同样危险（§二十四.1）。
	// 这条负对照必须在真库上跑：只验成功路径时，"进了 handler 就写"与"成功后写"表现完全相同。
	//
	// detail 记 path 的理由与 Purge 相同（§二十四.7）：恢复之后这行仍可能被 purge，
	// 届时 target_id 查不回任何东西；path 是这条媒体在库里最后的可解析线索，
	// 而只有在恢复的当口才取得到。键名 owner_id / path 均不含敏感子串，不会被 RedactDetail 剔除。
	h.record(c, audit.ActionMediaRestore, audit.TargetMedia, id,
		map[string]any{"owner_id": ownerID, "path": path})
	c.JSON(http.StatusOK, gin.H{"id": id, "restored": true})
}

// Purge DELETE /media/trash/:id（永久删除：行删除 + 上传目录内文件清理）
func (h *Handler) Purge(c *gin.Context) {
	id := c.Param("id")
	ownerID, ok := h.checkAccess(c, id)
	if !ok {
		return
	}
	path, refsCleared, err := h.Store.Purge(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或不在回收站")
		return
	} else if err != nil {
		// 防御残留路径（P1-02）：迁移 00027 已把指向 media 的四处外键改为
		// ON DELETE SET NULL，且 Purge 在 DELETE 前同事务显式解引用，理论上不再撞
		// 23503；若仍撞（迁移未应用、或未来新增了他处 NO ACTION 引用），映射为 409
		// 并说明原因，而不是落到通用 500「更新失败」（伪服务故障，用户无可操作指引）。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			log.Printf("[media] Purge 仍撞外键约束（23503，应为残留引用路径）id=%s: %v", id, err)
			errResp(c, http.StatusConflict, "PURGE_CONFLICT",
				"媒体仍被其他数据引用（相册/人物封面、去重原件或 Live Photo 配对），无法彻底清除")
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	// ⭐ 本动作**不可恢复**，是全部动作里唯一"事后无从补救"的一个，所以审计行必须落在
	// DELETE 之后、且在下面删磁盘文件**之前** —— 反过来的话，进程若在写审计前挂掉，
	// 文件已经没了而审计里没有这条记录。
	//
	// detail 记 path 的理由：purge 会把 media 行**整行删除**，此后 target_id 再也查不回
	// 任何东西（GetDetail 只会说"不存在或已删除"）。path 是这条媒体在库里最后的痕迹，
	// 也是事后唯一能回答"到底销毁了什么"的线索 —— 行没了，审计还在，这才是审计的意义。
	// 键名 path / owner_id / refs_cleared 均不含敏感子串，不会被 RedactDetail 剔除。
	// refs_cleared：Purge 同事务顺带解除的封面/配对/原件引用数（P1-02）。
	h.record(c, audit.ActionMediaPurge, audit.TargetMedia, id,
		map[string]any{"owner_id": ownerID, "path": path, "refs_cleared": refsCleared})
	// 尽力清理磁盘文件（仅允许删除已知根目录下的文件）
	if abs, ok := h.ResolvePath(path); ok {
		_ = removeFile(abs)
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "purged": true})
}

// Pano360 GET /media/:id/360（360 播放元数据，契约 §13）
func (h *Handler) Pano360(c *gin.Context) {
	id := c.Param("id")
	// 读端点用读口径（P2-01）：shared 空间成员对 space='shared' 媒体同样可读元数据。
	if _, ok := h.checkReadAccess(c, id); !ok {
		return
	}
	d, err := h.Store.GetDetail(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	}
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	resp := gin.H{
		"is_360":         d.Is360,
		"projection":     d.Projection,
		"width":          d.Width,
		"height":         d.Height,
		"gyro_supported": true,
		"vr_supported":   true,
	}
	if d.HLSMaster != nil && *d.HLSMaster != "" {
		resp["hls_master"] = *d.HLSMaster
	} else {
		resp["hls_master"] = nil
		resp["needs_transcode"] = true // 无 HLS 流，前端提示先转码
	}
	c.JSON(http.StatusOK, resp)
}
