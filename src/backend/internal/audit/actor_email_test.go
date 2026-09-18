package audit

// actor_email 快照（迁移 00026）与审计写入语句的静态断言。
//
// 为什么这些必须用「文本断言」而不是端到端测试：
//   · 迁移是唯一没有编译期保护的交付物 —— SQL 写错只能等部署那刻在库上炸；
//   · 审计 INSERT 的**占位符/实参个数不一致**只在运行期报 `expected N arguments`，
//     编译期完全看不见，而审计写入又是「尽力而为」（错误会被 Recorder 吞掉），
//     所以在生产里它表现为「审计静默缺失」—— 本项目已经吃过这个亏。
// 与 internal/audit/migration_test.go 同风格、同理由。

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var auditPlaceholderRe = regexp.MustCompile(`\$(\d+)`)

// auditPlaceholderCount 返回 SQL 里出现的最大占位符编号与全部编号集合。
// 最大编号 = pgx 期望的实参个数；不连续（如缺 $4）同样会让 pgx 报错，故一并返回集合。
func auditPlaceholderCount(sql string) (max int, nums map[int]bool) {
	nums = map[int]bool{}
	for _, m := range auditPlaceholderRe.FindAllStringSubmatch(sql, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		nums[n] = true
		if n > max {
			max = n
		}
	}
	return max, nums
}

// TestInsertSQLParamCountMatchesArgs 钉死「INSERT 的占位符个数 = 传入实参个数」。
//
// 这是运行期才暴露、且会被尽力而为吞掉的一类错误：占位符多一个 → pgx 报
// `expected N arguments, got M`，审计整条写不进去，业务却毫发无损、毫无察觉。
func TestInsertSQLParamCountMatchesArgs(t *testing.T) {
	max, nums := auditPlaceholderCount(insertAuditSQL)

	// 引入 actor_email 时子查询**复用 $1**，不得新增参数位：编号必须恰好是 1..8。
	const want = 8
	if max != want {
		t.Fatalf("insertAuditSQL 的最大占位符是 $%d，期望 $%d（actor_email 子查询必须复用 $1，不新增参数位）：\n%s",
			max, want, insertAuditSQL)
	}
	for i := 1; i <= want; i++ {
		if !nums[i] {
			t.Fatalf("insertAuditSQL 缺少占位符 $%d（编号必须连续 1..%d，否则 pgx 报错）：\n%s",
				i, want, insertAuditSQL)
		}
	}

	args := insertAuditArgs(Entry{}, nil, time.Now().UTC())
	if len(args) != max {
		t.Fatalf("insertAuditArgs 传了 %d 个实参，但 SQL 有 %d 个占位符（运行期会报 expected %d arguments）：\n%s",
			len(args), max, max, insertAuditSQL)
	}
}

// TestInsertSQLSnapshotsActorEmail 钉死「写入时用子查询快照 actor_email」这一语义。
//
// 断言的是**子查询本身**而不是「SQL 里出现过 actor_email」：后者太弱，
// 比如写成 `actor_email, ... VALUES ($9, ...)` 也会含 actor_email 字样，
// 但那样就丢掉了「同一条语句、复用 $1、查不到即 NULL」这三个关键性质。
func TestInsertSQLSnapshotsActorEmail(t *testing.T) {
	const snapshot = "(SELECT email FROM users WHERE id = $1)"
	if !strings.Contains(insertAuditSQL, snapshot) {
		t.Fatalf("insertAuditSQL 必须含 actor_email 的子查询快照 %q（删掉它就没有归因冗余，\n"+
			"删账号后 user_id 置 NULL 会让归因永久丢失）：\n%s", snapshot, insertAuditSQL)
	}
	// 列清单必须真的写 actor_email，否则子查询的返回值无处落。
	if !strings.Contains(insertAuditSQL, "INSERT INTO audit_log (user_id, actor_email,") {
		t.Fatalf("insertAuditSQL 的列清单必须包含 actor_email 列：\n%s", insertAuditSQL)
	}
}

// TestMigration00026AddsActorEmail 00026 必须幂等加列 + 回填历史，且绝不删任何审计行。
func TestMigration00026AddsActorEmail(t *testing.T) {
	sql := readAuditMigration(t, "00026_audit_log_actor_email.sql")

	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin", "-- +goose StatementEnd"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("00026 缺少 goose 标记 %q：\n%s", want, sql)
		}
	}
	if got := strings.Count(sql, "-- +goose StatementBegin"); got != 2 {
		t.Fatalf("Up/Down 各需一个 StatementBegin（共 2 个），实际 %d：\n%s", got, sql)
	}

	// 幂等加列，且不得带 NOT NULL（会阻断「尽力而为」的审计写入）。
	const addCol = "ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS actor_email VARCHAR(255)"
	if !strings.Contains(sql, addCol) {
		t.Fatalf("00026 缺少幂等加列语句 %q：\n%s", addCol, sql)
	}
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ALTER TABLE audit_log ADD COLUMN") && strings.Contains(trimmed, "NOT NULL") {
			t.Fatalf("00026 的新增列不得带 NOT NULL（会阻断尽力而为的审计写入）：%s", trimmed)
		}
	}

	// 必须有 COMMENT 说清「写入时快照、不依赖 users 行是否还在」的语义。
	if !strings.Contains(sql, "COMMENT ON COLUMN audit_log.actor_email") {
		t.Fatalf("00026 必须给 actor_email 写 COMMENT（语义靠它传达）：\n%s", sql)
	}

	// 回填必须限定「user_id 仍能对上 users.id」且「尚未回填」——否则会把已删账号的行
	// （user_id IS NULL）误判成可回填，或在重跑时反复改写既有快照。
	const backfill = "UPDATE audit_log a SET actor_email = u.email FROM users u WHERE a.user_id = u.id AND a.actor_email IS NULL"
	if !strings.Contains(sql, backfill) {
		t.Fatalf("00026 的回填语句必须形如：\n%s\n实际：\n%s", backfill, sql)
	}

	// 审计只追加：本迁移绝不允许删行，也不许改既有列。
	if strings.Contains(sql, "DELETE FROM audit_log") {
		t.Fatalf("00026 不得删除审计行（只追加的账本，删行=篡改历史）：\n%s", sql)
	}
	if strings.Contains(sql, "ALTER TABLE audit_log ALTER COLUMN") {
		t.Fatalf("00026 不得修改既有列（本次只新增一列）：\n%s", sql)
	}
	for _, col := range []string{"id", "user_id", "action", "detail", "ip", "at", "target_type", "target_id", "user_agent"} {
		if strings.Contains(sql, "DROP COLUMN IF EXISTS "+col+"\n") ||
			strings.Contains(sql, "DROP COLUMN IF EXISTS "+col+";") {
			t.Fatalf("00026 的 Down 不得删除既有列 %q：\n%s", col, sql)
		}
	}

	// Down 必须真的可回滚（只删自己新增的 actor_email）。
	if !strings.Contains(sql, "DROP COLUMN IF EXISTS actor_email") {
		t.Fatalf("00026 的 Down 缺少 DROP COLUMN IF EXISTS actor_email：\n%s", sql)
	}
}
