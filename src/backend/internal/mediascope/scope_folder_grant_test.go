package mediascope

import (
	"strings"
	"testing"
)

// Job000069 目录级授权臂的守卫测试：谓词新增 folders EXISTS 臂，
// 但参数个数/特权短路/FailClosed 语义必须不变（签名级兼容）。

func TestFolderGrantArm_Present(t *testing.T) {
	where, args := VisibleCondFor(3, "u1", "m")
	if !strings.Contains(where, "FROM folders gf, jsonb_array_elements(gf.grants) gfge") {
		t.Errorf("VisibleCondFor 缺目录授予臂: %s", where)
	}
	if !strings.Contains(where, "m.folder_path = gf.path OR m.folder_path LIKE gf.path || '/%'") {
		t.Errorf("目录授予臂缺前缀语义: %s", where)
	}
	if !strings.Contains(where, "gfge->>'user_id' = $3") {
		t.Errorf("目录授予臂占位符应为 $3: %s", where)
	}
	if !strings.Contains(where, "(gfge->>'read')::boolean") {
		t.Errorf("目录授予臂缺 read 判定: %s", where)
	}
	if len(args) != 1 || args[0] != "u1" {
		t.Errorf("参数必须仍恰为 1 个 userID: %v", args)
	}
}

func TestFolderGrantArm_ReadCond(t *testing.T) {
	where, args := ReadCond(2, "u1", "member", "m")
	if !strings.Contains(where, "FROM folders gf") {
		t.Errorf("ReadCond 缺目录授予臂: %s", where)
	}
	if !strings.Contains(where, "gfge->>'user_id' = $2") {
		t.Errorf("ReadCond 目录臂占位符应为 $2: %s", where)
	}
	if len(args) != 1 || args[0] != "u1" {
		t.Errorf("ReadCond 参数必须仍恰为 1 个: %v", args)
	}
}

func TestFolderGrantArm_NoAlias(t *testing.T) {
	// 裸列名（alias=""）形态：地图查询用。
	where, _ := VisibleCondFor(1, "u1", "")
	if !strings.Contains(where, "folder_path = gf.path") {
		t.Errorf("裸别名形态缺目录臂: %s", where)
	}
	if strings.Contains(where, "m.folder_path") {
		t.Errorf("裸别名形态不应出现 m. 前缀: %s", where)
	}
}

// 语义不变式：特权短路与 FailClosed 不因加臂而改变。
func TestFolderGrantArm_SemanticsUnchanged(t *testing.T) {
	w, a := ReadCond(1, "u1", "owner", "m")
	if w != "true" || a != nil {
		t.Errorf("owner 特权短路必须保持 true: %q %v", w, a)
	}
	w, a = VisibleCondFor(1, "", "m")
	if w != FailClosed || a != nil {
		t.Errorf("空 userID 必须 FailClosed: %q %v", w, a)
	}
}
