//go:build !windows

package main

import "errors"

func desktopTraySupported() bool {
	return false
}

func newDesktopTray(func(), func()) (desktopTray, error) {
	return nil, errors.New("system tray is not supported on this platform")
}
