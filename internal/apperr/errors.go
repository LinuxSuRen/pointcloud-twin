// Package apperr 定义面向用户的稳定错误码。
//
// 约定：用户可见的错误由「稳定错误码 + 普通人能读懂的提示」组成；
// 技术根因（底层错误、堆栈等）只进日志，不透传给用户。
package apperr

import (
	"errors"
	"fmt"
)

// Code 是稳定错误码，形如 E1002。破坏性变更不允许复用旧码。
type Code string

const (
	// E1001 输入文件不存在或不可读。
	E1001 Code = "E1001"
	// E1002 PCD 文件无法解析。
	E1002 Code = "E1002"
	// E1003 PCD 字段缺少 x/y/z 坐标。
	E1003 Code = "E1003"
	// E1004 输入点云为空。
	E1004 Code = "E1004"

	// E2001 LAS 写入失败。
	E2001 Code = "E2001"
	// E2002 输出目录创建失败或不可写。
	E2002 Code = "E2002"

	// E3001 CRS / ENU 原点参数格式无效。
	E3001 Code = "E3001"

	// E4001 设备代理体配置解析失败。
	E4001 Code = "E4001"
	// E4002 设备代理体配置校验失败。
	E4002 Code = "E4002"

	// E5001 3D Tiles 生成失败。
	E5001 Code = "E5001"

	// E6001 参数取值不支持。
	E6001 Code = "E6001"
)

var messages = map[Code]string{
	E1001: "输入文件不存在或不可读",
	E1002: "无法解析 PCD 文件",
	E1003: "PCD 字段缺少 x/y/z 坐标",
	E1004: "输入点云为空",
	E2001: "LAS 文件写入失败",
	E2002: "输出目录创建失败或不可写",
	E3001: "CRS / ENU 原点参数格式无效",
	E4001: "设备代理体配置解析失败",
	E4002: "设备代理体配置校验失败",
	E5001: "3D Tiles 生成失败",
	E6001: "参数取值不支持",
}

// Message 返回错误码对应的用户可读提示。
func (c Code) Message() string {
	if m, ok := messages[c]; ok {
		return m
	}
	return "未知错误"
}

// Error 携带稳定错误码与底层根因。根因只用于日志。
type Error struct {
	Code Code
	// Cause 是技术根因，只进日志，不直接展示给用户。
	Cause error
	// Detail 是补充上下文，可以安全展示。
	Detail string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s：%s（%s）", e.Code, e.Code.Message(), e.Detail)
	}
	return fmt.Sprintf("%s：%s", e.Code, e.Code.Message())
}

func (e *Error) Unwrap() error { return e.Cause }

// New 构造带根因的错误。
func New(code Code, cause error, detail string) *Error {
	return &Error{Code: code, Cause: cause, Detail: detail}
}

// From 提取错误中的稳定错误码；没有时返回空。
func From(err error) (Code, bool) {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code, true
	}
	return "", false
}
