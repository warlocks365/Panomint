package media

// Job000100 移动/复制相册目标的回归网（纯逻辑/SQL 文本级，不触库——
// 与 internal/albums/additems_scope_test.go 同一方法论：越权类改动编译、vet、
// 既有测试全绿，只在跨用户/匿名分享时静默发作，必须钉文本与参数流）。

import (
	"strings"
	"testing"

	"panoalbum/internal/mediascope"
)

// ---- albumTargetGuard 决策矩阵 ----

func TestAlbumTargetGuardMatrix(t *testing.T) {
	cases := []struct {
		name            string
		notFound        bool
		typ             string
		userID, ownerID string
		role            string
		wantStatus      int
		wantCode        string
	}{
		{"不存在 → 404 同形", true, "", "u1", "u2", "viewer", 404, "NOT_FOUND"},
		{"智能相册 → 400 SMART_READONLY", false, "smart", "u1", "u1", "viewer", 400, "SMART_READONLY"},
		{"他人相册 viewer → 403", false, "manual", "u1", "u2", "viewer", 403, "FORBIDDEN"},
		{"本人相册 → 通过", false, "manual", "u1", "u1", "viewer", 0, ""},
		{"他人相册 owner 角色 → 通过", false, "manual", "u1", "u2", "owner", 0, ""},
		{"他人相册 admin 角色 → 通过", false, "manual", "u1", "u2", "admin", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, code, _ := albumTargetGuard(tc.notFound, tc.typ, tc.userID, tc.ownerID, tc.role)
			if st != tc.wantStatus || code != tc.wantCode {
				t.Fatalf("guard(%v,%q,%q,%q,%q) = (%d,%q)，期望 (%d,%q)",
					tc.notFound, tc.typ, tc.userID, tc.ownerID, tc.role, st, code, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

// ---- batchAddToAlbumQueries 可见性收敛（与 albums.addItemsQueries 同口径）----

func TestBatchAddToAlbumQueriesScopedToAlbumOwner(t *testing.T) {
	const albumID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	ids := []string{"11111111-2222-3333-4444-555555555555"}

	// 前提自证：VisibleCondFor 恰 1 个 userID 参数（编号约定地基）。
	if _, a := mediascope.VisibleCondFor(2, "u", "m"); len(a) != 1 {
		t.Fatalf("前提不成立：VisibleCondFor 不再返回恰 1 个 userID 参数：%v", a)
	}

	for _, owner := range []string{"owner-a", "owner-b"} {
		t.Run(owner, func(t *testing.T) {
			checkSQL, checkArgs, insertSQL, insertArgs := batchAddToAlbumQueries(albumID, owner, ids)

			// 1) 两段语句都逐字节包含**该属主**的可见性谓词（$2 / $3 起编号）。
			wantCheck, _ := mediascope.VisibleCondFor(2, owner, "m")
			if !strings.Contains(checkSQL, wantCheck) {
				t.Fatalf("checkSQL 缺少 mediascope 谓词（$2 起）:\n得到 %s\n期望含 %s", checkSQL, wantCheck)
			}
			wantInsert, _ := mediascope.VisibleCondFor(3, owner, "m")
			if !strings.Contains(insertSQL, wantInsert) {
				t.Fatalf("insertSQL 缺少 mediascope 谓词（$3 起）:\n得到 %s\n期望含 %s", insertSQL, wantInsert)
			}

			// 2) 属主必须作为参数出现，而不是写死进 SQL 文本。
			if len(checkArgs) != 2 || checkArgs[1] != owner {
				t.Fatalf("check 参数错：want [ids owner]，实际 %v", checkArgs)
			}
			if len(insertArgs) != 3 || insertArgs[2] != owner {
				t.Fatalf("insert 参数错：want [albumID ids owner]，实际 %v", insertArgs)
			}
			if strings.Contains(checkSQL, owner) || strings.Contains(insertSQL, owner) {
				t.Fatal("属主必须参数化，不得拼进 SQL 文本")
			}

			// 3) 参数个数与占位符基数自洽（ids 在 check=$1、insert=$2）。
			if !strings.Contains(checkSQL, "$1") || !strings.Contains(insertSQL, "$2") {
				t.Fatal("unnest 占位符错位：check 应含 $1，insert 应含 $2")
			}
		})
	}
}

// ---- 谓词参数化：SQL 文本不随属主变（防止拼串注入式写法），属主只走参数 ----

func TestBatchAddToAlbumQueriesParameterized(t *testing.T) {
	const albumID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	ids := []string{"11111111-2222-3333-4444-555555555555"}
	_, _, insA, argsA := batchAddToAlbumQueries(albumID, "owner-a", ids)
	_, _, insB, argsB := batchAddToAlbumQueries(albumID, "owner-b", ids)
	if insA != insB {
		t.Fatal("SQL 文本随属主变化——属主被拼进了语句文本（应参数化）")
	}
	if argsA[2] == argsB[2] {
		t.Fatalf("属主参数未随调用变化：两边都是 %v", argsA[2])
	}
}
