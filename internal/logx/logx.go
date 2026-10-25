// Package logx 提供统一的 slog 日志构造。
package logx

import (
	"log/slog"
	"os"
	"strings"
)

// New 构造输出到 stdout 的分级文本日志；level 为 debug/info/warn/error。
func New(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	return slog.New(h)
}
