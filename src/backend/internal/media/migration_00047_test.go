package media

// 迁移 00047 的静态断言（与 internal/audit/migration_test.go 同风格、同理由）。
//
// 为什么迁移需要这种测试：迁移是**唯一没有编译期保护**的交付物 —— Go 写错有编译器挡着，
// SQL 写错只能等部署那一刻在（测试）库上炸。这里把「加了哪些列、搬迁判定条件是什么、
// 不碰哪些既有列」用字符串断言钉在 CI 里。
//
// 断言只查关键字存在性而非完整文本：完整文本一改就碎，
// 反而会让人习惯性去改测试而不是想清楚改动是否安全。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readMigration(t *testing.T, name string) string {
	t.Helper()
	// 迁移在 ../../migrations/（internal/media → internal → backend）
	p := filepath.Join("..", "..", "migrations", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取迁移 %s 失败（路径 %s）：%v", name, p, err)
	}
	return string(b)
}

// TestMigration00047AddsAddressColumn 00047 必须幂等新增 address 列。
func TestMigration00047AddsAddressColumn(t *testing.T) {
	sql := readMigration(t, "00047_media_metadata_edit.sql")

	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin", "-- +goose StatementEnd"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("00047 缺少 goose 标记 %q：\n%s", want, sql)
		}
	}

	// 加列必须幂等 —— goose 迁移失败即拒启动（api/main.go 的 log.Fatal），
	// 不幂等等于重跑一次就再也起不来。
	if !strings.Contains(sql, "ALTER TABLE media ADD COLUMN IF NOT EXISTS address TEXT") {
		t.Fatalf("00047 必须幂等新增 address 列（ADD COLUMN IF NOT EXISTS）：\n%s", sql)
	}
	// address 不得有 NOT NULL / UNIQUE —— 绝大多数媒体没有 GPS 也没有地址。
	for _, bad := range []string{"address TEXT NOT NULL", "address TEXT UNIQUE"} {
		if strings.Contains(sql, bad) {
			t.Fatalf("00047 的 address 列不得带 %q（媒体可无地址）：\n%s", bad, sql)
		}
	}
}

// TestMigration00047RelocatesPlaceSemantics 00047 必须把 place 的详细地址内容搬走。
//
// 这是本迁移的核心语义：place 原本装的是 formatted_address（详细地址），
// 与新的「短地名」语义不符。判定搬迁是否合法的关键是**不能误搬已正确的短地名**。
func TestMigration00047RelocatesPlaceSemantics(t *testing.T) {
	sql := readMigration(t, "00047_media_metadata_edit.sql")

	// place 放宽：短地名用不到 128，放宽仅为兼容历史脏数据与手工长地名。
	if !strings.Contains(sql, "ALTER COLUMN place TYPE VARCHAR(256)") {
		t.Fatalf("00047 必须把 place 放宽到 VARCHAR(256)：\n%s", sql)
	}

	// 搬迁 UPDATE 必须存在，且**必须带 address IS NULL 守卫** ——
	// 否则重复执行会覆盖用户已手工填写的 address（违反「原值不丢」的设计承诺）。
	if !strings.Contains(sql, "SET address = place") {
		t.Fatalf("00047 必须包含把 place 内容复制到 address 的搬迁语句：\n%s", sql)
	}
	if !strings.Contains(sql, "address IS NULL") {
		t.Fatalf("00047 的搬迁必须带 address IS NULL 守卫（不得覆盖用户已填的地址）：\n%s", sql)
	}

	// 搬迁后必须清空已搬迁的 place，否则同一份内容两列重复。
	// 判定用「place = address」而非「address IS NOT NULL」——
	// 后者会连带清掉用户手写的、恰好与 address 相同的 place。
	if !strings.Contains(sql, "btrim(place) = btrim(address)") {
		t.Fatalf("00047 清空 place 必须用 btrim(place)=btrim(address) 精确判定：\n%s", sql)
	}

	// 误搬防护：短地名（如「故宫博物院」5 字）不应命中搬迁条件。
	// 因此条件里必须有长度门槛（length > 24），否则短地名会被误搬。
	if !strings.Contains(sql, "length(place) > 24") {
		t.Fatalf("00047 搬迁条件必须含长度门槛 length(place) > 24（防误搬短地名）：\n%s", sql)
	}
}

// TestMigration00047KeepsGPSAndTakenAt 00047 不得动 gps 与 taken_at。
//
// 这两列已有正确语义与类型（gps 是 geometry(Point,4326)，taken_at 是 TIMESTAMPTZ），
// 本需求只是「让它们可编辑」，不需要任何结构变更 —— 迁移碰它们就是范围蔓延。
func TestMigration00047KeepsGPSAndTakenAt(t *testing.T) {
	sql := readMigration(t, "00047_media_metadata_edit.sql")

	for _, bad := range []string{
		"ALTER COLUMN gps",
		"DROP COLUMN gps",
		"ALTER COLUMN taken_at",
		"DROP COLUMN taken_at",
	} {
		if strings.Contains(sql, bad) {
			t.Fatalf("00047 不得改 gps/taken_at 结构（出现 %q）：\n%s", bad, sql)
		}
	}
}

// TestMigration00047DownIsReversible 00047 的 Down 必须只删 address 列。
//
// Down 不做数据回填（place 从 address 回填）是有意的：用户可能已编辑过 place，
// 回填会用旧 address 覆盖新值。这是「撤销结构」而非「还原数据」。
func TestMigration00047DownIsReversible(t *testing.T) {
	sql := readMigration(t, "00047_media_metadata_edit.sql")

	i := strings.Index(sql, "-- +goose Down")
	if i < 0 {
		t.Fatal("00047 缺少 -- +goose Down 段")
	}
	down := sql[i:]

	if !strings.Contains(down, "DROP COLUMN IF EXISTS address") {
		t.Fatalf("00047 的 Down 必须删 address 列：\n%s", down)
	}
	// Down 绝不能删 place/gps/taken_at —— 那是数据损失，不是回滚。
	for _, bad := range []string{"DROP COLUMN IF EXISTS place", "DROP COLUMN IF EXISTS gps", "DROP COLUMN IF EXISTS taken_at"} {
		if strings.Contains(down, bad) {
			t.Fatalf("00047 的 Down 不得删 %q：\n%s", bad, down)
		}
	}
}
