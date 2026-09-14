package media

import (
	"strings"
	"testing"
)

// Phase 4 标签接口修复的纯逻辑单测（不触库、不依赖 Store 实例）。
// 覆盖：A3 批量确认请求体解析、A8 GET /tags 查询参数校验、A6 错误信息不泄露数据库细节。

// ---- A3：POST /media/:id/tags/confirm 请求体解析 ----

func TestParseConfirmTagIDs(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantNil bool // true 表示「未提供 tag_ids」→ 调用方按"全部确认"处理
		wantLen int
		wantErr bool
	}{
		{name: "空请求体_全部确认", body: "", wantNil: true},
		{name: "纯空白_全部确认", body: "  \n\t", wantNil: true},
		{name: "空对象_全部确认", body: "{}", wantNil: true},
		{name: "null_全部确认", body: "null", wantNil: true},
		{name: "显式空数组_确认0条", body: `{"tag_ids":[]}`, wantLen: 0},
		{name: "正常数组", body: `{"tag_ids":["t1","t2"]}`, wantLen: 2},
		{name: "type_error_字符串", body: `{"tag_ids":"t1,t2"}`, wantErr: true},
		{name: "type_error_数字", body: `{"tag_ids":7}`, wantErr: true},
		{name: "非法JSON", body: `{oops`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := parseConfirmTagIDs([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("body=%q 应返回解析错误，实际 ids=%v err=nil", tc.body, ids)
				}
				return
			}
			if err != nil {
				t.Fatalf("body=%q 不应报错: %v", tc.body, err)
			}
			if tc.wantNil {
				if ids != nil {
					t.Fatalf("body=%q 应为 nil（走全部确认），实际 %v", tc.body, *ids)
				}
				return
			}
			if ids == nil {
				t.Fatalf("body=%q 不应为 nil（否则会退化成全部确认）", tc.body)
			}
			if len(*ids) != tc.wantLen {
				t.Fatalf("body=%q 长度应为 %d，实际 %d", tc.body, tc.wantLen, len(*ids))
			}
		})
	}
}

// 回归重点：tag_ids 传字符串时必须 400，绝不能退化成「确认该媒体全部 AI 标签」。
func TestParseConfirmTagIDsTypeErrorDoesNotDegradeToAll(t *testing.T) {
	ids, err := parseConfirmTagIDs([]byte(`{"tag_ids":"t1,t2"}`))
	if err == nil {
		t.Fatal("tag_ids 类型错误必须返回错误（原实现静默退化为全部确认）")
	}
	if ids != nil {
		t.Fatal("解析失败时不得返回可被当作'未提供'的 nil 之外的值")
	}
}

// ---- A8：GET /tags 查询参数校验 ----

func TestParseTagListQuery(t *testing.T) {
	if _, _, n, err := parseTagListQuery("", "", ""); err != nil || n != 0 {
		t.Fatalf("留空 limit 应为 0（由 Store 取默认值），实际 n=%d err=%v", n, err)
	}
	if _, kind, _, err := parseTagListQuery("", "user", ""); err != nil || kind != "user" {
		t.Fatalf("kind=user 应放行，实际 kind=%q err=%v", kind, err)
	}
	if _, kind, _, err := parseTagListQuery("", " ai ", ""); err != nil || kind != "ai" {
		t.Fatalf("kind 应去首尾空白，实际 kind=%q err=%v", kind, err)
	}
	if _, _, n, err := parseTagListQuery("", "", "10"); err != nil || n != 10 {
		t.Fatalf("limit=10 应解析为 10，实际 n=%d err=%v", n, err)
	}
	if _, _, _, err := parseTagListQuery("", "foo", ""); err == nil {
		t.Fatal("未知 kind 应报错")
	}
	if _, _, _, err := parseTagListQuery("", "", "abc"); err == nil {
		t.Fatal("非数字 limit 应报错")
	}
	if _, _, _, err := parseTagListQuery("", "", "-1"); err == nil {
		t.Fatal("负数 limit 应报错")
	}
	if q, _, _, err := parseTagListQuery(" 西湖 ", "", ""); err != nil || q != " 西湖 " {
		t.Fatalf("q 应原样透传（清洗在 Store 侧做），实际 %q err=%v", q, err)
	}
}

// 上限常量：库内 147 个标签曾被硬编码 LIMIT 100 截断，缺省值必须显著大于它。
func TestTagListLimitConstants(t *testing.T) {
	if defaultTagListLimit < 147 {
		t.Fatalf("缺省 limit 必须大于库内现有标签数 147，实际 %d", defaultTagListLimit)
	}
	if defaultTagListLimit != 500 || maxTagListLimit != 500 {
		t.Fatalf("缺省/上限应为 500，实际 %d/%d", defaultTagListLimit, maxTagListLimit)
	}
}

// ---- A6：错误信息不得泄露 PG 外键原文 ----

func TestMergeTargetNotFoundMessage(t *testing.T) {
	msg := strings.ToLower(ErrMergeTargetNotFound.Error())
	for _, leak := range []string{"sqlstate", "23503", "foreign key", "constraint", "violates"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("错误信息泄露数据库细节 %q: %s", leak, ErrMergeTargetNotFound.Error())
		}
	}
}
