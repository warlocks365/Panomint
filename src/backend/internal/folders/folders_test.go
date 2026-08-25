package folders

import "testing"

func TestBuildTree(t *testing.T) {
	counts := map[string]int{
		"":           2,
		"2024":       0,
		"2024/08":    5,
		"2024/08/旅行": 3,
		"2025":       1,
	}
	root := BuildTree(counts)
	if root.Count != 2 {
		t.Fatalf("根目录计数应为 2，实际 %d", root.Count)
	}
	if len(root.Children) != 2 {
		t.Fatalf("根应有 2 个子节点，实际 %d", len(root.Children))
	}
	y2024 := root.Children[0]
	if y2024.Name != "2024" || y2024.Path != "2024" || y2024.ID != "2024" {
		t.Fatalf("2024 节点错误: %+v", y2024)
	}
	aug := y2024.Children[0]
	if aug.Name != "08" || aug.Count != 5 {
		t.Fatalf("08 节点错误: %+v", aug)
	}
	trip := aug.Children[0]
	if trip.Name != "旅行" || trip.Count != 3 || trip.ID != "2024/08/旅行" {
		t.Fatalf("旅行节点错误: %+v", trip)
	}
}

func TestBuildTreeEmpty(t *testing.T) {
	root := BuildTree(map[string]int{})
	if len(root.Children) != 0 || root.Count != 0 {
		t.Fatalf("空输入应得空根: %+v", root)
	}
}
