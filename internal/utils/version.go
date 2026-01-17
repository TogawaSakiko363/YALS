package utils

import (
	"fmt"
	"runtime"
)

// Version Information
const (
	AppName    = "YALS Community"
	AppVersion = "2026.2"
)

// GetVersionInfo Returns formatted version information
func GetVersionInfo() string {
	baseInfo := fmt.Sprintf(
		"Version: %s\n"+
			"Go Version: %s\n"+
			"OS: %s\n"+
			"Architecture: %s\n",
		AppVersion,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)

	return baseInfo
}

// GetAppName Returns the application name
func GetAppName() string {
	return AppName
}

// GetAppVersion Returns the application version
func GetAppVersion() string {
	return AppVersion
}
