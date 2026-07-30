//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func desktopInstalled() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return false
	}
	uninstallRoots := []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE}
	views := []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY}
	for _, root := range uninstallRoots {
		for _, view := range views {
			key, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\Uninstall`, registry.READ|view)
			if err != nil {
				continue
			}
			names, _ := key.ReadSubKeyNames(-1)
			for _, name := range names {
				entry, err := registry.OpenKey(key, name, registry.READ)
				if err != nil {
					continue
				}
				displayName, _, _ := entry.GetStringValue("DisplayName")
				installLocation, _, _ := entry.GetStringValue("InstallLocation")
				displayIcon, _, _ := entry.GetStringValue("DisplayIcon")
				entry.Close()
				if strings.EqualFold(strings.TrimSpace(displayName), desktopProductName) &&
					installedPathMatches(executable, installLocation, displayIcon) {
					key.Close()
					return true
				}
			}
			key.Close()
		}
	}
	return false
}

func installedPathMatches(executable, installLocation, displayIcon string) bool {
	executable = normalizeWindowsPath(executable)
	location := normalizeWindowsPath(strings.TrimSpace(installLocation))
	if location != "" && (executable == location || strings.HasPrefix(executable, location+`\`)) {
		return true
	}
	icon := strings.TrimSpace(displayIcon)
	if strings.HasPrefix(icon, `"`) {
		icon = strings.TrimPrefix(icon, `"`)
		if index := strings.Index(icon, `"`); index >= 0 {
			icon = icon[:index]
		}
	} else if index := strings.Index(icon, ","); index >= 0 {
		icon = icon[:index]
	}
	return normalizeWindowsPath(icon) == executable
}

func normalizeWindowsPath(value string) string {
	return strings.ToLower(strings.TrimRight(filepath.Clean(value), `\`))
}
