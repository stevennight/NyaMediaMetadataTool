//go:build !windows

package main

import "errors"

func desktopAutostartSupported() bool {
	return false
}

func desktopAutostartEnabled() (bool, error) {
	return false, nil
}

func setDesktopAutostart(bool) error {
	return errors.New("autostart is not supported on this platform")
}
