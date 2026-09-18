// Package pgxutil 放置跨包共用、只依赖 pgx 错误类型的判定（叶子包：只 import errors 与 pgconn）。
//
// 抽成独立包的理由不是"整洁"，而是**单一真源**：本项目的畸形 id 判定原先以私有函数
// isMalformedID 只存在于 internal/media/thumb.go，其余包（albums/faces/transcode/…）
// 想用只能各自复制一份；一旦复制，就会像此前的「7 份列清单、6 处可见性谓词」一样漂移。
package pgxutil

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsMalformedID 判定「请求里的 id 不是合法 UUID」这类 Postgres 输入语法错误（SQLSTATE 22P02）。
//
// 报错形如 `ERROR: invalid input syntax for type uuid: "not-a-uuid" (SQLSTATE 22P02)`，
// 由 media.id / tags.id / albums.id / people.id / transcode_jobs.id 这些 **uuid 列**
// 在收到非法文本时直接抛出。
//
// 为什么必须与「不存在」同归 404，而不是 500：
//   - 语义上二者对调用方是同一件事——拿不到这条记录。请求方拼错了 id 不该被当成服务故障；
//   - 500 会污染可用性监控、误导排障方向（真故障与"客户端传了个烂 id"混在一起）；
//   - 500 分支的惯例是把 err.Error() 回给客户端，而 PG 原文会带出 SQLSTATE、类型名、
//     列名等内部实现细节 —— 任何外部输入都不该让服务把这些吐出去。
//
// 真正的 DB 故障（连接失败、超时；或语法正确的 UUID 但查询失败）**不在此列**，
// 仍旧映射为 500，不会被伪装成「不存在」。
func IsMalformedID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
