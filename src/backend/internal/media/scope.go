package media

import "panoalbum/internal/mediascope"

// 本文件是**薄转发层**：media 包对外的空间作用域 API 名字与签名保持不变，
// 内部全部委托给 internal/mediascope —— 可见性谓词的**唯一真源**（设计说明、历史事故记录、
// fail-closed 规则、shared 的成员∪属主口径，全部在该包的注释里，请去那里读）。
//
// 为什么做成转发而不是直接让调用方改 import：
// internal/media 下的既有调用点（handlers.go / timeline.go / histogram.go / duplicates.go）
// 与既有测试（scope_test.go）一处都不用改 —— 收敛就不会"顺带"改坏别的东西。
//
// 为什么 ErrInvalidSpace / ErrMissingUser 用 var 而不是 const：
// 必须保持**同一个 error 值身份**，否则 handlers.go 里的 errors.Is(err, ErrMissingUser)
// 会失效（重新构造出的 error 与 mediascope 里的不是同一个值）。

// ErrInvalidSpace space 取值不在白名单内（真源：mediascope.ErrInvalidSpace）。
var ErrInvalidSpace = mediascope.ErrInvalidSpace

// ErrMissingUser 缺少可信身份（真源：mediascope.ErrMissingUser）。
var ErrMissingUser = mediascope.ErrMissingUser

// MediaScope 已解析的媒体可见作用域（真源：mediascope.Scope）。
type MediaScope = mediascope.Scope

// ResolveMediaScope 把 space 查询参数解析为作用域（真源：mediascope.Resolve）。
func ResolveMediaScope(space, userID string) (MediaScope, error) {
	return mediascope.Resolve(space, userID)
}

// failClosed 恒假条件。
//
// 值取自 mediascope.FailClosed（而不是在本包再写一份 "false" 字面量）：
// 本包的既有测试把它当作期望值引用，若两处各写一份，就又会退化成"两份真源"。
const failClosed = mediascope.FailClosed

// scopeConds 组装作用域谓词（真源：mediascope.Conds；调用约定见那里的注释）。
func scopeConds(s MediaScope) ([]string, []any) {
	return mediascope.Conds(s)
}
