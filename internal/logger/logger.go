package logger

import (
	"fmt"
	"io"
	"log"
	"os"
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

// New creates a new Logger with the specified level
func New(level LogLevel, output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}

	flags := log.Ldate | log.Ltime | log.Lshortfile

	return &Logger{
		level: level,
		debug: log.New(output, "[DEBUG] ", flags),
		info:  log.New(output, "[INFO]  ", flags),
		warn:  log.New(output, "[WARN]  ", flags),
		error: log.New(output, "[ERROR] ", flags),
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
		if len(v) == 1 {
			l.debug.Output(2, fmt.Sprint(v[0]))
		} else {
			l.debug.Output(2, fmt.Sprint(v...))
		}
	}
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, v ...interface{}) {
	if l.level <= DEBUG && format != "" {
		l.debug.Output(2, fmt.Sprintf(format, v...))
	}
}

// Info logs an info message
func (l *Logger) Info(v ...interface{}) {
	if l.level <= INFO {
		if len(v) == 1 {
			l.info.Output(2, fmt.Sprint(v[0]))
		} else {
			l.info.Output(2, fmt.Sprint(v...))
		}
	}
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, v ...interface{}) {
	if l.level <= INFO && format != "" {
		l.info.Output(2, fmt.Sprintf(format, v...))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(v ...interface{}) {
	if l.level <= WARN {
		if len(v) == 1 {
			l.warn.Output(2, fmt.Sprint(v[0]))
		} else {
			l.warn.Output(2, fmt.Sprint(v...))
		}
	}
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, v ...interface{}) {
	if l.level <= WARN && format != "" {
		l.warn.Output(2, fmt.Sprintf(format, v...))
	}
}

// Error logs an error message
func (l *Logger) Error(v ...interface{}) {
	if l.level <= ERROR {
		if len(v) == 1 {
			l.error.Output(2, fmt.Sprint(v[0]))
		} else {
			l.error.Output(2, fmt.Sprint(v...))
		}
	}
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, v ...interface{}) {
	if l.level <= ERROR && format != "" {
		l.error.Output(2, fmt.Sprintf(format, v...))
	}
}

// Fatal logs an error message and exits the program
func (l *Logger) Fatal(v ...interface{}) {
	l.error.Output(2, fmt.Sprint(v...))
	os.Exit(1)
}

// Fatalf logs a formatted error message and exits the program
func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.error.Output(2, fmt.Sprintf(format, v...))
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
