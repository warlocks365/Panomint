package albums

import (
	"strings"
	"testing"
)

// Job000068 新增三维（目录多选/标签/人物）的守卫测试。
// 所有期望值硬编码——谓词必须与 args 自洽，编号错位只在运行期报
// "expected N arguments"，必须在此钉死。

const probeOwner = "11111111-1111-1111-1111-111111111111"

// 目录多选：unnest 展开 + LIKE 通配符转义 + 子目录前缀语义。
func TestBuildCriteriaWhere_FolderPaths(t *testing.T) {
	where, args := buildCriteriaWhere(&Criteria{FolderPaths: []string{"2024/夏", "import"}}, probeOwner)
	if !strings.Contains(where, "unnest($1::text[])") {
		t.Errorf("目录谓词缺 unnest 展开: %s", where)
	}
	// 通配符转义必须存在：否则目录名含 % 时越界匹配。
	if !strings.Contains(where, "replace(replace(p.path, '%', '\\%'), '_', '\\_')") {
		t.Errorf("目录谓词缺通配符转义: %s", where)
	}
	if !strings.Contains(where, "ESCAPE '\\'") {
		t.Errorf("目录谓词缺 ESCAPE 子句: %s", where)
	}
	if !strings.Contains(where, "m.folder_path = p.path") {
		t.Errorf("目录谓词缺精确匹配分支: %s", where)
	}
	if !strings.Contains(where, "|| '/%'") {
		t.Errorf("目录谓词缺子目录前缀分支: %s", where)
	}
	if len(args) != 2 || len(args[0].([]string)) != 2 || args[1] != probeOwner {
		t.Errorf("args 应为 [paths, owner] 共 2 项: %v", args)
	}
}

// 目录多选：单值同样成谓词；与类型条件组合时编号自洽。
func TestBuildCriteriaWhere_FolderPaths_WithType(t *testing.T) {
	where, args := buildCriteriaWhere(&Criteria{Type: "photo", FolderPaths: []string{"a"}}, probeOwner)
	if !strings.Contains(where, "m.type = $1 AND m.is_360 = false") {
		t.Errorf("类型谓词编号错误: %s", where)
	}
	if !strings.Contains(where, "unnest($2::text[])") {
		t.Errorf("目录谓词编号应为 $2: %s", where)
	}
	if len(args) != 3 || args[0] != "photo" || args[1].([]string)[0] != "a" || args[2] != probeOwner {
		t.Errorf("组合 args 错位: %v", args)
	}
}

// 标签多选：media_tags EXISTS + uuid[] 转换；属主恒为最后占位符。
func TestBuildCriteriaWhere_TagIDs(t *testing.T) {
	tag := "22222222-2222-2222-2222-222222222222"
	where, args := buildCriteriaWhere(&Criteria{TagIDs: []string{tag}}, probeOwner)
	if !strings.Contains(where, "EXISTS (SELECT 1 FROM media_tags mt WHERE mt.media_id = m.id AND mt.tag_id = ANY($1::uuid[]))") {
		t.Errorf("标签谓词错误: %s", where)
	}
	if len(args) != 2 || args[0].([]string)[0] != tag || args[1] != probeOwner {
		t.Errorf("args 应为 [tags, owner]: %v", args)
	}
	// 属主必须是最后一个占位符（注释承诺）：SQL 里 $2 只应出现一次且在属主条件。
	if strings.Count(where, "$2") != 1 || !strings.Contains(where, "m.owner_id = $2") {
		t.Errorf("属主占位符约定被破坏: %s", where)
	}
}

// 人物多选：faces EXISTS + person_id 谓词。
func TestBuildCriteriaWhere_PersonIDs(t *testing.T) {
	person := "33333333-3333-3333-3333-333333333333"
	where, args := buildCriteriaWhere(&Criteria{PersonIDs: []string{person}}, probeOwner)
	if !strings.Contains(where, "EXISTS (SELECT 1 FROM faces f WHERE f.media_id = m.id AND f.person_id = ANY($1::uuid[]))") {
		t.Errorf("人物谓词错误: %s", where)
	}
	if len(args) != 2 || args[0].([]string)[0] != person || args[1] != probeOwner {
		t.Errorf("args 应为 [persons, owner]: %v", args)
	}
}

// 非法 UUID 静默忽略：混入垃圾时谓词消失，不污染参数编号。
func TestBuildCriteriaWhere_InvalidUUIDsSilentlyDropped(t *testing.T) {
	where, args := buildCriteriaWhere(&Criteria{
		TagIDs:    []string{"not-a-uuid", "also bad"},
		PersonIDs: []string{""},
	}, probeOwner)
	if strings.Contains(where, "media_tags") || strings.Contains(where, "faces f") {
		t.Errorf("全非法 UUID 不应生成谓词: %s", where)
	}
	if len(args) != 1 || args[0] != probeOwner {
		t.Errorf("非法 UUID 被过滤后只剩属主一个参数: %v", args)
	}
	// 混合：合法 + 非法 → 只保留合法。
	where, args = buildCriteriaWhere(&Criteria{TagIDs: []string{"bad", "22222222-2222-2222-2222-222222222222"}}, probeOwner)
	if !strings.Contains(where, "media_tags") || len(args[0].([]string)) != 1 {
		t.Errorf("混合 UUID 过滤错误: %s args=%v", where, args)
	}
}

// 三维全组合：编号 $1..$4 顺序 = folders/tags/persons/owner。
func TestBuildCriteriaWhere_AllDimensions(t *testing.T) {
	c := &Criteria{
		FolderPaths: []string{"x"},
		TagIDs:      []string{"22222222-2222-2222-2222-222222222222"},
		PersonIDs:   []string{"33333333-3333-3333-3333-333333333333"},
	}
	where, args := buildCriteriaWhere(c, probeOwner)
	for _, frag := range []string{"unnest($1::text[])", "ANY($2::uuid[]))", "ANY($3::uuid[]))", "m.owner_id = $4"} {
		if !strings.Contains(where, frag) {
			t.Errorf("全组合编号错位，缺 %q: %s", frag, where)
		}
	}
	if len(args) != 4 {
		t.Fatalf("args 应恰为 4 项: %v", args)
	}
}

// NormalizeCriteria：去空白/去重/空串剔除/上限。
func TestNormalizeCriteria(t *testing.T) {
	got := NormalizeCriteria(&Criteria{FolderPaths: []string{" a ", "", "a", "b"}})
	if len(got.FolderPaths) != 2 || got.FolderPaths[0] != "a" || got.FolderPaths[1] != "b" {
		t.Errorf("cleanPaths 去重/去空白错误: %v", got.FolderPaths)
	}
	// nil 透传。
	if NormalizeCriteria(nil) != nil {
		t.Error("nil criteria 应透传 nil")
	}
	// 全非法 UUID 写入侧也清空（不持久化脏数据）。
	got = NormalizeCriteria(&Criteria{TagIDs: []string{"junk"}})
	if got.TagIDs != nil {
		t.Errorf("非法 UUID 应在 Normalize 阶段清空: %v", got.TagIDs)
	}
	// 上限：51 个目录只留 50。
	many := make([]string, 0, 51)
	for i := 0; i < 51; i++ {
		many = append(many, strings.Repeat("x", i%7+1)+string(rune('a'+i%26)))
	}
	if n := len(NormalizeCriteria(&Criteria{FolderPaths: many}).FolderPaths); n != 50 {
		t.Errorf("目录上限 50，实际 %d", n)
	}
}

// isUUID 边界。
func TestIsUUID(t *testing.T) {
	for _, ok := range []string{
		"22222222-2222-2222-2222-222222222222",
		"aaaaaaaa-AAAA-aaaa-AAAA-aaaaaaaaaaaa",
	} {
		if !isUUID(ok) {
			t.Errorf("应为合法 UUID: %s", ok)
		}
	}
	for _, bad := range []string{
		"", "22222222-2222-2222-2222-22222222222", // 35 位
		"22222222-2222-2222-2222-2222222222222",  // 37 位
		"222222222222-222-2222-222222222222",     // 连字符位置错
		"g2222222-2222-2222-2222-222222222222",   // 非十六进制
		"22222222-2222-2222-2222-22222222222g",
	} {
		if isUUID(bad) {
			t.Errorf("应为非法 UUID: %q", bad)
		}
	}
}
