package platform

import (
	"errors"
	"log/slog"
	"os"
	"strings"
)

func NewLogger(level string) (*slog.Logger, error) {
	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parsedLevel,
	})
	return slog.New(handler), nil
}

func LogError(logger *slog.Logger, message string, err error, attrs ...any) {
	if logger == nil {
		return
	}
	fields := append([]any{"error", err, "code", CodeOf(err), "not_found", errors.Is(err, &Error{Code: CodeNotFound})}, attrs...)
	logger.Error(message, fields...)
}

func parseLevel(raw string) (slog.Leveler, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return nil, New(CodeInvalidArgument, "invalid log level")
	}
}
