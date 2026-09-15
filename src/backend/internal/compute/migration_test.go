package compute

// 迁移文件的静态断言。
//
// 为什么值得这么测：迁移是**唯一没有编译期保护**的交付物——Go 代码写错有编译器挡着，
// SQL 写错只能等部署那一刻才炸（而且往往是在生产库上炸）。这里用字符串断言把
// 「只新增列、不改既有列、Down 可回滚」这几条约束钉死在 CI 里。
//
// 断言刻意只查关键字存在性而非完整文本（与 store_test.go 的 SQL 契约测试同风格）：
// 完整文本一改就碎，反而会让人习惯性去改测试而不是想清楚改动是否安全。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readMigration 读取 migrations 目录下的迁移文件。
//
// go test 的 CWD 就是包目录（internal/compute），故 "../../migrations" = src/backend/migrations。
func readMigration(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "migrations", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移 %s 失败（路径 %s）: %v", name, path, err)
	}
	return string(b)
}

// TestMigration00019AddsStatusLock 迁移 00019 必须新增 status_locked 列，且不得改动既有结构。
//
// 背景：缺陷是「初始 offline」与「管理员强制 offline」共用同一个值。修法是新增一列状态锁，
// 所以这条迁移的**全部职责就是加一列**。任何 ALTER COLUMN / DROP COLUMN status 都会
// 破坏既有语义（01x 系列已经因为 compute_nodes 缺列踩过一次坑，见 00018 的注释）。
func TestMigration00019AddsStatusLock(t *testing.T) {
	sql := readMigration(t, "00019_compute_node_status_lock.sql")

	// goose 的段落标记必须齐备，否则迁移会静默不执行或执行到一半。
	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("00019 缺少 goose 标记 %q：\n%s", want, sql)
		}
	}
	if strings.Count(sql, "-- +goose StatementBegin") != 2 {
		t.Fatalf("00019 的 Up/Down 各需一个 StatementBegin（共 2 个）：\n%s", sql)
	}

	// 列定义：必须幂等（IF NOT EXISTS）、必须 NOT NULL DEFAULT false。
	// DEFAULT false 的语义是「存量节点 = 未锁定 = 心跳可自治」，若写成 true 会把
	// 所有既有节点误判成被管理员强制下线。
	if !strings.Contains(sql, "ALTER TABLE compute_nodes ADD COLUMN IF NOT EXISTS status_locked BOOLEAN NOT NULL DEFAULT false") {
		t.Fatalf("00019 必须用 ADD COLUMN IF NOT EXISTS 新增 status_locked BOOLEAN NOT NULL DEFAULT false：\n%s", sql)
	}
	if !strings.Contains(sql, "COMMENT ON COLUMN compute_nodes.status_locked") {
		t.Fatalf("00019 必须给 status_locked 写 COMMENT（说明与心跳的交互契约）：\n%s", sql)
	}

	// Down 必须可回滚且只删自己新增的列。
	if !strings.Contains(sql, "ALTER TABLE compute_nodes DROP COLUMN IF EXISTS status_locked") {
		t.Fatalf("00019 的 Down 必须 DROP COLUMN IF EXISTS status_locked：\n%s", sql)
	}

	// 防越界：不得改既有列、不得删既有列。
	if strings.Contains(sql, "ALTER TABLE compute_nodes ALTER COLUMN") {
		t.Fatalf("00019 不得修改既有列（本次只新增列）：\n%s", sql)
	}
	if strings.Contains(sql, "DROP COLUMN IF EXISTS status;") || strings.Contains(sql, "DROP COLUMN IF EXISTS status ") {
		t.Fatalf("00019 不得删除 status 列：\n%s", sql)
	}
}

// TestMigration00018Unchanged 00018 不得被本次改动污染。
//
// 00018 已经部署到测试服，改动一个已应用的迁移不会重跑（goose 按版本号跳过），
// 只会让「文件内容」与「线上实际结构」产生分歧 —— 那是最难查的一类问题。
func TestMigration00018Unchanged(t *testing.T) {
	sql := readMigration(t, "00018_compute_node_agent.sql")
	if strings.Contains(sql, "status_locked") {
		t.Fatal("00018 不应包含 status_locked（状态锁属 00019，00018 已部署不可改）")
	}
}
