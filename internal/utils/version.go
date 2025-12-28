package utils

import (
	"fmt"
	"runtime"
)

// Version Information
const (
	AppName    = "YALS Community"
	AppVersion = "2025.12d"
)

// GetVersionInfo Returns formatted version information
func GetVersionInfo() string {
	return fmt.Sprintf(
		"Version: %s\n"+
			"Go Version: %s\n"+
			"OS: %s\n"+
			"Architecture: %s",
		AppVersion,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}

// GetAppName Returns the application name
func GetAppName() string {
	return AppName
}

// GetAppVersion Returns the application version
func GetAppVersion() string {
	return AppVersion
}
