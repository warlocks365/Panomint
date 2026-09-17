// Package audit 审计日志：写入（尽力而为）+ 查询 + 管理端只读端点。
//
// 依据：
//   - TDD v1.1 §5「审计日志 → `audit_log` 表（用户/动作/时间/IP）→ PostgreSQL → **永久**」
//   - 契约 v1.1 §2「管理端点（需 `admin:users`）」中的 `GET /admin/audit`（审计日志，分页）
//   - 契约 v1.1 §14 管理后台（`GET /admin/stats`）
//   - 契约 v1.1 §12 `GET /admin/jobs`（索引/转码任务列表与进度）
//
// # 三条硬口径
//
//  1. **只记敏感/有后果的操作**，不做全站请求日志。全站访问日志是
//     `middleware.Logging` 的职责（TDD §5：结构化 JSON 文件日志，保留 30 天），
//     在 audit_log 里再记一份只会重复并淹没真正的审计线索。本包记录的对象包括：
//     登录/登出、令牌轮换、用户与角色变更、删除与清除、分享创建与撤销、设置变更、
//     以及**审计日志自身的读取**。
//
//  2. **写入尽力而为**（见 Recorder.Record）：审计失败只打 warn 日志，**绝不**使业务
//     请求失败，也不会 panic。审计是观测手段，不允许成为新的故障点。
//
//  3. **写入前脱敏**（见 RedactDetail）：detail 里任何键名形如
//     password / token / secret / credential / cookie / hash ... 的条目会被**整键剔除**，
//     并递归作用于嵌套对象与数组。审计表不能成为密码与令牌的第二份副本 ——
//     这是审计模块自身的红线。
//
// # 关于「中间件自动记录」的决定
//
// 本包**只提供显式调用能力**，不提供路径白名单中间件。理由见 Recorder 的注释。
//
// ⚠️ 分层说明：本包在本次范围约束（只允许新增 `internal/audit/**`）下同时承载了
// 管理端只读端点（handlers.go）。若后续要按职责拆分，handlers.go 可整体平移到
// `internal/admin`，本包只留 Log / Query / Recorder。
package audit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 动作名登记表
// ---------------------------------------------------------------------------

// 归一化后的动作名（点分小写，见 NormalizeAction）。
//
// 集中登记的目的：过滤参数 `?action=` 是精确匹配，拼写漂移会让过滤静默失效
// （写的时候是 share.create、查的时候写 share.create.share 就永远查不到）。
// 新增动作请在 audit_test.go 的 TestActionRegistryIsValid 覆盖范围内登记。
//
// ⚠️ 下列常量中，本次改动只真正写入 ActionAuditRead —— 其余是**后续接入清单**
// （见交付报告的「后续接入清单」一节），落地时直接引用常量即可，不要手写字符串。
const (
	// ActionAuditRead 审计日志被读取。审计表含全站敏感操作史，读取它本身即有后果。
	ActionAuditRead = "admin.audit.read"

	// ActionLogin 登录成功（失败登录不写审计：那是 middleware.Logging + 限流的职责，
	// 且会引入"任何人可写审计表"的滥用面）。
	ActionLogin = "auth.login"
	// ActionLogout 登出。
	ActionLogout = "auth.logout"
	// ActionTokenRotate 令牌轮换（refresh token / agent_token / 分享口令）。
	ActionTokenRotate = "auth.token.rotate"

	// ActionMFASetup 开始设置二次验证（生成待确认的 TOTP 密钥）。
	//
	// 之所以连"开始设置"也要记：它是"有人正在给你的账号加一把钥匙"的信号。
	// 若攻击者拿到会话并开始绑定自己的认证器，这条记录是唯一的早期线索
	// （confirm 之前账号本身还没被锁，用户仍能自救）。
	ActionMFASetup = "auth.mfa.setup"
	// ActionMFAEnable 二次验证已生效（TOTP 确认成功）。
	ActionMFAEnable = "auth.mfa.enable"
	// ActionMFADisable 二次验证被关闭。**高危**：等同于账号少一道防线。
	ActionMFADisable = "auth.mfa.disable"

	// ActionUserCreate 创建用户。
	ActionUserCreate = "admin.user.create"
	// ActionUserUpdate 改用户（角色/状态/重置密码）。
	ActionUserUpdate = "admin.user.update"
	// ActionUserDelete 禁用/删除用户。
	ActionUserDelete = "admin.user.delete"
	// ActionRoleChange 角色与权限变更。
	ActionRoleChange = "admin.role.change"

	// ActionShareCreate 创建分享链接。
	ActionShareCreate = "share.create"
	// ActionShareRevoke 撤销分享链接。
	ActionShareRevoke = "share.revoke"

	// ActionMediaDelete 移入回收站。
	ActionMediaDelete = "media.delete"
	// ActionMediaPurge 从回收站彻底清除，不可恢复。
	ActionMediaPurge = "media.purge"
	// ActionMediaRestore 从回收站恢复。
	//
	// 与 delete / purge 并列，但语义相反：它**撤销**「在回收站里」这个状态。
	// 之所以必须记：没有它，`media.delete` 就是一笔**无法闭合**的账 —— 库里只有
	// 「谁在什么时候把 X 删了」，而没有任何地方记录「后来又拿回来了」，于是
	// **「已删除」与「删了又恢复」在审计上不可区分**，「当前还在回收站」这件事只能靠
	// 现有数据反推。用户侧的直接后果是工具箱页的「已恢复」标签没有数据来源。
	ActionMediaRestore = "media.restore"

	// ActionSettingsPatch 系统配置变更。
	ActionSettingsPatch = "admin.settings.patch"
	// ActionIndexRebuild 索引重建。
	ActionIndexRebuild = "admin.index.rebuild"
)

// 目标类型登记表（写入 target_type 的推荐取值；空表示无特定对象）。
const (
	TargetUser        = "user"
	TargetRole        = "role"
	TargetShare       = "share"
	TargetMedia       = "media"
	TargetSetting     = "setting"
	TargetComputeNode = "compute_node"
	TargetAuditLog    = "audit_log"
)

// ---------------------------------------------------------------------------
// 规格常量（与 DDL / 迁移 00020 对齐）
// ---------------------------------------------------------------------------

const (
	// maxActionLen 对齐 audit_log.action 的 VARCHAR(128)。
	maxActionLen = 128
	// maxTargetTypeLen 对齐 00020 新增的 target_type VARCHAR(64)。
	maxTargetTypeLen = 64
	// maxTargetIDLen 对齐 00020 新增的 target_id VARCHAR(128)。
	maxTargetIDLen = 128
	// maxIPLen 对齐 audit_log.ip 的 VARCHAR(64)。
	maxIPLen = 64
	// maxUserAgentLen user_agent 是 TEXT，但 UA 完全由客户端控制，
	// 必须截断 —— 否则一个 8KB 的恶意 UA 就能把审计表当放大器用。
	maxUserAgentLen = 512

	// 分页默认值与上限。
	defaultAuditLimit = 50
	maxAuditLimit     = 200
	defaultJobsLimit  = 50
	maxJobsLimit      = 200
)

var (
	// actionRe 点分小写、以字母开头。不允许空白与任意符号。
	actionRe = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)
	// targetTypeRe 下划线小写、以字母开头。
	targetTypeRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	// uuidRe actor_user_id 是 users(id) 外键，必须在写入前挡住非 UUID，
	// 否则会以「外键冲突」的形式失败 —— 那属于脏数据，不是我们要的尽力而为。
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ---------------------------------------------------------------------------
// 记录模型
// ---------------------------------------------------------------------------

// Entry 一条审计记录。
//
// 字段名与 DDL 的对应关系：ActorUserID ↔ audit_log.user_id（历史列名，不改名）。
type Entry struct {
	ID          int64          `json:"id,omitempty"`
	ActorUserID string         `json:"actor_user_id,omitempty"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type,omitempty"`
	TargetID    string         `json:"target_id,omitempty"`
	IP          string         `json:"ip,omitempty"`
	UserAgent   string         `json:"user_agent,omitempty"`
	Detail      map[string]any `json:"detail,omitempty"`
	At          time.Time      `json:"at"`
}

// Normalize 校验并归一化（返回值是副本，不修改接收者）。
//
// 归一化而非报错的部分：去首尾空白、action/target_type 转小写、IP/UA 截断与控制字符
// 剔除、detail 脱敏、At 补零值为当前 UTC 时间。
// 报错的部分：动作非法（空/超长/含非法字符）、actor 非 UUID、target_id 无 target_type。
//
// 设计取舍：**宁可拒写也不落脏数据**。此处返回的错误由 Recorder.Record 吞掉并记 warn
// ——拒写一条格式错误的审计，比往审计表里塞进无法过滤的垃圾要好。
func (e Entry) Normalize() (Entry, error) {
	action, err := NormalizeAction(e.Action)
	if err != nil {
		return Entry{}, err
	}
	actor, err := NormalizeActor(e.ActorUserID)
	if err != nil {
		return Entry{}, err
	}
	targetType, err := NormalizeTargetType(e.TargetType)
	if err != nil {
		return Entry{}, err
	}
	targetID, err := NormalizeTargetID(e.TargetID)
	if err != nil {
		return Entry{}, err
	}
	// 不变量：有 target_id 必有 target_type。
	// 只有 id 没有类型，查询侧无法拼出「查某用户被改过什么」这类最有用的问句。
	if targetID != "" && targetType == "" {
		return Entry{}, errors.New("audit: 指定 target_id 时必须同时给出 target_type")
	}

	out := e
	out.Action = action
	out.ActorUserID = actor
	out.TargetType = targetType
	out.TargetID = targetID
	out.IP = NormalizeIP(e.IP)
	out.UserAgent = NormalizeUserAgent(e.UserAgent)
	out.Detail = RedactDetail(e.Detail)
	if out.At.IsZero() {
		out.At = time.Now().UTC()
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 归一化与校验（纯函数，便于直接断言）
// ---------------------------------------------------------------------------

// NormalizeAction 归一化动作名：去首尾空白 + 转小写，然后按 [a-z0-9_.] 校验。
func NormalizeAction(s string) (string, error) {
	a := strings.ToLower(strings.TrimSpace(s))
	if a == "" {
		return "", errors.New("audit: action 不能为空")
	}
	if len(a) > maxActionLen {
		return "", fmt.Errorf("audit: action 超长（%d > %d 字节）", len(a), maxActionLen)
	}
	if !actionRe.MatchString(a) {
		return "", fmt.Errorf("audit: action 仅允许 [a-z0-9_.] 且以字母开头，实际 %q", s)
	}
	return a, nil
}

// NormalizeActor 归一化 actor_user_id：空串合法（系统/匿名触发），非空必须是 UUID。
func NormalizeActor(s string) (string, error) {
	a := strings.TrimSpace(s)
	if a == "" {
		return "", nil
	}
	if !uuidRe.MatchString(a) {
		return "", fmt.Errorf("audit: actor_user_id 必须是 UUID，实际 %q", s)
	}
	return strings.ToLower(a), nil
}

// NormalizeTargetType 归一化 target_type：空串合法。
func NormalizeTargetType(s string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return "", nil
	}
	if len(t) > maxTargetTypeLen {
		return "", fmt.Errorf("audit: target_type 超长（%d > %d 字节）", len(t), maxTargetTypeLen)
	}
	if !targetTypeRe.MatchString(t) {
		return "", fmt.Errorf("audit: target_type 仅允许 [a-z0-9_] 且以字母开头，实际 %q", s)
	}
	return t, nil
}

// NormalizeTargetID 归一化 target_id：空串合法；只做长度与控制字符约束
// （目标标识不一定是 UUID —— 分享 token、设置键名都可能是任意短串）。
func NormalizeTargetID(s string) (string, error) {
	id := strings.TrimSpace(s)
	if id == "" {
		return "", nil
	}
	if len(id) > maxTargetIDLen {
		return "", fmt.Errorf("audit: target_id 超长（%d > %d 字节）", len(id), maxTargetIDLen)
	}
	if strings.ContainsFunc(id, isControl) {
		return "", errors.New("audit: target_id 不得含控制字符")
	}
	return id, nil
}

// NormalizeIP 归一化 IP：去空白、剔除控制字符、按 ip 列宽截断。
// 入参来自 c.ClientIP()（受 X-Forwarded-For 影响），一律当不可信输入处理。
func NormalizeIP(s string) string {
	ip := stripControl(strings.TrimSpace(s))
	if len(ip) > maxIPLen {
		ip = ip[:maxIPLen]
	}
	return ip
}

// NormalizeUserAgent 归一化 UA：去空白、剔除控制字符、按 rune 截断到 512。
// 按 rune 而非 byte 截断，避免把多字节字符劈成非法 UTF-8。
func NormalizeUserAgent(s string) string {
	r := []rune(stripControl(strings.TrimSpace(s)))
	if len(r) > maxUserAgentLen {
		r = r[:maxUserAgentLen]
	}
	return string(r)
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// stripControl 剔除控制字符（含 \r\n\t）。原样返回无控制字符的输入，避免多余分配。
func stripControl(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isControl(r) {
			return -1
		}
		return r
	}, s)
}

// ---------------------------------------------------------------------------
// 脱敏
// ---------------------------------------------------------------------------

// sensitiveKeyParts 键名命中任一子串（大小写不敏感）即整键剔除。
//
// 刻意保守：宁可少记一个字段，也不能把密钥写进审计表。审计表是**永久保留**的，
// 一旦落进去就再也收不回来 —— 这是本模块唯一不能事后补救的错误。
var sensitiveKeyParts = []string{
	// 口令类
	"password", "passwd", "pwd",
	// 密钥与凭据类
	"secret", "credential", "apikey", "api_key", "private_key", "signature", "hash",
	// 令牌与会话类
	"token", "jwt", "bearer", "session", "cookie", "authorization",
	// 二次验证类
	"otp", "totp", "mfa",
}

// IsSensitiveKey 判断键名是否命中脱敏名单（导出以便测试与后续调用方自检）。
func IsSensitiveKey(k string) bool {
	low := strings.ToLower(k)
	for _, p := range sensitiveKeyParts {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// RedactDetail 返回脱敏后的**副本**（不修改入参，调用方的 map 可继续复用）。
//
// 语义：
//   - 入参为 nil 或空 map → 返回 nil（detail 列落 NULL）；
//   - 入参非空但键全被剔除 → 返回**空 map**，落库为 `{}`。
//     这是刻意的：`{}` 诚实表达「有过 detail，但内容全属敏感」，
//     而 NULL 会被读成「压根没记 detail」，两者在追责时含义完全不同。
func RedactDetail(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if IsSensitiveKey(k) {
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

// redactValue 递归脱敏嵌套结构。
// 只处理 JSON 反序列化会产出的类型（map[string]any / []any）与显式传入的
// map[string]string；其余标量原样返回。
func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return RedactDetail(t)
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, redactValue(item))
		}
		return out
	case map[string]string:
		m := make(map[string]any, len(t))
		for k, s := range t {
			if IsSensitiveKey(k) {
				continue
			}
			m[k] = s
		}
		if len(m) == 0 {
			return map[string]any{}
		}
		return m
	default:
		return v
	}
}
