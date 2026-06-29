package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

type Logger struct {
	mu     sync.Mutex
	level  slog.Level
	file   string
	logger *slog.Logger
}

func New(level, file string) (*Logger, error) {
	l := &Logger{level: parseLevel(level), file: file}
	if err := l.rebuild(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Logger) Logger() *slog.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.logger
}

func (l *Logger) Configure(level, file string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = parseLevel(level)
	l.file = file
	return l.rebuildLocked()
}

func (l *Logger) rebuild() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rebuildLocked()
}

func (l *Logger) rebuildLocked() error {
	writers := []io.Writer{os.Stdout}
	if l.file != "" {
		f, err := os.OpenFile(l.file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		writers = append(writers, f)
	}

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: l.level})
	l.logger = slog.New(handler)
	slog.SetDefault(l.logger)
	return nil
}

func parseLevel(s string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
