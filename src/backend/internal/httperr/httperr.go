// Package httperr 是 HTTP 错误响应形状与「内部错误不回显」的单一真源。
//
// 存在的理由不是"整洁"，而是**避免同一逻辑复制多份后漂移**：本仓的
// errResp / fail / errJSON 曾各自在 8 个包里各写一份 c.JSON(gin.H{"error": ...})，
// 而"把 err.Error() 回给客户端"这个缺陷正是沿着这些复制品各自蔓延的
// （本项目已因"同一逻辑复制多份后漂移"出过三次严重事故）。
//
// 两条边界（改动前务必读懂）：
//
//  1. Envelope 只负责**形状**，不判断消息内容。
//  2. Fail 只用于**来自本端之外**的错误（DB/驱动/文件系统/第三方库）。
//     它把完整错误写进服务端日志（排障必需），给客户端的 message 永远是你传进来的
//     固定文案 —— 绝不要把 err.Error() 当 msg 传进来，那会把 SQLSTATE、表名、
//     列名、类型名、文件路径等实现细节漏给调用方。
//
// 本端自己构造、面向用户的校验文案（例如 ErrInvalidGranularity.Error()）不属于这一
// 类，直接用 Envelope 原样返回即可，不要套 Fail。
package httperr

import (
	"log"

	"github.com/gin-gonic/gin"
)

// Envelope 写统一错误封套 {"error":{"code","message"}}。
//
// msg 由调用方负责：面向用户的文案可以原样传；**绝不能**把回显内部实现的原文传进来。
func Envelope(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}

// Fail 处理来自本端之外的错误：完整错误只进服务端日志，客户端只拿固定文案 msg。
//
// 与 Envelope 的分工：msg 必须是**与 err 无关的固定字符串**（如"查询失败"），
// 由调用方选定；err 只用于日志。若某处确实需要把本端构造的校验文案回给用户，
// 用 Envelope 而不是 Fail。
func Fail(c *gin.Context, status int, code, msg string, err error) {
	if err != nil {
		log.Printf("[httperr] %s %s -> %d %s: %v",
			c.Request.Method, c.Request.URL.Path, status, code, err)
	}
	Envelope(c, status, code, msg)
}
