//go:build windows

package main

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	desktopAutostartKey        = `Software\Microsoft\Windows\CurrentVersion\Run`
	desktopAutostartName       = desktopProductName
	legacyDesktopAutostartName = "Nya Media"
)

func desktopAutostartSupported() bool {
	return true
}

func desktopAutostartEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, desktopAutostartKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()

	value, _, err := key.GetStringValue(desktopAutostartName)
	if err == nil {
		_ = key.DeleteValue(legacyDesktopAutostartName)
		return strings.TrimSpace(value) != "", nil
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}

	value, _, err = key.GetStringValue(legacyDesktopAutostartName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(value) == "" {
		return false, nil
	}
	if err := key.SetStringValue(desktopAutostartName, value); err != nil {
		return false, err
	}
	if err := key.DeleteValue(legacyDesktopAutostartName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	return true, nil
}

func setDesktopAutostart(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, desktopAutostartKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enabled {
		for _, name := range []string{desktopAutostartName, legacyDesktopAutostartName} {
			if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
				return err
			}
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := key.SetStringValue(desktopAutostartName, `"`+executable+`" --background`); err != nil {
		return err
	}
	if err := key.DeleteValue(legacyDesktopAutostartName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
