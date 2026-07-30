//go:build !windows

package main

func desktopInstalled() bool {
	return false
}
