// Package migrations 仅承载一个职责：把 SQL 迁移目录嵌入 Go 二进制。
//
// 为什么需要它：cmd/api 在 v1.0.0 起承担"启动自迁移"（发布形态"解压即起"的根基），
// 而 go:embed 不允许引用上级目录（../），所以 embed 指令必须落在 SQL 文件所在的
// 这一层目录里 —— 这就是本文件存在的原因。cmd/migrate 与 cmd/api 共用同一份 FS，
// 保证"手工迁移"与"自动迁移"永远读到同一批文件。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
