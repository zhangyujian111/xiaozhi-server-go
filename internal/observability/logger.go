// Package observability 提供可观测性基础设施：日志、metrics、tracing。
//
// 日志采用 zerolog 作为底层引擎，通过 slog.Handler 接口暴露，
// 确保与 Go 标准库生态兼容。
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/ykt/xiaozhi-server-go/internal/config"
)

// SlogHandler 将 zerolog 适配为 slog.Handler 接口。
//
// 实现 slog.Handler，将 slog.Record 转换为 zerolog.Event 输出。
// 支持结构化日志、日志级别映射、属性分组。
type SlogHandler struct {
	logger zerolog.Logger
	level  slog.Level
	attrs  []slog.Attr
	group  string
}

// NewSlogHandler 创建 slog.Handler（包装 zerolog.Logger）。
func NewSlogHandler(logger zerolog.Logger) *SlogHandler {
	return &SlogHandler{
		logger: logger,
		level:  slogLevelFromZerolog(logger.GetLevel()),
	}
}

// Enabled 判断指定级别是否启用。
func (h *SlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle 处理一条日志记录。
func (h *SlogHandler) Handle(_ context.Context, r slog.Record) error {
	zlvl := zerologLevelFromSlog(r.Level)
	if zlvl == zerolog.NoLevel {
		return nil
	}

	event := h.logger.WithLevel(zlvl)

	// 写入预置属性
	for _, attr := range h.attrs {
		writeAttr(event, attr, h.group)
	}

	// 写入记录属性
	r.Attrs(func(attr slog.Attr) bool {
		writeAttr(event, attr, h.group)
		return true
	})

	// 写入消息
	event.Msg(r.Message)
	return nil
}

// WithAttrs 返回带预设属性的新 Handler。
func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newH := h.clone()
	newH.attrs = append(newH.attrs, attrs...)
	return newH
}

// WithGroup 返回带分组的新 Handler。
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	newH := h.clone()
	if newH.group != "" {
		newH.group = newH.group + "." + name
	} else {
		newH.group = name
	}
	return newH
}

func (h *SlogHandler) clone() *SlogHandler {
	attrsCopy := make([]slog.Attr, len(h.attrs))
	copy(attrsCopy, h.attrs)
	return &SlogHandler{
		logger: h.logger,
		level:  h.level,
		attrs:  attrsCopy,
		group:  h.group,
	}
}

// writeAttr 将 slog.Attr 写入 zerolog.Event。
func writeAttr(event *zerolog.Event, attr slog.Attr, group string) {
	key := attr.Key
	if group != "" {
		key = group + "." + key
	}

	attr.Value = attr.Value.Resolve()
	switch attr.Value.Kind() {
	case slog.KindString:
		event.Str(key, attr.Value.String())
	case slog.KindInt64:
		event.Int64(key, attr.Value.Int64())
	case slog.KindUint64:
		event.Uint64(key, attr.Value.Uint64())
	case slog.KindFloat64:
		event.Float64(key, attr.Value.Float64())
	case slog.KindBool:
		event.Bool(key, attr.Value.Bool())
	case slog.KindDuration:
		event.Dur(key, attr.Value.Duration())
	case slog.KindTime:
		event.Time(key, attr.Value.Time())
	case slog.KindGroup:
		for _, subAttr := range attr.Value.Group() {
			writeAttr(event, subAttr, key)
		}
	default:
		event.Str(key, fmt.Sprintf("%v", attr.Value.Any()))
	}
}

// slogLevelFromZerolog 将 zerolog.Level 转换为 slog.Level。
func slogLevelFromZerolog(zl zerolog.Level) slog.Level {
	switch zl {
	case zerolog.DebugLevel:
		return slog.LevelDebug
	case zerolog.InfoLevel:
		return slog.LevelInfo
	case zerolog.WarnLevel:
		return slog.LevelWarn
	case zerolog.ErrorLevel:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// zerologLevelFromSlog 将 slog.Level 转换为 zerolog.Level。
func zerologLevelFromSlog(sl slog.Level) zerolog.Level {
	switch {
	case sl >= slog.LevelError:
		return zerolog.ErrorLevel
	case sl >= slog.LevelWarn:
		return zerolog.WarnLevel
	case sl >= slog.LevelInfo:
		return zerolog.InfoLevel
	case sl >= slog.LevelDebug:
		return zerolog.DebugLevel
	default:
		return zerolog.NoLevel
	}
}

// NewLogger 根据配置创建 zerolog.Logger。
//
// 支持两种输出格式：
//   - json：结构化 JSON 输出（生产环境推荐）
//   - console：彩色控制台输出（开发调试）
//
// 支持两种输出目标：
//   - stdout：标准输出
//   - file：写入文件
func NewLogger(cfg config.LogConfig) zerolog.Logger {
	var output io.Writer

	switch cfg.Output {
	case "file":
		if cfg.FilePath == "" {
			cfg.FilePath = "logs/xiaozhi-server-go.log"
		}
		f, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "logger: failed to open log file %s: %v, falling back to stdout\n", cfg.FilePath, err)
			output = os.Stdout
		} else {
			output = f
		}
	default:
		output = os.Stdout
	}

	// 根据格式选择输出 writer
	switch cfg.Format {
	case "console":
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
			NoColor:    false,
		}
	default:
		// JSON 格式，无需额外包装
	}

	// 解析日志级别
	level := parseLevel(cfg.Level)

	return zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Caller().
		Str("service", "xiaozhi-server-go").
		Logger()
}

// parseLevel 解析日志级别字符串。
func parseLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}