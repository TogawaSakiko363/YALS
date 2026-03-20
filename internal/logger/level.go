package logger

import (
	"io"
	"log"
	"os"
	"runtime"
	"strings"
)

// LogLevel represents the logging level
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel parses a string into a LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warn", "warning":
		return WARN
	case "error":
		return ERROR
	default:
		return INFO
	}
}

func getPackageName(calldepth int) string {
	pc, _, _, ok := runtime.Caller(calldepth)
	if !ok {
		return "unknown"
	}
	funcName := runtime.FuncForPC(pc).Name()
	lastSlash := strings.LastIndex(funcName, "/")
	if lastSlash == -1 {
		lastSlash = 0
	} else {
		lastSlash++
	}
	dotIndex := strings.Index(funcName[lastSlash:], ".")
	if dotIndex == -1 {
		return funcName[lastSlash:]
	}
	return funcName[lastSlash : lastSlash+dotIndex]
}

func New(level LogLevel, output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}

	flags := log.Ldate | log.Ltime

	return &Logger{
		level: level,
		debug: log.New(output, "", flags),
		info:  log.New(output, "", flags),
		warn:  log.New(output, "", flags),
		error: log.New(output, "", flags),
	}
}

// SetLevel changes the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() LogLevel {
	return l.level
}
