package httperr

// Repo 级源码形状守卫：禁止把 err.Error() 直接交给「响应写入」调用。
//
// 为什么需要它：本仓 2026-09-18 修了 5 个端点的"畸形 id → 500 + 回显 PostgreSQL 原文"，
// 当时普查出全仓约 143 处把 err.Error() 放进面向客户端的响应。逐个人肉找不可能守住 ——
// 新增一个 handler、复制一段旧代码，缺陷立刻复活。这条守卫把"命中必须登记"变成可执行断言：
//
//   - 未登记的命中 → FAIL（新增泄漏必须显式登记，逼作者做一次判断）；
//   - MustFix=true 的登记项**出现在源码里** → FAIL（这是"修到绿"的驱动：
//     登记了就必须修，源码里不许再有它）；
//   - MustFix=false 的登记项**在源码里找不到** → FAIL（登记表不许悬空：
//     一行文案改了、函数重命名了，都必须回来更新理由，否则登记表会慢慢变成摆设）。
//
// 变异自证见 TestErrTextGuardMutationProof：把一个已修的行改回 err.Error()，
// 上面的 MustFix=true 分支必须把测试弄红。
//
// 维护方式：跑 `ERRTEXT_DUMP=1 go test ./internal/httperr/ -run TestErrTextGuard -v`
// 会打印当前全部命中（可直接作为登记项模板）。

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// errEchoRegistry 是"把 err.Error() 传给响应写入调用"的**显式登记表**。
//
// 每条登记项区分两类，不能混为一谈：
//   - MustFix=true ：回显的是 DB/驱动/系统/第三方原文（pgconn.PgError、SQLSTATE、
//     os 路径、encoding/json 的 Go 字段名、bcrypt 等）→ 必须修；
//   - MustFix=false：回显的是**本端自己构造、面向用户的文案**（errors.New("…中文…")
//     或本仓的哨兵值）→ 可以保留，理由写在 Reason 里。
//
// 匹配键与字段含义见下方 errEcho。
var errEchoRegistry = []errEcho{
	// ===== MustFix=false：本端自己构造、面向用户的文案（保留，不得悬空）=====
	{"internal/audit/handlers.go", "ListAudit", "if err != nil {", "fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())", false, "参数过滤解析失败：parseAuditFilter/parseJobQuery 只返回本端 errors.New 中文文案（如「actor 必须是 UUID」「type 仅支持 index | transcode | all」），不含外部原文"},          // 76
	{"internal/audit/handlers.go", "Jobs", "if err != nil {", "fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())", false, "参数过滤解析失败：parseAuditFilter/parseJobQuery 只返回本端 errors.New 中文文案（如「actor 必须是 UUID」「type 仅支持 index | transcode | all」），不含外部原文"},               // 269
	{"internal/auth/handlers.go", "MFASetup", "if errors.Is(err, ErrMFAAlreadyEnabled) {", "errResp(c, http.StatusConflict, \"MFA_ALREADY_ENABLED\", err.Error())", false, "本端哨兵（ErrMFAAlreadyEnabled/ErrMFANotPending/ErrMFANotEnabled）的固定中文文案"},                                // 192
	{"internal/auth/handlers.go", "MFAConfirm", "if errors.Is(err, ErrMFANotPending) {", "errResp(c, http.StatusBadRequest, \"MFA_NOT_PENDING\", err.Error())", false, "本端哨兵（ErrMFAAlreadyEnabled/ErrMFANotPending/ErrMFANotEnabled）的固定中文文案"},                                    // 242
	{"internal/auth/handlers.go", "MFADisable", "if errors.Is(err, ErrMFANotEnabled) {", "errResp(c, http.StatusConflict, \"MFA_NOT_ENABLED\", err.Error())", false, "本端哨兵（ErrMFAAlreadyEnabled/ErrMFANotPending/ErrMFANotEnabled）的固定中文文案"},                                      // 285
	{"internal/auth/handlers.go", "CreateRole", "if err != nil {", "errResp(c, http.StatusBadRequest, \"INVALID_INPUT\", err.Error())", false, "NormalizeRoleInput/NormalizeUserUpdate 只返回本端校验文案（如「role 名非法」），非外部原文"},                                                            // 449
	{"internal/auth/handlers.go", "CreateRole", "case errors.Is(err, ErrInvalidInput):", "errResp(c, http.StatusBadRequest, \"INVALID_INPUT\", err.Error())", false, "本端哨兵 ErrInvalidInput 的校验文案（角色/用户字段非法）"},                                                                    // 478
	{"internal/auth/handlers.go", "guardUserChange", "case errors.Is(err, ErrSelfLockout):", "errResp(c, http.StatusConflict, \"SELF_LOCKOUT\", err.Error())", false, "本端哨兵（ErrSelfLockout/ErrLastOwner）的固定中文文案"},                                                                // 514
	{"internal/auth/handlers.go", "guardUserChange", "case errors.Is(err, ErrLastOwner):", "errResp(c, http.StatusConflict, \"LAST_OWNER\", err.Error())", false, "本端哨兵（ErrSelfLockout/ErrLastOwner）的固定中文文案"},                                                                    // 516
	{"internal/auth/handlers.go", "UpdateUser", "if err != nil {", "errResp(c, http.StatusBadRequest, \"INVALID_INPUT\", err.Error())", false, "NormalizeRoleInput/NormalizeUserUpdate 只返回本端校验文案（如「role 名非法」），非外部原文"},                                                            // 540
	{"internal/auth/handlers.go", "UpdateUser", "case errors.Is(err, ErrInvalidInput):", "errResp(c, http.StatusBadRequest, \"INVALID_INPUT\", err.Error())", false, "本端哨兵 ErrInvalidInput 的校验文案（角色/用户字段非法）"},                                                                    // 568
	{"internal/compute/agentapi.go", "Result", "case errors.Is(err, ErrInvalidInput):", "fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())", false, "本端哨兵 ErrInvalidInput 的校验文案：调用点只在 errors.Is(err, ErrInvalidInput) 分支上放行"},                                        // 208
	{"internal/compute/handlers.go", "failStore", "case errors.Is(err, ErrInvalidInput):", "fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())", false, "本端哨兵 ErrInvalidInput 的校验文案：调用点只在 errors.Is(err, ErrInvalidInput) 分支上放行"},                                     // 64
	{"internal/faces/api.go", "CreatePerson", "if errors.Is(err, ErrEmptyName) {", "fail(c, http.StatusBadRequest, \"INVALID_PARAMS\", err.Error())", false, "本端哨兵 ErrEmptyName 的固定中文文案"},                                                                                        // 65
	{"internal/faces/api.go", "PatchPerson", "case errors.Is(err, ErrPersonNotFound):", "fail(c, http.StatusNotFound, \"NOT_FOUND\", err.Error())", false, "本端哨兵 ErrPersonNotFound 的固定中文文案"},                                                                                     // 88
	{"internal/faces/api.go", "PatchPerson", "case errors.Is(err, ErrEmptyName), errors.Is(err, ErrNothingToUpdate):", "fail(c, http.StatusBadRequest, \"INVALID_PARAMS\", err.Error())", false, "本端哨兵（ErrEmptyName/ErrNothingToUpdate）的固定中文文案"},                                 // 91
	{"internal/geo/handlers.go", "Clusters", "if err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", false, "geo.ParseTime 的本地校验文案（「时间格式非法（需 RFC3339 或 YYYY-MM-DD）」附非法入参），非 DB/系统原文"},               // 184,189
	{"internal/geo/handlers.go", "Items", "if err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", false, "geo.ParseTime 的本地校验文案（「时间格式非法（需 RFC3339 或 YYYY-MM-DD）」附非法入参），非 DB/系统原文"},                  // 223,228
	{"internal/geo/handlers.go", "Places", "if err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", false, "geo.ParseTime 的本地校验文案（「时间格式非法（需 RFC3339 或 YYYY-MM-DD）」附非法入参），非 DB/系统原文"},                 // 283,288
	{"internal/geo/handlers.go", "PutUIPrefs", "if err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", false, "geo.NormalizeUIPrefs 的本端校验文案（如「map_slider_pos 仅支持 top|bottom」），非 DB/系统原文"},           // 364
	{"internal/geo/handlers.go", "PutMapConfig", "if errors.Is(err, ErrInvalidConfig) {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", false, "本端哨兵 ErrInvalidConfig 的配置校验文案（字段取值非法）"},                      // 420
	{"internal/media/handlers.go", "rejectScope", "if errors.Is(err, ErrMissingUser) {", "errResp(c, http.StatusUnauthorized, \"UNAUTHENTICATED\", err.Error())", false, "mediascope 哨兵 ErrMissingUser 的固定中文文案（身份缺失 → 401）"},                                                     // 95
	{"internal/media/handlers.go", "rejectScope", "}", "errResp(c, http.StatusBadRequest, \"INVALID_PARAMS\", err.Error())", false, "mediascope 哨兵 ErrInvalidSpace 的固定中文文案（「space 仅支持 personal|shared」）"},                                                                        // 98
	{"internal/media/handlers.go", "Duplicates", "if err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", false, "媒体 ErrInvalidDuplicateParams 的固定中文文案（threshold/limit 非整数）"},                        // 110
	{"internal/media/tag_handlers.go", "ListTags", "if err != nil {", "errResp(c, http.StatusBadRequest, \"BAD_REQUEST\", err.Error())", false, "本端校验文案（NormalizeTagName/normalizeColor/parseTagListQuery，如「标签名需为 1-128 字符」）"},                                                   // 45
	{"internal/media/tag_handlers.go", "AddTag", "if err != nil {", "errResp(c, http.StatusBadRequest, \"BAD_REQUEST\", err.Error())", false, "本端校验文案（NormalizeTagName/normalizeColor/parseTagListQuery，如「标签名需为 1-128 字符」）"},                                                     // 67
	{"internal/media/tag_handlers.go", "CreateTag", "if err != nil {", "errResp(c, http.StatusBadRequest, \"BAD_REQUEST\", err.Error())", false, "本端校验文案（NormalizeTagName/normalizeColor/parseTagListQuery，如「标签名需为 1-128 字符」）"},                                                  // 148,161
	{"internal/media/tag_handlers.go", "PatchTag", "if err != nil {", "errResp(c, http.StatusBadRequest, \"BAD_REQUEST\", err.Error())", false, "本端校验文案（NormalizeTagName/normalizeColor/parseTagListQuery，如「标签名需为 1-128 字符」）"},                                                   // 190,199
	{"internal/media/tag_handlers.go", "DeleteTag", "if errors.Is(err, ErrMergeTargetNotFound) {", "errResp(c, http.StatusNotFound, \"NOT_FOUND\", err.Error())", false, "本端哨兵 ErrMergeTargetNotFound 的固定中文文案；这里刻意提前校验，避免 PG 外键 23503 原文回显"},                                     // 223
	{"internal/media/upload.go", "Upload", "if err != nil {", "errResp(c, http.StatusBadRequest, \"BAD_RANGE\", err.Error())", false, "parseContentRange 的本端校验文案（「Content-Range 格式错误/范围非法」）"},                                                                                    // 134
	{"internal/search/handlers.go", "Search", "code := \"INVALID_PARAMS\"", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": code, \"message\": err.Error()}})", false, "search.ParseParams 只返回本端 errors.New 文案（ErrInvalidType/ErrInvalidDate/ErrInvalidCursor）"}, // 20

	// ===== MustFix=true：曾回显 DB/驱动/系统/第三方原文，已修 =====
	// 源码中不得再出现这些形态；一旦被改回（如回退某次修复），守卫会在此 FAIL。
	{"cmd/mapproto/main.go", "main", "if err != nil {", "http.Error(w, err.Error(), http.StatusInternalServerError)", true, "原先直接回显 pgx/PG 原文 → 已改固定文案，完整错误只进服务端日志"},                                                                                                                                                                                   // 71
	{"internal/albums/handlers.go", "Create", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"CREATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                             // 61
	{"internal/albums/handlers.go", "List", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                // 71
	{"internal/albums/handlers.go", "Patch", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                               // 139
	{"internal/albums/handlers.go", "Patch", "if err := h.Store.Patch(c.Request.Context(), id, req.Name, req.Description, req.CoverMediaID, req.Criteria); err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                    // 161
	{"internal/albums/handlers.go", "Delete", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                              // 176
	{"internal/albums/handlers.go", "Delete", "if err := h.Store.Delete(c.Request.Context(), id); err != nil {", "errResp(c, http.StatusInternalServerError, \"DELETE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                             // 188
	{"internal/albums/handlers.go", "AddItems", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                            // 203
	{"internal/albums/handlers.go", "AddItems", "if err != nil {", "errResp(c, http.StatusBadRequest, \"ADD_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                       // 232
	{"internal/albums/handlers.go", "RemoveItem", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                          // 247
	{"internal/albums/handlers.go", "RemoveItem", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"DELETE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                         // 264
	{"internal/albums/handlers.go", "ListComments", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                        // 274
	{"internal/albums/handlers.go", "AddComment", "case err != nil:", "errResp(c, http.StatusInternalServerError, \"CREATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                        // 306
	{"internal/albums/handlers.go", "DeleteComment", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                       // 321
	{"internal/albums/handlers.go", "DeleteComment", "if err := h.Store.DeleteComment(c.Request.Context(), cid); err != nil {", "errResp(c, http.StatusInternalServerError, \"DELETE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                              // 330
	{"internal/auth/handlers.go", "Refresh", "if err != nil {", "errResp(c, http.StatusUnauthorized, \"INVALID_REFRESH\", err.Error())", true, "Store.RotateSession 既可能返回本端文案也可能返回 PG 原文，原先一律回显 → 已改固定文案，完整错误只进服务端日志"},                                                                                                                                 // 333
	{"internal/faces/api.go", "ListPeople", "if err != nil {", "fail(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                   // 46
	{"internal/faces/api.go", "CreatePerson", "if err != nil {", "fail(c, http.StatusInternalServerError, \"CREATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                // 69
	{"internal/faces/api.go", "PatchPerson", "case err != nil:", "fail(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                // 94
	{"internal/faces/api.go", "TriggerScan", "if err != nil {", "fail(c, http.StatusInternalServerError, \"RESET_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                  // 158
	{"internal/folders/folders.go", "Tree", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                    // 85
	{"internal/folders/folders.go", "Tree", "if err := rows.Scan(&p, &n); err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                          // 94
	{"internal/geo/handlers.go", "Clusters", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                   // 197
	{"internal/geo/handlers.go", "Items", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                      // 234
	{"internal/geo/handlers.go", "Histogram", "}", "c.JSON(status, gin.H{\"error\": gin.H{\"code\": code, \"message\": err.Error()}})", true, "混用分支：粒度非法为本端文案、其余为 DB 故障，原先共用一行 err.Error() → 已拆分，DB 分支固定文案、粒度文案保留"},                                                                                                                                    // 257
	{"internal/geo/handlers.go", "Places", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                     // 293
	{"internal/geo/handlers.go", "GetMapIconPref", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                             // 309
	{"internal/geo/handlers.go", "PutMapIconPref", "if err := c.ShouldBindJSON(&p); err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", true, "原先回显 encoding/json 原文（带 Go 结构体/字段名，属实现细节）→ 已改固定文案，完整错误只进服务端日志"},                                                            // 320
	{"internal/geo/handlers.go", "PutMapIconPref", "if err := h.Media.PutMapIcon(c.Request.Context(), userID, &p); err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                 // 333
	{"internal/geo/handlers.go", "GetUIPrefs", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                 // 346
	{"internal/geo/handlers.go", "PutUIPrefs", "if err := c.ShouldBindJSON(&in); err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", true, "原先回显 encoding/json 原文（带 Go 结构体/字段名，属实现细节）→ 已改固定文案，完整错误只进服务端日志"},                                                               // 359
	{"internal/geo/handlers.go", "PutUIPrefs", "if err := h.Media.PutUIPrefs(c.Request.Context(), c.GetString(\"user_id\"), norm); err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"}, // 368
	{"internal/geo/handlers.go", "GetMapConfig", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                               // 382
	{"internal/geo/handlers.go", "PutMapConfig", "if err := c.ShouldBindJSON(&body); err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"INVALID_PARAMS\", \"message\": err.Error()}})", true, "原先回显 encoding/json 原文（带 Go 结构体/字段名，属实现细节）→ 已改固定文案，完整错误只进服务端日志"},                                                           // 401
	{"internal/geo/handlers.go", "PutMapConfig", "log.Printf(\"geo: 更新地图配置失败: %v\", err)", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                        // 425
	{"internal/geo/tile.go", "Serve", "if err != nil {", "c.JSON(http.StatusBadGateway, gin.H{\"error\": gin.H{\"code\": \"TILE_UPSTREAM\", \"message\": err.Error()}})", true, "原先回显 net/http 与上游 URL 原文（URL 里带高德 key 参数）→ 已改固定文案，完整错误只进服务端日志"},                                                                                                       // 112,120,133
	{"internal/health/health.go", "Ready", "if err := h.Pool.Ping(ctx); err != nil {", "checks[\"postgres\"] = \"fail: \" + err.Error()", true, "无鉴权端点（/ready 挂在根路由）原先回显 PG/Valkey/os 原文（主机、端口、用户名、文件路径）→ 已改固定 \"fail\"，详情只进服务端日志"},                                                                                                                    // 40
	{"internal/health/health.go", "Ready", "if err := h.Queue.Ping(ctx); err != nil {", "checks[\"valkey\"] = \"fail: \" + err.Error()", true, "无鉴权端点（/ready 挂在根路由）原先回显 PG/Valkey/os 原文（主机、端口、用户名、文件路径）→ 已改固定 \"fail\"，详情只进服务端日志"},                                                                                                                     // 47
	{"internal/health/health.go", "Ready", "if err := checkDiskWritable(h.DiskCheckDir); err != nil {", "checks[\"disk\"] = \"fail: \" + err.Error()", true, "无鉴权端点（/ready 挂在根路由）原先回显 PG/Valkey/os 原文（主机、端口、用户名、文件路径）→ 已改固定 \"fail\"，详情只进服务端日志"},                                                                                                       // 54
	{"internal/media/handlers.go", "List", "if err != nil {", "c.JSON(http.StatusBadRequest, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                              // 81
	{"internal/media/handlers.go", "Duplicates", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                           // 127
	{"internal/media/handlers.go", "DateHistogram", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                            // 148
	{"internal/media/tag_handlers.go", "ListTags", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                         // 50
	{"internal/media/tag_handlers.go", "AddTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"TAG_CREATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                      // 76
	{"internal/media/tag_handlers.go", "AddTag", "if err := h.Store.AttachTag(c.Request.Context(), id, tag.ID); err != nil {", "errResp(c, http.StatusInternalServerError, \"TAG_ATTACH_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                           // 80
	{"internal/media/tag_handlers.go", "AddTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                           // 88
	{"internal/media/tag_handlers.go", "RemoveTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                        // 103
	{"internal/media/tag_handlers.go", "RemoveTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                       // 112
	{"internal/media/tag_handlers.go", "CreateTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"TAG_CREATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                   // 166
	{"internal/media/tag_handlers.go", "PatchTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                        // 210
	{"internal/media/tag_handlers.go", "DeleteTag", "if err != nil {", "errResp(c, http.StatusBadRequest, \"DELETE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                // 231
	{"internal/media/tag_handlers.go", "ConfirmTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                       // 254
	{"internal/media/tag_handlers.go", "ConfirmTag", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                      // 268,276
	{"internal/media/tag_handlers.go", "ConfirmMediaTags", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                // 324,334
	{"internal/media/tag_handlers.go", "AITagsPreview", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                    // 418,427
	{"internal/media/tag_handlers.go", "AITagsTrigger", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"TAG_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                      // 469
	{"internal/media/tag_handlers.go", "AITagsTrigger", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                    // 477
	{"internal/media/upload.go", "Upload", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"READ_FAILED\", err.Error())", true, "原先回显 os 错误原文（含文件系统路径）→ 已改固定文案，完整错误只进服务端日志"},                                                                                                                                                           // 140,267
	{"internal/media/upload.go", "Upload", "if err := os.MkdirAll(h.UploadTmp, 0o755); err != nil {", "errResp(c, http.StatusInternalServerError, \"WRITE_FAILED\", err.Error())", true, "原先回显 os 错误原文（含文件系统路径）→ 已改固定文案，完整错误只进服务端日志"},                                                                                                                  // 183
	{"internal/media/upload.go", "Upload", "if err := os.WriteFile(metaPath, data, 0o600); err != nil {", "errResp(c, http.StatusInternalServerError, \"WRITE_FAILED\", err.Error())", true, "原先回显 os 错误原文（含文件系统路径）→ 已改固定文案，完整错误只进服务端日志"},                                                                                                              // 207
	{"internal/media/upload.go", "Upload", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"WRITE_FAILED\", err.Error())", true, "原先回显 os 错误原文（含文件系统路径）→ 已改固定文案，完整错误只进服务端日志"},                                                                                                                                                          // 248
	{"internal/media/upload.go", "Upload", "f.Close()", "errResp(c, http.StatusInternalServerError, \"WRITE_FAILED\", err.Error())", true, "原先回显 os 错误原文（含文件系统路径）→ 已改固定文案，完整错误只进服务端日志"},                                                                                                                                                                // 253
	{"internal/media/upload.go", "respondIngest", "}", "errResp(c, http.StatusInternalServerError, \"INGEST_FAILED\", err.Error())", true, "原先回显入库链路的原文（PG/os/元数据提取）→ 已改固定文案，完整错误只进服务端日志"},                                                                                                                                                             // 287
	{"internal/media/write_handlers.go", "Favorite", "if err := h.Store.SetFavorite(c.Request.Context(), id, c.GetString(\"user_id\"), req.Favorite); err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                         // 94
	{"internal/media/write_handlers.go", "Rate", "} else if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                   // 117
	{"internal/media/write_handlers.go", "Patch", "} else if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                  // 152,172
	{"internal/media/write_handlers.go", "Rotate", "} else if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                  // 243
	{"internal/media/write_handlers.go", "Rotate", "} else if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                 // 263
	{"internal/media/write_handlers.go", "Delete", "} else if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                 // 303
	{"internal/media/write_handlers.go", "Trash", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                          // 317
	{"internal/media/write_handlers.go", "Restore", "} else if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                // 335
	{"internal/media/write_handlers.go", "Purge", "} else if err != nil {", "errResp(c, http.StatusInternalServerError, \"UPDATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                  // 362
	{"internal/media/write_handlers.go", "Pano360", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                        // 394
	{"internal/search/handlers.go", "Search", "}", "c.JSON(status, gin.H{\"error\": gin.H{\"code\": code, \"message\": err.Error()}})", true, "混用分支：ErrInvalidCursor 为本端文案、其余为 DB 故障，原先共用一行 err.Error() → 已拆分，DB 分支固定文案"},                                                                                                                              // 31
	{"internal/shares/bandwidth.go", "PublicBandwidthTest", "bandwidthScope{Scope: \"share_token\", RefID: &refID}, up, down, lat); err != nil {", "errResp(c, http.StatusInternalServerError, \"SAVE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                             // 309
	{"internal/shares/bandwidth.go", "SelfTest", "if err := h.Store.SaveSelfTest(c.Request.Context(), bandwidthScope{Scope: \"global\"}, up, down, lat); err != nil {", "errResp(c, http.StatusInternalServerError, \"SAVE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                        // 325
	{"internal/shares/bandwidth.go", "GetBandwidth", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                       // 336
	{"internal/shares/bandwidth.go", "PatchBandwidth", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                     // 388,405
	{"internal/shares/bandwidth.go", "PatchBandwidth", "if err := h.Store.SaveManual(c.Request.Context(), sc, req.UpKbps, req.DownKbps); err != nil {", "errResp(c, http.StatusInternalServerError, \"SAVE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                        // 400
	{"internal/shares/handlers.go", "Create", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                              // 95
	{"internal/shares/handlers.go", "Create", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"HASH_FAILED\", err.Error())", true, "原先回显 bcrypt（第三方库）原文 → 已改固定文案，完整错误只进服务端日志"},                                                                                                                                                         // 109
	{"internal/shares/handlers.go", "Create", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"CREATE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                             // 131
	{"internal/shares/handlers.go", "List", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                // 160
	{"internal/shares/handlers.go", "Delete", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                              // 185
	{"internal/shares/handlers.go", "Delete", "if err := h.Store.Delete(c.Request.Context(), id); err != nil {", "errResp(c, http.StatusInternalServerError, \"DELETE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                             // 194
	{"internal/shares/handlers.go", "guardPublic", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                         // 217
	{"internal/shares/handlers.go", "PublicGet", "if err := h.Store.RecordAccess(ctx, sh.ID, c.ClientIP(), c.Request.UserAgent()); err != nil {", "errResp(c, http.StatusInternalServerError, \"LOG_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                               // 244
	{"internal/shares/handlers.go", "PublicGet", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                           // 249
	{"internal/shares/handlers.go", "PublicThumb", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                         // 271
	{"internal/shares/handlers.go", "PublicHLS", "if err != nil {", "errResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                           // 313,322
	{"internal/spaces/spaces.go", "Get", "Scan(&p.MediaCount, &p.UsedBytes); err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                       // 40
	{"internal/spaces/spaces.go", "Get", "if err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                       // 50
	{"internal/spaces/spaces.go", "Get", "if err := rows.Scan(&r.ID, &r.Name, &r.Role); err != nil {", "c.JSON(http.StatusInternalServerError, gin.H{\"error\": gin.H{\"code\": \"QUERY_FAILED\", \"message\": err.Error()}})", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                            // 57
	{"internal/transcode/transcode.go", "CreateJob", "if err != nil {", "errJSON(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                       // 136
	{"internal/transcode/transcode.go", "CreateJob", "VALUES ($1, 'hls', $2, 'pending') RETURNING id`, req.MediaID, req.Profile).Scan(&jobID); err != nil {", "errJSON(c, http.StatusInternalServerError, \"INSERT_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                // 152
	{"internal/transcode/transcode.go", "CreateJob", "if err != nil {", "errJSON(c, http.StatusInternalServerError, \"ENQUEUE_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                     // 161
	{"internal/transcode/transcode.go", "ServeHLS", "}", "errJSON(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())", true, "原先直接回显 pgx/PG 原文（SQLSTATE、表/列线索）→ 已改 httperr.Fail 固定文案，完整错误只进服务端日志"},                                                                                                                                      // 258
}

// errEcho 一条登记项。
//
// 匹配键 = File + "|" + Func + "|" + Prev + "|" + Pattern（归一化后的行内容）。
// 之所以既带 Func 又带 Prev（上一处非空行）：同一函数里同形的调用可能分属不同分支、
// 分类不同 —— 例如 geo/handlers.go 的 PutUIPrefs，ShouldBindJSON 的解析失败（标准库
// 原文，必须修）与 NormalizeUIPrefs 的校验失败（本端文案，可保留）在源码里是**同一行文字**，
// 只用行内容会把它们压成一条，从而无法分类。带上紧邻的上一行即可区分。
type errEcho struct {
	File    string // 相对 src/backend 的路径，slash 分隔
	Func    string // 所在函数名（方法名不含接收者）
	Prev    string // 上一处非空、非注释行（归一化）；分支判据
	Pattern string // 归一化后的行内容
	MustFix bool
	Reason  string
}

func (e errEcho) key() string {
	return e.File + "|" + e.Func + "|" + e.Prev + "|" + e.Pattern
}

// 命中的"响应写入"形态。按内容特征匹配（不只看函数名）：
// 三类本仓自有的响应 helper + gin/http 的通用写入 + 裸 gin.H 的 message 字段。
var responseWritePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\berrResp\(`), // albums/auth/media/shares 的错误封套 helper
	regexp.MustCompile(`\bfail\(`),    // audit/compute/faces 的错误封套 helper
	regexp.MustCompile(`\berrJSON\(`), // transcode 的错误封套 helper
	regexp.MustCompile(`\.JSON\(`),    // gin Context.JSON
	regexp.MustCompile(`\.AbortWithStatusJSON\(`),
	regexp.MustCompile(`\.String\(`),    // gin Context.String
	regexp.MustCompile(`http\.Error\(`), // net/http 的 Error（cmd/ 下工具）
	regexp.MustCompile(`"message":\s*err\.Error\(\)`),
	regexp.MustCompile(`"fail: "\s*\+\s*err\.Error\(\)`), // health.Ready 的 checks 拼装
}

var funcLineRe = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)

// scanHit 一处命中。
type scanHit struct {
	File string
	Func string
	Prev string
	Line int
	Text string // 归一化后的行内容
}

func (h scanHit) key() string { return h.File + "|" + h.Func + "|" + h.Prev + "|" + h.Text }

// backendRoot 由本测试文件位置反推 src/backend 根目录（不依赖 go test 的 cwd 约定）。
func backendRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败，无法定位仓库根")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatalf("解析仓库根失败: %v", err)
	}
	return filepath.ToSlash(root)
}

// scanErrEchoes 遍历 internal/ 与 cmd/ 下的 .go（排除 _test.go），检出响应写入里的 err.Error()。
func scanErrEchoes(t *testing.T, root string) []scanHit {
	t.Helper()
	var hits []scanHit
	for _, top := range []string{"internal", "cmd"} {
		dir := filepath.Join(root, top)
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)

			f, oerr := os.Open(path)
			if oerr != nil {
				return oerr
			}
			defer f.Close()

			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			curFunc := ""
			prev := ""
			lineNo := 0
			for sc.Scan() {
				lineNo++
				raw := sc.Text()
				trimmed := strings.TrimSpace(raw)
				if strings.HasPrefix(trimmed, "//") {
					continue // 注释里的提及不算回显（本项目曾因断言太宽而误报）
				}
				if m := funcLineRe.FindStringSubmatch(trimmed); m != nil {
					curFunc = m[1]
				}
				if !strings.Contains(raw, "err.Error()") {
					if trimmed != "" {
						prev = normalizeLine(raw)
					}
					continue
				}
				if !isResponseWrite(raw) {
					if trimmed != "" {
						prev = normalizeLine(raw)
					}
					continue
				}
				hits = append(hits, scanHit{
					File: rel,
					Func: curFunc,
					Prev: prev,
					Line: lineNo,
					Text: normalizeLine(raw),
				})
				prev = normalizeLine(raw)
			}
			return sc.Err()
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", dir, err)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})
	return hits
}

func isResponseWrite(line string) bool {
	for _, re := range responseWritePatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func normalizeLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestErrTextGuard 是守卫本体：命中必须登记；MustFix=true 不得出现在源码；
// MustFix=false 不得变成悬空登记。
func TestErrTextGuard(t *testing.T) {
	root := backendRoot(t)
	hits := scanErrEchoes(t, root)

	if os.Getenv("ERRTEXT_DUMP") != "" {
		for _, h := range hits {
			fmt.Printf("{%q, %q, %q, %q, false, \"\"}, // %d\n", h.File, h.Func, h.Prev, h.Text, h.Line)
		}
		fmt.Printf("// 共 %d 处命中\n", len(hits))
		return
	}

	reg := map[string]errEcho{}
	for _, e := range errEchoRegistry {
		if prev, dup := reg[e.key()]; dup {
			t.Fatalf("登记表有重复键（%s | %s | %s）：前一条 MustFix=%v，后一条 MustFix=%v —— "+
				"重复键会让分类互相覆盖，请合并或改用 Func/Pattern 区分",
				e.File, e.Func, e.Pattern, prev.MustFix, e.MustFix)
		}
		reg[e.key()] = e
	}

	hitByKey := map[string][]int{}
	for _, h := range hits {
		hitByKey[h.key()] = append(hitByKey[h.key()], h.Line)
	}

	var problems []string
	for _, h := range hits {
		e, ok := reg[h.key()]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"未登记的回显：%s:%d func=%s\n      %s", h.File, h.Line, h.Func, h.Text))
			continue
		}
		if e.MustFix {
			problems = append(problems, fmt.Sprintf(
				"已登记为 MustFix=true 但源码仍在回显：%s:%d func=%s\n      %s\n      理由: %s",
				h.File, h.Line, h.Func, h.Text, e.Reason))
		}
	}
	for _, e := range errEchoRegistry {
		if e.MustFix {
			continue // MustFix=true 的登记项修好后本就该从源码消失，不做悬空检查
		}
		if len(hitByKey[e.key()]) == 0 {
			problems = append(problems, fmt.Sprintf(
				"登记项悬空（源码里找不到这一行，请更新登记表）：%s func=%s\n      %s",
				e.File, e.Func, e.Pattern))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("err.Error() 回显守卫失败（%d 项）：\n  - %s", len(problems), strings.Join(problems, "\n  - "))
	}
	t.Logf("err.Error() 回显守卫通过：命中 %d 处，全部登记；MustFix=true 已清零", len(hits))
}
