package media

// P2-01 读访问口径扩大的回归网。
//
// 修复前：GET /media?space=shared 成员可见全部条目，但 GET /media/:id / :id/thumb /
// :id/download 全部被 canAccess（仅本人/owner/admin）拒掉 —— 共享空间功能对成员
// 实际不可用。修复后读判定扩为「属主 ∪（space='shared' 且调用者是成员）∪ owner/admin」，
// 谓词收敛进 mediascope.ReadCond（单一真源）。
//
// 这是**扩大可见面**的改动，所以负对照必须钉住：
// **非成员仍 403**（Detail/Download），Thumb 仍是同形 404（存在性预言机策略不变）。
//
// 分层：
//   - 本文件钉「判定结果的响应映射」（无库单测，fake lookup 模拟 readAccessOf 的三种结果）；
//   - 「shared 成员/非成员在 SQL 层如何区分」由 mediascope 的 ReadCond 测试钉住
//     （谓词必须含 shared_space_members 与 shared_space 的双 EXISTS，且绑定调用者）。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// fakeReadAccess 充当 *Store.readAccessOf（签名逐字相同）。
func fakeReadAccess(ownerID string, allowed bool, err error) readAccessLookup {
	return func(context.Context, string, string, string) (string, bool, bool, error) {
		return ownerID, false, allowed, err
	}
}

// ---- 判定层：三种结果的口径 ----

func TestReadAccessCheckSharedMemberAllowed(t *testing.T) {
	// shared 空间成员：readAccessOf 在 SQL 层判 allowed=true（见 mediascope.ReadCond），
	// 本层必须放行并返回 ownerID —— 列表可见的媒体，详情也必须可读。
	ownerID, err := readAccessCheck(context.Background(), fakeReadAccess("u1", true, nil), "u2", "member", "m1")
	if err != nil || ownerID != "u1" {
		t.Fatalf("shared 成员应放行并返回属主，实际 owner=%q err=%v", ownerID, err)
	}
}

func TestReadAccessCheckOwnerAndPrivilegedAllowed(t *testing.T) {
	for _, tc := range []struct{ user, role string }{
		{"u1", "member"}, // 属主本人
		{"u9", "owner"},  // 全局 owner（ReadCond 特权短路 → allowed=true）
		{"u9", "admin"},  // 管理员（同上）
	} {
		if _, err := readAccessCheck(context.Background(), fakeReadAccess("u1", true, nil), tc.user, tc.role, "m1"); err != nil {
			t.Fatalf("user=%s role=%s 应放行，实际 %v", tc.user, tc.role, err)
		}
	}
}

func TestReadAccessCheckNonMemberForbidden(t *testing.T) {
	// ⭐ 负对照：非属主、非 shared 成员、非特权 —— 必须 ErrForbidden（→ 403）。
	// 这条是「扩大可见面」的另一半：放行面扩了，拒绝面不能跟着扩。
	_, err := readAccessCheck(context.Background(), fakeReadAccess("u1", false, nil), "u2", "member", "m1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("非成员必须判为 ErrForbidden，实际 %v", err)
	}
}

func TestReadAccessCheckMissingMediaIsNotFound(t *testing.T) {
	_, err := readAccessCheck(context.Background(), fakeReadAccess("", false, ErrNotFound), "u1", "member", "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的媒体应为 ErrNotFound，实际 %v", err)
	}
}

func TestReadAccessCheckPropagatesDBError(t *testing.T) {
	dbErr := errors.New("db down")
	_, err := readAccessCheck(context.Background(), fakeReadAccess("", false, dbErr), "u1", "member", "m1")
	if !errors.Is(err, dbErr) {
		t.Fatalf("DB 错误应原样透出（由响应层映射 500），实际 %v", err)
	}
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
		t.Fatal("DB 错误不得被折叠成 403/404 —— 否则故障被伪装成「无权」或「不存在」")
	}
}

// ---- 响应层：ErrForbidden → 403（Detail/Download 与列表的 403 口径一致） ----
// gin 上下文复用 thumb_access_test.go 的 thumbCtx()。

func TestReadAccessDenyIs403WithReason(t *testing.T) {
	c, rec := thumbCtx()
	writeReadAccessError(c, ErrForbidden) // 非成员
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非成员读详情必须 403（负对照），实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "FORBIDDEN") {
		t.Fatalf("403 响应应带 FORBIDDEN 错误码: %s", rec.Body.String())
	}
}

func TestReadAccessMissingIs404(t *testing.T) {
	c, rec := thumbCtx()
	writeReadAccessError(c, ErrNotFound)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在应 404，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReadAccessDBErrorIs500(t *testing.T) {
	dbErr := errors.New("db down")
	c, rec := thumbCtx()
	writeReadAccessError(c, dbErr)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("DB 错误应 500，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), dbErr.Error()) {
		t.Fatalf("500 响应回显了数据库原文: %s", rec.Body.String())
	}
}
