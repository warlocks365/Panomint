// Package mediascope 是「媒体可见性」谓词的**唯一真源**：解析在 Resolve，谓词在 Conds / VisibleCond。
//
// 本包是**叶子包**：只依赖标准库（errors / fmt），不引入任何项目内包 ——
// 因此 internal/media 与 internal/search 都可以依赖它，而它不可能参与任何 import 环。
//
// 本文件是 /media 系列端点（GET /media、/media/date-histogram、/media/duplicates）与
// /search（语义并集分支）**空间作用域的唯一真源**。
//
// 为什么值得单独一个包、并写这么多注释：
// 这处曾发生过**跨用户越权泄漏**（已实测复现并修复）。原实现里
// 「space 缺省时不追加任何属主条件」，于是任何持 media:read 的账号只需
// `GET /media?limit=200` 就能拿到全站他人个人空间的媒体
// （实测 93 条全部属 owner@pano.local；加入临时账号夹具后为 95 条 = owner 93 + 他人 2）；
// /media/date-histogram 与 /media/duplicates 同源，同样漏（前者泄漏他人媒体的按日计数，
// 后者直接泄漏他人的重复组与媒体 ID）。
//
// 根因不是"少写了一个 if"，而是**作用域默认为"全部"**：只要调用方忘了指定，
// 条件就消失，而消失比写错更难被发现 —— SQL 不会报错，只会多返回数据。
// 因此本文件的设计规则是：
//
//  1. 不存在"全库"这一档。作用域只有 personal / shared 两档，且都必须携带主体；
//  2. 任何无法证明安全的作用域一律收敛为恒假条件（FailClosed），
//     使"忘记解析作用域"的后果是**空结果**而不是**全库**；
//  3. space 取值走白名单，枚举外取值在 handler 层就被 400 拦掉，
//     不把 PostgreSQL 的枚举原文（`invalid input value for enum media_space: "bogus"`）透给调用方。
//
// 关于本包的存在理由（第二处越权风险）：
// 同一段「调用者本人可见集合」的谓词曾在 internal/search/query.go 被**第二次手写**，
// 只判 shared_space_members 而漏掉 shared_space.owner_id，且与 media 侧各自维护、
// 谁改了另一边都不知道。现收敛为共享函数：media 用 Conds、search 用 VisibleCond，
// 两侧的 shared 臂都出自同一个 sharedVerdict。
package mediascope

import (
	"errors"
	"fmt"
)

// ErrInvalidSpace space 取值不在白名单内。
var ErrInvalidSpace = errors.New("space 仅支持 personal|shared")

// ErrMissingUser 缺少可信身份（鉴权中间件未注入 user_id）。
// 绝不允许在无身份时放宽作用域：没有主体就没有合法的作用域。
var ErrMissingUser = errors.New("缺少用户身份")

// Scope 已解析的媒体可见作用域。
//
// 两档都必须携带主体（OwnerID / MemberID），否则视为非法作用域并收敛为空结果。
type Scope struct {
	Space    string // personal|shared
	OwnerID  string // personal：本人 user_id
	MemberID string // shared：用于 shared_space_members 成员判定
}

// Resolve 把 space 查询参数解析为作用域（参数缺失的安全默认值在此定死）。
//
//   - 缺省 / 空 → personal（调用者本人）。"没说要哪个空间"只能理解为"我自己的"，
//     绝不能理解为"全部"—— 这正是历史事故的成因；
//   - personal → 本人个人空间；
//   - shared   → 共享空间；**是否可见交由 SQL 的成员/属主判定**（失败关闭，见 Conds：
//     既非成员也非属主者得到空结果，而不是"所有 shared 媒体"）；
//   - 其余取值 → ErrInvalidSpace（handler 转 400 INVALID_PARAMS）。
//
// userID 为空视为身份缺失并返回 ErrMissingUser：此时若按 personal 继续组装，
// 会退化成不带属主条件、只剩 `m.space='personal'` 的查询 —— 即"全站所有个人空间媒体"，
// 与本次事故完全同形，故显式拒绝而非静默降级。
func Resolve(space, userID string) (Scope, error) {
	if userID == "" {
		return Scope{}, ErrMissingUser
	}
	switch space {
	case "", "personal":
		return Scope{Space: "personal", OwnerID: userID}, nil
	case "shared":
		return Scope{Space: "shared", MemberID: userID}, nil
	default:
		return Scope{}, ErrInvalidSpace
	}
}

// FailClosed 恒假条件：作用域无法证明安全时显式收窄为"无结果"。
//
// 用恒假而不是"什么都不加"，是因为漏加条件不会报错、只会多返回数据；
// 而恒假会立刻表现为"页面空了"，是可被发现、可被测试断言的失败。
//
// 为什么是**导出**的（原名 failClosed，包内私有）：
// internal/media 的既有测试直接引用包内 `failClosed` 作为期望值，而那份测试与
// internal/media 下的既有调用点在本轮收敛中「一处都不许改」。media 的薄转发层
// （media/scope.go）必须能引用**同一个值**，否则就得在两个包里各写一份 "false"
// 字面量 —— 那等于在"单一真源"的题面上重新造出两份真源。
const FailClosed = "false"

// sharedVerdict 共享空间可见性臂：成员 ∪ 属主，两个 EXISTS 绑**同一个**占位符 $n。
//
// ⚠️ 这是本包"单一真源"的落地载体：Conds 的 shared 档与 VisibleCond 的 shared 臂
// 都调用本函数，任何一侧想改口径都必须改这里，另一侧自动跟随。
//
// 关于 shared 的口径：media 表**没有 space_id 列**（见 migrations/00004_ddl_part.sql），
// 共享空间无法按"具体某个空间"归属，因此"共享可见" = space='shared' 且调用者
// **是该共享空间的成员或属主**（membership ∪ ownership）。两个 EXISTS 都绑同一个占位符，
// 故只占一个参数位。非成员且非属主者得到**空结果**（失败关闭），而不是"所有 shared 媒体"。
//
// 文本用**转义字符串**而非 raw string 书写：这段 SQL 的换行与缩进会进入最终 SQL 文本，
// 用 `\n` / `\t` 显式写出可以避免"编辑器把 tab 变成空格"这类不可见漂移
// （原实现在 internal/media/scope.go 里是 raw string，字节为：一条 `\n` + 4 个 tab + `OR EXISTS(...)`）。
func sharedVerdict(n int) string {
	return fmt.Sprintf("(EXISTS(SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $%[1]d)\n"+
		"\t\t\t\tOR EXISTS(SELECT 1 FROM shared_space ss WHERE ss.owner_id = $%[1]d))", n)
}

// folderGrantArm 目录级授权可见性臂（Job000069）：媒体 folder_path 命中一个授予
// 调用者 read 的注册目录（精确或子目录前缀），即对该调用者可见。
// 与 sharedVerdict 同族：单一真源，VisibleCondFor 与 ReadCond 两臂都调本函数，
// 绑同一个占位符 $n，只占一个参数位（目录授权按 user_id 判定，不需要 role）。
// grants 元素形态：[{"user_id":"uuid","read":true,"write":true}] —— write 不
// 进本谓词（写权限在端点层校验）；read=false 的元素在此自然不命中。
func folderGrantArm(n int, alias string) string {
	fp := qual(alias, "folder_path")
	return fmt.Sprintf("EXISTS(SELECT 1 FROM folder_dirs gf, jsonb_array_elements(gf.grants) gfge"+
		" WHERE (%[1]s = gf.path OR %[1]s LIKE gf.path || '/%%')"+
		" AND gfge->>'user_id' = $%[2]d AND (gfge->>'read')::boolean)", fp, n)
}

// qual 给 media 表的列名加上表别名前缀；alias 为空表示该查询未给 media 起别名。
//
// 为什么需要这一层：本包原先把 `m.` **硬编码**进谓词文本，于是只有"恰好把 media
// 起名为 m"的查询能用它。实测 internal/geo 的四个地图查询用的是**不带别名的裸列名**
// （`FROM media` + `gps` / `deleted_at`），因此无法复用本包，只能继续手写、继续漂移；
// 而"只能被两个包用"的所谓唯一真源，在共享语义上是名不副实的。
//
// 现在谓词与别名解耦：
//   - alias = "m"（既有调用点）→ 输出与收敛前**逐字节相同**，零影响；
//   - alias = ""（裸列名）→ 任何未加别名的单表查询都能直接接入。
func qual(alias, col string) string {
	if alias == "" {
		return col
	}
	return alias + "." + col
}

// Conds 组装作用域谓词（/media 系列三个端点共用，防止各写各的而漂移）。
//
// 等价于 CondsFor(s, "m")：保留原签名，使既有调用点与既有测试**一处都不用改**。
//
// ⚠️ 调用约定：调用方必须把返回的 args **最先**追加进自己的 args 切片，
// 因为内部占位符从 $1 开始编号（其余条件一律按 len(args) 续编）。
//
// ⚠️ 与 search 侧的口径差异（**已收敛，保留记录**）：
// internal/search/query.go 的可见性谓词原先只判 `shared_space_members`，不看
// `shared_space.owner_id` —— 差异只影响"空间属主但没有 members 行"这一种人：
// 时间轴会给他看、搜索不给。这个方向是**收窄**（不会漏），
// 且当时没有任何 API 能创建共享空间（无 POST /spaces），故那条差异不可达。
// 现已按当时的计划收敛：谓词被提成两个包共用的函数（本包 Conds / VisibleCond 共用 sharedVerdict），
// search 侧从此也认空间属主，与时间轴口径一致。收敛只会让 search 的可见集合
// **变宽到与时间轴相等**，不会变宽到泄漏：谓词仍绑定调用者本人（$start），
// 且 userID 为空时 VisibleCond 恒假。
func Conds(s Scope) ([]string, []any) { return CondsFor(s, "m") }

// CondsFor 与 Conds 同义，但允许指定 media 表的别名（alias 为空 = 不加前缀）。
//
// 需要**区分空间档位**的调用方（space=personal|shared 白名单）用这个；
// 需要"调用者本人可见集合的并集"的调用方用 VisibleCondFor。
func CondsFor(s Scope, alias string) ([]string, []any) {
	switch s.Space {
	case "personal":
		if s.OwnerID == "" {
			return []string{FailClosed}, nil
		}
		// 目录级授权臂（Job000069）：personal 列表 = 本人 owner 的 ∪ 被授 read 的
		// 注册目录下的（媒体 space 仍 personal、owner 是目录属主，靠 folderGrantArm 命中）。
		return []string{
			qual(alias, "space") + " = 'personal'",
			"(" + qual(alias, "owner_id") + " = $1 OR " + folderGrantArm(1, alias) + ")",
		}, []any{s.OwnerID}
	case "shared":
		if s.MemberID == "" {
			return []string{FailClosed}, nil
		}
		return []string{qual(alias, "space") + " = 'shared'", sharedVerdict(1)}, []any{s.MemberID}
	default:
		// space 为空（未解析）或枚举外取值：不放行任何行。
		return []string{FailClosed}, nil
	}
}

// VisibleCond 产出「调用者本人可见集合」的**并集**谓词，供 search 侧使用
// （媒体无 space_id 列，"共享可见"= 是该共享空间的成员或属主）。
//
// 语义 = (m.space = 'personal' AND m.owner_id = $start)
//
//	OR (m.space = 'shared'  AND (是 shared_space_members 成员 或 是 shared_space.owner_id))
//
// 占位符从 $start 开始编号：personal 臂与 shared 臂的两个 EXISTS **复用同一个 $start**，
// 因此返回的 args **恰好 1 个元素**（userID），调用方只需追加它一个参数。
// 这一点必须被测试钉住：占位符个数与 args 个数不一致会直接导致
// pgx 运行时报 "expected N arguments"（不是编译期错误）。
//
// ⚠️ userID 为空时**必须 fail-closed**（返回恒假 "false" 且不带参数）：
// 绝不能返回一段"不绑定主体"的谓词 —— 那正是历史越权事故的同形
// （调用方拿不到主体时，唯一安全的输出是"谁也看不见"）。
func VisibleCond(start int, userID string) (string, []any) {
	return VisibleCondFor(start, userID, "m")
}

// VisibleCondFor 与 VisibleCond 同义，但允许指定 media 表的别名（alias 为空 = 不加前缀）。
//
// ⚠️ 这是"新增任何返回 media 行的查询"的**默认入口**：凡是要把 media 行返回给终端用户的
// 查询，都应调用本函数或 CondsFor，而不是自己拼谓词。理由见包文档：
// 本包之外的每一份手写副本都迟早会漂移，而漂移方向往往是**放宽**（漏掉某一臂 = 多返回数据，
// SQL 不报错）。alias 参数的存在就是为了让"表别名不同"不再成为不复用的借口。
func VisibleCondFor(start int, userID, alias string) (string, []any) {
	if userID == "" {
		return FailClosed, nil
	}
	return fmt.Sprintf("((%[3]s = 'personal' AND %[4]s = $%[1]d)\n"+
		"\t\tOR (%[3]s = 'shared' AND %[2]s)\n"+
		"\t\tOR (%[5]s))",
		start, sharedVerdict(start), qual(alias, "space"), qual(alias, "owner_id"), folderGrantArm(start, alias)), []any{userID}
}

// RolePrivileged 全局特权角色（owner/admin）：绕过一切归属判定、见全量，
// 这是刻意的管理员语义（与 media 包写路径 canAccess 的取值保持一致 ——
// 取值若漂移，两侧必须同时改，故也收敛进本包）。
func RolePrivileged(role string) bool { return role == "owner" || role == "admin" }

// ReadCond 单条媒体「读」访问的完整判定（P2-01：Detail / Thumb / Download 共用）。
//
// 口径 = 属主 ∪（media.space='shared' 且调用者是共享空间成员或属主）∪ owner/admin 角色。
//
// 为什么也必须收敛在本包：/media?space=shared 的**列表**可见性（Conds 的 shared 臂）
// 与单条媒体的详情/缩略图/下载曾各写一份 —— detail.go 的 canAccess 只认本人/owner/admin，
// 造成「列表可见、点进去 403」的口径断裂（共享空间功能对成员实际不可用）。
// 现在单条读判定与列表判定共用同一个 sharedVerdict：改口径只改一处，两侧自动跟随。
//
// 返回值约定（与 VisibleCondFor 同族）：
//   - owner/admin 角色 → ("true", nil)：特权短路，不进 SQL、不占参数位；
//   - userID 为空 → (FailClosed, nil)：无主体即「谁也看不见」，
//     绝不能返回不绑定主体的谓词（历史越权事故的同形，见包文档）；
//   - 其余 → (谓词, [userID])：谓词占位符 $start 绑定调用者，args 恰好 1 个元素。
//
// 与 VisibleCondFor 的差别：VisibleCondFor 是「调用者可见集合」的**行过滤**谓词
// （personal 臂要求 space='personal' AND owner_id=$n）；ReadCond 是「这一条媒体
// 能不能读」的**单行判定**（属主不受 space 限制 —— 属主读自己的 shared 媒体当然合法）。
func ReadCond(start int, userID, role, alias string) (string, []any) {
	if RolePrivileged(role) {
		return "true", nil
	}
	if userID == "" {
		return FailClosed, nil
	}
	return fmt.Sprintf("(%[3]s = $%[1]d OR (%[4]s = 'shared' AND %[2]s) OR (%[5]s))",
		start, sharedVerdict(start), qual(alias, "owner_id"), qual(alias, "space"), folderGrantArm(start, alias)), []any{userID}
}
