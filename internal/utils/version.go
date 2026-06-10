package utils

import (
	"fmt"
	"runtime"
)

// Version Information
const (
	AppName    = "YALS NR"
	AppVersion = "2026.06"
)

// GetVersionInfo Returns formatted version information
func GetVersionInfo(plugins []string) string {

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

	if len(plugins) == 0 {
		return baseInfo
	}

	// Sort plugins alphabetically for consistent output
	sortedPlugins := make([]string, len(plugins))
	copy(sortedPlugins, plugins)

	// Simple bubble sort (good enough for small lists)
	for i := 0; i < len(sortedPlugins)-1; i++ {
		for j := 0; j < len(sortedPlugins)-i-1; j++ {
			if sortedPlugins[j] > sortedPlugins[j+1] {
				sortedPlugins[j], sortedPlugins[j+1] = sortedPlugins[j+1], sortedPlugins[j]
			}
		}
	}

	pluginList := ""
	for i, plugin := range sortedPlugins {
		if i > 0 {
			pluginList += ", "
		}
		pluginList += plugin
	}

	return fmt.Sprintf("%s\nAvailable Plugins: %s", baseInfo, pluginList)
}

// GetAppName Returns the application name
func GetAppName() string {
	return AppName
}

// GetAppVersion Returns the application version
func GetAppVersion() string {
	return AppVersion
}
