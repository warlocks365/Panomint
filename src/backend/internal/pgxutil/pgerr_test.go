package pgxutil

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsMalformedID 是畸形 id 判定的**单一真源**（原 internal/media 的私有 isMalformedID
// 迁来此处，media/albums/faces/transcode 四个包共用）。
//
// 两个方向都必须钉住：
//   - 22P02 必须为 true，否则畸形 id 会掉进 500 分支并把 PG 原文回给客户端；
//   - 其它错误（尤其非 PgError 与其它 SQLSTATE）必须为 false，否则真实故障会被
//     伪装成「不存在」，把排障线索一起抹掉。
func TestIsMalformedID(t *testing.T) {
	if !IsMalformedID(&pgconn.PgError{Code: "22P02"}) {
		t.Fatal("22P02（invalid input syntax for type uuid）必须判为畸形 id")
	}
	// 真实形态的报错里 Code 与 Message 是分开的字段，这里用逐字真实的文本再钉一遍：
	real := &pgconn.PgError{
		Code:    "22P02",
		Message: `invalid input syntax for type uuid: "not-a-uuid"`,
	}
	if !IsMalformedID(fmt.Errorf("query media: %w", real)) {
		t.Fatal("被 fmt.Errorf 包裹的 22P02 也必须判为畸形 id（errors.As 必须能穿透包装）")
	}

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"普通错误", errors.New("db down")},
		{"其它 SQLSTATE（唯一约束冲突）", &pgconn.PgError{Code: "23505"}},
		{"其它 SQLSTATE（连接异常）", &pgconn.PgError{Code: "08006"}},
	} {
		if IsMalformedID(tc.err) {
			t.Fatalf("%s: 不该判为畸形 id —— 否则真实故障会被伪装成「不存在」", tc.name)
		}
	}
}
