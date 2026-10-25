package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogLevel defines the severity threshold for logging.
type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
	LogLevelTrace
)

// Standard subsystem category identifiers.
const (
	LogCategoryNet        = "net"
	LogCategorySync       = "sync"
	LogCategoryValidation = "validation"
	LogCategoryMempool    = "mempool"
	LogCategoryRPC        = "rpc"
	LogCategoryMiner      = "miner"
	LogCategoryDiscovery  = "discovery"
	LogCategoryGeneral    = "general"
)

// String returns the string representation of a LogLevel.
func (l LogLevel) String() string {
	switch l {
	case LogLevelError:
		return "ERROR"
	case LogLevelWarn:
		return "WARN"
	case LogLevelInfo:
		return "INFO"
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelTrace:
		return "TRACE"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel converts a level name string to LogLevel.
func ParseLogLevel(s string) (LogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error", "err":
		return LogLevelError, nil
	case "warn", "warning":
		return LogLevelWarn, nil
	case "info", "notice":
		return LogLevelInfo, nil
	case "debug", "dbg":
		return LogLevelDebug, nil
	case "trace":
		return LogLevelTrace, nil
	default:
		return LogLevelInfo, fmt.Errorf("unknown log level: %q (expected: error, warn, info, debug, trace)", s)
	}
}

// Logger provides thread-safe, categorized, and level-filtered logging.
type Logger struct {
	mu             sync.Mutex
	level          LogLevel
	enabledCats    map[string]bool
	allDebug       bool
	printToConsole bool
	fileWriter     io.WriteCloser
	customWriter   io.Writer
}

// NewLogger initializes a new Logger instance with default settings.
func NewLogger() *Logger {
	return &Logger{
		level:          LogLevelInfo,
		enabledCats:    make(map[string]bool),
		printToConsole: true,
	}
}

// SetLevel sets the global minimum log severity.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current global log severity.
func (l *Logger) GetLevel() LogLevel {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

// SetPrintToConsole configures whether log output is written to standard output/console.
func (l *Logger) SetPrintToConsole(enable bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.printToConsole = enable
}

// SetCustomWriter sets an optional io.Writer for testing or redirecting output.
func (l *Logger) SetCustomWriter(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.customWriter = w
}

// SetDebugCategories enables debug logging for specified categories (comma-separated or "1"/"all").
func (l *Logger) SetDebugCategories(cats string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.enabledCats = make(map[string]bool)
	l.allDebug = false

	trimmed := strings.TrimSpace(cats)
	if trimmed == "" {
		return
	}

	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		cat := strings.ToLower(strings.TrimSpace(part))
		if cat == "1" || cat == "all" || cat == "*" {
			l.allDebug = true
			return
		}
		if cat != "" {
			l.enabledCats[cat] = true
		}
	}
}

// SetLogFile opens and attaches a file target for persistent logging.
func (l *Logger) SetLogFile(filePath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fileWriter != nil {
		_ = l.fileWriter.Close()
		l.fileWriter = nil
	}

	if filePath == "" {
		return nil
	}

	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create log directory %q: %w", dir, err)
		}
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file %q: %w", filePath, err)
	}

	l.fileWriter = f
	return nil
}

// Close flushes and closes active file writers.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fileWriter != nil {
		err := l.fileWriter.Close()
		l.fileWriter = nil
		return err
	}
	return nil
}

// ShouldLog checks whether a message of given level and category should be emitted.
func (l *Logger) ShouldLog(level LogLevel, category string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// If global level is high enough to include this level, check basic inclusion
	if level <= l.level {
		return true
	}

	// If message is Debug or Trace, check if category is specifically enabled
	if level == LogLevelDebug || level == LogLevelTrace {
		if l.allDebug {
			return true
		}
		if l.enabledCats[strings.ToLower(category)] {
			return true
		}
	}

	return false
}

// Log writes a formatted message to console and file if enabled.
func (l *Logger) Log(level LogLevel, category string, format string, args ...any) {
	if !l.ShouldLog(level, category) {
		return
	}

	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	// Strip trailing newline if present to normalize
	msg = strings.TrimRight(msg, "\r\n")

	ts := time.Now().UTC().Format("2006-01-02 15:04:05")
	cat := strings.ToLower(category)
	if cat == "" {
		cat = LogCategoryGeneral
	}

	line := fmt.Sprintf("%s [%s] [%s] %s\n", ts, level.String(), cat, msg)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.customWriter != nil {
		_, _ = l.customWriter.Write([]byte(line))
	}

	if l.printToConsole {
		if level == LogLevelError {
			_, _ = os.Stderr.WriteString(line)
		} else {
			_, _ = os.Stdout.WriteString(line)
		}
	}

	if l.fileWriter != nil {
		_, _ = l.fileWriter.Write([]byte(line))
	}
}

// Global default logger instance
var defaultLogger = NewLogger()

// InitLogger configures the global logger instance with CLI flags and targets.
func InitLogger(levelStr string, debugCategories string, printToConsole bool, logFilePath string) error {
	level, err := ParseLogLevel(levelStr)
	if err != nil {
		level = LogLevelInfo
	}

	defaultLogger.SetLevel(level)
	defaultLogger.SetPrintToConsole(printToConsole)
	defaultLogger.SetDebugCategories(debugCategories)

	if logFilePath != "" {
		return defaultLogger.SetLogFile(logFilePath)
	}
	return nil
}

// CloseLogger closes active log files on daemon shutdown.
func CloseLogger() {
	_ = defaultLogger.Close()
}

// SetGlobalLogLevel updates the active logging threshold.
func SetGlobalLogLevel(level LogLevel) {
	defaultLogger.SetLevel(level)
}

// SetGlobalPrintToConsole updates console output behavior.
func SetGlobalPrintToConsole(enable bool) {
	defaultLogger.SetPrintToConsole(enable)
}

// GetGlobalLogger returns the global logger instance.
func GetGlobalLogger() *Logger {
	return defaultLogger
}

// LogInfo emits an INFO-level log message.
func LogInfo(category string, format string, args ...any) {
	defaultLogger.Log(LogLevelInfo, category, format, args...)
}

// LogWarn emits a WARN-level log message.
func LogWarn(category string, format string, args ...any) {
	defaultLogger.Log(LogLevelWarn, category, format, args...)
}

// LogError emits an ERROR-level log message.
func LogError(category string, format string, args ...any) {
	defaultLogger.Log(LogLevelError, category, format, args...)
}

// LogDebug emits a DEBUG-level log message.
func LogDebug(category string, format string, args ...any) {
	defaultLogger.Log(LogLevelDebug, category, format, args...)
}

// LogTrace emits a TRACE-level log message.
func LogTrace(category string, format string, args ...any) {
	defaultLogger.Log(LogLevelTrace, category, format, args...)
}

// IsDebugEnabled returns whether debug logging is active globally or for a category.
func IsDebugEnabled(category string) bool {
	return defaultLogger.ShouldLog(LogLevelDebug, category)
}
