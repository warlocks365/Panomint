package media

import "errors"

// 本文件是 /media 系列端点（GET /media、/media/date-histogram、/media/duplicates）
// **空间作用域的唯一真源**：解析在 ResolveMediaScope，谓词在 scopeConds。
//
// 为什么值得单独一个文件、并写这么多注释：
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
//	1. 不存在"全库"这一档。作用域只有 personal / shared 两档，且都必须携带主体；
//	2. 任何无法证明安全的作用域一律收敛为恒假条件（failClosed），
//	   使"忘记解析作用域"的后果是**空结果**而不是**全库**；
//	3. space 取值走白名单，枚举外取值在 handler 层就被 400 拦掉，
//	   不把 PostgreSQL 的枚举原文（`invalid input value for enum media_space: "bogus"`）透给调用方。

// ErrInvalidSpace space 取值不在白名单内。
var ErrInvalidSpace = errors.New("space 仅支持 personal|shared")

// ErrMissingUser 缺少可信身份（鉴权中间件未注入 user_id）。
// 绝不允许在无身份时放宽作用域：没有主体就没有合法的作用域。
var ErrMissingUser = errors.New("缺少用户身份")

// MediaScope 已解析的媒体可见作用域。
//
// 两档都必须携带主体（OwnerID / MemberID），否则视为非法作用域并收敛为空结果。
type MediaScope struct {
	Space    string // personal|shared
	OwnerID  string // personal：本人 user_id
	MemberID string // shared：用于 shared_space_members 成员判定
}

// ResolveMediaScope 把 space 查询参数解析为作用域（参数缺失的安全默认值在此定死）。
//
//   - 缺省 / 空 → personal（调用者本人）。"没说要哪个空间"只能理解为"我自己的"，
//     绝不能理解为"全部"—— 这正是历史事故的成因；
//   - personal → 本人个人空间；
//   - shared   → 共享空间；**是否可见交由 SQL 的成员/属主判定**（失败关闭，见 scopeConds：
//     既非成员也非属主者得到空结果，而不是"所有 shared 媒体"）；
//   - 其余取值 → ErrInvalidSpace（handler 转 400 INVALID_PARAMS）。
//
// userID 为空视为身份缺失并返回 ErrMissingUser：此时若按 personal 继续组装，
// 会退化成不带属主条件、只剩 `m.space='personal'` 的查询 —— 即"全站所有个人空间媒体"，
// 与本次事故完全同形，故显式拒绝而非静默降级。
func ResolveMediaScope(space, userID string) (MediaScope, error) {
	if userID == "" {
		return MediaScope{}, ErrMissingUser
	}
	switch space {
	case "", "personal":
		return MediaScope{Space: "personal", OwnerID: userID}, nil
	case "shared":
		return MediaScope{Space: "shared", MemberID: userID}, nil
	default:
		return MediaScope{}, ErrInvalidSpace
	}
}

// failClosed 恒假条件：作用域无法证明安全时显式收窄为"无结果"。
//
// 用恒假而不是"什么都不加"，是因为漏加条件不会报错、只会多返回数据；
// 而恒假会立刻表现为"页面空了"，是可被发现、可被测试断言的失败。
const failClosed = "false"

// scopeConds 组装作用域谓词（/media 系列三个端点共用，防止各写各的而漂移）。
//
// ⚠️ 调用约定：调用方必须把返回的 args **最先**追加进自己的 args 切片，
// 因为内部占位符从 $1 开始编号（其余条件一律按 len(args) 续编）。
//
// 关于 shared 的口径：media 表**没有 space_id 列**（见 migrations/00004_ddl_part.sql），
// 共享空间无法按"具体某个空间"归属，因此"共享可见" = space='shared' 且调用者
// **是该共享空间的成员或属主**（membership ∪ ownership）。两个 EXISTS 都绑同一个 $1，
// 故只占一个参数位。非成员且非属主者得到**空结果**（失败关闭），而不是"所有 shared 媒体"。
//
// ⚠️ 与 search 侧的口径差异（已知，安全方向）：internal/search/query.go 的 visibleCond
// 只判 `shared_space_members`，不看 `shared_space.owner_id`。差异只影响"空间属主但没有
// members 行"这一种人：时间轴会给他看、搜索不给。这个方向是**收窄**（不会漏），
// 且当前没有任何 API 能创建共享空间（无 POST /spaces），故这条差异暂时不可达。
// 真正的收敛做法是把这条谓词提成两个包共用的函数，而不是各自维护——留给后续任务。
func scopeConds(s MediaScope) ([]string, []any) {
	switch s.Space {
	case "personal":
		if s.OwnerID == "" {
			return []string{failClosed}, nil
		}
		return []string{"m.space = 'personal'", "m.owner_id = $1"}, []any{s.OwnerID}
	case "shared":
		if s.MemberID == "" {
			return []string{failClosed}, nil
		}
		return []string{
			"m.space = 'shared'",
			`(EXISTS(SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $1)
				OR EXISTS(SELECT 1 FROM shared_space ss WHERE ss.owner_id = $1))`,
		}, []any{s.MemberID}
	default:
		// space 为空（未解析）或枚举外取值：不放行任何行。
		return []string{failClosed}, nil
	}
}
