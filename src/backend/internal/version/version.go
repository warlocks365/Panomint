// Package version 构建版本三元组。
//
// 三个变量由 release 构建脚本（release/build.sh）经 -ldflags 注入：
//
//	-X panoalbum/internal/version.Version=$(cat VERSION)
//	-X panoalbum/internal/version.Commit=$(git rev-parse --short HEAD)
//	-X panoalbum/internal/version.BuildDate=<UTC RFC3339>
//
// 开发构建（go run / go test /  IDE 内运行）不注入，回落到这里的零值 ——
// 因此「Version == "dev"」是开发产物的明确标识，绝不与发布产物混淆。
// 版本号本身的递增规则见 文档/版本管理规范.md（权威真源）。
package version

// Version 语义化版本号，不带 v 前缀（"1.0.0"）。
var Version = "dev"

// Commit 构建时仓库提交的短 sha（注入失败为空串）。
var Commit = ""

// BuildDate 构建 UTC 时间，RFC3339（注入失败为空串）。
var BuildDate = ""

// String 返回对外显示形态：发布产物 "v1.0.0"，开发产物 "dev"。
func String() string {
	if Version == "dev" || Version == "" {
		return "dev"
	}
	return "v" + Version
}
