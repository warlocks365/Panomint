// migrate 数据库迁移工具（goose，up-only；T1.2）。
// 用法：go run ./cmd/migrate [up|status|reset]  （reset 清空重建，仅开发用）
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"panoalbum/internal/config"
	"panoalbum/migrations"
)

func main() {
	flag.Parse()
	cmd := flag.Arg(0)
	if cmd == "" {
		cmd = "up"
	}

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		log.Fatalf("配置错误: %v", cfgErr)
	}
	db, err := sql.Open("pgx", cfg.PGDSN)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatal(err)
	}
	// 迁移源改为编译期嵌入（panoalbum/migrations）：pano-migrate 二进制从此
	// **无需 migrations/ 目录随行**（裸机/集成包形态的关键），且与 cmd/api 的
	// 启动自迁移读到的永远是同一批文件。相对路径参数 "." = 嵌入 FS 根。
	goose.SetBaseFS(migrations.FS)

	switch cmd {
	case "up":
		err = goose.Up(db, ".")
	case "status":
		err = goose.Status(db, ".")
	case "reset":
		// 仅开发环境：清库重建（drop schema public cascade）
		if cfg.Env == "prod" {
			log.Fatal("reset 禁止在生产环境执行")
		}
		if _, err = db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err == nil {
			fmt.Println("schema 已清空，重新执行 up…")
			err = goose.Up(db, ".")
		}
	default:
		log.Fatalf("未知命令 %q（up|status|reset）", cmd)
	}
	if err != nil {
		log.Fatalf("迁移失败: %v", err)
	}
	fmt.Println("OK:", cmd)
	os.Exit(0)
}
