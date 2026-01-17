package logger

import (
	"fmt"
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
		return INFO // Default to INFO level
	}
}

// Logger represents a custom logger with level filtering
type Logger struct {
	level LogLevel
	debug *log.Logger
	info  *log.Logger
	warn  *log.Logger
	error *log.Logger
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

// Debug logs a debug message
func (l *Logger) Debug(v ...interface{}) {
	if l.level <= DEBUG {
		pkg := getPackageName(3)
		if len(v) == 1 {
			l.debug.Output(3, fmt.Sprintf("[DEBUG] [%s]: %v", pkg, v[0]))
		} else {
			l.debug.Output(3, fmt.Sprintf("[DEBUG] [%s]: %v", pkg, fmt.Sprint(v...)))
		}
	}
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, v ...interface{}) {
	if l.level <= DEBUG && format != "" {
		pkg := getPackageName(3)
		l.debug.Output(3, fmt.Sprintf("[DEBUG] [%s]: %v", pkg, fmt.Sprintf(format, v...)))
	}
}

// Info logs an info message
func (l *Logger) Info(v ...interface{}) {
	if l.level <= INFO {
		pkg := getPackageName(3)
		if len(v) == 1 {
			l.info.Output(3, fmt.Sprintf("[INFO] [%s]: %v", pkg, v[0]))
		} else {
			l.info.Output(3, fmt.Sprintf("[INFO] [%s]: %v", pkg, fmt.Sprint(v...)))
		}
	}
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, v ...interface{}) {
	if l.level <= INFO && format != "" {
		pkg := getPackageName(3)
		l.info.Output(3, fmt.Sprintf("[INFO] [%s]: %v", pkg, fmt.Sprintf(format, v...)))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(v ...interface{}) {
	if l.level <= WARN {
		pkg := getPackageName(3)
		if len(v) == 1 {
			l.warn.Output(3, fmt.Sprintf("[WARN] [%s]: %v", pkg, v[0]))
		} else {
			l.warn.Output(3, fmt.Sprintf("[WARN] [%s]: %v", pkg, fmt.Sprint(v...)))
		}
	}
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, v ...interface{}) {
	if l.level <= WARN && format != "" {
		pkg := getPackageName(3)
		l.warn.Output(3, fmt.Sprintf("[WARN] [%s]: %v", pkg, fmt.Sprintf(format, v...)))
	}
}

// Error logs an error message
func (l *Logger) Error(v ...interface{}) {
	if l.level <= ERROR {
		pkg := getPackageName(3)
		if len(v) == 1 {
			l.error.Output(3, fmt.Sprintf("[ERROR] [%s]: %v", pkg, v[0]))
		} else {
			l.error.Output(3, fmt.Sprintf("[ERROR] [%s]: %v", pkg, fmt.Sprint(v...)))
		}
	}
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, v ...interface{}) {
	if l.level <= ERROR && format != "" {
		pkg := getPackageName(3)
		l.error.Output(3, fmt.Sprintf("[ERROR] [%s]: %v", pkg, fmt.Sprintf(format, v...)))
	}
}

// Fatal logs an error message and exits the program
func (l *Logger) Fatal(v ...interface{}) {
	pkg := getPackageName(3)
	if len(v) == 1 {
		l.error.Output(3, fmt.Sprintf("[ERROR] [%s]: %v", pkg, v[0]))
	} else {
		l.error.Output(3, fmt.Sprintf("[ERROR] [%s]: %v", pkg, fmt.Sprint(v...)))
	}
	os.Exit(1)
}

// Fatalf logs a formatted error message and exits the program
func (l *Logger) Fatalf(format string, v ...interface{}) {
	pkg := getPackageName(3)
	l.error.Output(3, fmt.Sprintf("[ERROR] [%s]: %v", pkg, fmt.Sprintf(format, v...)))
	os.Exit(1)
}

// Print logs a message at INFO level (for compatibility with standard log)
func (l *Logger) Print(v ...interface{}) {
	l.Info(v...)
}

// Printf logs a formatted message at INFO level (for compatibility with standard log)
func (l *Logger) Printf(format string, v ...interface{}) {
	l.Infof(format, v...)
}

// Println logs a message at INFO level (for compatibility with standard log)
func (l *Logger) Println(v ...interface{}) {
	l.Info(v...)
}
