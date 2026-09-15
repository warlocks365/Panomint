package audit

// 迁移 00020 的静态断言。
//
// 与 internal/compute/migration_test.go 同风格、同理由：迁移是**唯一没有编译期保护**
// 的交付物 —— Go 代码写错有编译器挡着，SQL 写错只能等部署那一刻在（测试）库上炸。
// 这里用字符串断言把「只新增列与索引、不改既有列、Down 可回滚」钉死在 CI 里。
//
// 断言刻意只查关键字存在性而非完整文本：完整文本一改就碎，
// 反而会让人习惯性去改测试而不是想清楚改动是否安全。

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestMigration00020AddsAuditColumns 00020 必须补齐审计查询所需的三列 + 三条索引。
func TestMigration00020AddsAuditColumns(t *testing.T) {
	sql := readAuditMigration(t, "00020_audit_log_target_and_indexes.sql")

	// goose 段落标记必须齐备，否则迁移会静默不执行或执行到一半。
	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin", "-- +goose StatementEnd"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("00020 缺少 goose 标记 %q：\n%s", want, sql)
		}
	}
	if got := strings.Count(sql, "-- +goose StatementBegin"); got != 2 {
		t.Fatalf("Up/Down 各需一个 StatementBegin（共 2 个），实际 %d：\n%s", got, sql)
	}

	// 三列：必须幂等（IF NOT EXISTS）、必须允许 NULL。
	// 可空是硬要求 —— 审计写入是"尽力而为"的，绝不能再引入一个能让 INSERT 失败的新约束。
	for _, want := range []string{
		"ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_type VARCHAR(64)",
		"ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_id VARCHAR(128)",
		"ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS user_agent TEXT",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("00020 缺少幂等加列语句 %q：\n%s", want, sql)
		}
	}
	// 新增列不得带 NOT NULL（会阻断尽力而为的审计写入）。
	// 只检查以 ALTER TABLE audit_log ADD COLUMN 开头的语句行 —— 注释里提到 NOT NULL 不算。
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ALTER TABLE audit_log ADD COLUMN") && strings.Contains(trimmed, "NOT NULL") {
			t.Fatalf("00020 的新增列不得带 NOT NULL（会阻断尽力而为的审计写入）：%s", trimmed)
		}
	}

	// 三条查询索引：管理端固定 ORDER BY at DESC，三类过滤各一。
	for _, idx := range []string{
		"CREATE INDEX IF NOT EXISTS idx_audit_action_at ON audit_log(action, at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_audit_actor_at ON audit_log(user_id, at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_log(target_type, target_id, at DESC)",
	} {
		if !strings.Contains(sql, idx) {
			t.Fatalf("00020 缺少查询索引 %q：\n%s", idx, sql)
		}
	}

	// 列宽必须与 Go 侧常量一致，否则长值会在 INSERT 时报错（而错误会被尽力而为地吞掉，
	// 表现为"审计静默缺失"—— 最难查的一类问题）。
	if !strings.Contains(sql, "target_type VARCHAR("+strconv.Itoa(maxTargetTypeLen)+")") {
		t.Fatalf("target_type 列宽应与 maxTargetTypeLen=%d 一致：\n%s", maxTargetTypeLen, sql)
	}
	if !strings.Contains(sql, "target_id VARCHAR("+strconv.Itoa(maxTargetIDLen)+")") {
		t.Fatalf("target_id 列宽应与 maxTargetIDLen=%d 一致：\n%s", maxTargetIDLen, sql)
	}
}

// TestMigration00020NeverTouchesExistingColumns 审计表是只增不改的账本。
func TestMigration00020NeverTouchesExistingColumns(t *testing.T) {
	sql := readAuditMigration(t, "00020_audit_log_target_and_indexes.sql")

	if strings.Contains(sql, "ALTER TABLE audit_log ALTER COLUMN") {
		t.Fatalf("00020 不得修改既有列（本次只新增列与索引）：\n%s", sql)
	}
	if strings.Contains(sql, "ALTER TABLE audit_log RENAME") {
		t.Fatalf("00020 不得重命名既有列（user_id 保留原名，语义由 Go 侧 actor_user_id 表达）：\n%s", sql)
	}
	if strings.Contains(sql, "UPDATE audit_log") || strings.Contains(sql, "DELETE FROM audit_log") {
		t.Fatalf("00020 不得改写既有审计行（等于篡改历史）：\n%s", sql)
	}
	// Down 只能删自己新增的东西：既有的 6 列一个都不能动。
	for _, col := range []string{"id", "user_id", "action", "detail", "ip", "at"} {
		if strings.Contains(sql, "DROP COLUMN IF EXISTS "+col+"\n") ||
			strings.Contains(sql, "DROP COLUMN IF EXISTS "+col+";") {
			t.Fatalf("00020 的 Down 不得删除既有列 %q：\n%s", col, sql)
		}
	}
	// Down 必须真的可回滚（删掉本次新增的三列与三条索引）。
	for _, want := range []string{
		"DROP INDEX IF EXISTS idx_audit_target",
		"DROP INDEX IF EXISTS idx_audit_actor_at",
		"DROP INDEX IF EXISTS idx_audit_action_at",
		"DROP COLUMN IF EXISTS user_agent",
		"DROP COLUMN IF EXISTS target_id",
		"DROP COLUMN IF EXISTS target_type",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("00020 的 Down 缺少 %q：\n%s", want, sql)
		}
	}
}

// TestDeployedMigrationsUntouched 已部署的迁移不得被本次改动污染。
//
// 00002 建了 audit_log；00018/00019 已应用到测试服（goose 版本表实测到 19）。
// 改动一个已应用的迁移不会重跑（goose 按版本号跳过），只会让「文件内容」与
// 「线上实际结构」产生分歧 —— 那是最难查的一类问题。
//
// 注意：不能拿 user_agent 当判据 —— 00002 的 sessions 表本来就有 user_agent 列。
// 只检查 audit_log 专属的新增物（target_type/target_id 与本次的三条索引名）。
func TestDeployedMigrationsUntouched(t *testing.T) {
	ddl := readAuditMigration(t, "00002_ddl_part.sql")
	if !strings.Contains(ddl, "CREATE TABLE audit_log") {
		t.Fatal("00002 应仍是 audit_log 的建表处（结构被意外改动了？）")
	}
	for _, leak := range []string{"target_type", "target_id", "idx_audit_action_at", "idx_audit_actor_at", "idx_audit_target"} {
		if strings.Contains(ddl, leak) {
			t.Fatalf("00002 不得包含本次新增的 %q（它已部署，只许 00020 引入）", leak)
		}
	}
}

// readAuditMigration 读取 migrations 目录下的文件。
// go test 的 CWD 是包目录（internal/audit），故 "../../migrations" = src/backend/migrations。
func readAuditMigration(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "migrations", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移 %s 失败（路径 %s）: %v", name, path, err)
	}
	return string(b)
}
