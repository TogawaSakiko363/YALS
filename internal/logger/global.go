package logger

import (
	"io"
	"os"
)

// Global logger instance
var globalLogger *Logger

// init initializes the global logger with INFO level by default
func init() {
	globalLogger = New(INFO, os.Stdout)
}

// SetGlobalLevel sets the global logger level
func SetGlobalLevel(level LogLevel) {
	globalLogger.SetLevel(level)
}

// SetGlobalLevelFromString sets the global logger level from a string
func SetGlobalLevelFromString(level string) {
	globalLogger.SetLevel(ParseLogLevel(level))
}

// SetGlobalOutput sets the global logger output
func SetGlobalOutput(output io.Writer) {
	globalLogger = New(globalLogger.GetLevel(), output)
}

// GetGlobalLogger returns the global logger instance
func GetGlobalLogger() *Logger {
	return globalLogger
}

// Global logging functions for convenience

// Debug logs a debug message using the global logger
func Debug(v ...interface{}) {
	globalLogger.Debug(v...)
}

// Debugf logs a formatted debug message using the global logger
func Debugf(format string, v ...interface{}) {
	globalLogger.Debugf(format, v...)
}

// Info logs an info message using the global logger
func Info(v ...interface{}) {
	globalLogger.Info(v...)
}

// Infof logs a formatted info message using the global logger
func Infof(format string, v ...interface{}) {
	globalLogger.Infof(format, v...)
}

// Warn logs a warning message using the global logger
func Warn(v ...interface{}) {
	globalLogger.Warn(v...)
}

// Warnf logs a formatted warning message using the global logger
func Warnf(format string, v ...interface{}) {
	globalLogger.Warnf(format, v...)
}

// Error logs an error message using the global logger
func Error(v ...interface{}) {
	globalLogger.Error(v...)
}

// Errorf logs a formatted error message using the global logger
func Errorf(format string, v ...interface{}) {
	globalLogger.Errorf(format, v...)
}

// Fatal logs an error message and exits the program using the global logger
func Fatal(v ...interface{}) {
	globalLogger.Fatal(v...)
}

// Fatalf logs a formatted error message and exits the program using the global logger
func Fatalf(format string, v ...interface{}) {
	globalLogger.Fatalf(format, v...)
}

// Print logs a message at INFO level using the global logger
func Print(v ...interface{}) {
	globalLogger.Print(v...)
}

// Printf logs a formatted message at INFO level using the global logger
func Printf(format string, v ...interface{}) {
	globalLogger.Printf(format, v...)
}

// Println logs a message at INFO level using the global logger
func Println(v ...interface{}) {
	globalLogger.Println(v...)
}
