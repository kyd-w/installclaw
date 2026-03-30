// Package logger provides logging functionality
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level represents log level
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the string representation of log level
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a string to log level
func ParseLevel(s string) Level {
	switch s {
	case "debug", "DEBUG":
		return LevelDebug
	case "info", "INFO":
		return LevelInfo
	case "warn", "WARN", "warning", "WARNING":
		return LevelWarn
	case "error", "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// Config contains logger configuration
type Config struct {
	Level  Level
	File   string
	Color  bool
	Prefix string
}

// Logger wraps standard logger with level and file output
type Logger struct {
	mu       sync.Mutex
	level    Level
	file     *os.File
	logger   *log.Logger
	color    bool
	prefix   string
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Init initializes the default logger
func Init(cfg *Config) error {
	var err error
	once.Do(func() {
		defaultLogger, err = NewLogger(cfg)
	})
	return err
}

// NewLogger creates a new logger
func NewLogger(cfg *Config) (*Logger, error) {
	if cfg == nil {
		cfg = &Config{
			Level: LevelInfo,
			Color: true,
		}
	}

	l := &Logger{
		level:  cfg.Level,
		color:  cfg.Color,
		prefix: cfg.Prefix,
	}

	// Set output writer
	var writer io.Writer = os.Stdout

	// If file is specified, open it and use multi-writer
	if cfg.File != "" {
		// Ensure directory exists
		dir := filepath.Dir(cfg.File)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create log directory: %w", err)
			}
		}

		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		l.file = file
		writer = io.MultiWriter(os.Stdout, file)
	}

	l.logger = log.New(writer, "", 0) // No default prefix, we'll add our own

	return l, nil
}

// Close closes the log file if opened
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// log writes a log message
func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)

	var levelStr string
	if l.color {
		levelStr = l.colorizeLevel(level)
	} else {
		levelStr = fmt.Sprintf("[%s]", level.String())
	}

	prefix := ""
	if l.prefix != "" {
		prefix = fmt.Sprintf("[%s] ", l.prefix)
	}

	l.logger.Printf("%s %s%s%s", timestamp, levelStr, prefix, message)
}

// colorizeLevel returns colored level string
func (l *Logger) colorizeLevel(level Level) string {
	const (
		colorReset  = "\033[0m"
		colorDebug  = "\033[36m" // Cyan
		colorInfo   = "\033[32m" // Green
		colorWarn   = "\033[33m" // Yellow
		colorError  = "\033[31m" // Red
	)

	var color string
	switch level {
	case LevelDebug:
		color = colorDebug
	case LevelInfo:
		color = colorInfo
	case LevelWarn:
		color = colorWarn
	case LevelError:
		color = colorError
	}

	return fmt.Sprintf("%s[%s]%s", color, level.String(), colorReset)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Fatal logs an error message and exits
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
	os.Exit(1)
}

// SetLevel sets the log level
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Global logger functions

// Debug logs a debug message using default logger
func Debug(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Debug(format, args...)
	}
}

// Info logs an info message using default logger
func Info(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Info(format, args...)
	}
}

// Warn logs a warning message using default logger
func Warn(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Warn(format, args...)
	}
}

// Error logs an error message using default logger
func Error(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Error(format, args...)
	}
}

// Fatal logs an error message and exits using default logger
func Fatal(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Fatal(format, args...)
	}
	os.Exit(1)
}

// Close closes the default logger
func Close() error {
	if defaultLogger != nil {
		return defaultLogger.Close()
	}
	return nil
}
