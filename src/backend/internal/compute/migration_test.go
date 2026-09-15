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
	"regexp"
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

// TestMigration00021DropsPlaintextToken 迁移 00021 必须删掉明文令牌列，且 Down 不得引入约束。
//
// 这是安全欠债的收口测试：00018 只清空、不删列，明文列本身仍是「备份/审计副本泄漏可用凭据」
// 的载体。Down 只恢复结构（可空、无值）——若重建时带上 NOT NULL / DEFAULT / 其它约束，
// 会让"回滚后新的 INSERT 因缺列值而失败"，比不回滚更糟。
func TestMigration00021DropsPlaintextToken(t *testing.T) {
	sql := readMigration(t, "00021_drop_compute_node_agent_token.sql")

	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("00021 缺少 goose 标记 %q：\n%s", want, sql)
		}
	}
	if strings.Count(sql, "-- +goose StatementBegin") != 2 {
		t.Fatalf("00021 的 Up/Down 各需一个 StatementBegin（共 2 个）：\n%s", sql)
	}

	// Up：必须删列，且幂等。
	if !strings.Contains(sql, "ALTER TABLE compute_nodes DROP COLUMN IF EXISTS agent_token;") {
		t.Fatalf("00021 的 Up 必须 DROP COLUMN IF EXISTS agent_token：\n%s", sql)
	}
	// Up 不得顺手删别的列（本次只处理明文令牌列）。
	for _, bad := range []string{
		"DROP COLUMN IF EXISTS agent_token_hash",
		"DROP COLUMN IF EXISTS agent_token_expires_at",
		"DROP COLUMN IF EXISTS updated_at",
		"DROP COLUMN IF EXISTS status_locked",
	} {
		if strings.Contains(sql, bad) {
			t.Fatalf("00021 的 Up 越界删列 %q（本次只删明文 agent_token）：\n%s", bad, sql)
		}
	}

	// Down：只重建结构，必须可空、不加约束，且绝不回填数据。
	if !strings.Contains(sql, "ALTER TABLE compute_nodes ADD COLUMN IF NOT EXISTS agent_token VARCHAR(255)") {
		t.Fatalf("00021 的 Down 必须重建可空 agent_token VARCHAR(255)：\n%s", sql)
	}
	// 约束检查只看**可执行语句**：注释里为了解释"不得带约束"必然会写到这些词，不能误判。
	if code := stripSQLComments(sql); strings.Contains(code, "NOT NULL") ||
		strings.Contains(code, "DEFAULT") ||
		strings.Contains(code, "UNIQUE") ||
		strings.Contains(code, "CHECK") ||
		strings.Contains(code, "UPDATE compute_nodes SET agent_token") {
		t.Fatalf("00021 的语句不得带约束或回填（重建列必须可空无约束，且不允许回填数据）：\n%s", code)
	}
}

// stripSQLComments 删掉 `--` 行注释，只留可执行语句文本，供上面的约束断言使用。
func stripSQLComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// TestFullResetSchemaMatchesComputeNodesMigrations 手工全量重置参考
// （docker/db/init/01-schema.sql）的 compute_nodes 块必须与迁移保持一致。
//
// 为什么值得单独一条：迁移只作用于**已存在的库**，而 01-schema.sql 是"从零重置"的参考。
// 两者漂移时症状极隐蔽 —— 重置出来的库缺列，于是 compute_nodes 上那个
// set_updated_at_compute_nodes 触发器（函数体写 NEW.updated_at）会让**任何 UPDATE 都失败**，
// 而这张表恰好只靠 UPDATE 工作（心跳续期 / 上下线 / 令牌轮换）。这个坑已经踩过一次
// （迁移 00018 修的就是它），所以在这里钉死，避免"改了迁移、忘了参考脚本"再次发生。
//
// 找不到该文件时 Skip：本测试横跨到仓库根的 docker/ 目录，在只含 src/backend 的环境里
// 不存在，Skip 比误报成失败更合适。
func TestFullResetSchemaMatchesComputeNodesMigrations(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "docker", "db", "init", "01-schema.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("未找到 %s（非全仓环境？）: %v", path, err)
	}
	whole := string(raw)
	block := computeNodesBlock(t, whole)

	// ① 迁移为 compute_nodes 追加的列，必须都出现在全量重置脚本里。
	for _, col := range []string{"agent_token_hash", "agent_token_expires_at", "updated_at", "status_locked"} {
		if !hasColumnDef(block, col) {
			t.Fatalf("01-schema.sql 的 compute_nodes 缺列 %q（迁移已加，全量重置会造出与线上不一致的库）：\n%s",
				col, block)
		}
	}

	// ② 明文令牌列已由 00021 删除，重置脚本不得再造出来。
	if hasColumnDef(block, "agent_token") {
		t.Fatalf("01-schema.sql 的 compute_nodes 仍有明文列 agent_token（迁移 00021 已删除）：\n%s", block)
	}

	// ③ updated_at 与该触发器必须成对出现 —— 只有列没触发器只是不自动更新；
	// 只有触发器没列则**整张表的 UPDATE 全废**（00018 的原始缺陷）。
	if strings.Contains(whole, "set_updated_at_compute_nodes") && !hasColumnDef(block, "updated_at") {
		t.Fatalf("本文件建了 set_updated_at_compute_nodes 触发器，却没有 updated_at 列 " +
			"→ 任何 UPDATE compute_nodes 都会报 record \"new\" has no field \"updated_at\"")
	}
}

// computeNodesBlock 从全量重置 SQL 里截出 CREATE TABLE compute_nodes (...) 这一段。
func computeNodesBlock(t *testing.T, sql string) string {
	t.Helper()
	start := strings.Index(sql, "CREATE TABLE compute_nodes (")
	if start < 0 {
		t.Fatal("01-schema.sql 未找到 CREATE TABLE compute_nodes，解析假设已失效")
	}
	rest := sql[start:]
	end := strings.Index(rest, "\n);")
	if end < 0 {
		t.Fatal("compute_nodes 建表语句未正常结束（找不到 \\n);）")
	}
	return rest[:end]
}

// hasColumnDef 判断建表块里是否**真正定义了**某列（而不是只在注释里提到）。
//
// 判定：行首缩进后就是列名、紧跟空白再跟类型首字母；整行被 `--` 注释掉的不算。
// 这样 `agent_token_hash` 不会因为前缀而误判成 `agent_token`。
func hasColumnDef(block, col string) bool {
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(col) + `\s+[A-Za-z]`)
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// TestHasColumnDefHelper 自检辅助函数本身。
//
// 没有这条，上面那条一致性断言可能**静默变成永真**（例如正则写错导致任何列都"存在"），
// 于是一整类"参考脚本与迁移漂移"的缺陷会重新变成无人看守。store_test.go 的
// TestColumnUsedHelper 出于同样的理由存在。
func TestHasColumnDefHelper(t *testing.T) {
	block := `
    id             UUID PRIMARY KEY,
    agent_token_hash       VARCHAR(64),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- agent_token    VARCHAR(256),  注释里的列不算定义
    last_heartbeat TIMESTAMPTZ
`
	for _, col := range []string{"id", "agent_token_hash", "updated_at", "last_heartbeat"} {
		if !hasColumnDef(block, col) {
			t.Fatalf("应判定 %q 已定义", col)
		}
	}
	// 前缀碰撞：agent_token_hash 存在，不代表 agent_token 存在。
	if hasColumnDef(block, "agent_token") {
		t.Fatal("agent_token 只出现在注释与 agent_token_hash 里，不应被判为已定义")
	}
	// 完全没出现过的列。
	if hasColumnDef(block, "status_locked") {
		t.Fatal("block 里没有 status_locked，不应判定为已定义")
	}
	// 真·缺列（用来证明这条断言不是永真）。
	if !hasColumnDef("    updated_at     TIMESTAMPTZ\n", "updated_at") {
		t.Fatal("基准正例未通过，辅助函数失效")
	}
}
